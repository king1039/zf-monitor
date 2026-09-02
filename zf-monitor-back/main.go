package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	HostID    string        `json:"hostId"`
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

type HostRuntimeState struct {
	Hostname  string
	LastSeen  time.Time
	Processes []ProcessInfo
}

type HostListItem struct {
	HostID   string `json:"hostId"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	LastSeen string `json:"lastSeen"`
}

type summaryResponse struct {
	HostID    string        `json:"hostId"`
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

type DatabaseReport struct {
	InstanceID          string  `json:"instanceId"`
	Name                string  `json:"name"`
	Type                string  `json:"type"`
	Host                string  `json:"host"`
	Port                int     `json:"port"`
	Status              string  `json:"status"`
	ServerName          string  `json:"serverName,omitempty"`
	Version             string  `json:"version,omitempty"`
	ProductLevel        string  `json:"productLevel,omitempty"`
	Edition             string  `json:"edition,omitempty"`
	Timestamp           string  `json:"timestamp,omitempty"`
	Error               string  `json:"error,omitempty"`
	UptimeSeconds       float64 `json:"uptimeSeconds"`
	Connections         float64 `json:"connections"`
	MaxConnections      float64 `json:"maxConnections"`
	ActiveSessions      float64 `json:"activeSessions"`
	RunningRequests     float64 `json:"runningRequests"`
	DatabaseCount       float64 `json:"databaseCount"`
	TotalDatabaseSizeMB float64 `json:"totalDatabaseSizeMB"`
}

type DatabaseInstanceItem struct {
	InstanceID string `json:"instanceId"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	ServerName string `json:"serverName,omitempty"`
	Status     string `json:"status"`
	LastSeen   string `json:"lastSeen,omitempty"`
}

type DatabaseSummaryResponse struct {
	InstanceID          string  `json:"instanceId"`
	Name                string  `json:"name"`
	Type                string  `json:"type"`
	Host                string  `json:"host"`
	Port                int     `json:"port"`
	Status              string  `json:"status"`
	ServerName          string  `json:"serverName,omitempty"`
	Version             string  `json:"version,omitempty"`
	ProductLevel        string  `json:"productLevel,omitempty"`
	Edition             string  `json:"edition,omitempty"`
	LastSeen            string  `json:"lastSeen,omitempty"`
	UptimeSeconds       float64 `json:"uptimeSeconds"`
	Connections         float64 `json:"connections"`
	MaxConnections      float64 `json:"maxConnections"`
	ActiveSessions      float64 `json:"activeSessions"`
	RunningRequests     float64 `json:"runningRequests"`
	DatabaseCount       float64 `json:"databaseCount"`
	TotalDatabaseSizeMB float64 `json:"totalDatabaseSizeMB"`
}

var (
	stateMu       sync.RWMutex
	alertMu       sync.Mutex
	hostStates    = map[string]*HostRuntimeState{}
	stateAlertMap = map[string]map[string]bool{}
	stateDB       *sql.DB
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

	http.HandleFunc("/api/hosts", handleHosts)
	http.HandleFunc("/api/report", handleReport)
	http.HandleFunc("/api/summary", handleSummary)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/processes", handleProcesses)
	http.HandleFunc("/api/database/report", handleDatabaseReport)
	http.HandleFunc("/api/databases", handleDatabases)
	http.HandleFunc("/api/database/summary", handleDatabaseSummary)
	http.Handle("/", http.FileServer(http.Dir("web")))

	log.Println("server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func initDB(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS hosts (host_id TEXT PRIMARY KEY, hostname TEXT, last_seen TEXT);`,
		`CREATE TABLE IF NOT EXISTS metrics (id INTEGER PRIMARY KEY AUTOINCREMENT, host_id TEXT NOT NULL, timestamp TEXT, name TEXT, value REAL, unit TEXT);`,
		`CREATE TABLE IF NOT EXISTS alerts (id INTEGER PRIMARY KEY AUTOINCREMENT, host_id TEXT NOT NULL, rule_name TEXT, level TEXT, message TEXT, timestamp TEXT);`,
		`CREATE TABLE IF NOT EXISTS database_instances (instance_id TEXT PRIMARY KEY, name TEXT, db_type TEXT, host TEXT, port INTEGER, server_name TEXT, version TEXT, product_level TEXT, edition TEXT, status TEXT, last_seen DATETIME, last_error TEXT);`,
		`CREATE TABLE IF NOT EXISTS database_metrics (id INTEGER PRIMARY KEY AUTOINCREMENT, instance_id TEXT NOT NULL, timestamp DATETIME NOT NULL, uptime_seconds REAL, connections REAL, max_connections REAL, active_sessions REAL, running_requests REAL, database_count REAL, total_database_size_mb REAL);`,
		`CREATE INDEX IF NOT EXISTS idx_database_metrics_instance_timestamp ON database_metrics(instance_id, timestamp);`,
	}
	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}

	if err := ensureColumn(db, "metrics", "host_id"); err != nil {
		return err
	}
	if err := ensureColumn(db, "alerts", "host_id"); err != nil {
		return err
	}
	return nil
}

