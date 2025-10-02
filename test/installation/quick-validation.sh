#!/bin/bash
set -euo pipefail

# Quick MCP Installation Scripts Validation
# Tests core functionality without full Docker setup

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

success() { echo -e "${GREEN}✓${NC} $*"; }
error() { echo -e "${RED}✗${NC} $*" >&2; }
warn() { echo -e "${YELLOW}⚠${NC} $*"; }
log() { echo -e "${BLUE}[$(date +'%H:%M:%S')]${NC} $*"; }

main() {
    echo "Quick MCP Installation Scripts Validation"
    echo "========================================"
    echo
    
    local failed=0
    
    # Test 1: Check script existence and permissions
    log "Checking script files..."
    
    local scripts=(
        "$PROJECT_ROOT/scripts/install.sh"
        "$PROJECT_ROOT/services/router/install.sh"
        "$PROJECT_ROOT/services/router/uninstall.sh"
    )
    
    for script in "${scripts[@]}"; do
        if [[ -f "$script" ]]; then
            success "Found: $script"
            if [[ -x "$script" ]]; then
                success "Executable: $script"
            else
                error "Not executable: $script"
                ((failed++))
            fi
        else
            error "Missing: $script"
            ((failed++))
        fi
    done
    
    # Test 2: Basic syntax validation
    log "Validating script syntax..."
    
    for script in "${scripts[@]}"; do
        if [[ -f "$script" ]]; then
            if bash -n "$script" 2>/dev/null; then
                success "Syntax valid: $(basename "$script")"
            else
                error "Syntax error: $(basename "$script")"
                ((failed++))
            fi
        fi
    done
    
    # Test 3: Help option functionality
    log "Testing help options..."
    
    # Test development install help
    if [[ -f "$PROJECT_ROOT/scripts/install.sh" ]]; then
        if "$PROJECT_ROOT/scripts/install.sh" --help >/dev/null 2>&1; then
            success "Dev install --help works"
        else
            error "Dev install --help failed"
            ((failed++))
        fi
    fi
    
    # Test router install help
    if [[ -f "$PROJECT_ROOT/services/router/install.sh" ]]; then
        if "$PROJECT_ROOT/services/router/install.sh" --help >/dev/null 2>&1; then
            success "Router install --help works"
        else
            error "Router install --help failed"
            ((failed++))
        fi
    fi
    
    # Test 4: GitHub repository references
    log "Checking GitHub repository references..."
    
    local expected_repo="actual-software/mcp-bridge"
    
    for script in "${scripts[@]}"; do
        if [[ -f "$script" ]]; then
            if grep -q "$expected_repo" "$script"; then
                success "Correct repo reference: $(basename "$script")"
            else
                error "Missing/incorrect repo reference: $(basename "$script")"
                ((failed++))
            fi
        fi
    done
    
    # Test 5: Check for hardcoded paths that might be problematic
    log "Checking for potential path issues..."
    
    local problematic_paths=(
        "/tmp/hardcoded"
        "/usr/bin/hardcoded"
        "/opt/hardcoded"
    )
    
    for script in "${scripts[@]}"; do
        if [[ -f "$script" ]]; then
            local issues=0
            for path in "${problematic_paths[@]}"; do
                if grep -q "$path" "$script"; then
                    warn "Potential hardcoded path in $(basename "$script"): $path"
                    ((issues++))
                fi
            done
            if [[ $issues -eq 0 ]]; then
                success "No problematic paths: $(basename "$script")"
            fi
        fi
    done
    
    # Test 6: Essential command dependencies
    log "Checking required command usage..."
    
    local required_commands=(
        "curl"
        "chmod"
        "mkdir"
    )
    
    for script in "${scripts[@]}"; do
        if [[ -f "$script" ]]; then
            local missing_commands=()
            for cmd in "${required_commands[@]}"; do
                if ! grep -q "$cmd" "$script"; then
                    missing_commands+=("$cmd")
                fi
            done
            
            if [[ ${#missing_commands[@]} -eq 0 ]]; then
                success "All required commands used: $(basename "$script")"
            else
                warn "Missing commands in $(basename "$script"): ${missing_commands[*]}"
            fi
        fi
    done
    
    # Test 7: Error handling patterns
    log "Checking error handling patterns..."
    
    for script in "${scripts[@]}"; do
        if [[ -f "$script" ]]; then
            local has_error_handling=false
            
            # Check for common error handling patterns
            if grep -q "set -e" "$script" || grep -q "exit 1" "$script" || grep -q "error()" "$script"; then
                has_error_handling=true
            fi
            
            if [[ $has_error_handling == true ]]; then
                success "Error handling present: $(basename "$script")"
            else
                warn "Limited error handling: $(basename "$script")"
            fi
        fi
    done
    
    # Summary
    echo
    if [[ $failed -eq 0 ]]; then
        success "All quick validation tests passed! 🎉"
        echo
        echo "Ready for full Docker validation:"
        echo "  $SCRIPT_DIR/docker-validation.sh"
        echo
        echo "Or test specific environment:"
        echo "  $SCRIPT_DIR/docker-validation.sh --env ubuntu:22.04"
        exit 0
    else
        error "$failed validation issues found"
        echo
        echo "Fix issues before running full Docker validation"
        exit 1
    fi
}

main "$@"