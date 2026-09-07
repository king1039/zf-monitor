package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"
)

type AlertRuleConfig struct {
	Enabled   bool    `json:"enabled"`
	Threshold float64 `json:"threshold"`
	Level     string  `json:"level"`
}

type AlertRulesConfig struct {
	CPU    AlertRuleConfig `json:"cpu"`
	Memory AlertRuleConfig `json:"memory"`
	Disk   AlertRuleConfig `json:"disk"`
}

type EmailSettings struct {
	Enabled                bool     `json:"enabled"`
	SMTPHost               string   `json:"smtpHost"`
	SMTPPort               int      `json:"smtpPort"`
	SMTPUsername           string   `json:"smtpUsername"`
	From                   string   `json:"from"`
	To                     []string `json:"to"`
	CC                     []string `json:"cc"`
	SMTPPasswordConfigured bool     `json:"smtpPasswordConfigured"`
}

type SettingsResponse struct {
	AlertRules    AlertRulesConfig `json:"alertRules"`
	Notifications struct {
		Email EmailSettings `json:"email"`
	} `json:"notifications"`
	Platform struct {
		Name string `json:"name"`
	} `json:"platform"`
}

type alertRulesRequest struct {
	CPU    AlertRuleConfig `json:"cpu"`
	Memory AlertRuleConfig `json:"memory"`
	Disk   AlertRuleConfig `json:"disk"`
}

type notificationsRequest struct {
	Email EmailSettings `json:"email"`
}

type platformRequest struct {
	Name string `json:"name"`
}

var backendStartTime = time.Now()

var defaultSettings = map[string]string{
	"alert.cpu.enabled":                "true",
	"alert.cpu.threshold":              "90",
	"alert.cpu.level":                  "warning",
	"alert.memory.enabled":             "true",
	"alert.memory.threshold":           "85",
	"alert.memory.level":               "warning",
	"alert.disk.enabled":               "true",
	"alert.disk.threshold":             "90",
	"alert.disk.level":                 "critical",
	"notification.email.enabled":       "false",
	"notification.email.smtp_host":     "",
	"notification.email.smtp_port":     "587",
	"notification.email.smtp_username": "",
	"notification.email.from":          "",
	"notification.email.to":            "",
	"notification.email.cc":            "",
	"platform.name":                    "Stark monitor",
}

func initSettings(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range defaultSettings {
		if _, err := db.Exec(`INSERT OR IGNORE INTO settings (key, value, updated_at) VALUES (?, ?, ?)`, key, value, now); err != nil {
			return err
		}
	}
	legacyPlatformName := strings.Join([]string{"ZF", "Monitor"}, " ")
	if _, err := db.Exec(`UPDATE settings SET value = 'Stark monitor', updated_at = ? WHERE key = 'platform.name' AND value = ?`, now, legacyPlatformName); err != nil {
		return err
	}
	return nil
}

func getSetting(key string) (string, error) {
	if stateDB == nil {
		return "", fmt.Errorf("database unavailable")
	}
	var value string
	err := stateDB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	return value, err
}

func loadAlertRules() (AlertRulesConfig, error) {
	readRule := func(name string) (AlertRuleConfig, error) {
		enabled, err := getSetting("alert." + name + ".enabled")
		if err != nil {
			return AlertRuleConfig{}, err
		}
		threshold, err := getSetting("alert." + name + ".threshold")
		if err != nil {
			return AlertRuleConfig{}, err
		}
		level, err := getSetting("alert." + name + ".level")
		if err != nil {
			return AlertRuleConfig{}, err
		}
		parsedThreshold, err := strconv.ParseFloat(threshold, 64)
		if err != nil {
			return AlertRuleConfig{}, err
		}
		parsedEnabled, err := strconv.ParseBool(enabled)
		if err != nil {
			return AlertRuleConfig{}, err
		}
		return AlertRuleConfig{Enabled: parsedEnabled, Threshold: parsedThreshold, Level: level}, nil
	}
	cpu, err := readRule("cpu")
	if err != nil {
		return AlertRulesConfig{}, err
	}
	memory, err := readRule("memory")
	if err != nil {
		return AlertRulesConfig{}, err
	}
	disk, err := readRule("disk")
	if err != nil {
		return AlertRulesConfig{}, err
	}
	return AlertRulesConfig{CPU: cpu, Memory: memory, Disk: disk}, nil
}

func splitRecipients(value string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" && !seen[strings.ToLower(item)] {
			seen[strings.ToLower(item)] = true
			result = append(result, item)
		}
	}
	return result
}

func parseRecipients(values []string) ([]string, error) {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			parsed, err := mail.ParseAddress(item)
			if err != nil || parsed.Address != item {
				return nil, fmt.Errorf("invalid email address: %s", item)
			}
			key := strings.ToLower(item)
			if !seen[key] {
				seen[key] = true
				result = append(result, item)
			}
		}
	}
	return result, nil
}

