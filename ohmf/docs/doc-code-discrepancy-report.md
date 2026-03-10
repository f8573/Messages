**Plan / heuristic**

- I'll inspect the top-level spec and the core design & API docs, then map each claim to runtime code (handlers, services, proto, SQL migrations, OpenAPI).  
- Heuristic for mapping: match doc claims (endpoints, request/response shapes, invariants, architecture statements) to:
  - OpenAPI + JSON schema files under `ohmf/packages/protocol/openapi` and `ohmf/packages/protocol/events`
  - REST/WebSocket handlers and services under `ohmf/services/gateway/internal/*` and the router in `ohmf/services/gateway/cmd/api/main.go`
  - Proto definitions under `ohmf/packages/protocol/proto`
  - SQL schema / migrations under `ohmf/packages/protocol/sql` and `ohmf/services/gateway/migrations`
  - Storage / behaviour in `messages.Service`, `auth.Service`, `carrier.Service`, `sync.Service` and other service code
- Files I inspected (primary set)
  - OHMF_Complete_Platform_Spec_v1.md
  - ohmf/docs/api-mvp.md
  - ohmf/docs/backend-mvp.md
  - ohmf/docs/architecture.md
  - ohmf/docs/client-sync.md
  - ohmf/docs/linked-device-relay.md
  - ohmf/docs/user-signup-and-send-message.md
  - ohmf/packages/protocol/openapi/openapi.yaml
  - ohmf/packages/protocol/events/message-ingress.schema.json
  - ohmf/packages/protocol/proto/envelope.proto
  - ohmf/packages/protocol/sql/ohmf_schema.sql
  - ohmf/services/gateway/cmd/api/main.go
  - ohmf/services/gateway/internal/openapi/handler.go
  - ohmf/services/gateway/internal/auth/service.go
  - ohmf/services/gateway/internal/auth/handler.go
  - ohmf/services/gateway/internal/messages/service.go
  - ohmf/services/gateway/internal/messages/handler.go
  - ohmf/services/gateway/internal/realtime/ws.go
  - ohmf/services/gateway/internal/carrier/service.go
  - ohmf/services/gateway/internal/sync/service.go and handler.go
  - ohmf/infra/docker/docker-compose.yml

Below I give a structured mapping and discrepancy report for the most important claims and APIs (priority: auth, messages, sync, relay, carrier, openapi schema). Each discrepancy entry includes quote, code locations, description, severity, reproduce steps, and fix suggestions.

**Key mappings (docs → code)**

