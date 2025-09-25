#!/bin/bash

# Enhanced wsl_v5 fixer - targets specific patterns from golangci-lint output

set -e

echo "🔧 Enhanced wsl_v5 fixer - targeting specific missing whitespace patterns..."

# Count issues before
echo "📊 Counting wsl_v5 issues before fixing..."
before_count=$(timeout 60 golangci-lint run --enable-only=wsl_v5 ./services/router/... ./services/gateway/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "Found $before_count wsl_v5 issues before fixing"

# Find all Go files in router and gateway services
find ./services/router ./services/gateway -name "*.go" \
    -not -path "./vendor/*" \
    -not -path "./.git/*" \
    | while read -r file; do
    
    echo "Processing: $file"
    
    # Create a temp file
    temp_file="${file}.tmp"
    
    # Use more sophisticated AWK processing
    awk '
    BEGIN { 
        prev_line = ""; 
        prev_prev_line = "";
        line_count = 0;
        non_blank_count = 0;
    }
    {
        current_line = $0
        line_count++
        
        # Track non-blank lines
        if (current_line !~ /^\s*$/ && current_line !~ /^\s*\/\//) {
            non_blank_count++
        } else {
            non_blank_count = 0
        }
        
        # Pattern: "too many statements above if"
        if (current_line ~ /^\s*if / && prev_line != "" && 
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
            !match(prev_line, /^\s*}/) && non_blank_count > 1) {
            print ""
        }
        
        # Pattern: "never cuddle decl" - var declarations need blank line
        if (current_line ~ /^\s*var / && prev_line != "" && 
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
            !match(prev_line, /^\s*{/) && !match(prev_line, /^\s*}/)) {
            print ""
        }
        
        # Pattern: "invalid statement above go" - go routines need blank line  
        if (current_line ~ /^\s*go (func|[a-zA-Z])/ && prev_line != "" &&
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//)) {
            print ""
        }
        
        # Pattern: "too many lines above return" - returns after multiple statements
        if (current_line ~ /^\s*return / && prev_line != "" &&
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
            !match(prev_line, /^\s*}/) && non_blank_count > 2) {
            print ""
        }
        
        # Pattern: assignment/declaration cuddle issues
        if (current_line ~ /^\s*[a-zA-Z_][a-zA-Z0-9_]*.*:=/ && prev_line != "" &&
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
            !match(prev_line, /^\s*{/) && 
            (match(prev_line, /^\s*(if|for|switch|select)/) || non_blank_count > 2)) {
            print ""
        }
        
        # Pattern: defer statements that need spacing
        if (current_line ~ /^\s*defer / && prev_line != "" &&
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
            !match(prev_line, /^\s*{/) && !match(prev_line, /^\s*}/) &&
            !match(prev_line, /^\s*defer/) && non_blank_count > 1) {
            print ""
        }
        
        print current_line
        
        # Update tracking variables
        prev_prev_line = prev_line
        prev_line = current_line
        
        # Reset counter on blank lines
        if (current_line ~ /^\s*$/) {
            non_blank_count = 0
        }
    }
    ' "$file" > "$temp_file"
    
    # Replace original file if changes were made
    if ! cmp -s "$file" "$temp_file"; then
        mv "$temp_file" "$file"
        echo "  ✅ Fixed wsl_v5 issues in $file"
    else
        rm "$temp_file"
        echo "  ⏭️  No changes needed in $file"
    fi
done

echo "📊 Counting wsl_v5 issues after fixing..."
after_count=$(timeout 60 golangci-lint run --enable-only=wsl_v5 ./services/router/... ./services/gateway/... 2>/dev/null | grep "wsl_v5" | wc -l | tr -d ' ')
echo "Found $after_count wsl_v5 issues after fixing"

improvement=$((before_count - after_count))
if [ $improvement -gt 0 ]; then
    echo "🎯 SUCCESS: Fixed $improvement wsl_v5 issues! ($before_count → $after_count)"
else
    echo "⚠️  No improvement detected. Issues may be in untargeted patterns."
fi

echo "✨ Enhanced wsl_v5 fix script completed!"