# E2EE Testing - Docker PostgreSQL Setup & Execution Guide

**Status**: Tests ready to run with Docker PostgreSQL
**Docker Config**: PostgreSQL 15-alpine, credentials: dev:dev, port: 5432

---

## Quick Start (3 Steps)

### Step 1: Start Docker Compose Services

```bash
cd /c/Users/James/Downloads/Messages

# Start all services including PostgreSQL
docker-compose up -d

# Verify PostgreSQL is running
docker-compose ps

# Expected output should show: ohmf_postgres  ... Up
```

### Step 2: Create Test Database & Apply Migrations

```bash
cd ohmf/services/gateway

# Install migrate tool if not already installed
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Apply migrations (creates e2ee_sessions, device_key_trust tables)
migrate -path ./migrations -database "postgres://dev:dev@localhost:5432/messages_test?sslmode=disable" up
```

### Step 3: Run Tests

```bash
# From gateway directory
/c/Users/James/Downloads/Messages/ohmf/.tools/go/bin/go.exe test -v ./internal/e2ee -timeout 60s
```

---

## Detailed Setup Instructions

### Verify Docker Compose Configuration

Your `docker-compose.yml` has PostgreSQL already configured:

```yaml
postgres:
  image: postgres:15-alpine
  container_name: ohmf_postgres
  environment:
    - POSTGRES_USER=dev
    - POSTGRES_PASSWORD=dev
    - POSTGRES_DB=dev
  ports:
    - "5432:5432"
```

**Connection Details**:
- Host: localhost
- Port: 5432
- User: dev
- Password: dev
- Default DB: dev
- Connection string: `postgres://dev:dev@localhost:5432/dev?sslmode=disable`

### Start PowerShell in the Repository Root

```powershell
cd C:\Users\James\Downloads\Messages

# Verify docker-compose.yml exists
ls docker-compose.yml
```

### Start All Services

```bash
docker-compose up -d postgres

# Or start everything
docker-compose up -d

# Watch logs (leave running in separate terminal)
docker-compose logs -f postgres
```

### Verify PostgreSQL is Running

```bash
# Check container status
docker-compose ps

# Test connection
docker-compose exec postgres psql -U dev -d dev -c "SELECT version();"

# Should output PostgreSQL version
```

### Create Test Database

```bash
# Option 1: Via docker-compose exec
docker-compose exec postgres psql -U dev -d dev -c "CREATE DATABASE messages_test;"

# Option 2: Via direct connection (once migrations run, database auto-created)
# The test code will try to create it automatically
```

### Apply Database Migrations

```bash
cd ohmf/services/gateway

# Install migrate tool
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Apply migrations
migrate -path ./migrations -database "postgres://dev:dev@localhost:5432/messages_test?sslmode=disable" up

# Verify migrations applied
migrate -path ./migrations -database "postgres://dev:dev@localhost:5432/messages_test?sslmode=disable" version

# Expected: version should match your latest migration (e.g., 46)
```

### Check What Tables Were Created

```bash
docker-compose exec postgres psql -U dev -d messages_test -c "\dt"

# Should show tables like:
# - e2ee_sessions
# - device_key_trust
# - device_identity_keys
# - device_one_time_prekeys
# - etc.
```

---

## Running the Tests

### From the Gateway Directory

```bash
cd /c/Users/James/Downloads/Messages/ohmf/services/gateway

# Run all E2EE tests
/c/Users/James/Downloads/Messages/ohmf/.tools/go/bin/go.exe test -v ./internal/e2ee -timeout 60s

# Run specific test
/c/Users/James/Downloads/Messages/ohmf/.tools/go/bin/go.exe test -v ./internal/e2ee -run TestPostgresSessionStore_StoreAndLoadSession

# Run with coverage
/c/Users/James/Downloads/Messages/ohmf/.tools/go/bin/go.exe test -cover ./internal/e2ee

# Run benchmarks
/c/Users/James/Downloads/Messages/ohmf/.tools/go/bin/go.exe test -bench=. ./internal/e2ee -benchmem
```

---

## Test Categories

### Store Interface Tests (libsignal_stores_test.go)

Will run if database is available:

1. **TestPostgresSessionStore_StoreAndLoadSession**
   - Tests: Store → Load → Verify → Delete
   - Validates session round-trip

2. **TestPostgresSessionStore_DeleteAllSessions**
   - Tests: Multiple sessions → Delete all → Verify
   - Validates cascading deletion

3. **TestPostgresIdentityKeyStore_TrustModel**
   - Tests: TOFU trust acceptance
   - Validates identity key trust

4. Plus PreKey and SignedPreKey tests

### Integration Tests (e2ee_integration_tests.go)

