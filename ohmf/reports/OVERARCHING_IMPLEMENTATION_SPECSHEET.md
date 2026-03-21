# OHMF Overarching Implementation Spec Sheet

Current-state snapshot derived from repository code, migrations, tests, and local docs.

Scan date: 2026-03-21
Repository root: `C:\Users\James\Downloads\Messages`

## 1. Scope And Method

This document describes what is currently implemented in the repository, not what older planning docs say should exist.

Primary source-of-truth inputs used for this sheet:

- `ohmf/services/gateway/cmd/api/main.go`
- `ohmf/services/gateway/internal/*`
- `ohmf/services/gateway/migrations/*`
- `ohmf/services/apps/*`
- `ohmf/services/messages-processor/*`
- `ohmf/services/delivery-processor/*`
- `ohmf/services/sms-processor/*`
- `ohmf/apps/web/*`
- `ohmf/apps/android/miniapp-host/*`
- `ohmf/packages/miniapp/*`
- `ohmf/packages/protocol/*`
- `ohmf/infra/docker/docker-compose.yml`
- `ohmf/services/gateway/integration/*`

This is a current implementation map, so it intentionally distinguishes between:

- implemented runtime behavior
- partially implemented scaffolds
- placeholder directories reserved for future service extraction

## 2. High-Level System Snapshot

The project currently implements a working messaging platform centered on a monolithic Go gateway plus a mini-app registry service and several background/event processors.

Implemented top-level runtime pieces:

- `gateway`
  - single client-facing API surface
  - REST + WebSocket
  - auth, users, conversations, messages, media, discovery, mini-app sessions, relay, device keys, presence, sync, notifications, abuse, carrier import/linking
- `apps`
  - dedicated mini-app registry and catalog/review service
- `messages-processor`
  - Kafka ingress consumer that validates, persists, fans out, and writes Cassandra
- `delivery-processor`
  - persisted-message consumer that emits delivery events and Redis pubsub fanout
- `sms-processor`
  - SMS dispatch stub processor that marks SMS dispatch as sent in local/dev flows
- `gateway worker`
  - background runner for media cleanup, notifications, abuse aggregation, relay retry, and sync fanout
- `web app`
  - browser client with auth, conversations, sync, WebSocket realtime, attachments, E2EE, push setup, and mini-app embedding
- `android miniapp-host`
  - standalone Android mini-app catalog/runtime scaffold
- `packages/miniapp`
  - manifest schema, bridge contract, web SDK, types, CLI, examples, and test harness
- `packages/protocol`
  - OpenAPI, JSON schemas, protobuf envelope, protocol types, and canonical SQL schema assets
- `infra/observability`
  - Prometheus, Grafana, alerts, dashboard provisioning

## 3. Runtime Topology

### 3.1 Main Compose Stack

`ohmf/infra/docker/docker-compose.yml` brings up:

- PostgreSQL
- Redis
- Cassandra
- Kafka
- Kafka topic bootstrap container
- Prometheus
- Grafana
- `api` (gateway)
- `apps` (mini-app registry)
- `messages-processor`
- `delivery-processor`
- `sms-processor`

`ohmf/infra/docker/docker-compose.client.yml` adds:

- `client`
  - static Python-hosted web app container

### 3.2 Current Dependency Shape

Current operational role of each dependency:

- PostgreSQL
  - primary system of record for accounts, conversations, messages, sync state, mini-app sessions, device keys, notifications, relay jobs, and audit data
- Redis
  - rate limits, presence/typing freshness, websocket/session state, message ack correlation, pubsub fanout
- Kafka
  - async send pipeline and downstream processing topics
- Cassandra
  - optional message timeline read model used by the async messaging pipeline

### 3.3 Gateway Runtime Modes

The gateway has two startup modes in `cmd/api/main.go`:

- full mode
  - initializes DB, Redis, optional Kafka producer, optional Cassandra reads, workers/services, and full routing
- smoke mode
  - enabled by `APP_SMOKE_MODE=1`
  - only exposes health/ready, metrics, `_services`, and lightweight reverse proxies for contacts/apps/media

## 4. Service Boundary Reality

The repository contains many `services/*` directories, but the current runtime is not fully decomposed into separate services yet.

### 4.1 Centralized In Gateway Today

These domains are implemented as gateway internal packages, not standalone deployables:

