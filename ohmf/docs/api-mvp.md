# API MVP Notes

Canonical contract is `packages/protocol/openapi/openapi.yaml`.

Conventions:
- Base path: `/v1`
- Auth: `Authorization: Bearer <access_token>`
- Errors: `ErrorEnvelope`
- Cursor pagination for list endpoints
- `idempotency_key` required on `POST /v1/messages`
- Send endpoints may return `202` with `queued=true` when Kafka persistence ack exceeds timeout.
- WebSocket endpoint: `GET /v1/ws` (auth via `access_token` query param or bearer header in handshake).
