# E2EE Gateway - Quick Start Reference Card

## 🚀 Get Started in 30 Seconds

### macOS / Linux
```bash
cd ohmf/services/gateway
chmod +x start-services.sh
./start-services.sh
```

### Windows
```powershell
cd ohmf\services\gateway
Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope Process
.\start-services.ps1
```

---

## What It Does

| Step | Action | Status |
|------|--------|--------|
| 0 | Check Docker, Go, paths | ✓ Automatic |
| 1 | Stop old containers | ✓ Automatic |
| 2 | Start PostgreSQL | ✓ Automatic |
| 3 | Build app | ✓ Automatic |
| 4 | Run tests | ✓ Automatic |
| 5 | Show next steps | ✓ Displays commands |

---

## 📊 Expected Output

```
✓ Docker installed
✓ PostgreSQL is ready
✓ Application built successfully
✓ Integration tests passed

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ READY FOR TESTING
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

---

## 🔧 After Startup

### Run Tests
```bash
cd ohmf/services/gateway

# All tests
go test -v ./internal/e2ee

# Specific category
go test -v -run TestDoubleRatchet ./internal/e2ee
go test -v -run TestX3DH ./internal/e2ee

# With benchmarks
go test -bench=. -benchmem ./internal/e2ee
```

### Access Database
```bash
# Direct connection (if psql installed)
psql -h localhost -U e2ee_test -d e2ee_test

# Docker connection
docker exec -it e2ee-test-db psql -U e2ee_test -d e2ee_test
```

### Stop Services
```bash
cd ohmf/services/gateway
docker-compose -f docker-compose.e2ee-test.yml down
```

### Full Reset
```bash
cd ohmf/services/gateway
docker-compose -f docker-compose.e2ee-test.yml down -v
./start-services.sh  # or .\start-services.ps1
```

---

## 📚 Learn More

- **Full Guide**: `E2EE_COMPLETE_DOCUMENTATION.md`
- **Script Details**: `START_SERVICES_README.md`
- **Database**: `internal/e2ee/migrations/README.md`
- **Troubleshooting**: See `START_SERVICES_README.md` (Troubleshooting section)

---

## 🐛 If Something Goes Wrong

| Issue | Check |
|-------|-------|
| Docker not found | `docker --version` |
| Port 5432 in use | `lsof -i :5432` or `netstat -ano \| findstr :5432` |
| Build fails | Check Go version: `go version` |
| Tests fail | Check database: `docker logs e2ee-test-db` |
| Script won't run | PowerShell execution policy on Windows |

---

## ✨ Key Features

✅ One command to start everything
✅ Automatic prerequisite checking
✅ Automatic database initialization
✅ Automatic integration tests
✅ Cross-platform (Windows/Mac/Linux)
✅ No configuration needed
✅ Idempotent (safe to repeat)
✅ Color-coded success/failure
✅ Shows next steps on completion

---

## 📊 Database Access

```
Host:     localhost
Port:     5432
User:     e2ee_test
Password: test_password_e2ee
Database: e2ee_test
```

**Test URL**:
```
postgres://e2ee_test:test_password_e2ee@localhost:5432/e2ee_test
```

---

## ⚡ Pro Tips

1. **First run takes longer** (~30-40s for database)
2. **Subsequent runs faster** (database container already exists)
3. **You can interrupt** (Ctrl+C) - database keeps running
4. **Scripts are safe** - Only start containers, don't modify code
5. **Run multiple times** - Scripts cleanup automatically

---

## 🎯 Common Workflows

### "Just check if E2EE still works"
```bash
./start-services.sh
# Tests run automatically, shows PASS/FAIL
```

### "I want to run specific tests"
```bash
./start-services.sh

# Then run specific test:
go test -v -run TestX3DHProtocol ./internal/e2ee
```

### "I need to query the database"
```bash
./start-services.sh

# In another terminal:
docker exec -it e2ee-test-db psql -U e2ee_test -d e2ee_test
```

### "Everything's broken, start fresh"
```bash
docker-compose -f docker-compose.e2ee-test.yml down -v
./start-services.sh
```

---

## 📝 Script Files

| File | Platform | Type |
|------|----------|------|
| `start-services.sh` | macOS/Linux | Bash |
| `start-services.ps1` | Windows | PowerShell |
| `START_SERVICES_README.md` | All | Documentation |
| `E2EE_COMPLETE_DOCUMENTATION.md` | All | Full Reference |

---

## 🚦 Status Indicators

| Symbol | Meaning |
|--------|---------|
| ✓ | Success - all good |
| ✘ | Error - must fix before continuing |
| ⚠ | Warning - may need attention, but continuing |
| . | Waiting for response (dots appear while waiting) |

---

## 💡 Remember

- **First run**: Takes 30-40 seconds (includes downloads)
- **Repeat runs**: Take 5-10 seconds (containers already exist)
- **Data persists**: Between script runs unless you delete the volume
- **Safe to repeat**: Scripts clean up before restarting
- **No code changes**: Scripts only start containers and run tests

**Enjoy testing the E2EE system! 🔒**