- auth
- users
- conversations
- messages
- discovery
- media API layer
- realtime/websocket
- sync
- devices
- device attestation
- device keys / E2EE bundle exchange
- notifications
- presence
- abuse
- carrier message import/linking
- relay
- mini-app session runtime and sharing

### 4.2 Standalone Deployables Today

These are actual separate binaries/services:

- `services/gateway`
- `services/apps`
- `services/messages-processor`
- `services/delivery-processor`
- `services/sms-processor`

### 4.3 Placeholder / Future Extraction Targets

Several top-level service directories are still scaffolds/placeholders rather than independently implemented services:

- `services/auth`
- `services/contacts`
- `services/conversations`
- `services/media`
- `services/messages`
- `services/realtime`
- `services/relay`
- `services/users`
- `services/abuse`

The codebase currently routes those concerns through the gateway instead.

## 5. Gateway Surface

The gateway is the main implemented platform surface.

### 5.1 Public Routes

Implemented unauthenticated routes from `cmd/api/main.go`:

- `POST /v1/auth/phone/start`
- `POST /v1/auth/phone/verify`
- `POST /v1/auth/refresh`
- `POST /v1/auth/recovery-code`
- `POST /v1/auth/pairing/complete`
- `GET /v1/ws`
- `GET /v2/ws`
- `PUT /v1/media/uploads/{token}`
- `GET /v1/media/downloads/{token}`
- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `GET /openapi.yaml`
- `GET /v1/_services`

### 5.2 Authenticated Route Families

Implemented authenticated route groups:

- auth/account
- me/profile resolution
- discovery
- conversations
- messages
- events/sync
- devices
- device attestation
- device keys / key backups
- media attachments
- carrier import/linking
- mini-app manifests/sessions/sharing/catalog
- publisher/admin app registry passthrough
- notifications
- relay jobs
- abuse
- blocks
- presence

### 5.3 Versioned Surfaces

Implemented version split:

- `v1`
  - broad surface area, write-heavy and feature-complete
- `v2`
  - websocket v2
  - projected conversation listing
  - incremental sync v2
  - delivered checkpoint endpoint

The gateway also applies API version headers through middleware and exposes the OpenAPI spec.

## 6. Gateway Subsystems

### 6.1 Authentication, Session, And Account Security

Implemented behaviors:

- phone OTP start/verify login
- refresh token rotation
- logout/revocation
- recovery code generation and use
- two-factor setup / verify / list methods
- device pairing start / complete flows
- JWT auth middleware
- auth rate limiting and OTP abuse checks

Implemented endpoints:

- `POST /v1/auth/phone/start`
- `POST /v1/auth/phone/verify`
- `POST /v1/auth/refresh`
- `POST /v1/auth/logout`
- `POST /v1/auth/recovery-code`
- `POST /v1/auth/pairing/start`
- `POST /v1/auth/pairing/complete`
- `GET /v1/account/recovery-codes`
- `POST /v1/account/2fa/setup`
- `POST /v1/account/2fa/verify`
- `GET /v1/account/2fa/methods`

Evidence in tests:

- OTP generation and rate limit tests
- refresh rotation tests
- invalid OTP / invalid refresh integration tests

### 6.2 User Profile, Account Data, And Blocking

Implemented behaviors:

- current profile read/update
- multi-user profile resolution
- account export
- export artifact creation/list/get
- deletion request
- deletion finalization
- block / unblock / list blocked users

Implemented endpoints:

- `GET /v1/me`
- `PATCH /v1/me`
- `POST /v1/users/resolve`
- `POST /v1/account/export`
- `POST /v1/account/exports`
- `GET /v1/account/exports`
- `GET /v1/account/exports/{id}`
- `POST /v1/account/delete`
- `POST /v1/account/delete/finalize`
- `POST /v1/blocks/{id}`
- `DELETE /v1/blocks/{id}`
- `GET /v1/blocks`

### 6.3 Discovery

Implemented behaviors:

- privacy-preserving contact discovery through hashed identifiers
- rate limiting and oversized batch rejection
- dual route aliases for discovery

Implemented endpoints:

- `POST /v1/discovery`
- `POST /v1/contacts/discover`

Test coverage confirms:

- versioned hash matching
- request size/rate limit enforcement

### 6.4 Conversations

