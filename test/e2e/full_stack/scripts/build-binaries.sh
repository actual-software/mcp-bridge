#!/bin/bash

# Build script for E2E testing binaries
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd)"

echo "Building MCP binaries for E2E testing..."
echo "Project root: $PROJECT_ROOT"

# Build router binary
echo ""
echo "=== Building Router Binary ==="
cd "$PROJECT_ROOT/services/router"
make build

if [ ! -f "bin/mcp-router" ]; then
    echo "❌ Router binary not found after build"
    exit 1
fi

echo "✅ Router binary built successfully"

# Build gateway binary
echo ""
echo "=== Building Gateway Binary ==="
cd "$PROJECT_ROOT/services/gateway"
make build

if [ ! -f "bin/mcp-gateway" ]; then
    echo "❌ Gateway binary not found after build"
    exit 1
fi

echo "✅ Gateway binary built successfully"

# Verify binaries work
echo ""
echo "=== Verifying Binaries ==="

echo "Testing router binary..."
if ! "$PROJECT_ROOT/services/router/bin/mcp-router" --version >/dev/null 2>&1; then
    echo "⚠️  Router binary --version check failed (this might be expected if --version is not implemented)"
fi

echo "Testing gateway binary..."
if ! "$PROJECT_ROOT/services/gateway/bin/mcp-gateway" --version >/dev/null 2>&1; then
    echo "⚠️  Gateway binary --version check failed (this might be expected if --version is not implemented)"
fi

echo ""
echo "🎉 All binaries built successfully!"
echo ""
echo "Built binaries:"
echo "  Router: $PROJECT_ROOT/services/router/bin/mcp-router"
echo "  Gateway: $PROJECT_ROOT/services/gateway/bin/mcp-gateway"