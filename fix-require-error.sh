#!/bin/bash

# Fix require-error testifylint violations by changing assert.Error to require.Error
echo "Fixing require-error testifylint violations..."

find services/gateway services/router -name "*.go" -type f -exec sed -i.bak -E '
s/assert\.Error\(t, /require.Error(t, /g
s/assert\.NoError\(t, /require.NoError(t, /g
' {} \;

# Clean up backup files
find services/gateway services/router -name "*.go.bak" -delete

echo "require-error fixes applied"