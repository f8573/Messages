# Transport Model

The platform supports two transport intents today:
- `OHMF`: native in-app delivery over the gateway, Redis fanout, and persisted message timeline.
- `SMS`: carrier-bound delivery initiated through the same message ingress path and fanned out to the SMS processor pipeline.

Ingress flow
1. Client sends a message via `POST /v1/messages`, `POST /v1/messages/phone`, or WebSocket `send_message`.
2. Gateway validates auth, schema, rate limits, and conversation membership.
3. Gateway persists synchronously or publishes an ingress event to Kafka when async send is enabled.
4. Processors persist the canonical message, emit delivery updates, and optionally dispatch carrier work.

Delivery guarantees
- Request idempotency is enforced with `idempotency_key`.
- Message ordering inside a conversation is represented by `server_order`.
- Delivery projections are eventually consistent when Kafka-backed async send is enabled.

Rate limiting
- HTTP and WebSocket control paths use the shared Redis-backed token bucket limiter in `services/gateway/internal/limit`.
- WebSocket connect limits are applied per IP.
- WebSocket control limits are applied per user.

References
- `services/gateway/internal/messages/service.go`
- `services/gateway/internal/messages/async.go`
- `services/gateway/internal/limit/token_bucket.go`
- `services/messages-processor/cmd/processor/main.go`
- `services/delivery-processor/cmd/processor/main.go`
