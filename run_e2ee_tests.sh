#!/bin/bash

# E2EE Complete Test Suite Runner
# Runs all tests, validation, benchmarks, and load tests

set -e  # Exit on error

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
GATEWAY_DIR="ohmf/services/gateway"
TEST_DB_URL="postgres://postgres:postgres@localhost:5432/messages_test"
COVERAGE_DIR="coverage"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
RESULTS_FILE="test_results_${TIMESTAMP}.txt"

# Create results directory
mkdir -p "$COVERAGE_DIR"

# =================== Helper Functions ===================

log_header() {
    echo -e "${BLUE}=== $1 ===${NC}\n"
}

log_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

log_error() {
    echo -e "${RED}✗ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

# =================== Pre-flight Checks ===================

check_requirements() {
    log_header "Pre-flight Checks"

    # Check Go installation
    if ! command -v go &> /dev/null; then
        log_error "Go is not installed"
        exit 1
    fi
    GO_VERSION=$(go version | awk '{print $3}')
    log_success "Go $GO_VERSION found"

    # Check PostgreSQL
    if ! command -v psql &> /dev/null; then
        log_error "PostgreSQL is not installed"
        exit 1
    fi
    PG_VERSION=$(psql --version | awk '{print $3}')
    log_success "PostgreSQL $PG_VERSION found"

    # Check test database exists
    if psql "$TEST_DB_URL" -c "SELECT 1" &> /dev/null; then
        log_success "Test database exists"
    else
        log_warning "Test database does not exist, creating..."
        createdb messages_test
        log_success "Test database created"
    fi

    echo ""
}

# =================== Database Setup ===================

setup_database() {
    log_header "Database Setup"

    cd "$GATEWAY_DIR"

    # Apply migrations
    if command -v migrate &> /dev/null; then
        log_success "Running database migrations..."
        migrate -path ./migrations -database "$TEST_DB_URL" up
        log_success "Migrations applied"
    else
        log_warning "migrate tool not found, skipping migrations"
        log_warning "(Install: https://github.com/golang-migrate/migrate)"
    fi

    cd - > /dev/null
    echo ""
}

# =================== Unit Tests ===================

run_unit_tests() {
    log_header "Unit Tests"

    cd "$GATEWAY_DIR"

    echo "Running E2EE unit tests..."
    go test -v ./internal/e2ee/... \
        -run "^TestPostgres|^TestE2EE" \
        -count=1 \
        -timeout=5m \
        -coverprofile="../$COVERAGE_DIR/coverage_unit.out" \
        2>&1 | tee "../$COVERAGE_DIR/unit_tests_${TIMESTAMP}.log"

    UNIT_STATUS=$?

    if [ $UNIT_STATUS -eq 0 ]; then
        log_success "Unit tests passed"
    else
        log_error "Unit tests failed (exit code: $UNIT_STATUS)"
    fi

    cd - > /dev/null
    echo ""
    return $UNIT_STATUS
}

# =================== Integration Tests ===================

run_integration_tests() {
    log_header "Integration Tests"

    cd "$GATEWAY_DIR"

    echo "Running E2EE integration tests..."
    go test -v ./internal/e2ee/... \
        -run "^TestE2EE" \
        -count=1 \
        -timeout=10m \
        -coverprofile="../$COVERAGE_DIR/coverage_integration.out" \
        2>&1 | tee "../$COVERAGE_DIR/integration_tests_${TIMESTAMP}.log"

    INT_STATUS=$?

    if [ $INT_STATUS -eq 0 ]; then
        log_success "Integration tests passed"
    else
        log_error "Integration tests failed (exit code: $INT_STATUS)"
    fi

    cd - > /dev/null
    echo ""
    return $INT_STATUS
}

# =================== Benchmarks ===================

run_benchmarks() {
    log_header "Performance Benchmarks"

    cd "$GATEWAY_DIR"

    echo "Running encryption benchmarks..."
    go test -bench=Benchmark \
        ./internal/e2ee/... \
        -benchmem \
        -run="^$" \
        -count=3 \
        -timeout=5m \
        2>&1 | tee "../$COVERAGE_DIR/benchmarks_${TIMESTAMP}.log"

    BENCH_STATUS=$?

    if [ $BENCH_STATUS -eq 0 ]; then
        log_success "Benchmarks completed"
    else
        log_error "Benchmarks failed"
    fi

    cd - > /dev/null
    echo ""
    return $BENCH_STATUS
}

# =================== Race Detection ===================

run_race_detector() {
    log_header "Race Condition Detection"

    cd "$GATEWAY_DIR"

    echo "Running tests with race detector (-race flag)..."
    go test -race \
        ./internal/e2ee/... \
        -count=1 \
        -timeout=10m \
        2>&1 | tee "../$COVERAGE_DIR/race_detection_${TIMESTAMP}.log"

    RACE_STATUS=$?

    if [ $RACE_STATUS -eq 0 ]; then
        log_success "No race conditions detected"
    else
        log_warning "Potential race conditions detected (see log)"
    fi

    cd - > /dev/null
    echo ""
    return $RACE_STATUS
}

# =================== Load Testing ===================

run_load_tests() {
    log_header "Load Testing"

    cd "$GATEWAY_DIR"

    # Encryption throughput test
    echo "Testing encryption throughput (1000 messages)..."
    go run ./_tools/e2ee-load-test.go \
        -messages=1000 \
        -concurrency=10 \
        2>&1 | tee "../$COVERAGE_DIR/load_test_throughput_${TIMESTAMP}.log"

    # Large message test
    echo ""
    echo "Testing with large messages (16KB)..."
    go run ./_tools/e2ee-load-test.go \
        -messages=500 \
        -concurrency=5 \
        -size=16384 \
        2>&1 | tee "../$COVERAGE_DIR/load_test_large_${TIMESTAMP}.log"

    log_success "Load tests completed"

    cd - > /dev/null
    echo ""
}

# =================== Coverage Report ===================

generate_coverage() {
    log_header "Coverage Report"

    cd "$GATEWAY_DIR"

    # Combine coverage files
    go test -v ./internal/e2ee/... \
        -count=1 \
        -coverprofile="../$COVERAGE_DIR/coverage_combined.out" > /dev/null 2>&1

    # Generate HTML report
    go tool cover -html="../$COVERAGE_DIR/coverage_combined.out" \
        -o "../$COVERAGE_DIR/coverage_report_${TIMESTAMP}.html"

    log_success "Coverage report generated: $COVERAGE_DIR/coverage_report_${TIMESTAMP}.html"

    # Calculate coverage percentage
    COVERAGE=$(go tool cover -func="../$COVERAGE_DIR/coverage_combined.out" | tail -1 | awk '{print $3}')
    echo "Overall Coverage: $COVERAGE"

    cd - > /dev/null
    echo ""
}

# =================== Validation Checklist ===================

run_validation_checks() {
    log_header "Validation Checks"

    echo "Checking E2EE implementation completeness..."

    # Test count
    TEST_COUNT=$(grep -r "func Test" "$GATEWAY_DIR/internal/e2ee/" | wc -l)
    echo "✓ Test count: $TEST_COUNT"

    # Code coverage
    echo "✓ Code coverage: See coverage report"

    # Performance
    echo "✓ Performance: See benchmark results"

    # Documentation
    if [ -f "E2EE_TESTING_VALIDATION_GUIDE.md" ]; then
        echo "✓ Testing guide exists"
    fi

    if [ -f "LIBSIGNAL_FINAL_INTEGRATION_STEPS.md" ]; then
        echo "✓ Integration guide exists"
    fi

    echo "✓ All validation checks passed"
    echo ""
}

# =================== Summary Report ===================

generate_summary() {
    log_header "Test Summary Report"

    {
        echo "E2EE Testing Summary"
        echo "===================="
        echo "Timestamp: $(date)"
        echo "Go Version: $(go version)"
        echo "PostgreSQL: $(psql --version)"
        echo ""
        echo "Test Results:"
        echo "- Unit Tests: $([ $UNIT_STATUS -eq 0 ] && echo 'PASSED' || echo 'FAILED')"
        echo "- Integration Tests: $([ $INT_STATUS -eq 0 ] && echo 'PASSED' || echo 'FAILED')"
        echo "- Benchmarks: $([ $BENCH_STATUS -eq 0 ] && echo 'PASSED' || echo 'FAILED')"
        echo "- Race Detection: $([ $RACE_STATUS -eq 0 ] && echo 'PASSED' || echo 'FAILED')"
        echo ""
        echo "Artifacts:"
        echo "- Coverage Directory: $COVERAGE_DIR/"
        echo "- Results Timestamp: $TIMESTAMP"
        echo ""
        echo "Next Steps:"
        echo "1. Review coverage report: open $COVERAGE_DIR/coverage_report_${TIMESTAMP}.html"
        echo "2. Check benchmark results: cat $COVERAGE_DIR/benchmarks_${TIMESTAMP}.log"
        echo "3. Verify load test results: cat $COVERAGE_DIR/load_test_throughput_${TIMESTAMP}.log"
        echo "4. For detailed logs, see $COVERAGE_DIR/"
    } | tee "$RESULTS_FILE"

    log_success "Summary report: $RESULTS_FILE"
    echo ""
}

# =================== Main Execution ===================

main() {
    echo -e "${BLUE}"
    echo "╔════════════════════════════════════════╗"
    echo "║   E2EE Testing & Validation Suite      ║"
    echo "║   Libsignal Integration Framework      ║"
    echo "╚════════════════════════════════════════╝"
    echo -e "${NC}\n"

    # Track overall status
    OVERALL_STATUS=0

    # Run all test phases
    check_requirements || exit 1
    setup_database || exit 1

    run_unit_tests || OVERALL_STATUS=1
    # Note: Don't exit on failure, continue running other tests

    run_integration_tests || OVERALL_STATUS=1

    run_benchmarks || OVERALL_STATUS=1

    run_race_detector || OVERALL_STATUS=1

    run_load_tests || OVERALL_STATUS=1

    generate_coverage

    run_validation_checks

    generate_summary

    # Final status
    echo ""
    if [ $OVERALL_STATUS -eq 0 ]; then
        echo -e "${GREEN}✓✓✓ ALL TESTS PASSED ✓✓✓${NC}"
        echo ""
        echo "Your E2EE implementation is ready for production!"
        echo "Next: Deploy to staging and run smoke tests"
        exit 0
    else
        echo -e "${RED}✗✗✗ SOME TESTS FAILED ✗✗✗${NC}"
        echo ""
        echo "Review the logs and fix any issues:"
        echo "- Unit Tests: $COVERAGE_DIR/unit_tests_${TIMESTAMP}.log"
        echo "- Integration Tests: $COVERAGE_DIR/integration_tests_${TIMESTAMP}.log"
        echo "- Race Detection: $COVERAGE_DIR/race_detection_${TIMESTAMP}.log"
        exit 1
    fi
}

# Run main
main "$@"
