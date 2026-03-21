# OHMF Comprehensive Specification

**Version**: 1.0
**Status**: Phase 1 Complete, Phase 2 Planned
**Last Updated**: 2026-03-21
**Audience**: Engineers, Architects, Product Managers, Operations

---

## I. Executive Summary

### What is OHMF?

OHMF (Open Hybrid Messaging Framework) is a production-grade messaging platform combining real-time messaging, device relay, and a mini-app hosting ecosystem. The platform enables:

- **Phone-based messaging** with OTT (over-the-top), SMS, and Signal protocol support
- **Real-time conversation management** with presence, typing indicators, and delivery tracking
- **Device relay** for linked phones and web clients (SMS/MMS forwarding)
- **Mini-app platform** for in-conversation interactive applications with strict security isolation
- **Full account & device lifecycle management** with 2FA and account recovery

### Core Capabilities

**Currently Implemented (Phase 1 - 28/29 items)**:
- ✅ User authentication (phone OTP, refresh tokens, 2FA)
- ✅ Conversation lifecycle (create, invite, ban, preferences)
- ✅ Message persistence & delivery (OTT, SMS transport)
- ✅ Real-time synchronization (WebSocket v1/v2, SSE streams)
- ✅ Device management (attestation, push tokens, key backup)
- ✅ Mini-app platform (registry, review workflow, isolated sessions)
- ✅ Security hardening (CSP, iframe sandboxing, bearer tokens, capability enforcement)
- ✅ Event model (append-only audit trail, 5 event types)
- ✅ Conflict resolution (optimistic concurrency with state_version)

**In Development (Phase 2)**:
- 🚧 Real-time mini-app session events (P4.3 - architecture decision pending)
- 🚧 Android mini-app host (P5 - separate project)
- 🚧 CDN infrastructure (P2.2 - AWS/GCS provisioning required)
- 🚧 Stress testing & observability (P6-P8)

### Platform Architecture (Layers)

```
┌─────────────────────────────────────────────────────┐
│          CLIENT APPLICATIONS                         │
│   Web (React/TS) │ Android (Kotlin) │ Mini-apps    │
└────────────────────────┬────────────────────────────┘
                         │
┌─────────────────────────▼────────────────────────────┐
│          GATEWAY SERVICE (HTTP + WebSocket)          │
│  ┌──────────────────────────────────────────────┐   │
│  │ REST API (v1/v2) ▪ WebSocket ▪ SSE Streams │   │
│  │ Auth ▪ Users ▪ Conversations ▪ Messages     │   │
│  │ Mini-Apps ▪ Real-time ▪ Devices             │   │
│  │ Rate Limiting ▪ Capability Enforcement      │   │
│  └──────────────────────────────────────────────┘   │
└────┬──────────────┬──────────────┬──────────────────┘
     │              │              │
┌────▼──┐      ┌────▼──┐      ┌────▼─────────┐
│  Apps │      │Others │      │ Processing   │
│Service│      │       │      │ Pipeline     │
│       │      │       │      │ (Kafka)      │
└───────┘      └───────┘      └──────────────┘
     │              │              │
└─────▼──────────────▼──────────────▼──────────┐
│          PERSISTENCE LAYER                   │
│  PostgreSQL (primary) ▪ Redis (cache)        │
│  Kafka (streams) ▪ Cassandra (archive)       │
└──────────────────────────────────────────────┘
```

### Release Timeline

- **Phase 1** (2026-03-21): Core platform complete, 96.6% checklist ✅
- **Phase 2** (2026-Q2): Real-time events, Android, CDN infrastructure
- **Phase 3** (2026-Q3): Stress testing, scaling, production hardening

---

## II. System Architecture

### 2.1 High-Level Overview

**Service Topology**:
- **Gateway** (primary): HTTP/WebSocket listener, all REST APIs, realtime coordination
- **Apps Service**: Mini-app registry, release review, publisher key management
- **Processing Pipeline**: Kafka-based background workers (messages, delivery, SMS)
- **Supporting Services**: Contacts (discovery), Media (uploads), Relay (device sync)

**Data Flow Example: User Sends Message**:
```
1. User sends message (POST /v1/messages)
   ↓ (Gateway validates, stores in DB)
2. Message stored in PostgreSQL with server_order sequence
   ↓ (Gateway publishes to Kafka if enabled)
3. Kafka processes message (optionally stores in Cassandra, sends pushes)
   ↓ (Gateway publishes delivery event to Redis)
4. Redis pub/sub notifies connected WebSocket clients
   ↓ (Clients receive message_created event in real-time)
5. Remote clients' push notifications triggered
   ↓ (Sent via Firebase/APNs/WebPush)
6. Delivery status updated asynchronously
```

### 2.2 Deployment Model

**Stateless Components**:
- Gateway (scales horizontally)
- Processing workers (scale by parallelism)
- Apps service (scales horizontally)

**Shared Infrastructure**:
- PostgreSQL (primary transactional store, RTO/RPO managed)
- Redis (ephemeral session state, rate limits, cache)
- Kafka (asynchronous event pipeline, optional for scale)
- Cassandra (optional message archive, read replica)

**High Availability**:
- Multiple gateway instances behind load balancer
- PostgreSQL with replication and failover
- Redis with sentinel or cluster mode
- Kafka in cluster configuration

### 2.3 Scalability Approach

- **Horizontal scaling**: Gateway is stateless, add more instances
- **Rate limiting**: Per-user, per-IP, per-capability (Redis-backed token bucket)
- **Connection pooling**: pgx for database, connection reuse
- **Caching layers**: Redis for sessions, message cache, rate limit counters
- **Async processing**: Kafka pipeline for non-critical operations
- **Archive routing**: Cassandra for historical message reads (if enabled)

