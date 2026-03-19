Here’s the redesign I would recommend if you want OHMF to scale from a solid prototype into a **true internet-scale messaging system** that can support **millions of concurrently connected clients**, preserve correctness, and remove the sync fragility you are currently fighting.

The short version is:

**stop treating sync as “DB + pubsub + periodic refresh” and redesign it as “durable event stream + stateless gateways + cursor-based client replication.”**

That is the core shift.

---

# 1. Design goals

Your new system should guarantee these properties:

### Correctness

* no silent message loss from the client’s perspective
* no dependence on transient pubsub for correctness
* deterministic replay after disconnect/reconnect
* monotonic ordering within a conversation
* idempotent send, delivery, read, and sync behavior

### Scale

* millions of concurrent SSE/WS clients
* horizontal scaling of gateways
* partitionable write path
* fanout that does not require N expensive synchronous DB writes per recipient event
* bounded per-connection memory and CPU

### Operational simplicity

* transport failures do not corrupt state
* reconnects do not force full thread reload
* no 5-second safety polling loop
* clear separation between:

  * canonical storage
  * durable event propagation
  * realtime delivery
  * client cache replication

---

# 2. What is wrong with the current model

Your current design is a hybrid of three incomplete sync models:

* **state-centric**: REST reloads `/conversations` and `/messages`
* **ephemeral event-centric**: Redis pubsub fanout
* **partial replay-centric**: `/v1/sync`, but only for some message events

That causes these structural problems.

## A. Redis pubsub is being used as a correctness mechanism

Redis pubsub is fine for low-latency notification, but it is not a durable per-user log. If a client or gateway misses the pubsub event, the event is gone.

So you compensate with:

* periodic REST refresh
* reload-on-active-thread
* visibility/online refreshes
* reconnect catchup hacks

That means the system is only eventually correct by patchwork.

## B. There is no single authoritative user-visible event log

The client sees:

* message creation
* delivery updates
* read updates
* thread reorderings
* presence changes

But those do not come from one canonical replayable stream.

That means the client cannot say:

> “Give me everything since cursor X.”

Instead it has to re-derive state from multiple endpoints.

## C. The client is doing too much inference

Your web app currently:

* inserts live messages optimistically
* reloads thread state
* patches outgoing statuses
* periodically reconciles full state

That works at modest scale, but at millions of clients it becomes very expensive and brittle.

## D. Presence is overloaded

You currently use Redis presence keys to infer immediate delivery. That is okay as a fast-path hint, but it should not be a hard correctness boundary.

Presence in large systems is almost always:

* approximate
* eventually updated
* a hint for fanout strategy, not a source of truth

---

# 3. New top-level architecture

The redesign should look like this:

```text
                  ┌─────────────────────────────┐
                  │         Clients             │
                  │  Web / iOS / Android / SDK  │
                  └──────────────┬──────────────┘
                                 │
                           WebSocket/SSE
                                 │
                  ┌──────────────▼──────────────┐
                  │      Realtime Gateways      │
                  │ stateless connection layer  │
                  └───────┬───────────┬─────────┘
                          │           │
               subscribe  │           │ notify(event_id only)
                          │           │
                ┌─────────▼───────────▼─────────┐
                │   Session / Presence Service   │
                │ shard-aware online registry    │
                └────────────────────────────────┘

                                 ▲
                                 │
                          consume durable events
                                 │
           ┌─────────────────────┴─────────────────────┐
           │                                           │
┌──────────▼───────────┐                   ┌───────────▼──────────┐
│ Per-user inbox/event │                   │ Conversation write    │
│ stream / fanout log  │                   │ service               │
│ durable replayable   │                   │ authoritative writer  │
└──────────┬───────────┘                   └───────────┬──────────┘
           │                                           │
           │                                   append message,
           │                                   update counters
           │                                           │
           │                                ┌──────────▼──────────┐
           │                                │ Conversations DB     │
           │                                │ Messages DB          │
           │                                └──────────┬──────────┘
           │                                           │
           └─────────────── produced by ───────────────┘
                                 domain events

```

The key architectural idea is this:

## There are two logs

