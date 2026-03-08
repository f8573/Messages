# OHMF Messaging Architecture (Phased Dual-Write)

## Runtime Topology

Gateway (REST + WebSocket) -> Kafka bus -> message processors -> Cassandra message store -> Redis presence/rate-limit cache -> app/game microservices

## Services

- `services/gateway`
  - Public edge service.
  - Accepts REST and WebSocket ingress.
  - Enforces distributed rate limits in Redis (token bucket).
  - Produces message ingress events to Kafka.
  - Reads message timelines from Cassandra when `APP_USE_CASSANDRA_READS=true` (otherwise Postgres fallback).
  - Maintains Redis presence keys and WebSocket delivery subscriptions.
- `services/messages-processor`
  - Consumes `msg.ingress.v1`.
  - Validates membership in Postgres.
  - Assigns per-conversation `server_order` using Postgres counters.
  - Writes canonical message records to Cassandra.
  - Writes REST ack correlation payload into Redis for gateway request/reply compatibility.
  - Emits persisted events to `msg.persisted.v1` and `microservice.events.v1`.
- `services/delivery-processor`
  - Consumes `msg.persisted.v1`.
  - Emits per-user delivery events to Kafka `msg.delivery.v1`.
  - Publishes per-user delivery updates to Redis pubsub channel `delivery:user:{user_id}` for WebSocket fanout.
- `services/sms-processor`
  - Consumes `msg.sms.dispatch.v1`.
  - Handles SMS side-effects and status projection (stubbed local implementation).

## Kafka Topics

- `msg.ingress.v1` (key: `conversation_id`)
- `msg.persisted.v1` (key: `conversation_id`)
- `msg.delivery.v1` (key: `recipient_user_id`)
- `msg.sms.dispatch.v1` (key: `conversation_id`)
- `presence.events.v1` (key: `user_id`)
- `microservice.events.v1` (key: `conversation_id`)
- `*.dlq.v1` (dead-letter topics by processor)

## Data Stores

- Postgres (metadata authority in phase 1)
  - users, auth, conversations, memberships, counters, idempotency.
- Cassandra (`ohmf_messages`)
  - `messages_by_conversation ((conversation_id, bucket_yyyymmdd), server_order)`
  - `message_by_id (message_id)` (reserved for lookups/dedupe)
  - `conversation_message_meta (conversation_id)` (reserved for read optimization)
- Redis
  - Presence: `presence:user:{user_id}` and `presence:conv:{conversation_id}:user:{user_id}`
  - Rate limits: token bucket keys under `rate:*`
  - Ack correlation: `msg:ack:{event_id}`
  - Delivery pubsub: `delivery:user:{user_id}`

## Client Contracts

- REST send endpoints keep path compatibility.
  - `201` when persistence ack resolves within timeout.
  - `202` with `queued=true` and `ack_timeout_ms` when ack is pending.
- WebSocket endpoint: `GET /v1/ws`
  - Auth: `access_token` query parameter or `Authorization: Bearer`.
  - Events:
    - client: `auth`, `send_message`, `presence_subscribe`
    - server: `auth`, `ack`, `delivery_update`, `presence_update`, `error`

## Consumer Contract (`microservice.events.v1`)

Each event is JSON and versioned by schema:

- Required fields
  - `event_id`
  - `message_id`
  - `conversation_id`
  - `sender_user_id`
  - `server_order`
  - `transport`
  - `status`
  - `persisted_at_ms`
  - `delivery_targets`
- Optional fields
  - `trace_id`
- Versioning rules
  - Only additive field changes are allowed in-place.
  - Breaking changes require a new topic suffix version (`*.v2`).
  - Unknown fields must be ignored by consumers.

## Rollout Flags

- `APP_USE_KAFKA_SEND`
- `APP_USE_CASSANDRA_READS`
- `APP_ENABLE_WS_SEND`

These flags support staged cutover and rollback to Postgres synchronous message path.
