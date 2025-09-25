#!/bin/bash

# MCP Bridge E2E Test Runner
# This script provides unified test discovery and execution for all E2E tests
# across different test suites in the project.

set -eo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Default settings
DEFAULT_TIMEOUT="30m"
DEFAULT_PARALLEL=4
VERBOSE=false
DRY_RUN=false
SELECTED_SUITES=()
RUN_ALL=false
EXCLUDE_SUITES=()
TEST_PATTERN=""
CI_MODE=false
CLEANUP_ON_EXIT=true

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

# Get test suite path
get_suite_path() {
    local suite=$1
    case $suite in
        unit) echo "./..." ;;
        integration) echo "./test/integration/..." ;;
        e2e-auth) echo "./test/e2e/auth/..." ;;
        e2e-chaos) echo "./test/e2e/chaos/..." ;;
        e2e-fuzz) echo "./test/e2e/fuzz/..." ;;
        e2e-load) echo "./test/e2e/load/..." ;;
        full-stack) echo "./test/e2e/full_stack" ;;
        k8s) echo "./test/e2e/k8s" ;;
        *) echo "" ;;
    esac
}

# Check if suite is valid
is_valid_suite() {
    local suite=$1
    local path=$(get_suite_path "$suite")
    [[ -n "$path" ]]
}

# Get all available suites
get_all_suites() {
    echo "unit integration e2e-auth e2e-chaos e2e-fuzz e2e-load full-stack k8s"
}

# Show usage information
show_usage() {
    cat << EOF
MCP Bridge E2E Test Runner

USAGE:
    $0 [OPTIONS] [SUITE...]

SUITES:
    unit            - Unit tests across all modules
    integration     - Integration tests
    e2e-auth        - E2E authentication tests
    e2e-chaos       - E2E chaos engineering tests
    e2e-fuzz        - E2E fuzz tests
    e2e-load        - E2E load tests
    full-stack      - Full stack E2E tests (Docker Compose)
    k8s             - Kubernetes E2E tests (requires KinD)

OPTIONS:
    -a, --all               Run all test suites
    -e, --exclude SUITE     Exclude specific test suite
    -p, --pattern PATTERN   Run tests matching pattern
    -t, --timeout TIMEOUT  Set test timeout (default: ${DEFAULT_TIMEOUT})
    -j, --parallel N        Set parallel execution count (default: ${DEFAULT_PARALLEL})
    -v, --verbose           Enable verbose output
    -n, --dry-run           Show what would be run without executing
    --ci                    Enable CI mode (stricter settings)
    --no-cleanup            Don't cleanup on exit
    -h, --help              Show this help message

EXAMPLES:
    # Run all E2E tests
    $0 --all

    # Run specific test suites
    $0 k8s full-stack

    # Run tests with pattern matching
    $0 --pattern "TestBasic" unit integration

    # Run in CI mode with timeout
    $0 --ci --timeout 45m --all

    # Exclude slow tests
    $0 --all --exclude k8s --exclude e2e-load

ENVIRONMENT VARIABLES:
    MCP_TEST_TIMEOUT        - Override default timeout
    MCP_TEST_PARALLEL       - Override default parallel count
    MCP_TEST_VERBOSE        - Enable verbose mode
    MCP_TEST_CI             - Enable CI mode
    MCP_TEST_NO_CLEANUP     - Disable cleanup on exit

EOF
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -a|--all)
                RUN_ALL=true
                shift
                ;;
            -e|--exclude)
                EXCLUDE_SUITES+=("$2")
                shift 2
                ;;
            -p|--pattern)
                TEST_PATTERN="$2"
                shift 2
                ;;
            -t|--timeout)
                DEFAULT_TIMEOUT="$2"
                shift 2
                ;;
            -j|--parallel)
                DEFAULT_PARALLEL="$2"
                shift 2
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -n|--dry-run)
                DRY_RUN=true
                shift
                ;;
            --ci)
                CI_MODE=true
                shift
                ;;
            --no-cleanup)
                CLEANUP_ON_EXIT=false
                shift
                ;;
            -h|--help)
                show_usage
                exit 0
                ;;
            -*)
                log_error "Unknown option: $1"
                show_usage
                exit 1
                ;;
            *)
                SELECTED_SUITES+=("$1")
                shift
                ;;
        esac
    done

    # Apply environment variable overrides
    DEFAULT_TIMEOUT="${MCP_TEST_TIMEOUT:-$DEFAULT_TIMEOUT}"
    DEFAULT_PARALLEL="${MCP_TEST_PARALLEL:-$DEFAULT_PARALLEL}"
    if [[ "${MCP_TEST_VERBOSE:-}" == "true" ]]; then
        VERBOSE=true
    fi
    if [[ "${MCP_TEST_CI:-}" == "true" ]]; then
        CI_MODE=true
    fi
    if [[ "${MCP_TEST_NO_CLEANUP:-}" == "true" ]]; then
        CLEANUP_ON_EXIT=false
    fi

    # CI mode adjustments
    if [[ "$CI_MODE" == "true" ]]; then
        VERBOSE=true
        DEFAULT_TIMEOUT="${DEFAULT_TIMEOUT:-60m}"
        DEFAULT_PARALLEL=2  # Lower parallelism for CI stability
    fi
}

