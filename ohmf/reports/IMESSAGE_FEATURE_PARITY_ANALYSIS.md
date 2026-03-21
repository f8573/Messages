# iMessage Feature Parity Analysis: OHMF Gateway
**Date:** March 20, 2026
**Version:** 3.0
**Status:** Updated to match the current gateway codebase

---

## Executive Summary

The OHMF gateway now covers most non-media, non-calling parity features that were previously identified as blocking. Since the earlier scans, the gateway added:

- Typing indicators with throttling, cleanup, and realtime fanout
- Per-message read receipts and conversation read-status aggregation
- Message effects plus conversation-level effect policy
- Push token storage and provider dispatch for FCM/APNs/WebPush
- Recovery codes, 2FA setup/verification, and recovery-code sign-in
- Message pinning, forwarding, search, descriptions, roles, invites, bans, and richer conversation settings
- Structured rich text and mention metadata
- Message expiry, retention-based expiry, and expires-on-read behavior
- Realtime resume using `last_user_cursor` or `last_cursor`
- Account export/deletion improvements
- Discovery and OTP abuse controls
- Relay expiry enforcement, capability-aware routing, and stronger accept signatures
- Secure pairing, encrypted key backup/restore, presence APIs, reply threads, and verifier-backed device attestation

The biggest remaining gaps are now concentrated in:

- Media depth: transcoding, thumbnails, encrypted attachments, video/GIF pipelines
- Calling: voice/video/signaling
- Some advanced security and compliance work: direct vendor-proof attestation parsing, retention/reporting runbooks
- Some advanced conversation UX: richer moderation workflows and client-side reaction/effect depth

---

## Status Matrix

### 1. Core Messaging

| Feature | Status | Notes |
|---|---|---|
| Text messages | Implemented | Server-ordered, idempotent, multi-device aware, sync/realtime integrated. |
| Message edits | Partial | Edit history exists and text/rich-text edits work; encrypted-message edit support remains restricted. |
| Reactions | Partial | Add/remove/list works; animated reaction UX is still absent. |
| Read receipts | Implemented | Per-message receipts, conversation member `read_at`, and `GET /v1/conversations/{id}/read-status` are available. |
| Delivery confirmations | Partial | Delivery state exists, but failure taxonomy and deeper device-level modeling remain limited. |
| Typing indicators | Implemented | Throttled, deduped, Redis-backed TTL refresh, disconnect cleanup, realtime broadcast. |
| Message search | Implemented | Conversation search endpoint exists with rate limiting; ranking/index optimization is still open. |
| Message expiration | Partial | `expires_at`, explicit expiry, retention-derived expiry, and expires-on-read are implemented; background cleanup UX is still basic. |
| Message forwarding | Implemented | Forward flow preserves source attribution in content. |
| Rich text / mentions | Implemented | Text payloads now support `spans` and `mentions` validation/persistence. |
| Message pinning | Implemented | Pin/unpin and pinned-message listing are implemented. |

### 2. Conversations

| Feature | Status | Notes |
|---|---|---|
| Direct messages | Implemented | Stable DM creation and lookup paths. |
| Group conversations | Implemented | Group creation, member management, metadata, and state projection are in place. |
| Group admin roles | Implemented | `OWNER` and `ADMIN` roles are enforced, including role updates and last-owner protection. |
| Archiving | Partial | Stored per user and surfaced in conversation state; notification dispatch now respects archive suppression, but richer UX/workflows are still limited. |
| Muting | Partial | `muted_until` exists and notification dispatch respects mute/archive state; broader client semantics are still limited. |
| Conversation settings | Implemented | Theme, retention, expiration, settings versioning, and effect policy exist. |
| Conversation descriptions | Implemented | Writable via metadata update and returned in conversation payloads. |
| Invites / join codes | Implemented | Create/list/redeem invite flows exist. |
| Moderation | Partial | Ban/unban is implemented; full moderation queues/filtering are not. |
| Conversation expiration | Partial | Expiration-related settings exist; no separate lifecycle worker is documented. |
| Threads / hierarchy | Implemented | Reply links and reply listing are implemented; deeper nested thread UX remains a client concern. |

### 3. Realtime

| Feature | Status | Notes |
|---|---|---|
| WebSocket connection | Implemented | `/v1/ws` and `/v2/ws` are live with auth, heartbeat, session tracking, and rate limiting. |
| Message events | Implemented | Message create/edit/delete, delivery, and state events are broadcast. |
| Read receipt broadcast | Partial | Receipt/state events are emitted, but client-facing aggregation UX is still simple. |
| Typing indicators | Implemented | Full realtime typing flow now exists. |
| Presence | Partial | Online presence, `last_seen`, session-aware presence APIs, and conversation presence listing exist; away/DND/custom presence do not. |
| Connection resume | Implemented | `last_user_cursor` and `last_cursor` resume plus replay/resync hints are supported. |
| Message replay / catch-up | Partial | Replay from user inbox cursor is implemented; broader offline recovery UX is still basic. |
| SSE fallback | Open | Not implemented. |
| Push notifications | Implemented | Provider infrastructure and dispatch are present. |

### 4. Media

