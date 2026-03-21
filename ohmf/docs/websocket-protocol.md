# WebSocket Protocol

This document defines the current gateway WebSocket contract implemented at `GET /v1/ws` and `GET /v2/ws`.

## Handshake

- Authenticate with `access_token=<jwt>` query parameter or `Authorization: Bearer <jwt>` during the upgrade request.
- Gateway returns `401` when the token is missing or invalid.
- Gateway applies a per-IP connect rate limit before upgrading.
- Both endpoints use heartbeat ping/pong and refresh presence/session metadata on activity.

## Frame Envelope

```json
{
  "event": "send_message",
  "data": {
    "conversation_id": "conv_123",
    "idempotency_key": "idem-123",
    "content_type": "text",
    "content": {
      "text": "hello",
      "spans": [],
      "mentions": []
    },
    "client_generated_id": "cgid-123"
  }
}
```

## `v1` Client Events

- `auth`
- `subscribe`
- `presence_subscribe`
- `send_message`
- `resync`
- `typing.started`
- `typing.stopped`

## `v2` Client Events

- `hello`
  - Supports `device_id`
  - Supports `last_user_cursor`
  - Supports `last_cursor`
- `ack`
- `send_message`
- `subscribe`
- `presence_subscribe`
- `resync`
- `typing.started`
- `typing.stopped`

## Server Events

- `hello_ack`
- `auth`
- `subscribe_ack`
- `presence_update`
- `ack`
- `delivery_update`
- `message_created`
- `message_edited`
- `message_deleted`
- `message_reaction_updated`
- `conversation_message_effect_triggered`
- `read_receipt`
- `conversation_preview_updated`
- `conversation_state_updated`
- `conversation_typing_updated`
- `typing.started`
- `typing.stopped`
- `resync_ack`
- `resync_required`
- `error`

## Resume and Replay

- `v2` resume supports both numeric `last_user_cursor` and string `last_cursor`.
- Invalid cursors are rejected with `error.code = "invalid_cursor"`.
- When the server can replay missed inbox events, it sends them immediately after `hello_ack`.
- When replay cannot fully satisfy the delta, the server emits `resync_required` with a `cursor_hint`.
- Legacy `resync` remains available, but `v2` resume should be preferred by new clients.

## Typing Semantics

- `typing.started` is throttled server-side and deduped using a short Redis TTL.
- Repeated `typing.started` within the TTL refreshes typing state instead of rebroadcasting indefinitely.
- `typing.stopped` is not rate-limited.
- Disconnect cleanup removes stale typing keys and emits stop notifications so indicators do not remain stuck.

## Presence Semantics

- Presence is stored under `presence:user:{user_id}`.
- Freshness is tracked under `presence:user:{user_id}:last_seen`.
- Session metadata is stored in Redis and includes `last_seen_at_ms`.
- Presence/session timestamps refresh on connect, pong, heartbeat, auth, subscribe, send, and typing activity.

## Validation

- `send_message` payloads follow the same validation rules as `POST /v1/messages`.
- Text payloads may include `spans`, `mentions`, `expires_on_read`, `expires_in_seconds`, and `expires_at`.
- `subscribe` and `presence_subscribe` payloads are validated against the WebSocket subscription schemas in `services/gateway/internal/middleware/schemas/`.

## Recovery Guidance

- Deterministic catch-up still relies on the sync endpoints when a client needs full reconciliation:
  - `GET /v1/sync`
  - `GET /v2/sync`
- Clients should reconnect, send `hello` with the latest cursor, then fall back to REST sync if `resync_required` is returned.

## Operational Notes

- Gateway exports websocket metrics on `/metrics`.
- Presence and typing state are Redis-backed and shared across gateway instances.
- Session IDs are stable for the lifetime of a connection and are included in `hello_ack`.

## Implementation References

- `services/gateway/internal/realtime/ws.go`
- `services/gateway/internal/middleware/schemas/ws-subscribe.schema.json`
- `services/gateway/internal/middleware/schemas/ws-send_message.schema.json`
- `docs/client-sync.md`
