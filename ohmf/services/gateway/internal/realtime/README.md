# 19.3 — Gateway: Realtime (WebSocket) Service

Mapping: OHMF spec sections 19 (Gateway) and 14 (Realtime).

Purpose
- Provide bidirectional, low-latency channels for push notifications, typing indicators, read receipts, and conversation streaming. Maintain session lifecycle and presence.

Expected behavior
- Authenticate WS handshake (JWT in query param or subprotocol).
- Support JSON-framed messages with a fixed envelope schema.
- Heartbeat handshake: client must reply to server pings within 20s.
- Reconnect semantics: clients supply `last_cursor` to resume missed messages.

Message envelope (JSON Schema)
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"$id":"https://ohmf.example/schemas/realtime.frame.json",
	"title":"RealtimeFrame",
	"type":"object",
	"required":["type"],
	"properties":{
		"type":{"type":"string","enum":["subscribe","unsubscribe","message","receipt","presence","heartbeat","error"]},
		"topic":{"type":"string"},
		"payload":{"type":["object","null"]},
		"cursor":{"type":"string"}
	}
}
```

WebSocket message examples
- Subscribe
Client -> Server:
{"type":"subscribe","topic":"conversation:conv_abc"}

Server -> Client:
{"type":"message","cursor":"c_910","payload":{"message_id":"msg_123","conversation_id":"conv_abc","from":"+1555123","body":"Hello"}}

Protocol buffer snippet (optional push)
```proto
message RealtimePush {
	string cursor = 1;
	oneof payload {
		Envelope envelope = 2;
		Presence presence = 3;
	}
}
```

Implementation constraints
- Maximum single text frame payload 64KB.
- Support per-connection subscription limits (default 50 topics).
- Use a session map with persistence (Redis) to enable connection failover across gateway instances.

Security considerations
- Validate topic subscription authorization: only allow access to conversations where user is a participant.
- Protect from broadcast amplification by limiting subscription wildcard patterns.

Observability and operational notes
- Metrics: `ws.connections.active`, `ws.messages.sent`, `ws.messages.received`, `ws.pings.missed`.
- Log connect/disconnect reasons with `user_id`, `session_id`.

Testing requirements
- Simulate high churn of WS connections and message fanout.
- Test resume semantics with last_cursor across disconnects.
- Verify that unauthorized subscription attempts are rejected.

References
- internal/auth for token verification
- internal/bus for event publication/subscription semantics