Implemented behaviors:

- create direct/group conversations
- create/find phone conversations
- list conversations
- projected conversation listing in `v2`
- get conversation
- conversation policy updates
- metadata updates
- user preference updates
- theme/retention/expiry settings
- effect policy toggling
- member add/remove/role management
- invite create/list/redeem
- bans / unbans
- thread key storage

Implemented endpoints:

- `POST /v1/conversations`
- `POST /v1/conversations/phone`
- `GET /v1/conversations`
- `GET /v2/conversations`
- `GET /v1/conversations/{id}`
- `PATCH /v1/conversations/{id}`
- `PATCH /v1/conversations/{id}/metadata`
- `PATCH /v1/conversations/{id}/settings`
- `PATCH /v1/conversations/{id}/preferences`
- `PUT /v1/conversations/{id}/effect-policy`
- `POST /v1/conversations/{id}/members`
- `DELETE /v1/conversations/{id}/members/{userID}`
- `PUT /v1/conversations/{id}/members/{userID}/role`
- `POST /v1/conversations/{id}/invites`
- `GET /v1/conversations/{id}/invites`
- `POST /v1/conversations/invites/redeem`
- `POST /v1/conversations/{id}/bans`
- `DELETE /v1/conversations/{id}/bans/{userID}`
- `POST /v1/conversations/{id}/thread_keys`

Notable enforced rules visible in tests/service code:

- owner/admin gating for effect policy
- last-owner demotion protection
- invite lifecycle support
- block state projection across devices

### 6.5 Messaging Core

Implemented behaviors:

- send OHMF messages
- send to phone/SMS transport
- idempotency keys
- client-generated id roundtrip
- server-order assignment
- list conversation messages
- unified timeline reads
- search by text/content type
- reply threading
- edit messages
- delete messages
- redact messages
- delivery record append/list
- mark read and read-status summary
- reactions add/remove/list aggregated
- pin/unpin/list pinned
- forward message
- message effects
- message expiry / retention support
- validation for structured text, attachments, app cards, encrypted payloads

Implemented endpoints:

- `POST /v1/messages`
- `POST /v1/messages/phone`
- `GET /v1/conversations/{id}/messages`
- `GET /v1/conversations/{id}/timeline`
- `GET /v1/conversations/{id}/search`
- `POST /v1/conversations/{id}/read`
- `POST /v2/conversations/{id}/read`
- `POST /v2/conversations/{id}/delivered`
- `POST /v1/messages/{id}/deliveries`
- `GET /v1/messages/{id}/deliveries`
- `POST /v1/messages/{id}/redact`
- `PATCH /v1/messages/{id}`
- `DELETE /v1/messages/{id}`
- `GET /v1/messages/{id}/edits`
- `POST /v1/messages/{id}/reactions`
- `DELETE /v1/messages/{id}/reactions`
- `GET /v1/messages/{id}/reactions`
- `POST /v1/messages/{id}/pin`
- `DELETE /v1/messages/{id}/pin`
- `GET /v1/conversations/{id}/pins`
- `GET /v1/messages/{id}/replies`
- `POST /v1/messages/{id}/forward`
- `POST /v1/messages/{id}/effects`
- `GET /v1/conversations/{id}/read-status`

Implemented content types and constraints visible in handlers/services:

- `text`
- `attachment`
- `app_card`
- `encrypted`
- server-authored app/session events are also projected in the platform

Current limitations visible in code:

- encrypted message edits are rejected in current rollout
- reactions on encrypted DM messages are disabled in current rollout

### 6.6 Async Messaging Pipeline

The gateway supports async send via Kafka when `APP_USE_KAFKA_SEND=true`.

Current pipeline:

1. gateway validates request and publishes ingress
2. `messages-processor` consumes `msg.ingress.v1`
3. processor validates membership and idempotency against Postgres
4. processor allocates monotonic `server_order`
5. processor optionally shadow-writes canonical Postgres message rows
6. processor writes Cassandra timeline rows
7. processor writes Redis ack payloads
8. processor publishes `msg.persisted.v1`
9. processor publishes `microservice.events.v1`
10. processor publishes `msg.sms.dispatch.v1` for SMS intent
11. processor publishes Redis user fanout payloads
12. `delivery-processor` emits delivery rows/events for online recipients
13. `sms-processor` marks SMS dispatch as sent in dev mode

