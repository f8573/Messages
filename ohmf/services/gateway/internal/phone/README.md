# 19.12 — Gateway: Phone Number Utilities & Carrier Integration

Mapping: OHMF spec sections 3 (Phone/Numbering) and 19 (Gateway).

Purpose
- Normalize and validate phone numbers; provide carrier routing hints and integration helpers for SMS providers.

Expected behavior
- Convert incoming numbers to E.164 when possible; return validation errors when ambiguous.
- Choose appropriate SMS provider based on destination and cost/availability rules (configurable).

JSON Schema — phone validation response
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"title":"PhoneValidation",
	"type":"object",
	"required":["input","e164","valid"],
	"properties":{
		"input":{"type":"string"},
		"e164":{"type":["string","null"]},
		"valid":{"type":"boolean"},
		"country":{"type":"string"}
	}
}
```

SQL snippet (carrier logs)
```sql
CREATE TABLE gateway.carrier_logs (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	provider TEXT,
	destination TEXT,
	status TEXT,
	provider_response JSONB,
	created_at TIMESTAMPTZ DEFAULT now()
);
```

Implementation constraints
- Use libphonenumber or equivalent.
- Cache country code rules.

Security considerations
- Do not leak routing decisions to callers.

Observability and operational notes
- Metrics for validation success/failed counts.

Testing requirements
- Unit tests covering international and edge-case formats.
- Integration tests with provider sandbox.

References
- infra for provider credentials configuration.
- packages/protocol for number fields.