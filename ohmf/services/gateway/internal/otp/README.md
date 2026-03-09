# 19.17 — Gateway: OTP & Device Linking

Mapping: OHMF spec sections 11 (Devices) and 10 (Auth).

Purpose
- Implement OTP flows for linking devices and short-lived verification flows (e.g., SMS-based verification).

Expected behavior
- Generate time-limited OTP codes, send to provided phone numbers via configured SMS provider, and validate submitted codes.

JSON Schema — OTP request
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"title":"OTPRequest",
	"type":"object",
	"required":["phone_number","purpose"],
	"properties":{
		"phone_number":{"type":"string"},
		"purpose":{"type":"string"}
	}
}
```

SQL snippet (otp storage)
```sql
CREATE TABLE gateway.otp_codes (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	phone_number TEXT NOT NULL,
	code_hash TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	attempts INT DEFAULT 0
);
```

Implementation constraints
- Store only hashed code; ensure short TTL (e.g., 5 minutes) and attempt limits.

Security considerations
- Rate-limit OTP requests and validation attempts per phone/IP.

Observability and operational notes
- Metric: `otp.sent_total`.

Testing requirements
- End-to-end test with mocked SMS provider and code validation.

References
- internal/devices for device linking choreography.