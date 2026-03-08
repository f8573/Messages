# Delivery Processor

Consumes `msg.persisted.v1`, emits normalized delivery events to `msg.delivery.v1`, and pushes WebSocket fanout notifications to Redis pubsub channels:

- `delivery:user:{user_id}`
