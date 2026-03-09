# 6 — Client: Android Messaging Core

Mapping: OHMF spec section 6 (Clients: Android).

Purpose
- Document Android client core messaging APIs, expected behaviors, and wire protocol expectations for interacting with Gateway realtime and REST endpoints.

Expected behavior
- Client must:
  - Authenticate via OAuth/OIDC to obtain tokens.
  - Establish WebSocket to `/realtime/v1?token=<jwt>`.
  - Implement reconnection/backoff and resume via `last_cursor`.

Example WebSocket message (Android client JSON)
Subscribe:
{"type":"subscribe","topic":"conversation:conv_abc","cursor":"c_123"}

Example REST send
POST /api/v1/messages
Headers:
Authorization: Bearer <jwt>
Content-Type: application/json
Body:
{
  "conversation_id":"conv_abc",
  "from":"+1555...","to":"+1555...","body":"Hi"
}

Implementation constraints
- Use networking libraries that support WebSocket + TLS pinning where required.
- Store refresh tokens securely (Android Keystore).

Security considerations
- Use secure storage; avoid logging tokens.

Observability and operational notes
- Surface metrics for connection quality (latency, dropped frames).

Testing requirements
- Integration tests with local gateway compose.
- E2E tests for reconnect/resume semantics.

References
- docs/user-signup-and-send-message.md and gateway README.