1. **Conversation log**
   Canonical messages and state transitions within a conversation.

2. **Per-user inbox/event log**
   What each user’s devices consume to stay synchronized.

The client should replicate from the **per-user event log**, not from Redis pubsub and not from ad hoc REST reloads.

---

# 4. Core principles of the redesign

## Principle 1: Realtime is not correctness

WebSocket/SSE is only:

* a fast notification path
* a low-latency event delivery channel

If it fails, correctness still comes from:

* durable per-user event log
* cursor-based replay

## Principle 2: Every user-visible state transition is an event

Not only message creation. Also:

* message_created
* message_edited
* message_deleted
* delivery_state_updated
* read_checkpoint_advanced
* conversation_created
* conversation_member_added
* conversation_metadata_updated
* typing_started/stopped if you want ephemeral, but those stay non-durable
* presence changes remain ephemeral

## Principle 3: Clients sync by cursor, not by timestamps

Never use time as the primary replay boundary.

Use:

* monotonic `event_id`
  or
* partition-local `(partition_id, offset)`

## Principle 4: One writer per conversation partition

To preserve ordering and reduce contention, conversation writes should be routed by:

* `conversation_id hash -> partition`

Within that partition, one logical writer serializes:

* next `server_order`
* conversation last_message update
* emission of domain events

## Principle 5: Delivery/read are logical checkpoints, not per-message fanout writes in the hot path

For large scale, sender-visible receipt state should be derived primarily from:

* recipient delivery checkpoint
* recipient read checkpoint

You can still materialize some rows, but the hot-path model should become checkpoint-oriented.

---

# 5. Canonical storage redesign

You need a cleaner domain model.

## A. Conversations table

Keep:

```sql
conversations (
  conversation_id bigint primary key,
  type smallint,
  created_at timestamptz,
  updated_at timestamptz,
  last_message_id bigint,
  last_server_order bigint,
  metadata jsonb
)
```

Important:

* `last_server_order` is cached here for fast reads
* only the conversation write service should mutate this

## B. Messages table

Keep canonical messages:

```sql
messages (
  message_id bigint primary key,
  conversation_id bigint not null,
  sender_user_id bigint not null,
  server_order bigint not null,
  client_message_id text,
  body jsonb not null,
  created_at timestamptz not null,
  edited_at timestamptz,
  deleted_at timestamptz,
  transport_type smallint,
  unique (conversation_id, server_order)
)
```

Notes:

* `server_order` is the authoritative conversation-local ordering
* `client_message_id` supports idempotent client retries

## C. Conversation members

Keep:

```sql
conversation_members (
  conversation_id bigint,
  user_id bigint,
  joined_at timestamptz,
  last_read_server_order bigint default 0,
  last_delivered_server_order bigint default 0,
  notification_state smallint,
  role smallint,
  primary key (conversation_id, user_id)
)
```

This is important: add **`last_delivered_server_order`**.

At scale, this is much better than materializing a `DELIVERED` row for every message on every online event.

## D. Optional message_receipts detail table

If you need exact per-message audit for specific scenarios:

```sql
message_receipts (
  message_id bigint,
  user_id bigint,
  receipt_type smallint, -- delivered/read
  created_at timestamptz,
  primary key (message_id, user_id, receipt_type)
)
```

But this should not be your primary hot path. It is too write-heavy at scale.

## E. Outbox / domain_events table

Every successful write should emit a durable domain event:

```sql
domain_events (
  event_id bigint primary key,
  aggregate_type smallint, -- conversation, receipt, membership
  aggregate_id bigint,
  event_type smallint,
  partition_key bigint,
  payload jsonb,
  created_at timestamptz
)
```

This can be:

* written transactionally in Postgres outbox style, then streamed to Kafka
  or
* emitted directly into Kafka by the partition writer if your architecture supports it safely

For scale, I strongly prefer:

* transactional DB write
* outbox relay
* Kafka durable transport

---

# 6. Introduce Kafka or a durable log layer

Redis pubsub is not sufficient as the backbone for millions of clients.

You need a durable broker. Kafka is the most obvious fit.

Use Kafka for at least these streams:

## A. `conversation-events`

