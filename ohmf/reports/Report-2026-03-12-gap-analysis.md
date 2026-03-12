# OHMF Platform – Gap Analysis Report
Date: 2026-03-12
Source spec: `OHMF_Complete_Platform_Spec_v1.md`

## Executive Summary — Top 5 High-Priority Gaps

- **Auth / Token lifecycle coverage gaps**: Partial implementation of phone-start/verify and token issuance exists but refresh/rotation, revocation, and hardened token validation are partially untested and missing CI checks. (Priority: High, Effort: 8–16h)
- **Message persistence & indexing parity with spec (server_order, Cassandra fallback)**: Message canonical schema exists in proto and code, but DB migrations, consistent use of Cassandra vs Postgres paths, and tests for server_order idempotency are partial. (Priority: High, Effort: 12–24h)
- **Android SMS/MMS and linked-device relay semantics**: Relay primitives and SMS constants exist, but end-to-end linked-device security model, permissions checks, and Android integration test harnesses are missing. (Priority: High, Effort: 20–40h)
- **Mini-app manifest signing verification and sandboxing**: Basic manifest verification exists but no sandbox runtime enforcement, permission model tests, or manifest schema JSON schema validation. (Priority: Medium, Effort: 12–24h)
- **OpenAPI / contract tests missing in CI and spec coverage gaps**: OpenAPI specs exist under protocol and gateway but there is no automated CI job that validates deployed API surface and schema drift. (Priority: Medium, Effort: 4–8h)

---

## Scope and Method

I scanned the platform specification at the repository root and relevant code under `ohmf/`, `services/`, `packages/protocol/`, and `tools/validate_openapi`. For each normative section in the spec I recorded implementation status, concrete file references, tests, remediation plans, risk flags, and prioritized fixes with estimated effort.

Source pointers used during analysis (examples):

