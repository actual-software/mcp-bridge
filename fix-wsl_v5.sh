#!/bin/bash

# Script to fix common wsl_v5 issues by adding blank lines

set -e

echo "🔧 Fixing wsl_v5 issues by adding blank lines..."

# Find all Go files (excluding vendor, .git, etc.)
find . -name "*.go" \
    -not -path "./vendor/*" \
    -not -path "./.git/*" \
    -not -path "./.*/*" \
    | while read -r file; do
    
    echo "Processing: $file"
    
    # Create a temp file
    temp_file="${file}.tmp"
    
    # Process the file line by line
    awk '
    BEGIN { prev_line = ""; prev_type = "" }
    {
        current_line = $0
        current_type = ""
        
        # Detect line types
        if (match(current_line, /^\s*defer /)) current_type = "defer"
        if (match(current_line, /^\s*ctx := /)) current_type = "ctx_assign"
        if (match(current_line, /^\s*return /)) current_type = "return"
        if (match(current_line, /^\s*var /)) current_type = "var_decl"
        if (match(current_line, /^\s*if /)) current_type = "if"
        if (match(current_line, /^\s*go func/)) current_type = "go_func"
        
        # Add blank line before defer if previous line is not blank/comment/brace
        if (current_type == "defer" && prev_line != "" && 
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) && 
            !match(prev_line, /^\s*{/) && !match(prev_line, /^\s*}/) &&
            prev_type != "defer") {
            print ""
        }
        
        # Add blank line before ctx assignment if not already there
        if (current_type == "ctx_assign" && prev_line != "" && 
            !match(prev_line, /^\s*$/)) {
            print ""
        }
        
        # Add blank line before return in certain contexts
        if (current_type == "return" && prev_line != "" && 
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
            !match(prev_line, /^\s*}/) && 
            (match(prev_line, /^\s*[a-zA-Z].*[,;]?\s*$/) || match(prev_line, /err/))) {
            print ""
        }
        
        # Add blank line before var declaration
        if (current_type == "var_decl" && prev_line != "" && 
            !match(prev_line, /^\s*$/) && !match(prev_line, /^\s*\/\//) &&
            !match(prev_line, /^\s*{/)) {
            print ""
        }
        
        # Add blank line before go func
        if (current_type == "go_func" && prev_line != "" && 
            !match(prev_line, /^\s*$/)) {
            print ""
        }
        
        print current_line
        prev_line = current_line
        prev_type = current_type
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

echo "✨ wsl_v5 fix script completed!"