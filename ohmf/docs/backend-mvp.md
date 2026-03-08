# Backend MVP Scope

Implemented in this milestone:
- Phone OTP auth start/verify/refresh/logout API surface
- Device registration hooks in auth verify flow
- Conversations create/list/get API surface
- Messages send/list/read API surface
- PostgreSQL migrations for MVP entities
- Redis wiring for rate-limit/cache foundation
- Kafka ingress + processor pipeline
- Cassandra message persistence path
- WebSocket gateway endpoint (`/v1/ws`)
- Distributed token-bucket send/control rate limiting

Excluded from MVP:
- media pipeline
- mini-app runtime