Contains canonical domain events produced by the conversation writer:

* message_created
* message_edited
* conversation_updated
* membership_changed

Partition by:

* `conversation_id`

This preserves per-conversation ordering.

## B. `user-inbox-events`

Contains per-user replicated events clients consume:

* new_message_for_user
* read_checkpoint_advanced
* delivery_checkpoint_advanced
* conversation_preview_updated

Partition by:

* `user_id`

This is the most important stream for client sync.

## C. `presence-events`

Optional, ephemeral-ish but still useful for cluster state:

* user_connected
* user_disconnected
* session_expired

May not need Kafka if presence is purely in-memory/Redis, but you may still want eventing.

---

# 7. Event model redesign

This is the biggest fix.

Your system needs two event layers.

## Layer 1: Domain events

These represent actual state transitions.

Examples:

```json
{
  "event_id": 9001001,
  "type": "message_created",
  "conversation_id": 123,
  "server_order": 456,
  "message_id": 999,
  "sender_user_id": 42,
  "created_at": "2026-03-14T23:10:00Z"
}
```

```json
{
  "event_id": 9001002,
  "type": "read_checkpoint_advanced",
  "conversation_id": 123,
  "reader_user_id": 77,
  "through_server_order": 456,
  "read_at": "2026-03-14T23:10:05Z"
}
```

```json
{
  "event_id": 9001003,
  "type": "delivery_checkpoint_advanced",
  "conversation_id": 123,
  "user_id": 77,
  "through_server_order": 456,
  "delivered_at": "2026-03-14T23:10:02Z"
}
```

## Layer 2: User-facing sync events

These are derived from domain events and tailored for clients.

For user 42:

```json
{
  "user_event_id": 700000100,
  "user_id": 42,
  "type": "conversation_message_appended",
  "conversation_id": 123,
  "message_id": 999,
  "server_order": 456,
  "preview": "...",
  "unread_count_delta": 1
}
```

For the sender:

```json
{
  "user_event_id": 700000101,
  "user_id": 42,
  "type": "conversation_receipt_updated",
  "conversation_id": 123,
  "through_server_order": 456,
  "receipt_kind": "delivered",
  "actor_user_id": 77
}
```

Clients should consume **user events**, not raw domain events, because user events are:

* personalized
* already filtered by permission/membership
* ready for client cache application

---

# 8. Per-user durable inbox log

This is the real sync mechanism.

Each user should have a durable ordered event stream, conceptually like:

```sql
user_inbox_events (
  user_id bigint,
  user_event_id bigint,
  event_type smallint,
  conversation_id bigint,
  payload jsonb,
  created_at timestamptz,
  primary key (user_id, user_event_id)
)
```

At very large scale, this may not stay in Postgres long-term. You may instead:

* use Kafka as the primary event log
* materialize recent windows in a fast store
* archive older events

But the concept remains:
**every client can sync by saying “give me all user events after cursor X.”**

That is the fundamental redesign.

---

# 9. Client sync protocol redesign

Your client protocol should be rebuilt around one API.

## New primary endpoint

```http
GET /v2/sync?cursor=<user_event_id>&limit=500
```

Response:

```json
{
  "events": [
    {
      "user_event_id": 700000101,
      "type": "conversation_message_appended",
      "conversation_id": 123,
      "payload": { ... }
    },
    {
      "user_event_id": 700000102,
      "type": "conversation_receipt_updated",
      "conversation_id": 123,
      "payload": { ... }
    }
  ],
  "next_cursor": 700000102,
  "has_more": false
}
```

This endpoint becomes the **authoritative replication API**.

## WebSocket protocol

On connect:

Client sends:

```json
{
  "op": "hello",
  "last_cursor": 700000050,
  "session_id": "..."
}
```

Server replies:

```json
{
  "op": "hello_ack",
  "connection_id": "...",
  "resume_supported": true
}
```

Then the server either:

* streams missing events immediately from the user-inbox stream
  or
* instructs the client to call `/v2/sync`

Live pushes should include:

```json
{
  "op": "notify",
  "cursor_hint": 700000102
}
```

or the full event if available:

