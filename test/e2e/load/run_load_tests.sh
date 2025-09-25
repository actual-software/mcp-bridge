#!/bin/bash

# Load Testing Script for MCP Gateway
# This script runs various load testing scenarios

set -e

# Configuration
GATEWAY_URL="${GATEWAY_URL:-wss://localhost:8443/ws}"
AUTH_TOKEN="${AUTH_TOKEN:-}"
RESULTS_DIR="./load-test-results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Create results directory
mkdir -p "$RESULTS_DIR/$TIMESTAMP"

echo -e "${GREEN}MCP Gateway Load Testing Suite${NC}"
echo "================================"
echo "Gateway URL: $GATEWAY_URL"
echo "Results will be saved to: $RESULTS_DIR/$TIMESTAMP"
echo

# Function to check prerequisites
check_prerequisites() {
    echo -e "${YELLOW}Checking prerequisites...${NC}"
    
    # Check if k6 is installed
    if ! command -v k6 &> /dev/null; then
        echo -e "${RED}k6 is not installed. Please install k6:${NC}"
        echo "  brew install k6  # macOS"
        echo "  sudo apt-get install k6  # Ubuntu/Debian"
        exit 1
    fi
    
    # Check if Go is installed for native tests
    if ! command -v go &> /dev/null; then
        echo -e "${YELLOW}Go is not installed. Native Go tests will be skipped.${NC}"
        GO_AVAILABLE=false
    else
        GO_AVAILABLE=true
    fi
    
    # Generate auth token if not provided
    if [ -z "$AUTH_TOKEN" ]; then
        echo -e "${YELLOW}No auth token provided. Generating test token...${NC}"
        AUTH_TOKEN=$(openssl rand -hex 32)
    fi
    
    echo -e "${GREEN}Prerequisites check complete${NC}"
    echo
}

# Function to run k6 load test
run_k6_test() {
    local test_name=$1
    local scenario=$2
    local duration=$3
    
    echo -e "${YELLOW}Running $test_name...${NC}"
    
    k6 run \
        --out json="$RESULTS_DIR/$TIMESTAMP/${test_name}_metrics.json" \
        --summary-export="$RESULTS_DIR/$TIMESTAMP/${test_name}_summary.json" \
        -e GATEWAY_URL="$GATEWAY_URL" \
        -e AUTH_TOKEN="$AUTH_TOKEN" \
        -e scenario="$scenario" \
        k6_load_test.js \
        > "$RESULTS_DIR/$TIMESTAMP/${test_name}_output.txt" 2>&1
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ $test_name completed successfully${NC}"
    else
        echo -e "${RED}✗ $test_name failed${NC}"
    fi
}

# Function to run Go native tests
run_go_test() {
    local test_name=$1
    local test_function=$2
    
    if [ "$GO_AVAILABLE" = false ]; then
        echo -e "${YELLOW}Skipping $test_name (Go not available)${NC}"
        return
    fi
    
    echo -e "${YELLOW}Running $test_name...${NC}"
    
    go test -v -run "$test_function" \
        -timeout 6h \
        ./... \
        > "$RESULTS_DIR/$TIMESTAMP/${test_name}_output.txt" 2>&1
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ $test_name completed successfully${NC}"
    else
        echo -e "${RED}✗ $test_name failed${NC}"
    fi
}

# Function to monitor gateway during tests
monitor_gateway() {
    local duration=$1
    local output_file=$2
    
    echo -e "${YELLOW}Starting gateway monitoring for $duration...${NC}"
    
    while true; do
        # Collect metrics from gateway
        curl -s http://localhost:9090/metrics >> "$output_file" 2>/dev/null
        echo "---" >> "$output_file"
        
        # Also collect system metrics
        if command -v iostat &> /dev/null; then
            iostat 1 1 >> "$output_file" 2>/dev/null
        fi
        
        sleep 10
    done &
    
    MONITOR_PID=$!
}

# Function to stop monitoring
stop_monitoring() {
    if [ ! -z "$MONITOR_PID" ]; then
        kill $MONITOR_PID 2>/dev/null
        echo -e "${GREEN}Stopped gateway monitoring${NC}"
    fi
}

