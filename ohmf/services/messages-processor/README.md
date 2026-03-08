# Messages Processor

Consumes `msg.ingress.v1`, validates metadata against Postgres, persists canonical timeline records to Cassandra, updates idempotency state, and publishes:

- `msg.persisted.v1`
- `microservice.events.v1`
- `msg.sms.dispatch.v1` (for SMS-intent events)

Also writes gateway ack correlation payload into Redis key `msg:ack:{event_id}`.