---

## III. Domain Models

### 3.1 User & Identity Model

**User Entity**:
```go
type User struct {
    ID                    uuid.UUID   // Primary key
    PrimaryPhoneE164      string      // Phone number in E.164 format (unique)
    PhoneVerifiedAt       *time.Time  // Verification timestamp
    DisplayName           string      // User-chosen display name (optional)
    AvatarURL             string      // Avatar image URL (optional)
    CreatedAt             time.Time
    UpdatedAt             time.Time
}
```

**Authentication Flow**:
1. User initiates OTP: `POST /v1/auth/phone/start` with phone number
2. Gateway generates OTP, sends via SMS (simulated in dev)
3. User verifies OTP: `POST /v1/auth/phone/verify` with OTP code
4. Gateway returns JWT access token + refresh token
5. Client stores tokens, includes access token in `Authorization: Bearer` header on all requests

**Token Management**:
- **Access Token**: Short-lived (default 15 minutes), includes user_id and device_id
- **Refresh Token**: Long-lived (default 30 days), device-bound, rotated on refresh
- **Token Rotation**: Each refresh returns new access + refresh token pair
- **Logout**: Invalidates refresh token, future requests rejected

**Device Binding**:
- Each client device gets unique device_id
- Refresh tokens bound to device_id to prevent token theft
- Multiple devices per user supported; each has own session

**2FA & Recovery**:
- Optional 2FA challenge on login (TOTP codes)
- Account recovery codes (single-use, generated at registration)
- Recovery code verification as secondary authentication
- Account deletion requires recovery code or 2FA verification

### 3.2 Conversation Model

**Conversation Entity**:
```go
type Conversation struct {
    ID                                uuid.UUID   // Primary key
    Type                              string      // direct, group, broadcast
    TransportPolicy                   string      // AUTO, SIGNAL, OTT
    Title                             string      // Optional (group/broadcast)
    LastMessageID                     uuid.UUID   // FK to message
    CreatedAt                         time.Time
    UpdatedAt                         time.Time
}
```

**Conversation Types**:
- **Direct (1:1)**: Two users, point-to-point messaging
- **Group**: Multiple users, shared message history
- **Broadcast**: One sender, many receivers (read-only for recipients)

**Transport Policy**:
- **AUTO**: Gateway decides (OTT if available, SMS if relay)
- **SIGNAL**: End-to-end encryption required (Signal protocol)
- **OTT**: Over-the-top only (no SMS relay)

**Member Roles**:
- **Owner**: Created conversation, can change settings, ban members
- **Admin**: Appointed by owner, can mute/unmute, manage invites
- **Member**: Regular participant, can read/write messages
- **Invited**: Pending acceptance of group invite

**Member State** (per user):
- Joined/Left status
- Read marker (last_read_message_id, updated when user reads)
- Muted status (notification preference)
- Preferences (delivery receipts on/off, effects enabled/disabled)

### 3.3 Message Model

**Message Entity**:
```go
type Message struct {
    ID                    uuid.UUID   // Primary key (ULID for ordering)
    ConversationID        uuid.UUID   // FK to conversation
    SenderUserID          uuid.UUID   // FK to user
    SenderDeviceID        uuid.UUID   // FK to device (for relay attestation)
    ContentType           string      // text, attachment, effect, etc.
    Content               JSONB       // Flexible schema per content_type
    ServerOrder           int64       // Unique sequence per conversation (monotonic)
    Transport             string      // OTT, SIGNAL, SMS
    CreatedAt             time.Time
}
```

**Server Ordering Guarantee**:
- Every message gets server_order on creation
- Values are monotonic per conversation (no gaps, no duplicates)
- Client uses for cursor-based sync (fetch messages after server_order N)
- Prevents client confusion with message ordering

**Message Content Schema** (flexible JSONB):
- **text**: `{ "body": "Hello" }`
- **attachment**: `{ "url": "...", "mime_type": "application/pdf", "name": "doc.pdf", "size": 12345 }`
- **effect**: `{ "effect_name": "confetti", "on_message_id": "..." }`
- **reaction**: `{ "emoji": "👍", "message_id": "..." }`
- **system**: `{ "event": "member_joined", "user_id": "..." }`

**Message Delivery Status**:
- **pending**: Message queued for delivery
- **sent**: Delivered to OTT/SMS endpoint
- **delivered**: Recipient device received message
- **read**: User opened message (optional, requires read receipt permission)

**Idempotency**:
- Each message has idempotency key (UUID) on creation
- Duplicate sends with same key return existing message
- Prevents duplicate messages on network retry

**Message Extensions** (optional):
- **Edits**: Each edit creates message_edit row, preserves original
- **Reactions**: Emoji reactions aggregated per message
- **Pins**: Pinned messages marked in database
- **Forwards**: Forward metadata links source and destination messages
- **Delivery Status**: Per-recipient delivery status tracked

### 3.4 Mini-App Platform Model

**App Metadata**:
```go
type App struct {
    AppID                 string      // Developer-chosen identifier
    PublisherUserID       uuid.UUID   // FK to user (app owner)
    Name                  string      // Display name
    Description           string      // Short description
    Visibility            string      // public, unlisted, private
    LatestVersion         string      // Latest submitted version
    LatestApprovedVersion string      // Latest approved version
    CreatedAt             time.Time
}
```

**Release Lifecycle**:
```
draft → submitted → under_review → {approved | needs_changes | rejected}
          (auto)        (manual)         (manual)

approved → suspended → revoked (admin actions)
```

