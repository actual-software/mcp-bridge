#!/bin/bash
set -e

# MCP Bridge Performance Benchmark Script
# This script runs comprehensive performance benchmarks using multiple tools

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RESULTS_DIR="${PROJECT_ROOT}/benchmarks/results/$(date +%Y%m%d_%H%M%S)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Create results directory
mkdir -p "${RESULTS_DIR}"

echo -e "${GREEN}MCP Bridge Performance Benchmark${NC}"
echo -e "${GREEN}================================${NC}"
echo "Results will be saved to: ${RESULTS_DIR}"
echo

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to run k6 tests
run_k6_benchmark() {
    echo -e "${YELLOW}Running k6 performance tests...${NC}"
    
    if ! command_exists k6; then
        echo -e "${RED}k6 not found. Please install k6: https://k6.io/docs/getting-started/installation${NC}"
        return 1
    fi
    
    # Set environment variables
    export GATEWAY_URL="${GATEWAY_URL:-wss://localhost:8443/ws}"
    export AUTH_TOKEN="${AUTH_TOKEN:-test-token}"
    
    # Run k6 test
    k6 run \
        --out json="${RESULTS_DIR}/k6-results.json" \
        --out csv="${RESULTS_DIR}/k6-results.csv" \
        --summary-export="${RESULTS_DIR}/k6-summary.json" \
        "${PROJECT_ROOT}/benchmarks/k6/performance-test.js" \
        | tee "${RESULTS_DIR}/k6-output.log"
    
    # Generate HTML report if k6-reporter is available
    if command_exists k6-reporter; then
        k6-reporter "${RESULTS_DIR}/k6-summary.json" -o "${RESULTS_DIR}/k6-report.html"
    fi
    
    echo -e "${GREEN}k6 tests completed${NC}"
}

# Function to run Go benchmarks
run_go_benchmarks() {
    echo -e "${YELLOW}Running Go benchmarks...${NC}"
    
    cd "${PROJECT_ROOT}"
    
    # Run benchmarks and save results
    go test -bench=. -benchmem -benchtime=10s ./... \
        -run=^$ \
        > "${RESULTS_DIR}/go-benchmark.txt" 2>&1
    
    # Generate CPU profile
    go test -bench=. -benchmem -benchtime=30s -cpuprofile="${RESULTS_DIR}/cpu.prof" \
        ./services/gateway/internal/... \
        -run=^$ >/dev/null 2>&1
    
    # Generate memory profile
    go test -bench=. -benchmem -benchtime=30s -memprofile="${RESULTS_DIR}/mem.prof" \
        ./services/router/internal/... \
        -run=^$ >/dev/null 2>&1
    
    echo -e "${GREEN}Go benchmarks completed${NC}"
}

# Function to run vegeta load test
run_vegeta_benchmark() {
    echo -e "${YELLOW}Running Vegeta load tests...${NC}"
    
    if ! command_exists vegeta; then
        echo -e "${RED}Vegeta not found. Please install vegeta: https://github.com/tsenart/vegeta${NC}"
        return 1
    fi
    
    # Create targets file
    cat > "${RESULTS_DIR}/vegeta-targets.txt" <<EOF
GET https://localhost:8443/health
Authorization: Bearer ${AUTH_TOKEN:-test-token}

POST https://localhost:8443/api/v1/execute
Content-Type: application/json
Authorization: Bearer ${AUTH_TOKEN:-test-token}
{"jsonrpc":"2.0","method":"tools/list","id":1}
EOF
    
    # Run vegeta attack
    echo "Running Vegeta attack at 1000 RPS for 30s..."
    vegeta attack \
        -duration=30s \
        -rate=1000/1s \
        -targets="${RESULTS_DIR}/vegeta-targets.txt" \
        -output="${RESULTS_DIR}/vegeta-results.bin"
    
    # Generate report
    vegeta report \
        -type=text \
        "${RESULTS_DIR}/vegeta-results.bin" \
        > "${RESULTS_DIR}/vegeta-report.txt"
    
    # Generate histogram
    vegeta report \
        -type=hist[0,10ms,25ms,50ms,100ms,200ms,500ms,1s,2s,5s] \
        "${RESULTS_DIR}/vegeta-results.bin" \
        > "${RESULTS_DIR}/vegeta-histogram.txt"
    
    # Generate JSON metrics
    vegeta report \
        -type=json \
        "${RESULTS_DIR}/vegeta-results.bin" \
        > "${RESULTS_DIR}/vegeta-metrics.json"
    
    echo -e "${GREEN}Vegeta tests completed${NC}"
}

