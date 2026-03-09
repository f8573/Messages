# 19.13 — Gateway: Token Management & Issuance Helpers

Mapping: OHMF spec sections 10 (Auth) and 19 (Gateway).

Purpose
- Validate tokens, manage revocations, issue short-lived session tokens for linked devices (via OTP flow), and provide token introspection helpers.

Expected behavior
- Provide `VerifyJWT`, `RevokeToken`, `IssueDeviceToken` APIs to other gateway modules.

JSON Schema — token introspection response
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"title":"TokenIntrospect",
	"type":"object",
	"properties":{
		"active":{"type":"boolean"},
		"sub":{"type":"string"},
		"scope":{"type":"string"},
		"exp":{"type":"integer"}
	}
}
```

SQL snippet (revoked tokens)
```sql
CREATE TABLE gateway.revoked_tokens (
	token_hash TEXT PRIMARY KEY,
	revoked_at TIMESTAMPTZ DEFAULT now(),
	reason TEXT
);
```

Implementation constraints
- Store only token hashes, not raw tokens.
- Use secure random for issued device tokens and short TTL (e.g., 10min).

Security considerations
- Protect issuance endpoints against abuse (rate limit & auth).
- Require admin action for long-lived API keys.

Observability and operational notes
- Metric: `tokens.revoked_count`.

Testing requirements
- Tests for revoked token rejection and issuance TTL.

References
- internal/otp for device linking OTP flow.