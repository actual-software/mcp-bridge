#!/bin/bash

# Test the enhanced wsl_v5 script on a single file

set -e

file="services/router/cmd/mcp-router/metrics_test.go"

echo "🧪 Testing enhanced wsl_v5 script on: $file"

# Show issues before
echo "📋 wsl_v5 issues BEFORE:"
golangci-lint run --enable-only=wsl_v5 "$file" 2>/dev/null | grep "wsl_v5" | head -5 || echo "No issues or error"

# Apply the fix
echo ""
echo "🔧 Applying fix..."
temp_file="${file}.tmp"

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
    
    # Pattern: assignment/declaration cuddle issues
    if (current_line ~ /^\s*[a-zA-Z_][a-zA-Z0-9_]*.*:=/ && prev_line != "" &&
        !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
        !match(prev_line, /^\s*{/) && 
        (match(prev_line, /^\s*(if|for|switch|select)/) || non_blank_count > 2)) {
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

# Apply changes
mv "$temp_file" "$file"

echo "✅ Fix applied!"
echo ""

# Show issues after
echo "📋 wsl_v5 issues AFTER:"
golangci-lint run --enable-only=wsl_v5 "$file" 2>/dev/null | grep "wsl_v5" | head -5 || echo "No issues remaining or error"

echo ""
echo "🎯 Single file test completed!"