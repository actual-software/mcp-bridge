#!/bin/bash

# Fix nlreturn violations by adding blank lines before return/continue/break statements
echo "Fixing nlreturn violations..."

# Get all nlreturn violations  
golangci-lint run --timeout=3m ./services/gateway/... ./services/router/... 2>/dev/null | grep "nlreturn" | while read -r line; do
    # Extract file, line number, and violation type
    file=$(echo "$line" | cut -d: -f1)
    linenum=$(echo "$line" | cut -d: -f2)
    
    if [[ "$line" == *"return with no blank line before"* ]]; then
        statement="return"
    elif [[ "$line" == *"continue with no blank line before"* ]]; then
        statement="continue"
    elif [[ "$line" == *"break with no blank line before"* ]]; then
        statement="break"  
    else
        continue
    fi
    
    echo "Adding blank line before $statement in $file:$linenum"
    
    # Add blank line before the return/continue/break statement
    sed -i.bak "${linenum}i\\
" "$file"
    
    # Remove backup file
    rm -f "$file.bak"
done

echo "nlreturn fixes applied"