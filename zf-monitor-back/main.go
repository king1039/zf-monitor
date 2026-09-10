package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	RuleName     string  `json:"ruleName"`
	Level        string  `json:"level"`
	Message      string  `json:"message"`
	Status       string  `json:"status"`
	CurrentValue float64 `json:"currentValue"`
	Threshold    float64 `json:"threshold"`
	StartedAt    string  `json:"startedAt"`
	ResolvedAt   string  `json:"resolvedAt"`
	UpdatedAt    string  `json:"updatedAt"`
	Timestamp    string  `json:"timestamp"`
}

type AlertListItem struct {
	ID           int64   `json:"id"`
	HostID       string  `json:"hostId"`
	Hostname     string  `json:"hostname"`
	RuleName     string  `json:"ruleName"`
	Level        string  `json:"level"`
	Message      string  `json:"message"`
	Status       string  `json:"status"`
	CurrentValue float64 `json:"currentValue"`
	Threshold    float64 `json:"threshold"`
	StartedAt    string  `json:"startedAt"`
	ResolvedAt   string  `json:"resolvedAt"`
	UpdatedAt    string  `json:"updatedAt"`
	Timestamp    string  `json:"timestamp"`
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
	stateMu    sync.RWMutex
	hostStates = map[string]*HostRuntimeState{}
	stateDB    *sql.DB
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

	if err := initDB(db); err != nil {
		_ = db.Close()
		log.Fatal(err)
	}
	initRedis()
	initKafka()
	startNotificationWorker()
	consumerContext, stopConsumer := context.WithCancel(context.Background())
	consumerDone := startKafkaConsumer(consumerContext)

	http.HandleFunc("/api/hosts", handleHosts)
	http.HandleFunc("/api/report", handleReport)
	http.HandleFunc("/api/summary", handleSummary)
	http.HandleFunc("/api/redis/status", handleRedisStatus)
	http.HandleFunc("/api/kafka/status", handleKafkaStatus)
	http.HandleFunc("/api/redis/host/latest", handleRedisHostLatest)
	http.HandleFunc("/api/history", handleHistory)
	http.HandleFunc("/api/processes", handleProcesses)
	http.HandleFunc("/api/alerts", handleAlerts)
	http.HandleFunc("/api/settings", handleSettings)
	http.HandleFunc("/api/settings/alert-rules", handleAlertRulesSettings)
	http.HandleFunc("/api/settings/notifications", handleNotificationSettings)
	http.HandleFunc("/api/settings/notifications/test", handleTestEmail)
	http.HandleFunc("/api/settings/platform", handlePlatformSettings)
	http.HandleFunc("/api/settings/system", handleSystemSettings)
	http.HandleFunc("/api/database/report", handleDatabaseReport)
	http.HandleFunc("/api/databases", handleDatabases)
	http.HandleFunc("/api/database/summary", handleDatabaseSummary)
	http.Handle("/", http.FileServer(http.Dir("web")))

	server := &http.Server{Addr: ":8080"}
	serverContext, stopServer := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopServer()

	serverErr := make(chan error, 1)
	go func() {
		log.Println("server listening on :8080")
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-serverContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			log.Printf("http server shutdown failed: %v", err)
		}
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("http server failed: %v", err)
		}
	}
	stopConsumer()
	closeKafkaReader()
	<-consumerDone
	closeKafka()
	closeRedis()
	if err := db.Close(); err != nil {
		log.Printf("database close failed: %v", err)
	}
}