# Function to run wrk benchmark
run_wrk_benchmark() {
    echo -e "${YELLOW}Running wrk benchmarks...${NC}"
    
    if ! command_exists wrk; then
        echo -e "${RED}wrk not found. Please install wrk: https://github.com/wg/wrk${NC}"
        return 1
    fi
    
    # Create Lua script for wrk
    cat > "${RESULTS_DIR}/wrk-script.lua" <<'EOF'
wrk.method = "POST"
wrk.body   = '{"jsonrpc":"2.0","method":"tools/list","id":1}'
wrk.headers["Content-Type"] = "application/json"
wrk.headers["Authorization"] = "Bearer " .. (os.getenv("AUTH_TOKEN") or "test-token")

-- Report latency percentiles
done = function(summary, latency, requests)
    io.write("------------------------------\n")
    io.write(string.format("Requests/sec: %d\n", summary.requests/summary.duration*1e6))
    io.write(string.format("Transfer/sec: %.2f MB\n", summary.bytes/summary.duration*1e6/1024/1024))
    io.write(string.format("Avg latency: %.2f ms\n", latency.mean/1000))
    io.write(string.format("Stdev latency: %.2f ms\n", latency.stdev/1000))
    io.write(string.format("Max latency: %.2f ms\n", latency.max/1000))
    io.write("Latency Distribution:\n")
    for _, p in pairs({50, 75, 90, 95, 99, 99.9}) do
        n = latency:percentile(p)
        io.write(string.format("  %g%%: %.2f ms\n", p, n/1000))
    end
end
EOF
    
    # Run wrk benchmark
    echo "Running wrk with 1000 connections for 60s..."
    wrk \
        -t12 \
        -c1000 \
        -d60s \
        -s "${RESULTS_DIR}/wrk-script.lua" \
        --latency \
        https://localhost:8443/api/v1/execute \
        > "${RESULTS_DIR}/wrk-results.txt" 2>&1
    
    echo -e "${GREEN}wrk benchmarks completed${NC}"
}

# Function to collect system metrics
collect_system_metrics() {
    echo -e "${YELLOW}Collecting system metrics...${NC}"
    
    # Get system info
    {
        echo "System Information"
        echo "=================="
        echo "Date: $(date)"
        echo "Hostname: $(hostname)"
        echo "OS: $(uname -a)"
        echo
        echo "CPU Information:"
        if [[ "$OSTYPE" == "linux-gnu"* ]]; then
            lscpu | grep -E "Model name|CPU\(s\)|Thread\(s\)|Core\(s\)"
        elif [[ "$OSTYPE" == "darwin"* ]]; then
            sysctl -n machdep.cpu.brand_string
            echo "CPU cores: $(sysctl -n hw.ncpu)"
        fi
        echo
        echo "Memory Information:"
        if [[ "$OSTYPE" == "linux-gnu"* ]]; then
            free -h
        elif [[ "$OSTYPE" == "darwin"* ]]; then
            echo "Total memory: $(sysctl -n hw.memsize | awk '{print $1/1024/1024/1024 " GB"}')"
        fi
    } > "${RESULTS_DIR}/system-info.txt"
    
    echo -e "${GREEN}System metrics collected${NC}"
}

