package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type ProcessInfo struct {
	PID      int32   `json:"pid"`
	Name     string  `json:"name"`
	CPU      float64 `json:"cpu"`
	MemoryMB float64 `json:"memoryMB"`
}

type Report struct {
	Hostname  string        `json:"hostname"`
	Timestamp int64         `json:"timestamp"`
	CPU       float64       `json:"cpu"`
	Memory    float64       `json:"memory"`
	Disk      float64       `json:"disk"`
	NetUp     float64       `json:"netUp"`
	NetDown   float64       `json:"netDown"`
	Processes []ProcessInfo `json:"processes"`
}

type AlertRecord struct {
	RuleName  string `json:"ruleName"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

type MetricPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

type summaryResponse struct {
	Hostname  string        `json:"hostname"`
	Status    string        `json:"status"`
	LastSeen  string        `json:"lastSeen"`
	CPU       float64       `json:"cpu"`
	Memory    float64       `json:"memory"`
	Disk      float64       `json:"disk"`
	NetUp     float64       `json:"netUp"`
	NetDown   float64       `json:"netDown"`
	Processes []ProcessInfo `json:"processes"`
	Alerts    []AlertRecord `json:"alerts"`
}

var (
	stateMu        sync.RWMutex
	stateHost      string
	stateLastSeen  time.Time
	stateProcesses []ProcessInfo
	stateAlertMap  = map[string]bool{}
	stateDB        *sql.DB
)

func main() {
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatal(err)
	}

	dbPath := filepath.Join("data", "monitor.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	stateDB = db
	defer db.Close()

	if err := initDB(db); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/api/report", handleReport)
	http.HandleFunc("/api/summary", handleSummary)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/processes", handleProcesses)
	http.Handle("/", http.FileServer(http.Dir("web")))

	log.Println("server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func initDB(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS metrics (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp TEXT, name TEXT, value REAL, unit TEXT);`,
		`CREATE TABLE IF NOT EXISTS alerts (id INTEGER PRIMARY KEY AUTOINCREMENT, rule_name TEXT, level TEXT, message TEXT, timestamp TEXT);`,
	}
	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func setNoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func handleReport(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var report Report
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	stateMu.Lock()
	stateHost = report.Hostname
	stateLastSeen = time.Now()
	stateProcesses = report.Processes
	stateMu.Unlock()

	log.Printf("received report host=%s cpu=%.1f memory=%.1f", report.Hostname, report.CPU, report.Memory)

	if err := saveMetrics(report); err != nil {
		log.Printf("save metrics failed: %v", err)
	}
	if err := evaluateAlerts(report); err != nil {
		log.Printf("evaluate alerts failed: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleSummary(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	stateMu.RLock()
	hostname := stateHost
	lastSeen := stateLastSeen
	processes := append([]ProcessInfo(nil), stateProcesses...)
	stateMu.RUnlock()

	var current Report
	current.Hostname = hostname
	current.Processes = processes

	latest, err := getLatestMetrics()
	if err == nil {
		current.CPU = latest["cpu"]
		current.Memory = latest["memory"]
		current.Disk = latest["disk"]
		current.NetUp = latest["net_up"]
		current.NetDown = latest["net_down"]
	}

	status := "offline"
	if !lastSeen.IsZero() && time.Since(lastSeen).Seconds() <= 15 {
		status = "online"
	}

	response := summaryResponse{
		Hostname:  hostname,
		Status:    status,
		CPU:       current.CPU,
		Memory:    current.Memory,
		Disk:      current.Disk,
		NetUp:     current.NetUp,
		NetDown:   current.NetDown,
		Processes: processes,
		Alerts:    currentAlerts(current),
	}
	if !lastSeen.IsZero() {
		response.LastSeen = lastSeen.Format(time.RFC3339)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	window := 1800
	if value := r.URL.Query().Get("window"); value != "" {
		if v, err := strconv.Atoi(value); err == nil && v > 0 {
			window = v
		}
	}

	start := time.Now().Add(-time.Duration(window) * time.Second)
	result := map[string][]MetricPoint{}
	for _, metricName := range []string{"cpu", "memory", "disk", "net_up", "net_down"} {
		points, err := queryHistory(metricName, start)
		if err != nil {
			continue
		}
		result[metricName] = points
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func handleProcesses(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	stateMu.RLock()
	processes := append([]ProcessInfo(nil), stateProcesses...)
	stateMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]ProcessInfo{"processes": processes})
}

func saveMetrics(report Report) error {
	if stateDB == nil {
		return nil
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	metrics := []struct {
		name  string
		value float64
		unit  string
	}{
		{name: "cpu", value: report.CPU, unit: "%"},
		{name: "memory", value: report.Memory, unit: "%"},
		{name: "disk", value: report.Disk, unit: "%"},
		{name: "net_up", value: report.NetUp, unit: "B/s"},
		{name: "net_down", value: report.NetDown, unit: "B/s"},
	}

	for _, item := range metrics {
		if _, err := stateDB.Exec(`INSERT INTO metrics (timestamp, name, value, unit) VALUES (?, ?, ?, ?)`, timestamp, item.name, item.value, item.unit); err != nil {
			return err
		}
	}
	return nil
}

func evaluateAlerts(report Report) error {
	if stateDB == nil {
		return nil
	}

	rules := []struct {
		key     string
		expr    bool
		level   string
		message string
	}{
		{key: "cpu", expr: report.CPU >= 90, level: "warning", message: "CPU usage exceeds 90%"},
		{key: "memory", expr: report.Memory >= 85, level: "warning", message: "Memory usage exceeds 85%"},
		{key: "disk", expr: report.Disk >= 90, level: "critical", message: "Disk usage exceeds 90%"},
	}

	for _, rule := range rules {
		if rule.expr {
			if !stateAlertMap[rule.key] {
				if _, err := stateDB.Exec(`INSERT INTO alerts (rule_name, level, message, timestamp) VALUES (?, ?, ?, ?)`, rule.key, rule.level, rule.message, time.Now().UTC().Format(time.RFC3339)); err != nil {
					return err
				}
				stateAlertMap[rule.key] = true
			}
		} else {
			stateAlertMap[rule.key] = false
		}
	}
	return nil
}

func currentAlerts(report Report) []AlertRecord {
	alerts := []AlertRecord{}
	if report.CPU >= 90 {
		alerts = append(alerts, AlertRecord{RuleName: "cpu", Level: "warning", Message: "CPU usage exceeds 90%", Timestamp: time.Now().UTC().Format(time.RFC3339)})
	}
	if report.Memory >= 85 {
		alerts = append(alerts, AlertRecord{RuleName: "memory", Level: "warning", Message: "Memory usage exceeds 85%", Timestamp: time.Now().UTC().Format(time.RFC3339)})
	}
	if report.Disk >= 90 {
		alerts = append(alerts, AlertRecord{RuleName: "disk", Level: "critical", Message: "Disk usage exceeds 90%", Timestamp: time.Now().UTC().Format(time.RFC3339)})
	}
	return alerts
}

func queryHistory(metricName string, start time.Time) ([]MetricPoint, error) {
	rows, err := stateDB.Query(`SELECT timestamp, value FROM metrics WHERE name = ? AND datetime(timestamp) >= datetime(?) ORDER BY timestamp ASC`, metricName, start.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []MetricPoint{}
	for rows.Next() {
		var ts string
		var value float64
		if err := rows.Scan(&ts, &value); err != nil {
			continue
		}
		result = append(result, MetricPoint{Timestamp: ts, Value: value})
	}
	return result, nil
}

func getLatestMetrics() (map[string]float64, error) {
	result := map[string]float64{"cpu": 0, "memory": 0, "disk": 0, "net_up": 0, "net_down": 0}
	if stateDB == nil {
		return result, nil
	}

	for _, name := range []string{"cpu", "memory", "disk", "net_up", "net_down"} {
		row := stateDB.QueryRow(`SELECT value FROM metrics WHERE name = ? ORDER BY id DESC LIMIT 1`, name)
		var value float64
		if err := row.Scan(&value); err != nil {
			continue
		}
		result[name] = value
	}
	return result, nil
}

func init() {
	stateAlertMap = map[string]bool{}
}
