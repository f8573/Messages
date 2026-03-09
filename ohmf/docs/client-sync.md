# 26 Client Sync Model

This document summarizes the Client Sync Model (Section 26 of the OHMF platform spec).

## Sources
Clients SHOULD manage synchronization from three possible sources:

1. canonical OTT server state
2. Android-local carrier provider state
3. optional mirrored carrier server state

## Sync cursor
Each client SHOULD track a durable cursor or equivalent watermark. The cursor MUST be opaque to clients and stable for incremental pagination/resume.

Cursor format (recommended):

- `cursor_version` (integer)
- `last_server_order` (map of conversation_id -> server_order)
- `timestamp_ms` (unix ms)

Example (JSON representation):

```json
{
  "cursor_version": 1,
  "last_server_order": {"cnv_01JXYZ": 456, "cnv_02ABC": 120},
  "timestamp_ms": 1678380000000
}
```

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
