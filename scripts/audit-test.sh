#!/bin/bash
# Test analysis for repository audit

set -e

# Get the directory where this script lives
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source shared helpers
source "${SCRIPT_DIR}/audit-helpers.sh"

# Function to parse JSON from test output
parse_json_from_test_output() {
    local test_output="$1"

    # Check prerequisites
    if ! command -v jq >/dev/null 2>&1; then
        echo "jq not available for JSON parsing" >&2
        return 1
    fi

    # Check if test output contains JSON data
    if [[ ! -s "${test_output}" ]]; then
        echo "Test output file is empty or missing" >&2
        return 1
    fi

    # Extract JSON lines from the test output
    local json_output="${test_output}.json"
    if ! grep "^{.*Action.*}" "${test_output}" > "${json_output}" 2>/dev/null; then
        echo "No JSON test data found in test output" >&2
        rm -f "${json_output}" 2>/dev/null
        return 1
    fi

    # Validate and parse the JSON output
    if validate_json_test_output "${json_output}" && parse_json_test_results "${json_output}"; then
        rm -f "${json_output}" 2>/dev/null
        return 0
    else
        rm -f "${json_output}" 2>/dev/null
        return 1
    fi
}

# Validate JSON test output
validate_json_test_output() {
    local json_file="$1"

    # Check if file contains valid JSON lines
    local valid_lines=0
    local total_lines=0

    while IFS= read -r line; do
        total_lines=$((total_lines + 1))
        if echo "${line}" | jq . >/dev/null 2>&1; then
            valid_lines=$((valid_lines + 1))
        fi

        # Only check first 100 lines for efficiency
        if [[ ${total_lines} -gt 100 ]]; then
            break
        fi
    done < "${json_file}"

    # Require at least 80% valid JSON lines
    if [[ ${total_lines} -gt 0 ]]; then
        local valid_percentage=$((valid_lines * 100 / total_lines))
        if [[ ${valid_percentage} -ge 80 ]]; then
            echo "JSON validation: ${valid_lines}/${total_lines} lines valid (${valid_percentage}%)" >&2
            return 0
        else
            echo "JSON validation failed: only ${valid_percentage}% valid lines" >&2
        fi
    fi

    return 1
}

# Parse JSON test results
parse_json_test_results() {
    local json_file="$1"

    # Use jq to parse test results with production-quality logic that handles pause/cont cycles
    echo "Parsing JSON test results with pause/cont awareness..." >&2

    # Count unique test executions that actually started (run or cont actions)
    local temp_tests=$(mktemp)
    local temp_results=$(mktemp)

    # Get all unique tests that had meaningful execution (run OR cont)
    jq -r 'select(has("Test") and (.Action == "run" or .Action == "cont")) | .Test' "${json_file}" 2>/dev/null | sort -u > "${temp_tests}"
    total_tests=$(wc -l < "${temp_tests}" | tr -d ' ')

    # Get all unique tests with final results
    jq -r 'select(has("Test") and (.Action == "pass" or .Action == "fail" or .Action == "skip")) | .Test' "${json_file}" 2>/dev/null | sort -u > "${temp_results}"

    # Count final results
    passed_tests=$(jq -r 'select(has("Test") and .Action == "pass") | .Test' "${json_file}" 2>/dev/null | sort -u | wc -l | tr -d ' ' || echo "0")
    failed_tests=$(jq -r 'select(has("Test") and .Action == "fail") | .Test' "${json_file}" 2>/dev/null | sort -u | wc -l | tr -d ' ' || echo "0")
    skipped_tests=$(jq -r 'select(has("Test") and .Action == "skip") | .Test' "${json_file}" 2>/dev/null | sort -u | wc -l | tr -d ' ' || echo "0")

    # Check for tests that executed but have no final status
    local incomplete_tests=$(mktemp)
    comm -23 "${temp_tests}" "${temp_results}" > "${incomplete_tests}"
    local incomplete_count=$(wc -l < "${incomplete_tests}" | tr -d ' ')

    rm -f "${temp_tests}" "${temp_results}"

    # Validate results make sense
    if [[ ${total_tests} -eq 0 ]]; then
        echo "No test executions found in JSON" >&2
        rm -f "${incomplete_tests}"
        return 1
    fi

    # Convert to numbers for safety
    total_tests=$((total_tests + 0))
    passed_tests=$((passed_tests + 0))
    failed_tests=$((failed_tests + 0))
    skipped_tests=$((skipped_tests + 0))
    incomplete_count=$((incomplete_count + 0))

    # Production-quality validation with pause/cont awareness
    if [[ ${incomplete_count} -gt 0 ]]; then
        echo "Found ${incomplete_count} tests that executed but have no final status:" >&2
        head -5 "${incomplete_tests}" >&2
        if [[ ${incomplete_count} -gt 5 ]]; then
            echo "... and $((incomplete_count - 5)) more" >&2
        fi
        echo "These might be paused tests that never continued, or tests interrupted during execution" >&2
    fi

    rm -f "${incomplete_tests}"

    echo "JSON parse results: ${total_tests} total, ${passed_tests} passed, ${failed_tests} failed, ${skipped_tests} skipped (${incomplete_count} incomplete)" >&2
    return 0
}

