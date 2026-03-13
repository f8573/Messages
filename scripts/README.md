Scripts
-------

Purpose
 - Centralized test runner scripts for this repository.

Files
 - `run-tests.sh` - POSIX shell test runner. Detects a local Go binary at `ohmf/.tools/go/bin/go` (or `.exe`) and uses it; otherwise uses system `go`. Starts a Postgres test container if needed.
 - `run-tests.ps1` - PowerShell test runner for Windows. Prefers `ohmf\.tools\go\bin\go.exe` when present.
 - `test-helpers.sh` - helper functions used by `run-tests.sh` (start/stop Postgres).

Usage
 - Linux / macOS / WSL:
   ```bash
   chmod +x scripts/*.sh
   ./scripts/run-tests.sh            # unit tests
   ./scripts/run-tests.sh --integration   # integration tests
   ```

 - Windows PowerShell:
   ```powershell
   powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\run-tests.ps1 -Integration
   ```

Notes
 - The scripts will start a Postgres service using `docker compose` if a `postgres` service is present in `docker-compose.yml`. If not, they fall back to a standalone `postgres:15-alpine` container named `ohmf_test_postgres`.
 - If you prefer to use an external database, set `TEST_DATABASE_URL` or `POSTGRES_URL` in the environment; the scripts will skip starting Postgres.
 - CI workflows have been updated to call `./scripts/run-tests.sh` so runners should have Docker and Go available.
