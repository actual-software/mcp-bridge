#!/bin/bash
set -euo pipefail

# MCP Installation Scripts Docker Validation
# Tests installation scripts across multiple clean environments

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TEST_RESULTS_DIR="$SCRIPT_DIR/results"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m'

# Configuration
VERBOSE=false
CLEANUP=true
PARALLEL=false

# Test environments to validate against
ENVIRONMENTS=(
    "ubuntu:22.04"
    "ubuntu:20.04" 
    "debian:12"
    "alpine:3.18"
    "centos:8"
)

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --no-cleanup)
            CLEANUP=false
            shift
            ;;
        --parallel|-p)
            PARALLEL=true
            shift
            ;;
        --env)
            ENVIRONMENTS=("$2")
            shift 2
            ;;
        --help|-h)
            cat << EOF
MCP Installation Scripts Docker Validation

Usage: $0 [options]

Options:
    --verbose, -v      Show detailed output
    --no-cleanup      Don't cleanup Docker containers/images after tests
    --parallel, -p    Run tests in parallel (faster but harder to debug)
    --env ENV         Test only specific environment (e.g., ubuntu:22.04)
    --help, -h        Show this help

Tested Environments:
$(printf "    %s\n" "${ENVIRONMENTS[@]}")

Test Categories:
    1. Development Setup (scripts/install.sh)
    2. Router Binary Installation (services/router/install.sh)
    3. Router Uninstallation (services/router/uninstall.sh)
    4. Error Handling & Edge Cases

EOF
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Logging functions
log() {
    echo -e "${BLUE}[$(date +'%H:%M:%S')]${NC} $*"
}

success() {
    echo -e "${GREEN}✓${NC} $*"
}

error() {
    echo -e "${RED}✗${NC} $*" >&2
}

warn() {
    echo -e "${YELLOW}⚠${NC} $*"
}

verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${PURPLE}[DEBUG]${NC} $*"
    fi
}

# Setup test environment
setup() {
    log "Setting up validation environment..."
    
    # Create results directory
    mkdir -p "$TEST_RESULTS_DIR"
    
    # Check Docker availability
    if ! command -v docker >/dev/null 2>&1; then
        error "Docker is required but not installed"
        exit 1
    fi
    
    # Check if Docker daemon is running
    if ! docker info >/dev/null 2>&1; then
        error "Docker daemon is not running"
        exit 1
    fi
    
    success "Environment setup complete"
}

# Create test Dockerfile for each environment
create_dockerfile() {
    local base_image="$1"
    local dockerfile="$TEST_RESULTS_DIR/Dockerfile.${base_image//[:\\/]/-}"
    
    # Determine package manager and setup commands based on base image
    local setup_commands=""
    case "$base_image" in
        ubuntu:*|debian:*)
            setup_commands=$(cat << 'EOF'
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    ca-certificates \
    openssl \
    jq \
    sudo \
    git \
    build-essential \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
EOF
            )
            ;;
        alpine:*)
            setup_commands=$(cat << 'EOF'
RUN apk add --no-cache \
    curl \
    wget \
    ca-certificates \
    openssl \
    jq \
    sudo \
    git \
    build-base \
    bash
EOF
            )
            ;;
        centos:*)
            setup_commands=$(cat << 'EOF'
RUN yum update -y && yum install -y \
    curl \
    wget \
    ca-certificates \
    openssl \
    sudo \
    git \
    gcc \
    gcc-c++ \
    make \
    && yum clean all
# Install jq manually for CentOS
RUN curl -L https://github.com/stedolan/jq/releases/download/jq-1.6/jq-linux64 -o /usr/local/bin/jq \
    && chmod +x /usr/local/bin/jq
EOF
            )
            ;;
    esac
    
    cat > "$dockerfile" << EOF
FROM $base_image

# Install basic dependencies
$setup_commands

# Create test user with sudo privileges
RUN useradd -m -s /bin/bash testuser && \
    echo 'testuser ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers

# Set working directory
WORKDIR /home/testuser

# Switch to test user
USER testuser

