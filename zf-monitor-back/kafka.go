package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

const (
	kafkaDefaultAddress       = "kafka:9092"
	kafkaMetricsTopic         = "stark.metrics.raw"
	kafkaDefaultConsumerGroup = "stark-monitor-metrics-v1"
	kafkaOperationTimeout     = 2 * time.Second
)

var (
	kafkaWriter    *kafka.Writer
	kafkaReader    *kafka.Reader
	kafkaPublisher eventPublisher
)

type HostReportEvent struct {
	EventID       string            `json:"eventId"`
	EventType     string            `json:"eventType"`
	SchemaVersion int               `json:"schemaVersion"`
	HostID        string            `json:"hostId"`
	Hostname      string            `json:"hostname"`
	OccurredAt    string            `json:"occurredAt"`
	IngestedAt    string            `json:"ingestedAt"`
	Payload       HostReportPayload `json:"payload"`
}

type HostReportPayload struct {
	CPU       float64       `json:"cpu"`
	Memory    float64       `json:"memory"`
	Disk      float64       `json:"disk"`
	NetUp     float64       `json:"netUp"`
	NetDown   float64       `json:"netDown"`
	Processes []ProcessInfo `json:"processes"`
}

type eventPublisher interface {
	Publish(context.Context, HostReportEvent) error
}

type kafkaEventPublisher struct {
	writer *kafka.Writer
}

func kafkaAddress() (string, bool) {
	value, configured := os.LookupEnv("KAFKA_ADDR")
	address := strings.TrimSpace(value)
	if configured && address == "" {
		return "", false
	}
	if address == "" {
		return kafkaDefaultAddress, true
	}
	return address, true
}

func kafkaTopicName() string {
	if topic := strings.TrimSpace(os.Getenv("KAFKA_TOPIC")); topic != "" {
		return topic
	}
	return kafkaMetricsTopic
}

func kafkaConsumerGroup() string {
	if group := strings.TrimSpace(os.Getenv("KAFKA_CONSUMER_GROUP")); group != "" {
		return group
	}
	return kafkaDefaultConsumerGroup
}

func initKafka() {
	address, enabled := kafkaAddress()
	if !enabled {
		log.Printf("kafka disabled: KAFKA_ADDR is empty")
		return
	}
	kafkaWriter = &kafka.Writer{Addr: kafka.TCP(address), Topic: kafkaTopicName(), Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll, WriteTimeout: kafkaOperationTimeout, Async: false}
	kafkaPublisher = kafkaEventPublisher{writer: kafkaWriter}
	log.Printf("kafka configured addr=%s topic=%s", address, kafkaTopicName())
}

func closeKafkaReader() {
	if kafkaReader != nil {
		if err := kafkaReader.Close(); err != nil {
			log.Printf("kafka reader close failed: %v", err)
		}
		kafkaReader = nil
	}
}

func closeKafka() {
	if kafkaWriter != nil {
		if err := kafkaWriter.Close(); err != nil {
			log.Printf("kafka writer close failed: %v", err)
		}
		kafkaWriter = nil
	}
	kafkaPublisher = nil
}

func newHostReportEvent(report Report, ingestedAt time.Time) HostReportEvent {
	ingestedAt = ingestedAt.UTC()
	occurredAt := ingestedAt
	if report.Timestamp > 0 {
		occurredAt = time.Unix(report.Timestamp, 0).UTC()
	}
	return HostReportEvent{EventID: uuid.NewString(), EventType: "host.report", SchemaVersion: 1, HostID: report.HostID, Hostname: report.Hostname, OccurredAt: occurredAt.Format(time.RFC3339), IngestedAt: ingestedAt.Format(time.RFC3339), Payload: HostReportPayload{CPU: report.CPU, Memory: report.Memory, Disk: report.Disk, NetUp: report.NetUp, NetDown: report.NetDown, Processes: report.Processes}}
}

func (publisher kafkaEventPublisher) Publish(ctx context.Context, event HostReportEvent) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return publisher.writer.WriteMessages(ctx, kafka.Message{Key: []byte(event.HostID), Value: encoded})
}

func publishHostReportEvent(ctx context.Context, event HostReportEvent) error {
	if kafkaPublisher == nil {
		return fmt.Errorf("kafka is disabled")
	}
	return kafkaPublisher.Publish(ctx, event)
}

func startKafkaConsumer(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	address, enabled := kafkaAddress()
	if !enabled {
		close(done)
		return done
	}
	kafkaReader = kafka.NewReader(kafka.ReaderConfig{Brokers: []string{address}, Topic: kafkaTopicName(), GroupID: kafkaConsumerGroup()})
	go runKafkaConsumer(ctx, kafkaReader, done)
	return done
}

