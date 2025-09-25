# Kubernetes End-to-End Tests

This directory contains comprehensive end-to-end tests for the MCP Bridge system running in Kubernetes.

## Overview

The Kubernetes E2E tests validate the complete MCP system deployment and functionality in a real Kubernetes environment using:

- **KinD (Kubernetes in Docker)**: Creates a local test cluster
- **Docker**: Builds and loads container images
- **Real MCP Protocol**: Full protocol testing with actual WebSocket connections
- **Load Balancing**: Tests distribution across multiple backend replicas
- **Metrics Collection**: Validates Prometheus metrics
- **Health Checks**: Verifies service health and readiness

## Test Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Test Client   │───▶│   MCP Gateway    │───▶│  Test MCP       │
│  (WebSocket)    │    │  (Load Balancer) │    │  Server (2x)    │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                              │
                              ▼
                       ┌──────────────────┐
                       │     Redis        │
                       │   (Sessions)     │
                       └──────────────────┘
```

### Components Deployed

1. **Redis**: Session storage and caching
2. **Test MCP Server**: 2 replicas with backend identification
3. **MCP Gateway**: Load balancer with metrics
4. **MCP Router**: Protocol translation (sidecar)

## Prerequisites

- **Docker**: Container runtime
- **KinD**: Kubernetes in Docker
- **kubectl**: Kubernetes CLI
- **Go 1.21+**: For running tests

### Installation

```bash
# Install KinD
go install sigs.k8s.io/kind@latest

# Install kubectl (if not already installed)
# See: https://kubernetes.io/docs/tasks/tools/

# Verify installation
kind --version
kubectl version --client
docker --version
```

## Running Tests

### Quick Test

```bash
# Run the main Kubernetes E2E test
cd test/e2e/k8s
go test -run TestKubernetesEndToEnd -v
```

### All Tests

```bash
# Run all Kubernetes tests
go test -v

# Run with timeout for CI
go test -timeout 30m -v
```

### Skip Long Tests

```bash
# Skip performance and failover tests
go test -short -v
```

## Test Scenarios

### 1. TestKubernetesEndToEnd

**Duration**: ~10-15 minutes

**Coverage**:
- KinD cluster creation (3 nodes: 1 control-plane, 2 workers)
- Docker image building and loading
- Kubernetes deployment (Redis, Gateway, Test MCP Server)
- MCP protocol handshake and initialization
- Tool discovery and execution
- Load balancing verification
- Metrics collection validation

**Success Criteria**:
- All pods reach Ready state
- MCP client successfully connects and initializes
- Tools are discovered and executable
- Requests are distributed across backends
- Metrics are collected and accessible

### 2. TestKubernetesPerformance

**Duration**: ~20-30 minutes

**Coverage**:
- High-throughput testing with multiple replicas (200+ concurrent requests)
- Resource utilization monitoring via Kubernetes metrics
- Scaling behavior validation (dynamic replica scaling)
- Network performance testing (latency and throughput)
- Load balancing verification across scaled replicas

**Success Criteria**:
- Achieves minimum 20 req/sec throughput in Kubernetes
- 95%+ success rate under load
- P95 latency under 2 seconds
- Resource metrics are accessible and valid
- Load balancing works across multiple scaled replicas

### 3. TestKubernetesFailover

**Duration**: ~15-20 minutes

**Coverage**:
- Pod failure and recovery scenarios (forced pod deletion)
- Service endpoint updates during pod restarts
- Rolling update testing with service availability validation
- Network partition handling (simulated via network policies)
- System resilience under rapid request patterns

**Success Criteria**:
- System recovers functionality within 10 attempts after pod failure
- Service endpoints update correctly after pod restarts  
- 70%+ success rate maintained during rolling updates
- 50%+ success rate maintained during network partitions
- Automatic log collection on test failures

## Test Configuration

### Environment Variables

- `K8S_TEST_CLUSTER_NAME`: KinD cluster name (default: "mcp-e2e-test")
- `K8S_TEST_NAMESPACE`: Test namespace (default: "mcp-e2e")
- `K8S_TEST_TIMEOUT`: Test timeout (default: "10m")

### Port Mappings

- `30443`: Gateway HTTP/WebSocket (NodePort)
- `30091`: Gateway metrics (NodePort)
- `8080`: HTTP ingress (host port)
- `8443`: HTTPS ingress (host port)

## Debugging

### Automatic Log Collection

The test suite automatically saves diagnostic logs when tests fail or panic. Logs are saved to the `test-logs/` directory with timestamps and include:

- **Service Logs**: mcp-gateway, test-mcp-server, redis
- **Pod Status**: Current pod states and resource usage
- **Events**: Kubernetes events with timestamps
- **Deployments**: Deployment status and configurations
- **Services**: Service endpoints and configurations

Example log files:
```
test-logs/
├── mcp-gateway-20240315-143022.log
├── test-mcp-server-20240315-143022.log
├── pod-status-20240315-143022.txt
├── pod-events-20240315-143022.txt
├── deployments-20240315-143022.txt
└── services-20240315-143022.txt
```

### View Logs

```bash
# Gateway logs
kubectl logs -n mcp-e2e deployment/mcp-gateway -c gateway

