#!/bin/bash

# Fuzzing Test Runner for MCP Protocol Handlers
# This script runs various fuzzing tests to find edge cases and vulnerabilities

set -e

# Configuration
FUZZ_TIME="${FUZZ_TIME:-60s}"  # Default 60 seconds per fuzz test
FUZZ_DIR="./fuzz-corpus"
RESULTS_DIR="./fuzz-results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Create directories
mkdir -p "$FUZZ_DIR" "$RESULTS_DIR/$TIMESTAMP"

echo -e "${BLUE}MCP Protocol Fuzzing Test Suite${NC}"
echo "================================"
echo "Fuzz time per test: $FUZZ_TIME"
echo "Results will be saved to: $RESULTS_DIR/$TIMESTAMP"
echo

# Function to run a fuzz test
run_fuzz_test() {
    local test_name=$1
    local test_func=$2
    
    echo -e "${YELLOW}Running $test_name...${NC}"
    
    # Create corpus directory for this test
    mkdir -p "$FUZZ_DIR/$test_func"
    
    # Run the fuzz test
    if go test -fuzz="$test_func" -fuzztime="$FUZZ_TIME" \
        -test.fuzzcachedir="$FUZZ_DIR/$test_func" \
        ./... \
        > "$RESULTS_DIR/$TIMESTAMP/${test_name}.log" 2>&1; then
        echo -e "${GREEN}✓ $test_name completed successfully${NC}"
        
        # Check for any crashes found
        if grep -q "failing input written to" "$RESULTS_DIR/$TIMESTAMP/${test_name}.log"; then
            echo -e "${RED}  ⚠ Found crashing inputs! Check the log for details.${NC}"
            
            # Extract crash information
            grep "failing input written to" "$RESULTS_DIR/$TIMESTAMP/${test_name}.log" | \
                sed 's/^/    /' >> "$RESULTS_DIR/$TIMESTAMP/crashes.txt"
        fi
    else
        echo -e "${RED}✗ $test_name failed${NC}"
        
        # Show last few lines of error
        echo "  Error output:"
        tail -n 10 "$RESULTS_DIR/$TIMESTAMP/${test_name}.log" | sed 's/^/    /'
    fi
    
    echo
}