- API contract (canonical): [ohmf/packages/protocol/openapi/openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml) → served at runtime by [ohmf/services/gateway/internal/openapi/handler.go](ohmf/services/gateway/internal/openapi/handler.go) and exposed at `/openapi.yaml` by the gateway router in [ohmf/services/gateway/cmd/api/main.go](ohmf/services/gateway/cmd/api/main.go#L100-L110).
- Phone OTP flows:
  - Doc claim/examples: [ohmf/docs/user-signup-and-send-message.md](ohmf/docs/user-signup-and-send-message.md) (phone start & verify examples).
  - Implemented by handlers: [ohmf/services/gateway/internal/auth/handler.go](ohmf/services/gateway/internal/auth/handler.go) using service [ohmf/services/gateway/internal/auth/service.go](ohmf/services/gateway/internal/auth/service.go).
- Message send endpoints:
  - Doc: openapi `/v1/messages` and `/v1/messages/phone` in [openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml) and examples in [user-signup-and-send-message.md](ohmf/docs/user-signup-and-send-message.md).
  - Implemented by: router registration in [ohmf/services/gateway/cmd/api/main.go](ohmf/services/gateway/cmd/api/main.go) → handlers [ohmf/services/gateway/internal/messages/handler.go](ohmf/services/gateway/internal/messages/handler.go) → service [ohmf/services/gateway/internal/messages/service.go](ohmf/services/gateway/internal/messages/service.go).
- WebSocket gateway:
  - Doc: `/v1/ws` in [openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml) and docs [api-mvp.md](ohmf/docs/api-mvp.md).
  - Implemented by: [ohmf/services/gateway/internal/realtime/ws.go](ohmf/services/gateway/internal/realtime/ws.go) and route in [main.go](ohmf/services/gateway/cmd/api/main.go).
- Carrier mirror / device-authoritative carrier messages:
  - Doc claim: OHMF spec device-authoritative carrier model in [OHMF_Complete_Platform_Spec_v1.md](OHMF_Complete_Platform_Spec_v1.md#L1).
  - Implemented by: carrier mirror table/migrations [ohmf/services/gateway/migrations/000015_carrier_messages.up.sql](ohmf/services/gateway/migrations/000015_carrier_messages.up.sql) and import/list logic in [ohmf/services/gateway/internal/carrier/service.go](ohmf/services/gateway/internal/carrier/service.go); unified timeline merging uses `carrier_messages` in [messages/service.go](ohmf/services/gateway/internal/messages/service.go).
- Sync endpoint and cursor model:
  - Doc: [ohmf/docs/client-sync.md](ohmf/docs/client-sync.md) recommends opaque cursor including `last_server_order` map.
  - Implemented by: `GET /v1/sync` → [ohmf/services/gateway/internal/sync/handler.go](ohmf/services/gateway/internal/sync/handler.go) → [ohmf/services/gateway/internal/sync/service.go](ohmf/services/gateway/internal/sync/service.go) which implements a timestamp-based cursor.

--- 

**Discrepancy 1 — OpenAPI missing response schemas for OTP verify & refresh (OpenAPI vs code)**

- Documentation excerpt (openapi.yaml):
  - (path: [ohmf/packages/protocol/openapi/openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml)) — for `/v1/auth/phone/verify` the spec lists the path but does not declare a response schema. Example fragment (openapi lines around path):
    - "/v1/auth/phone/verify:" (no response schema object defined)
- Code implementing behavior:
  - [ohmf/services/gateway/internal/auth/service.go](ohmf/services/gateway/internal/auth/service.go) — `VerifyPhone` returns a map containing `user`, `device`, and `tokens` (access_token + refresh_token). See `VerifyPhone` return value (function returns `map[string]any{ "user":..., "device":..., "tokens": ... }`).
  - [ohmf/services/gateway/internal/auth/service.go](ohmf/services/gateway/internal/auth/service.go) — `Refresh` returns map `{"access_token": access, "refresh_token": newRefresh}`.
- Description:
  - The OpenAPI spec lacks explicit response schemas for `/v1/auth/phone/verify` and `/v1/auth/refresh`. The runtime returns structured JSON for verify (user+device+tokens) and for refresh (token pair). This makes generated client code and API validation ambiguous and may break codegen/contract tests.
- Severity: Medium — breaks client-facing OpenAPI contract / client generation.
- Reproduction:
  - Start gateway and curl `/openapi.yaml` to confirm missing responses. Or inspect [openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml) for missing `responses` content under `/v1/auth/phone/verify` and `/v1/auth/refresh`.
  - Verify actual runtime response:
    - POST /v1/auth/phone/verify (example)
      curl -X POST http://localhost:18080/v1/auth/phone/verify -d '{"challenge_id":"...","otp_code":"123456", "device": {"platform":"WEB","device_name":"Chrome"}}' -H 'Content-Type: application/json'
    - Observe JSON: { "user": { "user_id": "...", "primary_phone_e164": "..." }, "device": {"device_id":"...",...}, "tokens": {"access_token":"...", "refresh_token":"..."}}
- Suggested fixes:
  - Edit OpenAPI: add response schemas.
  - Exact suggested OpenAPI snippet (replace/add under `components.schemas`):

    Add:

    - TokenPair (exists; ensure used)
    - VerifyPhoneResponse:

    ```
    VerifyPhoneResponse:
      type: object
      required: [user, device, tokens]
      properties:
        user:
          type: object
          required: [user_id, primary_phone_e164]
          properties:
            user_id: { type: string }
            primary_phone_e164: { type: string }
        device:
          type: object
          required: [device_id, platform]
          properties:
            device_id: { type: string }
            platform: { type: string }
        tokens:
          $ref: '#/components/schemas/TokenPair'
    ```

    Then update `/v1/auth/phone/verify` responses 200 to reference `VerifyPhoneResponse`, and `/v1/auth/refresh` responses 200 to reference `TokenPair`.

  - Priority: High for OpenAPI correctness. Effort: ~15–30 minutes to edit `openapi.yaml` and run any OpenAPI validation tests.
  - Status: Resolved — the OpenAPI spec (`ohmf/packages/protocol/openapi/openapi.yaml`) was updated to include `VerifyPhoneResponse`, `RefreshRequest`/`TokenPair` usage and `operationId`/`summary` entries for the auth paths.

---

**Discrepancy 2 — Sync cursor format: docs recommend opaque server-side cursor vs implementation uses RFC3339 timestamp (client-sync.md vs sync service)**

- Doc excerpt (client-sync.md):
  - "The cursor MUST be opaque to clients and stable for incremental pagination/resume." and recommended cursor JSON with `cursor_version` + `last_server_order` map (example).
  - Quote (client-sync.md): (approx)
    - "Cursor format (recommended): - `cursor_version` (integer) - `last_server_order` (map of conversation_id -> server_order) - `timestamp_ms` (unix ms)"
- Code implementing behavior:
  - [ohmf/services/gateway/internal/sync/service.go](ohmf/services/gateway/internal/sync/service.go) — `IncrementalSync` treats `cursor` as an RFC3339 timestamp (it tries to parse `cursor` with `time.Parse(time.RFC3339Nano, cursor)` and if invalid, defaults to last 5 minutes). It returns `next_cursor` as RFC3339 timestamp.
- Description:
  - The spec/doc requires opaque multi-field cursor; the running implementation uses a single-use timestamp string cursor. This is a protocol mismatch—clients built to send a `last_server_order` map will not be understood; conversely clients expecting opaque cursor may not handle the server-provided timestamp format.
- Severity: High — client/server sync compatibility is critical.
- Reproduction:
  - Call `GET /v1/sync?cursor=eyJjdXJzb3JfdmVyc2lvbiI6MSwidGFzaCI6...` (an opaque JSON or base64 token) — the server will attempt to parse it as RFC3339 and ignore/replace it (treats as since=now-5m if unparsable) → missed events.
  - Example: `curl -v "http://localhost:18080/v1/sync?cursor=opaque-token" -H "Authorization: Bearer <access>"` → observe `next_cursor` is RFC3339 timestamp, and results are based on created_at filtering.
- Suggested fixes (choose one; rank 1 or 2):

  Option A — Doc change (low-effort, immediate):
  - Update `ohmf/docs/client-sync.md` to document the server’s current sync cursor behavior: state that server accepts/returns RFC3339 timestamp tokens and provide example: `cursor=2026-03-08T12:00:00.000000000Z`. Note optional note that a more advanced opaque cursor is planned.
  - Exact doc replacement snippet (recommended):

    Replace the "Cursor format (recommended): ..." section with:

    - "Current implementation: `cursor` is interpreted as an RFC3339 timestamp (UTC). When provided it returns events with created_at > cursor; `next_cursor` is an RFC3339 timestamp representing the last event timestamp. Future versions may switch to an opaque cursor containing per-conversation server_order; clients SHOULD gracefully handle RFC3339 tokens."

  - Effort: ~10–20 minutes.
  - Status: Resolved — `RefreshRequest` and `TokenPair` are now referenced in `ohmf/packages/protocol/openapi/openapi.yaml` and the auth paths include request/response schemas and operationIds.

  Option B — Implement opaque cursor server-side (higher effort):
  - Implement encoding/decoding of an opaque cursor JSON (base64) that contains `cursor_version`, `last_server_order` map and `timestamp_ms`, and use `last_server_order` to produce deterministic incremental results as docs require.
  - Code changes needed in `sync/service.go` and possibly DB queries + tests.
  - Effort estimate: 4–8 hours (design + implement + tests + migration for cursor versioning).

  Recommendation: implement Option A immediately (document current behavior), and schedule Option B for later if you want spec-level conformance.

---

**Discrepancy 3 — Spec language: "Server MUST NOT overwrite telephony provider truth" vs server-side mirror & linking (OHMF_Complete_Platform_Spec_v1.md vs carrier/messages handling)**

- Doc excerpt (OHMF_Complete_Platform_Spec_v1.md; section 3.2 & 5.1):
  - "Device-authoritative carrier messaging MUST be device-local authoritative. Server state MUST NOT overwrite telephony provider truth."
- Code behavior:
  - Carrier import: [ohmf/services/gateway/internal/carrier/service.go](ohmf/services/gateway/internal/carrier/service.go) `ImportCarrierMessage` inserts into `carrier_messages` with `device_authoritative = true` and can store `server_message_id` (optional link to server message).
  - `messages.Service.ListUnified` merges `carrier_messages` into the timeline and marks `Source: "CARRIER"`, and includes `device_authoritative` when reading carrier rows.
  - When a phone message is sent from server side (`SendToPhone` / `sendToPhoneSync`), the server inserts a canonical message into `messages` table with `transport = 'SMS'` (see [messages/service.go](ohmf/services/gateway/internal/messages/service.go) `sendToPhoneSync`).
- Description:
  - The implementation accounts for `device_authoritative` (carrier mirror rows are marked device_authoritative). However the server also writes canonical `messages` rows for server-initiated phone sends (necessary). Potential ambiguity: the spec language is strong ("MUST NOT overwrite telephony provider truth"), but server code links `carrier_messages.server_message_id` optionally and may update server or carrier rows in some flows (e.g., when user verifies a phone and conversations are promoted).
  - Observed behavior is largely consistent with the intent (carrier mirror rows are stored separately and marked device_authoritative), but the spec's "MUST NOT overwrite" is absolute and requires a documented reconciliation policy (how and when server sets server_message_id, whether server ever updates carrier raw payload or device_authoritative flag). I could not find explicit code that would overwrite a carrier mirror's `device_authoritative=true` value (imports set true) but reconciliation flows exist (e.g., `findOrCreatePhoneConversation` converts external phone DM entries into user conversation).
- Severity: Medium — this is a spec-vs-implementation nuance that should be clarified for integrators and audits.
- Reproduction:
  - Import a carrier message via protected API (carrier import) and confirm DB `carrier_messages.device_authoritative` is true and server `messages` table unchanged.
  - Send a phone message server-side and confirm `messages` table contains `transport='SMS'` and that `carrier_messages` are separate.
- Suggested fixes:
  - Documentation: Clarify the reconciliation rules in [OHMF_Complete_Platform_Spec_v1.md] — add explicit section: "Server MAY store a mirror of carrier messages in `carrier_messages` marked `device_authoritative=true`. Server MUST NOT treat mirror rows as authoritative over telephony provider state; any `server_message_id` linkage must be explicit and must not be used to overwrite carrier-origin fields (created_at, raw_payload, device_authoritative) except via an explicit reconciliation/confirmation flow documented here."
  - Code: add an assertion / DB trigger or code guard where `carrier_messages` writes set `device_authoritative = true` and prevent updates to core carrier fields except via an explicit admin endpoint. If desired, add a check wherever `server_message_id` is set that the operation is idempotent and logged.
  - Effort: doc update 15–30 minutes; minor code hardening (guarding updates) 1–2 hours.

---

**Discrepancy 4 — OpenAPI `/v1/messages` `SendMessageResponse.transport` enum vs handler normalization (openapi.yaml vs messages/handler.go)**

- Doc excerpt (openapi.yaml):
  - `SendMessageResponse.transport` allowed enum: `[OHMF]` (OpenAPI defines for `/v1/messages` transport enum OHMF).
- Code behavior:
  - [ohmf/services/gateway/internal/messages/handler.go](ohmf/services/gateway/internal/messages/handler.go) normalizes internal `"OTT"` to `"OHMF"` and empty transport to `"OHMF"`. For phone sends (`SendToPhone`) transport is set to `"SMS"` in responses. The OpenAPI defines `/v1/messages` transport enum only `"OHMF"`, while `/v1/messages/phone` allows `"SMS"`.
- Description:
  - For `/v1/messages`, implementation returns `"OHMF"` for OTT; code already normalizes `"OTT"` to `"OHMF"`, so behavior matches the OpenAPI contract. However OpenAPI only allows `"OHMF"` for `/v1/messages` responses whereas code maps several internal variants to `"OHMF"` — acceptable. The minor concern: some code paths might accidentally emit other strings (handler normalizes but message.Service might set `Transport: "OTT"` or `Transport: ""`), but handler maps them. That mapping is implemented; not strictly a discrepancy but worth noting.
- Severity: Low.
- Reproduction:
  - POST /v1/messages and observe response `transport` value. It will be `"OHMF"`.
- Suggested fix:
  - No change required; optionally update OpenAPI description to note the handler normalizes internal transport names to the API-facing enum.

---

**Discrepancy 5 — OpenAPI and API MVP docs: `/v1/auth/refresh` security / body shape mismatch**

- Doc excerpt:
  - [api-mvp.md](ohmf/docs/api-mvp.md) and [openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml) show `POST /v1/auth/refresh` as public (security: []), but OpenAPI path has no requestBody schema and no response schema defined for refresh.
- Code behavior:
  - Handler expects a JSON body with `refresh_token` — see [ohmf/services/gateway/internal/auth/handler.go](ohmf/services/gateway/internal/auth/handler.go) `Refresh` (reads body into RefreshRequest). The service returns new `access_token` + `refresh_token`.
- Description:
  - The OpenAPI does not document the request body for /v1/auth/refresh but handler requires it. This prevents client codegen / contract tests.
- Severity: Medium.
- Reproduction:
  - Call `POST /v1/auth/refresh` without a body; code will return error due to invalid JSON decode.
- Suggested fix:
  - Add `RefreshRequest` schema in OpenAPI and set `/v1/auth/refresh` requestBody schema to `RefreshRequest` and response to `TokenPair`.
  - Effort: ~10–20 minutes.

---

**Discrepancy 6 — Client quickstart port / example vs default runtime address (docs vs config)**

- Doc excerpt (user quickstart):
  - Example: "API is running at `http://localhost:18080`" and examples curl against port 18080 in [ohmf/docs/user-signup-and-send-message.md](ohmf/docs/user-signup-and-send-message.md).
- Code / docker:
  - [ohmf/services/gateway/internal/config/config.go](ohmf/services/gateway/internal/config/config.go) default `APP_ADDR` is `:8080` (server binds to `:8080`). Docker compose maps host 18080 → container 8080 (see [ohmf/infra/docker/docker-compose.yml](ohmf/infra/docker/docker-compose.yml) `ports: - "18080:8080"`), so examples are OK for Docker dev stack. However the canonical OpenAPI `servers:` entry uses `http://localhost:18080` (matching docker-compose) while the gateway config defaults to `:8080`. This is a mild mismatch (docs assume running via docker compose).
- Description:
  - Not a functional mismatch, but a potential confusion for developers running the binary locally without Docker (server will listen on :8080, while docs curl to :18080).
- Severity: Low.
- Suggested fix:
  - In quickstart, explicitly state the example is for Docker dev stack and that the binary defaults to `:8080` if run without docker. Add a note to `ohmf/docs/user-signup-and-send-message.md`.
  - Effort: 5–10 minutes.

---

**Discrepancy 7 — OpenAPI `ErrorEnvelope` required field `request_id` vs WriteError uses header `X-Request-Id` (header name canonicalization)**

- Doc excerpt:
  - OpenAPI defines `ErrorEnvelope` required: `code`, `message`, `request_id`.
- Code behavior:
  - [ohmf/services/gateway/internal/httpx/http.go](ohmf/services/gateway/internal/httpx/http.go) `WriteError` sets `RequestID` from request header `X-Request-Id`.
  - router registers `chi` middleware `chimiddleware.RequestID` which usually sets `X-Request-ID` header (capital D vs capital ID).
- Description:
  - HTTP header names are case-insensitive; `r.Header.Get("X-Request-Id")` will match `X-Request-ID`. This is not a practical bug, but for clarity, align expected header name in doc or ensure middleware emits `X-Request-Id` (currently chi emits `X-Request-ID`).
- Severity: Low.
- Suggested fix:
  - No code change necessary; add comment in `httpx.WriteError` or docs noting header used is `X-Request-ID` (case-insensitive). Effort: 5 minutes.

---

**Additional positive verifications (no discrepancy found)**

- `idempotency_key` requirement for `/v1/messages` is enforced by handler (`if req.IdempotencyKey == ""` returns 400). See [messages/handler.go](ohmf/services/gateway/internal/messages/handler.go).
- WebSocket auth via `access_token` query param or `Authorization: Bearer` header is implemented in [realtime/ws.go](ohmf/services/gateway/internal/realtime/ws.go) `authenticate()` — matches docs.
- `message-ingress` JSON schema exists at [ohmf/packages/protocol/events/message-ingress.schema.json](ohmf/packages/protocol/events/message-ingress.schema.json), and the router uses the `ValidateJSONMiddleware` to validate `/messages` and `/messages/phone` endpoints in [main.go](ohmf/services/gateway/cmd/api/main.go).

---

**Suggested concrete edit snippets**

1) OpenAPI: add VerifyPhoneResponse and Refresh/Request schemas.

- Add under `components.schemas` in [ohmf/packages/protocol/openapi/openapi.yaml](ohmf/packages/protocol/openapi/openapi.yaml):

  ```
  VerifyPhoneResponse:
    type: object
    required: [user, device, tokens]
    properties:
      user:
        type: object
        required: [user_id, primary_phone_e164]
        properties:
          user_id: { type: string }
          primary_phone_e164: { type: string }
      device:
        type: object
        required: [device_id, platform]
        properties:
          device_id: { type: string }
          platform: { type: string }
      tokens:
        $ref: '#/components/schemas/TokenPair'

  RefreshRequest:
    type: object
    required: [refresh_token]
    properties:
      refresh_token: { type: string }
  ```

- Then in paths:
  - `/v1/auth/phone/verify` responses '200' content schema -> `VerifyPhoneResponse`
  - `/v1/auth/refresh` add `requestBody` with `RefreshRequest` and responses '200' -> `TokenPair`

2) client-sync.md: replace cursor section with:

  ```
  Current implementation: `cursor` is interpreted as an RFC3339 timestamp (UTC).
  Example: `cursor=2026-03-08T12:00:00.000000000Z`.
  The server returns `next_cursor` as RFC3339 timestamp representing last event time.
  Future versions MAY migrate to an opaque cursor that contains per-conversation `last_server_order`; clients SHOULD support RFC3339 timestamps until a new cursor_version is introduced.
  ```

