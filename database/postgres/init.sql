CREATE TABLE IF NOT EXISTS hosts (
    host_id TEXT PRIMARY KEY,
    hostname TEXT,
    last_seen TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS metrics (
    id BIGSERIAL PRIMARY KEY,
    host_id TEXT NOT NULL,
    timestamp TIMESTAMPTZ,
    name TEXT,
    value DOUBLE PRECISION,
    unit TEXT
);

CREATE TABLE IF NOT EXISTS alerts (
    id BIGSERIAL PRIMARY KEY,
    host_id TEXT NOT NULL,
    rule_name TEXT,
    level TEXT,
    message TEXT,
    timestamp TIMESTAMPTZ,
    status TEXT,
    current_value DOUBLE PRECISION,
    threshold DOUBLE PRECISION,
    started_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS database_instances (
    instance_id TEXT PRIMARY KEY,
    name TEXT,
    db_type TEXT,
    host TEXT,
    port INTEGER,
    server_name TEXT,
    version TEXT,
    product_level TEXT,
    edition TEXT,
    status TEXT,
    last_seen TIMESTAMPTZ,
    last_error TEXT
);

CREATE TABLE IF NOT EXISTS database_metrics (
    id BIGSERIAL PRIMARY KEY,
    instance_id TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    uptime_seconds DOUBLE PRECISION,
    connections DOUBLE PRECISION,
    max_connections DOUBLE PRECISION,
    active_sessions DOUBLE PRECISION,
    running_requests DOUBLE PRECISION,
    database_count DOUBLE PRECISION,
    total_database_size_mb DOUBLE PRECISION
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_metrics_host_timestamp
    ON metrics(host_id, timestamp);

CREATE INDEX IF NOT EXISTS idx_database_metrics_instance_timestamp
    ON database_metrics(instance_id, timestamp);

CREATE INDEX IF NOT EXISTS idx_alerts_host_rule_status
    ON alerts(host_id, rule_name, status);

CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_unique_firing
    ON alerts(host_id, rule_name)
    WHERE status = 'FIRING';