# Validate test suite selection
validate_suites() {
    local suites_to_check=()
    
    if [[ "$RUN_ALL" == "true" ]]; then
        suites_to_check=($(get_all_suites))
    else
        suites_to_check=("${SELECTED_SUITES[@]}")
    fi

    # Remove excluded suites
    for exclude in "${EXCLUDE_SUITES[@]}"; do
        local new_suites=()
        for suite in "${suites_to_check[@]}"; do
            if [[ "$suite" != "$exclude" ]]; then
                new_suites+=("$suite")
            fi
        done
        suites_to_check=("${new_suites[@]}")
    done

    # Validate suite names
    for suite in "${suites_to_check[@]}"; do
        if ! is_valid_suite "$suite"; then
            log_error "Unknown test suite: $suite"
            log_info "Available suites: $(get_all_suites)"
            exit 1
        fi
    done

    # Update selected suites with validated list
    SELECTED_SUITES=("${suites_to_check[@]}")
}

# Check prerequisites for test execution
check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check Go installation
    if ! command -v go &> /dev/null; then
        log_error "Go is not installed or not in PATH"
        exit 1
    fi

    # Check if we're in the right directory
    if [[ ! -f "$PROJECT_ROOT/go.mod" ]]; then
        log_error "Not in MCP Bridge project root directory"
        exit 1
    fi

    # Check for specific suite requirements
    for suite in "${SELECTED_SUITES[@]}"; do
        case $suite in
            k8s)
                if ! command -v kind &> /dev/null && ! command -v kubectl &> /dev/null; then
                    log_warning "Kubernetes tests require 'kind' or 'kubectl' - K8s tests may fail"
                fi
                ;;
            full-stack)
                if ! command -v docker &> /dev/null; then
                    log_warning "Full stack tests require Docker - tests may fail"
                fi
                ;;
        esac
    done

    log_success "Prerequisites check passed"
}

