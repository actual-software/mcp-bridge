#!/bin/bash

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
MAX_PARALLEL="${MAX_PARALLEL:-5}"
MAX_ISSUES="${MAX_ISSUES:-100}"
TIMEOUT_PER_FIX="${TIMEOUT_PER_FIX:-120}"
LOG_DIR="lint-fix-logs"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Print colored status messages
print_status() { echo -e "${BLUE}[$(date +'%H:%M:%S')]${NC} $1"; }
print_success() { echo -e "${GREEN}[$(date +'%H:%M:%S')]${NC} ✓ $1"; }
print_warning() { echo -e "${YELLOW}[$(date +'%H:%M:%S')]${NC} ⚠ $1"; }
print_error() { echo -e "${RED}[$(date +'%H:%M:%S')]${NC} ✗ $1"; }

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --max-parallel)
            MAX_PARALLEL="$2"
            shift 2
            ;;
        --max-issues)
            MAX_ISSUES="$2"
            shift 2
            ;;
        --timeout)
            TIMEOUT_PER_FIX="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --max-parallel N   Maximum parallel Claude processes (default: 5)"
            echo "  --max-issues N     Maximum issues to process (default: 100)"
            echo "  --timeout N        Timeout per fix in seconds (default: 120)"
            echo "  --help            Show this help message"
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Create log directory
mkdir -p "$LOG_DIR"

print_status "Starting automated lint fix process (v2)"
print_status "Configuration: MAX_PARALLEL=$MAX_PARALLEL, MAX_ISSUES=$MAX_ISSUES"

# Find all Go modules
modules=($(find . -name "go.mod" -type f | xargs -I {} dirname {}))

print_status "Found ${#modules[@]} Go modules"

# Function to fix a single file's issues
fix_file_issues() {
    local module="$1"
    local file="$2"
    local batch_num="$3"
    
    local log_file="$LOG_DIR/fix_batch${batch_num}_$(basename "$file").log"
    
    # Create a focused prompt for this file
    local prompt="Fix ALL lint issues in the file $file in module $module.

First run: cd $module && golangci-lint run --timeout=5m $file 2>&1

Then fix all issues reported. Focus on:
- errcheck: Add proper error handling with if err != nil checks
- unused: Remove or use unused variables/imports
- ineffassign: Fix ineffectual assignments
- gosec: Fix security issues
- forcetypeassert: Add type checking for type assertions
- mnd: Define magic numbers as named constants
- funlen: Split long functions into smaller ones
- Other issues as reported

Make all necessary changes using the Edit or MultiEdit tools.
After fixing, output 'COMPLETE' at the end."

    (
        timeout "$TIMEOUT_PER_FIX" claude -p "$prompt" > "$log_file" 2>&1
        local exit_code=$?
        
        if [[ $exit_code -eq 0 ]]; then
            print_success "Completed $file"
        elif [[ $exit_code -eq 124 ]]; then
            print_error "Timeout fixing $file"
        else
            print_error "Error fixing $file (exit: $exit_code)"
        fi
    ) &
    
    return 0
}

# Process each module
total_fixed=0
for module in "${modules[@]}"; do
    print_status "Processing module: $module"
    cd "$module" 2>/dev/null || continue
    
    # Get files with issues
    files_with_issues=$(golangci-lint run --timeout=5m --out-format=json 2>/dev/null | jq -r '.Issues[].Pos.Filename' | sort -u | head -$MAX_ISSUES)
    
    if [[ -z "$files_with_issues" ]]; then
        print_success "No issues in $module"
        cd - > /dev/null 2>&1
        continue
    fi
    
    # Process files in batches
    batch_num=1
    active_jobs=0
    
    for file in $files_with_issues; do
        # Wait if we have too many parallel jobs
        while [[ $(jobs -r | wc -l) -ge $MAX_PARALLEL ]]; do
            sleep 1
        done
        
        fix_file_issues "$module" "$file" "$batch_num" &
        ((batch_num++))
        ((total_fixed++))
        
        if [[ $total_fixed -ge $MAX_ISSUES ]]; then
            print_warning "Reached maximum issue limit ($MAX_ISSUES)"
            break 2
        fi
    done
    
    cd - > /dev/null 2>&1
done

# Wait for all background jobs to complete
print_status "Waiting for all fixes to complete..."
wait

# Final summary
print_status "Generating final summary..."
echo "=== Lint Fix Summary (v2) ===" | tee "$LOG_DIR/summary_${TIMESTAMP}.txt"
echo "Timestamp: $(date)" | tee -a "$LOG_DIR/summary_${TIMESTAMP}.txt"
echo "Files processed: $total_fixed" | tee -a "$LOG_DIR/summary_${TIMESTAMP}.txt"

# Check remaining issues
print_status "Checking remaining issues..."
total_remaining=0
for module in "${modules[@]}"; do
    if [[ -d "$module" ]]; then
        cd "$module" 2>/dev/null || continue
        remaining=$(golangci-lint run --timeout=5m 2>&1 | grep -E "^[0-9]+ issues" | awk '{print $1}' || echo "0")
        if [[ "$remaining" != "0" ]]; then
            echo "$module: $remaining issues remaining" | tee -a "$LOG_DIR/summary_${TIMESTAMP}.txt"
            total_remaining=$((total_remaining + remaining))
        fi
        cd - > /dev/null 2>&1
    fi
done

if [[ $total_remaining -eq 0 ]]; then
    print_success "All lint issues resolved!"
else
    print_warning "$total_remaining issues remaining across all modules"
fi

echo "Logs saved to: $LOG_DIR" | tee -a "$LOG_DIR/summary_${TIMESTAMP}.txt"