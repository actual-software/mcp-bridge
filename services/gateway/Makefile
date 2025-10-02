.PHONY: build test docker clean lint fmt deps

# Variables
BINARY_NAME := mcp-gateway
IMAGE_NAME := ghcr.io/poiley/mcp-bridge-gateway
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
CMD_DIR := ./cmd/mcp-gateway
BIN_DIR := ./bin
DIST_DIR := ./dist

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	@echo "Tests complete"

# Run tests with detailed coverage
test-coverage: test
	@echo "Generating coverage report..."
	@$(GOCMD) tool cover -func=coverage.out | grep -E "^total:|^github.com/actual-software/mcp-bridge/services/gateway/" | sort
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
		echo "Usage: make test-pkg PKG=./internal/auth"; \
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

# Generate test report in JSON format
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

# Build Docker image
docker:
	@echo "Building Docker image..."
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-t $(IMAGE_NAME):$(VERSION) \
		-t $(IMAGE_NAME):latest \
		.
	@echo "Docker image built: $(IMAGE_NAME):$(VERSION)"

# Push Docker image
docker-push: docker
	@echo "Pushing Docker image..."
	docker push $(IMAGE_NAME):$(VERSION)
	docker push $(IMAGE_NAME):latest
	@echo "Docker image pushed"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out coverage.html
	@echo "Clean complete"

# Run linting
lint:
	@echo "Running linter..."
	@if command -v $(GOLINT) >/dev/null; then \
		$(GOLINT) run --timeout=5m --config=../../.config/golangci.yml ./...; \
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

# Run locally for development
run: build
	@echo "Running $(BINARY_NAME)..."
	$(BIN_DIR)/$(BINARY_NAME) --config gateway.yaml

# Run with debug logging
run-debug: build
	@echo "Running $(BINARY_NAME) with debug logging..."
	$(BIN_DIR)/$(BINARY_NAME) --config gateway.yaml --log-level debug

# Generate mocks for testing
mocks:
	@echo "Generating mocks..."
	@if command -v mockgen >/dev/null; then \
		mockgen -source=internal/auth/provider.go -destination=internal/auth/mock_provider.go -package=auth; \
		mockgen -source=internal/discovery/kubernetes.go -destination=internal/discovery/mock_discovery.go -package=discovery; \
		mockgen -source=internal/session/manager.go -destination=internal/session/mock_manager.go -package=session; \
	else \
		echo "mockgen not installed. Install with: go install github.com/golang/mock/mockgen@latest"; \
		exit 1; \
	fi
	@echo "Mocks generated"

# Kubernetes deployment
k8s-deploy:
	@echo "Deploying to Kubernetes..."
	kubectl apply -f ../mcp-k8s-manifests/gateway/
	@echo "Deployment complete"

# Kubernetes status
k8s-status:
	@echo "Checking Kubernetes status..."
	kubectl get pods -n mcp-system -l app=mcp-gateway
	kubectl get svc -n mcp-system -l app=mcp-gateway
	@echo "Status check complete"

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
	@echo "  make run            - Run locally"
	@echo "  make run-debug      - Run with debug logging"
	@echo "  make docker         - Build Docker image"
	@echo "  make docker-push    - Push Docker image"
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
	@echo "  make mocks          - Generate test mocks"
	@echo ""
	@echo "Dependencies:"
	@echo "  make deps           - Update dependencies"
	@echo "  make test-deps      - Install test dependencies"
	@echo ""
	@echo "Deployment:"
	@echo "  make k8s-deploy     - Deploy to Kubernetes"
	@echo "  make k8s-status     - Check Kubernetes status"
	@echo ""
	@echo "Other:"
	@echo "  make clean          - Remove build artifacts"
	@echo "  make version        - Show version information"
	@echo "  make help           - Show this help message"