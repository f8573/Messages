# API MVP Notes

Canonical contract is `packages/protocol/openapi/openapi.yaml`.

Conventions:
- Base path: `/v1`
- Auth: `Authorization: Bearer <access_token>`
- Errors: `ErrorEnvelope`
- Cursor pagination for list endpoints
- `idempotency_key` required on `POST /v1/messages`
