# OHMF Setup Guide

This guide shows how to run the full local stack (gateway + Kafka + Cassandra + Redis + processors + Postgres).

## Prerequisites

- Docker Desktop running
- Windows PowerShell (examples below use PowerShell)
- Optional local toolchain (already vendored in `.tools`) if you want to run Go tests outside Docker

## 1) Start the Stack

From repo root:

```powershell
docker compose -f .\infra\docker\docker-compose.yml up -d --build
```

Quick status check:

```powershell
docker compose -f .\infra\docker\docker-compose.yml ps
```

## 2) Verify Health

API health:

```powershell
Invoke-WebRequest -UseBasicParsing http://localhost:18081/healthz
```

Expected status: `200`.

## 3) Core Endpoints

- REST API base: `http://localhost:18081/v1`
- WebSocket: `ws://localhost:18081/v1/ws?access_token=<JWT>`

OpenAPI contract:

- `packages/protocol/openapi/openapi.yaml`

## 4) Common Dev Commands

Start (foreground, with logs):

```powershell
.\scripts\dev.ps1 -Action up
```

Stop and remove volumes:

```powershell
.\scripts\dev.ps1 -Action down
```

Tail API logs:

```powershell
.\scripts\dev.ps1 -Action logs
```

## 5) Run Tests (Gateway)

```powershell
$env:PATH="$PWD\.tools\go\bin;$env:PATH"
cd .\services\gateway
go test ./...
$env:OHMF_RUN_INTEGRATION="1"
go test .\integration -v
```

## 6) Feature Flags (Gateway)

Configured in compose under `api.environment`:

- `APP_USE_KAFKA_SEND=true`
- `APP_USE_CASSANDRA_READS=false`
- `APP_ENABLE_WS_SEND=true`

For Cassandra read cutover testing, set:

- `APP_USE_CASSANDRA_READS=true`

Then restart API:

```powershell
docker compose -f .\infra\docker\docker-compose.yml up -d --build api
```

## 7) Troubleshooting

- If `kafka-init` fails, rerun:
  - `docker compose -f .\infra\docker\docker-compose.yml up -d kafka kafka-init`
- If any service is unhealthy:
  - `docker compose -f .\infra\docker\docker-compose.yml logs -f <service>`
- If you need a clean reset:
  - `docker compose -f .\infra\docker\docker-compose.yml down -v`
  - `docker compose -f .\infra\docker\docker-compose.yml up -d --build`