Current Kafka topics created by compose:

- `msg.ingress.v1`
- `msg.persisted.v1`
- `msg.delivery.v1`
- `msg.sms.dispatch.v1`
- `presence.events.v1`
- `microservice.events.v1`
- `msg.ingress.dlq.v1`
- `msg.delivery.dlq.v1`
- `msg.sms.dlq.v1`

### 6.7 Realtime, WebSocket, Presence, And Sync

Implemented behaviors:

- websocket auth via access token
- v1 and v2 websocket handlers
- hello/resume semantics
- typing start/stop propagation
- presence touch/update through Redis-backed state
- fanout of message/delivery/sync events
- incremental sync endpoint
- incremental sync v2 with opaque/base64 cursor handling
- delivered checkpointing
- event stream endpoint
- background sync fanout worker

Implemented endpoints:

- `GET /v1/ws`
- `GET /v2/ws`
- `GET /v1/events/stream`
- `GET /v1/sync`
- `GET /v2/sync`
- `GET /v1/presence/{userID}`
- `GET /v1/conversations/{id}/presence`
- `POST /v2/conversations/{id}/delivered`

Test coverage confirms:

- typing broadcast behavior
- resume replay from last cursor
- typing cleanup on disconnect
- sync payload/cursor behavior
- delivered/read status projection

### 6.8 Devices, Push, Device Attestation, And E2EE Foundations

Implemented behaviors:

- device registration/list/update/revoke
- push token registration by provider
- attestation challenge and verify
- relay attestation enforcement hook
- device identity bundle publish
- one-time prekey upload
- user bundle list/claim
- key backup CRUD + restore marker
- notification preferences
- FCM/APNs provider integration in worker process

Implemented endpoints:

- `POST /v1/devices`
- `GET /v1/devices`
- `PATCH /v1/devices/{id}`
- `DELETE /v1/devices/{id}`
- `POST /v1/devices/{id}/push-token`
- `POST /v1/devices/{id}/attestation/challenge`
- `POST /v1/devices/{id}/attestation/verify`
- `PUT /v1/device-keys/{deviceID}`
- `POST /v1/device-keys/{deviceID}/prekeys`
- `GET /v1/device-keys/{userID}`
- `POST /v1/device-keys/{userID}/claim`
- `GET /v1/device-keys/backups`
- `PUT /v1/device-keys/backups/{name}`
- `GET /v1/device-keys/backups/{name}`
- `POST /v1/device-keys/backups/{name}/restore`
- `DELETE /v1/device-keys/backups/{name}`
- `GET /v1/notifications/preferences`
- `PUT /v1/notifications/preferences`
- `POST /v1/notifications/send`

### 6.9 Media

Implemented behaviors:

- attachment metadata registration
- upload token creation
- raw object upload by token
- upload completion
- download token creation
- download-by-token streaming
- attachment purge
- local object store implementation
- redacted-media cleanup worker hook

Implemented endpoints:

- `POST /v1/media/attachments`
- `GET /v1/media/attachments/{id}/download`
- `DELETE /v1/media/attachments/{id}`
- `POST /v1/media/uploads`
- `PUT /v1/media/uploads/{token}`
- `POST /v1/media/uploads/{token}/complete`
- `GET /v1/media/downloads/{token}`

### 6.10 Mini-App Runtime And Sharing

The mini-app feature set is split across two runtime domains:

- gateway
  - owns manifest registration aliasing, app sessions, sharing, session state/events, and install/status passthrough
- apps service
  - owns registry/catalog/release workflow/install catalog source of truth

Implemented gateway mini-app behaviors:

- register manifest
- create/get/end/join session
- append session event
- snapshot session state with version checks
- share mini-app session into a conversation
- list apps
- list installed apps
- get app details
- install/uninstall app
- check for updates
- publisher/admin registry passthrough routes

Implemented gateway endpoints:

- `POST /v1/miniapps/manifests`
- `POST /v1/miniapps/sessions`
- `GET /v1/miniapps/sessions/{id}`
- `DELETE /v1/miniapps/sessions/{id}`
- `POST /v1/miniapps/sessions/{id}/join`
- `POST /v1/miniapps/sessions/{id}/events`
- `POST /v1/miniapps/sessions/{id}/snapshot`
- `POST /v1/miniapps/shares`
- alias routes under `/v1/apps/*`

