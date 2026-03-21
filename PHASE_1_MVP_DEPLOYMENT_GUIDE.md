# Phase 1 MVP Deployment Guide - Current Gateway State

**Date:** 2026-03-20  
**Status:** Phase 1 delivered; follow-on parity slices are also merged in the current repository state

---

## Summary

Phase 1 is complete in the gateway, and the current branch includes the immediate follow-on parity work that shipped after it. The repository now contains:

- Phase 1 slice:
  - Typing indicators
  - Read receipts
  - Message effects and effect policy
  - Push token storage plus FCM/APNs/WebPush dispatch
  - Recovery codes and 2FA flows
- Follow-on parity slice already merged on top:
  - Message pinning and forwarding
  - Conversation descriptions, roles, invites, bans, theme, retention, expiration, settings versioning
  - Structured rich text and mentions
  - Message lifecycle fields: `expires_at`, `expires_on_read`, retention-based expiry
  - Realtime resume via `last_user_cursor` and `last_cursor`
  - Discovery and OTP abuse controls
  - Richer account export and deletion audit/grace-period state
  - Relay hardening for capability-aware SMS/MMS execution

Out of scope and still open:

- Media processing depth
- Voice/video calling
- Threads/hierarchy
- Full platform attestation and secure pairing

---

## Required Migrations

For the current gateway behavior, ensure the baseline gateway migrations plus these later parity migrations are applied:

- `000030_read_receipts_enhancement`
- `000031_message_effects`
- `000032_account_recovery`
- `000033_device_push_tokens`
- `000034_message_pins_and_descriptions`
- `000035_conversation_controls`
- `000036_message_lifecycle`
- `000038_account_data_management`

If you are deploying from an older environment, verify both `up` and `down` files are present for rollback-sensitive operations.

---

## Configuration Checklist

### Push Providers

- `APP_FIREBASE_PROJECT_ID`
- `APP_FIREBASE_CREDENTIALS_PATH`
- `APP_APNS_CERT_PATH`
- `APP_APNS_KEY_PATH`
- `APP_APNS_BUNDLE_ID`
- `APP_APNS_TEAM_ID`
- `APP_APNS_KEY_ID`
- `APP_PUSH_SUBSCRIPTION_KEY`

### Discovery and OTP Abuse Controls

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

### Existing Core Gateway Configuration

- Database/Redis/Kafka settings
- JWT secret / auth configuration
- Cassandra settings if optional timeline reads are enabled
- `APP_CLAIM_ANDROID_CARRIER` if Android relay devices should advertise carrier capability

---

## API Surface Added Since Original Phase 1 Draft

The current branch includes additional protected endpoints beyond the original Phase 1 slice:

- Account:
  - `POST /v1/auth/recovery-code`
  - `GET /v1/account/recovery-codes`
  - `POST /v1/account/2fa/setup`
  - `POST /v1/account/2fa/verify`
  - `GET /v1/account/2fa/methods`
  - `POST /v1/account/export`
  - `POST /v1/account/delete`
- Conversations:
  - `PATCH /v1/conversations/{id}/settings`
  - `PUT /v1/conversations/{id}/members/{userID}/role`
  - `POST /v1/conversations/{id}/invites`
  - `GET /v1/conversations/{id}/invites`
  - `POST /v1/conversations/invites/redeem`
  - `POST /v1/conversations/{id}/bans`
  - `DELETE /v1/conversations/{id}/bans/{userID}`
  - `GET /v1/conversations/{id}/search`
  - `GET /v1/conversations/{id}/read-status`
- Messages:
  - `GET /v1/messages/{id}/reactions`
  - `POST|DELETE /v1/messages/{id}/pin`
  - `POST /v1/messages/{id}/forward`
  - `POST /v1/messages/{id}/effects`
  - `GET /v1/messages/{id}/edits`
- Relay:
  - `POST /v1/relay/messages`
  - `GET /v1/relay/jobs/{id}`
  - `GET /v1/relay/jobs/available`
  - `POST /v1/relay/jobs/{id}/accept`
  - `POST /v1/relay/jobs/{id}/result`
- Realtime:
  - `GET /v1/ws`
  - `GET /v2/ws`

---

## Verification

Run the gateway suite after migration and configuration changes:

```powershell
cd C:\Users\James\Downloads\Messages\ohmf\services\gateway
C:\Users\James\Downloads\Messages\ohmf\.tools\go\bin\go.exe test ./...
```

Expected result on the current branch: all gateway packages pass.

---

## Deployment Notes

- Message expiration and conversation retention are now part of the runtime contract. Old clients that only send plain `content.text` continue to work, but newer clients may send `spans`, `mentions`, `expires_on_read`, `expires_in_seconds`, and `expires_at`.
- Relay acceptance is stricter than the original draft. Carrier jobs now depend on device capability, SMS-role state, freshness, and optional signature headers.
- Account deletion is now a grace-period operation instead of immediate irreversible row removal. Operational purge tooling should respect the `users.deletion_*` fields and `account_deletion_audit`.
- The OpenAPI and protocol docs should be treated as the public reference for the current route shapes.
