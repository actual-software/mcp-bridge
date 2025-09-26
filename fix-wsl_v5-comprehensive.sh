#!/bin/bash

# Comprehensive automated wsl_v5 fixer - handles ALL violation patterns

set -e

echo "🔥 COMPREHENSIVE WSL_V5 AUTOMATION - FIXING ALL VIOLATIONS"

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

echo "🎯 PROCESSING $(echo "$violation_files" | wc -l) FILES WITH VIOLATIONS"

# Process each file
for file in $violation_files; do
    echo "🔧 FIXING: $file"
    temp_file="${file}.tmp"
    
    # Comprehensive AWK script that handles ALL patterns
    awk '
    BEGIN { 
        prev_line = ""
        prev_prev_line = ""
        in_func = 0
        brace_count = 0
    }
    
    {
        current_line = $0
        current_stripped = gensub(/^\s*/, "", "g", current_line)
        prev_stripped = gensub(/^\s*/, "", "g", prev_line)
        
        # Track function context and brace levels
        if (current_line ~ /^func /) in_func = 1
        if (current_line ~ /{/) brace_count++
        if (current_line ~ /}/) brace_count--
        
        # PATTERN 1: missing whitespace above this line (invalid statement above if)
        # Add blank line before if statements when needed
        if (current_stripped ~ /^if / && prev_line !~ /^\s*$/ && prev_line != "" &&
            prev_stripped !~ /^{$/ && prev_stripped !~ /^}/ && 
            prev_stripped !~ /^if / && prev_stripped !~ /^else/ &&
            prev_stripped ~ /(Error\(t, err\)|\.Error\(|assert\.|require\.)/) {
            print ""
        }
        
        # PATTERN 2: invalid statement above assign  
        # Add blank line before assignments after certain statements
        if (current_stripped ~ /^[a-zA-Z_][a-zA-Z0-9_]*.*[=:]/ && prev_line !~ /^\s*$/ && 
            prev_line != "" && prev_stripped !~ /^{$/ && prev_stripped !~ /^}/ &&
            (prev_stripped ~ /t\.Parallel\(\)/ || prev_stripped ~ /t\.Helper\(\)/ ||
             prev_stripped ~ /require\.(NoError|Error)/ || prev_stripped ~ /assert\./)) {
            print ""
        }
        
        # PATTERN 3: too many lines above return
        # Add blank line before return statements in certain contexts  
        if (current_stripped ~ /^return / && prev_line !~ /^\s*$/ && prev_line != "" &&
            prev_stripped !~ /^{$/ && prev_stripped !~ /^}/ && brace_count > 1 &&
            (prev_stripped ~ /^\}$/ || prev_stripped ~ /[;}]$/ || 
             prev_prev_line !~ /^\s*$/)) {
            print ""
        }
        
        # PATTERN 4: invalid statement above expr
        # Add blank line before certain expressions
        if ((current_stripped ~ /^t\./ || current_stripped ~ /^assert\./ || 
             current_stripped ~ /^require\./) && prev_line !~ /^\s*$/ && 
            prev_line != "" && prev_stripped !~ /^{$/ && prev_stripped !~ /^}/ &&
            prev_stripped !~ /^(assert|require|t\.)/) {
            print ""
        }
        
        # PATTERN 5: no shared variables above range/defer/go
        # Add blank line before range, defer, go statements
        if ((current_stripped ~ /^for.*range/ || current_stripped ~ /^defer / || 
             current_stripped ~ /^go /) && prev_line !~ /^\s*$/ && prev_line != "" &&
            prev_stripped !~ /^{$/ && prev_stripped !~ /^}/)) {
            print ""
        }
        
        # PATTERN 6: too many statements above range/if/defer
        # Add blank line when too many consecutive statements
        if ((current_stripped ~ /^(for.*range|if |defer )/) && 
            prev_line !~ /^\s*$/ && prev_prev_line !~ /^\s*$/ && 
            prev_line != "" && prev_prev_line != "") {
            print ""
        }
        
        print current_line
        
        # Update tracking variables
        prev_prev_line = prev_line
        prev_line = current_line
    }
    ' "$file" > "$temp_file"
    
    # Apply changes
    if ! cmp -s "$file" "$temp_file"; then
        mv "$temp_file" "$file"
        echo "  ✅ APPLIED FIXES TO $file"
    else
        rm "$temp_file"
        echo "  ⏭️  NO CHANGES NEEDED FOR $file"
    fi
done

# Remove leading/trailing whitespace issues
echo "🧹 CLEANING UP WHITESPACE..."
for file in $violation_files; do
    # Remove unnecessary leading whitespace (leading-whitespace)
    sed -i '' '/^[[:space:]]*$/N;/^[[:space:]]*\n[[:space:]]*$/d' "$file" 2>/dev/null || true
    
    # Remove trailing whitespace (trailing-whitespace) 
    sed -i '' 's/[[:space:]]*$//' "$file" 2>/dev/null || true
done

# Final count
echo ""
echo "📊 COUNTING VIOLATIONS AFTER COMPREHENSIVE FIX..."
after_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')

improvement=$((before_count - after_count))
echo ""
echo "🎯 FINAL RESULTS:"
echo "   BEFORE: $before_count violations"
echo "   AFTER:  $after_count violations" 
echo "   FIXED:  $improvement violations"

if [ $improvement -gt 0 ]; then
    percentage=$(( (improvement * 100) / before_count ))
    echo "   SUCCESS: $percentage% improvement!"
    
    if [ $after_count -eq 0 ]; then
        echo ""
        echo "🏆 TOTAL VICTORY! ALL WSL_V5 VIOLATIONS ELIMINATED!"
        echo "🚀 1,044 → 0 WSL_V5 ISSUES - MISSION ACCOMPLISHED!"
    else
        echo ""
        echo "📋 REMAINING ISSUES TO ANALYZE:"
        golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | head -5
    fi
else
    echo "❌ NO IMPROVEMENT - PATTERNS NEED REFINEMENT"
    echo "📋 SAMPLE UNRESOLVED ISSUES:"
    golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | head -5
fi

echo ""
echo "✨ COMPREHENSIVE WSL_V5 AUTOMATION COMPLETED!"