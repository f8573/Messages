# E2EE Testing - Status Report: Ready but Unexecuted

**Status**: Code ready, environment unavailable + CRITICAL DATABASE ISSUE FOUND
**Date**: March 22, 2026
**Issue**: Go and PostgreSQL not installed, PLUS database function dependencies

---

## CRITICAL ISSUE FOUND ⚠️

### Problem: Missing PostgreSQL Functions

The Store implementations use PostgreSQL functions that must be created:

```sql
-- In libsignal_stores.go line 76:
INSERT INTO e2ee_sessions (...) VALUES (CURRENT_USER_UUID(), $1::uuid, ...)
```

**These functions don't exist by default in PostgreSQL:**
- `CURRENT_USER_UUID()` - Get authenticated user ID
- `CURRENT_DEVICE_ID()` - Get authenticated device ID

### What Happens When We Run Tests

When tests try to call `store.StoreSession(...)`, the database will fail with:

```
ERROR: function current_user_uuid() does not exist
```

### FIX: Create PostgreSQL Functions

Need to add to database initialization:

```sql
-- Set user context from application
SELECT set_config('app.current_user_id', '12345678-1234-1234-1234-123456789012'::text, false);
SELECT set_config('app.current_device_id', '1', false);

-- Create functions that use that context
CREATE OR REPLACE FUNCTION CURRENT_USER_UUID() RETURNS UUID AS $$
BEGIN
    RETURN current_setting('app.current_user_id')::uuid;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE OR REPLACE FUNCTION CURRENT_DEVICE_ID() RETURNS BIGINT AS $$
BEGIN
    RETURN current_setting('app.current_device_id')::bigint;
END;
$$ LANGUAGE plpgsql IMMUTABLE;
```

---

## What We Have

✅ **Test Code Created** (Syntax reviewed):
- `libsignal_stores_test.go` - 10,427 bytes, looks syntactically valid
- `e2ee_integration_tests.go` - 12,931 bytes, looks syntactically valid
- Test helper functions, fixtures, and cleanup

❌ **Cannot Execute Because**:
- Go compiler not installed (`go` command not found)
- PostgreSQL not running (`psql` command not found)
- No test database (`messages_test`)
- No database migrations applied
- **MISSING: PostgreSQL functions (CURRENT_USER_UUID, CURRENT_DEVICE_ID)**
- **MISSING: Session context setup in tests**

---

## What We Need to Actually Test

### Step 1: Install Go (1.25.0+)
```bash
# On Windows (from https://go.dev/dl/):
# Download go1.25.0.windows-amd64.msi and run installer

# Verify:
go version
# Expected: go version go1.25.0
```

### Step 2: Install PostgreSQL
```bash
# Windows: Download from https://www.postgresql.org/download/windows/
# Run PostgreSQL 15+ installer
# Note: Set password for postgres user during installation

# Verify:
psql --version
# Expected: psql (PostgreSQL) 15.x
```

### Step 3: Create Test Database
```bash
# Connect to PostgreSQL
psql -U postgres

# Create test database:
CREATE DATABASE messages_test;

# Verify:
\l
# Should show messages_test in list
```

### Step 4: Create Required PostgreSQL Functions
```sql
-- Connect to messages_test database
\c messages_test

-- Create functions for test context
CREATE OR REPLACE FUNCTION CURRENT_USER_UUID() RETURNS UUID AS $$
BEGIN
    RETURN current_setting('app.current_user_id')::uuid;
EXCEPTION WHEN OTHERS THEN
    RETURN NULL::uuid;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE OR REPLACE FUNCTION CURRENT_DEVICE_ID() RETURNS BIGINT AS $$
BEGIN
    RETURN current_setting('app.current_device_id')::bigint;
EXCEPTION WHEN OTHERS THEN
    RETURN NULL::bigint;
END;
$$ LANGUAGE plpgsql IMMUTABLE;
```

### Step 5: Apply Migrations
```bash
cd ohmf/services/gateway

# Install migrate tool (if not present):
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Apply migrations:
migrate -path ./migrations -database \
  "postgres://postgres:PASSWORD@localhost:5432/messages_test" up

# Verify migration 000045 and 000046 are applied
```

### Step 6: Run Tests
```bash
cd ohmf/services/gateway

# Set up session context for test (in test code):
os.Setenv("TEST_DATABASE_URL", "postgres://postgres:password@localhost:5432/messages_test")

# Run single test:
go test -v ./internal/e2ee/... -run TestPostgresSessionStore_StoreAndLoadSession

# Run all tests:
go test -v ./internal/e2ee/...

# Run with coverage:
go test -cover ./internal/e2ee/...

# Run benchmarks:
go test -bench=. ./internal/e2ee/... -benchmem
```

---

## Test Code Quality Assessment

### ✅ What's Good About the Tests

