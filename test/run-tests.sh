#!/bin/bash
# MCP Bridge Test Runner Script

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORT_DIR="${TEST_DIR}/reports"
BASELINE_DIR="${TEST_DIR}/baselines"
LOG_FILE="${REPORT_DIR}/test-run-$(date +%Y%m%d-%H%M%S).log"

# Default values
TEST_LEVEL="${TEST_LEVEL:-quick}"
SAVE_BASELINE="${SAVE_BASELINE:-false}"
GENERATE_REPORT="${GENERATE_REPORT:-true}"
PARALLEL="${PARALLEL:-false}"

# Create directories
mkdir -p "$REPORT_DIR" "$BASELINE_DIR"

# Logging
log() {
    echo -e "${GREEN}[$(date +'%H:%M:%S')]${NC} $*" | tee -a "$LOG_FILE"
}

error() {
    echo -e "${RED}[ERROR]${NC} $*" | tee -a "$LOG_FILE"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $*" | tee -a "$LOG_FILE"
}

info() {
    echo -e "${BLUE}[INFO]${NC} $*" | tee -a "$LOG_FILE"
}

# Help function
show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

MCP Bridge Test Runner - Comprehensive test execution script

OPTIONS:
    -l, --level LEVEL       Test level: quick, standard, full, nightly (default: quick)
    -t, --type TYPE         Test type: smoke, contract, performance, regression, all
    -p, --parallel          Run tests in parallel
    -b, --save-baseline     Save performance results as baseline
    -r, --report            Generate HTML report after tests
    -s, --services          Check services before running tests
    -c, --ci                CI mode - fail on any test failure
    -h, --help              Show this help message

EXAMPLES:
    # Quick smoke test
    $0 -l quick -t smoke

    # Full test suite with report
    $0 -l full -t all -r

    # Performance regression with baseline update
    $0 -t regression -b

    # CI pipeline test
    $0 -c -l standard -t all

ENVIRONMENT VARIABLES:
    MCP_BASE_URL            Base URL for testing
    MCP_AUTH_TOKEN          Authentication token
    TEST_TIMEOUT            Overall test timeout (default: 30m)
    SKIP_CLEANUP            Skip cleanup after tests
EOF
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -l|--level)
            TEST_LEVEL="$2"
            shift 2
            ;;
        -t|--type)
            TEST_TYPE="$2"
            shift 2
            ;;
        -p|--parallel)
            PARALLEL=true
            shift
            ;;
        -b|--save-baseline)
            SAVE_BASELINE=true
            shift
            ;;
        -r|--report)
            GENERATE_REPORT=true
            shift
            ;;
        -s|--services)
            CHECK_SERVICES=true
            shift
            ;;
        -c|--ci)
            CI_MODE=true
            shift
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Service check function
check_services() {
    log "Checking service availability..."
    
    local services_ok=true
    
    # Check Gateway
    if curl -sf "${MCP_BASE_URL:-http://localhost:8080}/health" > /dev/null 2>&1; then
        log "✓ Gateway is healthy"
    else
        error "✗ Gateway is not accessible"
        services_ok=false
    fi
    
    # Check Router
    if curl -sf "http://localhost:9091/health" > /dev/null 2>&1; then
        log "✓ Router is healthy"
    else
        warning "✗ Router is not accessible"
    fi
    
    # Check Redis
    if redis-cli ping > /dev/null 2>&1; then
        log "✓ Redis is available"
    else
        warning "✗ Redis is not accessible"
    fi
    
    if [ "$services_ok" = false ] && [ "${CI_MODE:-false}" = true ]; then
        error "Required services are not available"
        exit 1
    fi
}

# Test execution functions
run_smoke_tests() {
    local level="${1:-quick}"
    log "Running smoke tests (level: $level)..."
    
    case "$level" in
        quick)
            make -C "$TEST_DIR" smoke-quick
            ;;
        standard|full)
            make -C "$TEST_DIR" smoke-full
            ;;
        critical)
            make -C "$TEST_DIR" smoke-critical
            ;;
        *)
            make -C "$TEST_DIR" smoke-quick
            ;;
    esac
}

run_contract_tests() {
    log "Running contract tests..."
    make -C "$TEST_DIR" contract
}

