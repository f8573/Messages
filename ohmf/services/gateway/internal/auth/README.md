# 19.6 — Gateway: Authentication & Authorization

Mapping: OHMF spec sections 10 (Authn/Authz) and 19 (Gateway).

Purpose
- Authenticate incoming tokens (JWT), manage API keys, validate device tokens, and provide authorization checks for gateway routes and realtime topics.

Expected behavior
- Verify JWT signature with configured JWKS, validate claims (`iss`, `aud`, `exp`, `sub`).
- Provide authorization helpers: `CanAccessConversation(user_id, conversation_id)` and `CanSubscribeTopic(user_id, topic)`.

Full spec details
- Accept token types:
	- Access JWT issued by OHMF auth service.
	- Short-lived device tokens for linked devices.
	- API keys in `X-API-Key` for server-to-server integration.
- Token introspection: use local JWT verification; fallback to auth service introspect for revocation checks only if necessary.

Example JWT verification pseudocode (Go-like)
- Verify signature using RS256 and JWKS
- Validate `exp`, `nbf`, `aud`, `iss`
- Map `scope` claim to abilities.

Implementation constraints
- Keep public keys cached with TTL.
- Rate-limit introspection calls.

Security considerations
- Revoke tokens via a blacklist table or cache.
- Rotate API keys; require audit for creation and deletion.

Observability and operational notes
- Metrics: `auth.success`, `auth.failure`, `auth.revocations`.
- Log `sub` and `scope` on success.

Testing requirements
- Tests for expired, malformed, revoked, and valid tokens.
- Authorization unit tests for topic subscription rules.

References
- services/auth (if present) for token issuance semantics.
- internal/realtime for subscription authorization.