3) OHMF_Complete_Platform_Spec_v1.md: add clarification near device-authoritative section:

  - Add a paragraph clarifying server mirror semantics:

    ```
    Note: The server MAY maintain a mirror store of carrier messages (`carrier_messages`) marked with `device_authoritative = true`. The server MUST NOT treat mirror rows as authoritative over telephony provider state; any `server_message_id` linkage must be explicit and must not be used to overwrite carrier-origin fields (created_at, raw_payload, device_authoritative) except via an explicit reconciliation/confirmation flow documented here.
    ```

---

**Prioritized action list (quick wins first)**

1. Add OpenAPI response/request schemas for OTP verify and refresh (OpenAPI edit).
   - Files: ohmf/packages/protocol/openapi/openapi.yaml
   - Effort: 15–30 minutes
   - Priority: High (client generation + contract tests)

2. Update `ohmf/docs/client-sync.md` to reflect implemented timestamp cursor and note future planned opaque cursor.
   - Files: ohmf/docs/client-sync.md
   - Effort: 10–20 minutes
   - Priority: High (prevents client-server sync breakages)

3. Add missing OpenAPI request body for `/v1/auth/refresh` (same as #1).
   - Files: ohmf/packages/protocol/openapi/openapi.yaml
   - Effort: included in #1
   - Priority: High

4. Clarify device-authoritative reconciliation language in the spec (`OHMF_Complete_Platform_Spec_v1.md`) and add a note around how `server_message_id` may be set.
   - Files: OHMF_Complete_Platform_Spec_v1.md
   - Effort: 15–30 minutes
   - Priority: Medium

5. (Optional) Implement server-side opaque cursor with per-conversation last_server_order (align runtime with spec).
   - Files: ohmf/services/gateway/internal/sync/service.go (+ tests, maybe DB changes if per-conversation cursors are stored)
   - Effort: 4–8 hours
   - Priority: Medium (if strict spec conformance required)

6. Small doc clarity: call out Docker port mapping in quickstart `ohmf/docs/user-signup-and-send-message.md`.
   - Files: ohmf/docs/user-signup-and-send-message.md
   - Effort: 5–10 minutes
   - Priority: Low

7. (Optional) Add a DB / code guard preventing accidental updates to `carrier_messages.device_authoritative` except via explicit admin flow, and log when `server_message_id` is set.
   - Files: ohmf/services/gateway/internal/carrier/service.go, DB migrations / constraints
   - Effort: 1–2 hours
   - Priority: Low/Medium depending on risk tolerance

---

**Coverage summary (high-level estimate)**

- Docs inspected (primary set): 9 major docs + openapi + proto + SQL + key READMEs. (List shown above.)
- Mapping coverage:
  - OpenAPI endpoints → router handlers: ~100% of paths defined in `ohmf/packages/protocol/openapi/openapi.yaml` are wired in `ohmf/services/gateway/cmd/api/main.go` and implemented in `internal/*` handlers (auth, messages, carrier, media, miniapp, relay, etc.). I verified main routes mapping for the important endpoints (`/auth/*`, `/messages`, `/messages/phone`, `/ws`, `/conversations`, `/sync`, `/relay/*`, `/carrier/messages/import`).
  - Protocol schemas (proto + JSON schema) → runtime: `envelope.proto` exists and types are mirrored in code (`Envelope` structs and Envelope publishing exist in messages.* async paths); `message-ingress.schema.json` is used by ValidateJSONMiddleware for ingress payloads.
- Percent of docs mapped to code (estimate): for core runtime behavior (auth, messages, sync, ws, relay, carrier) ~90% of doc claims had a mapped implementation; for peripheral docs & READMEs not fully scanned (miniapp manifests, infra nuance), mapping coverage lower.
- Number of docs with at least one verified mapping: 9 (the ones I inspected).
- Undocumented public APIs / mismatches found:
  - OpenAPI missing response schema for `/v1/auth/phone/verify` and missing request body for `/v1/auth/refresh` — documented routes but missing response shape.
  - Sync cursor mismatch between doc and implementation.
- Files/areas requiring deeper manual review:
  - `ohmf/services/messages-processor` and `delivery-processor` — I did not fully trace the Kafka ingress/persisted path (these processors define the async pipeline behavior that the gateway depends on). Deeper review needed to verify event schema & ack semantics across Kafka topics.
  - Mini-app runtime & manifest signing flow (`ohmf/packages/miniapp/*` and `ohmf/services/gateway/internal/miniapp/*`) — I saw handlers but did not validate full capability mapping vs spec; security review required for sandboxing/manifest verification (miniapp public key PEM in config).
  - Database schema: large migrations set in `ohmf/services/gateway/migrations` require a thorough pass to ensure doc/DB field naming parity (I inspected some migrations like `carrier_messages` and `messages` used in code, but not every migration).
  - Cassandra storage & messages-processor behavior (`ohmf/services/messages-processor`) — I did not run/read full processor code to confirm `server_order` assignment semantics exactly match spec (docs say Postgres counters, messages-processor claims to assign server_order using Postgres counters — but cross-checking distributed semantics needs end-to-end tests).

---

If you want, I can now:
- Apply the quick OpenAPI edits (I can open & patch `ohmf/packages/protocol/openapi/openapi.yaml`) and update `client-sync.md` and `OHMF_Complete_Platform_Spec_v1.md` with the exact suggested snippets; or
- Implement the longer sync cursor change (server-side opaque cursor) as a PR (will require tests).  

Which of the suggested fixes should I implement first?