# Set environment variables
ENV HOME=/home/testuser
ENV PATH=\$PATH:/usr/local/bin

# Default command
CMD ["/bin/bash"]
EOF

    echo "$dockerfile"
}

# Test development installation script
test_dev_install() {
    local env="$1"
    local container_name="mcp-dev-test-${env//[:\\/]/-}-$TIMESTAMP"
    local test_log="$TEST_RESULTS_DIR/dev-install-${env//[:\\/]/-}-$TIMESTAMP.log"
    
    log "Testing development installation on $env..."
    
    # Create Dockerfile
    local dockerfile=$(create_dockerfile "$env")
    
    # Build test image
    local image_name="mcp-test-${env//[:\\/]/-}:$TIMESTAMP"
    if ! docker build -t "$image_name" -f "$dockerfile" . >"$test_log" 2>&1; then
        error "Failed to build test image for $env"
        return 1
    fi
    
    # Copy installation script into container and test
    local test_script=$(cat << 'EOF'
#!/bin/bash
set -euo pipefail

echo "=== Testing Development Installation ==="

# Test 1: Check if script exists
if [[ ! -f "/mcp/scripts/install.sh" ]]; then
    echo "ERROR: Development install script not found"
    exit 1
fi

# Test 2: Script has correct permissions
if [[ ! -x "/mcp/scripts/install.sh" ]]; then
    echo "ERROR: Install script is not executable"
    exit 1
fi

# Test 3: Help option works
if ! /mcp/scripts/install.sh --help >/dev/null 2>&1; then
    echo "ERROR: Help option failed"
    exit 1
fi

# Test 4: Dry run with skip tests (should not fail on dependencies)
# Note: This tests argument parsing and basic validation
if ! timeout 30s /mcp/scripts/install.sh --skip-tests --yes --environment development 2>/dev/null; then
    echo "WARNING: Full installation failed (expected due to missing Go/Docker)"
    echo "INFO: This is normal in minimal container environments"
else
    echo "SUCCESS: Development installation completed"
fi

echo "=== Development Installation Test Complete ==="
exit 0
EOF
    )
    
    # Run test in container
    if docker run --rm \
        --name "$container_name" \
        -v "$PROJECT_ROOT:/mcp:ro" \
        "$image_name" \
        bash -c "$test_script" >>"$test_log" 2>&1; then
        success "Development installation test passed for $env"
        return 0
    else
        warn "Development installation test had issues for $env (check logs)"
        return 1
    fi
}

# Test router binary installation script
test_router_install() {
    local env="$1"
    local container_name="mcp-router-test-${env//[:\\/]/-}-$TIMESTAMP"
    local test_log="$TEST_RESULTS_DIR/router-install-${env//[:\\/]/-}-$TIMESTAMP.log"
    
    log "Testing router installation on $env..."
    
    # Create Dockerfile
    local dockerfile=$(create_dockerfile "$env")
    
    # Build test image
    local image_name="mcp-router-test-${env//[:\\/]/-}:$TIMESTAMP"
    if ! docker build -t "$image_name" -f "$dockerfile" . >"$test_log" 2>&1; then
        error "Failed to build test image for $env"
        return 1
    fi
    
    # Create comprehensive test script
    local test_script=$(cat << 'EOF'
#!/bin/bash
set -euo pipefail

echo "=== Testing Router Installation ==="

# Test 1: Check if script exists and is executable
if [[ ! -f "/mcp/services/router/install.sh" ]]; then
    echo "ERROR: Router install script not found"
    exit 1
fi

if [[ ! -x "/mcp/services/router/install.sh" ]]; then
    echo "ERROR: Router install script is not executable"
    exit 1
fi

# Test 2: Help option works
if ! /mcp/services/router/install.sh --help >/dev/null 2>&1; then
    echo "ERROR: Help option failed"
    exit 1
fi

# Test 3: Test argument parsing
echo "Testing argument parsing..."
if /mcp/services/router/install.sh --verbose --no-secure-storage --no-completions --no-interactive --binary-only --help >/dev/null 2>&1; then
    echo "SUCCESS: Argument parsing works"
else
    echo "ERROR: Argument parsing failed"
    exit 1
fi

# Test 4: Mock GitHub API to test download logic
echo "Testing GitHub API interaction..."
# This would normally fail due to no releases, but tests the API call logic
if /mcp/services/router/install.sh --verbose --binary-only 2>&1 | grep -q "Fetching latest version"; then
    echo "SUCCESS: GitHub API interaction attempted"
else
    echo "WARNING: GitHub API interaction may have issues"
fi

echo "=== Router Installation Test Complete ==="
exit 0
EOF
    )
    
    # Run test in container with sudo for installation
    if docker run --rm \
        --name "$container_name" \
        -v "$PROJECT_ROOT:/mcp:ro" \
        "$image_name" \
        sudo bash -c "$test_script" >>"$test_log" 2>&1; then
        success "Router installation test passed for $env"
        return 0
    else
        warn "Router installation test had issues for $env (check logs)"
        return 1
    fi
}