```json
{
  "op": "event",
  "event": {
    "user_event_id": 700000102,
    ...
  }
}
```

But even if live delivery fails, the client just replays from cursor.

## Remove full safety polling

Once `/v2/sync` is complete, remove:

* 5-second refresh loop
* ad hoc thread reload on every live event
* tab-visible full conversation refreshes except maybe as a soft health fallback

---

# 10. Realtime gateway redesign

Your current gateway mixes:

* auth
* live subscriptions
* send path
* presence
* Redis pubsub
* catch-up logic

For internet scale, split responsibilities.

## A. Realtime gateway responsibilities

Gateways should only do:

* auth/session establishment
* maintain WS/SSE connections
* attach connection to user
* deliver user events to active sessions
* receive lightweight client ops:

  * send_message request
  * ack cursor
  * typing
  * presence subscription

Gateways should be **stateless** with regard to durable sync.

That means if a gateway dies:

* clients reconnect elsewhere
* replay from cursor
* no sync corruption

## B. Gateway internals

Use an event-loop / nonblocking network design.

At millions of concurrent idle sockets, the limiting factors are:

* memory per connection
* heartbeat cost
* TLS termination overhead
* fanout bookkeeping

Design goals:

* no goroutine-per-connection doing heavy work
* bounded write buffers
* backpressure and disconnect slow consumers
* no synchronous DB queries in the hot delivery path

## C. Delivery path

When a new user event is produced:

1. session service knows which gateways host that user’s active sessions
2. the responsible gateway receives a notify/event
3. gateway writes to active sockets

If not connected, nothing breaks because the event remains durable.

---

# 11. Presence redesign

Presence should be split into two levels.

## A. Session presence

Tracks:

* user has active connection(s)
* which gateway owns those sessions
* last heartbeat
* device IDs

Use:

* Redis with TTL
  or
* a dedicated session registry
  or both

Schema conceptually:

```text
session:{session_id} -> {
  user_id,
  gateway_id,
  device_id,
  connected_at,
  last_seen_at
}
user_sessions:{user_id} -> [session_id...]
```

## B. Conversation presence

This is optional and more expensive. Only track if needed for typing/active-thread UX.

Presence should not be the basis for correctness. It is only:

* a fanout optimization
* a UX feature

## C. Delivery semantics

Do not define “DELIVERED” as “presence key exists”.

Instead define levels:

* `SENT`: persisted and accepted by server
* `DELIVERED`: event was made available to at least one recipient device/session or delivered into recipient durable inbox
* `READ`: recipient advanced read checkpoint

In many systems, “delivered” is effectively:

* inserted into recipient inbox / available to device sync stream

That is much easier to scale than “recipient currently online at send time.”

If you want a stricter delivered meaning:

* delivered when at least one active recipient device ACKs receipt of user event cursor >= message event
  That is feasible, but more complex.

---

# 12. Delivery and read receipt redesign

This is where you can massively improve scale.

## Current issue

You currently create/update per-message delivery rows opportunistically based on presence and connection timing.

That creates complexity and write amplification.

## Better model: checkpoints

For each `(conversation_id, user_id)`, maintain:

* `last_delivered_server_order`
* `last_read_server_order`

### Delivered checkpoint

When the recipient device receives or durably syncs messages through order N:

* advance `last_delivered_server_order = max(old, N)`

### Read checkpoint

When the user reads through order N:

* advance `last_read_server_order = max(old, N)`

That is enough to derive visible status for most messaging apps.

For sender UI:

* messages `<= read checkpoint` are READ
* else messages `<= delivered checkpoint` are DELIVERED
* else SENT

This is far cheaper than per-message rows.

### Multi-device support

Each device can report device-level cursor/checkpoint.
Server then computes user-level checkpoint as:

* delivered = max over active/durable device acknowledgements if your semantics allow it
  or
* delivered = max over any device that durably received
* read = max over any device where the user explicitly read

If you need per-device receipts, keep them separately, but most apps only expose user-level state.

---

# 13. Write path redesign

Message sends should go through a dedicated conversation write service.

## Step-by-step send path

### 1. Client sends command

