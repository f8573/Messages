# 19.7 — Gateway: Conversation Access & Routing

Mapping: OHMF spec sections 5 (Conversations) and 19 (Gateway).

Purpose
- Gate access to conversation-level operations (list, read, send), perform conversation membership checks, and apply per-conversation policy (e.g., retention flags).

Expected behavior
- Validate conversation existence and membership before allowing read or publish operations.
- Map conversation identifiers between external and internal representations.

Data shape: conversation access response (JSON Schema)
```json
{
	"$schema":"https://json-schema.org/draft/2020-12/schema",
	"title":"ConversationAccess",
	"type":"object",
	"required":["conversation_id","members"],
	"properties":{
		"conversation_id":{"type":"string"},
		"members":{"type":"array","items":{"type":"string"}}
	}
}
```

SQL snippet (conversation membership)
```sql
CREATE TABLE conversations.membership (
	conversation_id TEXT NOT NULL,
	user_id UUID NOT NULL,
	role TEXT NOT NULL,
	created_at TIMESTAMPTZ DEFAULT now(),
	PRIMARY KEY (conversation_id, user_id)
);
```

Implementation constraints
- Read heavy: use cache for membership checks (TTL 30s).
- Keep authorization checks centralized.

Security considerations
- Avoid leaking membership lists to unauthorized callers.
- Respect conversation-level retention controls on reads.

Observability and operational notes
- Metrics: `conversations.access.allowed`, `conversations.access.denied`.

Testing requirements
- Tests verifying membership checks, pagination, and policy enforcement.

References
- packages/protocol for conversation identifiers.
- internal/users for mapping user ids.