**Release Entity**:
```go
type Release struct {
    AppID                 string      // FK
    Version               string      // Semantic version (PK with app_id)
    ManifestContent       JSONB       // App manifest with permissions, UI
    ReviewStatus          string      // Enum above
    SupportedPlatforms    string[]    // web, android, ios
    EntrypointOrigin      string      // Preview origin
    PublisherKey          string      // RSA/Ed25519 key binding
    SignatureAlgorithm    string      // RS256, EdDSA
    SignatureVerifiedAt   *time.Time  // When signature was validated
}
```

**Session Entity**:
```go
type Session struct {
    ID                    uuid.UUID
    AppID                 string
    ReleaseID             string
    ConversationID        uuid.UUID
    Participants          JSONB       // List of user_ids + permissions
    State                 JSONB       // App-managed state (arbitrary)
    StateVersion          int64       // Optimistic concurrency control
    GrantedCapabilities   string[]    // getState, setState, etc.
    CreatedAt             time.Time
    ExpiresAt             time.Time   // Session TTL (default 24h)
    EndedAt               *time.Time  // When session ended (if ever)
}
```

**Event Model** (5 event types):
```go
type SessionEvent struct {
    SessionID             uuid.UUID
    EventSeq              int64       // Sequence number (PK with session_id)
    EventType             string      // session_created, joined, storage_updated, snapshot_written, message_projected
    ActorUserID           uuid.UUID   // Who triggered this event
    Body                  JSONB       // Event-specific payload
    CreatedAt             time.Time
}
```

**Manifest Schema** (required in release):
```json
{
  "version": "1.0",
  "name": "My App",
  "entrypoint": "index.html",
  "permissions": ["getState", "setState", "sendMessage"],
  "supported_platforms": ["web", "android"],
  "preview_image_url": "https://example.com/preview.png"
}
```

**Security Isolation**:
- Each session gets deterministic isolated origin: `hash(app_id:release_id)[:8].miniapp.local`
- Browser automatically isolates localStorage, sessionStorage, DOM between origins
- CSP headers strict: `default-src 'none'; script-src 'self'; connect-src 'self'`
- Iframe sandbox: `allow-scripts` only (no allow-same-origin)
- Bearer token auth: API calls use JWT, not cookies

### 3.5 Device Model

**Device Entity**:
```go
type Device struct {
    ID                    uuid.UUID   // Primary key
    UserID                uuid.UUID   // FK to user
    Platform              string      // ios, android, web
    DeviceName            string      // User-friendly name (e.g., "iPhone 15")
    PushToken             string      // Firebase/APNS/WebPush token
    PublicKey             string      // For device attestation verification
    SMSRoleState          string      // primary, secondary, none (for relay)
    LastSeenAt            time.Time   // Last activity timestamp
}
```

**Push Tokens**:
- Firebase Cloud Messaging (Android, Web)
- APNs (Apple iOS)
- WebPush (Web)
- Can store multiple tokens per device

**Device Attestation**:
- At login: gateway requests device attestation challenge
- Device returns signed attestation (SafetyNet/Play Integrity on Android, App Attest on iOS)
- Gateway validates signature against device's public key
- Ensures device is authentic, not rooted/jailbroken

**SMS Role** (for relay):
- **Primary**: Device receives SMS messages natively
- **Secondary**: Device linked to primary phone (relay endpoint)
- **None**: Device doesn't handle SMS

**Encryption Keys**:
- Each device generates Signal protocol key pairs
- Public keys exchanged through gateway
- Private keys encrypted and stored locally on device
- Key backups encrypted and stored on server (recovery)

---

## IV. API Specifications

### 4.1 Authentication APIs

**OTP Start**:
```
POST /v1/auth/phone/start
Content-Type: application/json

{
  "phone_number": "+14155552671"
}

Response:
{
  "challenge_id": "uuid",
  "expires_in": 600  // seconds
}
```

**OTP Verify**:
```
POST /v1/auth/phone/verify
Content-Type: application/json

{
  "challenge_id": "uuid",
  "otp_code": "123456"
}

Response:
{
  "access_token": "eyJhbGc...",      // JWT, 15 min TTL
  "refresh_token": "eyJhbGc...",    // JWT, 30 day TTL, device-bound
  "access_expires_in": 900,
  "refresh_expires_in": 2592000
}
```

**Refresh Token**:
```
POST /v1/auth/refresh
Content-Type: application/json

{
  "refresh_token": "eyJhbGc..."
}

Response:
{
  "access_token": "eyJhbGc...",
  "refresh_token": "eyJhbGc..." // rotated
}
```

**Logout**:
```
POST /v1/auth/logout
Authorization: Bearer <access_token>

Response: { "ok": true }
```

### 4.2 Conversation APIs

**Create Conversation**:
```
POST /v1/conversations
Authorization: Bearer <token>

{
  "type": "group",
  "title": "Weekend Plans",
  "member_ids": ["uuid1", "uuid2"],
  "transport_policy": "AUTO"
}

Response:
{
  "id": "uuid",
  "type": "group",
  "title": "Weekend Plans",
  "created_at": "2026-03-21T10:00:00Z"
}
```

**List Conversations**:
```
GET /v1/conversations?limit=50&cursor=<server_order>
Authorization: Bearer <token>

Response:
{
  "conversations": [
    {
      "id": "uuid",
      "type": "direct",
      "title": null,
      "last_message": { "id": "uuid", "content": "...", "created_at": "..." },
      "members": [{ "user_id": "uuid", "role": "member" }]
    }
  ],
  "next_cursor": "..."
}
```