# Function to analyze fuzzing results
analyze_results() {
    echo -e "${BLUE}Analyzing fuzzing results...${NC}"
    echo
    
    # Count total executions
    total_execs=0
    for log in "$RESULTS_DIR/$TIMESTAMP"/*.log; do
        if [ -f "$log" ]; then
            execs=$(grep -oE 'execs: [0-9]+' "$log" | tail -1 | awk '{print $2}')
            if [ -n "$execs" ]; then
                total_execs=$((total_execs + execs))
            fi
        fi
    done
    
    echo "Total fuzzing executions: $total_execs"
    
    # Check for crashes
    if [ -f "$RESULTS_DIR/$TIMESTAMP/crashes.txt" ]; then
        crash_count=$(wc -l < "$RESULTS_DIR/$TIMESTAMP/crashes.txt")
        echo -e "${RED}Found $crash_count crashes!${NC}"
        echo "Crash details saved to: $RESULTS_DIR/$TIMESTAMP/crashes.txt"
    else
        echo -e "${GREEN}No crashes found${NC}"
    fi
    
    # Generate summary report
    cat > "$RESULTS_DIR/$TIMESTAMP/summary.txt" << EOF
MCP Protocol Fuzzing Summary
===========================
Date: $(date)
Fuzz Duration: $FUZZ_TIME per test
Total Executions: $total_execs

Test Results:
EOF
    
    for log in "$RESULTS_DIR/$TIMESTAMP"/*.log; do
        if [ -f "$log" ]; then
            test_name=$(basename "$log" .log)
            if grep -q "PASS" "$log"; then
                echo "✓ $test_name: PASSED" >> "$RESULTS_DIR/$TIMESTAMP/summary.txt"
            else
                echo "✗ $test_name: FAILED" >> "$RESULTS_DIR/$TIMESTAMP/summary.txt"
            fi
        fi
    done
    
    echo
    echo "Summary saved to: $RESULTS_DIR/$TIMESTAMP/summary.txt"
}

# Function to run continuous fuzzing
continuous_fuzz() {
    local duration=$1
    echo -e "${BLUE}Running continuous fuzzing for $duration...${NC}"
    
    # Run all fuzz tests in parallel with longer duration
    parallel_fuzz() {
        local test_func=$1
        FUZZ_TIME=$duration run_fuzz_test "continuous_$test_func" "$test_func" &
    }
    
    # Start all fuzz tests in parallel
    parallel_fuzz "FuzzWebSocketProtocol"
    parallel_fuzz "FuzzBinaryProtocol"
    parallel_fuzz "FuzzProtocolValidation"
    parallel_fuzz "FuzzMessageRouting"
    
    # Wait for all to complete
    wait
    
    echo -e "${GREEN}Continuous fuzzing completed${NC}"
}

# Main menu
echo "Select fuzzing mode:"
echo "1) Quick fuzz (1 minute per test)"
echo "2) Standard fuzz (5 minutes per test)"
echo "3) Extended fuzz (30 minutes per test)"
echo "4) Continuous fuzz (custom duration)"
echo "5) Reproduce crash (provide crash file)"
read -p "Enter your choice (1-5): " choice

case $choice in
    1)
        FUZZ_TIME="1m"
        ;;
    2)
        FUZZ_TIME="5m"
        ;;
    3)
        FUZZ_TIME="30m"
        ;;
    4)
        read -p "Enter duration (e.g., 2h, 6h, 24h): " custom_duration
        continuous_fuzz "$custom_duration"
        analyze_results
        exit 0
        ;;
    5)
        read -p "Enter path to crash file: " crash_file
        if [ -f "$crash_file" ]; then
            echo -e "${YELLOW}Reproducing crash...${NC}"
            go test -run=Fuzz -v . < "$crash_file"
        else
            echo -e "${RED}Crash file not found${NC}"
        fi
        exit 0
        ;;
    *)
        echo -e "${RED}Invalid choice${NC}"
        exit 1
        ;;
esac

# Run the fuzzing tests
echo -e "${BLUE}Starting fuzzing tests...${NC}"
echo

# Run each fuzz test
run_fuzz_test "websocket_protocol_fuzzing" "FuzzWebSocketProtocol"
run_fuzz_test "binary_protocol_fuzzing" "FuzzBinaryProtocol"
run_fuzz_test "protocol_validation_fuzzing" "FuzzProtocolValidation"
run_fuzz_test "message_routing_fuzzing" "FuzzMessageRouting"

# Analyze results
analyze_results

# Optional: Archive corpus for future use
echo
read -p "Archive fuzzing corpus for future use? (y/n): " archive_choice
if [[ "$archive_choice" == "y" ]]; then
    archive_name="fuzz-corpus-$TIMESTAMP.tar.gz"
    tar -czf "$archive_name" "$FUZZ_DIR"
    echo -e "${GREEN}Corpus archived to: $archive_name${NC}"
fi

echo
echo -e "${BLUE}Fuzzing complete!${NC}"
echo "Results saved to: $RESULTS_DIR/$TIMESTAMP/"

# Show instructions for crash reproduction
if [ -f "$RESULTS_DIR/$TIMESTAMP/crashes.txt" ]; then
    echo
    echo -e "${YELLOW}To reproduce crashes:${NC}"
    echo "1. Check crash files listed in: $RESULTS_DIR/$TIMESTAMP/crashes.txt"
    echo "2. Run: go test -run=FuzzTestName/CrashID"
    echo "   Example: go test -run=FuzzWebSocketProtocol/5a7d8f9e"
fi