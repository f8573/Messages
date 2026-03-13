# WebSocket Protocol

This document defines the concrete gateway WebSocket contract implemented at `GET /v1/ws`.

Handshake
- Authenticate with `access_token=<jwt>` query parameter or `Authorization: Bearer <jwt>` during the upgrade request.
- Gateway responds with `401` when the token is missing or invalid.
- Gateway applies a per-IP connect rate limit before upgrading.

Frame envelope
```json
{
  "event": "send_message",
  "data": {
    "conversation_id": "conv_123",
    "idempotency_key": "idem-123",
    "content_type": "text",
    "content": { "text": "hello" },
    "client_generated_id": "cgid-123"
  }
}
```

Client events
- `auth`: returns the authenticated `user_id`.
- `subscribe`: alias for `presence_subscribe`.
- `presence_subscribe`: subscribe to presence fanout for one or more conversations.
- `send_message`: send a message through the gateway using the same validation rules as `POST /v1/messages`.
- `resync`: request a reconnect acknowledgement so the client can fall back to `GET /v1/sync`.
- `typing.started`: announce typing state for a conversation.
- `typing.stopped`: clear typing state for a conversation.

Server events
- `auth`
- `subscribe_ack`
- `presence_update`
- `ack`
- `delivery_update`
- `resync_ack`
- `typing.started`
- `typing.stopped`
- `error`

Validation
- `send_message` payloads are validated against `services/gateway/internal/middleware/schemas/ws-send_message.schema.json`.
- `subscribe` and `presence_subscribe` payloads are validated against `services/gateway/internal/middleware/schemas/ws-subscribe.schema.json`.

Reconnect and sync
- WebSocket reconnect is best-effort. Clients should reconnect, re-subscribe, and then call `GET /v1/sync?cursor=<opaque_cursor>` when they need deterministic catch-up.
- `resync_ack` is only an acknowledgement; the authoritative delta stream is the REST sync endpoint.

Operational notes
- Gateway exports `ohmf_gateway_ws_connections_active` and `ohmf_gateway_ws_messages_total` on `/metrics`.
- Presence keys are stored in Redis using `presence:user:{user_id}` and `presence:conv:{conversation_id}:user:{user_id}`.

Implementation references
- `services/gateway/internal/realtime/ws.go`
- `services/gateway/internal/middleware/schemas/ws-subscribe.schema.json`
- `services/gateway/internal/middleware/schemas/ws-send_message.schema.json`
- `docs/client-sync.md`