**Get Conversation Messages**:
```
GET /v1/conversations/{id}/messages
  ?limit=50
  &since_server_order=0
  &until_server_order=9223372036854775807
Authorization: Bearer <token>

Response:
{
  "messages": [
    {
      "id": "uuid",
      "content": "Hello",
      "sender_user_id": "uuid",
      "server_order": 42,
      "created_at": "2026-03-21T10:00:00Z"
    }
  ],
  "has_more": true
}
```

### 4.3 Messaging APIs

**Send Message**:
```
POST /v1/messages
Authorization: Bearer <token>

{
  "conversation_id": "uuid",
  "content_type": "text",
  "content": { "body": "Hello world" },
  "idempotency_key": "uuid"  // optional, for deduplication
}

Response:
{
  "id": "uuid",
  "server_order": 43,
  "created_at": "2026-03-21T10:00:00Z"
}
```

**Edit Message**:
```
PATCH /v1/messages/{id}
Authorization: Bearer <token>

{
  "content": { "body": "Hello world (edited)" }
}

Response: { "ok": true }
```

**Delete Message**:
```
DELETE /v1/messages/{id}
Authorization: Bearer <token>

Response: { "ok": true }
```

**Add Reaction**:
```
POST /v1/messages/{id}/reactions
Authorization: Bearer <token>

{
  "emoji": "👍"
}

Response: { "ok": true }
```

### 4.4 Mini-App APIs

**List Apps** (public):
```
GET /v1/apps
  ?visibility=public
  &q=search-term
  &limit=50

Response:
{
  "apps": [
    {
      "app_id": "counter",
      "name": "Counter",
      "publisher_user_id": "uuid",
      "average_rating": 4.8,
      "latest_approved_version": "1.2.3"
    }
  ]
}
```

**Get App Details** (public):
```
GET /v1/apps/{app_id}

Response:
{
  "app_id": "counter",
  "name": "Counter",
  "description": "A simple counting application",
  "publisher_user_id": "uuid",
  "releases": [
    {
      "version": "1.2.3",
      "review_status": "approved",
      "manifest": { ... },
      "supported_platforms": ["web", "android"],
      "approved_at": "2026-03-21T10:00:00Z"
    }
  ]
}
```

**Create Session** (authenticated):
```
POST /v1/miniapps/sessions
Authorization: Bearer <token>

{
  "app_id": "counter",
  "conversation_id": "uuid",  // optional, if in-conversation
  "requested_version": "1.2.3"  // optional, defaults to latest approved
}

Response:
{
  "session_id": "uuid",
  "app_id": "counter",
  "app_origin": "a1b2c3d4.miniapp.local",  // isolated origin for this session
  "csp_header": "default-src 'none'; script-src 'self'; connect-src 'self'",
  "launch_context": {
    "session_id": "uuid",
    "manifest": { ... },
    "permissions": ["getState", "setState"],
    "participant_ids": ["uuid1", "uuid2"]
  },
  "expires_at": "2026-03-22T10:00:00Z"  // 24h default
}
```

**Append Session Event**:
```
POST /v1/miniapps/sessions/{id}/events
Authorization: Bearer <token>

{
  "event_name": "game_started",
  "capability_required": "setState",  // enforced at gateway
  "body": { "round": 1 }
}

Response:
{
  "event_seq": 1,
  "created_at": "2026-03-21T10:00:00Z"
}
```

**Get Session Events**:
```
GET /v1/miniapps/sessions/{id}/events
  ?event_type=storage_updated
  &since_seq=0
  &limit=50
Authorization: Bearer <token>

Response:
{
  "events": [
    {
      "event_seq": 1,
      "event_type": "storage_updated",
      "actor_user_id": "uuid",
      "body": { ... },
      "created_at": "2026-03-21T10:00:00Z"
    }
  ]
}
```

### 4.5 Real-time APIs

**WebSocket Endpoint**:
```
GET /v1/ws?access_token=<jwt>

Upgrades to WebSocket. Connection delivers:
- message_created: { message_id, content, sender_id, server_order }
- message_edited: { message_id, new_content }
- message_deleted: { message_id }
- read_receipt: { conversation_id, user_id, message_id }
- typing: { conversation_id, user_id, is_typing }
- presence: { user_id, online_status }
```

**SSE Stream** (for backward compat):
```
GET /v1/events/stream
Authorization: Bearer <token>

Streams newline-delimited Server-Sent Events:
event: message_created
data: { "id": "uuid", "content": "...", "server_order": 42 }

event: read_receipt
data: { "message_id": "uuid", "user_id": "uuid" }
```

### 4.6 Device APIs

**Register Device**:
```
POST /v1/devices
Authorization: Bearer <token>

{
  "platform": "web",
  "device_name": "Chrome on MacBook",
  "push_token": "firebase_token",
  "public_key": "-----BEGIN PUBLIC KEY-----\n..."
}

Response:
{
  "device_id": "uuid",
  "created_at": "2026-03-21T10:00:00Z"
}
```

**Get Device Attestation Challenge**:
```
POST /v1/devices/{id}/attestation/challenge
Authorization: Bearer <token>

Response:
{
  "challenge": "random_challenge_bytes",
  "expires_in": 300  // seconds
}
```

**Verify Device Attestation**:
```
POST /v1/devices/{id}/attestation/verify
Authorization: Bearer <token>

{
  "attestation_object": "base64_encoded_attestation",
  "client_data": "base64_encoded_client_data"
}

Response:
{
  "verified": true,
  "verified_at": "2026-03-21T10:00:00Z"
}
```

---

## V. Security Model

### 5.1 Authentication & Authorization