# Router logs  
kubectl logs -n mcp-e2e deployment/mcp-gateway -c router

# Test MCP Server logs
kubectl logs -n mcp-e2e deployment/test-mcp-server

# All logs
kubectl logs -n mcp-e2e --all-containers=true --selector=app=mcp-gateway
```

### Manual Testing

```bash
# Port forward for direct access
kubectl port-forward -n mcp-e2e service/mcp-gateway 8443:8443

# Test gateway health
curl http://localhost:8443/health

# Test metrics
curl http://localhost:9091/metrics
```

### Cluster Inspection

```bash
# Check pod status
kubectl get pods -n mcp-e2e

# Check services
kubectl get services -n mcp-e2e

# Check events
kubectl get events -n mcp-e2e --sort-by='.lastTimestamp'

# Describe problematic pods
kubectl describe pod -n mcp-e2e <pod-name>
```

## Cleanup

Tests automatically clean up resources, but manual cleanup may be needed:

```bash
# Delete test cluster
kind delete cluster --name mcp-e2e-test

# Clean up Docker images
docker rmi mcp-gateway:test mcp-router:test test-mcp-server:test
```

## Troubleshooting

### Common Issues

1. **Kind cluster creation fails**
   - Ensure Docker is running
   - Check available disk space
   - Verify ports 8080, 8443, 30080, 30443 are available

2. **Image build failures**
   - Verify Dockerfiles exist in expected locations
   - Check Docker daemon accessibility
   - Ensure sufficient disk space

3. **Pod startup failures**
   - Check image pull policy (should be "Never" for local images)
   - Verify resource limits are appropriate
   - Check ConfigMap and Secret configurations

4. **Test timeouts**
   - Increase test timeout with `-timeout` flag
   - Check system resources (CPU, memory)
   - Verify network connectivity

5. **Load balancing tests fail**
   - Ensure multiple backend replicas are running
   - Check service selector matches pod labels
   - Verify backend health checks are passing

### Performance Considerations

- **Minimum Resources**: 4GB RAM, 2 CPU cores recommended
- **Concurrent Tests**: Only run one K8s E2E test at a time
- **CI/CD**: Consider using separate agents for K8s tests

## Contributing

When adding new tests:

1. Follow the existing pattern for test structure
2. Add appropriate cleanup functions
3. Include comprehensive logging
4. Update this README with new test descriptions
5. Test on both Linux and macOS (if possible)

## Integration with CI/CD

A complete GitHub Actions workflow has been provided at `.github/workflows/k8s-e2e.yml` that includes:

- **Matrix Testing**: Basic E2E, Performance, and Failover tests
- **PR Integration**: Runs essential tests on pull requests
- **Scheduled Runs**: Daily testing to catch regressions
- **Artifact Collection**: Logs and debugging information
- **Resource Cleanup**: Automatic cleanup after tests

The workflow automatically triggers on:
- Push to main/develop branches
- Pull requests affecting relevant code
- Daily schedule (2 AM UTC)
- Manual dispatch with custom parameters

To integrate, the workflow is already in place at:
```
.github/workflows/k8s-e2e.yml
```

## Future Enhancements

- [ ] Helm chart testing integration
- [ ] Multi-cluster testing scenarios
- [ ] Service mesh integration testing
- [ ] Advanced failure injection testing
- [ ] Performance benchmarking automation
- [ ] Security scanning integration