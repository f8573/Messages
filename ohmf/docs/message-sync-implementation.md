# Message Sync Implementation

This document describes how message synchronization works in the current OHMF codebase. It is implementation-focused, not spec-first.

## Scope

This document covers:

- how messages are persisted
- how recipients learn about new messages
- how delivery and read receipt state propagates
- how the web client stays in sync
- which sync APIs exist versus which ones the web client actually uses

Primary implementation references:

- `ohmf/apps/web/app.js`
- `ohmf/services/gateway/internal/messages/service.go`
- `ohmf/services/gateway/internal/messages/handler.go`
- `ohmf/services/gateway/internal/events/handler.go`
- `ohmf/services/gateway/internal/realtime/ws.go`
- `ohmf/services/gateway/internal/sync/service.go`
- `ohmf/services/messages-processor/cmd/processor/main.go`
- `ohmf/services/delivery-processor/cmd/processor/main.go`

## High-Level Model

The current system uses a layered sync model:

1. Messages are persisted in Postgres.
2. Realtime fanout is pushed through Redis pubsub.
3. The gateway exposes two live transport surfaces:
   - Server-Sent Events at `GET /v1/events/stream`
   - WebSocket at `GET /v1/ws`
4. The web client keeps a local in-memory thread/message cache and reconciles against REST APIs.
5. Receipt state is materialized in `message_deliveries` and `conversation_members.last_read_*`.

In practice, sync is not a single mechanism. It is the combination of:

- write-side persistence
- Redis pubsub fanout
- SSE/WS live notifications
- REST reconciliation

## Message Creation Flow

### Current web client path

The web client sends messages over REST, not WebSocket:

- `POST /v1/messages`
- `POST /v1/messages/phone`

The relevant client code lives in `sendOTT`, `sendSMS`, `sendInConversation`, and `sendInDraftPhoneConversation` in `ohmf/apps/web/app.js`.

### Gateway synchronous send path

For the common synchronous path in `services/gateway/internal/messages/service.go`:

1. The gateway checks membership and block rules.
2. It increments `conversation_counters.next_server_order`.
3. It inserts the message row into `messages`.
4. It updates `conversations.last_message_id` and `conversations.updated_at`.
5. It writes an idempotency response snapshot.
6. It loads recipient user IDs from `conversation_members`.
7. After commit, it publishes a `message_created` payload to Redis channel:
   - `message:user:{recipient_user_id}`
8. After commit, if a recipient is currently online (`presence:user:{recipient}` exists), it also:
   - inserts a `DELIVERED` row into `message_deliveries`
   - publishes `delivery_update` to `delivery:user:{sender_user_id}`

This means new-message fanout and some delivery-state fanout happen immediately after the database transaction commits.

### Async processor path

There is also an async ingress path in `services/messages-processor/cmd/processor/main.go`.

That path:

1. persists the message
2. publishes a persisted event to Kafka
3. publishes `message_created` to each `message:user:{recipient}` Redis channel

This is functionally similar to the gateway sync path, but the fanout originates from the processor.

## Delivery Status Flow

The system uses `message_deliveries` for delivery state.

### Immediate online delivery

If the sender-side gateway sees that a recipient is online at send time:

- it inserts a `DELIVERED` row immediately
- it publishes a `delivery_update` to the sender channel

This is the fastest path.

### Delivery processor path

The delivery processor also consumes persisted message events and checks Redis presence:

- if `presence:user:{recipient}` exists, it inserts a `DELIVERED` row
- it publishes the update to:
  - `delivery:user:{recipient}`
  - `delivery:user:{sender}` when sender and recipient differ

This gives another path for delivery-state propagation.

### Pending delivery catch-up

When a user connects to SSE or WebSocket, the gateway calls `DeliverPendingToUser`.

That function:

- finds OHMF/OTT messages addressed to the user with no `DELIVERED` row yet
- inserts missing `DELIVERED` rows
- returns delivery updates

The gateway then republishes those updates to `delivery:user:{sender}` so senders can catch up on receipt state after reconnect or stale sessions.

## Read Receipt Flow

Read state is stored on `conversation_members`:

- `last_read_server_order`
- `last_read_at`

The web client marks reads when it loads an active thread.

### Read path

1. The client fetches `GET /v1/conversations/{id}/messages`.
2. It computes the highest incoming `server_order`.
3. It calls `POST /v1/conversations/{id}/read` with `through_server_order`.
4. `MarkRead` updates the reader's `conversation_members` row.
5. The gateway publishes `READ` updates to every sender with messages up to that order using:
   - `delivery:user:{sender_user_id}`

The sender client applies these updates by marking all outgoing messages up to `through_server_order` as `READ`.

## Live Transport Surfaces

### SSE: `GET /v1/events/stream`

The event stream does three things:

1. marks `presence:user:{user_id}`
2. subscribes to Redis:
   - `message:user:{user_id}`
   - `delivery:user:{user_id}`
3. emits:
   - `message_created`
   - `delivery_update`
   - `sync_required`

`sync_required` is driven by a periodic snapshot comparison over:

