# 29 Unified Conversation Rendering

This document expands Section 29 of the OHMF platform spec with implementation guidance for rendering a unified conversation timeline that mixes OTT and carrier (SMS/MMS) sources.

Goals
- Present a single ordered timeline to users while preserving clear transport boundaries.
- Keep transport metadata available so clients can surface origin, delivery semantics, and any device-local constraints.
- Ensure deterministic ordering and idempotent reconciliation across sources.

Key concepts
- Canonical event: server-produced OTT event persisted in the canonical message store.
- Carrier-local event: Android SMS/MMS entry provided by the telephony provider and persisted locally on the device.
- Mirrored carrier event: optional server mirror of carrier-local content when user policy allows.
- Timeline item: the renderer's atomic unit; includes `server_order` (when available), `display_timestamp`, `source`, and `transport`.

Rendering rules
1. Sort primary by `display_timestamp`, secondary by `server_order` when present, tertiary by stable unique id.
2. Always surface `transport` and `source` badges (e.g., "SMS (device)", "OHMF (cloud)").
3. For device-local carrier items without `server_order`, render using provider timestamps but visually indicate "local" status; do not claim canonical ordering.
4. When mirrored carrier content exists, show mirror provenance and allow user to jump to device view where available.
5. Preserve provider metadata (thread id, provider message id) in a non-prominent metadata area to allow advanced debugging and reconciliation.

Merging strategy
- Merge by display time; when server_order ties occur, prefer canonical (server) items over mirrors and local items, unless product policy dictates otherwise.
- Provide deterministic tie-breakers (e.g., lexicographic id, source priority) to avoid flicker on resync.

UI considerations
- Show a small badge with transport (OTT/SMS/MMS/RELAY) and source (server/device/mirror).
- When tapping a mirrored carrier item, offer "View on device" if the device is linked and available.
- For edits/redactions, keep timeline placeholders so indices and cursors remain stable.

Sync and reconciliation
- During initial sync, clients SHOULD fetch both canonical OTT items and optionally mirrored carrier items per user policy.
- Local device imports SHOULD be stitched into the timeline using provider timestamps; server-assigned `server_order` should take precedence once available.
- Clients MUST dedupe messages using `message_id` and `client_generated_id`.

Example timeline item (JSON)

```json
{
  "message_id": "msg_01JXYZ",
  "conversation_id": "cnv_01",
  "server_order": 456,
  "display_timestamp": "2026-03-06T17:00:01Z",
  "sender": {"user_id": "usr_01"},
  "transport": "OTT",
  "source": "server",
  "content_type": "text",
  "content": {"text":"hello"},
  "provider_metadata": null,
  "visibility_state": "ACTIVE"
}
```

Notes for implementers
- Keep the timeline item compact; consider sending heavier `content` payloads lazily.
- For mobile clients, prioritize stable cursors and incremental updates to conserve bandwidth.
- Add visual affordances for transport switches (e.g., when subsequent messages switch from OTT to SMS).
