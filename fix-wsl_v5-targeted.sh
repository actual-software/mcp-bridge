#!/bin/bash

# Targeted wsl_v5 fixer - focuses on specific patterns from working examples

set -e

echo "🎯 Targeted wsl_v5 fixer - focusing on proven patterns..."

# Target files that are likely to compile (avoid test files with missing test utilities)
find ./services/gateway ./services/router -name "*.go" \
    -not -path "*/test*" \
    -not -path "*_test.go" \
    -not -path "*/internal/errors/*" \
    -not -path "*/cmd/mcp-*/*" \
    | head -20 | while read -r file; do
    
    echo "Processing: $file"
    
    # Check if file has typecheck errors first
    if golangci-lint run --enable-only=typecheck "$file" 2>/dev/null | grep -q "typecheck"; then
        echo "  ⏭️  Skipping $file (has typecheck errors)"
        continue
    fi
    
    # Check if it has wsl_v5 issues
    wsl_issues=$(golangci-lint run --enable-only=wsl_v5 "$file" 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
    
    if [ "$wsl_issues" -eq 0 ]; then
        echo "  ⏭️  No wsl_v5 issues in $file"
        continue
    fi
    
    echo "  🔍 Found $wsl_issues wsl_v5 issues, applying fixes..."
    
    # Create a temp file
    temp_file="${file}.tmp"
    
    # Apply the precise fixes we know work
    awk '
    BEGIN { prev_line = ""; }
    {
        current_line = $0
        
        # Add blank line before var declarations (never cuddle decl)
        if (current_line ~ /^\s*var / && prev_line != "" && 
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
            !match(prev_line, /^\s*{/)) {
            print ""
        }
        
        # Add blank line before assignments (invalid statement above assign)  
        if (current_line ~ /^\s*[a-zA-Z_][a-zA-Z0-9_]*\s*=\s*[^=]/ && prev_line != "" &&
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
            !match(prev_line, /^\s*{/) && !match(prev_line, /^\s*var/)) {
            print ""
        }
        
        # Add blank line before go func() (no shared variables above go)
        if (current_line ~ /^\s*go func\(\)/ && prev_line != "" &&
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//)) {
            print ""
        }
        
        print current_line
        prev_line = current_line
    }
    ' "$file" > "$temp_file"
    
    # Check if changes were made
    if ! cmp -s "$file" "$temp_file"; then
        mv "$temp_file" "$file"
        
        # Verify the fix worked
        new_issues=$(golangci-lint run --enable-only=wsl_v5 "$file" 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
        improvement=$((wsl_issues - new_issues))
        
        if [ $improvement -gt 0 ]; then
            echo "  ✅ Fixed $improvement/$wsl_issues wsl_v5 issues in $file"
        else
            echo "  ⚠️  Applied changes but no issues resolved in $file"
        fi
    else
        rm "$temp_file"
        echo "  ⏭️  No pattern matches in $file"
    fi
done

echo "✨ Targeted wsl_v5 fix completed!"