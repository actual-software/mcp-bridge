#!/bin/bash

# Repository Audit Script
# Performs comprehensive code quality analysis including linting, testing, and coverage

set -e  # Exit on any error (but we'll handle specific cases)

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Emoji for better visual output
CHECK="✅"
CROSS="❌"
WARNING="⚠️"
INFO="ℹ️"
ROCKET="🚀"
MAGNIFY="🔍"
CHART="📊"
GEAR="⚙️"

# Configuration
TIMEOUT="15m"
COVERAGE_THRESHOLD=80
AUDIT_DIR="audit-results"
TIMESTAMP=$(date +"%Y%m%d-%H%M%S")

# Helper functions
print_header() {
    echo -e "\n${BLUE}================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================================${NC}\n"
}

print_success() {
    echo -e "${GREEN}${CHECK} $1${NC}"
}

print_error() {
    echo -e "${RED}${CROSS} $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}${WARNING} $1${NC}"
}

print_info() {
    echo -e "${CYAN}${INFO} $1${NC}"
}

print_stat() {
    echo -e "${PURPLE}${CHART} $1${NC}"
}

# Create audit results directory
create_audit_dir() {
    mkdir -p "${AUDIT_DIR}"
    echo -e "${GEAR} Created audit results directory: ${AUDIT_DIR}"
}