func ensureColumn(db *sql.DB, tableName, columnName string) error {
	rows, err := db.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType, notNull, dfltValue, pk interface{}
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return err
		}
		if name == columnName {
			return nil
		}
	}
	_, err = db.Exec("ALTER TABLE " + tableName + " ADD COLUMN " + columnName + " TEXT")
	return err
}

func setNoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func normalizeHostID(hostID, hostname string) string {
	if hostID != "" {
		return hostID
	}
	if hostname != "" {
		return hostname
	}
	return "unknown-host"
}

func saveHostRecord(hostID, hostname string, lastSeen time.Time) error {
	if stateDB == nil {
		return nil
	}
	_, err := stateDB.Exec(`INSERT INTO hosts (host_id, hostname, last_seen) VALUES (?, ?, ?) ON CONFLICT(host_id) DO UPDATE SET hostname = excluded.hostname, last_seen = excluded.last_seen`, hostID, hostname, lastSeen.UTC().Format(time.RFC3339))
	return err
}

func handleHosts(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hosts, err := listHosts()
	if err != nil {
		http.Error(w, "failed to read hosts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hosts)
}

func listHosts() ([]HostListItem, error) {
	if stateDB == nil {
		return nil, nil
	}
	rows, err := stateDB.Query(`SELECT host_id, hostname, last_seen FROM hosts ORDER BY hostname ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []HostListItem{}
	for rows.Next() {
		var hostID, hostname, lastSeen string
		if err := rows.Scan(&hostID, &hostname, &lastSeen); err != nil {
			return nil, err
		}
		status := "offline"
		if ts, err := time.Parse(time.RFC3339, lastSeen); err == nil && time.Since(ts).Seconds() <= 15 {
			status = "online"
		}
		items = append(items, HostListItem{HostID: hostID, Hostname: hostname, Status: status, LastSeen: lastSeen})
	}
	return items, nil
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

	report.HostID = normalizeHostID(report.HostID, report.Hostname)
	lastSeen := time.Now()
	if err := saveHostRecord(report.HostID, report.Hostname, lastSeen); err != nil {
		log.Printf("save host record failed: %v", err)
	}

	stateMu.Lock()
	stateHost := hostStates[report.HostID]
	if stateHost == nil {
		stateHost = &HostRuntimeState{}
	}
	stateHost.Hostname = report.Hostname
	stateHost.LastSeen = lastSeen
	stateHost.Processes = append([]ProcessInfo(nil), report.Processes...)
	hostStates[report.HostID] = stateHost
	stateMu.Unlock()

	log.Printf("received report hostId=%s host=%s cpu=%.1f memory=%.1f", report.HostID, report.Hostname, report.CPU, report.Memory)

	if err := saveMetrics(report); err != nil {
		log.Printf("save metrics failed: %v", err)
	}
	if err := evaluateAlerts(report); err != nil {
		log.Printf("evaluate alerts failed: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func loadHostState(hostID string) (HostRuntimeState, bool) {
	stateMu.RLock()
	state, ok := hostStates[hostID]
	stateMu.RUnlock()
	if ok && state != nil {
		return *state, true
	}
	if stateDB == nil {
		return HostRuntimeState{}, false
	}
	row := stateDB.QueryRow(`SELECT hostname, last_seen FROM hosts WHERE host_id = ?`, hostID)
	var hostname, lastSeen string
	if err := row.Scan(&hostname, &lastSeen); err != nil {
		return HostRuntimeState{}, false
	}
	parsed, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		parsed = time.Now()
	}
	return HostRuntimeState{Hostname: hostname, LastSeen: parsed}, true
}

func handleSummary(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	hostID := r.URL.Query().Get("hostId")
	if hostID == "" {
		http.Error(w, "hostId is required", http.StatusBadRequest)
		return
	}

	state, ok := loadHostState(hostID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	latest, err := getLatestMetricsForHost(hostID)
	if err != nil {
		latest = map[string]float64{"cpu": 0, "memory": 0, "disk": 0, "net_up": 0, "net_down": 0}
	}

	status := "offline"
	if !state.LastSeen.IsZero() && time.Since(state.LastSeen).Seconds() <= 15 {
		status = "online"
	}

	response := summaryResponse{
		HostID:    hostID,
		Hostname:  state.Hostname,
		Status:    status,
		CPU:       latest["cpu"],
		Memory:    latest["memory"],
		Disk:      latest["disk"],
		NetUp:     latest["net_up"],
		NetDown:   latest["net_down"],
		Processes: append([]ProcessInfo(nil), state.Processes...),
		Alerts:    getRecentAlerts(hostID),
	}
	if !state.LastSeen.IsZero() {
		response.LastSeen = state.LastSeen.Format(time.RFC3339)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	hostID := r.URL.Query().Get("hostId")
	if hostID == "" {
		http.Error(w, "hostId is required", http.StatusBadRequest)
		return
	}
	if _, ok := loadHostState(hostID); !ok {
		http.NotFound(w, r)
		return
	}

	window := 1800
	if value := r.URL.Query().Get("window"); value != "" {
		if v, err := strconv.Atoi(value); err == nil && v > 0 {
			window = v
		}
	}

	start := time.Now().Add(-time.Duration(window) * time.Second)
	result := map[string][]MetricPoint{}
	for _, metricName := range []string{"cpu", "memory", "disk", "net_up", "net_down"} {
		points, err := queryHistory(hostID, metricName, start)
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
	hostID := r.URL.Query().Get("hostId")
	if hostID == "" {
		http.Error(w, "hostId is required", http.StatusBadRequest)
		return
	}
	state, ok := loadHostState(hostID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]ProcessInfo{"processes": append([]ProcessInfo(nil), state.Processes...)})
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
		if _, err := stateDB.Exec(`INSERT INTO metrics (host_id, timestamp, name, value, unit) VALUES (?, ?, ?, ?, ?)`, report.HostID, timestamp, item.name, item.value, item.unit); err != nil {
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

	triggered := make([]struct {
		key     string
		level   string
		message string
	}, 0, len(rules))

	alertMu.Lock()
	if _, ok := stateAlertMap[report.HostID]; !ok {
		stateAlertMap[report.HostID] = map[string]bool{}
	}

	for _, rule := range rules {
		current, exists := stateAlertMap[report.HostID][rule.key]
		if rule.expr {
			if !exists || !current {
				stateAlertMap[report.HostID][rule.key] = true
				triggered = append(triggered, struct {
					key     string
					level   string
					message string
				}{key: rule.key, level: rule.level, message: rule.message})
			}
		} else {
			if exists && current {
				stateAlertMap[report.HostID][rule.key] = false
			}
		}
	}
	alertMu.Unlock()

	for _, alert := range triggered {
		if _, err := stateDB.Exec(`INSERT INTO alerts (host_id, rule_name, level, message, timestamp) VALUES (?, ?, ?, ?, ?)`, report.HostID, alert.key, alert.level, alert.message, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

func getRecentAlerts(hostID string) []AlertRecord {
	if stateDB == nil {
		return nil
	}
	rows, err := stateDB.Query(`SELECT rule_name, level, message, timestamp FROM alerts WHERE host_id = ? ORDER BY id DESC LIMIT 20`, hostID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := []AlertRecord{}
	for rows.Next() {
		var ruleName, level, message, ts string
		if err := rows.Scan(&ruleName, &level, &message, &ts); err != nil {
			continue
		}
		result = append(result, AlertRecord{RuleName: ruleName, Level: level, Message: message, Timestamp: ts})
	}
	return result
}

func queryHistory(hostID, metricName string, start time.Time) ([]MetricPoint, error) {
	if stateDB == nil {
		return nil, nil
	}
	rows, err := stateDB.Query(`SELECT timestamp, value FROM metrics WHERE host_id = ? AND name = ? AND datetime(timestamp) >= datetime(?) ORDER BY timestamp ASC`, hostID, metricName, start.Format(time.RFC3339))
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

func getLatestMetricsForHost(hostID string) (map[string]float64, error) {
	result := map[string]float64{"cpu": 0, "memory": 0, "disk": 0, "net_up": 0, "net_down": 0}
	if stateDB == nil {
		return result, nil
	}

	for _, name := range []string{"cpu", "memory", "disk", "net_up", "net_down"} {
		row := stateDB.QueryRow(`SELECT value FROM metrics WHERE host_id = ? AND name = ? ORDER BY id DESC LIMIT 1`, hostID, name)
		var value float64
		if err := row.Scan(&value); err != nil {
			continue
		}
		result[name] = value
	}
	return result, nil
}

func databaseStatusFromTimestamp(lastSeen string) string {
	if strings.TrimSpace(lastSeen) == "" {
		return "offline"
	}

	seen, err := time.Parse(time.RFC3339, lastSeen)
	if err != nil {
		return "offline"
	}
	if time.Since(seen).Seconds() > 30 {
		return "offline"
	}
	return "online"
}

func handleDatabaseReport(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var report DatabaseReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(report.InstanceID) == "" {
		http.Error(w, "instanceId is required", http.StatusBadRequest)
		return
	}

	if err := saveDatabaseReport(report); err != nil {
		log.Printf("save database report failed: %v", err)
		http.Error(w, "failed to store database report", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func saveDatabaseReport(report DatabaseReport) error {
	if stateDB == nil {
		return nil
	}

	lastSeen := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(report.Timestamp) != "" {
		if parsed, err := time.Parse(time.RFC3339, report.Timestamp); err == nil {
			lastSeen = parsed.UTC().Format(time.RFC3339)
		}
	}

	status := strings.TrimSpace(report.Status)
	if status == "" {
		status = "offline"
	}

	_, err := stateDB.Exec(`INSERT INTO database_instances (instance_id, name, db_type, host, port, server_name, version, product_level, edition, status, last_seen, last_error) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(instance_id) DO UPDATE SET name = excluded.name, db_type = excluded.db_type, host = excluded.host, port = excluded.port, server_name = CASE WHEN excluded.server_name <> '' THEN excluded.server_name ELSE database_instances.server_name END, version = CASE WHEN excluded.version <> '' THEN excluded.version ELSE database_instances.version END, product_level = CASE WHEN excluded.product_level <> '' THEN excluded.product_level ELSE database_instances.product_level END, edition = CASE WHEN excluded.edition <> '' THEN excluded.edition ELSE database_instances.edition END, status = excluded.status, last_seen = excluded.last_seen, last_error = excluded.last_error`,
		report.InstanceID,
		report.Name,
		report.Type,
		report.Host,
		report.Port,
		report.ServerName,
		report.Version,
		report.ProductLevel,
		report.Edition,
		status,
		lastSeen,
		report.Error,
	)
	if err != nil {
		return err
	}

	_, err = stateDB.Exec(`INSERT INTO database_metrics (instance_id, timestamp, uptime_seconds, connections, max_connections, active_sessions, running_requests, database_count, total_database_size_mb) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.InstanceID,
		lastSeen,
		report.UptimeSeconds,
		report.Connections,
		report.MaxConnections,
		report.ActiveSessions,
		report.RunningRequests,
		report.DatabaseCount,
		report.TotalDatabaseSizeMB,
	)
	return err
}

func handleDatabases(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	items, err := listDatabaseInstances()
	if err != nil {
		http.Error(w, "failed to read database instances", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func listDatabaseInstances() ([]DatabaseInstanceItem, error) {
	if stateDB == nil {
		return nil, nil
	}

	rows, err := stateDB.Query(`SELECT instance_id, name, db_type, host, port, server_name, status, last_seen FROM database_instances ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []DatabaseInstanceItem{}
	for rows.Next() {
		var instanceID, name, dbType, host, serverName, status, lastSeen string
		var port int
		if err := rows.Scan(&instanceID, &name, &dbType, &host, &port, &serverName, &status, &lastSeen); err != nil {
			return nil, err
		}

		effectiveStatus := status
		if !strings.EqualFold(effectiveStatus, "offline") {
			effectiveStatus = databaseStatusFromTimestamp(lastSeen)
		}
		items = append(items, DatabaseInstanceItem{
			InstanceID: instanceID,
			Name:       name,
			Type:       dbType,
			Host:       host,
			Port:       port,
			ServerName: serverName,
			Status:     effectiveStatus,
			LastSeen:   lastSeen,
		})
	}
	return items, nil
}

func handleDatabaseSummary(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	instanceID := strings.TrimSpace(r.URL.Query().Get("instanceId"))
	if instanceID == "" {
		http.Error(w, "instanceId is required", http.StatusBadRequest)
		return
	}

	row := stateDB.QueryRow(`SELECT instance_id, name, db_type, host, port, server_name, version, product_level, edition, status, last_seen FROM database_instances WHERE instance_id = ?`, instanceID)

	var (
		instanceIDValue, name, dbType, host, serverName, version, productLevel, edition, status, lastSeen string
		port                                                                                              int
	)
	if err := row.Scan(&instanceIDValue, &name, &dbType, &host, &port, &serverName, &version, &productLevel, &edition, &status, &lastSeen); err != nil {
		http.NotFound(w, r)
		return
	}

	metricRow := stateDB.QueryRow(`SELECT uptime_seconds, connections, max_connections, active_sessions, running_requests, database_count, total_database_size_mb FROM database_metrics WHERE instance_id = ? ORDER BY id DESC LIMIT 1`, instanceID)
	var uptimeSeconds, connections, maxConnections, activeSessions, runningRequests, databaseCount, totalDatabaseSizeMB float64
	if err := metricRow.Scan(&uptimeSeconds, &connections, &maxConnections, &activeSessions, &runningRequests, &databaseCount, &totalDatabaseSizeMB); err != nil {
		uptimeSeconds, connections, maxConnections, activeSessions, runningRequests, databaseCount, totalDatabaseSizeMB = 0, 0, 0, 0, 0, 0, 0
	}

	effectiveStatus := strings.TrimSpace(status)
	if effectiveStatus == "" || !strings.EqualFold(effectiveStatus, "offline") {
		effectiveStatus = databaseStatusFromTimestamp(lastSeen)
	}

	response := DatabaseSummaryResponse{
		InstanceID:          instanceIDValue,
		Name:                name,
		Type:                dbType,
		Host:                host,
		Port:                port,
		Status:              effectiveStatus,
		ServerName:          serverName,
		Version:             version,
		ProductLevel:        productLevel,
		Edition:             edition,
		LastSeen:            lastSeen,
		UptimeSeconds:       uptimeSeconds,
		Connections:         connections,
		MaxConnections:      maxConnections,
		ActiveSessions:      activeSessions,
		RunningRequests:     runningRequests,
		DatabaseCount:       databaseCount,
		TotalDatabaseSizeMB: totalDatabaseSizeMB,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func init() {
	hostStates = map[string]*HostRuntimeState{}
	stateAlertMap = map[string]map[string]bool{}
}