# Execute a single test suite
run_test_suite() {
    local suite=$1
    local path=$(get_suite_path "$suite")
    local start_time=$(date +%s)
    
    log_info "Running test suite: $suite"
    
    # Build test command
    local test_cmd=("go" "test")
    
    # Add timeout
    test_cmd+=("-timeout" "$DEFAULT_TIMEOUT")
    
    # Add verbose flag if requested
    if [[ "$VERBOSE" == "true" ]]; then
        test_cmd+=("-v")
    fi
    
    # Add parallel flag
    test_cmd+=("-parallel" "$DEFAULT_PARALLEL")
    
    # Add race detection for unit and integration tests
    if [[ "$suite" == "unit" || "$suite" == "integration" ]]; then
        test_cmd+=("-race")
    fi
    
    # Add pattern matching if specified
    if [[ -n "$TEST_PATTERN" ]]; then
        test_cmd+=("-run" "$TEST_PATTERN")
    fi
    
    # Add coverage for unit tests
    if [[ "$suite" == "unit" ]]; then
        test_cmd+=("-coverprofile=coverage-${suite}.out")
    fi
    
    # Add path
    test_cmd+=("$path")
    
    # Handle special cases for workspace modules
    local work_dir="$PROJECT_ROOT"
    if [[ "$suite" == "k8s" || "$suite" == "full-stack" ]]; then
        work_dir="$PROJECT_ROOT/test/e2e/$([[ "$suite" == "full-stack" ]] && echo "full_stack" || echo "$suite")"
        test_cmd=("go" "test")
        if [[ "$VERBOSE" == "true" ]]; then
            test_cmd+=("-v")
        fi
        test_cmd+=("-timeout" "$DEFAULT_TIMEOUT")
        if [[ -n "$TEST_PATTERN" ]]; then
            test_cmd+=("-run" "$TEST_PATTERN")
        fi
        test_cmd+=(".")
    fi
    
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "Would run: (cd $work_dir && ${test_cmd[*]})"
        return 0
    fi
    
    # Execute the test
    log_info "Executing: ${test_cmd[*]}"
    local exit_code=0
    
    (cd "$work_dir" && "${test_cmd[@]}") || exit_code=$?
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    if [[ $exit_code -eq 0 ]]; then
        log_success "Test suite '$suite' completed successfully in ${duration}s"
    else
        log_error "Test suite '$suite' failed after ${duration}s (exit code: $exit_code)"
    fi
    
    return $exit_code
}

# Main execution function
main() {
    local overall_start_time=$(date +%s)
    local failed_suites=()
    local successful_suites=()
    
    log_info "Starting MCP Bridge E2E Test Runner"
    log_info "Project root: $PROJECT_ROOT"
    log_info "Test suites to run: ${SELECTED_SUITES[*]}"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "DRY RUN MODE - No tests will actually be executed"
    fi
    
    # Change to project root
    cd "$PROJECT_ROOT"
    
    # Run each test suite
    for suite in "${SELECTED_SUITES[@]}"; do
        if run_test_suite "$suite"; then
            successful_suites+=("$suite")
        else
            failed_suites+=("$suite")
        fi
        echo # Add spacing between suites
    done
    
    # Summary
    local overall_end_time=$(date +%s)
    local total_duration=$((overall_end_time - overall_start_time))
    
    echo "=============================================="
    log_info "Test execution completed in ${total_duration}s"
    
    if [[ ${#successful_suites[@]} -gt 0 ]]; then
        log_success "Successful suites (${#successful_suites[@]}): ${successful_suites[*]}"
    fi
    
    if [[ ${#failed_suites[@]} -gt 0 ]]; then
        log_error "Failed suites (${#failed_suites[@]}): ${failed_suites[*]}"
        exit 1
    fi
    
    log_success "All test suites passed!"
}

# Cleanup function
cleanup() {
    if [[ "$CLEANUP_ON_EXIT" == "true" ]]; then
        log_info "Performing cleanup..."
        # Add cleanup tasks here (e.g., killing background processes, removing temp files)
        # Clean up coverage files if not in CI mode
        if [[ "$CI_MODE" != "true" ]]; then
            rm -f "$PROJECT_ROOT"/coverage-*.out 2>/dev/null || true
        fi
    fi
}

# Set up signal handlers
trap cleanup EXIT
trap 'log_error "Script interrupted"; exit 130' INT TERM

# Main execution
parse_args "$@"

# Set default behavior if no suites specified
if [[ ${#SELECTED_SUITES[@]} -eq 0 && "$RUN_ALL" != "true" ]]; then
    log_warning "No test suites specified. Use --all to run all suites or specify specific suites."
    show_usage
    exit 1
fi

validate_suites
check_prerequisites
main