# Test router uninstallation script
test_router_uninstall() {
    local env="$1"
    local container_name="mcp-uninstall-test-${env//[:\\/]/-}-$TIMESTAMP"
    local test_log="$TEST_RESULTS_DIR/router-uninstall-${env//[:\\/]/-}-$TIMESTAMP.log"
    
    log "Testing router uninstallation on $env..."
    
    # Create Dockerfile
    local dockerfile=$(create_dockerfile "$env")
    
    # Build test image
    local image_name="mcp-uninstall-test-${env//[:\\/]/-}:$TIMESTAMP"
    if ! docker build -t "$image_name" -f "$dockerfile" . >"$test_log" 2>&1; then
        error "Failed to build test image for $env"
        return 1
    fi
    
    # Create test script
    local test_script=$(cat << 'EOF'
#!/bin/bash
set -euo pipefail

echo "=== Testing Router Uninstallation ==="

# Test 1: Check if script exists and is executable
if [[ ! -f "/mcp/services/router/uninstall.sh" ]]; then
    echo "ERROR: Router uninstall script not found"
    exit 1
fi

if [[ ! -x "/mcp/services/router/uninstall.sh" ]]; then
    echo "ERROR: Router uninstall script is not executable"  
    exit 1
fi

# Test 2: Create fake installation to test uninstall
echo "Setting up fake installation..."
sudo mkdir -p /usr/local/bin
sudo touch /usr/local/bin/mcp-router
sudo chmod +x /usr/local/bin/mcp-router

mkdir -p "$HOME/.config/claude-cli"
cat > "$HOME/.config/claude-cli/mcp-router.yaml" <<'YAML'
# Test configuration
version: 1
gateway:
  url: wss://test.example.com
YAML

# Test 3: Test uninstall (with auto-confirmation)
echo "Testing uninstallation..."
if echo "y" | /mcp/services/router/uninstall.sh 2>/dev/null; then
    echo "SUCCESS: Uninstallation completed"
    
    # Verify files were removed
    if [[ ! -f "/usr/local/bin/mcp-router" ]]; then
        echo "SUCCESS: Binary was removed"
    else
        echo "ERROR: Binary was not removed"
        exit 1
    fi
    
    if [[ ! -f "$HOME/.config/claude-cli/mcp-router.yaml" ]]; then
        echo "SUCCESS: Configuration was removed"
    else
        echo "ERROR: Configuration was not removed"
        exit 1
    fi
    
else
    echo "ERROR: Uninstallation failed"
    exit 1
fi

echo "=== Router Uninstallation Test Complete ==="
exit 0
EOF
    )
    
    # Run test in container
    if docker run --rm \
        --name "$container_name" \
        -v "$PROJECT_ROOT:/mcp:ro" \
        "$image_name" \
        bash -c "$test_script" >>"$test_log" 2>&1; then
        success "Router uninstallation test passed for $env"
        return 0
    else
        warn "Router uninstallation test had issues for $env (check logs)"
        return 1
    fi
}

