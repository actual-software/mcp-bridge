.PHONY: build test release clean install lint fmt

# Variables
BINARY_NAME := mcp-router
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date +%FT%T%z)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT) -s -w"

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod
GOFMT := gofmt
GOLINT := $(shell which golangci-lint 2>/dev/null || echo "$(HOME)/go/bin/golangci-lint")

# Directories
SRC_DIR := .
CMD_DIR := ./cmd/mcp-router
BIN_DIR := ./bin
DIST_DIR := ./dist

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	@echo "Tests complete"

# Run tests with coverage report
test-coverage: test
	@echo "Generating coverage report..."
	@$(GOCMD) tool cover -func=coverage.out | grep -E "^total:|^github.com/actual-software/mcp-bridge/services/router/" | sort
	@echo ""
	@echo "Overall coverage: $$($(GOCMD) tool cover -func=coverage.out | grep total | awk '{print $$3}')"

# Run tests with HTML coverage report
test-coverage-html: test
	@echo "Generating HTML coverage report..."
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@if command -v open >/dev/null; then \
		open coverage.html; \
	elif command -v xdg-open >/dev/null; then \
		xdg-open coverage.html; \
	fi

# Run tests with detailed coverage report
test-coverage-detailed: test
	@echo "Detailed coverage by package:"
	@$(GOCMD) tool cover -func=coverage.out

# Run only unit tests (exclude integration)
test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v -short -race -coverprofile=coverage.out ./...
	@echo "Unit tests complete"

# Run only integration tests
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -run Integration -race ./...
	@echo "Integration tests complete"

# Run tests with race detection only
test-race:
	@echo "Running tests with race detection..."
	$(GOTEST) -race ./...
	@echo "Race detection complete"

# Run tests for specific package
test-pkg:
	@if [ -z "$(PKG)" ]; then \
		echo "Usage: make test-pkg PKG=./internal/config"; \
		exit 1; \
	fi
	@echo "Running tests for $(PKG)..."
	$(GOTEST) -v -race -coverprofile=coverage.out $(PKG)/...

# Run benchmarks
benchmark:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...
	@echo "Benchmarks complete"

# Check code coverage threshold
check-coverage: test
	@echo "Checking coverage threshold..."
	@coverage=$$($(GOCMD) tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	threshold=80; \
	if [ $$(echo "$$coverage < $$threshold" | bc) -eq 1 ]; then \
		echo "❌ Coverage $$coverage% is below threshold $$threshold%"; \
		exit 1; \
	else \
		echo "✅ Coverage $$coverage% meets threshold $$threshold%"; \
	fi

# Run all tests and checks
check: lint test check-coverage
	@echo "All checks passed!"

# Install test dependencies
test-deps:
	@echo "Installing test dependencies..."
	@$(GOCMD) install github.com/golang/mock/mockgen@latest
	@$(GOCMD) install gotest.tools/gotestsum@latest
	@$(GOCMD) install github.com/axw/gocov/gocov@latest
	@$(GOCMD) install github.com/matm/gocov-html@latest
	@echo "Test dependencies installed"

# Generate test report in JSON format (continue on package failures)
test-json:
	@echo "Running tests with JSON output..."
	@echo "Discovering test packages..."
	@rm -f test-report.json
	@find . -name "*_test.go" -exec dirname {} \; | sort -u | while read pkg; do \
		echo "Testing package: $$pkg"; \
		$(GOTEST) -v -race -json "$$pkg" >> test-report.json 2>/dev/null || echo "Package $$pkg failed (compilation or runtime error)"; \
	done
	@echo "Test report generated: test-report.json"
	@echo "Report includes results from all testable packages, even if some failed"

# Run tests with gotestsum for better output
test-pretty:
	@if command -v gotestsum >/dev/null; then \
		echo "Running tests with gotestsum..."; \
		gotestsum --format testname -- -race -coverprofile=coverage.out ./...; \
	else \
		echo "gotestsum not installed. Install with: go install gotest.tools/gotestsum@latest"; \
		$(MAKE) test; \
	fi

# Build for multiple platforms
release:
	@echo "Building releases..."
	@mkdir -p $(DIST_DIR)
	
	# macOS AMD64
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) \
		-o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 $(CMD_DIR)
	
	# macOS ARM64 (Apple Silicon)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) \
		-o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 $(CMD_DIR)
	
	# Linux AMD64
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) \
		-o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 $(CMD_DIR)
	
	# Linux ARM64
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) \
		-o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 $(CMD_DIR)
	
	# Windows AMD64
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) \
		-o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe $(CMD_DIR)
	
	@echo "Releases built in $(DIST_DIR)/"
	@ls -la $(DIST_DIR)/

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out coverage.html
	@echo "Clean complete"

# Install the binary to $GOPATH/bin
install: build
	@echo "Installing $(BINARY_NAME)..."
	@cp $(BIN_DIR)/$(BINARY_NAME) $(GOPATH)/bin/
	@echo "Installation complete"

# Run linting
lint:
	@echo "Running linter..."
	@if command -v $(GOLINT) >/dev/null; then \
		$(GOLINT) run ./...; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w $(SRC_DIR)
	@echo "Code formatted"

# Update dependencies
deps:
	@echo "Updating dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "Dependencies updated"

# Run the binary
run: build
	@echo "Running $(BINARY_NAME)..."
	$(BIN_DIR)/$(BINARY_NAME)

# Run with debug logging
run-debug: build
	@echo "Running $(BINARY_NAME) with debug logging..."
	$(BIN_DIR)/$(BINARY_NAME) --log-level debug

# Development mode - rebuild and run on file changes
dev:
	@echo "Starting development mode..."
	@if command -v air >/dev/null; then \
		air; \
	else \
		echo "Air not installed. Install with: go install github.com/cosmtrek/air@latest"; \
		exit 1; \
	fi

# Print version information
version:
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Git Commit: $(GIT_COMMIT)"

# Help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build & Run:"
	@echo "  make build          - Build the binary"
	@echo "  make run            - Build and run"
	@echo "  make run-debug      - Run with debug logging"
	@echo "  make dev            - Development mode with auto-reload"
	@echo "  make release        - Build for multiple platforms"
	@echo "  make install        - Install to GOPATH/bin"
	@echo ""
	@echo "Testing:"
	@echo "  make test           - Run all tests with coverage"
	@echo "  make test-unit      - Run unit tests only"
	@echo "  make test-integration - Run integration tests"
	@echo "  make test-race      - Run tests with race detection"
	@echo "  make test-coverage  - Show test coverage summary"
	@echo "  make test-coverage-html - Generate HTML coverage report"
	@echo "  make test-coverage-detailed - Show detailed coverage"
	@echo "  make test-pkg PKG=path - Test specific package"
	@echo "  make test-pretty    - Run tests with pretty output"
	@echo "  make test-json      - Generate JSON test report"
	@echo "  make benchmark      - Run benchmarks"
	@echo "  make check-coverage - Check coverage threshold"
	@echo "  make check          - Run lint + test + coverage check"
	@echo ""
	@echo "Code Quality:"
	@echo "  make lint           - Run linter"
	@echo "  make fmt            - Format code"
	@echo ""
	@echo "Dependencies:"
	@echo "  make deps           - Update dependencies"
	@echo "  make test-deps      - Install test dependencies"
	@echo ""
	@echo "Other:"
	@echo "  make clean          - Remove build artifacts"
	@echo "  make version        - Show version information"
	@echo "  make help           - Show this help message"