# Quick Start Scripts

One-command startup for E2EE Gateway development and testing.

## Overview

These scripts automatically:
1. ✅ Start PostgreSQL database (Docker)
2. ✅ Wait for database to be ready
3. ✅ Build the Go application
4. ✅ Run E2EE integration tests
5. ✅ Display connection information
6. ✅ Show available test commands

## Usage

### macOS / Linux

```bash
cd ohmf/services/gateway
chmod +x start-services.sh
./start-services.sh
```

### Windows (PowerShell)

```powershell
cd ohmf\services\gateway
Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope Process  # Allow script execution
.\start-services.ps1
```

Or with no color output:

```powershell
.\start-services.ps1 -NoColor
```

## What the Scripts Do

### Step 0: Prerequisites
- ✓ Verify Docker installed
- ✓ Verify Docker Compose installed
- ✓ Verify Go 1.19+ installed
- ✓ Verify gateway directory exists

### Step 1: Cleanup
- Stop any existing containers
- Remove orphaned services
- Wait for cleanup to complete

### Step 2: Start Database
- Pull PostgreSQL 15 Alpine image (if needed)
- Start container with auto-initialization
- Verify database becomes healthy
- Wait up to 30 seconds for readiness

### Step 3: Build Application
- Verify go.mod exists
- Compile `./cmd/api`
- Display build output

### Step 4: Run Integration Tests
- Set TEST_DATABASE_URL environment variable
- Execute all E2EE integration tests
- Display test results

### Step 5: Display Instructions
- Database connection details
- Example test commands
- Manual database access instructions
- Service stop/cleanup commands
- Documentation references

## Example Output

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  E2EE Gateway - Quick Start
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Step 0: Checking prerequisites...
✓ Docker installed
✓ Docker Compose installed
✓ Go installed (go1.21.0)
✓ Gateway directory found

Step 1: Cleaning up existing containers...
✓ Cleanup complete

Step 2: Starting PostgreSQL database...
  PostgreSQL container started: abc123def456
  Waiting for database to be ready...
✓ PostgreSQL is ready

Step 3: Building Go application...
ohmf/services/gateway/internal/e2ee
ohmf/services/gateway/internal/conversations
ohmf/services/gateway/internal/messages
ohmf/services/gateway/internal/realtime
ohmf/services/gateway/cmd/api
✓ Application built successfully

Step 4: Running E2EE integration tests...
  Connection: postgres://e2ee_test:test_password_e2ee@localhost:5432/e2ee_test
  Running tests...
=== RUN   TestE2EEEndToEndWithDatabase
--- PASS: TestE2EEEndToEndWithDatabase (0.05s)
=== RUN   TestE2EEMultipleMessagesWithDatabase
--- PASS: TestE2EEMultipleMessagesWithDatabase (0.03s)
=== RUN   TestE2EEForwardSecrecyWithDatabase
--- PASS: TestE2EEForwardSecrecyWithDatabase (0.02s)
✓ Integration tests passed

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ READY FOR TESTING
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Database Connection:
  Host: localhost
  Port: 5432
  User: e2ee_test
  Password: test_password_e2ee
  Database: e2ee_test

Test Database:
  export TEST_DATABASE_URL="postgres://..."
  go test -v -tags integration ./internal/e2ee -run E2EE

Success! Everything is ready for testing.
```

## Common Commands After Startup

### Run All Unit Tests

```bash
cd ohmf/services/gateway
go test -v ./internal/e2ee
```

### Run Benchmarks

```bash
cd ohmf/services/gateway
go test -bench=. -benchmem ./internal/e2ee
```

### Run Specific Test Category

```bash
cd ohmf/services/gateway

# Double Ratchet tests
go test -v -run TestDoubleRatchet ./internal/e2ee

# X3DH tests
go test -v -run TestX3DH ./internal/e2ee

# Group Encryption tests
go test -v -run TestGroupEncryption ./internal/e2ee
```

### Access Database Manually

```bash
# From command line (if psql installed)
psql -h localhost -U e2ee_test -d e2ee_test

# From Docker
docker exec -it e2ee-test-db psql -U e2ee_test -d e2ee_test