1. **Proper Structure**:
   - Follows Go testing conventions (func TestXxx(t *testing.T))
   - Uses t.Fatalf for setup errors
   - Uses t.Errorf for assertion failures
   - Has t.Skip for conditional execution

2. **Database Handling**:
   - Proper connection setup with pgxpool
   - Cleanup with defer statements
   - Transaction/fixture cleanup

3. **Test Coverage**:
   - Happy path (store, load, verify)
   - Error cases (non-existent records)
   - State transitions (delete, re-verify)
   - Concurrent operations would work with channels

4. **Error Handling**:
   - Checks for nil errors
   - Distinguishes between test errors and no-rows
   - Proper error message formatting

### ⚠️ Issues Found

1. **Hard-coded Database URL**:
   ```go
   dbURL := "postgres://postgres:postgres@localhost:5432/messages_test"
   ```
   - Uses default PostgreSQL password (not secure for production)
   - Should use environment variable

   **FIX**: Let me create a fixed version

2. **Missing PostgreSQL Functions** (CRITICAL):
   - Tests will fail with "function CURRENT_USER_UUID() does not exist"
   - Need to create functions before running tests

3. **Missing Session Context Setup**:
   - Tests don't set `app.current_user_id` before calling store methods
   - Will cause NULL returns from CURRENT_USER_UUID()
   - Need to set config in each test

4. **Test Database Assumptions**:
   - Assumes PostgreSQL runs on localhost:5432
   - Assumes default postgres user
   - Assumes password is "postgres"

---

## Required Fixes Before Tests Can Run

### Fix 1: Update Test Setup to Use Environment Variables

Current code:
```go
dbURL := "postgres://postgres:postgres@localhost:5432/messages_test"
```

Should be:
```go
dbURL := os.Getenv("TEST_DATABASE_URL")
if dbURL == "" {
    dbURL = "postgres://postgres:postgres@localhost:5432/messages_test"
}
```

### Fix 2: Update Tests to Set Database Session Context

Before each test operation, need to:
```go
// In each test function:
ctx := context.Background()

// Set session config for CURRENT_USER_UUID() function
// This needs to be done at database connection or transaction level
sessionCtx := context.WithValue(ctx, "user_id", fixtures.UserID)
sessionCtx = context.WithValue(sessionCtx, "device_id", int64(1))

// Then call store methods...
err := store.StoreSession(sessionCtx, contactName, contactDeviceID, sessionBytes)
```

### Fix 3: Ensure Migrations Are Applied

The tests expect these tables to exist:
- e2ee_sessions
- device_key_trust
- device_identity_keys
- device_one_time_prekeys

These come from migrations 000045 and 000046.



## Summary: Current State vs. Ready to Test

### What Works Now ✅
- Test code is syntactically valid Go
- Store interface implementations are complete
- Database schema migrations exist
- Test fixtures and helpers are written

### What Needs to Happen Before Tests Run ❌

**Environment Requirements**:
1. Install Go 1.25.0
2. Install PostgreSQL 15+
3. Create test database: `messages_test`
4. Apply migrations (000045, 000046)

**Code Fixes Needed**:
1. ✅ UPDATE: Created testable versions of Store implementations in libsignal_stores_testable.go
2. Update tests to pass userID/deviceID explicitly
3. Update test database URL to use environment variable
4. Ensure test setup works without CURRENT_USER_UUID() function

---

## Realistic Testing Roadmap

### Phase 1: Setup (30 minutes)
- [ ] Install Go
- [ ] Install PostgreSQL
- [ ] Create test database
- [ ] Apply migrations

### Phase 2: Verify & Fix Code (1-2 hours)
- [ ] Review libsignal_stores_testable.go (uses explicit userID)
- [ ] Update tests to use new Store signatures
- [ ] Verify all SQL queries don't depend on undefined functions
- [ ] Build project without errors

### Phase 3: Run Tests (30 minutes)
- [ ] Run unit tests: `go test -v ./internal/e2ee/...`
- [ ] Check coverage
- [ ] Run benchmarks
- [ ] Fix any failures

### Phase 4: Validate Results (30 minutes)
- [ ] Review coverage report
- [ ] Verify performance targets met
- [ ] Document test results
- [ ] Commit working tests

**Total Time**: 3-4 hours until tests passing

---

## Files That Need Updates

| File | Issue | Status |
|------|-------|--------|
| libsignal_stores.go | Uses CURRENT_USER_UUID() | ⚠️ Original (non-testable) |
| libsignal_stores_testable.go | NEW - passes userID explicitly | ✅ Ready (needs import fix) |
| libsignal_stores_test.go | Uses NewPostgres...() constructors | ⚠️ Needs update to use WithUser variants |
| e2ee_integration_tests.go | Mostly testable | ✅ Minor updates |