# Function to generate summary report
generate_summary_report() {
    echo -e "${YELLOW}Generating summary report...${NC}"
    
    cat > "${RESULTS_DIR}/summary.md" <<EOF
# MCP Bridge Performance Benchmark Results

**Date**: $(date)
**Test Duration**: Various (see individual reports)

## Executive Summary

This report contains performance benchmark results for the MCP Bridge system.

### Test Scenarios

1. **k6 Load Test**: Simulated real user behavior with WebSocket connections
2. **Go Benchmarks**: Micro-benchmarks for critical code paths
3. **Vegeta Attack**: HTTP endpoint stress testing
4. **wrk Benchmark**: High-concurrency connection testing

### Key Metrics

EOF
    
    # Extract key metrics from k6 if available
    if [ -f "${RESULTS_DIR}/k6-summary.json" ]; then
        echo "#### k6 Results" >> "${RESULTS_DIR}/summary.md"
        echo '```' >> "${RESULTS_DIR}/summary.md"
        jq -r '.metrics | to_entries | .[] | "\(.key): \(.value.avg // .value.rate // .value.value)"' \
            "${RESULTS_DIR}/k6-summary.json" >> "${RESULTS_DIR}/summary.md" 2>/dev/null || true
        echo '```' >> "${RESULTS_DIR}/summary.md"
    fi
    
    # Add vegeta results if available
    if [ -f "${RESULTS_DIR}/vegeta-report.txt" ]; then
        echo -e "\n#### Vegeta Results" >> "${RESULTS_DIR}/summary.md"
        echo '```' >> "${RESULTS_DIR}/summary.md"
        cat "${RESULTS_DIR}/vegeta-report.txt" >> "${RESULTS_DIR}/summary.md"
        echo '```' >> "${RESULTS_DIR}/summary.md"
    fi
    
    echo -e "\n### Detailed Reports\n" >> "${RESULTS_DIR}/summary.md"
    echo "- [k6 Output](k6-output.log)" >> "${RESULTS_DIR}/summary.md"
    echo "- [Go Benchmarks](go-benchmark.txt)" >> "${RESULTS_DIR}/summary.md"
    echo "- [Vegeta Report](vegeta-report.txt)" >> "${RESULTS_DIR}/summary.md"
    echo "- [wrk Results](wrk-results.txt)" >> "${RESULTS_DIR}/summary.md"
    echo "- [System Info](system-info.txt)" >> "${RESULTS_DIR}/summary.md"
    
    echo -e "${GREEN}Summary report generated: ${RESULTS_DIR}/summary.md${NC}"
}

# Function to check if services are running
check_services() {
    echo -e "${YELLOW}Checking if services are running...${NC}"
    
    # Check gateway health
    if ! curl -k -s -f https://localhost:8443/health >/dev/null 2>&1; then
        echo -e "${RED}Gateway service is not running or not healthy${NC}"
        echo "Please start the gateway service before running benchmarks"
        return 1
    fi
    
    echo -e "${GREEN}Services are running${NC}"
    return 0
}

# Main execution
main() {
    echo "Starting benchmark suite..."
    echo
    
    # Check if services are running
    if ! check_services; then
        echo -e "${RED}Aborting benchmark due to service check failure${NC}"
        exit 1
    fi
    
    # Collect initial system metrics
    collect_system_metrics
    
    # Run benchmarks based on available tools
    run_k6_benchmark || echo -e "${YELLOW}Skipping k6 benchmarks${NC}"
    echo
    
    run_go_benchmarks || echo -e "${YELLOW}Skipping Go benchmarks${NC}"
    echo
    
    run_vegeta_benchmark || echo -e "${YELLOW}Skipping Vegeta benchmarks${NC}"
    echo
    
    run_wrk_benchmark || echo -e "${YELLOW}Skipping wrk benchmarks${NC}"
    echo
    
    # Generate summary report
    generate_summary_report
    
    echo
    echo -e "${GREEN}All benchmarks completed!${NC}"
    echo -e "${GREEN}Results saved to: ${RESULTS_DIR}${NC}"
    echo
    echo "To view the summary report:"
    echo "  cat ${RESULTS_DIR}/summary.md"
    echo
    echo "To analyze CPU profile:"
    echo "  go tool pprof ${RESULTS_DIR}/cpu.prof"
    echo
    echo "To analyze memory profile:"
    echo "  go tool pprof ${RESULTS_DIR}/mem.prof"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --gateway-url)
            export GATEWAY_URL="$2"
            shift 2
            ;;
        --auth-token)
            export AUTH_TOKEN="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  --gateway-url URL    WebSocket URL for gateway (default: wss://localhost:8443/ws)"
            echo "  --auth-token TOKEN   Authentication token (default: test-token)"
            echo "  --help               Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Run main function
main