run_performance_tests() {
    local level="${1:-quick}"
    log "Running performance tests (level: $level)..."
    
    case "$level" in
        quick)
            make -C "$TEST_DIR" performance-quick
            ;;
        full|nightly)
            make -C "$TEST_DIR" performance
            ;;
        *)
            make -C "$TEST_DIR" performance-quick
            ;;
    esac
}

run_regression_tests() {
    log "Running regression tests..."
    make -C "$TEST_DIR" regression
    
    if [ "$SAVE_BASELINE" = true ]; then
        log "Saving new baseline..."
        make -C "$TEST_DIR" baseline-save
    fi
}

# Main test runner
run_tests() {
    local test_type="${TEST_TYPE:-all}"
    local exit_code=0
    
    log "Starting test run: type=$test_type, level=$TEST_LEVEL"
    
    if [ "${CHECK_SERVICES:-false}" = true ]; then
        check_services
    fi
    
    case "$test_type" in
        smoke)
            run_smoke_tests "$TEST_LEVEL" || exit_code=$?
            ;;
        contract)
            run_contract_tests || exit_code=$?
            ;;
        performance)
            run_performance_tests "$TEST_LEVEL" || exit_code=$?
            ;;
        regression)
            run_regression_tests || exit_code=$?
            ;;
        all)
            if [ "$PARALLEL" = true ]; then
                log "Running tests in parallel..."
                (run_smoke_tests "$TEST_LEVEL") &
                (run_contract_tests) &
                (run_performance_tests "$TEST_LEVEL") &
                wait
            else
                run_smoke_tests "$TEST_LEVEL" || exit_code=$?
                run_contract_tests || exit_code=$?
                run_performance_tests "$TEST_LEVEL" || exit_code=$?
                if [ "$TEST_LEVEL" = "full" ] || [ "$TEST_LEVEL" = "nightly" ]; then
                    run_regression_tests || exit_code=$?
                fi
            fi
            ;;
        *)
            error "Unknown test type: $test_type"
            exit 1
            ;;
    esac
    
    return $exit_code
}

# Report generation
generate_report() {
    log "Generating test report..."
    
    # Combine JSON reports
    if command -v jq > /dev/null 2>&1; then
        jq -s '.' "$REPORT_DIR"/*.json > "$REPORT_DIR/combined.json" 2>/dev/null || true
    fi
    
    # Generate HTML report if tool is available
    if command -v go-test-report > /dev/null 2>&1; then
        go-test-report -input "$REPORT_DIR"/*.json -output "$REPORT_DIR/test-report.html"
        log "HTML report generated: $REPORT_DIR/test-report.html"
    fi
    
    # Generate summary
    cat > "$REPORT_DIR/summary.txt" << EOF
Test Run Summary
================
Date: $(date)
Test Level: $TEST_LEVEL
Test Type: ${TEST_TYPE:-all}

Results:
--------
EOF
    
    # Parse results from log
    grep -E "(PASS|FAIL|SKIP)" "$LOG_FILE" >> "$REPORT_DIR/summary.txt" 2>/dev/null || true
    
    log "Test summary: $REPORT_DIR/summary.txt"
}

# Cleanup function
cleanup() {
    if [ "${SKIP_CLEANUP:-false}" = false ]; then
        log "Cleaning up temporary files..."
        rm -f "$TEST_DIR"/*.tmp
        rm -f "$TEST_DIR"/*.prof
    fi
}

# Signal handlers
trap cleanup EXIT
trap 'error "Test run interrupted"; exit 130' INT TERM

# Main execution
main() {
    local start_time=$(date +%s)
    local exit_code=0
    
    info "MCP Bridge Test Runner"
    info "====================="
    info "Test Level: $TEST_LEVEL"
    info "Test Type: ${TEST_TYPE:-all}"
    info "Parallel: $PARALLEL"
    info "CI Mode: ${CI_MODE:-false}"
    echo
    
    # Run tests
    run_tests || exit_code=$?
    
    # Generate report
    if [ "$GENERATE_REPORT" = true ]; then
        generate_report
    fi
    
    # Calculate duration
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    echo
    if [ $exit_code -eq 0 ]; then
        log "✅ All tests completed successfully in ${duration}s"
    else
        error "❌ Some tests failed (exit code: $exit_code)"
    fi
    
    # CI mode handling
    if [ "${CI_MODE:-false}" = true ] && [ $exit_code -ne 0 ]; then
        error "CI build failed due to test failures"
        exit $exit_code
    fi
    
    return $exit_code
}

# Run main function
main "$@"