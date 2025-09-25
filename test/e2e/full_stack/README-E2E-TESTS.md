# Comprehensive E2E Test Suite

This document describes the comprehensive end-to-end test suite that validates all production features of the MCP Bridge system.

## Test Coverage Overview

### ✅ Core Functionality Tests
- **Basic MCP Flow**: Initialization, tool listing, tool execution
- **Multiple Tool Types**: Math operations, echo, error handling
- **Concurrent Operations**: Parallel request handling
- **Network Resilience**: Gateway restart and recovery

### ✅ Authentication & Security Tests
- **JWT Token Validation**: Valid token acceptance
- **Token Expiration**: Expired token rejection
- **Invalid Token Rejection**: Wrong signatures, malformed tokens
- **Security Headers**: HSTS, XSS protection, content type options
- **TLS Configuration**: Certificate validation, protocol versions

### ✅ Load Balancing & Service Discovery Tests
- **Round Robin Load Balancing**: Request distribution across backends
- **Health Check Validation**: Unhealthy backend exclusion
- **Service Discovery**: Dynamic backend addition
- **Backend Failover**: Graceful handling of backend failures

### ✅ Rate Limiting & Circuit Breaker Tests
- **Rate Limit Enforcement**: Request throttling under load
- **Circuit Breaker Activation**: Fast failure when backends down
- **Circuit Breaker Recovery**: Automatic recovery when backends return
- **Connection Limits**: Maximum concurrent connection enforcement

### ✅ Redis Session Management Tests
- **Session Persistence**: Session storage in Redis
- **Session Expiration**: TTL-based session cleanup
- **Redis Failure Handling**: Graceful degradation on Redis failure
- **Session Cleanup**: Automatic cleanup of expired sessions

### ✅ Protocol Variation Tests
- **WebSocket Protocol**: JSON-RPC over WebSocket
- **Binary TCP Protocol**: High-performance binary transport
- **Protocol Negotiation**: Version compatibility handling
- **Protocol Performance Comparison**: Throughput and latency metrics

### ✅ Comprehensive Fault Tolerance Tests
- **Partial System Degradation**: Service continues with limited capacity
- **Cascading Failures**: Graceful degradation as components fail
- **Graceful Shutdown**: Clean termination of ongoing operations
- **Data Consistency**: Consistent responses across multiple backends

### ✅ Performance & Scale Tests
- **High Throughput Testing**: 1000+ requests with 50 concurrent workers
- **Connection Pooling**: Efficient connection reuse validation
- **Memory Usage**: Memory leak detection and resource monitoring
- **Latency Under Load**: P50/P95/P99 latency measurements

### ✅ Monitoring & Metrics Tests
- **Metrics Collection**: Prometheus metrics validation
- **Health Endpoints**: Service health status verification
- **Distributed Tracing**: Request tracing across components
- **Logging Validation**: Structured logging format verification

## Test Architecture

### Docker Compose Configurations

1. **Standard Configuration** (`docker-compose.e2e.yml`)
   - Single backend for basic testing
   - Redis, Gateway, Test MCP Server

2. **Multi-Backend Configuration** (`docker-compose.multi-backend.yml`)
   - Three backend instances for load balancing tests
   - Each backend identifies itself in responses

3. **Monitoring Configuration** (with `--profile monitoring`)
   - Includes Prometheus for metrics testing
   - Extended observability stack

### Test MCP Server Features

The test MCP server includes:
- **Backend Identification**: Each instance reports its backend ID
- **Multiple Tool Types**: Echo, math operations, error generation
- **Health Checks**: Status reporting with backend information
- **Environment Configuration**: Configurable via BACKEND_ID env var

### Gateway Configurations

1. **Standard Gateway** (`gateway-test.yaml`)
   - Single backend routing
   - Standard rate limits and timeouts

2. **Multi-Backend Gateway** (`gateway-multi-backend.yaml`)
   - Round-robin load balancing
   - Health check configuration
   - Multiple backend endpoints

3. **Rate-Limited Gateway** (`gateway-rate-limited.yaml`)
   - Stricter rate limits for testing
   - Lower connection limits
   - Fast circuit breaker activation

## Running the Tests

