# 19.16 — Gateway: Internal Bus / Pub-Sub Wrapper

Mapping: OHMF spec section 8 (Events) and 19 (Gateway).

Purpose
- Abstract the platform event bus (publish/subscribe) with consistent headers, backoff, and DLQ mechanics.

Expected behavior
- Provide `Publish(topic, payload, headers)` and `Subscribe(topic, handler)` semantics with at-least-once delivery.

Implementation constraints
- Support multiple underlying brokers via adapter pattern (Kafka, Redis Streams).
- Ensure message schema validation before publish.

Security considerations
- Enforce publish ACLs via adapter layer.

Observability and operational notes
- Instrument publishes and consumer processing time.

Testing requirements
- Adapter integration tests and DLQ validation.

References
- packages/protocol for event schema.