# Security Controls

## Authentication and Authorization

- Protected HTTP routes use `services/gateway/internal/middleware.RequireAuth`.
- WebSocket upgrades validate the same access token classes in `services/gateway/internal/realtime/ws.go`.
- Protected relay, discovery, account, and messaging routes all depend on authenticated user context.

## OTP and Account Recovery Controls

- Phone verification start and verify flows are rate-limited by Redis-backed counters.
- Current OTP controls cover:
  - per-phone start limits
  - per-IP start limits
  - per-subnet start limits
  - per-challenge verify limits
  - per-IP verify limits
  - per-device verify limits
  - per-phone verify limits
- Recovery codes and 2FA methods are stored in dedicated tables and exposed only through authenticated account routes.

## Discovery Privacy and Abuse Controls

- Contact discovery requires authentication.
- Discovery requests are constrained by:
  - request body size limit
  - max contacts per request
  - per-user rate limit
  - per-IP rate limit
- Contact hashes are canonicalized before lookup.
- The gateway accepts versioned peppered discovery hashes:
  - `SHA256_PEPPERED_V1`
  - `SHA256_PEPPERED_V2`
- Pepper material is configuration-driven via the gateway config layer.

## Relay Integrity Controls

- Relay accept/result operations require account ownership of the job.
- Relay devices must satisfy capability checks before accepting or completing work.
- Carrier relay work additionally checks SMS-role state and recent device freshness.
- Acceptance can require a bound device signature using `relay_accept:v2`.
- Devices can mint a short-lived attestation challenge at `POST /v1/devices/{id}/attestation/challenge`.
- Attestation verification is completed at `POST /v1/devices/{id}/attestation/verify` using a verifier-signed verdict envelope.
- When relay attestation is required, relay authorization now checks persisted device attestation state plus expiry instead of trusting only a request-time flag.

## Message and Push Data Handling

- Message handlers validate request shape before persistence.
- Structured text payloads validate `spans`, `mentions`, and lifecycle fields before acceptance.
- Push subscriptions are encrypted before storage using the configured subscription key material.

## Transport Security

- Local development may use plaintext for convenience.
- Production traffic should terminate TLS at the edge and use HTTPS/WSS externally.
- Internal upstream traffic should also use TLS where supported by the deployment environment.

## Audit and Observability

- Domain events and user inbox events provide operational traceability for messaging flows.
- Account deletion now records explicit audit entries and grace-period state.
- Security-sensitive actions now append hash-chained audit entries, including pairing, export/download, deletion finalize, key backup operations, and device attestation challenge/verify events.
- Metrics are exported at `/metrics`.
- Request IDs and trace headers are attached for correlation.

## Configuration-Sensitive Controls

Important security-relevant environment variables include:

- `APP_DISCOVERY_PEPPER`
- `APP_DISCOVERY_MAX_CONTACTS`
- `APP_DISCOVERY_RATE_PER_USER`
- `APP_DISCOVERY_RATE_PER_IP`
- `APP_DISCOVERY_RATE_WINDOW_MINUTES`
- `APP_OTP_START_WINDOW_MINUTES`
- `APP_OTP_START_PER_PHONE_LIMIT`
- `APP_OTP_START_PER_IP_LIMIT`
- `APP_OTP_START_PER_SUBNET_LIMIT`
- `APP_OTP_VERIFY_WINDOW_MINUTES`
- `APP_OTP_VERIFY_PER_CHALLENGE_LIMIT`
- `APP_OTP_VERIFY_PER_IP_LIMIT`
- `APP_OTP_VERIFY_PER_DEVICE_LIMIT`
- `APP_OTP_VERIFY_PER_PHONE_LIMIT`
- `APP_REQUIRE_RELAY_ATTESTATION`
- `APP_DEVICE_ATTESTATION_SECRET`
- `APP_ATTESTATION_ANDROID_APP_ID`
- `APP_ATTESTATION_IOS_APP_ID`
- `APP_ATTESTATION_WEB_APP_ID`
- `APP_ATTESTATION_CHALLENGE_TTL_MINUTES`

## References

- `services/gateway/internal/auth/handler.go`
- `services/gateway/internal/discovery/handler.go`
- `services/gateway/internal/realtime/ws.go`
- `services/gateway/internal/relay/handler.go`
- `services/gateway/internal/messages/handler.go`
- `services/gateway/internal/config/config.go`
