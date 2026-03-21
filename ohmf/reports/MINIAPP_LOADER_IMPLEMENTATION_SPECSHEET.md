# Mini-App Loader Implementation Specsheet

Date: 2026-03-21

## Scope

This specsheet describes the mini-app loader platform as it is currently implemented in the repository. It reflects the actual code paths in:

- `services/apps`
- `services/gateway/internal/miniapp`
- `apps/web`
- `apps/android/miniapp-host`
- `packages/miniapp`

This is not the original target plan restated. It is the current implementation snapshot, including known limitations.

## Architecture

The current platform is split into four layers:

1. Registry service
- Path: `ohmf/services/apps`
- Owns app catalog, release metadata, review state, and per-user installs
- Uses PostgreSQL-backed persistence when `APP_DB_DSN` is configured
- Falls back to a JSON file only when Postgres is not configured

2. Gateway host adapter
- Path: `ohmf/services/gateway/internal/miniapp`
- Owns mini-app sessions, snapshots, session events, and conversation sharing
- Proxies catalog/install/release-management routes to the registry service

3. Web host
- Path: `ohmf/apps/web`
- Loads mini-apps in sandboxed iframes
- Uses the gateway for catalog/install/session/share
- Supports explicit localhost developer-mode bootstrap for built-in demo apps

4. Android host scaffold
- Path: `ohmf/apps/android/miniapp-host`
- Provides a standalone Android WebView host skeleton
- Includes catalog loading, local install marking, runtime shell, and bridge handling
- Is source-only in this repo; it was not built in this environment

## Registry Service

Primary implementation:

- `ohmf/services/apps/registry.go`
- `ohmf/services/apps/handlers.go`
- `ohmf/services/apps/db.go`
- `ohmf/services/apps/main.go`
- `ohmf/services/apps/server_std.go`
- `ohmf/services/apps/migrations/000001_registry_postgres.up.sql`

### Data model

Registry state contains:

- `apps`
  - keyed by `app_id`
- `installs`
  - keyed by `user_id`, then `app_id`

PostgreSQL persistence now materializes that state into:

- `miniapp_registry_apps`
- `miniapp_registry_releases`
- `miniapp_registry_installs`
- `miniapp_registry_publisher_keys`
- `miniapp_registry_review_audit_log`
- `miniapp_registry_schema_migrations`

Registered app fields:

- `app_id`
- `name`
- `owner_user_id`
- `visibility`
- `summary`
- `created_at`
- `updated_at`
- `latest_version`
- `latest_approved_version`
- `releases`

Release fields:

- `version`
- `manifest`
- `manifest_hash`
- `review_status`
- `review_note`
- `source_type`
- `visibility`
- `publisher_user_id`
- `supported_platforms`
- `entrypoint_origin`
- `preview_origin`
- `created_at`
- `submitted_at`
- `reviewed_at`
- `published_at`
- `revoked_at`

Install fields:

- `app_id`
- `installed_version`
- `auto_update`
- `enabled`
- `installed_at`
- `updated_at`

### Review states

Implemented release review states:

- `draft`
- `submitted`
- `under_review`
- `needs_changes`
- `approved`
- `rejected`
- `suspended`
- `revoked`

Current initial-review behavior:

- `dev` source releases start as `approved`
- non-dev releases start as `draft`

### Manifest validation

Current server-side manifest validation checks:

- `app_id` present
- `name` present
- `version` matches semver-style pattern
- `entrypoint.type` is one of `url`, `inline`, `web_bundle`
- `entrypoint.url` present
- `message_preview.type` is `static_image` or `live`
- `message_preview.url` present
- `permissions` is an array
- `capabilities` is an object
- if `signature` exists, `alg`, `kid`, and `sig` must exist

Current manifest-derived metadata:

- `source_type`
  - `dev` for localhost/127.0.0.1 entrypoints
  - `external` for non-local URLs
  - `registry` fallback when origin cannot be derived
- `visibility`
  - `private` if `metadata.visibility` is private
  - otherwise `public`
- `supported_platforms`
  - from `metadata.supported_platforms`
  - defaults to `web`
