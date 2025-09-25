#!/bin/bash

# Kubernetes E2E Test Runner Script
# This script provides a convenient way to run the Kubernetes E2E tests
# with proper environment setup and cleanup.

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
TEST_CLUSTER_NAME="${K8S_TEST_CLUSTER_NAME:-mcp-e2e-test}"
TEST_NAMESPACE="${K8S_TEST_NAMESPACE:-mcp-e2e}"
TEST_TIMEOUT="${K8S_TEST_TIMEOUT:-20m}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Usage function
usage() {
    cat << EOF
Usage: $0 [OPTIONS] [TEST_PATTERN]

Run Kubernetes E2E tests for the MCP Bridge system.

OPTIONS:
    -h, --help          Show this help message
    -c, --clean         Clean up resources before running tests
    -f, --force-clean   Force cleanup (removes docker images too)
    -d, --debug         Enable debug output
    -s, --short         Run short tests only
    -t, --timeout TIME  Set test timeout (default: ${TEST_TIMEOUT})
    --cluster-name NAME Set cluster name (default: ${TEST_CLUSTER_NAME})
    --namespace NAME    Set test namespace (default: ${TEST_NAMESPACE})
    --no-cleanup        Skip cleanup after tests (for debugging)

TEST_PATTERN:
    Optional Go test pattern (e.g., TestKubernetesEndToEnd)
    If not specified, runs all tests

EXAMPLES:
    $0                                  # Run all tests
    $0 -s                              # Run short tests only
    $0 TestKubernetesEndToEnd          # Run specific test
    $0 -c -t 30m                       # Clean and run with 30m timeout
    $0 --debug --no-cleanup            # Debug mode with no cleanup

EOF
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    local missing_deps=()
    
    if ! command -v docker &> /dev/null; then
        missing_deps+=("docker")
    fi
    
    if ! command -v kind &> /dev/null; then
        missing_deps+=("kind")
    fi
    
    if ! command -v kubectl &> /dev/null; then
        missing_deps+=("kubectl")
    fi
    
    if ! command -v go &> /dev/null; then
        missing_deps+=("go")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        log_error "Missing required dependencies: ${missing_deps[*]}"
        log_info "Please install the missing dependencies and try again."
        exit 1
    fi
    
    # Check Docker daemon
    if ! docker info &> /dev/null; then
        log_error "Docker daemon is not running"
        exit 1
    fi
    
    # Check Go version
    local go_version
    go_version=$(go version | awk '{print $3}' | sed 's/go//')
    if [[ $(echo "${go_version}" | cut -d. -f1) -lt 1 ]] || [[ $(echo "${go_version}" | cut -d. -f2) -lt 21 ]]; then
        log_warning "Go version ${go_version} detected. Go 1.21+ is recommended."
    fi
    
    log_success "All prerequisites satisfied"
}

# Cleanup function
cleanup_resources() {
    local force_clean=$1
    
    log_info "Cleaning up test resources..."
    
    # Delete KinD cluster
    if kind get clusters | grep -q "^${TEST_CLUSTER_NAME}$"; then
        log_info "Deleting KinD cluster: ${TEST_CLUSTER_NAME}"
        kind delete cluster --name "${TEST_CLUSTER_NAME}" || log_warning "Failed to delete cluster"
    fi
    
    # Remove kubectl context
    if kubectl config get-contexts -o name | grep -q "kind-${TEST_CLUSTER_NAME}"; then
        log_info "Removing kubectl context"
        kubectl config delete-context "kind-${TEST_CLUSTER_NAME}" 2>/dev/null || true
    fi
    
    if [ "$force_clean" = true ]; then
        log_info "Performing force cleanup..."
        
        # Remove Docker images
        local images=("mcp-gateway:test" "mcp-router:test" "test-mcp-server:test")
        for image in "${images[@]}"; do
            if docker images --format "table {{.Repository}}:{{.Tag}}" | grep -q "^${image}$"; then
                log_info "Removing Docker image: ${image}"
                docker rmi "${image}" 2>/dev/null || log_warning "Failed to remove image ${image}"
            fi
        done
        
        # Prune Docker system
        log_info "Pruning Docker system..."
        docker system prune -f || log_warning "Failed to prune Docker system"
    fi
    
    log_success "Cleanup completed"
}

