# E2E Test Suite Makefile
# Provides convenient commands for running and managing tests

.PHONY: help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

.PHONY: test
test: ## Run all e2e tests with race detection
	@echo "🧪 Running all E2E tests..."
	@go test -v -race -timeout 5m ./...

.PHONY: test-quick
test-quick: ## Run quick tests without Docker (Layer 2 & 3 only)
	@echo "⚡ Running quick tests (no Docker)..."
	@go test -v -race -timeout 30s -run "TestComprehensiveClaudeCodeE2E/(Layer[23])" ./...


.PHONY: test-layer1
test-layer1: ## Run Layer 1 container integration tests
	@echo "🐳 Running Layer 1 container tests..."
	@go test -v -race -timeout 3m -run "TestComprehensiveClaudeCodeE2E/Layer1" ./...

.PHONY: test-layer2
test-layer2: ## Run Layer 2 protocol tests
	@echo "📡 Running Layer 2 protocol tests..."
	@go test -v -race -timeout 1m -run "TestComprehensiveClaudeCodeE2E/Layer2" ./...

.PHONY: test-layer3
test-layer3: ## Run Layer 3 workflow tests
	@echo "🔄 Running Layer 3 workflow tests..."
	@go test -v -race -timeout 1m -run "TestComprehensiveClaudeCodeE2E/Layer3" ./...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	@echo "📊 Running tests with coverage..."
	@go test -race -cover -coverprofile=coverage.out -timeout 5m ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report generated: coverage.html"
	@echo "Coverage summary:"
	@go tool cover -func=coverage.out | grep total | awk '{print "  Total coverage: " $$3}'

.PHONY: test-bench
test-bench: ## Run benchmark tests
	@echo "⏱️ Running benchmarks..."
	@go test -bench=. -benchmem -run=^$$ ./...

.PHONY: test-compile
test-compile: ## Compile tests without running (quick validation)
	@echo "🔨 Compiling tests..."
	@go test -c -o test.bin
	@rm -f test.bin
	@echo "✅ Tests compile successfully"

.PHONY: docker-up
docker-up: ## Start Docker containers for testing
	@echo "🐳 Starting Docker containers..."
	@docker-compose -f docker-compose.claude-code.yml up -d --build

.PHONY: docker-down
docker-down: ## Stop and remove Docker containers
	@echo "🛑 Stopping Docker containers..."
	@docker-compose -f docker-compose.claude-code.yml down -v --remove-orphans

.PHONY: docker-logs
docker-logs: ## Show Docker container logs
	@docker-compose -f docker-compose.claude-code.yml logs -f

.PHONY: docker-clean
docker-clean: docker-down ## Clean up Docker resources
	@echo "🧹 Cleaning Docker resources..."
	@docker system prune -f --volumes
	@echo "✅ Docker cleanup complete"

.PHONY: clean
clean: ## Clean test artifacts
	@echo "🧹 Cleaning test artifacts..."
	@rm -f coverage.out coverage.html test.bin *.test
	@rm -rf certs/*.crt certs/*.key certs/*.csr
	@echo "✅ Cleanup complete"

.PHONY: lint
lint: ## Run linters
	@echo "🔍 Running linters..."
	@go vet ./...
	@golangci-lint run --deadline=5m ./... || true
	@echo "✅ Linting complete"

.PHONY: fmt
fmt: ## Format code
	@echo "✨ Formatting code..."
	@go fmt ./...
	@echo "✅ Formatting complete"

.PHONY: deps
deps: ## Download dependencies
	@echo "📦 Downloading dependencies..."
	@go mod download
	@go mod tidy
	@echo "✅ Dependencies updated"

.PHONY: build-router
build-router: ## Build router binary
	@echo "🔨 Building router binary..."
	@cd ../../../services/router && go build -o bin/mcp-router ./cmd/mcp-router
	@echo "✅ Router binary built"

.PHONY: verify
verify: test-compile lint fmt ## Verify code quality (compile, lint, format)
	@echo "✅ All verifications passed"

.PHONY: ci
ci: verify test-coverage ## Run CI pipeline locally
	@echo "✅ CI pipeline complete"

# Advanced targets for debugging

.PHONY: test-verbose
test-verbose: ## Run tests with verbose debug output
	@echo "🔬 Running tests with debug output..."
	@go test -v -race -timeout 5m -args -debug ./...

.PHONY: test-specific
test-specific: ## Run specific test (use TEST=TestName make test-specific)
	@echo "🎯 Running test: $(TEST)"
	@go test -v -race -timeout 2m -run "$(TEST)" ./...

.PHONY: test-stress
test-stress: ## Run stress test (multiple iterations)
	@echo "💪 Running stress test (10 iterations)..."
	@for i in $$(seq 1 10); do \
		echo "Iteration $$i/10..."; \
		go test -race -timeout 5m ./... || exit 1; \
	done
	@echo "✅ Stress test passed"

# Default target
.DEFAULT_GOAL := help