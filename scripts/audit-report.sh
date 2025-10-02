#!/bin/bash
# Generate final audit report

set -e

# Get the directory where this script lives
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source shared helpers
source "${SCRIPT_DIR}/audit-helpers.sh"

# Function to generate final audit report
generate_final_report() {
    print_header "${CHART} GENERATING FINAL AUDIT REPORT"

    # Find the most recent summary files
    local lint_summary=$(ls -t "${AUDIT_DIR}"/lint-summary-*.txt 2>/dev/null | head -1)
    local test_summary=$(ls -t "${AUDIT_DIR}"/test-summary-*.txt 2>/dev/null | head -1)
    local coverage_summary=$(ls -t "${AUDIT_DIR}"/coverage-summary-*.txt 2>/dev/null | head -1)

    # Extract metrics from summaries
    local lint_issues=0
    local test_failures=0
    local coverage_status=1

    if [[ -f "${lint_summary}" ]]; then
        lint_issues=$(grep "Total Issues:" "${lint_summary}" 2>/dev/null | awk '{print $3}' || echo "0")
    fi

    if [[ -f "${test_summary}" ]]; then
        test_failures=$(grep "Failed:" "${test_summary}" 2>/dev/null | awk '{print $2}' || echo "0")
    fi

    if [[ -f "${coverage_summary}" ]]; then
        local coverage=$(grep "Total Coverage:" "${coverage_summary}" 2>/dev/null | awk '{print $3}' | sed 's/%$//' || echo "0")
        if (( $(echo "${coverage} >= ${COVERAGE_THRESHOLD}" | bc -l 2>/dev/null || echo "0") )); then
            coverage_status=0
        fi
    fi

    local final_report="${AUDIT_DIR}/audit-report-${TIMESTAMP}.md"

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
        echo "| Coverage | $(if [[ ${coverage_status} -eq 0 ]]; then echo "✅ PASS"; else echo "❌ FAIL"; fi) | $(if [[ -f "${coverage_summary}" ]]; then grep "Total Coverage:" "${coverage_summary}" | cut -d':' -f2 | xargs; else echo "N/A"; fi) |"
        echo ""

        echo "## Detailed Results"
        echo ""
        echo "### Linting Analysis"
        if [[ -f "${lint_summary}" ]]; then
            echo '```'
            head -20 "${lint_summary}"
            echo '```'
        else
            echo "No linting summary available"
        fi
        echo ""

        echo "### Test Analysis"
        if [[ -f "${test_summary}" ]]; then
            echo '```'
            head -20 "${test_summary}"
            echo '```'
        else
            echo "No test summary available"
        fi
        echo ""

        echo "### Coverage Analysis"
        if [[ -f "${coverage_summary}" ]]; then
            echo '```'
            head -20 "${coverage_summary}"
            echo '```'
        else
            echo "No coverage summary available"
        fi
        echo ""

        echo "## Files Generated"
        echo ""
        find "${AUDIT_DIR}" -name "*${TIMESTAMP}*" -type f 2>/dev/null | while read -r file; do
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
    print_stat "Coverage: $(if [[ -f "${coverage_summary}" ]]; then grep "Total Coverage:" "${coverage_summary}" | cut -d':' -f2 | xargs; else echo "N/A"; fi)"

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
    # Ensure audit directory exists
    if [[ ! -d "${AUDIT_DIR}" ]]; then
        print_warning "Audit directory does not exist. Creating it..."
        create_audit_dir
    fi

    # Generate final report
    if generate_final_report; then
        exit 0
    else
        exit 1
    fi
}

# Run main function
main "$@"
