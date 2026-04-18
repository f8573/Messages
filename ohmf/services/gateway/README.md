# Gateway Service

Mapping: OHMF spec section 19.

## Purpose

The gateway is the single client-facing service for the current OHMF runtime. It terminates HTTP and WebSocket traffic, authenticates users/devices, applies rate limits, persists messaging state, emits sync/realtime events, and fronts account, conversation, media, relay, and discovery flows.

## Current Route Surface

Public routes:

- `POST /v1/auth/phone/start`
- `POST /v1/auth/phone/verify`
- `POST /v1/auth/refresh`
- `POST /v1/auth/recovery-code`
- `GET /v1/ws`
- `GET /v2/ws`
- `GET /healthz`
- `GET /readyz`
- `GET /openapi.yaml`

Authenticated route groups include:

- account: `/v1/account/*`
- profile: `/v1/me`, `/v1/users/resolve`
- discovery: `/v1/discovery`, `/v1/contacts/discover`
- conversations: `/v1/conversations/*`, `/v2/conversations`
- messages: `/v1/messages/*`, `/v1/conversations/{id}/messages`, `/v1/conversations/{id}/search`
- realtime/sync: `/v1/events/stream`, `/v1/sync`, `/v2/sync`
- relay: `/v1/relay/*`
- presence: `/v1/presence/{userID}`, `/v1/conversations/{id}/presence`
- attestation: `/v1/devices/{id}/attestation/*`
- carrier/media/miniapps/notifications/device-keys/devices/abuse/blocks`

## Implemented Capabilities

- OTP auth, refresh rotation, recovery codes, and 2FA
- Conversation and message persistence with idempotency
- Read receipts, typing, effects, pinning, forwarding, search, and message lifecycle controls
- Realtime websocket fanout plus sync replay/resume support
- Device registration, push token management, and provider dispatch
- Device attestation challenge/verify flows with relay enforcement against persisted attestation state
- Account export/deletion flows with deletion audit state
- Discovery hashing and abuse controls
- Linked-device relay for SMS/MMS execution

## Mini-App Platform Ownership

The gateway is **EXCLUSIVE owner** of:
- Mini-app sessions (ephemeral runtime state tied to users/conversations)
- Session events (append-only log of bridge calls)
- Session snapshots (versioned state checkpoints within sessions)
- Session joins (per-user participation in a session)
- Conversation shares (initiated sharing events)

The gateway **delegates to app service** (via REST API):
- App registry queries (read-only)
- Release catalog and versions (read-only)
- User installs data (read-only)
- Update detection (computed by comparing installed vs latest approved release)
- Publisher keys (never touched by gateway)

**Legacy Tables (Deprecated):**
- `miniapp_releases` — deprecated; use apps service instead
- `miniapp_installs` — deprecated; use apps service instead
- See: Migration 000043_remove_miniapp_legacy_tables

For the current release, embedded-app deployment is deferred and `AppsAddr` should be left unset in deployed environments.
When the apps service is rolled out later, set `AppsAddr` to enable `RegistryClient` delegation.
See `docs/miniapp/ownership-boundaries.md` for detailed ownership matrix, data flow, and failure modes.

## Operational Notes

- The gateway uses Redis for rate limiting, presence, typing freshness, and some realtime/session state.
- PostgreSQL is the primary store; Cassandra remains an optional read path for message timelines.
- OpenAPI is served from `services/gateway/internal/openapi/openapi.yaml`.
- The canonical websocket behavior is documented in `docs/websocket-protocol.md`.

## Verification

Run:

```powershell
cd C:\Users\James\Downloads\Messages\ohmf\services\gateway
C:\Users\James\Downloads\Messages\ohmf\.tools\go\bin\go.exe test ./...
```

That command passes on the current branch state reflected by this README.
