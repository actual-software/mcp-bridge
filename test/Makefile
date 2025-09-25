# Test Infrastructure Makefile

.PHONY: all smoke contract performance regression help

# Default target
all: smoke contract performance

# Help target
help:
	@echo "MCP Bridge Test Infrastructure"
	@echo ""
	@echo "Available targets:"
	@echo "  make smoke-quick    - Run quick smoke tests (< 1 minute)"
	@echo "  make smoke-full     - Run full smoke test suite (< 5 minutes)"
	@echo "  make smoke-critical - Run critical path tests only"
	@echo "  make contract       - Run API contract tests"
	@echo "  make performance    - Run performance tests"
	@echo "  make regression     - Run performance regression tests"
	@echo "  make test-all       - Run all test suites"
	@echo "  make baseline       - Update performance baselines"
	@echo "  make report         - Generate test report"
	@echo ""
	@echo "Environment variables:"
	@echo "  MCP_BASE_URL    - Base URL for testing (default: http://localhost:8080)"
	@echo "  MCP_AUTH_TOKEN  - Authentication token (default: dev-token-12345)"
	@echo "  CI              - Set to 'true' for CI mode"

###################
# Smoke Tests
###################

smoke-quick:
	@echo "Running quick smoke tests..."
	@go test -v ./smoke -run TestQuickSmoke -timeout 1m

smoke-full:
	@echo "Running full smoke test suite..."
	@go test -v ./smoke -run TestFullSmoke -timeout 5m

smoke-critical:
	@echo "Running critical path smoke tests..."
	@go test -v ./smoke -run TestCriticalPathSmoke -timeout 2m

smoke: smoke-quick

###################
# Contract Tests
###################

contract:
	@echo "Running API contract tests..."
	@go test -v ./contract -timeout 10m

contract-gateway:
	@echo "Running Gateway contract tests..."
	@go test -v ./contract -run TestGateway

contract-router:
	@echo "Running Router contract tests..."
	@go test -v ./contract -run TestRouter

contract-validate:
	@echo "Validating OpenAPI spec..."
	@go test -v ./contract -run TestOpenAPIValidation

###################
# Performance Tests
###################

performance:
	@echo "Running performance tests..."
	@go test -v ./performance -run TestPerformance -timeout 30m

performance-quick:
	@echo "Running quick performance tests..."
	@go test -v ./performance -run TestPerformance -short -timeout 5m

regression:
	@echo "Running performance regression tests..."
	@go test -v ./performance -run TestRegression -timeout 20m

regression-gateway:
	@echo "Running Gateway regression tests..."
	@go test -v ./performance -run TestGatewayPerformanceRegression

regression-router:
	@echo "Running Router regression tests..."
	@go test -v ./performance -run TestRouterPerformanceRegression

memory-leak:
	@echo "Running memory leak detection..."
	@go test -v ./performance -run TestMemoryLeakDetection -timeout 30m

cpu-profile:
	@echo "Running CPU profile regression..."
	@go test -v ./performance -run TestCPUProfileRegression -timeout 10m

###################
# Baseline Management
###################

baseline-update:
	@echo "Updating performance baselines..."
	@mkdir -p baselines
	@go test -v ./performance -run TestUpdateBaseline

baseline-save:
	@echo "Saving current performance as baseline..."
	@cp reports/perf_current.json baselines/baseline.json
	@echo "Baseline saved"

baseline-compare:
	@echo "Comparing with baseline..."
	@go test -v ./performance -run TestCompareBaseline

###################
# Test Reports
###################

report:
	@echo "Generating test report..."
	@mkdir -p reports
	@go test -v ./smoke -json > reports/smoke.json 2>&1 || true
	@go test -v ./contract -json > reports/contract.json 2>&1 || true
	@go test -v ./performance -json > reports/performance.json 2>&1 || true
	@echo "Reports generated in reports/"

