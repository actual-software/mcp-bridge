#!/bin/bash

# Update coverage badge in README.md based on current coverage

set -euo pipefail

# Get current coverage from quick check
echo "🔍 Checking current coverage..."

# Run coverage and extract percentages
ROUTER_COV=$(cd services/router && timeout 60s go test -coverprofile=temp.out ./internal/router/... >/dev/null 2>&1 && go tool cover -func=temp.out | tail -1 | awk '{print $NF}' | sed 's/%//' || echo "0")
GATEWAY_COV=$(cd services/gateway && timeout 60s go test -coverprofile=temp.out ./internal/auth/... ./internal/backends/... ./internal/health/... >/dev/null 2>&1 && go tool cover -func=temp.out | tail -1 | awk '{print $NF}' | sed 's/%//' || echo "0")
CORE_COV=$(timeout 30s go test -coverprofile=temp.out ./pkg/common/config/... ./pkg/common/errors/... >/dev/null 2>&1 && go tool cover -func=temp.out | tail -1 | awk '{print $NF}' | sed 's/%//' || echo "0")

# Clean up temp files
rm -f services/router/temp.out services/gateway/temp.out temp.out

# Calculate weighted average (Router 40% + Gateway 40% + Core 20%)
WEIGHTED_AVG=$(python3 -c "print(f'{($ROUTER_COV * 0.4 + $GATEWAY_COV * 0.4 + $CORE_COV * 0.2):.1f}')")

echo "📊 Coverage Results:"
echo "  Router: ${ROUTER_COV}%"
echo "  Gateway: ${GATEWAY_COV}%"
echo "  Core: ${CORE_COV}%"
echo "  Weighted Average: ${WEIGHTED_AVG}%"

# Determine badge color
if (( $(python3 -c "print(1 if $WEIGHTED_AVG >= 80 else 0)") )); then
    COLOR="brightgreen"
elif (( $(python3 -c "print(1 if $WEIGHTED_AVG >= 70 else 0)") )); then
    COLOR="green"
elif (( $(python3 -c "print(1 if $WEIGHTED_AVG >= 60 else 0)") )); then
    COLOR="yellow"
else
    COLOR="red"
fi

# Format for URL (replace . with %2E for decimal)
WEIGHTED_URL=$(echo "$WEIGHTED_AVG" | sed 's/\./%2E/')

# New badge URL
NEW_BADGE="![Coverage](https://img.shields.io/badge/coverage-${WEIGHTED_URL}%25-${COLOR})"

echo "🏷️  New badge: $NEW_BADGE"

# Update README.md if it exists
if [[ -f "README.md" ]]; then
    # Check if coverage badge exists and update it
    if grep -q "!\[Coverage\]" README.md; then
        # Replace existing coverage badge
        sed -i.backup "s|!\[Coverage\]([^)]*)|$NEW_BADGE|g" README.md
        echo "✅ Updated existing coverage badge in README.md"
        echo "📄 Backup saved as README.md.backup"
    else
        echo "ℹ️  No existing coverage badge found in README.md"
        echo "📋 Add this badge to your README.md:"
        echo "   $NEW_BADGE"
    fi
else
    echo "📋 README.md not found. Badge URL:"
    echo "   $NEW_BADGE"
fi

# Update COVERAGE.md if it exists
if [[ -f "COVERAGE.md" ]]; then
    sed -i.backup "s|!\[Coverage\]([^)]*)|$NEW_BADGE|g" COVERAGE.md
    sed -i.backup2 "s/Router Service\*\*: [0-9.]*%/Router Service**: ${ROUTER_COV}%/g" COVERAGE.md
    sed -i.backup3 "s/Gateway Service\*\*: [0-9.]*%/Gateway Service**: ${GATEWAY_COV}%/g" COVERAGE.md
    sed -i.backup4 "s/Core Libraries\*\*: [0-9.]*%/Core Libraries**: ${CORE_COV}%/g" COVERAGE.md
    sed -i.backup5 "s/Weighted Production Average: \*\*[0-9.]*%\*\*/Weighted Production Average: **${WEIGHTED_AVG}%**/g" COVERAGE.md
    rm -f COVERAGE.md.backup*
    echo "✅ Updated COVERAGE.md with current metrics"
fi

echo ""
echo "🎯 Summary:"
echo "  Current production coverage: ${WEIGHTED_AVG}%"
echo "  Badge color: $COLOR"
echo "  All targets met: $(if (( $(python3 -c "print(1 if $WEIGHTED_AVG >= 60 else 0)") )); then echo "✅ Yes"; else echo "❌ No"; fi)"