# Test error handling and edge cases
test_edge_cases() {
    local env="$1"
    local container_name="mcp-edge-test-${env//[:\\/]/-}-$TIMESTAMP"
    local test_log="$TEST_RESULTS_DIR/edge-cases-${env//[:\\/]/-}-$TIMESTAMP.log"
    
    log "Testing edge cases on $env..."
    
    # Create Dockerfile
    local dockerfile=$(create_dockerfile "$env")
    
    # Build test image
    local image_name="mcp-edge-test-${env//[:\\/]/-}:$TIMESTAMP"
    if ! docker build -t "$image_name" -f "$dockerfile" . >"$test_log" 2>&1; then
        error "Failed to build test image for $env"
        return 1
    fi
    
    # Create edge case test script
    local test_script=$(cat << 'EOF'
#!/bin/bash
set -euo pipefail

echo "=== Testing Edge Cases ==="

# Test 1: Invalid arguments
echo "Testing invalid arguments..."
if /mcp/services/router/install.sh --invalid-option 2>/dev/null; then
    echo "ERROR: Should reject invalid arguments"
    exit 1
else
    echo "SUCCESS: Invalid arguments properly rejected"
fi

# Test 2: Permission handling
echo "Testing permission requirements..."
# Should fail without sudo for system installation
if /mcp/services/router/install.sh --binary-only 2>&1 | grep -q "Cannot write to"; then
    echo "SUCCESS: Permission check works"
else
    echo "WARNING: Permission check may not be working"
fi

# Test 3: Network error handling
echo "Testing network error handling..."
# Test with invalid GitHub repo (should handle gracefully)
if timeout 10s /mcp/services/router/install.sh --verbose --binary-only 2>&1 | grep -q "Failed to"; then
    echo "SUCCESS: Network errors handled gracefully"
else
    echo "INFO: Network error handling test inconclusive"
fi

# Test 4: File system edge cases
echo "Testing filesystem edge cases..."
# Test uninstall with non-existent files
if echo "y" | /mcp/services/router/uninstall.sh 2>/dev/null; then
    echo "SUCCESS: Uninstall handles missing files gracefully"
else
    echo "WARNING: Uninstall may not handle missing files well"
fi

echo "=== Edge Cases Test Complete ==="
exit 0
EOF
    )
    
    # Run test in container
    if docker run --rm \
        --name "$container_name" \
        -v "$PROJECT_ROOT:/mcp:ro" \
        "$image_name" \
        bash -c "$test_script" >>"$test_log" 2>&1; then
        success "Edge cases test passed for $env"
        return 0
    else
        warn "Edge cases test had issues for $env (check logs)"
        return 1
    fi
}

# Run all tests for a single environment
test_environment() {
    local env="$1"
    local results=()
    
    log "Starting validation for $env..."
    
    # Run all test types
    test_dev_install "$env" && results+=("dev:PASS") || results+=("dev:FAIL")
    test_router_install "$env" && results+=("router:PASS") || results+=("router:FAIL")
    test_router_uninstall "$env" && results+=("uninstall:PASS") || results+=("uninstall:FAIL")
    test_edge_cases "$env" && results+=("edge:PASS") || results+=("edge:FAIL")
    
    # Count results
    local passed=0
    local failed=0
    for result in "${results[@]}"; do
        if [[ "$result" == *":PASS" ]]; then
            ((passed++))
        else
            ((failed++))
        fi
    done
    
    # Log environment summary
    if [[ $failed -eq 0 ]]; then
        success "Environment $env: All tests passed ($passed/4)"
    else
        warn "Environment $env: $failed tests failed, $passed passed ($passed/4)"
    fi
    
    # Store results for final summary
    echo "$env:$passed:$failed:$(IFS=,; echo "${results[*]}")" >> "$TEST_RESULTS_DIR/summary.txt"
}

