#!/bin/bash

# Package-based wsl_v5 fixer - works around typecheck issues by targeting packages

set -e

echo "🚀 Package-based wsl_v5 fixer - targeting Go packages instead of individual files..."

# Count issues before
echo "📊 Counting wsl_v5 issues before fixing..."
cd /Users/poile/repos/mcp
before_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "Found $before_count wsl_v5 issues before fixing"

if [ "$before_count" -eq 0 ]; then
    echo "🎉 No wsl_v5 issues found! Job already done."
    exit 0
fi

# Get a sample of the issues to understand patterns
echo "📋 Sample issues to fix:"
golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | head -5

# Get all Go packages in gateway and router
packages=$(find ./services/gateway ./services/router -name "*.go" -exec dirname {} \; | sort -u | head -20)

for package_dir in $packages; do
    echo "🔍 Processing package: $package_dir"
    
    # Check if package has wsl_v5 issues
    package_issues=$(golangci-lint run --enable-only=wsl_v5 "$package_dir" 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
    
    if [ "$package_issues" -eq 0 ]; then
        echo "  ⏭️  No wsl_v5 issues in $package_dir"
        continue
    fi
    
    echo "  🔧 Found $package_issues wsl_v5 issues, applying fixes to all files in package..."
    
    # Process all Go files in the package
    find "$package_dir" -name "*.go" | while read -r file; do
        echo "    📝 Processing: $file"
        
        # Create a temp file
        temp_file="${file}.tmp"
        
        # Apply our proven wsl_v5 fixes
        awk '
        BEGIN { prev_line = ""; }
        {
            current_line = $0
            
            # Pattern: missing whitespace above this line (never cuddle decl)
            if (current_line ~ /^\s*var / && prev_line != "" && 
                !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
                !match(prev_line, /^\s*{/) && !match(prev_line, /^\s*}/)) {
                print ""
            }
            
            # Pattern: missing whitespace above this line (invalid statement above assign)
            if (current_line ~ /^\s*[a-zA-Z_][a-zA-Z0-9_]*.*=/ && prev_line != "" &&
                !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
                !match(prev_line, /^\s*{/) && !match(prev_line, /^\s*var /)) {
                print ""
            }
            
            # Pattern: missing whitespace above this line (invalid statement above if)
            if (current_line ~ /^\s*if / && prev_line != "" &&
                !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
                !match(prev_line, /^\s*{/) && !match(prev_line, /^\s*}/)) {
                print ""
            }
            
            # Pattern: missing whitespace above this line (no shared variables above go)
            if (current_line ~ /^\s*go func/ && prev_line != "" &&
                !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//)) {
                print ""
            }
            
            # Pattern: missing whitespace above this line (invalid statement above expr)  
            if (current_line ~ /^\s*t\.Cleanup/ && prev_line != "" &&
                !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//)) {
                print ""
            }
            
            print current_line
            prev_line = current_line
        }
        ' "$file" > "$temp_file"
        
        # Apply changes if different
        if ! cmp -s "$file" "$temp_file"; then
            mv "$temp_file" "$file"
            echo "      ✅ Applied fixes to $file"
        else
            rm "$temp_file"
        fi
    done
    
    # Check improvement for this package
    new_package_issues=$(golangci-lint run --enable-only=wsl_v5 "$package_dir" 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
    improvement=$((package_issues - new_package_issues))
    
    if [ $improvement -gt 0 ]; then
        echo "  🎯 Fixed $improvement/$package_issues issues in $package_dir"
    else
        echo "  ⚠️  No improvement in $package_dir (patterns may not match)"
    fi
done

# Final count
echo ""
echo "📊 Counting wsl_v5 issues after fixing..."
after_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "Found $after_count wsl_v5 issues after fixing"

total_improvement=$((before_count - after_count))
if [ $total_improvement -gt 0 ]; then
    echo "🎉 SUCCESS: Fixed $total_improvement wsl_v5 issues! ($before_count → $after_count)"
    percentage=$(( (total_improvement * 100) / before_count ))
    echo "📈 Improvement: $percentage% of issues resolved"
else
    echo "⚠️  No improvement detected. May need to refine patterns."
fi

echo "✨ Package-based wsl_v5 fix completed!"