report-html:
	@echo "Generating HTML report..."
	@go-test-report -input reports/*.json -output reports/test-report.html

coverage:
	@echo "Running tests with coverage..."
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o reports/coverage.html
	@echo "Coverage report: reports/coverage.html"

###################
# CI/CD Integration
###################

ci-smoke:
	@echo "Running CI smoke tests..."
	CI=true go test -v ./smoke -run TestQuickSmoke -json | tee reports/ci-smoke.json

ci-contract:
	@echo "Running CI contract tests..."
	CI=true go test -v ./contract -json | tee reports/ci-contract.json

ci-regression:
	@echo "Running CI regression tests..."
	CI=true go test -v ./performance -run TestRegression -json | tee reports/ci-regression.json

ci-all: ci-smoke ci-contract ci-regression
	@echo "All CI tests completed"

###################
# Test All
###################

test-all: smoke-full contract performance regression
	@echo "All test suites completed"

test-quick: smoke-quick contract-validate performance-quick
	@echo "Quick test suite completed"

test-nightly: test-all memory-leak cpu-profile
	@echo "Nightly test suite completed"

###################
# Development Tools
###################

watch-smoke:
	@echo "Watching smoke tests..."
	@while true; do \
		go test -v ./smoke -run TestQuickSmoke; \
		echo "Waiting for changes..."; \
		sleep 10; \
	done

debug-contract:
	@echo "Running contract tests with debug output..."
	@GODEBUG=http2debug=2 go test -v ./contract -run TestContractTestSuite

profile-performance:
	@echo "Running performance tests with profiling..."
	@go test -v ./performance -cpuprofile=cpu.prof -memprofile=mem.prof
	@echo "Profiles saved: cpu.prof, mem.prof"

###################
# Setup and Dependencies
###################

setup:
	@echo "Setting up test infrastructure..."
	@go get -u github.com/stretchr/testify
	@go get -u github.com/montanaflynn/stats
	@go get -u github.com/gorilla/websocket
	@mkdir -p reports baselines logs

clean:
	@echo "Cleaning test artifacts..."
	@rm -rf reports/*.json reports/*.html
	@rm -f cpu.prof mem.prof coverage.out
	@rm -f smoke_results_*.json

###################
# Docker Integration
###################

docker-test:
	@echo "Running tests in Docker..."
	@docker-compose -f ../docker-compose.test.yml up --abort-on-container-exit

docker-smoke:
	@echo "Running smoke tests in Docker..."
	@docker run --rm -v $(PWD):/test -w /test golang:1.21 make smoke-quick

###################
# Kubernetes Integration
###################

k8s-smoke:
	@echo "Running smoke tests in Kubernetes..."
	@kubectl run smoke-test --image=golang:1.21 --rm -it --restart=Never -- make smoke-quick

k8s-contract:
	@echo "Running contract tests in Kubernetes..."
	@kubectl run contract-test --image=golang:1.21 --rm -it --restart=Never -- make contract

###################
# Load Testing Integration
###################

load-test:
	@echo "Running load tests..."
	@go test -v ../load -run TestLoad -timeout 60m

stress-test:
	@echo "Running stress tests..."
	@go test -v ../stress -run TestStress -timeout 120m

###################
# Validation Helpers
###################

validate-env:
	@echo "Validating test environment..."
	@test -n "$(MCP_BASE_URL)" || (echo "MCP_BASE_URL not set, using default" && true)
	@test -n "$(MCP_AUTH_TOKEN)" || (echo "MCP_AUTH_TOKEN not set, using default" && true)
	@echo "Environment validated"

check-services:
	@echo "Checking service availability..."
	@curl -sf $(MCP_BASE_URL)/health || (echo "Gateway not accessible" && exit 1)
	@echo "Services are available"

###################
# Composite Targets
###################

pre-commit: smoke-quick contract-validate
	@echo "Pre-commit tests passed"

pre-merge: test-quick regression
	@echo "Pre-merge tests passed"

release-validation: test-all baseline-compare
	@echo "Release validation complete"