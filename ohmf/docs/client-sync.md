# 26 Client Sync Model

This document summarizes the Client Sync Model (Section 26 of the OHMF platform spec).

## Sources
Clients SHOULD manage synchronization from three possible sources:

1. canonical OTT server state
2. Android-local carrier provider state
3. optional mirrored carrier server state

## Sync cursor
Each client SHOULD track a durable cursor or equivalent watermark. The spec recommends an opaque cursor format (see below) for long-term stability, but the current server implementation accepts and returns an RFC3339 timestamp token. Clients MUST be prepared to handle RFC3339 timestamps as the `cursor` value and also SHOULD support opaque cursors if the server introduces a `cursor_version` migration in the future.

Spec-recommended cursor format (opaque JSON/base64):

- `cursor_version` (integer)
- `last_server_order` (map of conversation_id -> server_order)
- `timestamp_ms` (unix ms)

Example (JSON representation of an opaque cursor):

```json
{
  "cursor_version": 1,
  "last_server_order": {"cnv_01JXYZ": 456, "cnv_02ABC": 120},
  "timestamp_ms": 1678380000000
}
```

Current implementation (server behavior):

- The server accepts `cursor` as an RFC3339 timestamp string (UTC). When provided, the server returns events with `created_at > cursor`.
- The server returns `next_cursor` as an RFC3339 timestamp representing the last-event time processed.
- Example token: `2026-03-08T12:00:00.000000000Z`
Cursor format (current implementation):

Clients SHOULD:

- Send an RFC3339 timestamp as `cursor` when resuming incremental sync until the server publishes a `cursor_version` migration.
- Gracefully handle `next_cursor` values that are RFC3339 timestamps and store them as the client's durable cursor.
- Be prepared to decode or accept opaque/base64 cursors in future versions if the server transitions to the spec-recommended format.

Implementation note:

- The gateway currently parses `cursor` by attempting RFC3339 parsing; if unparsable it falls back to a short default window (e.g., last 5 minutes). Clients that send opaque tokens without a `cursor_version` may receive unexpected results. If you require spec-level opaque cursors, open an issue to track implementing `cursor_version`-aware opaque cursors on the server.

## Sync endpoint
The server SHOULD accept a `cursor` parameter and return `events` plus a `next_cursor` when available.

Response example:

```json
{
  "events": [...],
  "next_cursor": "opaque-cursor-token"
}
```

## Reconnect algorithm (recommended)
1. restore local cache
2. reopen websocket
3. issue incremental sync from last durable cursor
4. reconcile pending local outbox entries using idempotency keys
5. apply remote events in stable order

## Notes for implementers
- Cursor compactness is important for mobile networks — consider base64-encoding a compact binary representation.
- Server-side cursors should be stable across minor schema changes; include `cursor_version` to allow evolution.
- For very large conversation sets, clients MAY request per-conversation cursors or use conversation-scoped sync operations.