```json
{
  "op": "send_message",
  "conversation_id": 123,
  "client_message_id": "uuid-123",
  "body": {...}
}
```

Can be via REST or WebSocket. For scale and simplicity, both should converge on the same backend command path.

### 2. Command router hashes to conversation partition

Use:

* `conversation_id % N`

This ensures one logical partition owns ordering.

### 3. Partition writer processes serially

It:

* validates membership/block state
* checks idempotency via `(sender_user_id, client_message_id)` or conversation-scoped token
* increments conversation `last_server_order`
* inserts message
* updates conversation summary fields
* writes outbox/domain event
* commits

### 4. Fanout pipeline consumes domain event

A fanout service receives `message_created` and determines affected users.

For each conversation member:

* emit user-inbox event
* update conversation preview materialization
* enqueue push notification if offline

### 5. Realtime notify active sessions

If recipient has active sessions:

* gateway notified to push immediately

No synchronous Redis fanout in the write transaction.

---

# 14. Fanout service redesign

The fanout service is critical at scale.

## Responsibilities

* consume domain events from `conversation-events`
* resolve recipients
* produce personalized `user-inbox-events`
* update materialized inbox/conversation preview stores
* trigger push notification jobs for offline devices
* possibly update badge/unread counters

## Important property

This service is **asynchronous but durable**.

That means the canonical message commit does not wait on:

* all recipient inbox writes
* all live socket deliveries
* all push notifications

It only waits on:

* canonical persistence
* durable emission of the domain event

That is what lets you scale large groups and spikes.

---

# 15. Materialized read models

At scale, clients should not load full raw state from normalized tables for common views.

Build materialized read models.

## A. User inbox / conversation list projection

For each user-conversation pair, maintain:

```sql
user_conversation_state (
  user_id bigint,
  conversation_id bigint,
  last_message_id bigint,
  last_message_preview text,
  last_message_at timestamptz,
  unread_count bigint,
  last_read_server_order bigint,
  last_delivered_server_order bigint,
  muted boolean,
  pinned boolean,
  updated_at timestamptz,
  primary key (user_id, conversation_id)
)
```

This powers `/conversations` cheaply.

## B. Message page cache / projection

Message history can still come from canonical messages store, but consider:

* conversation-partitioned storage
* cached pages for hot conversations
* descending indexes by `(conversation_id, server_order desc)`

## C. Receipt projection

The sender-visible derived status should be computed from checkpoints efficiently, potentially with precomputed summary fields for active threads.

---

# 16. Database scaling strategy

Postgres can go far, but for millions of concurrent clients and heavy messaging volume, you must be intentional.

## A. Partition by conversation

Messages table should be partitioned or sharded by `conversation_id`.

Why:

* preserves locality
* reduces index bloat
* supports routing writers cleanly

## B. Separate OLTP from analytics

Do not let large product analytics queries touch primary message tables.

## C. Use append-heavy patterns

Messages are append-mostly. That is good.

Avoid frequent rewrite-heavy hot rows except for:

* conversation summaries
* read checkpoints

## D. Keep transactions short

The send transaction should do only:

* idempotency check
* message insert
* conversation counter/update
* outbox append

No live fanout or expensive read-after-write loops inside.

---

# 17. Multi-region strategy

If you truly mean millions of concurrent clients globally, you need a regional story.

## Option 1: single write region, global edge gateways

Simplest strong-consistency model:

* all writes go to one primary region
* gateways distributed globally
* user sync/event delivery globally
* higher write latency for distant users

This is operationally simpler and acceptable for many apps at first.

## Option 2: regionalized conversations

More advanced:

* home each conversation in a region
* conversation writes route to home region
* user inbox replication across regions
* local gateways terminate sockets near user

This is much more complex but better long-term.

For OHMF, I would recommend:

* start with one primary write region
* globally distributed gateways if needed later
* multi-region only after the event/cursor model is stable

---

# 18. WebSocket protocol redesign in detail

You should formalize the socket protocol.

## Connection setup

Client sends:

```json
{
  "op": "hello",
  "auth_token": "...",
  "device_id": "...",
  "session_resume_token": "...",
  "last_user_cursor": 700000102
}
```

Server returns:

