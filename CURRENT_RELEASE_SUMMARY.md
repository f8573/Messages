# Current Release Summary

Date: 2026-04-16

This document captures the current version's completed implementations, the items intentionally excluded from this release, and the remaining release to-do list before signoff.

## Release Scope

This release is centered on the core OTT messaging stack and its supporting backend services.

Included in scope:
- Gateway service and processor stack
- Web client foundation
- Core identity, conversation, messaging, media, sync, and realtime flows

Intentionally excluded from this release:
- Embedded apps deployment
- Android SMS/MMS client execution
- Linked-device relay execution on Android
- iOS client work

## Completed Implementations In The Current Version

- Core OTT messaging: direct and group conversations, authenticated send and receive flows, message persistence, and timeline retrieval.
- Message interaction features: edit, react, delete, pin, read receipts, typing indicators, presence, and sync/realtime convergence.
- Identity and session management: phone OTP authentication, token refresh rotation, logout, recovery codes, and multi-device session handling.
- Conversation management: create, list, update, membership changes, invite flows, and conversation policy/settings endpoints.
- Privacy and safety controls: hash-based contact discovery, user blocking, rate-limiting foundations, and abuse-control surfaces.
- Device management: device registration, listing, revocation, device-key publication and claim flows, and linked-device session support.
- Media pipeline foundation: attachment registration, upload/download token flows, and media retrieval APIs.
- Backend runtime stack: gateway, Kafka-backed message and delivery processors, SMS processor service, PostgreSQL, Redis, Cassandra, health checks, and basic observability endpoints.
- Web client foundation: authentication, conversation list/thread flows, responsive layout, and basic OTT messaging UX.
- Release hardening already landed for mini-app isolation: origin isolation, CSP integration, and related runtime-security work are implemented in-repo.

## Implemented But Deferred From Deployment

- Embedded apps / mini-app runtime: runtime, bridge, registry flows, and example apps are implemented in the repository.
- The apps service is intentionally not part of the current deployment and should remain deferred to a later release.
- Release messaging should describe embedded apps as implemented-but-not-shipping in this cut.

## Known Partial Or Excluded Areas

- Android SMS/MMS: backend/schema work exists, but the Android client and end-to-end carrier flows are not release-complete.
- Linked-device relay: server-side routing exists, but the Android relay executor and full UX are not release-complete.
- Rich text, full threading/replies UX, thumbnail generation, chunked uploads, scheduled purge jobs, passkeys/WebAuthn, and iOS remain future work.
- Account deletion and some export/erasure behaviors should be treated as partial or degraded-mode flows during release validation.

## Release To-Do List

- Run the automated gates from the staging checklist:
  - `npm run test:integration`
  - `npm run test:e2e`
  - `npm run test:live`
  - `npm run test:perf`
  - `npm run test:staging` if staging automation is enabled
- Complete manual staging coverage for in-scope flows:
  - OTP signup, refresh rotation, logout, and recovery-code access
  - DM and group messaging, edit/react/delete flows, mute/pin/block, and reconnect behavior
  - Multi-device link, revoke, and session consistency checks
  - Media upload/download and retry-after-failure paths
- Perform one soak run with sustained messaging, reconnects, group churn, and live metrics review.
- Verify health and readiness endpoints, request IDs in logs, and sane latency/error-rate behavior during staging.
- Confirm release notes explicitly call out the deferred embedded-app rollout and the exclusion of Android SMS/MMS and Android relay execution from this version.
- Record any degraded-mode or partially verified flows in the signoff notes before release approval.
- Prepare rollout and rollback steps for the gateway and processor stack used by the current deployment.

## Documentation Alignment To Resolve Before Release Notes

- Search status is inconsistent across the repository:
  - `IMPLEMENTATION_STATUS.md` still lists message search as unimplemented/partial.
  - `DEPLOYMENT_CHECKLIST.txt` describes search as ready for deployment.
  - Release notes should not claim search as shipped until staging verification and document alignment are complete.
- E2EE status is also inconsistent in `IMPLEMENTATION_STATUS.md`:
  - Section 14 marks direct-message and group E2EE as implemented.
  - Later summary sections still describe E2EE as future or out of scope.
  - Release notes should use one reconciled position before publication.

## Suggested Release Framing

Recommended summary for this cut:
- Ship the OTT messaging core, web client foundation, backend processors, media flows, and core account/device/conversation capabilities.
- Hold embedded apps deployment for a later release even though much of the underlying implementation is already present.
- Treat Android SMS/MMS, Android relay execution, and any unresolved documentation discrepancies as out of scope for this release.

## References

- [`IMPLEMENTATION_STATUS.md`](./IMPLEMENTATION_STATUS.md)
- [`DEPLOYMENT_CHECKLIST.txt`](./DEPLOYMENT_CHECKLIST.txt)
- [`testing/STAGING_CHECKLIST.md`](./testing/STAGING_CHECKLIST.md)
- [`ohmf/README.md`](./ohmf/README.md)
- [`ohmf/SESSION_COMPLETION_SUMMARY.txt`](./ohmf/SESSION_COMPLETION_SUMMARY.txt)