# Main test execution
main() {
    check_prerequisites
    
    # Start monitoring
    monitor_gateway "6h" "$RESULTS_DIR/$TIMESTAMP/gateway_metrics.txt"
    
    # Test selection menu
    echo "Select load tests to run:"
    echo "1) Quick smoke test (5 minutes)"
    echo "2) Standard load test (30 minutes)"
    echo "3) 10k concurrent connections test (30 minutes)"
    echo "4) Sustained load test (2 hours)"
    echo "5) Memory leak test (48 hours)"
    echo "6) Full test suite (4+ hours)"
    echo "7) Custom k6 test"
    read -p "Enter your choice (1-7): " choice
    
    case $choice in
        1)
            echo -e "${GREEN}Running smoke test...${NC}"
            k6 run \
                --vus 10 \
                --duration 5m \
                -e GATEWAY_URL="$GATEWAY_URL" \
                -e AUTH_TOKEN="$AUTH_TOKEN" \
                k6_load_test.js
            ;;
        2)
            echo -e "${GREEN}Running standard load test...${NC}"
            run_k6_test "standard_load" "concurrent_connections" "30m"
            ;;
        3)
            echo -e "${GREEN}Running 10k connections test...${NC}"
            run_go_test "10k_connections" "TestGateway_10kConcurrentConnections"
            ;;
        4)
            echo -e "${GREEN}Running sustained load test...${NC}"
            run_go_test "sustained_load" "TestGateway_SustainedLoad"
            ;;
        5)
            echo -e "${GREEN}Running memory leak test...${NC}"
            echo -e "${YELLOW}This test will run for 48 hours. Press Ctrl+C to stop.${NC}"
            k6 run \
                --config memoryLeakOptions \
                -e GATEWAY_URL="$GATEWAY_URL" \
                -e AUTH_TOKEN="$AUTH_TOKEN" \
                k6_load_test.js
            ;;
        6)
            echo -e "${GREEN}Running full test suite...${NC}"
            run_k6_test "stage1_rampup" "concurrent_connections" "30m"
            run_k6_test "stage2_burst" "burst_traffic" "10m"
            run_go_test "stage3_10k" "TestGateway_10kConcurrentConnections"
            run_go_test "stage4_sustained" "TestGateway_SustainedLoad"
            ;;
        7)
            echo "Enter custom k6 command:"
            read -p "> k6 run " custom_args
            k6 run $custom_args \
                -e GATEWAY_URL="$GATEWAY_URL" \
                -e AUTH_TOKEN="$AUTH_TOKEN" \
                k6_load_test.js
            ;;
        *)
            echo -e "${RED}Invalid choice${NC}"
            exit 1
            ;;
    esac
    
    # Stop monitoring
    stop_monitoring
    
    # Generate report
    generate_report
}

# Function to generate final report
generate_report() {
    echo
    echo -e "${GREEN}Generating test report...${NC}"
    
    cat > "$RESULTS_DIR/$TIMESTAMP/report.md" << EOF
# MCP Gateway Load Test Report

**Date:** $(date)
**Gateway URL:** $GATEWAY_URL
**Test Duration:** Various

## Test Results

### Summary

EOF

    # Parse results and add to report
    for result_file in "$RESULTS_DIR/$TIMESTAMP"/*_summary.json; do
        if [ -f "$result_file" ]; then
            test_name=$(basename "$result_file" _summary.json)
            echo "### $test_name" >> "$RESULTS_DIR/$TIMESTAMP/report.md"
            
            # Extract key metrics using jq if available
            if command -v jq &> /dev/null; then
                jq -r '
                    "- Total Requests: \(.metrics.http_reqs.count // 0)",
                    "- Success Rate: \(.metrics.checks.passes // 0)/\(.metrics.checks.fails + .metrics.checks.passes // 1)",
                    "- Avg Response Time: \(.metrics.http_req_duration.avg // 0)ms",
                    "- P95 Response Time: \(.metrics.http_req_duration["p(95)"] // 0)ms"
                ' "$result_file" >> "$RESULTS_DIR/$TIMESTAMP/report.md" 2>/dev/null || echo "Failed to parse $result_file" >> "$RESULTS_DIR/$TIMESTAMP/report.md"
            fi
            echo >> "$RESULTS_DIR/$TIMESTAMP/report.md"
        fi
    done
    
    echo -e "${GREEN}Report generated at: $RESULTS_DIR/$TIMESTAMP/report.md${NC}"
    echo
    echo "Test results saved to: $RESULTS_DIR/$TIMESTAMP/"
    ls -la "$RESULTS_DIR/$TIMESTAMP/"
}

# Trap to ensure monitoring is stopped on exit
trap stop_monitoring EXIT

# Run main function
main "$@"