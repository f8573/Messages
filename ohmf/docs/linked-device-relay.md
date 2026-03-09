# 28 Linked-Device Relay

This document expands Section 28 of the OHMF platform spec and provides implementation guidance for linked-device relay.

## Purpose
Linked-device relay allows a web or non-Android client to request an SMS/MMS send that is executed by a trusted Android device associated with the same account.

## Security and consent
- Relay requests MUST be authenticated and authorized by the originating user.
- Android devices MUST present a user consent/accept flow when required by platform policy.
- Relay jobs must include a short TTL and idempotency key to reduce replay and duplicate sends.

## Relay Job Schema (high-level)
A relay job describes the destination, transport hint, content, and metadata:

- `relay_job_id` — unique server-assigned id
- `requested_by` — `{user_id, device_id}` of the requester
- `executing_device_id` — the Android device chosen to execute the job
- `transport` — `RELAY_SMS` or `RELAY_MMS`
- `destination` — e.g., `{phone_e164: "+1555..."}`
- `content` — typed payload (e.g., text)
- `status` — QUEUED_ON_SERVER, DISPATCHED_TO_ANDROID, ACCEPTED_BY_DEVICE, SENT_TO_MODEM, FINAL, DEVICE_OFFLINE, ROLE_NOT_HELD
- `created_at`, `updated_at` timestamps

## Server responsibilities
- Queue relay jobs and select eligible Android devices based on capabilities and online state.
- Provide job status updates and allow clients to poll or subscribe to job updates.
- Apply rate limits and abuse mitigations for relay endpoints.

## Android device responsibilities
- Authenticate incoming relay assignment from server and validate request signature/metadata.
- Present accept/decline UI when required and surface relevant metadata to the user.
- Execute the send via telephony APIs and report modem-level results back to server.
- Honor `idempotency_key` to avoid duplicate sends.

## Failure handling and retries
- If the device is offline, the job should remain queued and a retry policy should be applied.
- If the device does not hold the SMS role, return `ROLE_NOT_HELD` and surface guidance to user.
- Limit automatic retries to avoid spam or carrier throttling; require user interaction for repeated attempts.

## API endpoints (examples)
- `POST /v1/relay/messages` — create relay job
- `GET /v1/relay/jobs/{relay_job_id}` — get job status
- `POST /v1/relay/jobs/{relay_job_id}/accept` — device accepts job
- `POST /v1/relay/jobs/{relay_job_id}/result` — device posts final result

## Telemetry and auditing
- Log relay job lifecycle events with minimal PII.
- Emit metrics: relay job queue depth, device acceptance latency, send success rate.

## Testing
- Emulate device online/offline behavior in integration tests.
- Validate idempotency by replaying the same job with the same `idempotency_key`.
- Test role-not-held and user-decline flows.