# Count tests with text fallback
count_tests_with_text_fallback() {
    local test_output="$1"

    echo "Using enhanced text-based fallback counting" >&2

    # Dynamic indentation detection for robustness
    local indent_levels
    indent_levels=$(grep -E "^[ ]*=== RUN" "${test_output}" 2>/dev/null | sed 's/=== RUN.*//' | sort -u | wc -l)

    if [[ ${indent_levels} -gt 3 ]]; then
        echo "Warning: Detected ${indent_levels} indentation levels, may miss some tests" >&2
    fi

    # Count with comprehensive patterns
    total_tests=$(grep -E "^[ ]*=== RUN" "${test_output}" 2>/dev/null | wc -l | tr -d ' \n' || echo "0")
    passed_tests=$(grep -E "^[ ]*--- PASS:" "${test_output}" 2>/dev/null | wc -l | tr -d ' \n' || echo "0")
    skipped_tests=$(grep -E "^[ ]*--- SKIP:" "${test_output}" 2>/dev/null | wc -l | tr -d ' \n' || echo "0")
    failed_tests=$(grep -E "^[ ]*--- FAIL:" "${test_output}" 2>/dev/null | wc -l | tr -d ' \n' || echo "0")

    # Convert to numbers for safety
    total_tests=$((total_tests + 0))
    passed_tests=$((passed_tests + 0))
    failed_tests=$((failed_tests + 0))
    skipped_tests=$((skipped_tests + 0))

    echo "Text-based count: ${total_tests} total, ${passed_tests} passed, ${failed_tests} failed, ${skipped_tests} skipped" >&2
}

# Discover total tests
discover_total_tests() {
    local total_discoverable=0

    # Get all Go modules
    local go_modules=()
    while IFS= read -r module_dir; do
        go_modules+=("$module_dir")
    done < <(discover_go_modules)

    # Discover tests in each module
    for module_dir in "${go_modules[@]}"; do
        # Skip modules that we know won't have tests or are problematic
        case "${module_dir}" in
            "./test/e2e/"*)
                # Skip E2E tests by default unless explicitly enabled
                if [[ "${RUN_E2E_TESTS}" != "true" ]]; then
                    continue
                fi
                ;;
            "./test/e2e/full_stack/test-mcp-server")
                # Skip incomplete modules
                continue
                ;;
        esac

        # Change to module directory and discover tests
        local current_dir=$(pwd)
        if cd "${module_dir}" 2>/dev/null; then
            local module_tests=$(go test -list=. ./... 2>/dev/null | grep -E "^(Test|Example|Benchmark)" | wc -l | tr -d ' \n' || echo "0")
            total_discoverable=$((total_discoverable + module_tests))
            cd "${current_dir}" 2>/dev/null || true
        fi
    done

    echo "${total_discoverable}"
}

