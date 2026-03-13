# Security Controls

Authentication and authorization
- Protected HTTP routes use `services/gateway/internal/middleware.RequireAuth`.
- WebSocket upgrades validate the same access token types in `services/gateway/internal/realtime/ws.go`.
- Admin-only carrier link listing checks the `ADMIN` profile from request context.

Input validation
- Message ingress and WebSocket payloads are validated with JSON Schema before handler execution.
- Mini-app manifests are validated for required fields and signature shape before persistence.

Secrets and key material
- Local development uses environment variables.
- Production deployments should provide JWT secrets and mini-app public keys through a secret manager or platform secret store instead of checked-in values.
- The recommended integration point is the environment consumed by `services/gateway/internal/config/config.go`.

Transport security
- Local compose uses plaintext for developer convenience.
- Production service-to-service and external traffic should terminate TLS at the edge and use TLS for internal upstream calls.
- WebSocket authentication relies on bearer tokens and should only be exposed behind HTTPS/WSS.

Audit and observability
- Message creation emits structured events in the gateway.
- Metrics are exported at `/metrics`.
- Request IDs and `Traceparent` headers are attached for request correlation.

References
- `services/gateway/internal/middleware/auth.go`
- `services/gateway/internal/middleware/validation.go`
- `services/gateway/internal/realtime/ws.go`
- `services/gateway/internal/miniapp/service.go`
- `services/gateway/internal/messages/handler.go`
