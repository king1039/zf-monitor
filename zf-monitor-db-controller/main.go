package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"gopkg.in/yaml.v3"
)

const configFileName = "config.yaml"
const connectionTimeout = 5 * time.Second
const placeholderPassword = "CHANGE_ME_BEFORE_RUN"

type Config struct {
	Interval  int              `yaml:"interval"`
	Backend   BackendConfig    `yaml:"backend"`
	Databases []DatabaseConfig `yaml:"databases"`
}

type BackendConfig struct {
	URL string `yaml:"url"`
}

type DatabaseConfig struct {
	InstanceID string `yaml:"instanceId"`
	Name       string `yaml:"name"`
	Type       string `yaml:"type"`
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
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

func main() {
	cfg, err := LoadConfig(configFileName)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if cfg.Interval <= 0 {
		cfg.Interval = 10
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("ZF Monitor DB Controller starting...")
	fmt.Printf("Loaded %d database instance(s)\n", len(cfg.Databases))

	collectInstances(cfg)

	ticker := time.NewTicker(time.Duration(cfg.Interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Shutting down DB controller")
			return
		case <-ticker.C:
			collectInstances(cfg)
		}
	}
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("failed to load config.yaml")
		}
		return nil, fmt.Errorf("failed to load config.yaml: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config.yaml: %w", err)
	}

	return &cfg, nil
}

func collectInstances(cfg *Config) {
	for _, database := range cfg.Databases {
		printConnectionInfo(database)

		switch strings.ToLower(database.Type) {
		case "mssql":
			report, err := collectMSSQLReport(database)
			if err != nil {
				fmt.Printf("[MSSQL] %s connection failed: %v\n", database.InstanceID, err)
			}
			if report != nil {
				if err := postDatabaseReport(cfg.Backend.URL, report); err != nil {
					log.Printf("[MSSQL] %s backend report warning: %v", database.InstanceID, err)
				}
			}
		default:
			fmt.Printf("Unsupported database type: %s\n", database.Type)
		}
	}
}

func printConnectionInfo(database DatabaseConfig) {
	fmt.Println("Connecting to:")
	fmt.Printf("Instance: %s\n", database.InstanceID)
	fmt.Printf("Address: %s:%d\n", database.Host, database.Port)
}