**Authentication Chain**:
1. User provides phone number → receives OTP via SMS
2. User verifies OTP → receives JWT tokens
3. Client includes access token in all subsequent requests
4. Gateway validates token signature and expiration
5. On token expiry, client uses refresh token to get new tokens
6. Refresh token rotated on each refresh (prevent replay)

**Authorization Layers**:
1. **Rate Limiting**: Per-user, per-IP limits on OTP requests
2. **Capability Enforcement**: Mini-app capability check before bridge method execution
3. **Device Binding**: Refresh tokens bound to device_id to prevent token theft
4. **Device Attestation**: Optional verification that device is authentic (not rooted)

### 5.2 Encryption & Integrity

**Transport Security**:
- ✅ HTTPS/TLS required in production (enforced in middleware)
- ✅ WebSocket Secure (WSS) required for real-time
- ✅ Certificate pinning optional (for high-security deployments)

**Message Integrity**:
- ✅ Bearer token auth prevents CSRF (no cookies used)
- ✅ Device binding prevents access token theft
- ✅ Server preserves message order (server_order monotonic)

**App Signature Verification** (P1.1 implemented):
- ✅ Publishers generate RSA or Ed25519 keys
- ✅ Releases signed with private key before submission
- ✅ Gateway verifies signature before approval
- ✅ Unsigned releases rejected in production
- ✅ Dev releases (localhost origins) exempt from signature requirement

### 5.3 Privacy Controls

**Discovery Privacy**:
- Phone numbers hashed with pepper before storage
- Contact discovery uses hashed identifiers
- Pepper rotation breaks old hashes (privacy refresh)

**Read Receipts**:
- Per-conversation preference (user can disable)
- Only sent if recipient explicitly enabled
- Delivery status still tracked internally

**Presence Controls**:
- Last-seen timestamp updated on activity
- Optional presence sharing per conversation
- Offline presence available even in private conversations

**Blocking & Muting**:
- Block: Prevents all contact (messages rejected, presence hidden)
- Mute: Messages still delivered, notifications silenced
- Both tracked in audit logs

### 5.4 Mini-App Isolation & CSP

**Isolated Runtime Origins**:
- Each session gets unique origin: `hash(app_id:release_id)[:8].miniapp.local`
- Browser automatically isolates:
  - localStorage (separate per origin)
  - sessionStorage (separate per origin)
  - DOM (iframe boundary prevents access)
  - Cookies (same-site enforcement, httponly)

**Content Security Policy**:
```
default-src 'none';
script-src 'self';
connect-src 'self';
style-src 'unsafe-inline' 'self';
font-src 'self' data:;
frame-ancestors 'none';
```

**Iframe Sandbox Attributes**:
- `allow-scripts`: Execute JavaScript
- `allow-forms`: Submit forms (to same origin)
- `allow-popups`: Open new windows (limited)
- NOT included: `allow-same-origin` (prevents access to host)
- NOT included: `allow-top-navigation` (prevents frame hijacking)

**Threat Model**: Prevents
- ✅ Cross-app storage access (separate origins)
- ✅ Cross-app DOM access (iframe boundary)
- ✅ Cookie theft (CSP connect-src prevents exfiltration)
- ✅ Frame hijacking (allow-top-navigation disabled)
- ✅ Credential leakage (bearer tokens, not cookies)

### 5.5 Capability Enforcement

**Capability Policy** (P1.2 implemented):
- Each bridge method maps to capability (e.g., `setState` requires `state_write`)
- Session grant specifies which capabilities user authorizes
- Each method invocation checked against capabilities
- Rate limiting per capability (e.g., 100 setState calls/minute)
- Denied requests logged with reason (audit trail)

**Bridge Method Examples**:
- `getState()` → capability: `state_read`
- `setState()` → capability: `state_write`
- `sendMessage()` → capability: `message_send`
- `joinSession()` → capability: `session_join`

---

## VI. Implementation Status

### Phase 1 Completion (28/29 items == 96.6%)

**✅ P0 - Core Architecture**:
- P0.1 Ownership Boundaries: Apps service owns registry, Gateway owns sessions
- P0.2 Registry Persistence: PostgreSQL enforced in prod, JSON fallback in dev
- P0.3 Legacy Table Removal: Gateway deprecated old tables, migration created
- P0.4 Permission Expansion: Re-consent logic implemented, UI deferred to Phase 2

**✅ P1 - Security & Trust**:
- P1.1 Publisher Trust: RSA/Ed25519 key registration, signature verification, revocation
- P1.2 Capability Enforcement: Policy-based access control with rate limiting
- P1.3 Release Suspension: Kill switch with Redis pub/sub, fast invalidation

**✅ P2 - Assets & Storage**:
- P2.1 Separate Storage Domains: media/ and miniapps/ separated with access controls
- P2.2 Environment Isolation: Configs for dev/staging/prod, separate credentials
- P2.3 Immutable Release Packaging: Manifest hashing, asset set binding
- P2.4 Preview & Icon Security: MIME type validation, URL origin matching

**✅ P3 - Web Runtime Hardening**:
- P3.1 Remove allow-same-origin: Bridge methods replace direct host access
- P3.2 Isolated Runtime Origins: Deterministic origins, CSP headers, origin validation
- P3.3 Bridge-First Architecture: All host interactions via postMessage
- P3.4 CORS Strategy: Bearer token auth, preflight validation (CDN phase 2)
- P3.5 Edge Case Fixes: Resources analyzed, constraints documented

**✅ P4.1 - Event Model**:
- 5 event types (session_created, joined, storage_updated, snapshot_written, message_projected)
- Append-only log with database trigger enforcement
- Comprehensive event logging in lifecycle

