# Kubernetes E2E Test Makefile

.PHONY: test test-quick test-all clean setup check-prereqs help

# Default target
test: test-quick

# Quick test - main E2E test only
test-quick:
	@echo "Running quick Kubernetes E2E test..."
	go test -run TestKubernetesEndToEnd -v -timeout 20m

# All tests including performance and failover
test-all:
	@echo "Running all Kubernetes E2E tests..."
	go test -v -timeout 45m

# Short tests only (skips long-running tests)
test-short:
	@echo "Running short Kubernetes E2E tests..."
	go test -short -v -timeout 15m

# Setup test environment
setup: check-prereqs
	@echo "Setting up test environment..."
	go mod tidy
	@echo "Test environment ready"

# Check prerequisites
check-prereqs:
	@echo "Checking prerequisites..."
	@command -v docker >/dev/null 2>&1 || { echo "Docker is required but not installed"; exit 1; }
	@command -v kind >/dev/null 2>&1 || { echo "KinD is required but not installed"; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required but not installed"; exit 1; }
	@docker info >/dev/null 2>&1 || { echo "Docker daemon is not running"; exit 1; }
	@echo "All prerequisites satisfied"

# Clean up test resources
clean:
	@echo "Cleaning up test resources..."
	-kind delete cluster --name mcp-e2e-test
	-docker rmi mcp-gateway:test mcp-router:test test-mcp-server:test 2>/dev/null
	@echo "Cleanup complete"

# Force clean - more aggressive cleanup
clean-force: clean
	@echo "Performing force cleanup..."
	-docker system prune -f
	-kubectl config delete-context kind-mcp-e2e-test 2>/dev/null
	@echo "Force cleanup complete"

# Install dependencies
deps:
	@echo "Installing Go dependencies..."
	go mod download
	go mod tidy

# Install prerequisites (requires appropriate package manager)
install-prereqs:
	@echo "Installing prerequisites..."
	@if command -v brew >/dev/null 2>&1; then \
		echo "Using Homebrew to install prerequisites..."; \
		brew install kind kubectl docker; \
	elif command -v apt-get >/dev/null 2>&1; then \
		echo "Using apt to install prerequisites..."; \
		sudo apt-get update && sudo apt-get install -y docker.io; \
		curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64; \
		chmod +x ./kind && sudo mv ./kind /usr/local/bin/kind; \
		curl -LO "https://dl.k8s.io/release/$$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"; \
		chmod +x kubectl && sudo mv kubectl /usr/local/bin/; \
	else \
		echo "Please install Docker, KinD, and kubectl manually"; \
		exit 1; \
	fi

# Debug - show cluster status
debug:
	@echo "=== Cluster Status ==="
	-kind get clusters
	-kubectl cluster-info --context kind-mcp-e2e-test
	@echo "=== Pods ==="
	-kubectl get pods -n mcp-e2e
	@echo "=== Services ==="
	-kubectl get services -n mcp-e2e
	@echo "=== Events ==="
	-kubectl get events -n mcp-e2e --sort-by='.lastTimestamp' | tail -20

# Logs - show logs from all components
logs:
	@echo "=== Gateway Logs ==="
	-kubectl logs -n mcp-e2e deployment/mcp-gateway -c gateway --tail=50
	@echo "=== Router Logs ==="
	-kubectl logs -n mcp-e2e deployment/mcp-gateway -c router --tail=50
	@echo "=== Test MCP Server Logs ==="
	-kubectl logs -n mcp-e2e deployment/test-mcp-server --tail=50

# Port forward for manual testing
port-forward:
	@echo "Port forwarding gateway service (use Ctrl+C to stop)..."
	kubectl port-forward -n mcp-e2e service/mcp-gateway 8443:8443 9091:9091

# Test cluster creation only
test-cluster:
	@echo "Testing cluster creation..."
	go test -run TestKubernetesEndToEnd -v -timeout 10m | grep -E "(cluster|namespace|image|deploy)"

# Performance test
test-perf:
	@echo "Running performance tests..."
	go test -run TestKubernetesPerformance -v -timeout 30m

# Failover test
test-failover:
	@echo "Running failover tests..."
	go test -run TestKubernetesFailover -v -timeout 25m

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -coverprofile=coverage.out -v -timeout 20m
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Benchmark tests
benchmark:
	@echo "Running benchmark tests..."
	go test -bench=. -benchmem -v

# Lint the test code
lint:
	@echo "Linting test code..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found, skipping lint"; \
	fi

# Help
help:
	@echo "Available targets:"
	@echo "  test          - Run quick E2E test (default)"
	@echo "  test-quick    - Run main E2E test only"
	@echo "  test-all      - Run all E2E tests including performance"
	@echo "  test-short    - Run short tests only"
	@echo "  test-cluster  - Test cluster creation only"
	@echo "  test-perf     - Run performance tests"
	@echo "  test-failover - Run failover tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  setup         - Setup test environment"
	@echo "  check-prereqs - Check if prerequisites are installed"
	@echo "  install-prereqs - Install prerequisites (brew/apt)"
	@echo "  clean         - Clean up test resources"
	@echo "  clean-force   - Aggressive cleanup"
	@echo "  debug         - Show cluster status"
	@echo "  logs          - Show logs from all components" 
	@echo "  port-forward  - Port forward gateway for manual testing"
	@echo "  deps          - Install Go dependencies"
	@echo "  lint          - Lint the test code"
	@echo "  benchmark     - Run benchmark tests"
	@echo "  help          - Show this help message"