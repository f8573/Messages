# OHMF Milestone 1 Foundation

Setup guide: [`SETUP.md`](./SETUP.md)

## Local toolchain (non-admin)

Because system-wide installs were blocked by permissions, local binaries were installed under `.tools`:
- Go: `.tools/go/bin/go.exe`
- sqlc: `.tools/bin/sqlc.exe`
- migrate: `.tools/bin/migrate.exe`

## Build and test

```powershell
$env:PATH="C:\Users\James\Downloads\Messages\ohmf\.tools\go\bin;C:\Users\James\Downloads\Messages\ohmf\.tools\bin;$env:PATH"
cd C:\Users\James\Downloads\Messages\ohmf\services\gateway
go mod tidy
go build ./...
go test ./...
$env:OHMF_RUN_INTEGRATION="1"
go test ./integration -v
```

Preferred (repo-local Go and integrated test runner)

If you have a repo-local Go binary under `ohmf/.tools/go/bin`, you can prefer it by setting the `GO_CMD` env var or by using the provided setup scripts. This is useful when system-wide Go is not installed.

Bash (Linux / macOS / WSL):

```bash
source ./scripts/setup-go.sh   # sets GO_CMD and prepends local go to PATH if present
./scripts/run-tests.sh --integration
```

PowerShell (Windows):

```powershell
. .\scripts\setup-go.ps1      # dot-source into current session to set GO_CMD
!.\scripts\run-tests.ps1 -Integration
```

The test runner will start a temporary Postgres Docker container and set `TEST_DATABASE_URL` when `--integration`/`-Integration` is used and `TEST_DATABASE_URL` is not already set.

CI note: Prefer using `GO_CMD` in CI jobs (or installing the same Go version) so test behavior is consistent across environments.


## Run with Docker Compose

1. Start Docker Desktop.
2. From repo root:

```powershell
docker compose -f .\infra\docker\docker-compose.yml up -d --build
```

API will be available at `http://localhost:18080`.

This stack now includes:
- Gateway (REST + WebSocket)
- Kafka (KRaft) + topic bootstrap
- Cassandra
- Redis
- `messages-processor`
- `delivery-processor`
- `sms-processor`

WebSocket endpoint: `ws://localhost:18080/v1/ws?access_token=<JWT>`

Feature flags (gateway):
- `APP_USE_KAFKA_SEND` (`true` by default in compose)
- `APP_USE_CASSANDRA_READS` (`false` by default in compose for phased rollout)
- `APP_ENABLE_WS_SEND` (`true` by default in compose)

## API endpoints

- `POST /v1/auth/phone/start`
- `POST /v1/auth/phone/verify`
- `POST /v1/auth/refresh`
- `POST /v1/auth/logout`
- `POST /v1/conversations`
- `GET /v1/conversations`
- `GET /v1/conversations/{id}`
- `POST /v1/messages`
- `POST /v1/messages/phone`
- `GET /v1/conversations/{id}/messages`
- `POST /v1/conversations/{id}/read`

OpenAPI: `packages/protocol/openapi/openapi.yaml`

User guide: `docs/user-signup-and-send-message.md`
