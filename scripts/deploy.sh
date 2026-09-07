#!/bin/bash
set -euo pipefail

cd /opt/zf-monitor-cicd

echo "[$(date '+%F %T')] Checking Docker images..."

docker compose pull

echo "[$(date '+%F %T')] Applying latest images..."

docker compose up -d --remove-orphans

echo "[$(date '+%F %T')] Deployment completed."

docker compose ps
