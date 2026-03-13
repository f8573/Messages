# Identity And Account Model

Canonical identity records live in Postgres and are created during phone verification.

Core entities
- `users`: primary account identity keyed by UUID.
- `devices`: registered device records bound to a user and used for refresh/logout and linked-device flows.
- `user_blocks`: user-level block list applied during message send and discovery flows.
- `carrier_messages` and relay job ownership records: attach transport-side activity back to the authenticated account.

Lifecycle
1. `POST /v1/auth/phone/start` creates a challenge.
2. `POST /v1/auth/phone/verify` creates or reuses the user, registers a device, and issues access/refresh tokens.
3. Authenticated users manage devices, export/delete the account, and manage block state through protected routes.

Auth surface
- Access tokens: bearer JWTs for HTTP and WebSocket access.
- Refresh tokens: opaque rotation tokens stored server-side and invalidated on logout.
- Device-bound logout: `POST /v1/auth/logout`.

Schema references
- `services/gateway/migrations/000001_init.up.sql`
- `services/gateway/migrations/000003_devices.up.sql`
- `services/gateway/migrations/000007_user_blocks.up.sql`
- `services/gateway/internal/auth/service.go`
- `services/gateway/internal/users/service.go`