```json
{
  "op": "hello_ack",
  "connection_id": "gw-17:abc123",
  "server_time": "...",
  "heartbeat_interval_ms": 25000,
  "resume_window_sec": 300
}
```

## Server → client event

```json
{
  "op": "event",
  "event": {
    "user_event_id": 700000103,
    "type": "conversation_message_appended",
    ...
  }
}
```

## Client ack

```json
{
  "op": "ack",
  "through_user_event_id": 700000103
}
```

This helps:

* backpressure accounting
* delivered checkpoint updates
* resume reliability

## Resume flow

On reconnect:

```json
{
  "op": "resume",
  "session_id": "...",
  "last_user_cursor": 700000103
}
```

If gateway cannot resume directly, it tells client to sync from durable store.

---

# 19. Slow consumer and backpressure design

At millions of sockets, this matters a lot.

Each socket should have:

* bounded outbound queue
* heartbeat timeout
* compressed frames where useful
* disconnect policy for lagging consumers

If a client cannot keep up:

1. stop pushing full events
2. send `resync_required` with cursor hint
3. client replays via `/v2/sync`

This is a huge improvement over trying to preserve every live frame in memory.

---

# 20. Typing and ephemeral events

Typing should not go through the durable user-inbox log.

Typing is ephemeral.

Use a separate channel:

* gateway to gateway via Redis or lightweight broker
* TTL-based suppression/coalescing
* only delivered to actively subscribed thread viewers

Same for:

* live presence
* cursor position
* draft indicators

Durable sync and ephemeral UX must remain separate.

---

# 21. Push notification integration

For mobile/offline clients, push is a side effect of the fanout pipeline.

Fanout service logic:

* if no active sessions for user
* and notification preferences permit
* enqueue push notification job

Push should never be the primary delivery guarantee.
It is only a wake-up hint.

The durable inbox log remains the source of truth when the app opens.

---

# 22. Security and tenancy considerations

At scale, tighten these invariants.

## A. User event authorization

A user must only be able to consume events from their own user stream.

## B. Conversation membership at event creation time

Fanout should use membership snapshot valid for the event’s commit point.

## C. E2EE future-proofing

If you eventually want end-to-end encryption:

* server fanout still works on encrypted payload envelopes
* event log stores ciphertext metadata
* sync model does not fundamentally change

This is another reason to separate sync from message semantics.

---

# 23. Migration path from your current system

You do not need a big-bang rewrite. Migrate in stages.

## Phase 1: Introduce durable user event IDs

Add:

* `user_inbox_events`
* `user_event_id`
* `/v2/sync`

Keep current SSE/WS and Redis, but make them secondary.

At this point, clients can already recover correctly without full REST reloads.

## Phase 2: Expand event coverage

Make `/v2/sync` include:

* message_created
* delivery checkpoint updates
* read checkpoint updates
* conversation preview updates

Now the client can stop relying on `/conversations` and `/messages` for routine sync.

## Phase 3: Move live transport to event-driven replication

Change WS/SSE to deliver:

* actual user events
  or
* cursor hints

Remove the 5-second refresh loop.

## Phase 4: Replace Redis correctness paths

Redis becomes:

* presence
* ephemeral typing
* maybe lightweight gateway notify

Not the source of replayable sync.

## Phase 5: Introduce partitioned conversation writer

Move send path into:

* dedicated write service
* outbox/domain event production
* async fanout service

## Phase 6: Add Kafka

When traffic grows enough, migrate durable eventing from DB-only outbox relays to Kafka-backed streams:

* conversation-events
* user-inbox-events

## Phase 7: Optimize receipts to checkpoint model

Move away from per-message delivery insertions on hot path.

---

# 24. Recommended final model for OHMF

If I were designing your production target, I would choose this exact shape:

## Write plane

* `conversation-command-service`
* partitioned by `conversation_id`
* writes to Postgres
* appends outbox/domain events

## Event plane

* Kafka
* topic: `conversation-events` partitioned by `conversation_id`
* topic: `user-inbox-events` partitioned by `user_id`

## Projection plane

* fanout workers create per-user events
* inbox projection workers update `user_conversation_state`
* receipt projection workers update checkpoints

