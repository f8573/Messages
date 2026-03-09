# 19.15 — Gateway: HTTP Client Utilities

Mapping: OHMF spec section 19 (Gateway) and 21 (Inter-service comms).

Purpose
- Provide standardized, instrumented HTTP client wrappers for calling internal services (retries, timeouts, tracing propagation, circuit breaking).

Expected behavior
- All downstream calls must carry `X-Request-Id` and `traceparent`.
- Retry idempotent requests with exponential backoff.

Implementation constraints
- Configurable timeouts per service.
- Use circular buffer logging for slow endpoints.

Security considerations
- Do not forward client tokens to unrelated upstreams; strip sensitive headers.

Observability and operational notes
- Trace outbound calls and emit `http_outbound.duration_seconds`.

Testing requirements
- Test failure injection (timeouts) and retry behavior.

References
- internal/openapi for downstream endpoint definitions.