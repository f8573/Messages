# 17.1 — Docker: Local Development Compose

Mapping: OHMF spec section 17 (Infrastructure) and infra/docker README context.

Purpose
- Provide Docker Compose definitions and instructions to run a local development environment matching platform dependencies for rapid iteration.

Expected behavior
- `docker-compose.yml` should start:
	- Postgres (with preloaded migrations)
	- Redis
	- Event bus (e.g., Kafka or NATS)
	- Gateway service (local build)
	- Mock auth service
	- Mock users service

Usage example
- Start compose:
```powershell
docker-compose -f infra/docker/docker-compose.yml up --build
```

Environment variables
- Postgres credentials: `PGUSER`, `PGPASSWORD`, `PGDATABASE`, `PGHOST`
- Redis URL: `REDIS_URL`
- JWT JWKS URL: `JWKS_URL`

Example docker-compose snippets (illustrative)
```yaml
version: '3.8'
services:
	postgres:
		image: postgres:15
		environment:
			POSTGRES_USER: dev
			POSTGRES_PASSWORD: dev
			POSTGRES_DB: ohmf_dev
		ports: ["5432:5432"]
	redis:
		image: redis:7
		ports: ["6379:6379"]
	gateway:
		build: ../../services/gateway
		environment:
			- DATABASE_URL=postgres://dev:dev@postgres:5432/ohmf_dev
			- REDIS_URL=redis://redis:6379
		ports: ["8080:8080"]
```

Implementation constraints
- Compose for dev only; production uses orchestration with stronger security.

Security considerations
- Do not use real credentials in compose. Use ephemeral or dev-only secrets.

Observability and operational notes
- Expose ports and set health-checks for each service.

Testing requirements
- Health check script to verify service endpoints after compose up.

References
- infra/README.md and service-specific README files.