Will run in short mode (no database needed):

1. **TestE2EEEncryptionFlow** - Full encryption/decryption
2. **TestMediaEncryption** - Media key wrapping
3. **TestMessageSignatureFlow** - Signature verification
4. **TestFingerprintComputation** - TOFU fingerprints
5. **TestRecipientKeyWrapping** - X25519 key wrapping
6. **TestDeviceRevocationScenario** - Session cleanup
7. **TestAccountDeletionScenario** - Cascade deletion
8. **TestSearchCompatibility** - Encrypted search
9. **TestE2EESessionFlow** - Full session setup
10. Performance & benchmark tests

---

## Troubleshooting

### "Docker PostgreSQL not responding"

```bash
# Check if container is running
docker-compose ps postgres

# If not running, start it
docker-compose up -d postgres

# Check logs for errors
docker-compose logs postgres

# Restart the container
docker-compose restart postgres
```

### "role 'dev' does not exist"

The PostgreSQL container might not have initialized properly:

```bash
# Stop and remove the container
docker-compose down postgres

# Remove the data volume to recreate
docker volume rm messages_postgres-data

# Restart
docker-compose up -d postgres

# Wait for healthcheck to pass
docker-compose ps  # Should show "healthy"
```

### "Cannot connect to database"

```bash
# Verify network connectivity
docker-compose exec postgres pg_isready -U dev

# Should output: accepting connections

# Or test from host
psql -h localhost -U dev -d dev -c "SELECT 1"
```

### Migration Fails

```bash
# Check migration files exist
ls ohmf/services/gateway/migrations/

# Verify migration syntax
grep -r "000045\|000046" ohmf/services/gateway/migrations/

# If tables already exist, you can clean them
docker-compose exec postgres psql -U dev -d messages_test -c "DROP DATABASE messages_test;"
docker-compose exec postgres psql -U dev -d dev -c "CREATE DATABASE messages_test;"

# Then reapply migrations
migrate -path ./migrations -database "postgres://dev:dev@localhost:5432/messages_test?sslmode=disable" up
```

---

## Expected Test Output

### With Docker Running & Database Ready

```
=== RUN   TestPostgresSessionStore_StoreAndLoadSession
--- PASS: TestPostgresSessionStore_StoreAndLoadSession (0.50s)

=== RUN   TestPostgresSessionStore_DeleteAllSessions
--- PASS: TestPostgresSessionStore_DeleteAllSessions (0.45s)

=== RUN   TestPostgresIdentityKeyStore_TrustModel
--- PASS: TestPostgresIdentityKeyStore_TrustModel (0.40s)

...

=== RUN   TestE2EEEncryptionFlow
--- PASS: TestE2EEEncryptionFlow (0.02s)

=== RUN   TestMediaEncryption
--- PASS: TestMediaEncryption (0.01s)

...

ok  	ohmf/services/gateway/internal/e2ee	5.234s
```

### With Docker Not Running

```
=== RUN   TestPostgresSessionStore_StoreAndLoadSession
    libsignal_stores_test.go:39: skipping: Docker PostgreSQL not responding - ensure 'docker-compose up' is running
--- SKIP: TestPostgresSessionStore_StoreAndLoadSession (0.16s)

...

=== RUN   TestE2EEEncryptionFlow
--- PASS: TestE2EEEncryptionFlow (0.02s)

...

ok  	ohmf/services/gateway/internal/e2ee	0.500s
```

The tests that depend on database will skip gracefully, while in-memory tests run normally.

---

## Full Test Suite Execution

To run everything:

```bash
cd /c/Users/James/Downloads/Messages

# 1. Start Docker
docker-compose up -d

# 2. Wait for PostgreSQL to be healthy
docker-compose ps  # Check Status column shows "healthy"

# 3. Create test database and apply migrations
cd ohmf/services/gateway
migrate -path ./migrations -database "postgres://dev:dev@localhost:5432/messages_test?sslmode=disable" up

#4. Run all tests
/c/Users/James/Downloads/Messages/ohmf/.tools/go/bin/go.exe test -v ./internal/e2ee -timeout 120s

# 5. Generate coverage report
/c/Users/James/Downloads/Messages/ohmf/.tools/go/bin/go.exe test -cover ./internal/e2ee -coverprofile=coverage.out

# 6. View coverage
go tool cover -html=coverage.out
```

---

## Summary

✅ **Tests compile** - Ready to use
✅ **Tests run** - In-memory tests pass immediately
✅ **Docker integration** - Setup for Docker PostgreSQL
✅ **Graceful degradation** - Tests skip if Docker not running
✅ **Full documentation** - All setup and troubleshooting covered

**Next**: Start Docker and run the tests!

