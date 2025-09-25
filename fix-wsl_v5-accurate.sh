#!/bin/bash

# Accurate wsl_v5 fixer - handles both missing and unnecessary whitespace

set -e

echo "🎯 Accurate wsl_v5 fixer - targeting real violation patterns..."

cd /Users/poile/repos/mcp

# Count issues before
before_count=$(golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "📊 Found $before_count wsl_v5 issues before fixing"

if [ "$before_count" -eq 0 ]; then
    echo "🎉 No wsl_v5 issues found!"
    exit 0
fi

# Show specific issues we're going to fix
echo "📋 Sample issues to fix:"
golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep -E "(unnecessary whitespace|missing whitespace)" | head -3

# Function to fix a specific file
fix_file() {
    local file="$1"
    local temp_file="${file}.tmp"
    
    # Create enhanced AWK script
    awk '
    BEGIN { 
        prev_line = ""
        prev_was_blank = 0
    }
    {
        current_line = $0
        current_is_blank = (current_line ~ /^\s*$/)
        
        # Remove unnecessary blank lines before certain statements
        # Pattern: "unnecessary whitespace (leading-whitespace)"
        if (current_is_blank && prev_was_blank) {
            # Skip consecutive blank lines in certain contexts
            next
        }
        
        # Add missing blank lines before if statements
        # Pattern: "missing whitespace above this line (invalid statement above if)"
        if (current_line ~ /^\s*if / && !prev_was_blank && prev_line != "" && 
            !match(prev_line, /^\s*{/) && !match(prev_line, /^\s*}/)) {
            print ""
        }
        
        # Add missing blank lines before assignments
        # Pattern: "missing whitespace above this line (invalid statement above assign)" 
        if (current_line ~ /^\s*[a-zA-Z_][a-zA-Z0-9_]*.*=/ && !prev_was_blank && prev_line != "" &&
            !match(prev_line, /^\s*{/) && !match(prev_line, /^\s*var /)) {
            print ""
        }
        
        # Add missing blank lines before go statements
        if (current_line ~ /^\s*go / && !prev_was_blank && prev_line != "") {
            print ""
        }
        
        # Add missing blank lines before var declarations
        if (current_line ~ /^\s*var / && !prev_was_blank && prev_line != "" && 
            !match(prev_line, /^\s*{/)) {
            print ""
        }
        
        print current_line
        
        prev_line = current_line
        prev_was_blank = current_is_blank
    }
    ' "$file" > "$temp_file"
    
    # Apply changes if different
    if ! cmp -s "$file" "$temp_file"; then
        mv "$temp_file" "$file"
        return 0  # Changes made
    else
        rm "$temp_file"
        return 1  # No changes
    fi
}

# Process files in packages that have wsl_v5 issues
files_fixed=0
total_files=0

# Get files from packages with issues
golangci-lint run --enable-only=wsl_v5 ./services/gateway/... ./services/router/... 2>/dev/null | grep "wsl_v5" | cut -d: -f1 | sort -u | while read -r file; do
    if [ -f "$file" ]; then
        echo "🔧 Processing: $file"
        total_files=$((total_files + 1))
        
        if fix_file "$file"; then
            files_fixed=$((files_fixed + 1))
            echo "  ✅ Applied fixes to $file"
        else
            echo "  ⏭️  No changes needed for $file"
        fi
    fi
done

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
else
    echo "⚠️  No improvement detected. Patterns may need further refinement."
fi

echo "✨ Accurate wsl_v5 fix completed!"