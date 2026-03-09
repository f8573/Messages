# 19.5 — Gateway: Database Access Layer

Mapping: OHMF spec section 19 (Gateway) and 7.2 (SQL schemas).

Purpose
- Provide transactional persistence for gateway-managed tables: ingest audit, session maps, tokens blacklist, rate-limiter counters (where persisted).

Expected behavior
- Expose minimal, well-typed repository functions for other gateway subsystems (messages, sessions, tokens).
- Use migrations located in `migrations/` for schema evolution.

Primary SQL schema example
```sql
-- message_ingest_audit
CREATE TABLE gateway.message_ingest_audit (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	message_id TEXT NOT NULL UNIQUE,
	conversation_id TEXT NOT NULL,
	actor TEXT,
	payload JSONB,
	status TEXT NOT NULL,
	created_at TIMESTAMPTZ DEFAULT now()
);

-- session table for ephemeral session mapping (may be stored in Redis instead)
CREATE TABLE gateway.sessions (
	session_id TEXT PRIMARY KEY,
	user_id UUID NOT NULL,
	connection_info JSONB,
	expires_at TIMESTAMPTZ
);
```

Implementation constraints
- Use a DB connection pool and prepared statements.
- Prefer `jsonb` for flexible payload storage; ensure indexed fields for lookup.

Security considerations
- Use DB role with least privilege; avoid storing raw tokens.
- Encrypt columns where required by privacy rules.

Observability and operational notes
- Metrics for DB pool: active_connections, idle_connections, query_histogram.
- Alert on long-running (>2s) gateway DB queries.

Testing requirements
- Migration tests verifying up/down scripts.
- Repository unit tests using ephemeral test DB.

References
- infra/docker for local DB dev config.
- gateway internal messages for audit usage.