- `summary`
  - from `metadata.summary`

### Registry endpoints

Host-facing:

- `POST /v1/apps/register`
  - compatibility endpoint for manifest registration
  - creates a release when the `app_id + version` pair does not already exist
- `GET /v1/apps`
  - lists the latest visible release per app for the current user
- `GET /v1/apps/installed`
  - lists apps installed by the current user
- `GET /v1/apps/{appID}`
  - returns the latest visible release for the app
- `POST /v1/apps/{appID}/install`
  - installs the latest approved release for the current user
- `DELETE /v1/apps/{appID}/install`
  - removes the install record for the current user
- `GET /v1/apps/{appID}/updates`
  - compares current install version to latest approved version

Publisher-facing:

- `POST /v1/publisher/apps`
  - creates a publisher-owned app shell
- `POST /v1/publisher/apps/{appID}/releases`
  - creates a draft release for an owned app
- `GET /v1/publisher/apps/{appID}/releases`
  - lists releases for an owned app
- `POST /v1/publisher/apps/{appID}/releases/{version}/submit`
  - transitions a release to `submitted`
- `POST /v1/publisher/apps/{appID}/releases/{version}/revoke`
  - transitions a release to `revoked`

Admin-facing:

- `POST /v1/admin/apps/{appID}/releases/{version}/start-review`
  - transitions a release to `under_review`
- `POST /v1/admin/apps/{appID}/releases/{version}/needs-changes`
  - transitions a release to `needs_changes`
- `POST /v1/admin/apps/{appID}/releases/{version}/approve`
  - transitions a release to `approved`
- `POST /v1/admin/apps/{appID}/releases/{version}/reject`
  - transitions a release to `rejected`
- `POST /v1/admin/apps/{appID}/releases/{version}/suspend`
  - transitions a release to `suspended`

### Visibility rules

Implemented behavior:

- admins can see all releases
- normal users can see approved public releases
- owners and publishers can also see their non-approved releases
- install uses the latest approved release only

### Persistence model

Implemented behavior:

- startup applies migrations when `APP_DB_DSN` is configured
- registry writes are serialized with a PostgreSQL advisory transaction lock
- app, release, install, and review transition changes are persisted into PostgreSQL tables
- review actions append rows to `miniapp_registry_review_audit_log`
- file-backed JSON remains available only as a fallback mode for lightweight local use

Not implemented:

- registry backup and restore procedures
- finer-grained relational write paths instead of state serialization into normalized tables

## Gateway Mini-App Host Adapter

Primary implementation:

- `ohmf/services/gateway/internal/miniapp/handler.go`
- `ohmf/services/gateway/internal/miniapp/registry_client.go`
- `ohmf/services/gateway/cmd/api/main.go`

### Responsibilities

Implemented in gateway:

- create session
- get session
- end session
- append session event
- snapshot session state
- join session
- share app card into a conversation

Proxied from gateway to registry:

- list apps
- get app
- install app
- uninstall app
- list installed apps
- check updates
- publisher create/list/submit/revoke release
- admin approve/reject release

### Gateway app routes

Implemented aliases under `/v1/apps`:

- `POST /v1/apps/register`
- `GET /v1/apps`
- `GET /v1/apps/installed`
- `GET /v1/apps/{appID}`
- `POST /v1/apps/{appID}/install`
- `DELETE /v1/apps/{appID}/install`
- `GET /v1/apps/{appID}/updates`
- `POST /v1/apps/sessions`
- `GET /v1/apps/sessions/{id}`
- `DELETE /v1/apps/sessions/{id}`
- `POST /v1/apps/sessions/{id}/join`
- `POST /v1/apps/sessions/{id}/events`
- `POST /v1/apps/sessions/{id}/snapshot`
- `POST /v1/apps/shares`
- publisher/admin endpoints mirrored at `/v1/publisher/...` and `/v1/admin/...`

Legacy/internal aliases also still exist under `/v1/miniapps/...`.

### Session behavior

Implemented session request fields:

- `manifest_id`
- `app_id`
- `conversation_id`
- `viewer`
- `participants`
- `capabilities_granted`
- `ttl_seconds`
- `state_snapshot`
- `resume_existing`