### Prerequisites
- Docker and Docker Compose
- Go 1.21+
- Sufficient system resources (4GB+ RAM recommended)

### Individual Test Suites

```bash
# Basic functionality tests
go test -run TestFullStackEndToEnd -v

# Authentication tests
go test -run TestAuthenticationSecurity -v

# Load balancing tests  
go test -run TestLoadBalancingAndDiscovery -v

# Rate limiting tests
go test -run TestRateLimitingAndCircuitBreakers -v

# Session management tests
go test -run TestRedisSessionManagement -v

# Protocol tests
go test -run TestProtocolVariations -v

# Fault tolerance tests
go test -run TestComprehensiveFaultTolerance -v

# Performance tests (long-running)
go test -run TestPerformanceAndScale -v

# Monitoring tests
go test -run TestMonitoringAndMetrics -v
```

### All Tests
```bash
# Run all tests (can take 30+ minutes)
go test -v

# Run all tests excluding performance tests
go test -short -v

# Run with timeout for CI
go test -timeout 60m -v
```

### Benchmark Tests
```bash
# Performance benchmarks
go test -bench=. -benchtime=30s
```

## Test Configuration

### Environment Variables
- `BACKEND_ID`: Identifies backend instances in multi-backend tests
- `JWT_SECRET_KEY`: JWT signing secret for authentication tests
- `MCP_AUTH_TOKEN`: Bearer token for router authentication

### Port Allocation
- `8443`: Gateway HTTPS/WebSocket
- `9091`: Gateway metrics
- `9092`: Gateway health
- `3001-3003`: Backend instances
- `6379`: Redis
- `9093`: Prometheus (monitoring profile)

## Expected Test Results

### Success Criteria
- **Basic Tests**: 100% success rate
- **Authentication Tests**: Proper token validation and rejection
- **Load Balancing**: Even distribution across backends
- **Rate Limiting**: Appropriate throttling under load
- **Performance**: >10 req/sec throughput, <2s P95 latency
- **Fault Tolerance**: Graceful degradation and recovery

### Common Failure Scenarios
- **Port Conflicts**: Ensure ports 3001-3003, 6379, 8443, 9091-9093 are available
- **Resource Constraints**: Tests may fail with <4GB RAM
- **Docker Issues**: Ensure Docker daemon is running and accessible
- **Network Issues**: Corporate firewalls may block container networking

## Troubleshooting

### Docker Logs
```bash
# View gateway logs
docker-compose -f docker-compose.e2e.yml logs gateway

# View backend logs
docker-compose -f docker-compose.multi-backend.yml logs backend-1

# View all logs
docker-compose -f docker-compose.e2e.yml logs
```

### Test Debugging
```bash
# Run with verbose output
go test -v -run TestSpecificTest

# Run with race detection
go test -race -v

# Generate test coverage
go test -coverprofile=coverage.out -v
go tool cover -html=coverage.out
```

### Common Issues

1. **Authentication Test Failures**
   - Check JWT_SECRET_KEY environment variable
   - Verify token generation logic

2. **Load Balancing Test Failures**
   - Ensure all backend containers are healthy
   - Check network connectivity between containers

3. **Performance Test Failures**
   - Increase system resources
   - Check for background processes consuming CPU/memory

4. **Redis Test Failures**
   - Verify Redis container is running
   - Check Redis connectivity from gateway

## Contributing

When adding new tests:

1. Follow the established naming convention: `Test<Category><Feature>`
2. Add comprehensive error handling and cleanup
3. Include performance assertions where appropriate
4. Document any new Docker configurations
5. Update this README with new test descriptions

## Test Maintenance

### Regular Updates Required
- **Docker Images**: Keep base images updated for security
- **Dependencies**: Update Go modules regularly
- **Timeouts**: Adjust based on CI/CD environment performance
- **Resource Limits**: Monitor and adjust for different environments

### Performance Baselines
Current performance baselines (may vary by hardware):
- **Throughput**: 50-200 req/sec (depending on complexity)
- **P95 Latency**: <2 seconds
- **Memory Usage**: <100MB increase per 100 requests
- **Connection Setup**: <500ms average

These baselines should be updated as the system evolves.