#!/usr/bin/env bash
set -euo pipefail

# Run unit tests locally and integration tests inside the docker-compose 'itest' service.
# Usage: ./scripts/run-validation.sh

echo "== Running unit tests locally =="
go test ./... -v

echo "== Running integration tests in docker-compose (itest) =="
# Build and run the itest service; abort when it exits and return its exit code.
docker-compose up --build --abort-on-container-exit --exit-code-from itest itest

echo "== Done =="