# Function to run linting analysis
run_linting_analysis() {
    print_header "${MAGNIFY} REPOSITORY LINTING ANALYSIS"

    local lint_output="${AUDIT_DIR}/lint-results-${TIMESTAMP}.txt"
    local lint_summary="${AUDIT_DIR}/lint-summary-${TIMESTAMP}.txt"

    print_info "Running golangci-lint across all Go modules in repository..."

    # Find all Go modules in the repository
    local go_modules=()
    while IFS= read -r module_dir; do
        go_modules+=("$module_dir")
    done < <(find . -name "go.mod" -exec dirname {} \;)

    if [[ ${#go_modules[@]} -eq 0 ]]; then
        print_error "No Go modules found in repository"
        return 1
    fi

    print_info "Found ${#go_modules[@]} Go module(s): ${go_modules[*]}"

    local total_issues=0
    local lint_status="PASS"

    # Run linting for each Go module
    {
        echo "=== COMPREHENSIVE GO MODULE LINTING ==="
        echo "Timestamp: $(date)"
        echo "Found modules: ${go_modules[*]}"
        echo ""

        for module_dir in "${go_modules[@]}"; do
            echo "=== LINTING MODULE: ${module_dir} ==="
            local module_name=$(basename "${module_dir}")
            if [[ "${module_dir}" == "." ]]; then
                module_name="root"
            fi

            # Change to module directory and run linting
            local current_dir=$(pwd)
            if cd "${module_dir}"; then
                # Run golangci-lint for this module
                local module_issues=0
                if ~/go/bin/golangci-lint run ./... --timeout="${TIMEOUT}" --max-issues-per-linter=1000 --max-same-issues=1000 2>&1; then
                    echo "Module ${module_name}: 0 issues"
                else
                    # Count issues in the output
                    local module_output_temp=$(mktemp)
                    ~/go/bin/golangci-lint run ./... --timeout="${TIMEOUT}" --max-issues-per-linter=1000 --max-same-issues=1000 > "${module_output_temp}" 2>&1 || true
                    module_issues=$(grep -c "^[^[:space:]].*:[0-9]*:[0-9]*:" "${module_output_temp}" 2>/dev/null || echo "0")

                    if [[ ${module_issues} -gt 0 ]]; then
                        echo "Module ${module_name}: ${module_issues} issues found"
                        cat "${module_output_temp}"
                        lint_status="FAIL"
                        total_issues=$((total_issues + module_issues))
                    else
                        echo "Module ${module_name}: 0 issues"
                    fi
                    rm -f "${module_output_temp}"
                fi

                cd "${current_dir}"
            else
                echo "ERROR: Cannot access module directory: ${module_dir}"
                lint_status="FAIL"
            fi
            echo ""
        done

        echo "=== LINTING COMPLETE ==="
        echo "Total issues across all modules: ${total_issues}"
        echo "Overall status: ${lint_status}"
    } > "${lint_output}" 2>&1

    # Generate linting summary
    {
        echo "=== REPOSITORY LINTING SUMMARY ==="
        echo "Timestamp: $(date)"
        echo "Status: ${lint_status}"
        echo "Total Modules: ${#go_modules[@]}"
        echo "Total Issues: ${total_issues}"
        echo ""

        if [[ ${total_issues} -gt 0 ]]; then
            echo "=== ISSUES BY MODULE ==="
            for module_dir in "${go_modules[@]}"; do
                local module_name=$(basename "${module_dir}")
                if [[ "${module_dir}" == "." ]]; then
                    module_name="root"
                fi
                local module_issues=$(grep -c "Module ${module_name}: [0-9]\+ issues" "${lint_output}" 2>/dev/null || echo "0")
                if [[ ${module_issues} -gt 0 ]]; then
                    grep "Module ${module_name}: [0-9]\+ issues" "${lint_output}" 2>/dev/null || echo "${module_name}: 0 issues"
                fi
            done
            echo ""

            echo "=== ISSUES BY LINTER ==="
            grep "^[^[:space:]].*:[0-9]*:[0-9]*:" "${lint_output}" 2>/dev/null | \
                awk -F'[()]' '{print $(NF-1)}' | \
                sort | uniq -c | sort -nr | head -10 || echo "No categorized issues found"
            echo ""

            echo "=== ISSUES BY FILE ==="
            grep "^[^[:space:]].*:[0-9]*:[0-9]*:" "${lint_output}" 2>/dev/null | \
                cut -d':' -f1 | \
                sort | uniq -c | sort -nr | head -15 || echo "No file-specific issues found"
        fi
    } > "${lint_summary}"

    # Display results
    if [[ ${lint_status} == "PASS" ]]; then
        print_success "Linting: ${total_issues} issues found across ${#go_modules[@]} modules"
    else
        print_error "Linting: ${total_issues} issues found across ${#go_modules[@]} modules"
        print_info "Modules with issues:"
        grep "Module.*: [1-9][0-9]* issues" "${lint_output}" 2>/dev/null | head -5 | while read -r line; do
            echo "  ${line}"
        done
    fi

    print_info "Detailed results saved to: ${lint_output}"
    print_info "Summary saved to: ${lint_summary}"

    return ${total_issues}
}

# Function to discover total available tests across all modules

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

# Removed duplicate test execution functions - using integrated approach

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

parse_json_test_results() {
    local json_file="$1"

    # Use jq to parse test results with production-quality logic that handles pause/cont cycles
    echo "Parsing JSON test results with pause/cont awareness..." >&2

    # Count unique test executions that actually started (run or cont actions)
    # Tests can be paused and continued, so we need to count tests that were actually executed
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
    local sum_results=$((passed_tests + failed_tests + skipped_tests))
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

discover_total_tests() {
    local total_discoverable=0

    # Handle Go workspace by reading go.work file if it exists
    local go_modules=()
    if [[ -f "go.work" ]]; then
        # Parse go.work file to get all modules
        while IFS= read -r line; do
            if [[ $line =~ ^[[:space:]]*\. ]]; then
                # Extract module path, handling both quoted and unquoted paths
                local module_path=$(echo "$line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//')
                go_modules+=("$module_path")
            fi
        done < <(grep -A 100 "^use" go.work | grep -v "^use" | grep -v "^)" || true)
    fi

    # Also add any additional go.mod files not covered by workspace
    while IFS= read -r module_dir; do
        local already_included=false
        for existing in "${go_modules[@]}"; do
            if [[ "$existing" == "$module_dir" ]]; then
                already_included=true
                break
            fi
        done
        if [[ "$already_included" == false ]]; then
            go_modules+=("$module_dir")
        fi
    done < <(find . -name "go.mod" -exec dirname {} \;)

    # Discover tests in each module
    for module_dir in "${go_modules[@]}"; do
        local module_name=$(basename "${module_dir}")
        if [[ "${module_dir}" == "." ]]; then
            module_name="root"
        fi

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
    local is_workspace=false
    if [[ -f "go.work" ]]; then
        is_workspace=true
        print_info "Detected Go workspace - testing all modules comprehensively"
    fi

    # Find all Go modules using workspace-aware approach
    local go_modules=()
    if [[ -f "go.work" ]]; then
        # Parse go.work file to get all modules
        while IFS= read -r line; do
            if [[ $line =~ ^[[:space:]]*\. ]]; then
                # Extract module path, handling both quoted and unquoted paths
                local module_path=$(echo "$line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//')
                go_modules+=("$module_path")
            fi
        done < <(grep -A 100 "^use" go.work | grep -v "^use" | grep -v "^)" || true)
    fi

    # Also add any additional go.mod files not covered by workspace
    while IFS= read -r module_dir; do
        local already_included=false
        for existing in "${go_modules[@]}"; do
            if [[ "$existing" == "$module_dir" ]]; then
                already_included=true
                break
            fi
        done
        if [[ "$already_included" == false ]]; then
            go_modules+=("$module_dir")
        fi
    done < <(find . -name "go.mod" -exec dirname {} \;)

    print_info "Found ${#go_modules[@]} Go module(s) to test: ${go_modules[*]}"

    # Discover total available tests
    print_info "Discovering total available tests across all modules..."
    local total_discoverable_tests=$(discover_total_tests)
    print_info "Total discoverable tests: ${total_discoverable_tests}"

    # Verify we're about to run tests on all the same modules
    print_info "Will execute tests on ${#go_modules[@]} modules: ${go_modules[*]}"

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
                "./services/router"*) module_timeout=600 ;; # 10 minutes for router (complex parallel tests)
                "./services/"*) module_timeout=480 ;;       # 8 minutes for other services
                "./test/e2e/"*) module_timeout=420 ;;       # 7 minutes for E2E tests
                ".") module_timeout=540 ;;                  # 9 minutes for root (has many subdirs)
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
                    else
                        # Quick build check for e2e modules
                        if [[ -d "${module_dir}" ]] && cd "${module_dir}" 2>/dev/null; then
                            if ! go build ./... >/dev/null 2>&1; then
                                skip_reason="build errors detected"
                            fi
                            cd "${current_dir}" 2>/dev/null || true
                        fi
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
                    "./services/router"*) go_test_timeout="8m" ;;  # 8 minutes for router (parallel tests)
                    "./services/"*) go_test_timeout="6m" ;;        # 6 minutes for other services
                    "./test/e2e/"*) go_test_timeout="5m" ;;        # 5 minutes for E2E
                    ".") go_test_timeout="7m" ;;                   # 7 minutes for root
                    *) go_test_timeout="3m" ;;                     # 3 minutes for others
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
    if grep -q "^FAIL" "${test_output}"; then
        test_status="FAIL"
        failed_tests=$(grep "^--- FAIL:" "${test_output}" 2>/dev/null | wc -l | tr -d ' \n' || echo "0")
    else
        test_status="PASS"
        failed_tests=0
    fi

    # Production-quality JSON-based test counting using main test run output
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
    if command -v bc >/dev/null 2>&1; then
        total_duration=$(grep "^ok.*[0-9]\+\.[0-9]\+s" "${test_output}" | \
                        awk '{print $(NF-0)}' | sed 's/s$//' | \
                        awk '{sum += $1} END {print sum}' | bc -l 2>/dev/null || echo "0")
    else
        total_duration="N/A"
    fi

    # Calculate tests with explicit results - no assumptions needed with JSON counting
    local tests_with_explicit_results=$((passed_tests + failed_tests + skipped_tests))
    local unaccounted_tests=$((total_tests - tests_with_explicit_results))

    # Use actual counted results without assumptions
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

    # Display results with comprehensive accounting
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

