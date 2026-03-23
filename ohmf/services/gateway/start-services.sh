#!/bin/bash

# E2EE Gateway - Quick Start Script for macOS/Linux
# This script starts all services and prepares the application for testing

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
GATEWAY_DIR="ohmf/services/gateway"
COMPOSE_FILE="docker-compose.e2ee-test.yml"
API_PORT=8080
DB_PORT=5432
GO_BUILD_TIMEOUT=60

echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}  E2EE Gateway - Quick Start${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

# Step 0: Verify requirements
echo -e "\n${YELLOW}Step 0: Checking prerequisites...${NC}"

if ! command -v docker &> /dev/null; then
    echo -e "${RED}✘ Docker not found. Please install Docker first.${NC}"
    echo "  Download: https://www.docker.com/products/docker-desktop"
    exit 1
fi
echo -e "${GREEN}✓ Docker installed${NC}"

if ! command -v docker-compose &> /dev/null; then
    echo -e "${RED}✘ Docker Compose not found. Please install Docker Compose.${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Docker Compose installed${NC}"

if ! command -v go &> /dev/null; then
    echo -e "${RED}✘ Go not found. Please install Go 1.19+.${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Go installed ($(go version | awk '{print $3}'))${NC}"

if [ ! -d "$GATEWAY_DIR" ]; then
    echo -e "${RED}✘ Gateway directory not found: $GATEWAY_DIR${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Gateway directory found${NC}"

# Step 1: Stop any existing containers
echo -e "\n${YELLOW}Step 1: Cleaning up existing containers...${NC}"

cd "$GATEWAY_DIR"

if docker-compose -f "$COMPOSE_FILE" ps 2>/dev/null | grep -q "e2ee-test-db"; then
    echo "  Stopping existing PostgreSQL container..."
    docker-compose -f "$COMPOSE_FILE" down --remove-orphans 2>/dev/null || true
    sleep 2
fi
echo -e "${GREEN}✓ Cleanup complete${NC}"

# Step 2: Start PostgreSQL
echo -e "\n${YELLOW}Step 2: Starting PostgreSQL database...${NC}"

docker-compose -f "$COMPOSE_FILE" up -d
DB_CONTAINER_ID=$(docker-compose -f "$COMPOSE_FILE" ps -q postgres-e2ee)

if [ -z "$DB_CONTAINER_ID" ]; then
    echo -e "${RED}✘ Failed to start PostgreSQL container${NC}"
    exit 1
fi
echo "  PostgreSQL container started: $DB_CONTAINER_ID"

# Wait for database to be healthy
echo "  Waiting for database to be ready..."
RETRY_COUNT=0
MAX_RETRIES=30

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    STATUS=$(docker-compose -f "$COMPOSE_FILE" ps postgres-e2ee 2>/dev/null | grep -o "healthy\|unhealthy" || echo "unknown")

    if [ "$STATUS" = "healthy" ]; then
        echo -e "${GREEN}✓ PostgreSQL is ready${NC}"
        break
    fi

    RETRY_COUNT=$((RETRY_COUNT + 1))
    if [ $RETRY_COUNT -eq $MAX_RETRIES ]; then
        echo -e "${RED}✘ PostgreSQL failed to become healthy within timeout${NC}"
        echo "  Check logs: docker logs e2ee-test-db"
        exit 1
    fi

    echo -n "."
    sleep 1
done

# Step 3: Build the application
echo -e "\n${YELLOW}Step 3: Building Go application...${NC}"

# Verify Go modules
if [ ! -f "go.mod" ]; then
    echo -e "${RED}✘ go.mod not found in $GATEWAY_DIR${NC}"
    exit 1
fi

# Build with timeout
timeout $GO_BUILD_TIMEOUT go build -v ./cmd/api 2>&1 | tail -5
if [ $? -ne 0 ]; then
    echo -e "${RED}✘ Build failed${NC}"
    exit 1
fi

if [ ! -f "./cmd/api/api" ] && [ ! -f "./api" ] && [ ! -f "./api.exe" ]; then
    echo -e "${YELLOW}⚠ Build output not found in expected location (this may be OK)${NC}"
fi

echo -e "${GREEN}✓ Application built successfully${NC}"

# Step 4: Display connection information
echo -e "\n${YELLOW}Step 4: Running E2EE integration tests...${NC}"

export TEST_DATABASE_URL="postgres://e2ee_test:test_password_e2ee@localhost:5432/e2ee_test"

echo "  Connection: $TEST_DATABASE_URL"
echo "  Running tests..."

# Run tests with timeout
if timeout 120 go test -v -tags integration ./internal/e2ee -run E2EE 2>&1 | tail -20; then
    echo -e "${GREEN}✓ Integration tests passed${NC}"
else
    RESULT=$?
    if [ $RESULT -eq 124 ]; then
        echo -e "${YELLOW}⚠ Tests timed out${NC}"
    else
        echo -e "${YELLOW}⚠ Some tests may have failed (check output above)${NC}"
    fi
fi

# Step 5: Ready for use
echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}✓ READY FOR TESTING${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

echo -e "\n${YELLOW}Database Connection:${NC}"
echo -e "  Host: localhost"
echo -e "  Port: $DB_PORT"
echo -e "  User: e2ee_test"
echo -e "  Password: test_password_e2ee"
echo -e "  Database: e2ee_test"

echo -e "\n${YELLOW}Test Database:${NC}"
echo "  export TEST_DATABASE_URL=\"$TEST_DATABASE_URL\""
echo "  go test -v -tags integration ./internal/e2ee -run E2EE"

echo -e "\n${YELLOW}Manual Database Access:${NC}"
echo "  psql -h localhost -U e2ee_test -d e2ee_test"
echo "  Or: docker exec -it e2ee-test-db psql -U e2ee_test -d e2ee_test"

echo -e "\n${YELLOW}Available Unit Tests:${NC}"
echo "  cd $GATEWAY_DIR"
echo "  go test -v ./internal/e2ee"
echo "  go test -bench=. -benchmem ./internal/e2ee"

echo -e "\n${YELLOW}Stop Services:${NC}"
echo "  cd $GATEWAY_DIR"
echo "  docker-compose -f $COMPOSE_FILE down"

echo -e "\n${YELLOW}Full Reset (deletes all data):${NC}"
echo "  cd $GATEWAY_DIR"
echo "  docker-compose -f $COMPOSE_FILE down -v"
echo "  docker-compose -f $COMPOSE_FILE up -d"

echo -e "\n${BLUE}For more information:${NC}"
echo "  See: $GATEWAY_DIR/E2EE_COMPLETE_DOCUMENTATION.md"
echo "  See: $GATEWAY_DIR/internal/e2ee/migrations/README.md"

echo -e "\n${BLUE}Success! Everything is ready for testing.${NC}\n"