Implemented session state shape:

- `snapshot`
- `session_storage`
- `shared_conversation_storage`
- `projected_messages`
- `state_version`

Launch context currently includes:

- `app_id`
- `app_session_id`
- `conversation_id`
- `viewer`
- `participants`
- `capabilities_granted`
- `state_snapshot`
- `state_version`
- `consent_required`
- `joinable`

### Sharing behavior

Implemented share output:

- an `app_card` message payload for the conversation
- session metadata
- launch context
- state snapshot
- state version

### Current gateway limitations

- catalog/release metadata is not owned by gateway anymore
- gateway does not do full publisher signature verification itself
- gateway app catalog behavior depends on registry availability when configured

## Web Host

Primary implementation:

- `ohmf/apps/web/app.js`
- `ohmf/apps/web/runtime-config.js`

### Catalog behavior

Implemented web catalog behavior:

- loads `/v1/apps`
- normalizes entries into local UI state
- stores:
  - `appId`
  - `title`
  - `summary`
  - `sourceType`
  - `version`
  - `publishedAt`
  - `reviewStatus`
  - `latestApprovedVersion`
  - `latestVersion`
  - `updateAvailable`
  - `manifest`
  - `install` state
- sorts installed apps first, then update-available apps, then title

### Developer mode

Implemented developer-mode behavior:

- built-in mini-app bootstrap only happens when one of these is true:
  - `?dev_apps=1`
  - `localStorage["ohmf.dev_apps"] === "1"`
  - localhost/127.0.0.1 and `window.OHMF_RUNTIME_CONFIG.developer_mode === true`

Current runtime config:

- `developer_mode: true` in the checked-in localhost config

### Install/launch behavior

Implemented web host behavior:

- selecting an app fetches its manifest from `/v1/apps/{appID}`
- opening or sharing an app auto-installs via `/v1/apps/{appID}/install`
- launching creates or resumes a gateway app session
- sharing creates or resumes a gateway share session and posts an app card

### Launcher UI state

Implemented launcher behavior:

- shows installed/published/dev state in the picker
- distinguishes blocked review states from installable apps
- marks update availability in launcher state
- disables permission toggles and launch actions when an app is not installable
- changes CTA text based on install state
  - `Install & Open`
  - `Open App`
  - `Install & Send`
  - `Send`
  - `Pending Review`

### Runtime bridge behavior

Implemented web bridge methods:

- `host.getLaunchContext`
- `conversation.readContext`
- `conversation.sendMessage`
- `participants.readBasic`
- `storage.session.get`
- `storage.session.set`
- `storage.sharedConversation.get`
- `storage.sharedConversation.set`
- `session.updateState`
- `media.pickUser`
- `notifications.inApp.show`

Implemented runtime state behavior:

- in-memory granted permissions
- session snapshot persistence through gateway snapshots
- projected transcript updates
- session event append on state/message/storage changes

### Sandbox behavior

Current implemented iframe sandbox:

- `allow-scripts allow-same-origin`

This is known to be a temporary compromise and is not considered final production isolation.

## Android Host Scaffold

Primary implementation:

- `ohmf/apps/android/miniapp-host`

### Project layout

Included:

- Gradle Android app skeleton
- manifest
- layouts
- assets
- Kotlin source for catalog/runtime/bridge/store/client

### Catalog activity

Implemented in `CatalogActivity`:

- configurable API base URL input
- bearer token input
- developer-mode switch
- refresh button
- list view for catalog items
- background fetch of `/v1/apps`
- visibility filter:
  - approved releases
  - dev releases
  - all releases when local developer-mode is enabled
- local install mark on open

### Runtime activity

Implemented in `MiniAppRuntimeActivity`:

- creates a `WebView`
- disables file and content access
- enables JavaScript and DOM storage
- injects `MiniAppBridge` as `MiniAppHostBridge`
- loads local shell asset `miniapp_host_shell.html`
- passes:
  - `entrypoint`
  - `app_id`
  - `channel`
  - `parent_origin=app://ohmf-miniapp-host`