# Setup environment
setup_environment() {
    log_info "Setting up test environment..."
    
    cd "${SCRIPT_DIR}"
    
    # Initialize Go module if needed
    if [ ! -f go.mod ]; then
        log_info "Initializing Go module..."
        go mod init k8s-e2e-test
    fi
    
    # Download dependencies
    log_info "Downloading Go dependencies..."
    go mod tidy
    
    log_success "Environment setup completed"
}

# Run tests
run_tests() {
    local test_pattern="$1"
    local short_flag="$2"
    local debug_flag="$3"
    local timeout="$4"
    
    log_info "Running Kubernetes E2E tests..."
    log_info "Cluster: ${TEST_CLUSTER_NAME}, Namespace: ${TEST_NAMESPACE}, Timeout: ${timeout}"
    
    cd "${SCRIPT_DIR}"
    
    # Build test command
    local test_cmd="go test"
    
    if [ -n "$test_pattern" ]; then
        test_cmd+=" -run $test_pattern"
    fi
    
    if [ "$short_flag" = true ]; then
        test_cmd+=" -short"
    fi
    
    test_cmd+=" -v -timeout $timeout"
    
    # Set environment variables
    export K8S_TEST_CLUSTER_NAME="$TEST_CLUSTER_NAME"
    export K8S_TEST_NAMESPACE="$TEST_NAMESPACE"
    
    if [ "$debug_flag" = true ]; then
        export K8S_DEBUG=true
        test_cmd+=" -test.v"
    fi
    
    log_info "Running command: $test_cmd"
    
    # Run tests
    if eval "$test_cmd"; then
        log_success "All tests passed!"
        return 0
    else
        log_error "Tests failed!"
        return 1
    fi
}

# Show test results summary
show_summary() {
    local test_result=$1
    local cleanup_skipped=$2
    
    echo
    echo "=========================="
    echo "  TEST EXECUTION SUMMARY  "
    echo "=========================="
    echo "Cluster Name: ${TEST_CLUSTER_NAME}"
    echo "Namespace: ${TEST_NAMESPACE}"
    echo "Timeout: ${TEST_TIMEOUT}"
    
    if [ $test_result -eq 0 ]; then
        log_success "TESTS PASSED"
    else
        log_error "TESTS FAILED"
    fi
    
    if [ "$cleanup_skipped" = true ]; then
        log_info "Cleanup was skipped for debugging"
        log_info "To manually clean up:"
        log_info "  kind delete cluster --name ${TEST_CLUSTER_NAME}"
        log_info "  kubectl config delete-context kind-${TEST_CLUSTER_NAME}"
    fi
    
    echo "=========================="
}

# Main function
main() {
    local clean_before=false
    local force_clean=false
    local debug_mode=false
    local short_tests=false
    local timeout="$TEST_TIMEOUT"
    local test_pattern=""
    local no_cleanup=false
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                usage
                exit 0
                ;;
            -c|--clean)
                clean_before=true
                shift
                ;;
            -f|--force-clean)
                clean_before=true
                force_clean=true
                shift
                ;;
            -d|--debug)
                debug_mode=true
                shift
                ;;
            -s|--short)
                short_tests=true
                shift
                ;;
            -t|--timeout)
                timeout="$2"
                shift 2
                ;;
            --cluster-name)
                TEST_CLUSTER_NAME="$2"
                shift 2
                ;;
            --namespace)
                TEST_NAMESPACE="$2"
                shift 2
                ;;
            --no-cleanup)
                no_cleanup=true
                shift
                ;;
            -*)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
            *)
                test_pattern="$1"
                shift
                ;;
        esac
    done
    
    # Trap for cleanup on exit
    if [ "$no_cleanup" != true ]; then
        trap 'cleanup_resources false' EXIT
    fi
    
    # Main execution flow
    check_prerequisites
    
    if [ "$clean_before" = true ]; then
        cleanup_resources "$force_clean"
    fi
    
    setup_environment
    
    # Run tests and capture result
    local test_result=0
    run_tests "$test_pattern" "$short_tests" "$debug_mode" "$timeout" || test_result=$?
    
    # Show summary
    show_summary $test_result "$no_cleanup"
    
    exit $test_result
}

# Run main function with all arguments
main "$@"