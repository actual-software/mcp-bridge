.PHONY: help test build clean lint fmt coverage install check check-go-version

# Extract Go version from go.mod (industry standard approach)
GO_VERSION := $(shell awk '/^go / {print $$2}' go.mod)

# Default target
all: test

# =============================================================================
# CORE DEVELOPMENT TARGETS
# =============================================================================

# Run all tests
test:
	@echo "🧪 Running all tests..."
	@$(MAKE) -C services/gateway test
	@$(MAKE) -C services/router test
	@echo "✅ All tests complete!"

# Build all services
build:
	@echo "🏗️  Building all services (Go $(GO_VERSION))..."
	@$(MAKE) -C services/gateway build
	@$(MAKE) -C services/router build
	@echo "✅ Build complete!"

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	@$(MAKE) -C services/gateway clean
	@$(MAKE) -C services/router clean
	@rm -f *.coverprofile *.out
	@echo "✅ Clean complete!"

# =============================================================================
# CODE QUALITY TARGETS
# =============================================================================

# Run linters
lint:
	@echo "🔍 Running linters..."
	@$(MAKE) -C services/gateway lint
	@$(MAKE) -C services/router lint
	@echo "✅ Linting complete!"

# Format code
fmt:
	@echo "🎨 Formatting code..."
	@$(MAKE) -C services/gateway fmt
	@$(MAKE) -C services/router fmt
	@echo "✅ Formatting complete!"

# Check Go version compatibility
check-go-version:
	@INSTALLED_GO=$$(go version | awk '{print $$3}' | sed 's/go//'); \
	REQUIRED_GO=$(GO_VERSION); \
	echo "Required Go version: $$REQUIRED_GO"; \
	echo "Installed Go version: $$INSTALLED_GO"; \
	if [ "$$(printf '%s\n' "$$REQUIRED_GO" "$$INSTALLED_GO" | sort -V | head -n1)" != "$$REQUIRED_GO" ]; then \
		echo "❌ Go version $$INSTALLED_GO is older than required $$REQUIRED_GO"; \
		echo "   Upgrade with: brew upgrade go"; \
		exit 1; \
	else \
		echo "✅ Go version is compatible"; \
	fi

# Run all quality checks
check: lint test
	@echo "✅ All quality checks passed!"

# =============================================================================
# COVERAGE TARGETS
# =============================================================================

# Quick coverage check (30 seconds)
coverage:
	@echo "📊 Quick coverage check..."
	@echo ""
	@echo "=== Router Service (Primary) ==="
	@cd services/router && go test -coverprofile=coverage.out -timeout=1m ./internal/router/... && go tool cover -func=coverage.out | tail -1
	@echo ""
	@echo "=== Gateway Service ==="
	@cd services/gateway && go test -coverprofile=coverage.out -timeout=1m ./internal/auth/... ./internal/backends/... ./internal/health/... && go tool cover -func=coverage.out | tail -1
	@echo ""
	@echo "=== Core Libraries ==="
	@go test -coverprofile=coverage.out -timeout=30s ./pkg/common/config/... ./pkg/common/errors/... && go tool cover -func=coverage.out | tail -1
	@rm -f services/*/coverage.out coverage.out

# Comprehensive coverage report
coverage-report:
	@echo "📋 Generating comprehensive coverage report..."
	@./scripts/coverage-report.sh

# Update coverage badges
coverage-badge:
	@echo "🏷️  Updating coverage badges..."
	@./scripts/update-coverage-badge.sh

# Generate HTML coverage reports
coverage-html:
	@echo "🌐 Generating HTML coverage reports..."
	@$(MAKE) -C services/gateway test-coverage-html
	@$(MAKE) -C services/router test-coverage-html
	@echo "✅ HTML reports available in services/*/coverage.html"

# =============================================================================
# DEVELOPMENT TARGETS
# =============================================================================

# Install development dependencies
install:
	@echo "📦 Installing dependencies..."
	@$(MAKE) -C services/gateway deps
	@$(MAKE) -C services/router deps
	@echo "✅ Dependencies installed!"

# Quick development check (for git hooks)
dev-check: fmt lint
	@echo "🔬 Running quick unit tests..."
	@$(MAKE) -C services/gateway test-unit
	@$(MAKE) -C services/router test-unit
	@echo "✅ Development checks passed!"

# Development workflow
dev: clean build test coverage
	@echo "🎉 Development workflow complete!"

