# Mini-App Bridge Contract

The web and Android hosts use the same bridge envelope. Mini-apps should treat this file as the canonical runtime contract.

## Request Envelope

```json
{
  "bridge_version": "1.0",
  "channel": "chan_123",
  "request_id": "req_123",
  "method": "host.getLaunchContext",
  "params": {}
}
```

## Response Envelope

```json
{
  "bridge_version": "1.0",
  "channel": "chan_123",
  "request_id": "req_123",
  "ok": true,
  "result": {}
}
```

## Host Event Envelope

```json
{
  "bridge_version": "1.0",
  "channel": "chan_123",
  "bridge_event": "session.stateUpdated",
  "payload": {}
}
```

## Launch Context

```json
{
  "bridge_version": "1.0",
  "app_id": "app.ohmf.eightball",
  "app_version": "1.0.0",
  "app_session_id": "aps_123",
  "conversation_id": "conv_123",
  "viewer": {
    "user_id": "usr_123",
    "role": "PLAYER",
    "display_name": "Avery"
  },
  "participants": [],
  "capabilities_granted": [
    "conversation.read_context",
    "storage.session"
  ],
  "host_capabilities": [
    "conversation.read_context",
    "conversation.send_message",
    "participants.read_basic",
    "storage.session",
    "storage.shared_conversation",
    "realtime.session",
    "media.pick_user",
    "notifications.in_app"
  ],
  "state_snapshot": {},
  "state_version": 1,
  "joinable": true
}
```

## Built-In Methods

- `host.getLaunchContext`
- `conversation.readContext`
- `conversation.sendMessage`
- `participants.readBasic`
- `storage.session.get`
- `storage.session.set`
- `storage.sharedConversation.get`
- `storage.sharedConversation.set`
- `session.updateState`
- `media.pickUser`
- `notifications.inApp.show`

## Runtime Rules

- Hosts must validate the bridge `channel` and mini-app origin before honoring calls.
- Permission checks are per session and may be narrower than the manifest declaration.
- Normal-user production apps should be loaded from registry-issued origins only.
- Developer-mode raw URLs are for explicit local development only.
- `bridge_version` is the runtime compatibility anchor for host and app communication.
