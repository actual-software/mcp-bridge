#!/bin/bash

# Run Claude Code E2E Test
# This script sets up and runs the Claude Code integration E2E test

set -e

echo "🚀 Starting Claude Code E2E Test"

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Docker is not running. Please start Docker and try again."
    exit 1
fi

# Clean up any existing containers
echo "🧹 Cleaning up existing containers..."
docker-compose -f docker-compose.claude-code.yml down --remove-orphans 2>/dev/null || true

# Build required binaries
echo "🔨 Building MCP binaries..."
cd ../../../services/gateway && make build
cd ../router && make build
cd ../../test/e2e/full_stack

# Generate certificates if they don't exist
if [ ! -f "certs/tls.crt" ]; then
    echo "🔐 Generating TLS certificates..."
    ./scripts/generate-certs.sh
fi

# Run the E2E test
echo "🧪 Running Claude Code E2E Test..."
go test -v -run TestClaudeCodeE2E -timeout 15m

echo "✅ Claude Code E2E Test completed!"

# Optional: Keep containers running for manual testing
if [ "$1" = "--keep-running" ]; then
    echo "🔄 Containers are still running for manual testing."
    echo "   Use 'docker-compose -f docker-compose.claude-code.yml down' to stop them."
    echo ""
    echo "   Available containers:"
    echo "   - claude-code-container: docker exec -it full_stack_claude-code-container_1 sh"
    echo "   - mcp-router: docker exec -it full_stack_mcp-router_1 sh"
    echo "   - gateway: docker exec -it full_stack_gateway_1 sh"
else
    echo "🧹 Cleaning up containers..."
    docker-compose -f docker-compose.claude-code.yml down --remove-orphans
fi