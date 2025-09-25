# Production-ready Makefile for Weather MCP E2E Test
.PHONY: all build test clean docker-build k8s-deploy e2e-test teardown help

# Variables
DOCKER_REGISTRY ?= localhost:5000
VERSION ?= latest
NAMESPACE ?= mcp-weather-test
CLUSTER_NAME ?= mcp-weather-e2e
TIMEOUT ?= 10m

# Colors for output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
NC := \033[0m # No Color

# Default target
all: help

## help: Display this help message
help:
	@echo "Weather MCP E2E Test Suite"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/## /  /'

## build: Build all Go binaries
build:
	@echo "$(GREEN)Building Weather MCP server...$(NC)"
	go build -o bin/weather-server ./cmd/weather-server
	@echo "$(GREEN)Building test binaries...$(NC)"
	go test -c -o bin/weather-e2e-test .

## docker-build: Build Docker images for all components
docker-build:
	@echo "$(GREEN)Building Weather MCP server Docker image...$(NC)"
	docker build -t $(DOCKER_REGISTRY)/weather-mcp-server:$(VERSION) -f Dockerfile .
	
	@echo "$(GREEN)Building Gateway Docker image...$(NC)"
	cd ../../../services/gateway && \
		docker build -t $(DOCKER_REGISTRY)/mcp-gateway:$(VERSION) .
	
	@echo "$(GREEN)Building Router Docker image...$(NC)"
	cd ../../../services/router && \
		docker build -t $(DOCKER_REGISTRY)/mcp-router:$(VERSION) .

## docker-push: Push Docker images to registry
docker-push: docker-build
	@echo "$(GREEN)Pushing images to registry...$(NC)"
	docker push $(DOCKER_REGISTRY)/weather-mcp-server:$(VERSION)
	docker push $(DOCKER_REGISTRY)/mcp-gateway:$(VERSION)
	docker push $(DOCKER_REGISTRY)/mcp-router:$(VERSION)

## cluster-create: Create Kind cluster for testing
cluster-create:
	@echo "$(GREEN)Creating Kind cluster...$(NC)"
	@kind get clusters | grep -q $(CLUSTER_NAME) || \
		(cat <<EOF | kind create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: $(CLUSTER_NAME)
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    hostPort: 80
    protocol: TCP
  - containerPort: 443
    hostPort: 443
    protocol: TCP
networking:
  podSubnet: "10.244.0.0/16"
  serviceSubnet: "10.96.0.0/12"
  disableDefaultCNI: false
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:5000"]
    endpoint = ["http://host.docker.internal:5000"]
EOF
)
	@echo "$(GREEN)Cluster created successfully$(NC)"

## cluster-load-images: Load Docker images into Kind cluster
cluster-load-images: docker-build
	@echo "$(GREEN)Loading images into Kind cluster...$(NC)"
	kind load docker-image $(DOCKER_REGISTRY)/weather-mcp-server:$(VERSION) --name $(CLUSTER_NAME)
	kind load docker-image $(DOCKER_REGISTRY)/mcp-gateway:$(VERSION) --name $(CLUSTER_NAME)
	kind load docker-image $(DOCKER_REGISTRY)/mcp-router:$(VERSION) --name $(CLUSTER_NAME)

## cluster-delete: Delete Kind cluster
cluster-delete:
	@echo "$(YELLOW)Deleting Kind cluster...$(NC)"
	kind delete cluster --name $(CLUSTER_NAME)

## k8s-deploy: Deploy all components to Kubernetes
k8s-deploy:
	@echo "$(GREEN)Creating namespace...$(NC)"
	kubectl create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	
	@echo "$(GREEN)Deploying network policies...$(NC)"
	kubectl apply -f manifests/network-policies.yaml -n $(NAMESPACE)
	
	@echo "$(GREEN)Deploying Weather MCP server...$(NC)"
	kubectl apply -f manifests/weather-server.yaml -n $(NAMESPACE)
	
	@echo "$(GREEN)Deploying MCP Gateway...$(NC)"
	kubectl apply -f manifests/gateway.yaml -n $(NAMESPACE)
	
	@echo "$(GREEN)Deploying MCP Router...$(NC)"
	kubectl apply -f manifests/router.yaml -n $(NAMESPACE)
	
	@echo "$(GREEN)Waiting for deployments to be ready...$(NC)"
	kubectl wait --for=condition=available --timeout=300s \
		deployment/weather-mcp-server deployment/mcp-gateway deployment/mcp-router \
		-n $(NAMESPACE)

## k8s-teardown: Remove all components from Kubernetes
k8s-teardown:
	@echo "$(YELLOW)Removing deployments...$(NC)"
	kubectl delete namespace $(NAMESPACE) --ignore-not-found=true

## test-unit: Run unit tests
test-unit:
	@echo "$(GREEN)Running unit tests...$(NC)"
	go test -v -race -coverprofile=coverage.out ./...
	@echo "$(GREEN)Coverage report:$(NC)"
	go tool cover -func=coverage.out

## test-integration: Run integration tests
test-integration: k8s-deploy
	@echo "$(GREEN)Running integration tests...$(NC)"
	kubectl port-forward -n $(NAMESPACE) service/mcp-gateway 8080:8080 &
	sleep 5
	go test -v -tags=integration -timeout=$(TIMEOUT) ./...
	pkill -f "kubectl port-forward"

## e2e-test: Run complete E2E test suite
e2e-test: cluster-create cluster-load-images k8s-deploy
	@echo "$(GREEN)Running E2E test suite...$(NC)"
	E2E_TEST=true go test -v -timeout=$(TIMEOUT) -run TestWeatherMCPEndToEnd

## e2e-test-ci: Run E2E tests in CI environment
e2e-test-ci:
	@echo "$(GREEN)Setting up CI environment...$(NC)"
	@make cluster-create
	@make docker-build
	@make cluster-load-images
	@make k8s-deploy
	@echo "$(GREEN)Running E2E tests...$(NC)"
	E2E_TEST=true CI=true go test -v -timeout=$(TIMEOUT) -run TestWeatherMCPEndToEnd || \
		(echo "$(RED)Tests failed$(NC)"; make logs; exit 1)
	@echo "$(GREEN)Tests passed successfully$(NC)"

## logs: Get logs from all pods
logs:
	@echo "$(YELLOW)Fetching logs from all pods...$(NC)"
	@for pod in $$(kubectl get pods -n $(NAMESPACE) -o name); do \
		echo "$(GREEN)Logs for $$pod:$(NC)"; \
		kubectl logs -n $(NAMESPACE) $$pod --tail=50; \
		echo "---"; \
	done

## debug: Start debug session with port forwarding
debug:
	@echo "$(GREEN)Starting debug session...$(NC)"
	@echo "Gateway: http://localhost:8080"
	@echo "Weather Server: http://localhost:8081"
	@echo "Metrics: http://localhost:9090"
	kubectl port-forward -n $(NAMESPACE) service/mcp-gateway 8080:8080 &
	kubectl port-forward -n $(NAMESPACE) service/weather-mcp-server 8081:8080 &
	kubectl port-forward -n $(NAMESPACE) service/weather-mcp-server 9090:9090 &
	@echo "$(YELLOW)Press Ctrl+C to stop port forwarding$(NC)"
	@wait

## monitor: Open monitoring dashboards
monitor:
	@echo "$(GREEN)Opening monitoring dashboards...$(NC)"
	kubectl port-forward -n $(NAMESPACE) service/weather-mcp-server 9090:9090 &
	kubectl port-forward -n $(NAMESPACE) service/mcp-gateway 9091:9090 &
	@echo "Weather Metrics: http://localhost:9090/metrics"
	@echo "Gateway Metrics: http://localhost:9091/metrics"
	@wait

## validate: Validate all manifests and configurations
validate:
	@echo "$(GREEN)Validating Kubernetes manifests...$(NC)"
	kubectl apply --dry-run=client -f manifests/ -n $(NAMESPACE)
	@echo "$(GREEN)Validating Go code...$(NC)"
	go vet ./...
	golangci-lint run ./...
	@echo "$(GREEN)All validations passed$(NC)"

## benchmark: Run performance benchmarks
benchmark: k8s-deploy
	@echo "$(GREEN)Running performance benchmarks...$(NC)"
	go test -bench=. -benchmem -benchtime=10s -timeout=$(TIMEOUT) ./...

## coverage: Generate and open coverage report
coverage:
	@echo "$(GREEN)Generating coverage report...$(NC)"
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Opening coverage report...$(NC)"
	open coverage.html

## clean: Clean up all generated files and resources
clean: k8s-teardown cluster-delete
	@echo "$(YELLOW)Cleaning up...$(NC)"
	rm -rf bin/
	rm -f coverage.out coverage.html
	rm -f *.log
	docker image prune -f
	@echo "$(GREEN)Cleanup complete$(NC)"

## full-test: Run complete test suite with setup and teardown
full-test:
	@echo "$(GREEN)Running full test suite...$(NC)"
	@make clean
	@make validate
	@make test-unit
	@make e2e-test
	@make benchmark
	@make coverage
	@make clean
	@echo "$(GREEN)Full test suite completed successfully$(NC)"

# CI/CD targets
.PHONY: ci cd

## ci: Continuous Integration pipeline
ci:
	@echo "$(GREEN)Running CI pipeline...$(NC)"
	@make validate
	@make test-unit
	@make docker-build
	@make e2e-test-ci
	@make coverage

## cd: Continuous Deployment pipeline
cd: ci
	@echo "$(GREEN)Running CD pipeline...$(NC)"
	@make docker-push
	@echo "$(GREEN)Deployment complete$(NC)"

# Development helpers
.PHONY: dev-setup dev-test

## dev-setup: Set up development environment
dev-setup:
	@echo "$(GREEN)Setting up development environment...$(NC)"
	go mod download
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "$(GREEN)Development environment ready$(NC)"

## dev-test: Quick test for development
dev-test:
	@echo "$(GREEN)Running quick development test...$(NC)"
	go test -v -short ./...

# Performance testing
.PHONY: load-test stress-test chaos-test

## load-test: Run load testing
load-test: k8s-deploy
	@echo "$(GREEN)Running load test...$(NC)"
	go test -v -run TestLoadScenarios -timeout=$(TIMEOUT)

## stress-test: Run stress testing
stress-test: k8s-deploy
	@echo "$(GREEN)Running stress test...$(NC)"
	STRESS_TEST=true go test -v -run TestStress -timeout=30m

## chaos-test: Run chaos engineering tests
chaos-test: k8s-deploy
	@echo "$(GREEN)Running chaos test...$(NC)"
	CHAOS_TEST=true go test -v -run TestChaos -timeout=$(TIMEOUT)