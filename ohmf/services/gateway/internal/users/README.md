# 19.9 — Gateway: User Directory Integration

Mapping: OHMF spec sections 4 (Users) and 19 (Gateway).

Purpose
- Look up user identity, profile, preferences relevant for routing and authorization (display name, devices, linked accounts).

Expected behavior
- Provide cached `GetUser(user_id)` and `GetUserDevices(user_id)` functions.
- Cache invalidation on user-update events.

JSON Schema — user profile
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"title":"UserProfile",
	"type":"object",
	"required":["user_id","primary_phone"],
	"properties":{
		"user_id":{"type":"string"},
		"display_name":{"type":"string"},
		"primary_phone":{"type":"string"},
		"metadata":{"type":"object"}
	}
}
```

SQL snippet (user devices)
```sql
CREATE TABLE users.devices (
	device_id UUID PRIMARY KEY,
	user_id UUID NOT NULL,
	last_seen TIMESTAMPTZ,
	meta JSONB
);
CREATE INDEX ON users.devices (user_id);
```

Implementation constraints
- Use service calls to `users` service; cache locally with TTL.

Security considerations
- Respect privacy and only reveal fields authorized for the caller.

Observability and operational notes
- Cache hit/miss metrics.

Testing requirements
- Simulate stale cache and subsequent refresh paths.

References
- internal/auth for mapping sub->user_id.