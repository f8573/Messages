# E2EE Database Migrations

This directory contains SQL migration files for initializing the E2EE testing database.

## Overview

PostgreSQL automatically executes SQL files in this directory (`/docker-entrypoint-initdb.d`) **only on first startup**.

**File naming convention**: `NNN_description.sql` (alphabetically ordered)

## Migrations

### 001_e2ee_schema.sql

**Tables created**:
- `device_identity_keys` - Long-lived X25519 keypairs (annually rotated)
- `device_signed_prekeys` - Medium-lived X25519 keypairs (~4 week rotation)
- `device_one_time_prekeys` - Ephemeral keypairs (consumed after use)
- `sessions` - Double Ratchet state for 1-to-1 conversations

**Indexes created**:
- User ID lookups (find all keys for a user)
- Session lookups (find session by contact)
- Unused one-time prekey discovery

**Features**:
- Automatic timestamps (created_at, updated_at)
- Foreign key constraints (user_id references)
- Permissions for test user (e2ee_test)

## How It Works

### First Startup (Fresh Docker)

```bash
docker-compose -f docker-compose.e2ee-test.yml up -d
```

1. PostgreSQL 15 Alpine image pulls
2. Container starts
3. Initialization runs (if data volume is empty):
   - Creates database `e2ee_test`
   - Executes 001_e2ee_schema.sql
   - Creates all tables and indexes
   - Sets permissions
4. PostgreSQL becomes ready (healthcheck passes)5. Volume persists for next restart

### Subsequent Starts

```bash
docker-compose -f docker-compose.e2ee-test.yml up -d
```

1. Data volume already exists
2. Migrations **NOT re-executed** (PostgreSQL skips init on existing volumes)
3. Tables remain from previous run
4. Data persists between restarts

### Cleanup (Full Reset)

```bash
docker-compose -f docker-compose.e2ee-test.yml down -v
# -v removes all volumes (data deleted)

docker-compose -f docker-compose.e2ee-test.yml up -d
# Next startup re-initializes from scratch
```

## Database Connection

**From Docker host**:
```bash
psql -h localhost -U e2ee_test -d e2ee_test
# Enter password: test_password_e2ee
```

**From Go tests**:
```bash
export TEST_DATABASE_URL="postgres://e2ee_test:test_password_e2ee@localhost:5432/e2ee_test"
go test -v -tags integration ./internal/e2ee -run E2EE
```

**From Docker container**:
```bash
docker exec -it e2ee-test-db psql -U e2ee_test -d e2ee_test
```

## Troubleshooting

### Database won't initialize

**Problem**: Start up fails, no tables created

**Causes**:
1. ❌ migrations directory doesn't exist
2. ❌ Permission error on SQL files
3. ❌ Port 5432 already in use

**Solutions**:
```bash
# Ensure migrations directory exists
mkdir -p internal/e2ee/migrations

# Ensure SQL files are readable
chmod 644 internal/e2ee/migrations/*.sql

# Check if port 5432 in use
lsof -i :5432

# View PostgreSQL initialization logs
docker logs e2ee-test-db
```

### Health check fails

**Problem**: Container running but `pg_isready` returns error

**Cause**: PostgreSQL needs 30+ seconds to initialize on first run

**Solution**:
```bash
# Wait longer for startup
sleep 30

# Verify manually
docker exec e2ee-test-db pg_isready -U e2ee_test -d e2ee_test
```

### Connection refused

**Problem**: `ECONNREFUSED` when running tests

**Causes**:
1. ❌ Container stopped
2. ❌ Port not exposed (5432)
3. ❌ Wrong host (`localhost` vs `127.0.0.1`)

**Solutions**:
```bash
# Verify container is running
docker-compose -f docker-compose.e2ee-test.yml ps

# Check port mapping
docker port e2ee-test-db

# Verify connectivity
nc -zv localhost 5432
```

### Data not persisting

**Problem**: Data lost after container restart

**Cause**: Named volume might not be mounted correctly

**Solution**:
```bash
# Verify volume mount
docker inspect e2ee-test-db | grep -A 5 Mounts

# If missing, stop and restart with volume
docker-compose -f docker-compose.e2ee-test.yml down
docker-compose -f docker-compose.e2ee-test.yml up -d
```

## Adding New Migrations

To add schema changes:

1. Create new file: `002_add_new_table.sql`
2. Add SQL statements
3. **Important**: Only works if data volume is deleted first!

```sql
-- migrations/002_add_new_table.sql
CREATE TABLE IF NOT EXISTS new_table (
  id UUID PRIMARY KEY,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

To apply:
```bash
# Delete old volume (data loss!)
docker-compose -f docker-compose.e2ee-test.yml down -v

# Re-initialize with new schema
docker-compose -f docker-compose.e2ee-test.yml up -d
```

**Recommendation**: For persistent databases, use migration tools like Flyway, Liquibase, or Go migrate instead of Docker init scripts.

## Reference

**Docker execution order**:
```
1. Container starts
2. PostgreSQL initializes (if first run)
3. /docker-entrypoint-initdb.d/*.sql runs (alphabetical)
4. PostgreSQL becomes ready
5. Healthcheck passes
```

**PostgreSQL documentation**: https://hub.docker.com/_/postgres/
- "How to extend this image": Environment and volume setup
- "Environment Variables": POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB
- "Initialization scripts": Custom SQL files

**E2EE database schema**: See E2EE_COMPLETE_DOCUMENTATION.md