### Android shell behavior

Implemented in `miniapp_host_shell.js`:

- loads the remote mini-app entrypoint in an iframe
- appends `channel`, `parent_origin`, and `app_id` to the iframe URL
- listens for `postMessage` traffic from the iframe
- forwards request envelopes to the native bridge
- posts native responses back into the iframe

### Android bridge behavior

Implemented in `MiniAppBridge`:

- request envelope handling
- response envelope generation
- launch-context generation
- permission enforcement for granted capabilities
- session storage
- shared conversation storage
- state snapshot updates
- in-memory projected transcript
- in-app notification acknowledgment

Implemented Android bridge methods:

- `host.getLaunchContext`
- `conversation.readContext`
- `participants.readBasic`
- `storage.session.get`
- `storage.session.set`
- `storage.sharedConversation.get`
- `storage.sharedConversation.set`
- `session.updateState`
- `conversation.sendMessage`
- `notifications.inApp.show`

Current Android bridge limitations:

- no gateway-backed session persistence
- no gateway-backed share flow
- no media picker bridge implementation
- no install/uninstall/update calls beyond catalog load and local install marking
- no compile verification in this environment

## Shared Contract Package

Primary implementation:

- `ohmf/packages/miniapp/manifest.schema.json`
- `ohmf/packages/miniapp/bridge-contract.md`
- `ohmf/packages/miniapp/tools/miniapp-cli.mjs`
- `ohmf/packages/miniapp/README.md`

### Manifest schema

Implemented manifest schema includes:

- `manifest_version`
- `app_id`
- `name`
- `version`
- `entrypoint`
- `icons`
- `message_preview`
- `permissions`
- `capabilities`
- `metadata`
- `signature`

### Bridge contract doc

Implemented documentation covers:

- request envelope
- response envelope
- host event envelope
- launch-context example
- built-in host methods
- runtime rules

### CLI tooling

Implemented CLI commands:

- `validate`
- `sign`
- `package`
- `upload-draft`
- `submit`

Current CLI behavior:

- validates required manifest structure
- signs canonicalized manifest JSON with Ed25519 or RSA-SHA256
- writes `signature.alg`, `signature.kid`, and `signature.sig`
- packages local asset metadata with SHA-256 hashes
- uploads a draft release through publisher APIs
- submits a release for review

## Local Stack Integration

Implemented local stack wiring:

- registry service added to Docker Compose
- gateway configured to reach registry via `APP_APPS_ADDR`

Current compose path:

- `ohmf/infra/docker/docker-compose.yml`

## OpenAPI

Current spec updates are in:

- `ohmf/services/gateway/internal/openapi/openapi.yaml`

Documented mini-app additions include:

- catalog entries
- install state
- update status
- publisher create app
- publisher create/list/submit/revoke release
- admin approve/reject release
- existing session/share APIs

## Verification Completed

Verified in this environment:

- `go test ./...` in `ohmf/services/gateway`
- `go test ./...` in `ohmf/services/apps`
- `node --check` for:
  - `ohmf/apps/web/app.js`
  - `ohmf/packages/miniapp/tools/miniapp-cli.mjs`
- CLI manifest validation against the eight-ball example

## Known Gaps

Not implemented yet:

- uploaded binary asset storage in the registry
- a publisher/admin web portal UI
- strong production-grade manifest signature trust-chain verification
- production-grade web sandbox hardening beyond `allow-scripts allow-same-origin`
- registry-issued dedicated runtime origin separation actually enforced in the web host
- Android install/update lifecycle parity with the web host
- Android gateway-backed sessions/shares
- Android build/test execution in this environment

## Status Summary

Implemented now:

- separate mini-app registry service
- immutable release model
- review workflow
- host-facing install/catalog/update endpoints
- publisher/admin release-management endpoints
- gateway adapter/proxy integration
- web catalog-first host behavior
- shared manifest and bridge contract package
- publisher CLI
- Android host scaffold with runtime bridge

Still incomplete for a production-complete loader:

- hardened production isolation
- strong publisher trust/signing model
- durable asset storage and delivery
- full Android parity and runtime persistence