Implemented mini-app validation/runtime details:

- manifest schema validation
- manifest signature verification for RS256 and Ed25519
- unsigned local web bundles allowed for dev flow
- permission grant sanitization
- session launch context generation
- session state snapshot persistence
- sync event fanout on share
- registry client passthrough to apps service

### 6.11 Mini-App Registry Service

Implemented standalone `services/apps` capabilities:

- publisher app ownership
- immutable releases by `app_id + version`
- review workflow statuses
  - `draft`
  - `submitted`
  - `under_review`
  - `needs_changes`
  - `approved`
  - `rejected`
  - `suspended`
  - `revoked`
- catalog listing and filtering
- install tracking
- update detection
- developer-mode visibility for dev apps
- permission-expansion consent gating
- PostgreSQL-backed registry state
- JSON file-backed fallback state
- review/install audit log persistence

Implemented host-facing endpoints:

- `GET /v1/apps`
- `GET /v1/apps/installed`
- `GET /v1/apps/{appID}`
- `POST /v1/apps/{appID}/install`
- `DELETE /v1/apps/{appID}/install`
- `GET /v1/apps/{appID}/updates`
- compatibility `POST /v1/apps/register`

Implemented publisher/admin endpoints:

- `POST /v1/publisher/apps`
- `POST /v1/publisher/apps/{appID}/releases`
- `GET /v1/publisher/apps/{appID}/releases`
- `POST /v1/publisher/apps/{appID}/releases/{version}/submit`
- `POST /v1/publisher/apps/{appID}/releases/{version}/revoke`
- `POST /v1/admin/apps/{appID}/releases/{version}/start-review`
- `POST /v1/admin/apps/{appID}/releases/{version}/needs-changes`
- `POST /v1/admin/apps/{appID}/releases/{version}/approve`
- `POST /v1/admin/apps/{appID}/releases/{version}/reject`
- `POST /v1/admin/apps/{appID}/releases/{version}/suspend`

Supported catalog filters visible in README/handlers:

- `q`
- `source_type`
- `visibility`
- `platform`
- `installed`
- `review_status`
- `limit`
- `cursor`
- `developer_mode`

### 6.12 Relay And Carrier Interop

Implemented relay behaviors:

- create relay message/job
- get relay job
- list queued jobs
- accept relay job
- submit relay result
- retry metadata and expiry enforcement
- required device capability checks
- attestation-aware acceptance path

Implemented endpoints:

- `POST /v1/relay/messages`
- `GET /v1/relay/jobs/{id}`
- `GET /v1/relay/jobs/available`
- `POST /v1/relay/jobs/{id}/accept`
- `POST /v1/relay/jobs/{id}/result`

Implemented carrier behaviors:

- import carrier messages
- list carrier messages
- link carrier message to server message
- list links
- admin link audit listing

Implemented endpoints:

- `POST /v1/carrier/messages/import`
- `GET /v1/carrier/messages`
- `POST /v1/carrier/messages/{id}/link`
- `GET /v1/carrier/messages/{id}/links`
- `GET /v1/admin/carrier_message_links`

### 6.13 Abuse, Audit, And Security Controls

Implemented behaviors:

- abuse event recording
- user score lookup
- destination score lookup
- OTP throttle checks
- security audit event persistence package
- content validation middleware
- auth middleware
- API version/deprecation headers
- rate limiting token bucket backed by Redis
- CSP header injection at gateway

Implemented endpoints:

- `POST /v1/abuse/events`
- `GET /v1/abuse/score/{id}`
- `GET /v1/abuse/destination`
- `POST /v1/abuse/otp/check`

## 7. Data Model Coverage

The migration history shows broad schema coverage in PostgreSQL.

### 7.1 Account / Auth / Device Tables

Implemented schema domains include:

- `users`
- `devices`
- `refresh_tokens`
- `phone_verification_challenges`
- `account_recovery_codes`
- `two_factor_methods`
- `device_push_tokens`
- `device_attestation_challenges`
- `device_pairing_sessions`
- `account_exports`
- `account_deletion_audit`
- `security_audit_events`

### 7.2 Conversation / Message Tables

Implemented schema domains include:

- `conversations`
- `conversation_members`
- `conversation_external_members`
- `external_contacts`
- `conversation_counters`
- `conversation_thread_keys`
- `conversation_invites`
- `conversation_bans`
- `messages`
- `message_deliveries`
- `message_reactions`
- `message_edits`
- `message_read_receipts`
- `message_effects`
- `user_conversation_state`
- `user_blocks`

### 7.3 Sync / Eventing Tables

Implemented schema domains include:

- `domain_events`
- `user_inbox_events`
- `user_device_cursors`

### 7.4 Media / Notification Tables

Implemented schema domains include:

- `attachments`
- `upload_tokens`
- `notifications`
- `notification_preferences`

### 7.5 Mini-App Tables

Implemented schema domains include:

- gateway-side:
  - `miniapp_manifests`
  - `miniapp_sessions`
  - `miniapp_events`
  - `miniapp_releases`
  - `miniapp_installs`
- apps-service-side:
  - `miniapp_registry_apps`
  - `miniapp_registry_releases`
  - `miniapp_registry_installs`
  - `miniapp_registry_publisher_keys`
  - `miniapp_registry_review_audit_log`

### 7.6 Relay / Carrier / Abuse / E2EE Tables

Implemented schema domains include:

- `relay_jobs`
- `carrier_messages`
- `carrier_message_links_audit`
- `abuse_events`
- `abuse_scores`
- `device_identity_keys`
- `device_one_time_prekeys`
- `device_key_backups`

## 8. Eventing And Storage Beyond Postgres

### 8.1 Redis Usage

Current Redis responsibilities visible in code:

- rate limiting
- presence tracking
- typing freshness
- message ack correlation
- user-targeted pubsub fanout
- delivery pubsub
- sync/realtime assist state

### 8.2 Cassandra Usage

Current Cassandra usage:

- `messages_by_conversation`
  - time-bucketed conversation timeline table
  - descending `server_order`
  - 1-year default TTL

### 8.3 Domain Event Projection

Current code persists and projects domain events for:

- message created
- message edited
- message deleted/tombstoned
- reactions updated
- message effects
- read receipts
- typing
- conversation state changes
- mini-app share/session state events

## 9. Web Client Implementation

The web client is substantially more implemented than the web README implies.

### 9.1 Core Messaging UX

Implemented browser features in `apps/web/app.js`:

- phone OTP auth
- refresh/logout
- conversation list and active thread
- draft phone thread bootstrap
- direct and group conversation creation
- per-user conversation cache in browser storage
- message send for OHMF and SMS fallback
- message replies and reply jumpbacks
- reactions
- edits
- deletes
- delivery/read state rendering
- typing indicators
- local search/filtering of visible thread list
- block/unblock controls
- close conversation control

### 9.2 Realtime And Sync

Implemented browser transport/state features:

- websocket connect/resume using access token
- `v2/sync` incremental replay
- delivered checkpointing
- notification click deep-link reopen
- live refresh/reconnect logic
- service worker registration
- push subscription registration

### 9.3 Attachments And Encryption

Implemented browser crypto/media features:

- attachment upload flow through gateway media endpoints
- encrypted attachment upload for DM flows
- Web Crypto use for AES-GCM attachment encryption
- IndexedDB-backed crypto device state
- device bundle publish to `/v1/device-keys/{deviceID}`
- user bundle fetch/claim
- X25519 / Ed25519 signal-style key material management
- encrypted conversation payload creation/decryption

### 9.4 Mini-App UX In Web Client

Implemented mini-app browser features:

- built-in dev catalog bootstrap for Counter Lab and Mystic 8-Ball
- catalog loading from gateway/apps service
- install/update/consent handling
- embedded iframe runtime
- permission-grant toggles
- session create/fetch/join/reset
- session state snapshot persistence
- share mini-app to conversation
- open app cards from message thread

### 9.5 Standalone Mini-App Runtime Lab

`apps/web/miniapp-runtime.js` implements a separate runtime test page with:

- manifest loading and validation
- local or gateway-backed session mode
- bridge logging
- transcript projection
- host permission enforcement
- storage/session/shared state emulation
- media picker and in-app notification bridge calls

## 10. Android Mini-App Host Implementation

The Android app is a scaffolded but real source implementation, not just docs.

Implemented pieces:

