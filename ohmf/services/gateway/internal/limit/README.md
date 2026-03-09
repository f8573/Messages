# 19.14 — Gateway: Rate Limiting & Quotas

Mapping: OHMF spec section 19 (Gateway) and 16 (Throttling).

Purpose
- Enforce per-token, per-user, per-IP rate limits and longer-term quotas (daily).

Expected behavior
- Return HTTP 429 with structured error when limit exceeded.
- Support burstable token buckets with refill intervals.

JSON Schema — rate limit exceeded response
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"title":"RateLimitResponse",
	"type":"object",
	"required":["type","title","status","retry_after"],
	"properties":{
		"type":{"type":"string"},
		"title":{"type":"string"},
		"status":{"type":"integer"},
		"retry_after":{"type":"integer"}
	}
}
```

Implementation constraints
- Use Redis or in-memory with distributed coordination for rate counters.
- Ensure accuracy and bounded memory growth.

Security considerations
- Avoid side-channels that reveal system capacity; return generic messages.

Observability and operational notes
- Metrics: `rate_limiter.hit`, `rate_limiter.throttle`.

Testing requirements
- Exhaustive tests for boundary conditions and concurrency.

References
- infra for Redis/cluster config.