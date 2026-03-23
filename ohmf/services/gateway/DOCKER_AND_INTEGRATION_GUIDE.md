# Phase 3.5 & 3.6: Docker Testing & Integration Tests Setup

## Docker Testing (Phase 3.5c)

### Quick Start

```bash
# From gateway directory
cd c:/Users/James/Downloads/Messages/ohmf/services/gateway

# Navigate to project root if needed
cd ../../../..

# Build and start all services
docker-compose -f ohmf/services/gateway/docker-compose.test.yml up -d

# Watch logs
docker-compose -f ohmf/services/gateway/docker-compose.test.yml logs -f gateway

# Run database migrations (if needed)
docker-compose -f ohmf/services/gateway/docker-compose.test.yml exec gateway \
  /bin/api migrate up

# Stop all services
docker-compose -f ohmf/services/gateway/docker-compose.test.yml down
```

### Services Started

1. **PostgreSQL 14** (port 5432)
   - User: `ohmf`
   - Password: `ohmf`
   - Database: `ohmf`
   - Health check: 10x5s retries

2. **Redis 7** (port 6379)
   - Available for caching/sessions
   - Health check: 10x5s retries

3. **Gateway API** (port 8080)
   - Built from multi-stage Dockerfile
   - Depends on both PostgreSQL and Redis
   - Logs available via docker-compose logs

### Manual API Testing

Once container is running:

```bash
# List device keys (requires auth token)
curl -X GET http://localhost:8080/v1/e2ee/keys \
  -H "Authorization: Bearer <AUTH_TOKEN>" \
  -H "Content-Type: application/json"

# Claim one-time prekey
curl -X POST http://localhost:8080/v1/e2ee/claim-prekey \
  -H "Authorization: Bearer <AUTH_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "contact_user_id": "550e8400-e29b-41d4-a716-446655440000",
    "contact_device_id": "device-uuid"
  }'

# Get device key bundle
curl -X GET http://localhost:8080/v1/e2ee/bundle/<user_id>/<device_id> \
  -H "Authorization: Bearer <AUTH_TOKEN>" \
  -H "Content-Type: application/json"

# Verify device fingerprint
curl -X POST http://localhost:8080/v1/e2ee/verify \
  -H "Authorization: Bearer <AUTH_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "contact_user_id": "550e8400-e29b-41d4-a716-446655440000",
    "contact_device_id": "device-uuid",
    "fingerprint": "abc123def456..."
  }'

# Get trust state
curl -X GET http://localhost:8080/v1/e2ee/trust/<contact_user_id> \
  -H "Authorization: Bearer <AUTH_TOKEN>" \
  -H "Content-Type: application/json"
```

### Troubleshooting

**Container won't start**
```bash
# Check logs
docker-compose -f docker-compose.test.yml logs gateway
docker-compose -f docker-compose.test.yml logs postgres

# Check health status
docker-compose -f docker-compose.test.yml ps
```

**Database connection refused**
```bash
# Wait for PostgreSQL to be ready
docker-compose -f docker-compose.test.yml logs postgres
# Should see: "ready to accept connections"

# Test connection directly
docker-compose -f docker-compose.test.yml exec postgres \
  psql -U ohmf -d ohmf -c "SELECT 1"
```

**Migrations not applied**
```bash
# Check current migration status
docker-compose -f docker-compose.test.yml exec gateway \
  /bin/api migrate status

# Manually run migrations
docker-compose -f docker-compose.test.yml exec gateway \
  /bin/api migrate up

# View migration files
ls migrations/
```

**Port already in use**
```bash
# Change port mappings in docker-compose.test.yml
# Or kill existing containers
docker-compose -f docker-compose.test.yml down --remove-orphans
lsof -i :8080  # Find process on port 8080
```

---

## Integration Tests (Phase 3.6)

### Setup

Create `integration_test.go` in `internal/e2ee/`:

```go
//go:build integration
// +build integration

package e2ee

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	// ... other imports
)

// Connect to test database
func setupTestDB(t *testing.T) *pgxpool.Pool {
	dsn := "postgres://ohmf:ohmf@localhost:5432/ohmf?sslmode=disable"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Wait for database to be ready
	for i := 0; i < 30; i++ {
		err = pool.Ping(ctx)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		t.Fatalf("Database not ready: %v", err)
	}

	// Clear test data
	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// Test E2EE end-to-end flow
func TestE2EEFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool := setupTestDB(t)
	sm := &SessionManager{db: pool}

	ctx := context.Background()

	// TODO: Implement full E2EE flow
}

// Test group encryption with multiple members
func TestGroupEncryption(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	pool := setupTestDB(t)
	mre := NewMultiRecipientEncryption(pool)

	ctx := context.Background()

	// TODO: Test group encryption
}

// Test message encryption/decryption roundtrip
func TestMessageRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	pool := setupTestDB(t)

	ctx := context.Background()

	// TODO: Encrypt -> decrypt -> verify
}
```

### Running Integration Tests

```bash
# Run integration tests only
go test ./internal/e2ee -tags integration -v

# Run with timeout
go test ./internal/e2ee -tags integration -v -timeout 5m

# Run specific integration test
go test ./internal/e2ee -tags integration -run TestE2EEFlow -v

# Run all tests (unit + integration)
go test ./internal/e2ee -v

# Run only unit tests
go test ./internal/e2ee -short -v
```

### Test Scenarios

**1. Device Key Exchange Flow**
- Create device
- Generate identity keys
- Store keys in database
- Retrieve keys
- Verify fingerprint

**2. Single Recipient Encryption**
- Create sender device
- Create recipient device
- Encrypt message to recipient
- Decrypt message as recipient
- Verify plaintext matches

**3. Group Encryption Flow**
- Create group
- Add 3 members
- Encrypt message for group
- Each member decrypts
- Verify all get same plaintext

**4. Member Addition & Rekey**
- Create group with 2 members
- Add 3rd member
- New member can decrypt only post-join messages
- Old members forced to rekey in new epoch
- Verify forward secrecy

**5. Member Removal & Rekey**
- Create group with 3 members
- Remove 1 member
- Removed member cannot decrypt new messages
- Remaining members forced to rekey

**6. Concurrent Operations**
- Multiple devices sending simultaneously
- No race conditions
- Transactions properly isolated
- Order maintained

### Phase 3.6 Acceptance Criteria

✅ All 5 device key endpoints tested with real database
✅ Group encryption/decryption working end-to-end
✅ Member addition/removal properly triggers rekey
✅ Forward secrecy verified (removed members can't decrypt)
✅ Concurrent operations don't break consistency
✅ No timing/ordering issues
✅ Transactions properly committed

---

## Docker Troubleshooting Reference

### Useful Commands

```bash
# Start services in background
docker-compose -f docker-compose.test.yml up -d

# View status
docker-compose -f docker-compose.test.yml ps

# View logs (all services)
docker-compose -f docker-compose.test.yml logs

# View logs (specific service)
docker-compose -f docker-compose.test.yml logs -f gateway

# Execute command in container
docker-compose -f docker-compose.test.yml exec gateway bash

# Stop services
docker-compose -f docker-compose.test.yml down

# Stop and remove volumes
docker-compose -f docker-compose.test.yml down -v

# Rebuild image
docker-compose -f docker-compose.test.yml build --no-cache gateway

# View network
docker network ls

# Inspect network
docker network inspect ohmf-gateway_ohmf-network
```

### Common Issues & Solutions

| Issue | Cause | Solution |
|-------|-------|----------|
| Connection refused | PostgreSQL not ready | Check `docker-compose ps`, wait for health check |
| Port already in use | Service already running | `docker-compose down`, `lsof -i :8080` |
| Migrations failed | Database not initialized | Check `/bin/api migrate status` |
| Auth token issues | Missing authentication | Ensure Bearer token in headers |
| Slow queries | Index missing | Check migration files, run `ANALYZE` |
