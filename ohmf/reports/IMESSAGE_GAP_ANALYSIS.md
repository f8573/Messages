# iMessage Gap Analysis
**Date:** March 20, 2026  
**Scope:** Current remaining gaps after the gateway parity work merged

This document tracks what is still materially missing for iMessage-style parity after the implemented non-media, non-calling gateway slices landed. It supersedes the earlier pre-implementation gap list and focuses on the remaining work rather than already-closed items.

---

## Executive Summary

The gateway is no longer blocked on the original core messaging gaps. Typing, read receipts, effects, forwarding, pinning, richer conversation controls, realtime resume, account recovery, and relay hardening are now in place. The remaining gaps cluster into five buckets:

1. Media depth
2. Calling
3. Security hardening
4. Advanced conversation UX
5. Compliance/operational polish

---

## Remaining Major Gaps

### 1. Media Depth

**Status:** Still the largest parity gap outside calling.

Still missing or incomplete:

- Image transcoding, compression, and thumbnail generation
- Video message ingest/playback
- GIF/sticker pipelines
- Robust link preview extraction/caching
- Encrypted attachment flow for OTT/E2EE
- Rich attachment variants and responsive delivery

Why it matters:

- iMessage parity is not credible without polished image/video handling.
- Current attachment support is functional infrastructure, not product-complete media UX.

### 2. Calling

**Status:** Open.

Still missing:

- Voice/video signaling
- Call invitations and session state
- Missed-call handling
- Device ringing state and reconnect semantics
- Call history and notification integration

Why it matters:

- This remains a category-level gap versus iMessage and other modern messengers.

### 3. Security Hardening

**Status:** Improved materially, but not complete.

Implemented recently:

- OTP abuse controls
- Discovery rate limits and pepper versioning
- Recovery codes and 2FA
- Stronger relay accept signatures and device capability checks
- Secure pairing/link-new-device flow
- Encrypted device key backup/upload/list/restore/delete workflow
- Hash-chained security audit events for export/delete/pairing/key-backup actions
- Device attestation challenge/verify flow with persisted relay authorization state

Still missing:

- Direct vendor-proof parsing in the gateway itself is still optional; the current rollout verifies signed verdicts from an upstream attestation verifier.

Why it matters:

- The trust posture is materially stronger now, but a deployment that wants first-party Google/Apple proof validation inside the gateway would still need that extra integration.

### 4. Advanced Conversation UX

**Status:** Core controls are implemented; product depth is not.

Implemented recently:

- Admin roles
- Invite codes
- Ban/unban moderation
- Richer settings
- Rich text and mentions
- Message expiry
- Threads/replies
- Notification mute/archive enforcement at dispatch time
- Advanced search ranking/filtering
- Presence APIs backed by websocket session state

Still missing:

- Deeper moderation tooling
- Richer reaction/effect animation UX

Why it matters:

- The backend now supports most of the structural features, but not all of the expected conversation ergonomics.

### 5. Compliance and Operations Polish

**Status:** Much stronger than before, but not complete.

Implemented recently:

- Rich account export payload
- Deletion audit
- Grace-period deletion state
- Better lifecycle fields on users/messages/conversations
- Export packaging/download workflow
- Final purge/finalize deletion workflow
- Device key backup state and restore timestamps

Still missing:

- Automated retention enforcement outside message-path logic
- Broader compliance reporting/process documentation
- Backup/restore runbook documentation

Why it matters:

- The runtime model is now viable, but operational maturity is still behind the code surface.

---

## Minor or Secondary Gaps

- SSE fallback for restricted networks
- RCS support
- Presence richness beyond session-aware online/last-seen
- Full client documentation for newer websocket and relay behaviors
- OpenAPI detail depth for some dynamic payloads

---

## Suggested Next Order of Work

### Priority 1

- Media pipeline
- Calling/signaling

### Priority 2

- Compliance/reporting/runbook polish around deletion, retention, and backup restore

### Priority 3

- Deeper moderation tooling
- Richer client-side reaction/effect UX

---

## Relationship to Other Docs

- Use `reports/IMESSAGE_FEATURE_PARITY_ANALYSIS.md` for the current implemented-vs-partial-vs-open matrix.
- Use `PHASE_1_MVP_DEPLOYMENT_GUIDE.md` for the current deployed gateway scope and migration/config checklist.
