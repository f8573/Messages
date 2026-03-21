# 28 Linked-Device Relay

This document expands Section 28 of the OHMF platform spec and reflects the relay behavior currently implemented in the gateway.

## Purpose

Linked-device relay allows a web or non-Android client to request an SMS/MMS send that is executed by a trusted Android device associated with the same account.

## Security and Consent

- Relay requests must be authenticated and authorized by the originating user.
- Relay jobs use short TTLs and are rejected once expired.
- Relay acceptance can require device signature headers:
  - `X-Device-Signature`
  - `X-Device-Timestamp`
- The gateway verifies a stronger `relay_accept:v2` signature payload when signature headers are present.
- Legacy acceptance verification is only allowed when strict attestation is not required.
- Carrier-class jobs require stronger device checks:
  - device capability
  - SMS-role state
  - recent device freshness (`last_seen_at`)

## Relay Job Schema

A relay job describes the destination, transport policy, content, and execution requirements:

- `id` — unique server-assigned id
- `creator_user_id`
- `executing_device_id`
- `destination`
- `transport_hint`
- `required_capability`
- `content`
- `status`
- `consent_state`
- `expires_at`
- `accepted_at`
- `attested_at`
- `result`

## Canonical Transport Policy

The server now canonicalizes relay policy instead of trusting the raw client hint:

- Text-only content maps to `RELAY_SMS`
- Content with attachments/media maps to `RELAY_MMS`
- The resulting relay job also records the required device capability:
  - `RELAY_EXECUTOR`
  - `ANDROID_CARRIER`

This means a client can send `transport_hint: "AUTO"` or even a mismatched hint, and the server will normalize the job to the transport actually required by the content.

## Server Responsibilities

- Queue relay jobs and select eligible devices based on capability and account ownership.
- Reject expired jobs on `GET`, `accept`, and `result`.
- Enforce capability-aware authorization before a device accepts or completes a job.
- Record result payloads and final status transitions.

## Device Responsibilities

- Authenticate relay polling/accept/result calls with the account token.
- Validate relay assignment metadata.
- Provide a valid device signature when required.
- Ensure the device actually holds the necessary execution capability.
- For carrier jobs, hold the default SMS role and remain recently active.

## Failure Handling

- Expired jobs return `410 Gone`.
- Unauthorized device/job combinations return `403 Forbidden`.
- Missing or incomplete attestation/signature material returns `401 Unauthorized` when required.
- Jobs stay queued until accepted, expired, or completed according to current server policy.

## API Endpoints

- `POST /v1/relay/messages` — create relay job
- `GET /v1/relay/jobs/{id}` — get a relay job owned by the authenticated user
- `GET /v1/relay/jobs/available` — list queued jobs available to the authenticated account
- `POST /v1/relay/jobs/{id}/accept` — device accepts relay job
- `POST /v1/relay/jobs/{id}/result` — device posts final result

## Acceptance Signature Payload

When present, the current preferred signed payload is:

```text
relay_accept:v2:{job_id}:{device_id}:{timestamp}:{transport_hint}:{required_capability}:{consent_state}:{status}
```

That payload binds the acceptance decision to the relay policy fields that matter for execution.

## Testing Focus

- Canonical SMS/MMS fallback
- Expired-job rejection
- Device capability rejection
- SMS-role enforcement for carrier jobs
- Acceptance signature verification

## References

- `services/gateway/internal/relay/handler.go`
- `services/gateway/internal/relay/service_test.go`