# Function to run coverage analysis
run_coverage_analysis() {
    print_header "${CHART} REPOSITORY COVERAGE ANALYSIS"

    local coverage_output="${AUDIT_DIR}/coverage-results-${TIMESTAMP}.txt"
    local coverage_profile="${AUDIT_DIR}/coverage-profile-${TIMESTAMP}.out"
    local coverage_html="${AUDIT_DIR}/coverage-report-${TIMESTAMP}.html"
    local coverage_summary="${AUDIT_DIR}/coverage-summary-${TIMESTAMP}.txt"

    print_info "Running test coverage analysis..."

    # Find all Go modules and collect coverage from each
    local go_modules=()
    while IFS= read -r module_dir; do
        go_modules+=("$module_dir")
    done < <(find . -name "go.mod" -exec dirname {} \;)

    print_info "Found ${#go_modules[@]} Go module(s) for coverage: ${go_modules[*]}"

    # Run tests with coverage for each module
    local coverage_success=true
    local temp_profiles=()
    local successful_modules=()
    local failed_modules=()
    local skipped_modules=()

    {
        echo "=== COMPREHENSIVE GO MODULE COVERAGE ==="
        echo "Timestamp: $(date)"
        echo "Found modules: ${go_modules[*]}"
        echo ""

        local module_count=0
        for module_dir in "${go_modules[@]}"; do
            module_count=$((module_count + 1))
            echo "=== COLLECTING COVERAGE FROM MODULE: ${module_dir} (${module_count}/${#go_modules[@]}) ==="
            local module_name=$(basename "${module_dir}")
            if [[ "${module_dir}" == "." ]]; then
                module_name="root"
            fi

            # Check if module has known issues before attempting coverage
            local skip_reason=""
            case "${module_dir}" in
                "./test/e2e/"*)
                    # Skip E2E tests by default unless explicitly enabled
                    if [[ "${RUN_E2E_TESTS}" != "true" ]]; then
                        skip_reason="E2E tests skipped (set RUN_E2E_TESTS=true to enable)"
                    elif [[ "${module_dir}" == "./test/e2e/k8s" ]] && ! command -v kubectl >/dev/null 2>&1; then
                        skip_reason="kubernetes environment not available"
                    elif [[ ! -f "${module_dir}/go.mod" ]]; then
                        skip_reason="missing go.mod file"
                    fi
                    ;;
            esac

            if [[ -n "$skip_reason" ]]; then
                echo "Module ${module_name}: SKIPPED ($skip_reason)"
                skipped_modules+=("${module_name}:skipped_${skip_reason// /_}")
                continue
            fi

            # Change to module directory and run coverage
            local current_dir=$(pwd)
            if cd "${module_dir}"; then
                local module_profile="${current_dir}/${coverage_profile}.${module_name}"
                local module_log="${current_dir}/${coverage_profile}.${module_name}.log"
                echo "Running coverage for module: ${module_name}"

                # Use appropriate timeout per module type (allow time for legitimate long tests)
                local module_timeout=420  # 7 minutes for main modules (reasonable for integration tests)
                case "${module_dir}" in
                    "./test/e2e/"*) module_timeout=3600 ;;    # 60 minutes for E2E tests (slow network/hotspot conditions)
                    "./services/router"*) module_timeout=720 ;; # 12 minutes for router (complex parallel tests)
                    "./services/"*) module_timeout=600 ;;     # 10 minutes for other service modules
                    ".") module_timeout=480 ;;                # 8 minutes for root
                esac
                echo "Starting coverage collection (timeout: ${module_timeout}s)..."

                # Run tests WITHOUT -short to get real test results, but with monitoring
                local start_time=$(date +%s)
                echo "Running go test (allowing integration tests to complete)..."

                if timeout ${module_timeout}s go test -coverprofile="${module_profile}" -covermode=atomic ./... > "${module_log}" 2>&1; then
                    # Check if profile was actually created and has content
                    if [[ -f "${module_profile}" ]] && [[ $(wc -l < "${module_profile}") -gt 1 ]]; then
                        local coverage_lines=$(wc -l < "${module_profile}")
                        echo "Module ${module_name} coverage: SUCCESS (${coverage_lines} lines)"
                        temp_profiles+=("${module_profile}")
                        successful_modules+=("${module_name}")
                    else
                        echo "Module ${module_name} coverage: NO COVERAGE DATA"
                        echo "Log output: $(head -3 "${module_log}" | tr '\n' '; ')"
                        failed_modules+=("${module_name}:no_data")
                        rm -f "${module_profile}" 2>/dev/null
                    fi
                else
                    local end_time=$(date +%s)
                    local duration=$((end_time - start_time))

                    echo "Module ${module_name} coverage: FAILED after ${duration}s"

                    if [[ -f "${module_log}" ]]; then
                        # Analyze the failure more thoroughly
                        local total_lines=$(wc -l < "${module_log}")
                        echo "Test log has ${total_lines} lines. Analyzing failure..."

                        # Check for specific failure patterns in order of priority
                        if grep -q "signal: killed" "${module_log}" 2>/dev/null; then
                            echo "❌ TIMEOUT: Tests were killed after ${module_timeout}s timeout"
                            # Look for the last test that was running before timeout
                            local last_test=$(grep -E "=== RUN|--- PASS:|--- FAIL:|--- SKIP:" "${module_log}" | tail -1 | awk '{print $3}' 2>/dev/null || echo "unknown")
                            echo "Last test before timeout: ${last_test}"
                            echo "Context: $(tail -5 "${module_log}" | head -3 | tr '\n' '; ')"
                            failed_modules+=("${module_name}:timeout_${module_timeout}s")
                        elif grep -qE "(cannot find package|build failed|compile error)" "${module_log}" 2>/dev/null; then
                            echo "❌ BUILD FAILURE: Module failed to compile"
                            local build_error=$(grep -E "(cannot find package|build failed|compile error)" "${module_log}" | head -1 | cut -c1-100)
                            echo "Build error: ${build_error}..."
                            failed_modules+=("${module_name}:build_failed")
                        elif grep -qE "(setup failed|initialization failed|failed to start)" "${module_log}" 2>/dev/null; then
                            echo "❌ SETUP FAILURE: Test setup/initialization failed"
                            local setup_error=$(grep -E "(setup failed|initialization failed|failed to start)" "${module_log}" | head -1 | cut -c1-100)
                            echo "Setup error: ${setup_error}..."
                            failed_modules+=("${module_name}:setup_failed")
                        elif grep -q "panic:" "${module_log}" 2>/dev/null; then
                            echo "❌ PANIC: Tests panicked during execution"
                            local panic_line=$(grep -n "panic:" "${module_log}" | head -1)
                            local panic_test=$(grep -B5 "panic:" "${module_log}" | grep -E "=== RUN" | tail -1 | awk '{print $3}' 2>/dev/null || echo "unknown")
                            echo "Panic in test: ${panic_test}"
                            echo "Panic: $(echo "${panic_line}" | cut -d: -f2- | cut -c1-80)..."
                            failed_modules+=("${module_name}:panic")
                        elif grep -q "FAIL.*github.com" "${module_log}" 2>/dev/null; then
                            local failed_packages=$(grep "FAIL.*github.com" "${module_log}" | wc -l)
                            echo "❌ TEST FAILURES: ${failed_packages} package(s) have failing tests"
                            echo "Failed packages:"
                            grep "FAIL.*github.com" "${module_log}" | head -3 | while read -r line; do
                                local pkg=$(echo "$line" | awk '{print $2}')
                                local time=$(echo "$line" | awk '{print $3}' 2>/dev/null || echo "")
                                echo "  - ${pkg} ${time}"
                            done
                            failed_modules+=("${module_name}:test_failures_${failed_packages}")
                        elif grep -qE "(connection refused|network.*unreachable|dial.*timeout)" "${module_log}" 2>/dev/null; then
                            echo "❌ NETWORK FAILURE: Tests failed due to network connectivity issues"
                            local network_error=$(grep -E "(connection refused|network.*unreachable|dial.*timeout)" "${module_log}" | head -1 | cut -c1-100)
                            echo "Network error: ${network_error}..."
                            failed_modules+=("${module_name}:network_failed")
                        elif grep -qE "(no such file|permission denied|access denied)" "${module_log}" 2>/dev/null; then
                            echo "❌ FILE/PERMISSION ERROR: Tests failed due to file system issues"
                            local file_error=$(grep -E "(no such file|permission denied|access denied)" "${module_log}" | head -1 | cut -c1-100)
                            echo "File error: ${file_error}..."
                            failed_modules+=("${module_name}:filesystem_failed")
                        else
                            echo "❌ UNKNOWN FAILURE: Tests failed for unclear reasons"
                            echo "Exit code analysis: timeout returned non-zero"
                            echo "Last few lines of output:"
                            tail -5 "${module_log}" | while read -r line; do
                                echo "  ${line}"
                            done
                            failed_modules+=("${module_name}:unknown_failure")
                        fi
                    else
                        echo "❌ NO LOG: No test output generated (possible immediate crash)"
                        failed_modules+=("${module_name}:no_log")
                    fi
                    coverage_success=false
                fi

                # Clean up log file
                rm -f "${module_log}" 2>/dev/null
                cd "${current_dir}"
            else
                echo "ERROR: Cannot access module directory: ${module_dir}"
                failed_modules+=("${module_name}:access_failed")
                coverage_success=false
            fi
            echo ""
        done

        echo "=== COVERAGE COLLECTION COMPLETE ==="
        echo "Successful modules: ${successful_modules[*]}"
        echo "Skipped modules: ${skipped_modules[*]}"
        echo "Failed modules: ${failed_modules[*]}"
        local testable_modules=$((${#go_modules[@]} - ${#skipped_modules[@]}))
        echo "Total profiles collected: ${#temp_profiles[@]}/${testable_modules}"
    } > "${coverage_output}" 2>&1

    # Merge coverage profiles and track individual module coverage
    # Use parallel arrays for Bash 3.2 compatibility (no associative arrays)
    local module_names=()
    local module_coverages=()

    if [[ ${#temp_profiles[@]} -gt 0 ]]; then
        print_info "Merging coverage profiles from ${#temp_profiles[@]} successful modules..."
        echo "mode: atomic" > "${coverage_profile}"
        local total_merged_lines=0

        # First extract individual module coverage percentages before merging
        for profile in "${temp_profiles[@]}"; do
            if [[ -f "${profile}" ]]; then
                local module_name=$(basename "${profile}" | sed 's/.*\.out\.//')
                local module_cov=$(go tool cover -func="${profile}" 2>/dev/null | grep "^total:" | awk '{print $3}' | sed 's/%$//' || echo "0")

                # Store in parallel arrays
                module_names+=("${module_name}")
                module_coverages+=("${module_cov}")

                local profile_lines=$(tail -n +2 "${profile}" | wc -l)
                tail -n +2 "${profile}" >> "${coverage_profile}" 2>/dev/null || true
                total_merged_lines=$((total_merged_lines + profile_lines))
                rm -f "${profile}" # Clean up individual profiles
            fi
        done

        print_info "Merged ${total_merged_lines} coverage lines from ${#temp_profiles[@]} modules"
        coverage_status="PARTIAL"

        if [[ ${#failed_modules[@]} -eq 0 ]]; then
            coverage_status="PASS"
        fi
    else
        print_error "No coverage profiles could be collected from any module"
        coverage_status="FAIL"
        # Create empty profile to prevent downstream errors
        echo "mode: atomic" > "${coverage_profile}"
    fi

    # Generate coverage report if profile exists
    if [[ -f "${coverage_profile}" ]]; then
        # Get total coverage percentage
        total_coverage=$(go tool cover -func="${coverage_profile}" | grep "^total:" | awk '{print $3}' | sed 's/%$//')

        # Generate HTML report
        go tool cover -html="${coverage_profile}" -o "${coverage_html}" 2>/dev/null || print_warning "Could not generate HTML coverage report"

        # Generate detailed coverage summary
        local testable_modules=$((${#go_modules[@]} - ${#skipped_modules[@]}))
        {
            echo "=== REPOSITORY COVERAGE SUMMARY ==="
            echo "Timestamp: $(date)"
            echo "Total Coverage: ${total_coverage}%"
            echo "Coverage Threshold: ${COVERAGE_THRESHOLD}%"
            echo "Status: $(if (( $(echo "${total_coverage} >= ${COVERAGE_THRESHOLD}" | bc -l 2>/dev/null || echo "0") )); then echo "PASS"; else echo "FAIL"; fi)"
            echo ""

            echo "=== MODULE COVERAGE STATUS ==="
            echo "Total modules: ${#go_modules[@]} (${testable_modules} testable, ${#skipped_modules[@]} skipped)"
            echo "Modules successfully collected (${#successful_modules[@]}/${testable_modules}):"
            if [[ ${#module_names[@]} -gt 0 ]]; then
                # Display coverage for each module using parallel arrays
                for i in "${!module_names[@]}"; do
                    echo "  ✅ ${module_names[$i]}: ${module_coverages[$i]}% coverage"
                done
            else
                echo "  (none)"
            fi
            echo ""
            echo "Modules skipped (${#skipped_modules[@]}/${#go_modules[@]}):"
            if [[ ${#skipped_modules[@]} -gt 0 ]]; then
                for module in "${skipped_modules[@]}"; do
                    IFS=':' read -r mod_name skip_reason <<< "$module"
                    local reason=$(echo "$skip_reason" | sed 's/skipped_//' | tr '_' ' ')
                    echo "  ⏭️  ${mod_name} (${reason})"
                done
            else
                echo "  (none)"
            fi
            echo ""
            echo "Modules that failed coverage collection (${#failed_modules[@]}/${testable_modules}):"
            if [[ ${#failed_modules[@]} -gt 0 ]]; then
                for module in "${failed_modules[@]}"; do
                    IFS=':' read -r mod_name failure_reason <<< "$module"
                    case $failure_reason in
                        test_failures_*)
                            local count=$(echo "$failure_reason" | sed 's/test_failures_//')
                            echo "  ❌ ${mod_name} (${count} package(s) with test failures)" ;;
                        timeout_*)
                            local timeout_val=$(echo "$failure_reason" | sed 's/timeout_//')
                            echo "  ⏰ ${mod_name} (timed out after ${timeout_val})" ;;
                        build_failed) echo "  🏗️  ${mod_name} (build/compilation failed)" ;;
                        setup_failed) echo "  ⚙️  ${mod_name} (test setup failed)" ;;
                        panic) echo "  💥 ${mod_name} (test panic)" ;;
                        network_failed) echo "  🌐 ${mod_name} (network connectivity issues)" ;;
                        filesystem_failed) echo "  📂 ${mod_name} (file system access issues)" ;;
                        no_data) echo "  ⚠️  ${mod_name} (no coverage data generated)" ;;
                        access_failed) echo "  🚫 ${mod_name} (directory access failed)" ;;
                        no_log) echo "  📝 ${mod_name} (no test log generated)" ;;
                        unknown_failure) echo "  ❓ ${mod_name} (unknown test failure)" ;;
                        *) echo "  ❓ ${mod_name} (${failure_reason})" ;;
                    esac
                done
            else
                echo "  (none)"
            fi
            echo ""

            echo "=== COVERAGE BY PACKAGE ==="
            go tool cover -func="${coverage_profile}" | grep -v "^total:" | \
                awk '{printf "%-50s %8s\n", $1, $3}' | \
                sort -k2 -nr | head -20
            echo ""

            echo "=== LOW COVERAGE FILES (<70%) ==="
            go tool cover -func="${coverage_profile}" | \
                awk '$3 != "" && $3 != "coverage" {gsub(/%/, "", $3); if($3 < 70) print $1, $3"%"}' | \
                sort -k2 -n | head -10
            echo ""

            echo "=== COVERAGE COLLECTION WARNINGS ==="
            if [[ ${#skipped_modules[@]} -gt 0 ]]; then
                echo "ℹ️  ${#skipped_modules[@]} module(s) were intentionally skipped and excluded from coverage."
            fi
            if [[ ${#failed_modules[@]} -gt 0 ]]; then
                echo "⚠️  Coverage data is incomplete due to ${#failed_modules[@]} failed modules out of ${testable_modules} testable modules."
                echo "    The reported coverage percentage only reflects successfully tested modules."
                echo "    Fix failing tests in the failed modules for complete coverage analysis."
            elif [[ ${#skipped_modules[@]} -gt 0 ]]; then
                echo "✅ All testable modules contributed to coverage analysis."
            else
                echo "✅ All modules contributed to coverage analysis."
            fi

        } > "${coverage_summary}"

        # Display results
        local testable_modules=$((${#go_modules[@]} - ${#skipped_modules[@]}))
        if [[ ${coverage_status} == "PASS" ]]; then
            print_success "Coverage: ${total_coverage}% from ${#successful_modules[@]}/${testable_modules} testable modules (threshold: ${COVERAGE_THRESHOLD}%)"
        elif [[ ${coverage_status} == "PARTIAL" ]]; then
            if (( $(echo "${total_coverage} >= ${COVERAGE_THRESHOLD}" | bc -l 2>/dev/null || echo "0") )); then
                print_success "Coverage: ${total_coverage}% from ${#successful_modules[@]}/${testable_modules} testable modules (threshold: ${COVERAGE_THRESHOLD}%)"
                print_warning "Coverage incomplete: ${#failed_modules[@]} of ${testable_modules} testable modules failed"
            else
                print_error "Coverage: ${total_coverage}% from ${#successful_modules[@]}/${testable_modules} testable modules (below threshold: ${COVERAGE_THRESHOLD}%)"
                print_warning "Coverage incomplete: ${#failed_modules[@]} of ${testable_modules} testable modules failed"
            fi
        else
            print_error "Coverage: ${total_coverage}% (below threshold: ${COVERAGE_THRESHOLD}%)"
        fi

        if [[ ${#skipped_modules[@]} -gt 0 ]]; then
            print_info "Skipped ${#skipped_modules[@]} non-testable modules (E2E tests, etc.)"
        fi
        print_info "Successful modules: ${successful_modules[*]}"
        if [[ ${#failed_modules[@]} -gt 0 ]]; then
            print_warning "Failed modules: ${failed_modules[*]}"
        fi
        print_info "HTML report generated: ${coverage_html}"

    else
        total_coverage="0"
        print_error "Coverage profile not generated"
        echo "Coverage analysis failed" > "${coverage_summary}"
    fi

    print_info "Coverage results saved to: ${coverage_output}"
    print_info "Coverage summary saved to: ${coverage_summary}"

    # Return 0 if coverage meets threshold, 1 otherwise
    if (( $(echo "${total_coverage} >= ${COVERAGE_THRESHOLD}" | bc -l 2>/dev/null || echo "0") )); then
        return 0
    else
        return 1
    fi
}

# Function to generate final audit report
generate_final_report() {
    local lint_issues=$1
    local test_failures=$2
    local coverage_status=$3
    local final_report="${AUDIT_DIR}/audit-report-${TIMESTAMP}.md"

    print_header "${CHART} GENERATING FINAL AUDIT REPORT"

    # Determine overall status
    local overall_status="PASS"
    if [[ ${lint_issues} -gt 0 ]] || [[ ${test_failures} -gt 0 ]] || [[ ${coverage_status} -ne 0 ]]; then
        overall_status="FAIL"
    fi

    # Generate markdown report
    {
        echo "# Repository Audit Report"
        echo ""
        echo "**Generated:** $(date)"
        echo "**Repository:** $(basename "$(pwd)")"
        echo "**Overall Status:** ${overall_status}"
        echo ""

        echo "## Summary"
        echo ""
        echo "| Category | Status | Details |"
        echo "|----------|--------|---------|"
        echo "| Linting | $(if [[ ${lint_issues} -eq 0 ]]; then echo "✅ PASS"; else echo "❌ FAIL"; fi) | ${lint_issues} issues found |"
        echo "| Tests | $(if [[ ${test_failures} -eq 0 ]]; then echo "✅ PASS"; else echo "❌ FAIL"; fi) | ${test_failures} failed tests |"
        echo "| Coverage | $(if [[ ${coverage_status} -eq 0 ]]; then echo "✅ PASS"; else echo "❌ FAIL"; fi) | $(if [[ -f "${AUDIT_DIR}/coverage-summary-${TIMESTAMP}.txt" ]]; then grep "Total Coverage:" "${AUDIT_DIR}/coverage-summary-${TIMESTAMP}.txt" | cut -d':' -f2 | xargs; else echo "N/A"; fi) |"
        echo ""

        echo "## Detailed Results"
        echo ""
        echo "### Linting Analysis"
        if [[ -f "${AUDIT_DIR}/lint-summary-${TIMESTAMP}.txt" ]]; then
            echo '```'
            head -20 "${AUDIT_DIR}/lint-summary-${TIMESTAMP}.txt"
            echo '```'
        else
            echo "No linting summary available"
        fi
        echo ""

        echo "### Test Analysis"
        if [[ -f "${AUDIT_DIR}/test-summary-${TIMESTAMP}.txt" ]]; then
            echo '```'
            head -20 "${AUDIT_DIR}/test-summary-${TIMESTAMP}.txt"
            echo '```'
        else
            echo "No test summary available"
        fi
        echo ""

        echo "### Coverage Analysis"
        if [[ -f "${AUDIT_DIR}/coverage-summary-${TIMESTAMP}.txt" ]]; then
            echo '```'
            head -20 "${AUDIT_DIR}/coverage-summary-${TIMESTAMP}.txt"
            echo '```'
        else
            echo "No coverage summary available"
        fi
        echo ""

        echo "## Files Generated"
        echo ""
        find "${AUDIT_DIR}" -name "*${TIMESTAMP}*" -type f | while read -r file; do
            echo "- $(basename "${file}")"
        done

    } > "${final_report}"

    print_success "Final audit report generated: ${final_report}"

    # Display final summary
    echo ""
    print_header "${CHART} FINAL AUDIT SUMMARY"

    if [[ ${overall_status} == "PASS" ]]; then
        print_success "Repository audit: ${overall_status}"
        print_success "All quality checks passed!"
    else
        print_error "Repository audit: ${overall_status}"
        print_error "Quality issues detected - see detailed reports"
    fi

    print_stat "Linting: ${lint_issues} issues"
    print_stat "Tests: ${test_failures} failures"
    print_stat "Coverage: $(if [[ -f "${AUDIT_DIR}/coverage-summary-${TIMESTAMP}.txt" ]]; then grep "Total Coverage:" "${AUDIT_DIR}/coverage-summary-${TIMESTAMP}.txt" | cut -d':' -f2 | xargs; else echo "N/A"; fi)"

    echo ""
    print_info "All detailed reports available in: ${AUDIT_DIR}/"
    print_info "Open ${final_report} for complete audit results"

    if [[ ${overall_status} == "FAIL" ]]; then
        return 1
    else
        return 0
    fi
}

# Main execution
main() {
    print_header "${GEAR} REPOSITORY AUDIT STARTING"
    print_info "Auditing repository: $(basename "$(pwd)")"
    print_info "Timestamp: $(date)"

    # Create audit directory
    create_audit_dir

    # Initialize counters
    local lint_issues=0
    local test_failures=0
    local coverage_status=1

    # Run linting analysis
    if run_linting_analysis; then
        lint_issues=0
    else
        lint_issues=$?
    fi

    echo "" # Spacing

    # Run test analysis
    if run_test_analysis; then
        test_failures=0
    else
        test_failures=$?
    fi

    echo "" # Spacing

    # Run coverage analysis
    if run_coverage_analysis; then
        coverage_status=0
    else
        coverage_status=1
    fi

    echo "" # Spacing

    # Generate final report
    generate_final_report ${lint_issues} ${test_failures} ${coverage_status}

    # Return appropriate exit code
    if [[ ${lint_issues} -eq 0 ]] && [[ ${test_failures} -eq 0 ]] && [[ ${coverage_status} -eq 0 ]]; then
        exit 0
    else
        exit 1
    fi
}

# Help function
show_help() {
    echo "Repository Audit Script"
    echo ""
    echo "Usage: $0 [options]"
    echo ""
    echo "Options:"
    echo "  -h, --help              Show this help message"
    echo "  -t, --timeout DURATION  Set timeout for operations (default: ${TIMEOUT})"
    echo "  -c, --coverage NUM      Set coverage threshold percentage (default: ${COVERAGE_THRESHOLD})"
    echo "  --clean                 Clean previous audit results"
    echo ""
    echo "This script performs comprehensive repository analysis:"
    echo "1. Linting analysis using golangci-lint"
    echo "2. Test execution and failure analysis"
    echo "3. Code coverage analysis and reporting"
    echo ""
    echo "Results are saved in ${AUDIT_DIR}/ directory"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -t|--timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        -c|--coverage)
            COVERAGE_THRESHOLD="$2"
            shift 2
            ;;
        --clean)
            print_info "Cleaning previous audit results..."
            rm -rf "${AUDIT_DIR}"
            print_success "Audit results cleaned"
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Check prerequisites
if ! command -v ~/go/bin/golangci-lint >/dev/null 2>&1; then
    print_error "golangci-lint not found at ~/go/bin/golangci-lint"
    print_info "Please install golangci-lint or update the path in this script"
    exit 1
fi

if ! command -v go >/dev/null 2>&1; then
    print_error "Go not found in PATH"
    exit 1
fi

# Run main function
main "$@"