func validateRule(rule AlertRuleConfig) error {
	if rule.Threshold <= 0 || rule.Threshold > 100 {
		return fmt.Errorf("threshold must be greater than 0 and at most 100")
	}
	level := strings.ToLower(strings.TrimSpace(rule.Level))
	if level != "warning" && level != "critical" {
		return fmt.Errorf("level must be warning or critical")
	}
	return nil
}

func saveSetting(key, value string) error {
	_, err := stateDB.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, time.Now().UTC().Format(time.RFC3339))
	return err
}

func saveSettings(values map[string]string) error {
	tx, err := stateDB.Begin()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range values {
		if _, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, now); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readSettingsResponse() (SettingsResponse, error) {
	rules, err := loadAlertRules()
	if err != nil {
		return SettingsResponse{}, err
	}
	get := func(key string) string { value, _ := getSetting(key); return value }
	port, _ := strconv.Atoi(get("notification.email.smtp_port"))
	if port == 0 {
		port = 587
	}
	var response SettingsResponse
	response.AlertRules = rules
	response.Notifications.Email = EmailSettings{
		Enabled:  get("notification.email.enabled") == "true",
		SMTPHost: get("notification.email.smtp_host"), SMTPPort: port,
		SMTPUsername: get("notification.email.smtp_username"), From: get("notification.email.from"),
		To: splitRecipients(get("notification.email.to")), CC: splitRecipients(get("notification.email.cc")),
		SMTPPasswordConfigured: strings.TrimSpace(os.Getenv("SMTP_PASSWORD")) != "",
	}
	response.Platform.Name = get("platform.name")
	return response, nil
}

func writeJSON(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response, err := readSettingsResponse()
	if err != nil {
		http.Error(w, "failed to read settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func decodeJSON(r *http.Request, target interface{}) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func handleAlertRulesSettings(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request alertRulesRequest
	if err := decodeJSON(r, &request); err != nil || validateRule(request.CPU) != nil || validateRule(request.Memory) != nil || validateRule(request.Disk) != nil {
		http.Error(w, "invalid alert rules", http.StatusBadRequest)
		return
	}
	values := map[string]string{}
	for name, rule := range map[string]AlertRuleConfig{"cpu": request.CPU, "memory": request.Memory, "disk": request.Disk} {
		values["alert."+name+".enabled"] = strconv.FormatBool(rule.Enabled)
		values["alert."+name+".threshold"] = strconv.FormatFloat(rule.Threshold, 'f', -1, 64)
		values["alert."+name+".level"] = strings.ToLower(strings.TrimSpace(rule.Level))
	}
	if err := saveSettings(values); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}
	response, err := readSettingsResponse()
	if err != nil {
		http.Error(w, "failed to read settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func handleNotificationSettings(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request notificationsRequest
	if err := decodeJSON(r, &request); err != nil {
		http.Error(w, "invalid notification settings", http.StatusBadRequest)
		return
	}
	email := request.Email
	if email.SMTPPort < 1 || email.SMTPPort > 65535 {
		http.Error(w, "invalid SMTP port", http.StatusBadRequest)
		return
	}
	to, err := parseRecipients(email.To)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cc, err := parseRecipients(email.CC)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if email.Enabled {
		if strings.TrimSpace(email.SMTPHost) == "" || strings.TrimSpace(email.From) == "" || len(to) == 0 {
			http.Error(w, "SMTP host, from, and at least one recipient are required", http.StatusBadRequest)
			return
		}
		from, parseErr := mail.ParseAddress(strings.TrimSpace(email.From))
		if parseErr != nil || from.Address != strings.TrimSpace(email.From) {
			http.Error(w, "invalid from address", http.StatusBadRequest)
			return
		}
	}
	values := map[string]string{"notification.email.enabled": strconv.FormatBool(email.Enabled), "notification.email.smtp_host": strings.TrimSpace(email.SMTPHost), "notification.email.smtp_port": strconv.Itoa(email.SMTPPort), "notification.email.smtp_username": strings.TrimSpace(email.SMTPUsername), "notification.email.from": strings.TrimSpace(email.From), "notification.email.to": strings.Join(to, ","), "notification.email.cc": strings.Join(cc, ",")}
	if err := saveSettings(values); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}
	response, err := readSettingsResponse()
	if err != nil {
		http.Error(w, "failed to read settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func handlePlatformSettings(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request platformRequest
	if err := decodeJSON(r, &request); err != nil {
		http.Error(w, "invalid platform settings", http.StatusBadRequest)
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len([]rune(request.Name)) > 64 {
		http.Error(w, "invalid platform name", http.StatusBadRequest)
		return
	}
	if err := saveSettings(map[string]string{"platform.name": request.Name}); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"name": request.Name})
}

func handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var pageCount, pageSize int64
	if stateDB == nil || stateDB.QueryRow(`PRAGMA page_count`).Scan(&pageCount) != nil || stateDB.QueryRow(`PRAGMA page_size`).Scan(&pageSize) != nil {
		http.Error(w, "failed to read system status", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"backendStatus": "healthy", "database": "SQLite", "databaseSizeBytes": pageCount * pageSize, "version": "dev", "uptimeSeconds": int64(time.Since(backendStartTime).Seconds())})
}