# =============================================================================
# SERVICE-SPECIFIC TARGETS
# =============================================================================

# Gateway service targets
test-gateway:
	@$(MAKE) -C services/gateway test

build-gateway:
	@$(MAKE) -C services/gateway build

# Router service targets
test-router:
	@$(MAKE) -C services/router test

build-router:
	@$(MAKE) -C services/router build

# =============================================================================
# TESTING TARGETS
# =============================================================================

# Run unit tests only
test-unit:
	@echo "🧪 Running unit tests..."
	@$(MAKE) -C services/gateway test-unit
	@$(MAKE) -C services/router test-unit

# Run integration tests
test-integration:
	@echo "🔗 Running integration tests..."
	@$(MAKE) -C services/gateway test-integration
	@$(MAKE) -C services/router test-integration

# Run benchmarks
benchmark:
	@echo "⚡ Running benchmarks..."
	@$(MAKE) -C services/gateway benchmark
	@$(MAKE) -C services/router benchmark

# =============================================================================
# AUDIT TARGETS
# =============================================================================

# Run comprehensive repository audit
audit: audit-lint audit-test audit-coverage audit-report
	@echo "✅ Full audit complete!"

# Run linting audit
audit-lint:
	@echo "🔍 Running linting audit..."
	@./scripts/audit-lint.sh

# Run test audit
audit-test:
	@echo "🧪 Running test audit..."
	@./scripts/audit-test.sh

# Run coverage audit
audit-coverage:
	@echo "📊 Running coverage audit..."
	@./scripts/audit-coverage.sh

# Generate final audit report
audit-report:
	@echo "📋 Generating final audit report..."
	@./scripts/audit-report.sh

# Clean audit results
audit-clean:
	@echo "🧹 Cleaning audit results..."
	@rm -rf audit-results
	@echo "✅ Audit results cleaned!"

# =============================================================================
# UTILITY TARGETS
# =============================================================================

# Verify build creates correct artifacts
verify:
	@echo "✅ Verifying builds..."
	@test -f services/gateway/bin/mcp-gateway || (echo "❌ Gateway binary missing" && exit 1)
	@test -f services/router/bin/mcp-router || (echo "❌ Router binary missing" && exit 1)
	@echo "✅ All binaries present!"

# Display help
help:
	@echo "MCP Bridge - Streamlined Makefile"
	@echo ""
	@echo "🚀 CORE COMMANDS:"
	@echo "  make test        - Run all tests"
	@echo "  make build       - Build all services"
	@echo "  make clean       - Clean build artifacts"
	@echo "  make dev         - Complete development workflow"
	@echo ""
	@echo "📊 COVERAGE:"
	@echo "  make coverage         - Quick coverage check (30s)"
	@echo "  make coverage-report  - Comprehensive coverage analysis"
	@echo "  make coverage-badge   - Update README badges"
	@echo "  make coverage-html    - Generate HTML reports"
	@echo ""
	@echo "🔍 CODE QUALITY:"
	@echo "  make lint        - Run linters"
	@echo "  make fmt         - Format code"
	@echo "  make check       - Run all quality checks"
	@echo "  make dev-check   - Quick pre-commit checks"
	@echo ""
	@echo "🎯 SERVICE-SPECIFIC:"
	@echo "  make test-gateway     - Test gateway only"
	@echo "  make test-router      - Test router only"
	@echo "  make build-gateway    - Build gateway only"
	@echo "  make build-router     - Build router only"
	@echo ""
	@echo "🧪 SPECIALIZED TESTING:"
	@echo "  make test-unit        - Unit tests only"
	@echo "  make test-integration - Integration tests only"
	@echo "  make benchmark        - Performance benchmarks"
	@echo ""
	@echo "📋 AUDIT:"
	@echo "  make audit            - Run comprehensive audit (lint + test + coverage)"
	@echo "  make audit-lint       - Run linting audit only"
	@echo "  make audit-test       - Run test audit only"
	@echo "  make audit-coverage   - Run coverage audit only"
	@echo "  make audit-report     - Generate final audit report"
	@echo "  make audit-clean      - Clean audit results"
	@echo ""
	@echo "🛠️  UTILITIES:"
	@echo "  make install     - Install dependencies"
	@echo "  make verify      - Verify build artifacts"
	@echo "  make help        - Show this help"
	@echo ""
	@echo "For advanced features, see the old Makefile or individual service directories."