## Realtime plane

* stateless WS gateways
* user session registry
* direct user-event delivery to active connections
* durable replay from `/v2/sync`

## Storage plane

* Postgres for canonical conversation/message data
* Redis only for ephemeral presence/typing/session caches
* object store if attachments/media arrive later

## Client plane

* local cache
* single cursor per account/device
* sync via `/v2/sync`
* optional thread page fetch for older history only
* no constant full refresh

---

# 25. Concrete API redesign

## Send message

```http
POST /v2/messages
```

Request:

```json
{
  "conversation_id": 123,
  "client_message_id": "uuid",
  "body": {...}
}
```

Response:

```json
{
  "accepted": true,
  "message_id": 999,
  "conversation_id": 123,
  "server_order": 456
}
```

## Sync

```http
GET /v2/sync?cursor=700000100&limit=500
```

## Read checkpoint

```http
POST /v2/conversations/123/read
{
  "through_server_order": 456
}
```

## Delivered checkpoint from device

```http
POST /v2/conversations/123/delivered
{
  "through_server_order": 456,
  "device_id": "ios-abc"
}
```

This API allows server-side receipt derivation without constant row-level delivery writes.

---

# 26. Data ordering guarantees

You should explicitly define them.

## Guaranteed

* messages within a conversation are totally ordered by `server_order`
* user events for a given user are ordered by `user_event_id`
* `/v2/sync` never skips events after a committed cursor
* read/delivery checkpoints are monotonic

## Not guaranteed

* cross-conversation global message order
* exact wall-clock order across partitions
* ephemeral presence event delivery

This is important. Many sync bugs come from undefined guarantees.

---

# 27. Performance characteristics at scale

This redesign scales because:

## Connection scalability

Gateways only maintain sockets and small buffers. They do not perform heavy sync logic per event.

## Write scalability

Conversation writes are partitioned. No global lock.

## Fanout scalability

Fanout is asynchronous and horizontally scalable by consumer group.

## Recovery scalability

Reconnect is cursor replay, not full conversation/thread reload.

## DB scalability

Hot-path writes are bounded:

* 1 message insert
* 1 conversation summary update
* 1 outbox append
* occasional checkpoint advances

Not:

* N synchronous recipient operations
* repeated full reload queries

---

# 28. Key implementation choices I would strongly recommend

If you want the most practical production-grade version:

### Use WebSocket as the primary live transport

Keep SSE only as fallback if needed.

### Keep REST for:

* auth/bootstrap
* paginated message history
* sync replay
* read/delivered checkpoint posts

### Use Redis only for:

* presence/session cache
* typing/pubsub hints
* gateway routing metadata

### Use Kafka for:

* durable cross-service eventing
* partitioned replay
* async fanout

### Model receipts as checkpoints

Not per-message hot-path writes.

### Make `/v2/sync` the centerpiece

Not an afterthought.

---

# 29. What the web client should look like after redesign

The browser should become much simpler.

## Boot

1. authenticate
2. load local cache and last cursor
3. call `/v2/sync`
4. open WebSocket with cursor
5. render incrementally

## On live event

1. apply event directly
2. ack cursor

## On reconnect

1. reconnect socket
2. call `/v2/sync?cursor=last_cursor`
3. continue

## On active thread open

1. fetch older history page if needed
2. post read checkpoint
3. do not full-reload thread just because one live event arrived

That is dramatically cleaner than the current model.

---

# 30. Final verdict

The redesign that will carry OHMF to millions of concurrent clients is:

**a partitioned, durable, event-driven architecture where clients replicate from a per-user cursor-based inbox log and gateways are stateless live-delivery edges.**

If I had to boil it down into the most important upgrades, they are these five:

1. **Make `/sync` authoritative and cursor-based with durable user events**
2. **Introduce a per-user inbox event stream**
3. **Move message ordering to partitioned conversation writers**
4. **Replace Redis-as-correctness with Kafka/outbox durability**
5. **Model delivery/read mostly as monotonic checkpoints**

That is the architecture shift that removes your current synchronization fragility and gives you a realistic path to very large scale.