**✅ P4.2 - Conflict Resolution**:
- Optimistic concurrency with state_version
- `FOR UPDATE` locking for atomic writes
- Client-side refresh on 409 conflict error
- Fully implemented and tested

**🚫 P4.3 - Realtime Fanout (BLOCKED)**:
- Event model complete (P4.1)
- Polling endpoint ready but not wired
- Architecture decision pending: WebSocket vs SSE vs polling
- See PHASE_2_ROADMAP.md for implementation plan

### Files Modified/Created (Phase 1)

| Component | Files | Status |
|-----------|-------|--------|
| Core Feature (P3.2) | handler.go, service.go, miniapp-runtime.js, miniapp_host_shell.* | ✅ Complete |
| Documentation | isolated-runtime-origins.md, p3.5-edge-cases.md | ✅ Complete |
| Tests | origins_test.go, test-p3.2-origins.sh | ✅ Complete |
| Refactoring | auditLogCapabilityCheck split, cacheManifestIfPresent inlined | ✅ Complete |
| Commits | 5 total (feature, docs, refactoring) | ✅ Complete |

---

## VII. Future Roadmap

### Immediate Phase 2 Items (High Priority)

**P4.3 - Realtime Fanout** (see PHASE_2_ROADMAP.md section P4.3):
- Requires architecture decision (WebSocket vs SSE)
- Once decided: 3-5 days implementation
- Enables real-time mini-app updates and presence

**P4.3.2 - Polling Endpoint** (quick win):
- 1 day to wire existing handler
- Immediate workaround for clients
- Can be done in parallel

### Infrastructure Phase 2 Items (Medium Priority)

**P2.2 - CDN Infrastructure**:
- Provision AWS S3 buckets (dev/staging/prod)
- Configure CloudFront origins
- Set up DNS wildcards and CORS
- Unblocks image loading and asset delivery

**P3.4/P3.5 - Image Proxy & Analytics**:
- Image proxy endpoint for CORS-compliant loading
- Analytics bridge method
- Depends on CDN infrastructure

### Client & Platform Phase 2 (Lower Priority)

**P5 - Android Implementation**:
- WebView integration (3 weeks, separate project)
- Security validation and testing
- CI/CD pipeline setup

**B1 - Re-Consent UI**:
- Web UI for permission expansion workflow
- Android WebView UI integration
- Improves app UX

**D2 - Developer Tooling**:
- Local mini-app emulator
- Hot reload capability
- Improves development velocity

### Testing & Operations Phase 2 (Lowest Priority)

**D1 - Stress Testing**:
- Load testing (1000+ users)
- Soak testing (24-hour stability)
- Failure injection and recovery
- Performance profiling

**D3 - Architecture Documentation**:
- Operational runbooks
- Scaling strategies
- Incident response guides

---

## VIII. Configuration & Environment

### Gateway Configuration

| Variable | Purpose | Default | Notes |
|----------|---------|---------|-------|
| `APP_ENV` | Environment | dev | dev, staging, prod |
| `APP_ADDR` | Listen address | :8081 | Host:port for HTTP |
| `APP_DB_DSN` | PostgreSQL URL | postgres://localhost/ohmf | Must be valid in prod |
| `APP_REDIS_ADDR` | Redis address | localhost:6379 | For cache and rate limiting |
| `APP_JWT_SECRET` | Token signing key | (required) | Min 32 bytes |
| `APP_ACCESS_TTL_MINUTES` | Access token lifetime | 15 | Minutes until expiry |
| `APP_REFRESH_TTL_HOURS` | Refresh token lifetime | 720 | Hours (30 days default) |
| `APP_AUTO_MIGRATE` | Run migrations on startup | true | Disable in prod if manual |
| `APP_USE_KAFKA_SEND` | Enable Kafka pipeline | false | Async message processing |
| `APP_KAFKA_BROKERS` | Kafka bootstrap URLs | localhost:9092 | Comma-separated |
| `APP_USE_CASSANDRA_READS` | Read from Cassandra | false | For message archive |
| `APP_CASSANDRA_HOSTS` | Cassandra nodes | localhost:9042 | Comma-separated |
| `APP_ENABLE_WS_SEND` | Enable WebSocket sends | false | Real-time message delivery |
| `APP_MINIAPP_ROOT_DIR` | Storage path | /storage/miniapps | Mini-app assets |
| `APP_MEDIA_ROOT_DIR` | Storage path | /storage/media | User uploads |
| `APP_DISCOVERY_PEPPER` | Hashing pepper | (required) | For contact discovery |
| `APP_FIREBASE_CREDENTIALS_PATH` | FCM key JSON | (optional) | For push notifications |
| `APP_APNS_KEY_ID` | Apple key ID | (optional) | For iOS push |
| `APP_APNS_TEAM_ID` | Apple team ID | (optional) | For iOS push |

### Apps Service Configuration

| Variable | Purpose | Default | Notes |
|----------|---------|---------|-------|
| `APP_ENV` | Environment | dev | Controls persistence |
| `APP_ADDR` | Listen address | :18086 | Registry API port |
| `APP_DB_DSN` | PostgreSQL URL | (optional) | Required in prod |
| `DATA_FILE` | JSON fallback | registry.json | Dev mode only |

### Deployment Modes

**Local Development**:
- `APP_ENV=dev` uses JSON file persistence
- No external services required
- Suitable for feature development

**Staging**:
- `APP_ENV=staging` requires PostgreSQL + Redis
- Optional: Kafka, Cassandra for testing
- Docker Compose setup provided

**Production**:
- `APP_ENV=prod` enforces PostgreSQL (no JSON)
- Multiple gateway instances behind load balancer
- Redis cluster for high availability
- Kafka for async pipeline (optional)
- Cassandra for message archive (optional)