# Example queries:
# \dt                           # List tables
# SELECT * FROM device_identity_keys;
# SELECT * FROM sessions;
# \q                            # Quit
```

### Rebuild Application

```bash
cd ohmf/services/gateway
go build -v ./cmd/api
```

### Stop Services

```bash
cd ohmf/services/gateway
docker-compose -f docker-compose.e2ee-test.yml down
```

### Full Reset (Delete All Data)

```bash
cd ohmf/services/gateway
docker-compose -f docker-compose.e2ee-test.yml down -v
docker-compose -f docker-compose.e2ee-test.yml up -d
```

## Troubleshooting

### "Docker not found"

**Windows**: Install Docker Desktop for Windows
- https://www.docker.com/products/docker-desktop

**macOS**: Install Docker Desktop for Mac or use Homebrew
```bash
brew install docker-desktop
```

**Linux**: Install Docker using package manager
```bash
sudo apt-get install docker.io docker-compose
```

### "Port 5432 already in use"

Another application is using the database port.

**Option 1**: Stop the conflicting service
```bash
# Find what's using port 5432
lsof -i :5432              # macOS/Linux
netstat -ano | findstr :5432  # Windows

# Stop that service
```

**Option 2**: Use different port in docker-compose.e2ee-test.yml
```yaml
ports:
  - "5433:5432"  # Use 5433 instead

# Update connection string:
export TEST_DATABASE_URL="postgres://e2ee_test:test_password_e2ee@localhost:5433/e2ee_test"
```

### "PostgreSQL failed to become healthy"

Database initialization timing issue.

**Solution**: Check logs and wait longer
```bash
docker logs e2ee-test-db

# Or manually wait
sleep 30

# Verify readiness
docker-compose -f docker-compose.e2ee-test.yml ps
```

### "Build failed" / "Go mod not found"

PowerShell execution policy issue.

**Solution**:
```powershell
Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope Process
```

Or navigate to gateway directory first:
```powershell
cd ohmf\services\gateway
.\start-services.ps1
```

### Tests fail with "connection refused"

Database not ready or port mapping issue.

**Solutions**:
```bash
# Verify container is running
docker ps | grep e2ee-test-db

# Verify port mapping
docker port e2ee-test-db

# Check database logs
docker logs e2ee-test-db

# Manual health check
docker exec e2ee-test-db pg_isready -U e2ee_test -d e2ee_test
```

## Script Details

### macOS/Linux (start-services.sh)

- **Language**: Bash
- **Lines**: 182
- **Features**:
  - Color-coded output (Red/Yellow/Green/Blue)
  - Proper error handling with `set -e`
  - Timeout protection on build and tests
  - Comprehensive logging

### Windows (start-services.ps1)

- **Language**: PowerShell
- **Lines**: 246
- **Features**:
  - Color-coded output (Red/Yellow/Green/Cyan)
  - `-NoColor` option for CI/CD
  - Error handling with try/catch
  - Detailed error messages

## Environment Variables Set

After startup, these are available:

```bash
export TEST_DATABASE_URL="postgres://e2ee_test:test_password_e2ee@localhost:5432/e2ee_test"
```

## Next Steps

1. **Run tests**: Commands shown at script end
2. **Explore E2EE**: See `E2EE_COMPLETE_DOCUMENTATION.md`
3. **Database schema**: See `internal/e2ee/migrations/README.md`
4. **Quit database**: `Ctrl+C`

## Script Source Files

- **macOS/Linux**: `start-services.sh` (executable)
- **Windows**: `start-services.ps1` (PowerShell)

Both scripts are idempotent - safe to run multiple times. Previous services are cleaned up before starting new ones.

## FAQ

**Q: Does the script modify my files?**
A: No. Scripts only start containers and run tests. No code changes.

**Q: Can I interrupt the script?**
A: Yes. Press `Ctrl+C` to stop. Database will keep running.

**Q: How do I use the database after startup?**
A: See "Access Database Manually" section above.

**Q: What if I need to start from scratch?**
A: Run: `docker-compose down -v` then the startup script again.

**Q: Is the database in Docker production-ready?**
A: No. For production, use managed PostgreSQL or a dedicated database server with proper security, backups, and monitoring.
