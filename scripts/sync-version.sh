#!/bin/bash

# Version synchronization script
# Ensures VERSION file is the single source of truth

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get version from VERSION file
if [ ! -f "VERSION" ]; then
    echo "Error: VERSION file not found"
    exit 1
fi

VERSION=$(cat VERSION)
echo -e "${GREEN}Synchronizing version: ${VERSION}${NC}"

# Update Go files
echo "Updating Go version constants..."

# Update gateway main.go
if [ -f "services/gateway/cmd/mcp-gateway/main.go" ]; then
    sed -i.bak "s/Version.*=.*\".*\"/Version   = \"v${VERSION}\"/" services/gateway/cmd/mcp-gateway/main.go
    rm services/gateway/cmd/mcp-gateway/main.go.bak
    echo "  ✓ Updated services/gateway/cmd/mcp-gateway/main.go"
fi

# Update router main.go
if [ -f "services/router/cmd/mcp-router/main.go" ]; then
    sed -i.bak "s/Version.*=.*\".*\"/Version   = \"v${VERSION}\"/" services/router/cmd/mcp-router/main.go
    rm services/router/cmd/mcp-router/main.go.bak
    echo "  ✓ Updated services/router/cmd/mcp-router/main.go"
fi

# Update package.json if it exists
if [ -f "package.json" ]; then
    # Use a more portable approach for JSON update
    if command -v jq &> /dev/null; then
        jq ".version = \"${VERSION}\"" package.json > package.json.tmp && mv package.json.tmp package.json
        echo "  ✓ Updated package.json"
    else
        sed -i.bak "s/\"version\": \".*\"/\"version\": \"${VERSION}\"/" package.json
        rm package.json.bak
        echo "  ✓ Updated package.json (using sed)"
    fi
fi

# Update Helm Chart.yaml files if they exist
for chart in helm/*/Chart.yaml; do
    if [ -f "$chart" ]; then
        sed -i.bak "s/^version:.*/version: ${VERSION}/" "$chart"
        sed -i.bak "s/^appVersion:.*/appVersion: \"v${VERSION}\"/" "$chart"
        rm "${chart}.bak"
        echo "  ✓ Updated $chart"
    fi
done

# Update Docker Compose files with image tags
for compose in docker-compose*.yml; do
    if [ -f "$compose" ] && grep -q "mcp-gateway:\|mcp-router:" "$compose"; then
        sed -i.bak "s/mcp-gateway:.*$/mcp-gateway:v${VERSION}/" "$compose"
        sed -i.bak "s/mcp-router:.*$/mcp-router:v${VERSION}/" "$compose"
        rm "${compose}.bak"
        echo "  ✓ Updated $compose"
    fi
done

# Update Kubernetes manifests
for manifest in deployments/**/*.yaml deployments/**/*.yml; do
    if [ -f "$manifest" ] && grep -q "image:.*mcp-" "$manifest"; then
        sed -i.bak "s|\(image:.*mcp-gateway\):.*|\1:v${VERSION}|" "$manifest"
        sed -i.bak "s|\(image:.*mcp-router\):.*|\1:v${VERSION}|" "$manifest"
        rm "${manifest}.bak"
        echo "  ✓ Updated $manifest"
    fi
done

echo -e "${GREEN}✅ Version synchronization complete!${NC}"
echo ""
echo "Version ${VERSION} has been synchronized across:"
echo "  • Go source files"
echo "  • package.json"
echo "  • Helm charts"
echo "  • Docker Compose files"
echo "  • Kubernetes manifests"