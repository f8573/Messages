# 19.10 — Gateway: Device Management

Mapping: OHMF spec sections 11 (Devices) and 19 (Gateway).

Purpose
- Manage linked devices, deliver push notifications to devices, and validate device tokens used by clients.

Expected behavior
- Validate device registration on connect (WS or push).
- Support push/proxy delivery to mobile devices using stored push tokens and platform (APNs/FCM) adapters.

JSON Schema — device registration
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"title":"DeviceRegistration",
	"type":"object",
	"required":["device_id","user_id","push_token","platform"],
	"properties":{
		"device_id":{"type":"string"},
		"user_id":{"type":"string"},
		"push_token":{"type":"string"},
		"platform":{"type":"string","enum":["apns","fcm"]}
	}
}
```

SQL snippet
```sql
CREATE TABLE gateway.device_registry (
	device_id TEXT PRIMARY KEY,
	user_id UUID NOT NULL,
	push_token TEXT,
	platform TEXT,
	last_registered TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX ON gateway.device_registry (user_id);
```

Implementation constraints
- Protect push tokens at rest (encrypt).
- Abstract push provider with interface for retries and backoff.

Security considerations
- Only allow device registration from authenticated owners.
- Rotate push tokens on explicit refresh.

Observability and operational notes
- Track push success, failures, and retries.

Testing requirements
- Simulate push provider failures and TTL handling.
- Verify duplicate registration behavior.

References
- infra for push provider credentials configuration.
- internal/realtime for device WS sessions.