- `conversations.updated_at`
- `messages.created_at`
- `message_deliveries.updated_at`
- message count

The server emits `sync_required` whenever this aggregate snapshot changes.

### WebSocket: `GET /v1/ws`

The WebSocket gateway:

- authenticates via access token
- marks `presence:user:{user_id}`
- subscribes to:
  - `message:user:{user_id}`
  - `delivery:user:{user_id}`
- forwards those Redis messages as:
  - `message_created`
  - `delivery_update`

It also supports:

- `send_message`
- `presence_subscribe`
- `resync`
- typing events

The WebSocket handler performs pending-delivery catch-up on connect by calling `DeliverPendingToUser`.

## Web Client Sync Behavior

The current web client in `ohmf/apps/web/app.js` uses several mechanisms together.

### Initial boot

After auth, the client:

1. loads local thread cache from `localStorage`
2. fetches `GET /v1/conversations`
3. loads the active thread via `GET /v1/conversations/{id}/messages`
4. starts SSE
5. starts WebSocket

### Realtime event handling

Incoming live events are handled in:

- `handleSSEEvent`
- `handleRealtimeEvent`
- `applyIncomingMessage`
- `applyDeliveryUpdate`

For message events:

- existing thread: upsert message into the local thread cache
- active thread: rerender immediately, then reconcile via `loadMessagesForThread`
- unknown thread: reload conversations, then fetch messages for that conversation

For delivery events:

- `DELIVERED` updates patch one outgoing message
- `READ` updates patch all outgoing messages through `through_server_order`

### REST reconciliation

The client still relies on explicit REST sync even when live transport exists:

- `loadConversationsFromApi`
- `loadMessagesForThread`
- `refreshLiveState`

`refreshLiveState` reloads conversations and the active thread.

### Periodic live refresh

The web client now runs a periodic reconciliation loop:

- every 5 seconds while authenticated
- refresh immediately when the tab becomes visible again
- refresh immediately when the browser comes back online

That loop exists because live transports can stall or miss updates, and REST reconciliation is the safety net.

## REST Sync Endpoint

The gateway exposes:

- `GET /v1/sync?cursor=...`

The implementation in `services/gateway/internal/sync/service.go`:

- parses either:
  - opaque base64 JSON cursor with `timestamp_ms`
  - RFC3339 timestamp cursor
- returns message-created events since the cursor
- returns `next_cursor`

Important current limitation:

- the endpoint is message-focused
- it does not serve as the primary sync source for the current web app
- the web app mainly uses `/conversations`, `/messages`, SSE, and WebSocket

So `GET /v1/sync` exists, but the browser client's practical sync behavior is mostly conversation reload plus live event fanout.

## Redis Channels and Presence Keys

### Pubsub channels

- `message:user:{user_id}`
  - new message fanout to that user
- `delivery:user:{user_id}`
  - delivery and read updates for that user
- `typing:conv:{conversation_id}`
  - typing fanout

### Presence keys

- `presence:user:{user_id}`
  - indicates the user is currently online according to SSE/WS heartbeat
- `presence:conv:{conversation_id}:user:{user_id}`
  - set by WebSocket subscribe/presence_subscribe for conversation-scoped presence

Presence is used by send-time and processor-time logic to decide whether delivery can be marked immediately.

## Data That Drives Sync State

### Canonical message timeline

Canonical ordering comes from:

- `messages.server_order`
- `messages.created_at`

The UI generally uses fetched message lists as the authoritative timeline and uses realtime events as low-latency inserts before reconciliation.

### Delivery state

Delivery state comes from:

- `message_deliveries.state`
- `message_deliveries.updated_at`

### Read state

Read state comes from:

- `conversation_members.last_read_server_order`
- `conversation_members.last_read_at`

When listing messages for a conversation, the server computes:

- `READ`
- `DELIVERED`
- `SENT`

for sender-visible status based on those tables.

## Failure and Recovery Model

If a live event is missed, the system relies on one of these recovery paths:

1. SSE emits `sync_required`, causing `refreshLiveState`
2. the client's periodic 5-second reconciliation loop reloads state
3. tab visibility or browser online transition forces a refresh
4. reconnect to SSE/WS triggers pending-delivery catch-up
5. manual thread/message fetch reloads canonical state

This means the system is eventually consistent even when realtime fanout is missed, but the latency depends on transport health and reconciliation timing.

## Important Implementation Notes

- The web client currently uses REST for sends, even though WebSocket send is implemented.
- Both SSE and WebSocket are active in the browser client.
- The browser client does not primarily consume `GET /v1/sync`; it mainly reloads conversations/messages.
- Receipt updates are not purely ephemeral UI state; they are materialized in Postgres and then fanned out.
- Sync today is conversation-centric and message-list-centric, not event-log-centric.

## Practical Summary

If you want to understand message sync in the current codebase, think of it like this:

1. write the message to Postgres
2. fan out a per-user realtime event through Redis
3. update delivery/read tables as recipients become online or read messages
4. fan out receipt updates through Redis
5. let the web client reconcile against REST when live transport is incomplete

That is the current synchronization system.
