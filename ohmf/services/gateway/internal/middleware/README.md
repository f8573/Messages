# 19.2 — Gateway: HTTP Middleware Stack

Mapping: OHMF spec section 19 (Gateway) and 10 (Security / Authn).

Purpose
- Define, implement, and document middleware components: auth validation, request ID, rate limiting, CORS, CSP, telemetry, panic recovery, and JSON validation.

Expected behavior
- Each incoming HTTP request passes through a deterministic middleware chain:
	1. Panic recovery
	2. RequestID injection / propagation
	3. Rate limiting (global, per-token, per-IP)
	4. Authentication (JWT, API Key, Device token)
	5. JSON schema validation (where applicable)
	6. Logging + tracing instrumentation

Full specification details
- Must attach `X-Request-Id` header if not present.
- Enforce `Content-Type: application/json` for API endpoints with bodies.
- Rate limiting rules are configurable via environment with default:
	- 100 req/min per token
	- 1000 req/min per IP global
- Auth middleware must parse `Authorization` header and set `ctx.user` with `sub`, `scope`, `roles`.

JSON Schema example for middleware error response
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"$id":"https://ohmf.example/schemas/error.response.json",
	"title":"ErrorResponse",
	"type":"object",
	"required":["type","title","status"],
	"properties":{
		"type":{"type":"string"},
		"title":{"type":"string"},
		"status":{"type":"integer"},
		"detail":{"type":"string"}
	}
}
```

Implementation constraints
- Middleware must be minimal and fast; avoid blocking I/O inside middleware (e.g., use in-memory counters or backed by Redis for distributed rate limits).
- Authentication should use the platform token introspection endpoint only when necessary; prefer JWT verification locally.

Security considerations
- All middleware that logs request bodies must redact PII and tokens.
- Enforce TLS for upstream connections in middleware that touches sensitive data.

Observability and operational notes
- Emit counters: `http.requests.total`, `http.requests.errors`, `http.requests.rate_limited`.
- Correlate logs with trace id and request id.

Testing requirements
- Test cases for token expiry, missing headers, rate limit boundaries, invalid JSON, and high concurrency for rate limiter.

References
- See internal/token for JWT verification details.
- See infra/docker for environment variable usage.