- Gradle Android app structure under `apps/android/miniapp-host`
- catalog screen
- registry HTTP client
- install store
- runtime `WebView` activity
- JS bridge exposed through `JavascriptInterface`
- asset-hosted shell page
- launch context generation
- session/shared storage bridge handlers
- projected message transcript stubs
- host-side permission enforcement for bridge methods

Current status boundaries:

- source exists
- Android SDK is not present in this environment
- repo does not verify a successful Android build in this workspace

## 11. Shared Mini-App Package

`packages/miniapp` currently includes:

- `manifest.schema.json`
  - canonical mini-app manifest schema
- `bridge-contract.md`
  - host/app envelope contract
- `sdk-web/index.js`
  - reusable client bridge library
- `sdk-types/index.d.ts`
  - TypeScript declarations
- `test-harness/mock-host.js`
  - local reusable mock host
- `tools/miniapp-cli.mjs`
  - CLI commands for validate, sign, package, upload-draft, submit
- examples
  - `counter`
  - `eightball`

Known bridge capabilities exposed by the SDK/runtime:

- `conversation.read_context`
- `conversation.send_message`
- `participants.read_basic`
- `storage.session`
- `storage.shared_conversation`
- `realtime.session`
- `media.pick_user`
- `notifications.in_app`

## 12. Shared Protocol Package

`packages/protocol` currently includes:

- OpenAPI spec
  - `openapi/openapi.yaml`
- protobuf envelope
  - `proto/envelope.proto`
- JSON event schemas
  - `events/message-ingress.schema.json`
  - `events/message-persisted.schema.json`
- JSON data schemas
  - `schemas/miniapp_manifest.schema.json`
  - `schemas/relay_job.schema.json`
  - `schemas/sync_cursor.schema.json`
  - `schemas/unified_timeline_item.schema.json`
- canonical SQL schema snapshot
  - `sql/ohmf_schema.sql`
- Go protocol types
  - timeline
  - sync
  - relay
  - mini-app
  - constants

## 13. Observability And Operations

Implemented operational pieces:

- `/healthz`
- `/readyz`
- `/metrics`
- Prometheus scrape config
- starter alert rules
- Grafana datasource/dashboard provisioning
- gateway HTTP metrics middleware
- metrics tests
- runtime `_services` visibility endpoint

The gateway also injects:

- API version headers
- CORS policy
- Content-Security-Policy header
- request ID / recoverer / timeout middleware

## 14. Test And Verification Coverage

Current repository tests cover real implementation slices across:

- auth and token refresh
- config loading
- conversation policy/preferences/invites/block propagation
- mini-app manifest validation/signatures/sharing
- websocket typing/resume behavior
- device attestation verification
- device key validation and integration
- message effects, read receipts, forwarding, deletes, edits, reactions
- presence
- observability metrics
- middleware validation/versioning
- discovery limits/hashing
- media local storage binding
- replication event writing
- relay lifecycle and capability enforcement
- sync cursor parsing/handler responses
- user export/deletion
- phase-1 integration slice
- MVP auth/message flow integration
- OpenAPI serving integration
- client-generated ID roundtrip integration

## 15. Current Boundaries And Non-Implemented Areas

This repo is broad, but not every area is complete or production-hardened.

Clearly visible current limits:

- many top-level service directories are placeholders only
- `sms-processor` is a dev stub, not a real carrier dispatcher
- Android host exists as source, but was not compiled in this environment
- some internal READMEs are stale relative to the actual code
- web README understates current implemented behavior
- gateway still centralizes many domains that appear service-separated in folder naming
- Cassandra reads are optional and disabled by default in compose

Current rollout-specific restrictions visible in code:

- encrypted edits disabled
- encrypted DM reactions disabled
- mini-app support can be rejected for conversations lacking compatible participants/devices

## 16. Practical Summary

If this project is described strictly by what exists in code today, it is already:

- a working gateway-centric messaging backend
- a mini-app-enabled messaging platform
- a partially event-driven system using Kafka, Redis, Postgres, and optional Cassandra
- a browser client with realtime, sync, attachments, encryption, and mini-app embedding
- a separate mini-app registry/review/install service
- a repository that still contains several future service boundaries as placeholders rather than extracted runtimes

The most accurate short description is:

OHMF currently implements a feature-rich monolithic gateway plus supporting registry/processors, not a fully decomposed microservice mesh.