func collectMSSQLReport(database DatabaseConfig) (*DatabaseReport, error) {
	report := &DatabaseReport{
		InstanceID: database.InstanceID,
		Name:       database.Name,
		Type:       database.Type,
		Host:       database.Host,
		Port:       database.Port,
		Status:     "offline",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}

	if strings.TrimSpace(database.Password) == placeholderPassword {
		fmt.Printf("[MSSQL] %s configuration error: database password has not been configured\n", database.InstanceID)
		return nil, nil
	}

	dsn, err := buildMSSQLDSN(database)
	if err != nil {
		return report, err
	}

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		report.Error = fmt.Sprintf("open connection: %v", err)
		return report, err
	}
	defer db.Close()

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(1 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout)
	pingErr := db.PingContext(ctx)
	cancel()
	if pingErr != nil {
		report.Error = fmt.Sprintf("connection failed: %v", pingErr)
		fmt.Printf("[MSSQL] %s connection failed: %v\n", database.InstanceID, pingErr)
		return report, pingErr
	}

	report.Status = "online"

	serverName, version, productLevel, edition, err := queryServerProperties(db)
	if err != nil {
		log.Printf("[MSSQL] %s warning: server property query failed: %v", database.InstanceID, err)
	} else {
		report.ServerName = serverName
		report.Version = version
		report.ProductLevel = productLevel
		report.Edition = edition
	}

	report.MaxConnections, err = queryScalarFloat64(db, "SELECT @@MAX_CONNECTIONS;")
	if err != nil {
		log.Printf("[MSSQL] %s warning: max_connections query failed: %v", database.InstanceID, err)
	}

	report.DatabaseCount, err = queryScalarFloat64(db, "SELECT COUNT(*) FROM sys.databases WHERE state_desc = 'ONLINE';")
	if err != nil {
		log.Printf("[MSSQL] %s warning: database_count query failed: %v", database.InstanceID, err)
	}

	report.TotalDatabaseSizeMB, err = queryScalarFloat64(db, "SELECT COALESCE(SUM(CAST(size AS BIGINT)) * 8.0 / 1024.0, 0) FROM sys.master_files;")
	if err != nil {
		log.Printf("[MSSQL] %s warning: total_database_size_mb query failed: %v", database.InstanceID, err)
	}

	report.UptimeSeconds, err = queryScalarFloat64(db, "SELECT DATEDIFF_BIG(SECOND, create_date, GETDATE()) FROM sys.databases WHERE name = 'tempdb';")
	if err != nil {
		log.Printf("[MSSQL] %s warning: uptime_seconds query failed: %v", database.InstanceID, err)
	}

	report.Connections, err = queryScalarFloat64(db, "SELECT COUNT(*) FROM sys.dm_exec_connections;")
	if err != nil {
		log.Printf("[MSSQL] %s warning: connections query failed: %v", database.InstanceID, err)
	}

	report.ActiveSessions, err = queryScalarFloat64(db, "SELECT COUNT(*) FROM sys.dm_exec_sessions WHERE is_user_process = 1;")
	if err != nil {
		log.Printf("[MSSQL] %s warning: active_sessions query failed: %v", database.InstanceID, err)
	}

	report.RunningRequests, err = queryScalarFloat64(db, "SELECT COUNT(*) FROM sys.dm_exec_requests WHERE session_id <> @@SPID;")
	if err != nil {
		log.Printf("[MSSQL] %s warning: running_requests query failed: %v", database.InstanceID, err)
	}

	fmt.Printf("[MSSQL] %s connected\n", database.InstanceID)
	fmt.Printf("Server Name: %s\n", report.ServerName)
	fmt.Printf("Version: %s\n", report.Version)
	fmt.Printf("Product Level: %s\n", report.ProductLevel)
	fmt.Printf("Edition: %s\n", report.Edition)

	return report, nil
}

func queryServerProperties(db *sql.DB) (string, string, string, string, error) {
	query := `
		SELECT
			CAST(SERVERPROPERTY('ServerName') AS NVARCHAR(128)),
			CAST(SERVERPROPERTY('ProductVersion') AS NVARCHAR(128)),
			CAST(SERVERPROPERTY('ProductLevel') AS NVARCHAR(128)),
			CAST(SERVERPROPERTY('Edition') AS NVARCHAR(128))
	`

	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout)
	defer cancel()

	var serverName, version, productLevel, edition string
	if err := db.QueryRowContext(ctx, query).Scan(&serverName, &version, &productLevel, &edition); err != nil {
		return "", "", "", "", err
	}
	return serverName, version, productLevel, edition, nil
}

func queryScalarFloat64(db *sql.DB, query string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout)
	defer cancel()

	var value sql.NullFloat64
	if err := db.QueryRowContext(ctx, query).Scan(&value); err != nil {
		return 0, err
	}
	if !value.Valid {
		return 0, nil
	}
	return value.Float64, nil
}

func postDatabaseReport(baseURL string, report *DatabaseReport) error {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/api/database/report"
	payload, err := json.Marshal(report)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("backend responded with %s", resp.Status)
	}
	return nil
}

func buildMSSQLDSN(database DatabaseConfig) (string, error) {
	if strings.TrimSpace(database.Host) == "" {
		return "", fmt.Errorf("%s: host is required", database.InstanceID)
	}
	if database.Port <= 0 {
		return "", fmt.Errorf("%s: port is required", database.InstanceID)
	}
	if strings.TrimSpace(database.Username) == "" {
		return "", fmt.Errorf("%s: username is required", database.InstanceID)
	}
	if strings.TrimSpace(database.Password) == "" {
		return "", fmt.Errorf("%s: password is required", database.InstanceID)
	}

	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(database.Username, database.Password),
		Host:   fmt.Sprintf("%s:%d", database.Host, database.Port),
	}

	return u.String(), nil
}
