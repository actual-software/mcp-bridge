#!/bin/bash

# Standalone E2E test runner script
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Default values
TIMEOUT="15m"
VERBOSE="false"
CLEANUP="true"
QUICK="false"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --quick)
            QUICK="true"
            TIMEOUT="5m"
            shift
            ;;
        --verbose|-v)
            VERBOSE="true"
            shift
            ;;
        --no-cleanup)
            CLEANUP="false"
            shift
            ;;
        --timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --quick          Run quick E2E tests (5min timeout)"
            echo "  --verbose, -v    Enable verbose output"
            echo "  --no-cleanup     Skip cleanup after tests"
            echo "  --timeout DURATION  Set test timeout (default: 15m)"
            echo "  --help, -h       Show this help message"
            echo ""
            echo "Examples:"
            echo "  $0                # Run full E2E tests"
            echo "  $0 --quick       # Run quick E2E tests"
            echo "  $0 --verbose     # Run with verbose output"
            echo "  $0 --no-cleanup  # Skip cleanup for debugging"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

echo "🚀 Starting MCP E2E Tests"
echo "=================================="
echo "Quick mode: $QUICK"
echo "Timeout: $TIMEOUT"
echo "Verbose: $VERBOSE"
echo "Cleanup: $CLEANUP"
echo ""

# Cleanup function
cleanup() {
    if [ "$CLEANUP" = "true" ]; then
        echo ""
        echo "🧹 Cleaning up E2E environment..."
        cd "$E2E_DIR"
        docker-compose -f docker-compose.e2e.yml down -v --remove-orphans >/dev/null 2>&1 || true
        echo "✅ Cleanup complete"
    else
        echo ""
        echo "⚠️  Skipping cleanup (--no-cleanup specified)"
        echo "To cleanup manually: cd $E2E_DIR && docker-compose -f docker-compose.e2e.yml down -v"
    fi
}

# Set up cleanup trap
trap cleanup EXIT

# Change to E2E directory
cd "$E2E_DIR"

# Build binaries
echo "🔨 Building binaries..."
"$SCRIPT_DIR/build-binaries.sh"

# Prepare test args
TEST_ARGS="-timeout=$TIMEOUT"
if [ "$VERBOSE" = "true" ]; then
    TEST_ARGS="$TEST_ARGS -v"
fi
if [ "$QUICK" = "true" ]; then
    TEST_ARGS="$TEST_ARGS -short"
fi

echo ""
echo "🧪 Running E2E tests..."
echo "Test args: go test $TEST_ARGS"
echo ""

# Run tests
if go test $TEST_ARGS; then
    echo ""
    echo "🎉 All E2E tests passed!"
    exit 0
else
    echo ""
    echo "❌ E2E tests failed!"
    
    # Save logs for debugging
    echo ""
    echo "💾 Saving service logs for debugging..."
    LOG_DIR="/tmp/mcp-e2e-logs-$(date +%Y%m%d-%H%M%S)"
    mkdir -p "$LOG_DIR"
    
    echo "Saving logs to: $LOG_DIR"
    docker-compose -f docker-compose.e2e.yml logs --no-color gateway > "$LOG_DIR/gateway.log" 2>/dev/null || true
    docker-compose -f docker-compose.e2e.yml logs --no-color test-mcp-server > "$LOG_DIR/test-mcp-server.log" 2>/dev/null || true
    docker-compose -f docker-compose.e2e.yml logs --no-color redis > "$LOG_DIR/redis.log" 2>/dev/null || true
    
    echo "Service logs saved to: $LOG_DIR"
    exit 1
fi