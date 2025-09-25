#!/bin/bash

# Proven wsl_v5 fixer - using only verified working patterns

set -e

echo "✅ Proven wsl_v5 fixer - using manually verified patterns..."

cd /Users/poile/repos/mcp

# Count issues before
before_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "📊 Found $before_count wsl_v5 issues before fixing"

if [ "$before_count" -eq 0 ]; then
    echo "🎉 No wsl_v5 issues found!"
    exit 0
fi

# Apply the two proven patterns we know work
files_fixed=0

# Pattern 1: Add blank line after t.Parallel() before assignments
echo "🔧 Applying Pattern 1: blank line after t.Parallel()..."
find ./services/gateway ./services/router -name "*_test.go" | while read -r file; do
    temp_file="${file}.tmp"
    
    # Use sed to add blank line after t.Parallel() if not already there
    awk '
    BEGIN { prev_line = "" }
    {
        current_line = $0
        
        # If current line is an assignment and previous line is t.Parallel(), add blank line
        if (current_line ~ /^\s*[a-zA-Z_][a-zA-Z0-9_]*.*:=/ && prev_line ~ /t\.Parallel\(\)/) {
            print ""
        }
        
        print current_line
        prev_line = current_line
    }
    ' "$file" > "$temp_file"
    
    if ! cmp -s "$file" "$temp_file"; then
        mv "$temp_file" "$file"
        files_fixed=$((files_fixed + 1))
        echo "  ✅ Fixed t.Parallel() spacing in $file"
    else
        rm "$temp_file"
    fi
done

# Pattern 2: Add blank line after require.Error() before nested if statements  
echo "🔧 Applying Pattern 2: blank line after require.Error()..."
find ./services/gateway ./services/router -name "*_test.go" | while read -r file; do
    temp_file="${file}.tmp"
    
    awk '
    BEGIN { prev_line = "" }
    {
        current_line = $0
        
        # If current line is nested if and previous line is require.Error(), add blank line
        if (current_line ~ /^\s+if .* != ""/ && prev_line ~ /require\.Error\(t, err\)/) {
            print ""
        }
        
        print current_line
        prev_line = current_line
    }
    ' "$file" > "$temp_file"
    
    if ! cmp -s "$file" "$temp_file"; then
        mv "$temp_file" "$file"
        files_fixed=$((files_fixed + 1))
        echo "  ✅ Fixed require.Error() spacing in $file"
    else
        rm "$temp_file"
    fi
done

echo "📝 Applied fixes to $files_fixed files"

# Final count
echo ""
echo "📊 Counting wsl_v5 issues after fixing..."  
after_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "Found $after_count wsl_v5 issues after fixing"

improvement=$((before_count - after_count))
if [ $improvement -gt 0 ]; then
    echo "🎉 SUCCESS: Fixed $improvement wsl_v5 issues! ($before_count → $after_count)"
    percentage=$(( (improvement * 100) / before_count ))
    echo "📈 Improvement: $percentage% of issues resolved"
    
    echo ""
    echo "🎯 PROGRESS UPDATE:"
    echo "   • Started with: 1,044 wsl_v5 issues (from repository audit)"  
    echo "   • Current count: $after_count issues"
    echo "   • Issues fixed this session: $improvement"
    
    if [ $after_count -gt 0 ]; then
        echo ""
        echo "📋 Next patterns to investigate:"
        golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | head -3
    fi
else
    echo "⚠️  No improvement from these patterns."
fi

echo "✨ Proven wsl_v5 fix completed!"