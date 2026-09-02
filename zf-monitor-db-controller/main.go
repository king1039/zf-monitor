package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
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

func main() {
	cfg, err := LoadConfig(configFileName)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	fmt.Println("ZF Monitor DB Controller starting...")
	fmt.Printf("Loaded %d database instance(s)\n", len(cfg.Databases))

	for _, database := range cfg.Databases {
		printConnectionInfo(database)

		switch strings.ToLower(database.Type) {
		case "mssql":
			if err := testMSSQLInstance(database); err != nil {
				fmt.Printf("[MSSQL] %s connection failed: %v\n", database.InstanceID, err)
			}
		default:
			fmt.Printf("Unsupported database type: %s\n", database.Type)
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

func printConnectionInfo(database DatabaseConfig) {
	fmt.Println("Connecting to:")
	fmt.Printf("Instance: %s\n", database.InstanceID)
	fmt.Printf("Address: %s:%d\n", database.Host, database.Port)
}

func testMSSQLInstance(database DatabaseConfig) error {
	if strings.TrimSpace(database.Password) == placeholderPassword {
		fmt.Printf("[MSSQL] %s configuration error: database password has not been configured\n", database.InstanceID)
		return nil
	}

	dsn, err := buildMSSQLDSN(database)
	if err != nil {
		return err
	}

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return fmt.Errorf("open connection: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(1 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), connectionTimeout)
	err = db.PingContext(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	query := `
		SELECT
			CAST(SERVERPROPERTY('ServerName') AS NVARCHAR(128)),
			CAST(SERVERPROPERTY('ProductVersion') AS NVARCHAR(128)),
			CAST(SERVERPROPERTY('ProductLevel') AS NVARCHAR(128)),
			CAST(SERVERPROPERTY('Edition') AS NVARCHAR(128))
	`

	queryCtx, queryCancel := context.WithTimeout(context.Background(), connectionTimeout)
	defer queryCancel()

	rows, err := db.QueryContext(queryCtx, query)
	if err != nil {
		return fmt.Errorf("query %s failed: %w", database.InstanceID, err)
	}
	defer rows.Close()

	if !rows.Next() {
		return fmt.Errorf("query %s returned no data", database.InstanceID)
	}

	var serverName, version, productLevel, edition string
	if err := rows.Scan(&serverName, &version, &productLevel, &edition); err != nil {
		return fmt.Errorf("scan %s failed: %w", database.InstanceID, err)
	}

	fmt.Printf("[MSSQL] %s connected\n", database.InstanceID)
	fmt.Printf("Server Name: %s\n", serverName)
	fmt.Printf("Version: %s\n", version)
	fmt.Printf("Product Level: %s\n", productLevel)
	fmt.Printf("Edition: %s\n", edition)

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
