#!/bin/bash
# Coverage analysis for repository audit

set -e

# Get the directory where this script lives
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source shared helpers
source "${SCRIPT_DIR}/audit-helpers.sh"

# Function to run coverage analysis
run_coverage_analysis() {
    print_header "${CHART} REPOSITORY COVERAGE ANALYSIS"

    local coverage_output="${AUDIT_DIR}/coverage-results-${TIMESTAMP}.txt"
    local coverage_profile="${AUDIT_DIR}/coverage-profile-${TIMESTAMP}.out"
    local coverage_html="${AUDIT_DIR}/coverage-report-${TIMESTAMP}.html"
    local coverage_summary="${AUDIT_DIR}/coverage-summary-${TIMESTAMP}.txt"

    print_info "Running test coverage analysis..."

    # Find all Go modules
    local go_modules=()
    while IFS= read -r module_dir; do
        go_modules+=("$module_dir")
    done < <(discover_go_modules)

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

                # Use appropriate timeout per module type
                local module_timeout=420  # 7 minutes for main modules
                case "${module_dir}" in
                    "./test/e2e/"*) module_timeout=3600 ;;    # 60 minutes for E2E tests
                    "./services/router"*) module_timeout=720 ;; # 12 minutes for router
                    "./services/"*) module_timeout=600 ;;     # 10 minutes for other service modules
                    ".") module_timeout=480 ;;                # 8 minutes for root
                esac
                echo "Starting coverage collection (timeout: ${module_timeout}s)..."

                # Run tests WITHOUT -short to get real test results
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
                        # Analyze the failure
                        local total_lines=$(wc -l < "${module_log}")
                        echo "Test log has ${total_lines} lines. Analyzing failure..."

                        # Check for specific failure patterns
                        if grep -q "signal: killed" "${module_log}" 2>/dev/null; then
                            echo "❌ TIMEOUT: Tests were killed after ${module_timeout}s timeout"
                            local last_test=$(grep -E "=== RUN|--- PASS:|--- FAIL:|--- SKIP:" "${module_log}" | tail -1 | awk '{print $3}' 2>/dev/null || echo "unknown")
                            echo "Last test before timeout: ${last_test}"
                            failed_modules+=("${module_name}:timeout_${module_timeout}s")
                        elif grep -qE "(cannot find package|build failed|compile error)" "${module_log}" 2>/dev/null; then
                            echo "❌ BUILD FAILURE: Module failed to compile"
                            failed_modules+=("${module_name}:build_failed")
                        elif grep -q "panic:" "${module_log}" 2>/dev/null; then
                            echo "❌ PANIC: Tests panicked during execution"
                            failed_modules+=("${module_name}:panic")
                        elif grep -q "FAIL.*github.com" "${module_log}" 2>/dev/null; then
                            local failed_packages=$(grep "FAIL.*github.com" "${module_log}" | wc -l)
                            echo "❌ TEST FAILURES: ${failed_packages} package(s) have failing tests"
                            failed_modules+=("${module_name}:test_failures_${failed_packages}")
                        else
                            echo "❌ UNKNOWN FAILURE: Tests failed for unclear reasons"
                            failed_modules+=("${module_name}:unknown_failure")
                        fi
                    else
                        echo "❌ NO LOG: No test output generated"
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
    local module_names=()
    local module_coverages=()

    local coverage_status="FAIL"
    local total_coverage="0"

    if [[ ${#temp_profiles[@]} -gt 0 ]]; then
        print_info "Merging coverage profiles from ${#temp_profiles[@]} successful modules..."
        echo "mode: atomic" > "${coverage_profile}"
        local total_merged_lines=0

        # Extract individual module coverage percentages before merging
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
                        panic) echo "  💥 ${mod_name} (test panic)" ;;
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
    print_info "Summary saved to: ${coverage_summary}"

    # Return 0 if coverage meets threshold, 1 otherwise
    if (( $(echo "${total_coverage} >= ${COVERAGE_THRESHOLD}" | bc -l 2>/dev/null || echo "0") )); then
        return 0
    else
        return 1
    fi
}

# Main execution
main() {
    # Create audit directory
    create_audit_dir

    # Run coverage analysis
    if run_coverage_analysis; then
        exit 0
    else
        exit 1
    fi
}

# Run main function
main "$@"
