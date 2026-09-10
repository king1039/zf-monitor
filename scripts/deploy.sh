#!/bin/bash
set -euo pipefail

cd /opt/zf-monitor-kafka

echo "[$(date '+%F %T')] Checking Docker images..."

docker compose -p zf-monitor pull

echo "[$(date '+%F %T')] Applying latest images..."

docker compose -p zf-monitor up -d --remove-orphans

echo "[$(date '+%F %T')] Deployment completed."

docker compose -p zf-monitor ps
