#!/bin/bash
# Linting analysis for repository audit

set -e

# Get the directory where this script lives
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source shared helpers
source "${SCRIPT_DIR}/audit-helpers.sh"

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
    done < <(discover_go_modules)

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

# Main execution
main() {
    # Create audit directory
    create_audit_dir

    # Run linting analysis
    if run_linting_analysis; then
        exit 0
    else
        exit $?
    fi
}

# Run main function
main "$@"
