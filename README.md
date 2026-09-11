# Stark Monitor

> A distributed server monitoring platform built with Go, Kafka, Redis and PostgreSQL.

Stark Monitor is a distributed monitoring platform that collects server metrics, database metrics, processes events asynchronously, and provides real-time monitoring dashboards.

Current architecture:

```
Agent
  |
  ↓
Backend API
  |
  ↓
Kafka (Event Pipeline)
  |
  ↓
Consumer Processing
  |
  +---- PostgreSQL (Primary Storage)
  |
  +---- Redis (Cache Layer)
  |
  ↓
Web Dashboard
```

---

# Development Roadmap

The project was developed step by step following a production-oriented architecture evolution.

## Phase 1 - Containerization

### 1. Docker

Goal:

Containerize each service and create reproducible runtime environments.

Implemented:

* Backend Docker image
* Agent container
* Database controller container
* Runtime isolation

Technology:

```
Docker
Dockerfile
Linux Container
```

---

## Phase 2 - Service Orchestration

### 2. Docker Compose

Goal:

Manage multiple services together.

Implemented:

* Multi-container deployment
* Internal Docker network
* Persistent volumes
* Service dependency management

Architecture:

```
docker-compose

├── zf-monitor-back
├── zf-monitor-agent
├── zf-monitor-db-controller
├── redis
├── kafka
└── postgresql
```

Technology:

```
Docker Compose
Docker Network
Docker Volume
Healthcheck
```

---

## Phase 3 - CI/CD Automation

### 3. GitHub Actions + GHCR

Goal:

Automate image building and deployment.

Implemented:

```
Git Push
   |
   ↓
GitHub Actions
   |
   ↓
Docker Build
   |
   ↓
GHCR Image Registry
   |
   ↓
Linux Server Pull
   |
   ↓
Docker Compose Deploy
```

Technology:

```
GitHub Actions
GHCR
Docker Buildx
```

---

## Phase 4 - Monitoring Collection

### 4. Agent System

Goal:

Collect metrics from monitored servers.

Collected metrics:

* CPU
* Memory
* Disk
* Network
* Processes

Flow:

```
Windows/Linux Agent

       |
       ↓

HTTP Report API

       |
       ↓

Backend
```

Technology:

```
Go
Windows Service
HTTP API
```

---

## Phase 5 - Cache Layer

### 5. Redis Integration

Goal:

Reduce database pressure and provide fast access.

Implemented:

* Host latest status cache
* Latest metrics cache
* Redis health monitoring

Architecture:

```
Backend

  |
  +---- PostgreSQL
  |
  +---- Redis Cache
```

Technology:

```
Redis 7
Cache Layer
```

---

## Phase 6 - Event Driven Architecture

### 6. Kafka Integration

Goal:

Convert synchronous ingestion into asynchronous event processing.

Before:

```
Agent
 |
 ↓
Backend
 |
 ↓
SQLite
```

After:

```
Agent
 |
 ↓
Backend API
 |
 ↓
Kafka Topic

stark.metrics.raw

 |
 ↓

Consumer Group

stark-monitor-metrics-v1

 |
 ↓

PostgreSQL
```

Implemented:

* Kafka-first ingestion
* Message key: hostId
* Partition strategy
* Consumer group
* At-least-once delivery

Technology:

```
Apache Kafka 4.3.1
KRaft Mode
Event Driven Architecture
```

---

## Phase 7 - Database Migration

### 7. PostgreSQL Migration

Goal:

Replace SQLite with production database.

Migration:

Before:

```
Backend
 |
SQLite
```

After:

```
Backend
 |
PostgreSQL 16
```

Implemented:

* PostgreSQL schema migration
* pgx driver
* SQL compatibility layer
* SQLite fallback support

Database:

```
PostgreSQL 16

Tables:

├── hosts
├── metrics
├── alerts
├── settings
├── database_instances
└── database_metrics
```

Technology:

```
PostgreSQL
SQL Migration
database/sql
pgx
```

---

# Project Structure

```
Stark-monitor

├── zf-monitor-back
│   ├── Go Backend
│   ├── Kafka Producer
│   ├── Kafka Consumer
│   ├── REST API
│   └── PostgreSQL Access Layer
│
├── zf-monitor-agent
│   └── Server Metrics Collector
│
├── zf-monitor-db-controller
│   └── Database Monitoring Collector
│
├── database
│   └── postgres
│       ├── init.sql
│       └── migrations
│
├── scripts
│   └── deployment scripts
│
├── docker-compose.yaml
│
└── .github
    └── workflows
        └── docker-images.yml
```

---

# Current Technology Stack

| Layer         | Technology              |
| ------------- | ----------------------- |
| Backend       | Go                      |
| Agent         | Go                      |
| Frontend      | HTML/CSS/JavaScript     |
| Container     | Docker                  |
| Orchestration | Docker Compose          |
| CI/CD         | GitHub Actions          |
| Registry      | GHCR                    |
| Message Queue | Apache Kafka            |
| Cache         | Redis                   |
| Database      | PostgreSQL              |
| Deployment    | Alibaba Cloud ECS Linux |

---

# Current Architecture

```
                 Agent

                   |
                   |
                   ↓

              Backend API

                   |
                   |
          Kafka stark.metrics.raw

                   |
                   ↓

              Consumer

          /                 \

 PostgreSQL              Redis

 Storage                Cache

                   |
                   ↓

              Dashboard
```