- Spec file: [OHMF_Complete_Platform_Spec_v1.md](OHMF_Complete_Platform_Spec_v1.md)
- Protocol proto: [ohmf/packages/protocol/proto/envelope.proto](ohmf/packages/protocol/proto/envelope.proto#L1-L50)
- OpenAPI canonical: [ohmf/packages/protocol/openapi/openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml#L1-L100)
- Gateway OpenAPI embed: [ohmf/services/gateway/internal/openapi/openapi.yaml](ohmf/services/gateway/internal/openapi/openapi.yaml#L1-L80)
- Gateway API entrypoint: [ohmf/services/gateway/cmd/api/main.go](ohmf/services/gateway/cmd/api/main.go#L1-L120)
- Messaging code: [ohmf/services/gateway/internal/messages/service.go](ohmf/services/gateway/internal/messages/service.go#L1-L120)
- Protobuf / Envelope model in code: [ohmf/services/gateway/internal/messages/envelope.go](ohmf/services/gateway/internal/messages/envelope.go#L1-L160)
- OpenAPI validator tool: [tools/validate_openapi/main.go](tools/validate_openapi/main.go#L1-L43)
- Integration tests (gateway): [ohmf/services/gateway/integration/mvp_flow_test.go](ohmf/services/gateway/integration/mvp_flow_test.go#L1-L120)

---

## Per-Spec Section Analysis (numbered as in spec)

Notes on grading: "Implemented" = code implements required normative features and includes tests; "Partially implemented" = core feature exists but missing tests/edges/CI; "Missing" = no implementation found under targeted folders.

### 1. Purpose and Scope
- Status: Implemented (documentation-level) — repository contains spec and README artifacts.
- Evidence: [OHMF_Complete_Platform_Spec_v1.md](OHMF_Complete_Platform_Spec_v1.md#L1-L40)
- Tests: N/A
- Remediation: None.

### 2. Conformance and Normative Language
- Status: Implemented (spec doc only).
- Evidence: Spec header uses MUST/SHOULD normative language.
- Tests: N/A

### 3. Design Principles
- Status: Implemented (spec doc only).

### 4. Terminology
- Status: Implemented (spec doc only).

### 5. High-Level Architecture
- Status: Partially implemented.
- Evidence: Services and packages present (`ohmf/services/*`, `ohmf/packages/*`), gateway and processors exist: [ohmf/services/gateway/](ohmf/services/gateway/cmd/api/main.go#L1-L120), [services/messages-processor](ohmf/services/messages-processor/cmd/processor/main.go#L1-L120).
- Tests: Integration tests exist for gateway basic flows ([ohmf/services/gateway/integration/openapi_test.go](ohmf/services/gateway/integration/openapi_test.go#L1-L30), [mvp_flow_test.go](ohmf/services/gateway/integration/mvp_flow_test.go#L1-L120)).
- Gaps & Remediation: Missing architecture-level diagrams and cross-service contract tests verifying topics, schema versions and ingress/persisted topics. Add cross-service integration test verifying envelope payloads on Kafka topics and schema compatibility.
  - Suggested files to add: `ohmf/reports/arch/diagrams.md`, `ohmf/tests/integration/contract_test.go`.
  - Sample test patch (pseudo):

```patch
*** Add File: ohmf/tests/integration/contract_test.go
+package integration
+
+import (
+  "testing"
+  "ohmf/packages/protocol/proto"
+)
+
+func TestEnvelopeSchemaCompatibility(t *testing.T) { /* load proto, validate outgoing envelope payloads */ }
```

  - Priority: Medium, Effort: 6–12h.

### 6. Product Operating Modes
- Status: Partially implemented.
- Evidence: Gateway supports smoke mode in [ohmf/services/gateway/cmd/api/main.go#L1-L120]; transport modes appear in messages lifecycle and transport enums ([lifecycle.go](ohmf/services/gateway/internal/messages/lifecycle.go#L1-L68)).
- Gaps: Mode toggles for linked-device relay, SMS default handler, and client-mode flags are not globally documented or controlled via a common runtime config. Add standardized runtime flags and feature gates in `config`.
  - Suggested change: add `config.Mode` enumerations and tests.
  - Priority: Low, Effort: 4–8h.

### 7. Identity and Account Model
- Status: Partially implemented.
- Evidence: Phone-based auth flows exist under [ohmf/services/gateway/internal/auth](ohmf/services/gateway/internal/auth/handler.go#L1-L90) and [service.go](ohmf/services/gateway/internal/auth/service.go#L1-L120). Token generation/refresh is present (token package referenced) though token code not fully inspected here.
- Tests: Integration tests exercise phone start/verify flows ([mvp_flow_test.go](ohmf/services/gateway/integration/mvp_flow_test.go#L1-L80)). Unit tests for auth service appear limited/missing.
- Gaps:
  - Missing: explicit refresh token revocation, token rotation, device registry synchronization with account model, and tests for token expiry edge-cases.
  - Remediation Plan:
    - Add unit tests for `auth.Service.Refresh` and token parsing edge cases under `ohmf/services/gateway/internal/auth/auth_test.go`.
    - Add DB table for `device_tokens` with refresh token versioning and revocation flag and a small migration in `ohmf/services/gateway/internal/db/migrations/000002_refresh_tokens.up.sql`.
    - Sample pseudo-SQL migration snippet:

```sql
-- ohmf/services/gateway/internal/db/migrations/000002_refresh_tokens.up.sql
CREATE TABLE device_refresh_tokens (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL,
  device_id UUID NOT NULL,
  refresh_token_hash TEXT NOT NULL,
  revoked_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ DEFAULT now()
);
```

    - Priority: High, Effort: 8–16h.

  - Tests to add: unit tests for `VerifyPhone` edge cases, integration test verifying access token expiry and refresh flow.
  
  - CI check: add test job to run `go test ./...` and run token parsing fuzzing for `token` package.

### 8. Device Model
- Status: Partially implemented.
- Evidence: Device registration fields exist in `auth.VerifyRequest` (device.platform, device.device_name, public_key) ([ohmf/services/gateway/internal/auth/service.go#L1-L60]). Device service referenced in main imports ([ohmf/services/gateway/cmd/api/main.go#L1-L40]).
- Gaps: No centralized device registry API surfaced in handlers for listing or revoking devices; device-to-user mapping persistence locations are not obvious. Add device CRUD endpoints and DB tables, unit tests.
  - Priority: Medium, Effort: 6–12h.

### 9. Contact Discovery
- Status: Partially implemented.
- Evidence: A separate contacts service exists with a discover handler and seed function ([ohmf/services/contacts/main.go#L1-L120]). Protocol schema for contact discovery exists: [packages/protocol/openapi/openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml#L1-L80).
- Tests: No tests observed for the contacts service in repo (no `*_test.go` in that service). Integration tests do not exercise contact discovery.
- Gaps & Remediation:
  - Add unit tests for `discoverHandler` and security review for hashing algorithm (use HMAC with per-deployment pepper stored in config rather than embedding 'pepper-01').
  - Replace static pepper with environment-configured value and rotate option.
  - Priority: Medium, Effort: 4–8h.

### 10. Conversation Model
- Status: Implemented (core features present).
- Evidence: Conversation creation, listing, phone DMs implemented in [ohmf/services/gateway/internal/conversations/service.go](ohmf/services/gateway/internal/conversations/service.go#L1-L120) and handlers in [handler.go](ohmf/services/gateway/internal/conversations/handler.go#L1-L100).
- Tests: Integration tests exercise conversations in `mvp_flow_test.go` (create, list, messages retrieval) ([ohmf/services/gateway/integration/mvp_flow_test.go#L1-L120]).
- Gaps: Thread key handling (`ThreadKeys` field) exists but KMS or thread key rotation implementation not present. Add explicit key management or reference to external KMS.
  - Priority: Medium, Effort: 8–16h.

### 11. Message Model
- Status: Partially implemented.
- Evidence:
  - Canonical message proto: [ohmf/packages/protocol/proto/envelope.proto](ohmf/packages/protocol/proto/envelope.proto#L1-L50).
  - In-code canonical `MessageRecord` and `Envelope` in [ohmf/services/gateway/internal/messages/envelope.go](ohmf/services/gateway/internal/messages/envelope.go#L1-L160).
  - Service-level `Message` struct and persistence logic in [messages/service.go](ohmf/services/gateway/internal/messages/service.go#L1-L120).
- Tests: Integration tests verify idempotency and server_order behavior in `mvp_flow_test.go` lines 1-120.
- Gaps:
  - Missing: tests that validate serialization parity between proto and JSON envelope representation, and end-to-end schema compatibility when messages flow through Kafka topics and are persisted to Postgres/Cassandra.
  - Remediation:
    - Add `proto` <-> JSON roundtrip tests under `packages/protocol` that serialize `MessageRecord` and assert field matching.
    - Ensure `Envelope` spec_version enforcement and validation on ingress; add JSON schema validators.
  - Priority: High, Effort: 8–16h.

### 12. Transport Model
- Status: Partially implemented.
- Evidence: Transport constants and lifecycle states in [lifecycle.go](ohmf/services/gateway/internal/messages/lifecycle.go#L1-L68) and message service routing logic that maps `OTT` to `OHMF` in [messages/handler.go](ohmf/services/gateway/internal/messages/handler.go#L1-L120).
- Gaps: Formal transport negotiation (AUTO policy enforcement), carrier send fallbacks, and transport policy enforcement lacking tests.
  - Remediation: Implement transport decision module used by `messages.Service` (new file `ohmf/services/gateway/internal/transport/decision.go`) with unit tests.
  - Priority: High, Effort: 8–16h.

### 13. Envelope and Event Model
- Status: Implemented (core present).
- Evidence:
  - Protobuf envelope file: [ohmf/packages/protocol/proto/envelope.proto](ohmf/packages/protocol/proto/envelope.proto#L1-L50).
  - JSON Envelope builder: [ohmf/services/gateway/internal/messages/envelope.go](ohmf/services/gateway/internal/messages/envelope.go#L1-L160) and publishing in async pipeline ([async.go](ohmf/services/gateway/internal/messages/async.go#L1-L120)).
- Tests: Missing explicit proto vs JSON compatibility tests.
- Remediation: Add tests that validate `BuildEnvelopeFromIngressEvent` payload matches the proto schema and add CI check to run `protoc --go_out=...` or `go vet` style checks.
  - Priority: Medium, Effort: 6–10h.

### 14. Message Lifecycle
- Status: Implemented.
- Evidence: Lifecycle constants and transition validator: [ohmf/services/gateway/internal/messages/lifecycle.go](ohmf/services/gateway/internal/messages/lifecycle.go#L1-L68). Lifecycle state transitions enforced in processors and delivery flow (delivery-processor, messages-processor).
- Tests: No unit tests for `IsValidLifecycleTransition` observed. Integration coverage exercises end states through delivery processors indirectly.
- Remediation: Add unit tests for lifecycle transitions under `ohmf/services/gateway/internal/messages/lifecycle_test.go`. Add CT tests that assert invalid transitions are rejected.
  - Priority: Medium, Effort: 2–4h.

### 15. Editing, Reactions, Receipts, and Presence
- Status: Partially implemented.
- Evidence: Read receipts model `ReadReceipt` present in proto; `delivery` and `persisted` flows in delivery-processor; `realtime/ws.go` has presence subscription and markPresence usage. Message editing exists as `Redact` in [messages/service.go](ohmf/services/gateway/internal/messages/service.go#L1-L120) but editing (update content) endpoint not found.
- Tests: No tests found for presence, reactions, or editing flows.
- Gaps & Remediation:
  - Add APIs & handlers for `edit`, `react`, `receipt` updates in `messages` and `realtime` modules.
  - Add DB columns for `edited_at_unix_ms` and queries for receiving edits.
  - Priority: Medium, Effort: 12–24h.

### 16. Blocking, Visibility, and Client Nicknames
- Status: Missing / Partial.
- Evidence: Visibility state used in MessageRecord and Redact sets `visibility_state = 'REDACTED'` in service, but no user-level blocklist or nickname APIs found.
- Remediation: Implement `blocking` service and `nicknames` fields in `conversation_members` or `user_profiles` with APIs and tests.
  - Priority: Medium, Effort: 10–20h.

### 17. Privacy-Aware Deletion and Data Erasure
- Status: Partially implemented.
- Evidence: `Redact` exists in [messages/service.go](ohmf/services/gateway/internal/messages/service.go#L1-L120) which empties content; DB queries appear to set redacted_at. No user-level erase endpoint or documented erasure workflow observed.
- Gaps:
  - No bulk data erasure for user accounts, no retention policies enforcement, no erasure audit logs.
  - Remediation: Add `EraseAccount` service, write migration schema for erasure markers, and add integration test that performs deletion and validates data removal.
  - Priority: High, Effort: 16–32h.

### 18. Media and Attachment Handling
- Status: Partially implemented.
- Evidence: Media service stub exists with upload init and completion handlers ([ohmf/services/media/main.go](ohmf/services/media/main.go#L1-L120)). OpenAPI has `MediaUploadInitRequest` schemas ([packages/protocol/openapi/openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml#L1-L80)). Gateway references `internal/media` package.
- Tests: No media integration tests observed beyond the simple media server.
- Gaps & Remediation:
  - Need signed upload URLs, size/mime-type validation, virus scanning pipeline, and retention policy enforcement.
  - Suggested additions: `ohmf/services/media`: add `POST /v1/media/uploads/init` that returns pre-signed upload URLs, and a worker that verifies content and stores metadata in DB.
  - Priority: Medium, Effort: 12–24h.

### 19. Backend Services (Gateway, Auth, Users, Conversations, Messages, Relay, Presence, Notification, Media, Mini-App, Abuse)
- Status: Partially implemented.
- Evidence: Gateway and many services exist under `ohmf/services/` including `gateway`, `auth`, `conversations`, `media`, `relay`, `miniapp`, `contacts`, `apps`. The `abuse` and `notification` workers referenced but not fully examined here.
- Tests: Integration tests exercise core auth -> conversation -> message flows. Many service-level unit tests are missing.
- Gaps: Some services are light-weight or mock-like (apps, media, contacts) and lack production-grade validation, authentication guards, and tests. Add coverage and hardened implementations.
  - Priority: High, Effort: 24–48h across services (staggered).

### 20. Data Storage Architecture
- Status: Partially implemented.
- Evidence: Postgres pool helper, migrations loader, Cassandra store, and Selective use of Cassandra exists ([db/postgres.go](ohmf/services/gateway/internal/db/postgres.go#L1-L40), [messages/cassandra_store.go](ohmf/services/gateway/internal/messages/cassandra_store.go#L1-L120)). Migration application only reads `000001_init.up.sql` per code — additional migrations are not auto-discovered.
- Gaps:
  - Migration runner limited: `ApplyMigrations` reads a single file rather than running a directory of ordered migrations.
  - Remediation: Replace `ApplyMigrations` to iterate and run migrations from a folder (or integrate with `golang-migrate`).
  - Priority: High, Effort: 4–8h.

### 21. Realtime Architecture
- Status: Partially implemented.
- Evidence: WebSocket handler: [ohmf/services/gateway/internal/realtime/ws.go](ohmf/services/gateway/internal/realtime/ws.go#L1-L120) with presenceTTL and send loops.
- Tests: No WS integration tests found.
- Gaps & Remediation: Add WS integration tests (connect, subscribe, send message via WS), add CI job for WS smoke tests.
  - Priority: Medium, Effort: 6–12h.

### 22. REST API
- Status: Implemented (surface present) but contract validation gaps.
- Evidence: OpenAPI yaml under gateway and protocol packages; handler to serve OpenAPI in [ohmf/services/gateway/internal/openapi/handler.go](ohmf/services/gateway/internal/openapi/handler.go#L1-L16) and validator CLI at [tools/validate_openapi/main.go](tools/validate_openapi/main.go#L1-L43).
- Tests: `integration/openapi_test.go` ensures YAML served; no automated CI job to validate spec vs handlers.
- Remediation: Add CI step to run `tools/validate_openapi` against the embedded `openapi.yaml` and to run schema-based contract tests matching server responses to OpenAPI schemas.
  - Priority: Medium, Effort: 4–8h.

### 23. WebSocket Protocol
- Status: Partially implemented.
- Evidence: `realtime/ws.go` implements subscribe and send semantics and uses simple envelope payloads ([ohmf/services/gateway/internal/realtime/ws.go#L1-L120]).
- Gaps: No formal OpenAPI or Protobuf/Ws definition; add schema artifacts under `ohmf/packages/protocol/openapi` or `packages/protocol/schemas/ws-*.json` and enforce validation via middleware (there is JSON schema middleware in `middleware/validation.go`).
  - Priority: Medium, Effort: 6–12h.

### 24. Protobuf Transport Schemas
- Status: Implemented.
- Evidence: `ohmf/packages/protocol/proto/envelope.proto` contains Envelope, MessageRecord, ReadReceipt, RelayJob definitions ([ohmf/packages/protocol/proto/envelope.proto#L1-L50]).
- Tests: Missing proto compile checks in CI and grpc/proto roundtrip tests.
- Remediation: Add `make proto` or `go generate` step and CI lint to compile the proto and run generated code basic tests.
  - Priority: Medium, Effort: 4–8h.

### 25. Database Schema
- Status: Partially implemented.
- Evidence: `ApplyMigrations` expects `000001_init.up.sql` but repository contains multiple SQL files in migrations directories (not enumerated here). `messages/service.go` and others rely on DB schema.
- Gaps: Migration runner needs extension and schema versioning; tests missing for migrations.
  - Remediation: Use `golang-migrate` and include a test that runs migrations in CI against ephemeral Postgres.
  - Priority: High, Effort: 6–12h.

### 26. Client Sync Model
- Status: Partially implemented.
- Evidence: Realtime subscription, `sync` package referenced in main imports ([ohmf/services/gateway/cmd/api/main.go#L1-L40]) but `sync` implementation not fully enumerated in this pass.
- Gaps: No explicit long-poll/sync endpoints for incremental sync or checkpoints; recommend adding documented `sync` endpoints and tests.
  - Priority: Medium, Effort: 8–16h.

### 27. Android SMS/MMS Integration
- Status: Partially implemented (server-side relay primitives present).
- Evidence: Relay service and SMS lifecycle states exist (`relay/service.go`, lifecycle constants in `messages/lifecycle.go`). SMS-specific statuses `SENT_TO_MODEM` etc. exist.
- Gaps: No Android test harness, no Android client code paths, no documented security model for linked-device relay (attestation, auth between device and server).
  - Remediation: Define and implement device attestation handshake (e.g., single-use tokens + signed relay jobs). Add relay acceptance policy checks in `relay/service.go` and document required Android-side protocol.
  - Priority: High, Effort: 20–40h.

### 28. Linked-Device Relay
- Status: Partial (relay endpoints exist: [ohmf/services/gateway/internal/relay/handler.go](ohmf/services/gateway/internal/relay/handler.go#L1-L100) and service implementation [relay/service.go](ohmf/services/gateway/internal/relay/service.go#L1-L120)).
- Gaps: Missing device authentication for accept/claim operations, job ACLs, signing of job results.
  - Remediation: Add device auth middleware and job claim verification, add `device` credential storage, and add tests for accept/complete flows.
  - Priority: High, Effort: 12–24h.

### 29. Unified Conversation Rendering
- Status: Missing (presentation concerns are client-side; server provides content but no explicit rendering rules).
- Evidence: Message content types exist (`content_type`, `content`), but spec-level rendering rules not implemented.
- Remediation: Provide a `rendering` guidance doc and normalize content types (text, markdown, html, attachments) with server-side sanitization for HTML.
  - Priority: Low, Effort: 6–12h.

### 30–31. Mini-App Platform & SDK
- Status: Partially implemented.
- Evidence: `miniapp` service for manifest registration and session creation exists ([ohmf/services/gateway/internal/miniapp/service.go](ohmf/services/gateway/internal/miniapp/service.go#L1-L120) and handler.go). Manifest signature verification exists (RSA).
- Gaps: No runtime sandbox, no permission enforcement at runtime, no manifest schema validation beyond presence of `signature` field, and no CI tests.
  - Remediation: Add manifest JSON Schema under `ohmf/packages/protocol/openapi` and JSON schema validation in `miniapp/handler.go` using the middleware JSON schema system. Add a simple sandboxed runner for mini-apps or document runtime requirements. Add unit tests for manifest verification and signature acceptance/rejection.
  - Priority: Medium, Effort: 12–24h.

### 32. Security and Abuse Controls
- Status: Partial.
- Evidence: `abuse` package referenced in main, rate-limiting in auth, and rate limiter usage in messages and realtime. Some worker implementations for abuse aggregation referenced in runner (`wk.NewAbuseAggregatorWorker`).
- Gaps: Missing end-to-end abuse policies, lack of CI for fuzzing endpoints, lack of automated abuse-data retention policy enforcement.
  - Remediation: Add abuse test harness, add alerting for suspicious behavior, and export metrics for anomaly detection. Add unit tests for rate limiter boundary conditions.
  - Priority: High, Effort: 16–32h.

### 33. Observability and Operations
- Status: Partially implemented.
- Evidence: Observability helper package and RequestID middleware referenced across services (e.g., `observability.NewLogger` in main). Workers and services log events.
- Gaps: No Prometheus metrics, no structured tracing instrumentation (trace IDs are present but not exported to a tracing backend), and no CI smoke tests for observability endpoints.
  - Remediation: Add Prometheus metric exports, integrate with OpenTelemetry SDK for traces, and add a `healthz` and `/metrics` check test in integration suite.
  - Priority: Medium, Effort: 12–24h.

### 34. Versioning and Compatibility
- Status: Partial.
- Evidence: Protobuf `spec_version` field and OpenAPI versioning present. No automated compatibility checks.
- Gaps: No CI gate that ensures new proto or openapi changes bump spec_version and pass compatibility checks.
  - Remediation: Add a CI step to validate backward/forward compatibility of OpenAPI and Protobuf (e.g., `oapi-codegen`, `buf` or `protoc` checks).
  - Priority: Medium, Effort: 6–10h.

### 35. Repository Layout
- Status: Implemented.
- Evidence: Repository layout aligns with spec: `ohmf/`, `services/`, `packages/`, `tools/`.

### 36–39. Roadmap, Threat Model, Diagrams, Appendices
- Status: Missing / Partial.
- Evidence: Reports directory and README exist, but formal threat model and diagrams are absent.
- Remediation: Add `ohmf/reports/threat_model.md`, `ohmf/reports/diagrams/` and link to CI checks and deliverables.
  - Priority: Low, Effort: 6–12h.

---

## Cross-Cutting Discrepancies, Risks, and Remediation (detailed)

1) Auth Token Lifecycle and Revocation (High)
- Status: Partially implemented — phone start/verify and refresh handlers exist ([auth/handler.go](ohmf/services/gateway/internal/auth/handler.go#L1-L93), [auth/service.go](ohmf/services/gateway/internal/auth/service.go#L1-L120)).
- Risk: Without refresh token revocation and device-level revocation, compromised refresh tokens allow persistent account takeover. Lack of test coverage increases regression risk.
- Remediation Plan:
  - Add DB table `device_refresh_tokens` (see SQL earlier) and enforce issuance to stored token hashes.
  - Change `auth.Service.VerifyPhone` to store refresh token record with hash, and `Refresh` to validate existence and revocation status; add `Logout` to mark token revoked.
  - Add unit tests in `ohmf/services/gateway/internal/auth/auth_test.go` for: expired refresh token, revoked refresh token, token rotation.
  - Add CI job running new tests and a smoke test that exercises login -> refresh -> revoke.
- Suggested files to modify/add:
  - `ohmf/services/gateway/internal/db/migrations/000002_refresh_tokens.up.sql`
  - `ohmf/services/gateway/internal/auth/auth_test.go`
  - update `auth/service.go` token handling code.
- Priority: High. Est. Effort: 8–16 hours.

2) Message persistence ordering and dual-store consistency (High)
- Status: Partial. Code paths exist for Postgres and Cassandra, but orchestration between them and tests for idempotency/server_order are incomplete.
- Risk: Divergent reads (Postgres vs Cassandra) may return inconsistent timelines; server_order race conditions may cause message reordering.
- Remediation Plan:
  - Introduce a single canonical writer path that writes Postgres first (transactional increment of `conversation_counters` for server_order) and then writes to Cassandra as eventual read store. Add compensating operations on writer failure.
  - Add integration test `ohmf/services/gateway/integration/server_order_test.go` that posts many concurrent messages with same idempotency key and asserts server_order monotonicity.
  - Suggested modification points: `messages.Service.Send` implementation (add atomic server_order assignment), `messages/cassandra_store.go` ensure eventual write.
  - Sample pseudo-patch (high-level):

```patch
*** Update File: ohmf/services/gateway/internal/messages/service.go
@@
 func (s *Service) Send(...) (SendResult, error) {
-  // existing optimistic path
+  // new: begin tx; SELECT next_server_order FROM conversation_counters WHERE conversation_id = $1 FOR UPDATE; increment and persist server_order; insert message; commit tx; then async write to cassandra
 }
```

- Priority: High. Est. Effort: 12–24 hours.

3) Linked-device relay security (High)
- Status: Partially implemented — relay handlers exist but lack device auth and attestation.
- Risk: A device could claim and accept relay jobs without proof of identity, enabling malicious SMS sends or carrier abuse.
- Remediation Plan:
  - Implement device credentials (public key per device) and sign/verify accept requests. Store device public keys in `devices` table.
  - Add middleware to verify device signatures for `relay.Accept` and `relay.CreateMessage` when device actor is used.
  - Add integration tests covering accept/complete flows including failure cases such as unsigned accept.
- Priority: High. Est. Effort: 12–24 hours.

4) Mini-app manifest validation and runtime (Medium)
- Status: Partial — verify signature uses RSA public key if configured ([miniapp/service.go](ohmf/services/gateway/internal/miniapp/service.go#L1-L120)).
- Risk: Missing schema validation and runtime permission enforcement could allow malicious or malformed mini-apps.
- Remediation Plan:
  - Add JSON Schema for manifest (e.g., `ohmf/packages/protocol/openapi/miniapp_manifest.schema.json`) and validate on register.
  - Implement a `miniapp/permission` evaluation that verifies requested permissions against manifest and owner profile.
  - Add unit tests for signature verification and schema rejection paths.
- Priority: Medium. Est. Effort: 12–24 hours.

5) OpenAPI test and CI enforcement (Medium)
- Status: Implemented (validator tool exists) but not enforced in CI.
- Risk: API drift between OpenAPI YAML and implemented routes may break clients.
- Remediation Plan:
  - Add workflow (CI) step to run `go run tools/validate_openapi -spec=ohmf/services/gateway/internal/openapi/openapi.yaml` and fail on validation errors.
  - Add integration contract tests that POST/GET against endpoints and assert shapes using the OpenAPI schema.
- Priority: Medium. Est. Effort: 4–8 hours.

---

## Minimal Safe Steps for High-Priority Gaps (actionable)

1) Auth token lifecycle (High) — Minimal safe steps:
- Add DB migration to create `device_refresh_tokens` table.
- Modify `auth.Service.VerifyPhone` to write refresh token hash and device id to the table.
- Modify `auth.Service.Refresh` to check revocation flag prior to issuing new access tokens.
- Add unit tests for `Refresh` and `Logout`.
- CI: add `go test ./...` and run `tools/validate_openapi`.

2) Message persistence ordering (High) — Minimal safe steps:
- Implement server_order assignment in Postgres inside a `FOR UPDATE` transaction and atomically increment `conversation_counters` (quick change in `messages.Service.Send`).
- Add concurrency integration test that simulates concurrent sends and verifies monotonic server_order.

3) Linked-device relay security (High) — Minimal safe steps:
- Require `DeviceID` and device signature header for `relay.Accept` and `relay.CreateMessage` endpoints.
- Add device public-key verification utility in `relay` package and middleware to assert signature validity.

4) Privacy/Data erasure (High) — Minimal safe steps:
- Add `EraseAccount` endpoint and mark records with `erased_at` timestamp rather than immediate physical deletion; implement background erasure worker to scrub PII.
- Add test asserting that after erase, sensitive columns (phone, content) are redacted for the user.

## Test Additions (examples)

- `ohmf/services/gateway/internal/auth/auth_test.go` — tests for refresh token revocation and rotation.
- `ohmf/services/gateway/integration/server_order_test.go` — concurrent message sends asserting monotonic server_order.
- `ohmf/services/gateway/internal/messages/lifecycle_test.go` — unit tests for `IsValidLifecycleTransition`.
- `ohmf/services/gateway/integration/ws_smoke_test.go` — websocket connect/subscribe/send roundtrip.

Example unit test snippet for lifecycle:

```go
package messages

import "testing"

func TestValidTransitions(t *testing.T) {
  if !IsValidLifecycleTransition(LifecycleQueued, LifecycleAccepted) {
    t.Fatalf("expected queued -> accepted to be valid")
  }
  if IsValidLifecycleTransition(LifecycleDelivered, LifecycleQueued) {
    t.Fatalf("unexpected transition allowed")
  }
}
```

## CI / Automation Recommendations

- Add GitHub Actions or pipeline job steps:
  - `go test ./...` (unit + integration where possible)
  - `go vet` and `golangci-lint` for linting
  - `tools/validate_openapi` run against embedded `openapi.yaml`
  - `protoc` compile for `packages/protocol/proto/*.proto`
  - Migration smoke tests: spin up ephemeral Postgres, apply migrations and run minimal integration tests.

## Recommended Branch and Commit Message Template

- Branch name: `fix/spec-compliance/<short-description>` e.g. `fix/spec-compliance/auth-refresh-revoke`
- Commit message template:

```
<scope>: <short summary>

Detailed description of change, mapping to spec section(s):
- Spec section(s): 7 (Identity), 14 (Lifecycle)
- Files changed: <list>

Test plan: run unit tests and integration smoke tests
```

## Final Notes, Prioritization Matrix

- Immediate (High): Auth token lifecycle, message ordering and persistence, linked-device relay security, data erasure.
- Near-term (Medium): Mini-app runtime hardening, OpenAPI CI, proto compile and CI checks, migration runner improvement.
- Long-term (Low): Presentation/rendering guidelines, architecture diagrams, extended observability.

If you want, I can now:
- create the suggested DB migration and example test files, or
- open PR branches with the minimal patches for one of the high-priority items.

---

Appendix: Quick file index of references used during analysis

- [OHMF_Complete_Platform_Spec_v1.md](OHMF_Complete_Platform_Spec_v1.md)
- [ohmf/packages/protocol/proto/envelope.proto](ohmf/packages/protocol/proto/envelope.proto#L1-L50)
- [ohmf/packages/protocol/openapi/openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml#L1-L100)
- [ohmf/services/gateway/cmd/api/main.go](ohmf/services/gateway/cmd/api/main.go#L1-L120)
- [ohmf/services/gateway/internal/messages/envelope.go](ohmf/services/gateway/internal/messages/envelope.go#L1-L160)
- [ohmf/services/gateway/internal/messages/lifecycle.go](ohmf/services/gateway/internal/messages/lifecycle.go#L1-L68)
- [ohmf/services/gateway/internal/messages/service.go](ohmf/services/gateway/internal/messages/service.go#L1-L120)
- [ohmf/services/gateway/internal/relay/service.go](ohmf/services/gateway/internal/relay/service.go#L1-L120)
- [ohmf/services/gateway/internal/miniapp/service.go](ohmf/services/gateway/internal/miniapp/service.go#L1-L120)
- [tools/validate_openapi/main.go](tools/validate_openapi/main.go#L1-L43)
- [ohmf/services/gateway/integration/mvp_flow_test.go](ohmf/services/gateway/integration/mvp_flow_test.go#L1-L120)
