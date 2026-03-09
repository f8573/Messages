# 7.2 — Protocol: SQL Mappings & Canonical Tables

Mapping: OHMF spec section 7.2 (SQL Schema for protocol persistence)

Purpose
- Provide canonical SQL DDL for persisted protocol entities (messages, timelines, cursors).

SQL schema examples
- canonical messages
```sql
CREATE TABLE protocol.messages (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	message_id TEXT UNIQUE NOT NULL,
	conversation_id TEXT NOT NULL,
	from_number TEXT,
	to_number TEXT,
	body JSONB,
	metadata JSONB,
	created_at TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX ON protocol.messages (conversation_id);
```

- timeline cursor
```sql
CREATE TABLE protocol.cursors (
	cursor_id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL,
	position BIGINT NOT NULL,
	updated_at TIMESTAMPTZ DEFAULT now()
);
```

Implementation constraints
- Use migration tooling (sqlc/atlas/liquibase) and keep DDL idempotent.

Security considerations
- Role separation and column-level encryption where required.

Observability and operational notes
- Track insert rates and table growth.

Testing requirements
- Migration test harness, schema linting.

References
- packages/protocol proto files and JSON Schema.