| Feature | Status | Notes |
|---|---|---|
| Upload/download | Partial | Core upload/download and binding exist. |
| Image optimization | Open | No transcoding/thumbnail pipeline documented as complete. |
| Video support | Open | Not implemented. |
| GIF/sticker media | Open | Not implemented. |
| Link preview extraction | Partial | Schema exists but extraction pipeline is still limited. |
| Attachment encryption | Open | Not complete for OTT parity. |

### 5. Security & Privacy

| Feature | Status | Notes |
|---|---|---|
| E2EE foundations | Partial | Device keys, prekeys, encrypted payload validation, and trust state exist; full ratchet/attestation parity does not. |
| Device attestation | Partial | Challenge-based device attestation with verifier-signed verdicts is implemented and relay enforcement reads persisted attestation state; direct vendor-proof parsing in-gateway is still optional. |
| Contact privacy / discovery hashing | Partial | Pepper is configurable, versioned `SHA256_PEPPERED_V1/V2` is accepted, and abuse controls exist; rotation tooling is still open. |
| Account security | Implemented | OTP auth, refresh rotation, recovery codes, 2FA, and recovery-code login exist. |
| User blocks | Implemented | Block/unblock/list are enforced in message and conversation flows. |
| Secure pairing | Implemented | Pairing codes, linked-device completion, token issuance, and audit events are implemented. |
| Key recovery | Implemented | Encrypted device key backup upload/list/get/restore/delete flows are implemented. |
| Audit logging | Implemented | Hash-chained security audit events now cover pairing, export/download, deletion finalize, key backup, and attestation actions. |

### 6. Relay & Carrier

| Feature | Status | Notes |
|---|---|---|
| Relay job lifecycle | Implemented | Create/list/get/accept/result are all implemented. |
| SMS/MMS relay | Implemented | Canonical transport policy now maps to required device capability. |
| Carrier message import | Implemented | Import/link/audit flow exists. |
| Relay device authorization | Implemented | Capability, SMS-role, last-seen, signature checks, and persisted device attestation enforcement are implemented. |
| Signed job verification | Partial | Stronger v2 acceptance signature is implemented; broader signing lifecycle remains limited. |
| Relay expiry enforcement | Implemented | Expired jobs are rejected on get/accept/result paths. |
| Message scheduling | Partial | Expiry is enforced, but delayed-send scheduling is not implemented. |
| RCS | Open | Not implemented. |
| Automatic transport fallback | Partial | Relay canonicalization handles SMS/MMS fallback within relay policy, not full cross-transport fallback. |

### 7. Data Management & Compliance

| Feature | Status | Notes |
|---|---|---|
| Message persistence | Implemented | PostgreSQL plus optional Cassandra read path remain in place. |
| Idempotency | Implemented | Endpoint-scoped response caching is implemented. |
| Event sourcing | Partial | Domain/user inbox events exist; snapshotting/versioning remain limited. |
| Account export | Implemented | Export now includes user state, devices, sessions, 2FA methods, recovery codes, deletion audit, conversations, and messages. |
| Account deletion | Implemented | Deletion audit, grace-period markers, export/download, finalize purge, credential revocation, and scrubbing are implemented in the gateway. |
| Message retention policies | Partial | Conversation retention and message expiry exist; broader retention automation/reporting is still open. |
| Cloud backup | Partial | Encrypted device key backup/restore is implemented; broader consumer cloud-backup productization remains open. |
| GDPR portability/compliance | Partial | Export and deletion are materially improved, but broader compliance automation is still incomplete. |

---

## Remaining High-Value Gaps

### Highest Priority Remaining

1. Media pipeline completion
   - Image transcoding, thumbnails, video/GIF support, encrypted attachments
2. Calling
   - Voice/video signaling, call state, missed-call handling
3. Security hardening
   - Direct vendor-proof attestation parsing and operational verifier deployment
4. Conversation UX depth
   - Richer moderation and client-side reaction/effect semantics
5. Compliance/documentation polish
   - Export/download workflow, purge workflow, operational/runbook coverage

### Notable Non-Blocking Gaps

- Reaction/effect animation UX
- SSE fallback
- RCS support
- Advanced search indexing/ranking
- Richer presence states

---

## Recommended Next Tranches

### Tranche A: Media Parity

- Add thumbnails and responsive variants
- Add basic video message ingest/playback
- Document attachment encryption target state

### Tranche B: Calling

- Add signaling and call invitation state
- Add missed-call history and notification wiring

### Tranche C: Security Hardening

- Move verifier-backed attestation to direct vendor-proof parsing if required by deployment
- Add remaining compliance/reporting automation around retention and restore runbooks

---

## Source of Truth

This document is aligned to the gateway implementation and tests as of March 20, 2026. Primary code paths include:

- `services/gateway/internal/messages`
- `services/gateway/internal/conversations`
- `services/gateway/internal/realtime`
- `services/gateway/internal/auth`
- `services/gateway/internal/discovery`
- `services/gateway/internal/users`
- `services/gateway/internal/relay`
- `services/gateway/internal/openapi/openapi.yaml`

Media and calling remain intentionally outside the completed scope summarized above.
