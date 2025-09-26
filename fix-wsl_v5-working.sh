#!/bin/bash

# Working automated wsl_v5 fixer - handles the most common violation patterns

set -e

echo "🔥 AUTOMATED WSL_V5 FIXER - ELIMINATING ALL VIOLATIONS"

cd /Users/poile/repos/mcp

# Count issues before
before_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "📊 STARTING WITH $before_count WSL_V5 VIOLATIONS"

if [ "$before_count" -eq 0 ]; then
    echo "🎉 ALREADY PERFECT!"
    exit 0
fi

# Get all files with violations
violation_files=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | cut -d: -f1 | sort -u)

files_processed=0
files_modified=0

# Process each file
for file in $violation_files; do
    if [ -f "$file" ]; then
        echo "🔧 PROCESSING: $file"
        temp_file="${file}.tmp"
        files_processed=$((files_processed + 1))
        
        # Apply multiple passes for different patterns
        
        # Pass 1: Add blank lines where missing
        awk '
        BEGIN { prev_line = ""; prev_prev_line = "" }
        {
            current_line = $0
            
            # Pattern: invalid statement above if - add blank line before nested if
            if (current_line ~ /^\s+if .* != ""/ && 
                prev_line ~ /(require\.Error|assert\.Error)/ && 
                prev_line !~ /^\s*$/) {
                print ""
            }
            
            # Pattern: invalid statement above assign - add blank line after t.Parallel()
            if (current_line ~ /^\s*[a-zA-Z_].*[=:]/ && 
                prev_line ~ /t\.Parallel\(\)/ && 
                prev_line !~ /^\s*$/) {
                print ""
            }
            
            # Pattern: invalid statement above expr - add blank line before assertions  
            if (current_line ~ /^\s*(assert\.|require\.|t\.)/ && 
                prev_line !~ /^\s*$/ && prev_line != "" &&
                prev_line ~ /[;})]$/ &&
                prev_line !~ /(assert\.|require\.|t\.)/) {
                print ""
            }
            
            # Pattern: no shared variables above range/defer/go
            if (current_line ~ /^\s*(for.*range|defer |go )/ && 
                prev_line !~ /^\s*$/ && prev_line != "" &&
                prev_line !~ /^\s*{/ &&
                prev_line !~ /(for|defer|go)/) {
                print ""
            }
            
            # Pattern: too many lines above return
            if (current_line ~ /^\s*return / && 
                prev_line !~ /^\s*$/ && prev_line != "" &&
                prev_line !~ /^\s*{/ && prev_line !~ /^\s*}/ &&
                prev_prev_line !~ /^\s*$/) {
                print ""
            }
            
            print current_line
            prev_prev_line = prev_line
            prev_line = current_line
        }
        ' "$file" > "$temp_file"
        
        # Pass 2: Clean up extra whitespace
        awk '
        BEGIN { prev_blank = 0 }
        {
            current_blank = ($0 ~ /^\s*$/)
            
            # Remove consecutive blank lines (leading-whitespace)
            if (current_blank && prev_blank) {
                next
            }
            
            print $0
            prev_blank = current_blank
        }
        ' "$temp_file" > "${temp_file}.2"
        
        mv "${temp_file}.2" "$temp_file"
        
        # Remove trailing whitespace
        sed 's/[[:space:]]*$//' "$temp_file" > "${temp_file}.clean"
        mv "${temp_file}.clean" "$temp_file"
        
        # Apply changes if different
        if ! cmp -s "$file" "$temp_file"; then
            mv "$temp_file" "$file"
            files_modified=$((files_modified + 1))
            echo "  ✅ FIXED $file"
        else
            rm "$temp_file"
            echo "  ⏭️  NO CHANGES NEEDED"
        fi
    fi
done

echo ""
echo "📊 PROCESSED $files_processed FILES, MODIFIED $files_modified FILES"

# Count violations after fix
echo "📊 COUNTING VIOLATIONS AFTER FIX..."
after_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')

improvement=$((before_count - after_count))
echo ""
echo "🎯 AUTOMATED RESULTS:"
echo "   BEFORE: $before_count violations"
echo "   AFTER:  $after_count violations" 
echo "   FIXED:  $improvement violations"

if [ $improvement -gt 0 ]; then
    percentage=$(( (improvement * 100) / before_count ))
    echo "   SUCCESS: $percentage% AUTOMATED IMPROVEMENT!"
    
    if [ $after_count -eq 0 ]; then
        echo ""
        echo "🏆 COMPLETE AUTOMATION SUCCESS!"
        echo "🚀 ALL WSL_V5 VIOLATIONS ELIMINATED AUTOMATICALLY!"
        echo "🎉 MISSION ACCOMPLISHED - NO MANUAL INTERVENTION NEEDED!"
    else
        echo ""
        echo "📋 REMAINING VIOLATIONS TO REFINE:"
        golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | head -3
        echo ""
        echo "🔄 ITERATING FOR COMPLETE AUTOMATION..."
    fi
else
    echo ""
    echo "❌ AUTOMATION NEEDS REFINEMENT"
    echo "📋 ANALYZING UNRESOLVED PATTERNS:"
    golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | head -3
fi

echo ""
echo "✨ AUTOMATED WSL_V5 FIX COMPLETED!"