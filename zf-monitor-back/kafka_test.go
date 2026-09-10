package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeEventPublisher struct {
	event HostReportEvent
	err   error
}

func (publisher *fakeEventPublisher) Publish(_ context.Context, event HostReportEvent) error {
	publisher.event = event
	return publisher.err
}

func withTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := initDB(database); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	previousDatabase := stateDB
	stateDB = database
	stateMu.Lock()
	previousStates := hostStates
	hostStates = map[string]*HostRuntimeState{}
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		hostStates = previousStates
		stateMu.Unlock()
		stateDB = previousDatabase
		_ = database.Close()
	})
	return database
}

func reportRequest(t *testing.T, report Report) *http.Request {
	t.Helper()
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest(http.MethodPost, "/api/report", bytes.NewReader(body))
}

func TestHostReportEventSchema(t *testing.T) {
	ingestedAt := time.Date(2024, 9, 1, 12, 0, 0, 0, time.UTC)
	report := Report{HostID: "host-1", Hostname: "monitor-1", Timestamp: 1725188400, CPU: 10.5, Memory: 20.5, Disk: 30.5, NetUp: 40.5, NetDown: 50.5, Processes: []ProcessInfo{{PID: 42, Name: "agent", CPU: 1.5, MemoryMB: 12.5}}}
	event := newHostReportEvent(report, ingestedAt)
	if event.EventID == "" || event.EventType != "host.report" || event.SchemaVersion != 1 || event.HostID != report.HostID || event.Hostname != report.Hostname {
		t.Fatalf("unexpected event header: %+v", event)
	}
	if event.OccurredAt != time.Unix(report.Timestamp, 0).UTC().Format(time.RFC3339) || event.IngestedAt != ingestedAt.Format(time.RFC3339) {
		t.Fatalf("unexpected event timestamps: %+v", event)
	}
	if event.Payload.CPU != report.CPU || event.Payload.Memory != report.Memory || event.Payload.Disk != report.Disk || event.Payload.NetUp != report.NetUp || event.Payload.NetDown != report.NetDown || len(event.Payload.Processes) != 1 {
		t.Fatalf("unexpected payload: %+v", event.Payload)
	}
	if _, err := json.Marshal(event); err != nil {
		t.Fatalf("event is not JSON serializable: %v", err)
	}
}

func TestHostReportEventTimestampFallsBackToIngestedAt(t *testing.T) {
	ingestedAt := time.Date(2024, 9, 1, 12, 0, 0, 0, time.UTC)
	event := newHostReportEvent(Report{HostID: "host-1", Timestamp: 0}, ingestedAt)
	if event.OccurredAt != event.IngestedAt || event.OccurredAt == "1970-01-01T00:00:00Z" {
		t.Fatalf("unexpected fallback occurredAt: %s", event.OccurredAt)
	}
}

func TestKafkaDisabledStatusAndIngestion(t *testing.T) {
	database := withTestDatabase(t)
	previousWriter, previousPublisher := kafkaWriter, kafkaPublisher
	kafkaWriter, kafkaPublisher = nil, nil
	t.Setenv("KAFKA_ADDR", "")
	t.Cleanup(func() { kafkaWriter, kafkaPublisher = previousWriter, previousPublisher })

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/kafka/status", nil)
	statusResponse := httptest.NewRecorder()
	handleKafkaStatus(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", statusResponse.Code)
	}
	var status map[string]interface{}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status["enabled"] != false || status["status"] != "disabled" {
		t.Fatalf("unexpected disabled status: %#v", status)
	}

	response := httptest.NewRecorder()
	handleReport(response, reportRequest(t, Report{HostID: "disabled-host", Hostname: "disabled"}))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected ingestion status: %d", response.Code)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("disabled Kafka wrote SQLite: count=%d err=%v", count, err)
	}
}

func TestHandleReportReturnsServiceUnavailableWhenPublishFails(t *testing.T) {
	database := withTestDatabase(t)
	previousPublisher := kafkaPublisher
	kafkaPublisher = &fakeEventPublisher{err: errors.New("broker unavailable")}
	t.Cleanup(func() { kafkaPublisher = previousPublisher })

	response := httptest.NewRecorder()
	handleReport(response, reportRequest(t, Report{HostID: "failed-host", Hostname: "failed"}))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed publication wrote SQLite: count=%d err=%v", count, err)
	}
}

func TestHandleReportPublishesAcceptedEvent(t *testing.T) {
	publisher := &fakeEventPublisher{}
	previousPublisher := kafkaPublisher
	kafkaPublisher = publisher
	t.Cleanup(func() { kafkaPublisher = previousPublisher })
	report := Report{HostID: "host-1", Hostname: "monitor-1", Timestamp: 1725188400, CPU: 10.5, Processes: []ProcessInfo{{PID: 1, Name: "agent"}}}

	response := httptest.NewRecorder()
	handleReport(response, reportRequest(t, report))
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "accepted" || publisher.event.HostID != report.HostID || publisher.event.Payload.CPU != report.CPU || len(publisher.event.Payload.Processes) != 1 {
		t.Fatalf("unexpected published event: %+v", publisher.event)
	}
}

func TestProcessHostReportEventWritesSQLiteUsingEventTimes(t *testing.T) {
	database := withTestDatabase(t)
	ingestedAt := time.Date(2024, 9, 1, 12, 5, 0, 0, time.UTC)
	report := Report{HostID: "host-1", Hostname: "monitor-1", Timestamp: 1725188400, CPU: 10.5}
	event := newHostReportEvent(report, ingestedAt)
	if err := processHostReportEvent(event); err != nil {
		t.Fatal(err)
	}
	var lastSeen, metricTimestamp string
	if err := database.QueryRow(`SELECT last_seen FROM hosts WHERE host_id = ?`, report.HostID).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT timestamp FROM metrics WHERE host_id = ? AND name = 'cpu'`, report.HostID).Scan(&metricTimestamp); err != nil {
		t.Fatal(err)
	}
	if lastSeen != event.IngestedAt || metricTimestamp != event.OccurredAt {
		t.Fatalf("unexpected persisted times: lastSeen=%s metric=%s", lastSeen, metricTimestamp)
	}
	state, found := loadHostState(report.HostID)
	if !found || !state.LastSeen.Equal(ingestedAt) {
		t.Fatalf("runtime state was not updated: %+v", state)
	}
}
