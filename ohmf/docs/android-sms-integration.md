# 27 Android SMS/MMS Integration

This document expands Section 27 of the OHMF platform spec with implementation guidance for Android integration.

## Principles
- Carrier messaging state is device-authoritative: the Android device MUST be treated as the ground truth for SMS/MMS content and provider IDs.
- Server mirrors of carrier data are optional and MUST be clearly labeled and agreed by the user (policy-driven).
- Messaging logic should preserve provider identifiers (thread ids, provider message ids) when mirroring to allow reconcilation.

## Required Components
- Role manager adapter: determines whether the app holds the default SMS role and mediates consent flows.
- SMS receiver: listens to inbound SMS and invokes the normalization pipeline.
- MMS/WAP push receiver: handles MMS parts and metadata.
- Provider DAO: abstracts access to Android telephony provider APIs and local storage.
- SMS sender and MMS sender wrapper: executes local sends and surfaces local modem status.
- Relay executor: executes relay jobs requested by remote web clients.
- Carrier-thread normalizer: maps telephony provider threads to canonical conversation thread keys.

## Module Layout (recommended)

android-app/
  core/
    auth/           # auth + device registration
    api/            # remote API client + http clients
    db/             # local persistence (Room/SQL)
    realtime/       # websocket/rt components
    media/          # media handling + uploads
  messaging/
    conversations/  # thread mapping + renderer helpers
    composer/       # composer UI + send queue
    receipts/       # read/delivery receipts handling
  telephony/
    role/           # SMS role acquisition + checks
    sms/            # inbound SMS receiver + sender
    mms/            # MMS parsing + sender wrapper
    provider/       # telephony provider DAO
    relay/          # relay executor for web-originated sends

## SMS Import Pipeline
1. Telephony provider delivers SMS to receiver (via BroadcastReceiver).
2. Receiver pushes raw message to a normalization component.
3. Normalizer converts to `normalized_type` (e.g., `sms.inbound`), normalizes phone numbers to E.164, and extracts provider ids.
4. Normalized record is persisted locally in the local carrier store and may optionally be mirrored to the server according to user policy.

## Normalized SMS example

```json
{
  "normalized_type": "sms.inbound",
  "received_at": "2026-03-06T17:20:00Z",
  "from": "+15557654321",
  "subscription_id": 1,
  "body": "running late",
  "segments": 1,
  "thread_key": {"kind": "phone_number", "value": "+15557654321"},
  "provider_metadata": {"provider_message_id":"p12345","thread_id":"222"}
}
```

## Relay Execution (web-originated)
- Relay jobs MUST include an explicit user consent step on the phone when required by platform policies.
- Relay executor should expose accept/decline flows and provide ack/failure callbacks back to server.

## Security and Privacy
- Only mirror carrier content when explicitly enabled by the user.
- Telephony provider IDs are sensitive; access should be restricted and audited.
- Relay jobs must be authenticated and authorized; anti-automation and rate limits should apply.

## Testing and QA
- Unit test normalization logic with varied MMS/SMS payloads.
- End-to-end tests: emulate telephony provider deliver and verify local persistence and optional server mirror.
- Relay acceptance test: simulate web-originated relay and validate user approval flow and final modem result.

## Implementation notes
- Use `Room` or a lightweight SQL library for local persistence.
- Keep normalization deterministic and idempotent (use idempotency keys when mirroring).
- Preserve raw provider fields to support future reconciliation.
