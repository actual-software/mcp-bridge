#!/bin/bash
# Shared helper functions for audit scripts

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
TIMEOUT="${TIMEOUT:-15m}"
COVERAGE_THRESHOLD="${COVERAGE_THRESHOLD:-80}"
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

# Discover Go modules (workspace-aware)
discover_go_modules() {
    local go_modules=()

    # Handle Go workspace by reading go.work file if it exists
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

    # Output the modules (one per line)
    printf '%s\n' "${go_modules[@]}"
}
