package main

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

type notificationEvent struct {
	Alert    AlertRecord
	Hostname string
}

var notificationQueue chan notificationEvent

func startNotificationWorker() {
	notificationQueue = make(chan notificationEvent, 64)
	go func() {
		for event := range notificationQueue {
			if err := SendAlertEmail(event.Alert, event.Hostname); err != nil {
				log.Printf("email notification failed: %v", err)
			}
		}
	}()
}

func enqueueNotification(event notificationEvent) {
	if notificationQueue == nil {
		return
	}
	select {
	case notificationQueue <- event:
	default:
		log.Printf("email notification queue is full; dropping %s notification for %s", event.Alert.Status, event.Alert.RuleName)
	}
}

func getFiringAlert(hostID, ruleName string) (AlertRecord, bool) {
	if stateDB == nil {
		return AlertRecord{}, false
	}
	row := stateDB.QueryRow(`SELECT rule_name, level, message, status, current_value, threshold, started_at, resolved_at, updated_at, timestamp FROM alerts WHERE host_id = ? AND rule_name = ? AND status = 'FIRING' ORDER BY id DESC LIMIT 1`, hostID, ruleName)
	var rule, level, message, status, startedAt, resolvedAt, updatedAt, timestamp sql.NullString
	var currentValue, threshold sql.NullFloat64
	if err := row.Scan(&rule, &level, &message, &status, &currentValue, &threshold, &startedAt, &resolvedAt, &updatedAt, &timestamp); err != nil {
		return AlertRecord{}, false
	}
	return AlertRecord{RuleName: rule.String, Level: level.String, Message: message.String, Status: status.String, CurrentValue: currentValue.Float64, Threshold: threshold.Float64, StartedAt: startedAt.String, ResolvedAt: resolvedAt.String, UpdatedAt: updatedAt.String, Timestamp: timestamp.String}, true
}

func SendAlertEmail(alert AlertRecord, hostname string) error {
	settings, err := readEmailSettings()
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return nil
	}
	if settings.SMTPHost == "" || settings.From == "" || len(settings.To) == 0 {
		return fmt.Errorf("email settings are incomplete")
	}
	password := os.Getenv("SMTP_PASSWORD")
	if settings.SMTPUsername != "" && strings.TrimSpace(password) == "" {
		return fmt.Errorf("SMTP_PASSWORD is not configured")
	}

	recipients := append([]string{}, settings.To...)
	recipients = append(recipients, settings.CC...)
	subject, body := alertEmailContent(alert, hostname)
	headers := []string{
		"From: " + settings.From,
		"To: " + strings.Join(settings.To, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	if len(settings.CC) > 0 {
		headers = append(headers, "Cc: "+strings.Join(settings.CC, ", "))
	}
	message := strings.Join(headers, "\r\n") + "\r\n\r\n" + body
	return sendSMTP(settings, password, recipients, []byte(message))
}

func handleTestEmail(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	settings, err := readEmailSettings()
	if err != nil {
		http.Error(w, "failed to read email settings", http.StatusInternalServerError)
		return
	}
	if settings.SMTPHost == "" || settings.SMTPPort < 1 || settings.SMTPPort > 65535 || settings.From == "" || len(settings.To) == 0 {
		http.Error(w, "SMTP host, port, from, and at least one recipient are required", http.StatusBadRequest)
		return
	}
	from, parseErr := mail.ParseAddress(settings.From)
	if parseErr != nil || from.Address != settings.From {
		http.Error(w, "invalid from address", http.StatusBadRequest)
		return
	}
	password := os.Getenv("SMTP_PASSWORD")
	if settings.SMTPUsername != "" && strings.TrimSpace(password) == "" {
		http.Error(w, "SMTP_PASSWORD is not configured", http.StatusBadRequest)
		return
	}
	recipients := append([]string{}, settings.To...)
	recipients = append(recipients, settings.CC...)
	message := strings.Join([]string{
		"From: " + settings.From,
		"To: " + strings.Join(settings.To, ", "),
		"Subject: [Stark monitor] Test Email",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"", "This is a test email from Stark monitor.",
	}, "\r\n")
	if len(settings.CC) > 0 {
		message = strings.Replace(message, "Subject: [Stark monitor] Test Email\r\n", "Subject: [Stark monitor] Test Email\r\nCc: "+strings.Join(settings.CC, ", ")+"\r\n", 1)
	}
	if err := sendSMTP(settings, password, recipients, []byte(message)); err != nil {
		log.Printf("test email SMTP send failed: %v", err)
		http.Error(w, "failed to send test email", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func sendSMTP(settings EmailSettings, password string, recipients []string, message []byte) error {
	address := net.JoinHostPort(settings.SMTPHost, strconv.Itoa(settings.SMTPPort))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var connection net.Conn
	var err error
	if settings.SMTPPort == 465 {
		connection, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: settings.SMTPHost, MinVersion: tls.VersionTLS12})
	} else {
		connection, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(15 * time.Second))
	client, err := smtp.NewClient(connection, settings.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if settings.SMTPPort != 465 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: settings.SMTPHost, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if settings.SMTPUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", settings.SMTPUsername, password, settings.SMTPHost)); err != nil {
			return err
		}
	}
	if err := client.Mail(settings.From); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func readEmailSettings() (EmailSettings, error) {
	settings, err := readSettingsResponse()
	if err != nil {
		return EmailSettings{}, err
	}
	return settings.Notifications.Email, nil
}

func alertEmailContent(alert AlertRecord, hostname string) (string, string) {
	level := strings.ToUpper(alert.Level)
	rule := formatAlertRuleNameForEmail(alert.RuleName)
	if alert.Status == "RESOLVED" {
		return "[Stark monitor][RESOLVED] " + rule, fmt.Sprintf("Stark monitor Alert\n\nStatus: RESOLVED\n\nHost: %s\nRule: %s\nCurrent: %.1f%%\nDuration: %s\n", hostname, rule, alert.CurrentValue, formatAlertDuration(alert.StartedAt, alert.ResolvedAt))
	}
	return fmt.Sprintf("[Stark monitor][%s] %s Alert", level, rule), fmt.Sprintf("Stark monitor Alert\n\nStatus: FIRING\n\nHost: %s\nRule: %s\nCurrent: %.1f%%\nThreshold: %.1f%%\nStarted: %s\n", hostname, rule, alert.CurrentValue, alert.Threshold, alert.StartedAt)
}

func formatAlertRuleNameForEmail(rule string) string {
	switch strings.ToLower(rule) {
	case "cpu":
		return "CPU Usage"
	case "memory":
		return "Memory Usage"
	case "disk":
		return "Disk Usage"
	default:
		return rule
	}
}

func formatAlertDuration(startedAt, resolvedAt string) string {
	start, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return "-"
	}
	end := backendStartTime
	if resolvedAt != "" {
		end, err = time.Parse(time.RFC3339, resolvedAt)
		if err != nil {
			return "-"
		}
	} else {
		end = time.Now().UTC()
	}
	seconds := int(end.Sub(start).Seconds())
	if seconds < 0 {
		return "-"
	}
	minutes := seconds / 60
	if minutes < 1 {
		return fmt.Sprintf("%ds", seconds)
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dh %dm", minutes/60, minutes%60)
}
