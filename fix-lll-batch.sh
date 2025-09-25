#!/bin/bash

# Fix function signatures with many parameters in router_test.go
file="services/router/internal/router/router_test.go"

echo "Fixing lll issues in $file..."

# Use sed to fix the remaining long function signatures
# This will handle the common pattern of func name(param1 type1, param2 type2, ...) return_type
sed -i.bak -E '
/^func [^(]+\([^)]{120,}\) \{?$/ {
    s/^(func [^(]+)\(([^,]+), ([^,]+), ([^,]+), ([^)]+)\) \{?$/\1(\n\t\2,\n\t\3,\n\t\4,\n\t\5,\n) {/
}
' "$file"

# Clean up backup file
rm -f "${file}.bak" 2>/dev/null

echo "Fixed function signatures in $file"