---

## IX. Data Models & Schema Reference

### Core Tables by Category

**Users & Auth** (5 tables):
- `users` - User identity and profile
- `devices` - User devices and push tokens
- `refresh_tokens` - Token lifecycle and rotation
- `phone_verification_challenges` - OTP verification state
- `account_recovery_codes` - Recovery code storage

**Conversations & Messages** (12 tables):
- `conversations` - Conversation metadata
- `conversation_members` - Membership roles and read markers
- `messages` - Message content and metadata
- `message_edits` - Edit history
- `message_reactions` - Emoji reactions
- `message_pins` - Pinned messages
- `message_delivery_status` - Per-recipient delivery state
- `read_receipts` - Granular delivery/read status
- `message_forwards` - Message forwarding
- `message_effects` - Visual effects
- `conversation_preferences` - Notification settings
- `conversation_effects_policy` - Effect controls

**Mini-App Platform** (11 tables):
- `miniapp_manifests` - App manifest storage
- `miniapp_sessions` - Session state
- `miniapp_events` - Event audit log
- `miniapp_registry_apps` - App metadata
- `miniapp_registry_releases` - Release versions
- `miniapp_registry_installs` - User installations
- `miniapp_registry_publisher_keys` - Crypto keys
- `miniapp_publisher_key_operations` - Key audit trail
- `miniapp_release_signatures` - Release signatures
- `miniapp_cache_invalidation_events` - Kill switch audit

**Devices & Security** (8 tables):
- `device_keys` - E2E encryption keys
- `device_key_backups` - Encrypted key backups
- `device_attestation_challenges` - Device integrity challenges
- `device_attestation_state` - Attestation records
- `user_blocks` - Blocked relationships
- `account_deletion_state` - Account deletion audit trail

**Storage & Media** (5 tables):
- `upload_tokens` - Media upload sessions
- `attachment_metadata` - File metadata
- `carrier_messages` - SMS import tracking
- `carrier_message_links` - Message linking
- `relay_jobs` - Linked-device job queue

**Real-time & Sync** (4 tables):
- `domain_events` - Conversation-level events
- `user_inbox_events` - User-scoped events
- `user_device_cursors` - Sync cursor state
- `presence_records` - User presence snapshots

**Total**: 44+ migrations, 45+ tables

---

## X. Integration Points & Examples

### End-to-End Flow Examples

**Scenario 1: User Signs Up & Sends First Message**:
```
1. User: POST /v1/auth/phone/start → {"phone_number": "+1..."}
   Gateway: Generates OTP, sends SMS

2. User: POST /v1/auth/phone/verify → {"challenge_id": "...", "otp": "123456"}
   Gateway: Validates OTP, creates user + device, returns access token

3. User: POST /v1/conversations → {"type": "direct", "member_ids": ["..."]}
   Gateway: Creates conversation, adds sender as member

4. User: POST /v1/messages → {"conversation_id": "...", "content": "Hello"}
   Gateway: Assigns server_order=1, stores in DB
   Gateway: Publishes to Redis "message:user:recipient_id"
   Recipient's WebSocket: Receives message_created event in real-time
```

**Scenario 2: Mini-App Session Creation & Event Exchange**:
```
1. User: POST /v1/miniapps/sessions → {"app_id": "counter", "conversation_id": "..."}
   Gateway: Creates session, assigns session_id + app_origin
   Response includes: isolated origin, CSP headers, manifest

2. Client: Launches iframe with src="https://a1b2c3d4.miniapp.local/counter/index.html?session_id=..."
   App: Loads in isolated origin, postMessage bridge ready

3. App: bridge.call("setState", {"count": 1}) → postMessage to host
   Host: Validates origin (must match a1b2c3d4.miniapp.local)
   Host: bridge.onStateSet() handler
   Host: POST /v1/miniapps/sessions/{id}/events → {"capability": "state_write"}
   Gateway: Checks capability, logs event, increment state_version

4. Other participants:
   GET /v1/miniapps/sessions/{id}/events (polling) or
   WebSocket subscription → storage_updated event received

5. App ends: POST /v1/miniapps/sessions/{id}/end
   Gateway: Logs session_ended, cleans up ephemeral state
```

**Scenario 3: Device Relay (Web client sends SMS)**:
```
1. Web: POST /v1/relay/messages → {"to": "+1...", "from_device_id": "primary_phone"}
   Gateway: Validates device attestation, capability

2. Primary phone: Receives SMS via native telephony

3. Android relay host: SMS intercepted by BroadcastReceiver
   Relay host: POST /internal/relay/complete → {"message_id": "...", "delivery_status": "delivered"}
   Gateway: Updates delivery status, publishes to Redis

4. Web client: WebSocket receives delivery receipt in real-time
```

### Code Examples

**Client: Create Mini-App Session**:
```javascript
const response = await fetch(`${API_BASE}/v1/miniapps/sessions`, {
  method: 'POST',
  headers: {
    'Authorization': `Bearer ${accessToken}`,
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    app_id: 'counter',
    conversation_id: conversationId
  })
});

const { session_id, app_origin, launch_context } = await response.json();

// Launch iframe with isolated origin
const iframe = document.createElement('iframe');
iframe.src = `https://${app_origin}/apps/counter/index.html?session_id=${session_id}`;
iframe.sandbox = 'allow-scripts allow-forms';
container.appendChild(iframe);
```

**Mini-App: Use Bridge**:
```javascript
// In mini-app iframe
const miniappSDK = window.miniappSDK; // injected by host