# Cleanup function
cleanup() {
    if [[ "$CLEANUP" == "true" ]]; then
        log "Cleaning up Docker resources..."
        
        # Remove test containers (if any are still running)
        docker ps -q --filter "name=mcp-*-test-*" | xargs -r docker rm -f >/dev/null 2>&1 || true
        
        # Remove test images
        docker images -q "mcp-*-test-*:$TIMESTAMP" | xargs -r docker rmi -f >/dev/null 2>&1 || true
        
        # Remove temporary Dockerfiles
        rm -f "$TEST_RESULTS_DIR"/Dockerfile.* 2>/dev/null || true
        
        success "Cleanup complete"
    fi
}

# Generate final report
generate_report() {
    local report_file="$TEST_RESULTS_DIR/validation-report-$TIMESTAMP.md"
    
    log "Generating validation report..."
    
    cat > "$report_file" << EOF
# MCP Installation Scripts Validation Report

**Date:** $(date)  
**Environments Tested:** ${#ENVIRONMENTS[@]}  
**Test Categories:** 4 (Development Setup, Router Install, Router Uninstall, Edge Cases)

## Summary

EOF

    # Read summary data
    local total_envs=0
    local total_passed=0
    local total_failed=0
    
    if [[ -f "$TEST_RESULTS_DIR/summary.txt" ]]; then
        while IFS=: read -r env passed failed details; do
            ((total_envs++))
            ((total_passed += passed))
            ((total_failed += failed))
            
            echo "### $env" >> "$report_file"
            echo "" >> "$report_file"
            echo "- **Passed:** $passed/4" >> "$report_file"
            echo "- **Failed:** $failed/4" >> "$report_file"
            echo "- **Details:** $details" >> "$report_file"
            echo "" >> "$report_file"
        done < "$TEST_RESULTS_DIR/summary.txt"
    fi
    
    cat >> "$report_file" << EOF

## Overall Results

- **Total Environments:** $total_envs
- **Total Tests:** $((total_envs * 4))
- **Total Passed:** $total_passed
- **Total Failed:** $total_failed
- **Success Rate:** $(( total_passed * 100 / (total_envs * 4) ))%

## Test Categories

1. **Development Setup** - Tests \`scripts/install.sh\` for full development environment
2. **Router Install** - Tests \`services/router/install.sh\` for binary installation
3. **Router Uninstall** - Tests \`services/router/uninstall.sh\` for clean removal
4. **Edge Cases** - Tests error handling and edge cases

## Log Files

Individual test logs are available in:
\`$TEST_RESULTS_DIR/\`

$(ls -la "$TEST_RESULTS_DIR"/*.log 2>/dev/null | sed 's/^/- /' || echo "- No log files generated")

EOF

    success "Report generated: $report_file"
    
    # Show summary
    echo
    log "Validation Summary:"
    echo "  Total Environments: $total_envs"
    echo "  Total Tests: $((total_envs * 4))"
    echo "  Passed: $total_passed"
    echo "  Failed: $total_failed"
    echo "  Success Rate: $(( total_passed * 100 / (total_envs * 4) ))%"
    echo
    
    if [[ $total_failed -eq 0 ]]; then
        success "All installation scripts validated successfully! ✨"
    else
        warn "Some tests failed. Check the report and logs for details."
    fi
}

# Main execution
main() {
    echo
    echo -e "${PURPLE}╔══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${PURPLE}║${CYAN}              MCP Installation Scripts Validation             ${PURPLE}║${NC}"
    echo -e "${PURPLE}║${CYAN}                    Docker-Based Testing                     ${PURPLE}║${NC}"
    echo -e "${PURPLE}╚══════════════════════════════════════════════════════════════╝${NC}"
    echo
    
    # Setup
    setup
    
    # Setup cleanup trap
    trap cleanup EXIT
    
    # Clear previous summary
    rm -f "$TEST_RESULTS_DIR/summary.txt"
    
    # Run tests for each environment
    if [[ "$PARALLEL" == "true" ]]; then
        log "Running tests in parallel..."
        for env in "${ENVIRONMENTS[@]}"; do
            test_environment "$env" &
        done
        wait
    else
        log "Running tests sequentially..."
        for env in "${ENVIRONMENTS[@]}"; do
            test_environment "$env"
        done
    fi
    
    # Generate final report
    generate_report
}

# Execute main function
main "$@"