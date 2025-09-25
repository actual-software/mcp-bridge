#!/bin/bash

# Script to fix common lll (line length limit) issues across the repository
# Focuses on the most common patterns that can be automatically fixed

echo "Starting to fix lll issues across repository..."

# Find all Go files in services/ directory
find services/ -name "*.go" | while read file; do
    echo "Processing $file..."
    
    # Fix long function signatures - simple case with 5+ parameters
    sed -i.bak -E 's/^func ([^(]+)\(([^)]{120,})\) \{$/func \1(\n\t\2,\n) {/g' "$file"
    
    # Fix long struct literal lines by adding newlines after commas in long lines
    perl -i -pe 's/^(.{121,})$/$1/g; s/(\{[^}]{50,}), /$1,\n\t\t/g' "$file"
    
    # Remove backup files
    rm -f "${file}.bak"
done

echo "Done processing files. Re-running linter to check progress..."