# 19.8 — Gateway: Event Bus Integration

Mapping: OHMF spec section 8 (Event Bus) and 19 (Gateway).

Purpose
- Publish and subscribe to platform domain events (ingress, delivered, read, device.connected) using the platform internal bus (Kafka/Redis/NSQ depending on infra).

Expected behavior
- Reliable at-least-once publish semantics for ingress events.
- Idempotent consumer processing recommended downstream.

Event JSON Schema (message.ingress earlier) and example publish
```json
POST /internal/publish HTTP/1.1
Content-Type: application/json
Body:
{
	"topic":"message.ingress",
	"payload":{ "message_id":"msg_1", ... }
}
```

Implementation constraints
- Use async publisher library with retry/backoff.
- Include message metadata headers: `x-origin`, `x-request-id`, `x-origin-timestamp`.

Security considerations
- Authorize which components can publish to which topics.
- Sign or include provenance headers to avoid spoofing.

Observability and operational notes
- Track publish latency, retry counts, and failed publishes.
- Configure DLQ for poison messages.

Testing requirements
- Integration tests using test broker or mocked bus.
- Tests for retry and DLQ handling.

References
- infra for broker runbooks.
- packages/protocol for schema definitions.