func initDB(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS hosts (host_id TEXT PRIMARY KEY, hostname TEXT, last_seen TEXT);`,
		`CREATE TABLE IF NOT EXISTS metrics (id INTEGER PRIMARY KEY AUTOINCREMENT, host_id TEXT NOT NULL, timestamp TEXT, name TEXT, value REAL, unit TEXT);`,
		`CREATE TABLE IF NOT EXISTS alerts (id INTEGER PRIMARY KEY AUTOINCREMENT, host_id TEXT NOT NULL, rule_name TEXT, level TEXT, message TEXT, timestamp TEXT, status TEXT, current_value REAL, threshold REAL, started_at TEXT, resolved_at TEXT, updated_at TEXT);`,
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
	for _, column := range []struct {
		name     string
		typeName string
	}{
		{name: "status", typeName: "TEXT"},
		{name: "current_value", typeName: "REAL"},
		{name: "threshold", typeName: "REAL"},
		{name: "started_at", typeName: "TEXT"},
		{name: "resolved_at", typeName: "TEXT"},
		{name: "updated_at", typeName: "TEXT"},
	} {
		if err := ensureColumnType(db, "alerts", column.name, column.typeName); err != nil {
			return err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE alerts SET status = 'RESOLVED' WHERE status IS NULL OR TRIM(status) = ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE alerts
		SET status = 'RESOLVED', resolved_at = ?, updated_at = ?
		WHERE status = 'FIRING'
		  AND id NOT IN (
			SELECT MAX(id)
			FROM alerts
			WHERE status = 'FIRING'
			GROUP BY host_id, rule_name
		)`, now, now); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_unique_firing
		ON alerts(host_id, rule_name)
		WHERE status = 'FIRING'`); err != nil {
		return err
	}
	if err := initSettings(db); err != nil {
		return err
	}
	return nil
}

func ensureColumn(db *sql.DB, tableName, columnName string) error {
	return ensureColumnType(db, tableName, columnName, "TEXT")
}

func ensureColumnType(db *sql.DB, tableName, columnName, columnType string) error {
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
	_, err = db.Exec("ALTER TABLE " + tableName + " ADD COLUMN " + columnName + " " + columnType)
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

	redisFailed := false
	if redisClient != nil {
		hosts, hit, err := getCachedHosts()
		if err == nil && hit {
			w.Header().Set("X-Cache", "HIT")
			writeJSON(w, hosts)
			return
		}
		if err != nil {
			redisFailed = true
			log.Printf("redis cache get failed key=%s err=%v", hostsCacheKey(), err)
		}
	}

	hosts, err := listHosts()
	if err != nil {
		http.Error(w, "failed to read hosts", http.StatusInternalServerError)
		return
	}

	cacheStatus := "BYPASS"
	if redisClient != nil && !redisFailed {
		if err := cacheHosts(hosts); err != nil {
			log.Printf("redis cache set failed key=%s err=%v", hostsCacheKey(), err)
		} else {
			cacheStatus = "MISS"
		}
	}
	w.Header().Set("X-Cache", cacheStatus)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hosts)
}

func handleAlerts(w http.ResponseWriter, r *http.Request) {
	setNoCache(w)
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if stateDB == nil {
		http.Error(w, "database unavailable", http.StatusInternalServerError)
		return
	}

	query := r.URL.Query()
	conditions := []string{}
	args := []interface{}{}
	statusExpression := `CASE WHEN alerts.status IS NULL OR TRIM(alerts.status) = '' THEN 'RESOLVED' ELSE UPPER(alerts.status) END`

	status := strings.ToUpper(strings.TrimSpace(query.Get("status")))
	if status == "FIRING" || status == "RESOLVED" {
		conditions = append(conditions, statusExpression+" = ?")
		args = append(args, status)
	}
	if hostID := query.Get("hostId"); hostID != "" {
		conditions = append(conditions, "alerts.host_id = ?")
		args = append(args, hostID)
	}
	level := strings.ToLower(strings.TrimSpace(query.Get("level")))
	if level == "warning" || level == "critical" {
		conditions = append(conditions, "LOWER(alerts.level) = ?")
		args = append(args, level)
	}

	limit := 100
	if value := query.Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			limit = parsed
			if limit > 500 {
				limit = 500
			}
		}
	}

	queryText := `SELECT alerts.id, alerts.host_id,
		COALESCE(NULLIF(TRIM(hosts.hostname), ''), alerts.host_id),
		alerts.rule_name, alerts.level, alerts.message, ` + statusExpression + `,
		alerts.current_value, alerts.threshold, alerts.started_at, alerts.resolved_at,
		alerts.updated_at, alerts.timestamp
		FROM alerts
		LEFT JOIN hosts ON hosts.host_id = alerts.host_id`
	if len(conditions) > 0 {
		queryText += " WHERE " + strings.Join(conditions, " AND ")
	}
	queryText += ` ORDER BY CASE WHEN ` + statusExpression + ` = 'FIRING' THEN 0 ELSE 1 END,
		CASE WHEN LOWER(alerts.level) = 'critical' THEN 0 ELSE 1 END,
		alerts.updated_at DESC, alerts.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := stateDB.Query(queryText, args...)
	if err != nil {
		http.Error(w, "failed to read alerts", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	alerts := []AlertListItem{}
	for rows.Next() {
		var item AlertListItem
		var hostID, hostname, ruleName, level, message, status sql.NullString
		var currentValue, threshold sql.NullFloat64
		var startedAt, resolvedAt, updatedAt, timestamp sql.NullString
		if err := rows.Scan(&item.ID, &hostID, &hostname, &ruleName, &level, &message, &status, &currentValue, &threshold, &startedAt, &resolvedAt, &updatedAt, &timestamp); err != nil {
			http.Error(w, "failed to read alerts", http.StatusInternalServerError)
			return
		}
		item.HostID = hostID.String
		item.Hostname = hostname.String
		item.RuleName = ruleName.String
		item.Level = level.String
		item.Message = message.String
		item.Status = status.String
		item.CurrentValue = currentValue.Float64
		item.Threshold = threshold.Float64
		item.StartedAt = startedAt.String
		item.ResolvedAt = resolvedAt.String
		item.UpdatedAt = updatedAt.String
		item.Timestamp = timestamp.String
		alerts = append(alerts, item)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "failed to read alerts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(alerts)
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
	event := newHostReportEvent(report, time.Now().UTC())
	publishContext, cancel := context.WithTimeout(r.Context(), kafkaOperationTimeout)
	defer cancel()
	if err := publishHostReportEvent(publishContext, event); err != nil {
		log.Printf("kafka publish failed hostId=%s err=%v", report.HostID, err)
		http.Error(w, "telemetry service unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
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
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hostID := r.URL.Query().Get("hostId")
	if hostID == "" {
		http.Error(w, "hostId is required", http.StatusBadRequest)
		return
	}
	redisFailed := false
	if redisClient != nil {
		cached, hit, err := getCachedSummary(hostID)
		if err == nil && hit {
			w.Header().Set("X-Cache", "HIT")
			writeJSON(w, cached)
			return
		}
		if err != nil {
			redisFailed = true
			log.Printf("redis cache get failed key=%s err=%v", summaryCacheKey(hostID), err)
		}
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
	cacheStatus := "BYPASS"
	if redisClient != nil && !redisFailed {
		if err := cacheSummary(hostID, response); err != nil {
			log.Printf("redis cache set failed key=%s err=%v", summaryCacheKey(hostID), err)
		} else {
			cacheStatus = "MISS"
		}
	}

	w.Header().Set("X-Cache", cacheStatus)
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

func saveMetrics(report Report, timestamp time.Time) error {
	if stateDB == nil {
		return nil
	}

	timestampText := timestamp.UTC().Format(time.RFC3339)
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
		if _, err := stateDB.Exec(`INSERT INTO metrics (host_id, timestamp, name, value, unit) VALUES (?, ?, ?, ?, ?)`, report.HostID, timestampText, item.name, item.value, item.unit); err != nil {
			return err
		}
	}
	return nil
}

func evaluateAlerts(report Report) error {
	if stateDB == nil {
		return nil
	}
	rules, err := loadAlertRules()
	if err != nil {
		return err
	}
	configuredRules := []struct {
		key          string
		config       AlertRuleConfig
		currentValue float64
		message      string
	}{
		{key: "cpu", config: rules.CPU, currentValue: report.CPU, message: "CPU usage exceeds threshold"},
		{key: "memory", config: rules.Memory, currentValue: report.Memory, message: "Memory usage exceeds threshold"},
		{key: "disk", config: rules.Disk, currentValue: report.Disk, message: "Disk usage exceeds threshold"},
	}

	now := time.Now().UTC().Format(time.RFC3339)
	hostname := report.Hostname
	if strings.TrimSpace(hostname) == "" {
		hostname = report.HostID
	}
	for _, rule := range configuredRules {
		if !rule.config.Enabled {
			var existing AlertRecord
			if alert, exists := getFiringAlert(report.HostID, rule.key); exists {
				existing = alert
			}
			result, updateErr := stateDB.Exec(`UPDATE alerts SET status = 'RESOLVED', message = 'Rule disabled', resolved_at = ?, updated_at = ?, current_value = ? WHERE host_id = ? AND rule_name = ? AND status = 'FIRING'`, now, now, rule.currentValue, report.HostID, rule.key)
			if updateErr != nil {
				return updateErr
			}
			if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
				return rowsErr
			} else if affected > 0 {
				existing.Status = "RESOLVED"
				existing.Message = "Rule disabled"
				existing.CurrentValue = rule.currentValue
				existing.ResolvedAt = now
				existing.UpdatedAt = now
				enqueueNotification(notificationEvent{Alert: existing, Hostname: hostname})
			}
			continue
		}
		if rule.currentValue >= rule.config.Threshold {
			result, err := stateDB.Exec(`UPDATE alerts SET current_value = ?, threshold = ?, level = ?, message = ?, updated_at = ? WHERE host_id = ? AND rule_name = ? AND status = 'FIRING'`, rule.currentValue, rule.config.Threshold, strings.ToLower(rule.config.Level), rule.message, now, report.HostID, rule.key)
			if err != nil {
				return err
			}
			if affected, err := result.RowsAffected(); err != nil {
				return err
			} else if affected == 0 {
				insertResult, insertErr := stateDB.Exec(`INSERT OR IGNORE INTO alerts (host_id, rule_name, level, message, status, current_value, threshold, started_at, updated_at, timestamp)
					VALUES (?, ?, ?, ?, 'FIRING', ?, ?, ?, ?, ?)`,
					report.HostID, rule.key, strings.ToLower(rule.config.Level), rule.message, rule.currentValue, rule.config.Threshold, now, now, now)
				if insertErr != nil {
					return insertErr
				}
				inserted, rowsErr := insertResult.RowsAffected()
				if rowsErr != nil {
					return rowsErr
				}
				if inserted > 0 {
					alert := AlertRecord{RuleName: rule.key, Level: strings.ToLower(rule.config.Level), Message: rule.message, Status: "FIRING", CurrentValue: rule.currentValue, Threshold: rule.config.Threshold, StartedAt: now, UpdatedAt: now, Timestamp: now}
					enqueueNotification(notificationEvent{Alert: alert, Hostname: hostname})
				}
			}
			if _, err := stateDB.Exec(`UPDATE alerts SET current_value = ?, updated_at = ? WHERE host_id = ? AND rule_name = ? AND status = 'FIRING'`, rule.currentValue, now, report.HostID, rule.key); err != nil {
				return err
			}
			continue
		}

		existing, exists := getFiringAlert(report.HostID, rule.key)
		result, err := stateDB.Exec(`UPDATE alerts SET status = 'RESOLVED', resolved_at = ?, updated_at = ?, current_value = ? WHERE host_id = ? AND rule_name = ? AND status = 'FIRING'`, now, now, rule.currentValue, report.HostID, rule.key)
		if err != nil {
			return err
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
			return rowsErr
		} else if affected > 0 {
			if !exists {
				existing = AlertRecord{RuleName: rule.key, Level: strings.ToLower(rule.config.Level), Threshold: rule.config.Threshold}
			}
			existing.Status = "RESOLVED"
			existing.CurrentValue = rule.currentValue
			existing.ResolvedAt = now
			existing.UpdatedAt = now
			enqueueNotification(notificationEvent{Alert: existing, Hostname: hostname})
		}
	}
	return nil
}

func getRecentAlerts(hostID string) []AlertRecord {
	if stateDB == nil {
		return nil
	}
	rows, err := stateDB.Query(`SELECT rule_name, level, message, status, current_value, threshold, started_at, resolved_at, updated_at, timestamp FROM alerts WHERE host_id = ? ORDER BY id DESC LIMIT 20`, hostID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := []AlertRecord{}
	for rows.Next() {
		var ruleName, level, message, status, startedAt, resolvedAt, updatedAt, ts sql.NullString
		var currentValue, threshold sql.NullFloat64
		if err := rows.Scan(&ruleName, &level, &message, &status, &currentValue, &threshold, &startedAt, &resolvedAt, &updatedAt, &ts); err != nil {
			continue
		}
		recordStatus := status.String
		if recordStatus == "" {
			recordStatus = "RESOLVED"
		}
		result = append(result, AlertRecord{
			RuleName: ruleName.String, Level: level.String, Message: message.String, Status: recordStatus,
			CurrentValue: currentValue.Float64, Threshold: threshold.Float64, StartedAt: startedAt.String,
			ResolvedAt: resolvedAt.String, UpdatedAt: updatedAt.String, Timestamp: ts.String,
		})
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
}
