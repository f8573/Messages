# OHMF Milestone 1 Foundation

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
go test ./integration -v
```

## Run with Docker Compose

1. Start Docker Desktop.
2. From repo root:

```powershell
docker compose -f .\infra\docker\docker-compose.yml up -d --build
```

API will be available at `http://localhost:18080`.

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
