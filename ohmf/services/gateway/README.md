# 19 — Backend Services: API Gateway

Mapping: OHMF spec section 19 ("Backend Services: API Gateway")

Purpose
- Provide a single ingress for client traffic (HTTP + WebSocket), authentication, routing to internal services, rate limiting, observability headers, and protocol translation between client SDKs and internal protocol formats.

Expected behavior
- Validate auth and tokens for incoming requests.
- Route REST routes to internal services (gRPC/HTTP).
- Serve OpenAPI documentation and health endpoints.
- Upgrade connections to WebSocket for realtime and deliver bi-directional message frames.
- Emit and subscribe to platform events on the internal bus when messages arrive or are delivered.

Full specification details
- API surface:
	- REST control plane under /api/v1/*
	- Public static assets under /openapi/*
	- WebSocket endpoint: /realtime/v1?token=<jwt>
	- Health: GET /healthz (200 JSON)
- Invariants:
	- Every request must contain either a valid access token or a valid API key header: `X-API-Key`.
	- For producer paths, the gateway must ACK synchronous success OR return an error conforming to the JSON Problem spec.
	- WebSocket sessions are long-lived with heartbeats (ping/pong) every 30s.

Data shapes and protocol rules
- All JSON payloads must be UTF-8 and follow Draft 2020-12 JSON Schema supplied below.
- For cross-service transport, messages are converted to protocol envelope proto (see packages/protocol).

Implementation constraints
- Must be stateless; session affinity only for sticky WebSocket routing via shared session store or layer 7 routing.
- Configuration via environment and consul-style config: listen address, upstream addresses, JWT public keys, rate limits.
- gRPC upstreams must use TLS.

Security considerations
- Validate JWT `iss` and `aud`. Reject tokens with `exp` in past.
- Enforce per-token rate limits and global per-IP rate limits.
- Protect WebSocket handshake against CSRF: require `Origin` header check and token in query param or `Sec-WebSocket-Protocol`.
- Use strict Content Security Policy (CSP) for any served web UI.

Observability and operational notes
- Add request id header `X-Request-Id` if missing.
- Export traces (OpenTelemetry) and metrics (Prometheus): request duration, active websocket connections, auth failures, rate-limited requests.
- Health probes must verify upstream connectivity.

Testing requirements
- Unit tests for auth, routing, and error paths.
- Integration tests simulating:
	- HTTP request -> backend success/failure
	- WebSocket connect -> receive push
	- Token expiry and replay protection
- Load test for websocket concurrency and rate limiting.

API usage examples
- Health check
Request:
POST example omitted for brevity; use GET below.

HTTP request
GET /healthz HTTP/1.1
Host: gateway.example
Accept: application/json

HTTP response
HTTP/1.1 200 OK
Content-Type: application/json
Body:
{
	"status": "ok",
	"uptime_seconds": 12345
}

- Send message (REST)
HTTP request
POST /api/v1/messages HTTP/1.1
Host: api.example
Authorization: Bearer <access_token>
Content-Type: application/json
X-Request-Id: req-123

Body:
{
	"conversation_id": "conv_abc",
	"from": "+15551234567",
	"to": "+15557654321",
	"body": "Hello from gateway"
}

HTTP response
HTTP/1.1 202 Accepted
Content-Type: application/json
Body:
{
	"message_id": "msg_123",
	"status": "ingested"
}

- WebSocket connect
Client requests:
GET /realtime/v1?token=<jwt> HTTP/1.1
Host: api.example
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Version: 13

Example WebSocket message (JSON text frame)
Client -> Server:
{"type":"subscribe","topic":"conversation:conv_abc"}

Server -> Client:
{"type":"message","message_id":"msg_123","conversation_id":"conv_abc","from":"+15551234567","body":"Hello"}

JSON Schema (Draft 2020-12) — message ingestion request
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"$id":"https://ohmf.example/schemas/gateway.message.request.json",
	"title":"GatewayMessageRequest",
	"type":"object",
	"required":["conversation_id","from","to","body"],
	"properties":{
		"conversation_id":{"type":"string"},
		"from":{"type":"string","pattern":"^\\+?[0-9]{6,15}$"},
		"to":{"type":"string","pattern":"^\\+?[0-9]{6,15}$"},
		"body":{"type":"string","maxLength":4096},
		"metadata":{"type":"object","additionalProperties":true}
	},
	"additionalProperties":false
}
```

SQL snippet — ingestion audit table
```sql
CREATE TABLE gateway_message_ingest (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	message_id TEXT NOT NULL,
	conversation_id TEXT NOT NULL,
	actor TEXT NOT NULL,
	payload JSONB NOT NULL,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX ON gateway_message_ingest (conversation_id);
```

References
- See packages/protocol for envelope formats.
- See internal/realtime for WS specifics.
- See OHMF spec sections 7 (protocol), 10 (security), 20 (observability).