func runKafkaConsumer(ctx context.Context, reader *kafka.Reader, done chan<- struct{}) {
	defer close(done)
	log.Printf("kafka consumer started topic=%s group=%s", kafkaTopicName(), kafkaConsumerGroup())
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Printf("kafka consumer stopped")
				return
			}
			log.Printf("kafka consumer fetch failed topic=%s group=%s err=%v", kafkaTopicName(), kafkaConsumerGroup(), err)
			if !waitKafkaRetry(ctx, time.Second) {
				log.Printf("kafka consumer stopped")
				return
			}
			continue
		}

		var event HostReportEvent
		if err := json.Unmarshal(message.Value, &event); err != nil || !validHostReportEvent(event) || string(message.Key) != event.HostID {
			if err == nil {
				err = fmt.Errorf("invalid event schema or message key")
			}
			log.Printf("kafka poison message topic=%s partition=%d offset=%d eventId=%s err=%v", message.Topic, message.Partition, message.Offset, event.EventID, err)
			if err := reader.CommitMessages(ctx, message); err != nil {
				log.Printf("kafka poison message commit failed topic=%s partition=%d offset=%d eventId=%s err=%v", message.Topic, message.Partition, message.Offset, event.EventID, err)
			}
			continue
		}

		for backoff := time.Second; ; backoff = minKafkaBackoff(backoff * 2) {
			if err := processHostReportEvent(event); err != nil {
				log.Printf("kafka consumer processing failed hostId=%s eventId=%s partition=%d offset=%d err=%v", event.HostID, event.EventID, message.Partition, message.Offset, err)
				if !waitKafkaRetry(ctx, backoff) {
					log.Printf("kafka consumer stopped")
					return
				}
				continue
			}
			break
		}

		for backoff := time.Second; ; backoff = minKafkaBackoff(backoff * 2) {
			if err := reader.CommitMessages(ctx, message); err != nil {
				log.Printf("kafka consumer commit failed hostId=%s eventId=%s partition=%d offset=%d err=%v", event.HostID, event.EventID, message.Partition, message.Offset, err)
				if !waitKafkaRetry(ctx, backoff) {
					log.Printf("kafka consumer stopped")
					return
				}
				continue
			}
			break
		}
	}
}

func validHostReportEvent(event HostReportEvent) bool {
	return event.EventType == "host.report" && event.SchemaVersion == 1 && strings.TrimSpace(event.HostID) != ""
}

func minKafkaBackoff(value time.Duration) time.Duration {
	if value > 5*time.Second {
		return 5 * time.Second
	}
	return value
}

func waitKafkaRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func processHostReportEvent(event HostReportEvent) error {
	occurredAt, err := time.Parse(time.RFC3339, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("parse occurredAt: %w", err)
	}
	ingestedAt, err := time.Parse(time.RFC3339, event.IngestedAt)
	if err != nil {
		return fmt.Errorf("parse ingestedAt: %w", err)
	}
	report := Report{HostID: event.HostID, Hostname: event.Hostname, CPU: event.Payload.CPU, Memory: event.Payload.Memory, Disk: event.Payload.Disk, NetUp: event.Payload.NetUp, NetDown: event.Payload.NetDown, Processes: event.Payload.Processes}
	if err := saveHostRecord(report.HostID, report.Hostname, ingestedAt); err != nil {
		return err
	}
	if err := saveMetrics(report, occurredAt); err != nil {
		return err
	}
	if err := evaluateAlerts(report); err != nil {
		return err
	}
	stateMu.Lock()
	hostStates[report.HostID] = &HostRuntimeState{Hostname: report.Hostname, LastSeen: ingestedAt, Processes: append([]ProcessInfo(nil), report.Processes...)}
	stateMu.Unlock()
	updateRedisAfterReport(report, ingestedAt)
	return nil
}

func kafkaTopicPartitions(address, topic string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), kafkaOperationTimeout)
	defer cancel()
	connection, err := kafka.DialContext(ctx, "tcp", address)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	partitions, err := connection.ReadPartitions(topic)
	if err != nil {
		return 0, err
	}
	if len(partitions) == 0 {
		return 0, fmt.Errorf("topic %s has no partitions", topic)
	}
	return len(partitions), nil
}

func handleKafkaStatus(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	address, enabled := kafkaAddress()
	topic := kafkaTopicName()
	if !enabled || kafkaWriter == nil {
		writeJSON(w, map[string]interface{}{"enabled": false, "status": "disabled"})
		return
	}
	partitions, err := kafkaTopicPartitions(address, topic)
	if err != nil {
		writeJSON(w, map[string]interface{}{"enabled": true, "status": "unavailable", "broker": address, "topic": topic})
		return
	}
	writeJSON(w, map[string]interface{}{"enabled": true, "status": "healthy", "broker": address, "topic": topic, "consumerGroup": kafkaConsumerGroup(), "partitions": partitions})
}