# Function to run test analysis
run_test_analysis() {
    print_header "${ROCKET} REPOSITORY TEST ANALYSIS"

    local test_output="${AUDIT_DIR}/test-results-${TIMESTAMP}.txt"
    local test_summary="${AUDIT_DIR}/test-summary-${TIMESTAMP}.txt"

    print_info "Running all tests across entire repository (including Go workspace modules)..."

    # Check if this is a Go workspace
    if [[ -f "go.work" ]]; then
        print_info "Detected Go workspace - testing all modules comprehensively"
    fi

    # Find all Go modules
    local go_modules=()
    while IFS= read -r module_dir; do
        go_modules+=("$module_dir")
    done < <(discover_go_modules)

    print_info "Found ${#go_modules[@]} Go module(s) to test: ${go_modules[*]}"

    # Discover total available tests
    print_info "Discovering total available tests across all modules..."
    local total_discoverable_tests=$(discover_total_tests)
    print_info "Total discoverable tests: ${total_discoverable_tests}"

    # Run tests for each Go module
    {
        echo "=== COMPREHENSIVE GO MODULE TESTING ==="
        echo "Timestamp: $(date)"
        echo "Found modules: ${go_modules[*]}"
        echo ""

        for module_dir in "${go_modules[@]}"; do
            echo "=== TESTING MODULE: ${module_dir} ==="
            local module_name=$(basename "${module_dir}")
            if [[ "${module_dir}" == "." ]]; then
                module_name="root"
            fi

            # Use reasonable timeouts based on module complexity
            local module_timeout=300  # 5 minutes default
            case "${module_dir}" in
                "./services/router"*) module_timeout=600 ;; # 10 minutes for router
                "./services/"*) module_timeout=480 ;;       # 8 minutes for other services
                "./test/e2e/"*) module_timeout=420 ;;       # 7 minutes for E2E tests
                ".") module_timeout=540 ;;                  # 9 minutes for root
                *) module_timeout=240 ;;                    # 4 minutes for simple modules
            esac
            echo "Using ${module_timeout}s timeout for module: ${module_name}"

            # Check for known problematic modules and skip them
            local skip_reason=""
            case "${module_dir}" in
                "./test/e2e/"*)
                    # Skip E2E tests by default unless explicitly enabled
                    if [[ "${RUN_E2E_TESTS}" != "true" ]]; then
                        skip_reason="E2E tests skipped (set RUN_E2E_TESTS=true to enable)"
                    fi
                    ;;
                "./test/e2e/full_stack/test-mcp-server")
                    skip_reason="incomplete module (missing dependencies)"
                    ;;
            esac

            if [[ -n "$skip_reason" ]]; then
                echo "Module ${module_name}: SKIPPED (${skip_reason})"
                echo ""
                continue
            fi

            # Change to module directory and run tests
            local current_dir=$(pwd)
            if cd "${module_dir}"; then
                echo "Running tests in module: ${module_name}"
                local start_time=$(date +%s)

                # Use reasonable internal Go test timeouts
                local go_test_timeout="4m"   # 4 minute internal timeout default
                case "${module_dir}" in
                    "./services/router"*) go_test_timeout="8m" ;;
                    "./services/"*) go_test_timeout="6m" ;;
                    "./test/e2e/"*) go_test_timeout="5m" ;;
                    ".") go_test_timeout="7m" ;;
                    *) go_test_timeout="3m" ;;
                esac

                # Create a temp log file to capture output
                local temp_log=$(mktemp)

                # Run with JSON output for production-quality counting
                if timeout ${module_timeout}s go test --json ./... -timeout="${go_test_timeout}" > "${temp_log}" 2>&1; then
                    local end_time=$(date +%s)
                    local duration=$((end_time - start_time))
                    echo "Module ${module_name} tests: PASSED (${duration}s)"
                    # Show brief success summary
                    local total_tests=$(grep "=== RUN" "${temp_log}" 2>/dev/null | wc -l | tr -d ' \n' || echo "0")
                    local passed_tests=$(grep "--- PASS:" "${temp_log}" 2>/dev/null | wc -l | tr -d ' \n' || echo "0")
                    echo "  Tests run: ${total_tests}, passed: ${passed_tests}"
                else
                    local exit_code=$?
                    local end_time=$(date +%s)
                    local duration=$((end_time - start_time))

                    if [[ ${exit_code} -eq 124 ]]; then
                        echo "Module ${module_name} tests: TIMEOUT after ${duration}s"
                        echo "  Last output: $(tail -1 "${temp_log}" 2>/dev/null || echo 'no output')"
                    else
                        echo "Module ${module_name} tests: FAILED (${duration}s)"
                        local failed_count=$(grep "--- FAIL:" "${temp_log}" 2>/dev/null | wc -l | tr -d ' \n' || echo "0")
                        echo "  Failed tests: ${failed_count}"
                        if [[ ${failed_count} -gt 0 ]]; then
                            echo "  First failure: $(grep -m1 "--- FAIL:" "${temp_log}" 2>/dev/null || echo 'unknown')"
                        fi
                    fi
                fi

                # Output log content for detailed analysis
                if [[ -f "${temp_log}" ]]; then
                    echo "--- Test output for ${module_name} ---"
                    cat "${temp_log}"
                    echo "--- End test output for ${module_name} ---"
                    rm -f "${temp_log}"
                fi

                cd "${current_dir}"
            else
                echo "ERROR: Cannot access module directory: ${module_dir}"
            fi
            echo ""
        done

        echo "=== TESTING COMPLETE ==="
    } > "${test_output}" 2>&1

    # Determine test status based on output
    local test_status="PASS"
    local failed_tests=0
    if grep -q "^FAIL" "${test_output}"; then
        test_status="FAIL"
        failed_tests=$(grep "^--- FAIL:" "${test_output}" 2>/dev/null | wc -l | tr -d ' \n' || echo "0")
    fi

    # Production-quality JSON-based test counting
    local total_tests=0
    local passed_tests=0
    local skipped_tests=0
    local incomplete_count=0

    if parse_json_from_test_output "${test_output}"; then
        echo "Using production-quality JSON-based counting from main test run"
        # Calculate incomplete tests from the JSON parse output
        local tests_with_explicit_results=$((passed_tests + failed_tests + skipped_tests))
        incomplete_count=$((total_tests - tests_with_explicit_results))
        echo "Test counts: ${total_tests} total, ${passed_tests} passed, ${failed_tests} failed, ${skipped_tests} skipped, ${incomplete_count} incomplete"
    else
        echo "JSON parsing failed, using enhanced text-based fallback"
        count_tests_with_text_fallback "${test_output}"
        echo "Test counts: ${total_tests} total, ${passed_tests} passed, ${failed_tests} failed, ${skipped_tests} skipped"
    fi

    # Calculate test duration
    local total_duration="N/A"
    if command -v bc >/dev/null 2>&1; then
        total_duration=$(grep "^ok.*[0-9]\+\.[0-9]\+s" "${test_output}" | \
                        awk '{print $(NF-0)}' | sed 's/s$//' | \
                        awk '{sum += $1} END {print sum}' | bc -l 2>/dev/null || echo "0")
    fi

    # Calculate tests with explicit results
    local tests_with_explicit_results=$((passed_tests + failed_tests + skipped_tests))
    local unaccounted_tests=$((total_tests - tests_with_explicit_results))

    # Use actual counted results
    local effective_passed_tests=${passed_tests}
    local effective_failed_tests=${failed_tests}

    # Generate test summary
    {
        echo "=== REPOSITORY TEST SUMMARY ==="
        echo "Timestamp: $(date)"
        echo "Status: ${test_status}"
        echo "Total Tests Executed: ${total_tests}"
        echo "Passed: ${effective_passed_tests}"
        echo "Failed: ${effective_failed_tests}"
        echo "Skipped: ${skipped_tests}"
        if [[ ${unaccounted_tests} -gt 0 ]]; then
            echo "Tests without explicit status: ${unaccounted_tests}"
        fi
        echo "Test Functions Discovered: ${total_discoverable_tests}"
        echo "Duration: ${total_duration}s"
        echo ""

        if [[ ${failed_tests} -gt 0 ]]; then
            echo "=== FAILED TESTS ==="
            grep -A 3 "^--- FAIL:" "${test_output}" 2>/dev/null || echo "No failed tests details found"
            echo ""

            echo "=== FAILED PACKAGES ==="
            grep "^FAIL.*github.com" "${test_output}" 2>/dev/null || echo "No failed packages found"
        fi

        echo ""
        echo "=== PACKAGE TEST SUMMARY ==="
        grep "^ok\|^FAIL" "${test_output}" | head -20
    } > "${test_summary}"

    # Display results
    if [[ ${test_status} == "PASS" ]]; then
        print_success "Tests: ${effective_passed_tests}/${total_tests} passed (${effective_failed_tests} failed, ${skipped_tests} skipped)"
        if [[ ${incomplete_count} -gt 0 ]]; then
            print_info "Note: ${incomplete_count} tests completed without explicit status (counted as passed)"
        fi
        print_info "Executed ${total_tests} tests from ${total_discoverable_tests} discoverable test functions"
    else
        print_error "Tests: ${effective_passed_tests}/${total_tests} passed (${effective_failed_tests} failed, ${skipped_tests} skipped)"
        if [[ ${incomplete_count} -gt 0 ]]; then
            print_warning "${incomplete_count} tests completed without explicit status (status unknown)"
        fi
        print_info "Executed ${total_tests} tests from ${total_discoverable_tests} discoverable test functions"
        if [[ ${failed_tests} -gt 0 ]]; then
            print_info "Failed tests:"
            grep "^--- FAIL:" "${test_output}" | head -5 | while read -r line; do
                echo "  ${line}"
            done
        fi
    fi

    print_info "Detailed results saved to: ${test_output}"
    print_info "Summary saved to: ${test_summary}"

    return ${failed_tests}
}

# Main execution
main() {
    # Create audit directory
    create_audit_dir

    # Run test analysis
    if run_test_analysis; then
        exit 0
    else
        exit $?
    fi
}

# Run main function
main "$@"