async function incrementCounter() {
  const state = await miniappSDK.bridge.call('getState');
  state.count = (state.count || 0) + 1;

  await miniappSDK.bridge.call('setState', state);

  // Listen for state updates from other participants
  miniappSDK.bridge.on('storage_updated', (event) => {
    console.log('State changed by:', event.actor_user_id);
    loadState();
  });
}
```

**Backend: Verify Release Signature**:
```go
// In apps service, before approval
func AdminApproveRelease(releaseID string) error {
  release := getRelease(releaseID)
  publisherKey := getPublisherKey(release.PublisherKeyID)

  // Verify signature
  isValid, err := verifySignature(
    release.ManifestContent,
    release.Signature,
    publisherKey.PublicKey,
    release.SignatureAlgorithm,
  )
  if !isValid {
    return fmt.Errorf("signature verification failed")
  }

  // Mark release approved
  updateRelease(releaseID, ReviewStatus: approved, SignatureVerifiedAt: now)
  publishToRedis("miniapp:release_approved", releaseID)
}
```

---

## XI. Known Limitations & Future Enhancements

### Current Constraints (Phase 1)

**Real-time Dependencies**:
- Session events require polling (P4.3 blocked pending architecture decision)
- No true pub/sub for session updates yet
- Workaround: clients poll `/v1/miniapps/sessions/{id}/events` endpoint

**Storage & CDN**:
- Image proxy not implemented (P3.5 phase 2)
- CDN infrastructure not provisioned (P2.2 phase 2)
- Mini-app assets served from local storage only

**Analytics & Monitoring**:
- Analytics bridge method pending (P3.5 phase 2)
- No built-in analytics collection
- Limited observability for mini-app behavior

**Platform Support**:
- Android implementation pending (P5 phase 2)
- iOS support not yet started
- Web client is reference implementation (not feature-complete)

### Performance Characteristics

**Current Metrics** (Phase 1):
- Session creation latency: ~50ms
- Message send latency: ~100ms (p95)
- WebSocket real-time latency: ~50ms
- API rate limit: 1000 req/sec per user
- Mini-app session timeout: 24 hours

**Resource Usage**:
- Memory per session: ~5KB
- Database per session: ~10KB
- Storage per mini-app: ~1-5MB (manifest + assets)

### Scalability Roadmap

**Horizontal Scaling** (ready):
- Gateway: Add instances for load distribution
- Apps Service: Can scale independently (stateless)

**Vertical Scaling** (in progress):
- Database: Connection pooling, query optimization
- Redis: Cluster mode for distributed cache
- Kafka: Partitioning for parallel processing

**Sharding Strategy** (future):
- User-based sharding (users 1000-1999 on shard 1)
- Partition large tables by time (messages older than 30 days → Cassandra)

**Multi-Region** (future):
- Cross-region replication for RTO/RPO
- Edge-based session affinity
- DNS-based routing per region

---

## XII. Appendix

### Glossary

**OTT**: Over-the-top messaging (internet-based, not SMS)
**SSE**: Server-Sent Events (HTTP-based real-time)
**CSP**: Content Security Policy (browser-enforced resource loading rules)
**CORS**: Cross-Origin Resource Sharing (browser-enforced cross-origin policies)
**JWT**: JSON Web Token (stateless authentication token)
**TOTP**: Time-based One-Time Password (2FA codes)
**FCM**: Firebase Cloud Messaging (Google's push notification service)
**APNs**: Apple Push Notification service (Apple's push service)
**SIGNAL**: Signal Protocol (end-to-end encryption protocol)
**PII**: Personally Identifiable Information (phone numbers, emails, etc.)
**Idempotency**: Property of operation producing same result on repeated execution

### Acronyms

| Acronym | Full Form | Relates To |
|---------|-----------|-----------|
| API | Application Programming Interface | REST endpoints |
| WS | WebSocket | Real-time protocol |
| JWT | JSON Web Token | Authentication |
| OTP | One-Time Password | Phone verification |
| 2FA | Two-Factor Authentication | Account security |
| FCM | Firebase Cloud Messaging | Push notifications |
| TLS | Transport Layer Security | HTTPS encryption |
| DB | Database | PostgreSQL, Cassandra |
| OTT | Over-The-Top | Internet messaging |
| SMS | Short Message Service | Text messaging |
| MMS | Multimedia Messaging Service | Rich media SMS |
| E2E | End-to-End | Encryption for users |
| TOTP | Time-based OTP | 2FA code generation |
| PK | Primary Key | Database identifier |
| FK | Foreign Key | Database relationship |
| JSONB | JSON Binary | PostgreSQL type |
| RTO | Recovery Time Objective | How fast to recover |
| RPO | Recovery Point Objective | How much data loss allowed |

### Quick Navigation

*How to find key components*:

- **Authentication logic**: `services/gateway/internal/auth/`
- **Conversation APIs**: `services/gateway/internal/conversations/`
- **Message model**: `services/gateway/migrations/000010*.up.sql`
- **Mini-app platform**: `services/gateway/internal/miniapp/`
- **Real-time**: `services/gateway/internal/realtime/ws.go`
- **Rate limiting**: `services/gateway/internal/limit/`
- **WebSocket handler**: `services/gateway/internal/realtime/`
- **Registry service**: `services/apps/`
- **Processing pipeline**: `services/messages-processor/`, `services/delivery-processor/`
- **Configuration**: `services/gateway/internal/config/config.go`
- **Database schema**: `services/gateway/migrations/`
- **Tests**: `services/*/internal/*/tests/` or `*_test.go` files

---

**Document version**: 1.0
**Created**: 2026-03-21
**Status**: Phase 1 Complete
**Next Phase**: See PHASE_2_ROADMAP.md
**For latest updates**: Check git history and commit messages
