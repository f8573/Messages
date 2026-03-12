#!/usr/bin/env bash
set -euo pipefail
# Cross-platform (bash) test runner.
# Usage: ./scripts/run-tests.sh [--integration]
GO_CMD=${GO_CMD:-go}
ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT_DIR"

INTEGRATION=0
if [[ ${1:-} == "--integration" ]]; then
  INTEGRATION=1
fi

if [[ $INTEGRATION -eq 1 ]]; then
  start_postgres
  export OHMF_RUN_INTEGRATION=1
fi

# Helper: start a temporary postgres container for integration tests
start_postgres() {
  # If user already provided TEST_DATABASE_URL, do nothing
  if [[ -n "${TEST_DATABASE_URL:-}" ]]; then
    return
  fi

  PG_CONTAINER_NAME=messages_test_postgres
  PG_USER=test
  PG_PASS=testpass
  PG_DB=testdb
  PG_PORT=5433

  # If container already exists, remove it
  if docker ps -a --format '{{.Names}}' | grep -q "^${PG_CONTAINER_NAME}$"; then
    docker rm -f ${PG_CONTAINER_NAME} >/dev/null 2>&1 || true
  fi

  echo "Starting temporary Postgres container '${PG_CONTAINER_NAME}' on port ${PG_PORT}..."
  docker run -d --name ${PG_CONTAINER_NAME} -e POSTGRES_USER=${PG_USER} -e POSTGRES_PASSWORD=${PG_PASS} -e POSTGRES_DB=${PG_DB} -p ${PG_PORT}:5432 postgres:15-alpine >/dev/null

  # wait for postgres to accept connections
  echo "Waiting for Postgres to become ready..."
  for i in {1..60}; do
    if docker exec ${PG_CONTAINER_NAME} pg_isready -U ${PG_USER} >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  if ! docker exec ${PG_CONTAINER_NAME} pg_isready -U ${PG_USER} >/dev/null 2>&1; then
    echo "Postgres did not become ready in time" >&2
    docker logs ${PG_CONTAINER_NAME} || true
    docker rm -f ${PG_CONTAINER_NAME} >/dev/null 2>&1 || true
    exit 1
  fi

  export TEST_DATABASE_URL="postgres://${PG_USER}:${PG_PASS}@127.0.0.1:${PG_PORT}/${PG_DB}?sslmode=disable"
  echo "Set TEST_DATABASE_URL=${TEST_DATABASE_URL}"
  STARTED_PG=1
}

stop_postgres() {
  if [[ "${STARTED_PG:-0}" -eq 1 ]]; then
    echo "Stopping temporary Postgres container..."
    docker rm -f ${PG_CONTAINER_NAME} >/dev/null 2>&1 || true
  fi
}

trap stop_postgres EXIT

# Find modules
mods=$(find . -maxdepth 4 -name go.mod -not -path './ohmf/.tools/*' -print | sort)
if [[ -z "$mods" ]]; then
  echo "No go.mod files found; nothing to test."
  exit 0
fi

for mod in $mods; do
  dir=$(dirname "$mod")
  echo "== Module: $dir =="
  cd "$dir"

  pkgs=$($GO_CMD list -f '{{if or .GoFiles .TestGoFiles}}{{.ImportPath}}{{end}}' ./... | sed '/^$/d' || true)
  if [[ -z "$pkgs" ]]; then
    echo "No testable packages in $dir; skipping."
    cd - >/dev/null
    continue
  fi

  if [[ $INTEGRATION -eq 1 ]]; then
    echo "Running integration-enabled tests for $dir"
    # Set user-specified env vars expected by tests.
    # The caller should set TEST_DATABASE_URL, OHMF_RUN_INTEGRATION, etc.
    $GO_CMD test -v $pkgs
  else
    for p in $pkgs; do
      echo "-- testing $p --"
      $GO_CMD test -run Test -v $p || $GO_CMD test -v $p
    done
  fi

  cd - >/dev/null
done
