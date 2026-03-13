# Spec Traceability

This file maps the main report areas to concrete implementation files.

Identity and account model
- `services/gateway/internal/auth/service.go`
- `services/gateway/internal/users/service.go`
- `services/gateway/migrations/000001_init.up.sql`
- `services/gateway/migrations/000003_devices.up.sql`

Transport and message lifecycle
- `services/gateway/internal/messages/service.go`
- `services/gateway/internal/messages/async.go`
- `services/messages-processor/cmd/processor/main.go`
- `services/delivery-processor/cmd/processor/main.go`

Android SMS and carrier integration
- `docs/android-sms-integration.md`
- `services/gateway/internal/carrier/service.go`
- `services/sms-processor/cmd/processor/main.go`

Linked-device relay
- `docs/linked-device-relay.md`
- `services/gateway/internal/relay/service.go`
- `services/gateway/internal/relay/handler.go`

Mini-app platform
- `docs/mini-app-platform.md`
- `packages/protocol/schemas/miniapp_manifest.schema.json`
- `services/gateway/internal/miniapp/service.go`

Realtime and WebSocket protocol
- `docs/websocket-protocol.md`
- `services/gateway/internal/realtime/ws.go`
- `packages/protocol/openapi/openapi.yaml`

Database schema and migrations
- `packages/protocol/sql/ohmf_schema.sql`
- `services/gateway/migrations`
- `services/gateway/internal/db/postgres.go`

Client sync
- `docs/client-sync.md`
- `services/gateway/internal/sync/service.go`
- `services/gateway/internal/events/handler.go`

Observability
- `pkg/observability/observability.go`
- `services/gateway/internal/observability`
- `infra/observability`

Security
- `docs/security-controls.md`
- `services/gateway/internal/middleware/auth.go`
- `services/gateway/internal/token/jwt.go`
