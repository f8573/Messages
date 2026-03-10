# OHMF User Quickstart: Sign Up by Phone and Send a Message

This guide walks through a real API flow:
1. Sign up/login with your phone number
2. Send a message to another registered user
3. Send a message to a non-registered phone number

## Prerequisites

- API is running at `http://localhost:18080`
- Docker stack is up

Note: the examples in this guide assume you're running the Docker development stack, which maps host port `18080` to the gateway container's `8080`. If you run the gateway binary directly (without Docker) it defaults to listening on `:8080` — adjust the examples accordingly.

```powershell
docker compose -f C:\Users\James\Downloads\Messages\ohmf\infra\docker\docker-compose.yml up -d --build
```

## Step 1: Start phone verification

```bash
curl -s -X POST http://localhost:18080/v1/auth/phone/start \
  -H "Content-Type: application/json" \
  -d '{
    "phone_e164": "+15551230001",
    "channel": "SMS"
  }'
```

Example response:

```json
{
  "challenge_id": "...",
  "expires_in_sec": 300,
  "retry_after_sec": 30,
  "otp_strategy": "SMS"
}
```

Save `challenge_id`.

## Step 2: Verify OTP and receive tokens

Use OTP `123456` in local/dev.

```bash
curl -s -X POST http://localhost:18080/v1/auth/phone/verify \
  -H "Content-Type: application/json" \
  -d '{
    "challenge_id": "<CHALLENGE_ID>",
    "otp_code": "123456",
    "device": {
      "platform": "WEB",
      "device_name": "Chrome"
    }
  }'
```

Example response:

```json
{
  "user": {
    "user_id": "...",
    "primary_phone_e164": "+15551230001"
  },
  "device": {
    "device_id": "...",
    "platform": "WEB"
  },
  "tokens": {
    "access_token": "...",
    "refresh_token": "..."
  }
}
```

Save:
- `access_token`
- `user.user_id`

## Step 3A: Send a message to another registered user

You need the other user's `user_id` (they must complete Steps 1-2 with their own phone).

### Create DM conversation

```bash
curl -s -X POST http://localhost:18080/v1/conversations \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "DM",
    "participants": ["<OTHER_USER_ID>"]
  }'
```

Save `conversation_id`.

### Send message

```bash
curl -s -X POST http://localhost:18080/v1/messages \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "conversation_id": "<CONVERSATION_ID>",
    "idempotency_key": "msg-001",
    "content_type": "text",
    "content": {
      "text": "Hey there"
    }
  }'
```

### Read conversation messages

```bash
curl -s -X GET "http://localhost:18080/v1/conversations/<CONVERSATION_ID>/messages" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

## Step 3B: Send a message to a non-registered phone number

Use this when the recipient is not an OHMF account yet.

```bash
curl -s -X POST http://localhost:18080/v1/messages/phone \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "phone_e164": "+15551239999",
    "idempotency_key": "msg-phone-001",
    "content_type": "text",
    "content": {
      "text": "Hello from OHMF"
    }
  }'
```

Example response:

```json
{
  "message_id": "...",
  "conversation_id": "...",
  "server_order": 1,
  "transport": "SMS"
}
```

You can then fetch that conversation:

```bash
curl -s -X GET "http://localhost:18080/v1/conversations/<CONVERSATION_ID>/messages" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

## Token refresh/logout

### Refresh

```bash
curl -s -X POST http://localhost:18080/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<REFRESH_TOKEN>"}'
```

### Logout

```bash
curl -s -X POST http://localhost:18080/v1/auth/logout \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"device_id":"<DEVICE_ID>"}'
```
