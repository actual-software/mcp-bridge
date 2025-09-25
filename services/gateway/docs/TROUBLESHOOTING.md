# Troubleshooting Guide

This guide covers common issues and their solutions for MCP Gateway.

## Test Failures

### Rate Limiting Tests Failing

#### Circuit Breaker Test Timeouts
**Problem**: `TestRedisRateLimiterWithCircuitBreaker_*` tests failing with timeouts or wrong state expectations.

**Solution**:
```bash
# Check Redis connectivity in tests
go test ./internal/ratelimit -v -run TestRedisRateLimiterWithCircuitBreaker
```

**Common Causes**:
- Mock Redis expectations not matching actual calls
- Circuit breaker state transitions not understood
- Test using connected Redis instead of failure simulation

**Fix**: Use disconnected Redis clients for failure testing:
```go
// Create a disconnected Redis client to simulate failures
client := redis.NewClient(&redis.Options{
    Addr: "localhost:16379", // Non-existent Redis instance
})
```

#### Burst Rate Limit Tests Slow
**Problem**: `TestInMemoryRateLimiter_Burst` taking too long due to 10-second sleep.

**Solution**: Configure shorter burst window for testing:
```go
limiter := NewInMemoryRateLimiter(logger)
limiter.SetBurstWindow(50 * time.Millisecond) // Fast testing
```

### Server Configuration Tests Failing

#### Service Discovery Configuration Issues
**Problem**: `TestLoadConfig` failing with empty service discovery mode.

**Root Cause**: Mismatch between YAML key and Go struct mapstructure tag.

**Solution**: Ensure YAML uses `service_discovery:` and struct uses `mapstructure:"service_discovery"`:
```yaml
service_discovery:
  provider: kubernetes
  namespace_selector:
    - mcp-tools
```

#### Validation Tests Not Catching Errors
**Problem**: `TestValidate` tests expecting errors but validation passes incorrectly.

**Root Cause**: Validation checking `Mode` field but tests setting `Provider` field.

**Solution**: Update validation to check both fields:
```go
// Get the effective provider (prefer Provider over Mode)
provider := cfg.Discovery.Provider
if provider == "" && cfg.Discovery.Mode != "" {
    provider = cfg.Discovery.Mode
}
```

### TCP Connection Tests

#### TCP Handler Panics with Nil Pointer
**Problem**: `TestTCPPerIPLimits` panicking with nil pointer dereference in metrics.

**Root Cause**: TCP handler created without proper metrics registry.

**Solution**: Use `NewTCPHandler()` with all dependencies:
```go
s.tcpHandler = NewTCPHandler(
    logger,
    nil,        // auth provider
    nil,        // router
    nil,        // sessions
    metricsReg, // metrics registry - REQUIRED
    nil,        // rate limiter
    nil,        // message authenticator
)
```

#### Control Message Tests Hanging
**Problem**: `TestShutdownControlMessage` hanging indefinitely.

**Root Cause**: Using `net.Pipe()` which blocks writes until there's a concurrent reader.

**Solution**: Make read operations concurrent:
```go
// Start reading in a goroutine
go func() {
    defer close(done)
    msgType, msg, readErr = serverTransport.ReceiveMessage()
}()

// Now write won't block
err := clientTransport.SendControl(control)
```

## Runtime Issues

### Connection Failures

#### WebSocket Upgrade Failures
**Problem**: Clients can't establish WebSocket connections.

**Diagnostic**:
```bash
curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Authorization: Bearer $TOKEN" \
  https://gateway:8443/
```

**Common Causes**:
- Invalid JWT token
- Missing Authorization header  
- TLS certificate issues
- Connection limits exceeded

#### TCP Connection Refused
**Problem**: TCP clients can't connect to binary protocol port.

**Diagnostic**:
```bash
# Check if TCP port is open
netstat -ln | grep :9001

# Test TCP connection
telnet gateway-host 9001
```

**Solutions**:
- Verify `protocol: "tcp"` or `protocol: "both"` in config
- Check `tcp_port` configuration
- Ensure TCP handler is initialized

### Service Discovery Issues

#### No MCP Servers Found
**Problem**: Gateway can't discover any MCP servers.

**Diagnostic**:
```bash
kubectl logs -n mcp-system deployment/mcp-gateway | grep discovery
```

**Common Causes**:
- Incorrect `namespace_selector` configuration
- MCP servers missing required annotations
- RBAC permissions insufficient

**Solution**:
```yaml
service_discovery:
  provider: kubernetes
  namespace_selector:
    - "mcp-*"  # Match your server namespaces
  label_selector:
    mcp-enabled: "true"
```

#### Service Discovery Access Denied
**Problem**: `forbidden: services is forbidden` errors.

**Root Cause**: Insufficient RBAC permissions for service account.

**Solution**: Ensure proper RBAC:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: mcp-gateway
rules:
- apiGroups: [""]
  resources: ["services", "endpoints"]
  verbs: ["get", "list", "watch"]
```

### Rate Limiting and Circuit Breaker Issues

#### Rate Limits Not Working
**Problem**: Requests not being rate limited despite configuration.

**Diagnostic**:
```bash
# Check rate limit metrics
curl http://gateway:9090/metrics | grep rate_limit
```

**Common Causes**:
- Rate limiting disabled in config
- JWT token missing rate limit claims
- Circuit breaker open (using in-memory fallback)

#### Redis Circuit Breaker Always Open
**Problem**: Circuit breaker stuck in open state.

**Diagnostic**:
```bash
# Check circuit breaker metrics
curl http://gateway:9090/metrics | grep circuit_breaker_state

# Check Redis connectivity
kubectl exec -it deployment/mcp-gateway -- redis-cli -h redis ping
```

**Solutions**:
- Verify Redis connectivity and health
- Check circuit breaker timeout configuration
- Review Redis error patterns in logs

### Performance Issues

#### High Memory Usage
**Problem**: Gateway consuming excessive memory.

**Diagnostic**:
```bash
# Check Go runtime metrics
curl http://gateway:9090/metrics | grep go_memstats
```

**Common Causes**:
- Connection leaks (check connection cleanup)
- In-memory rate limiter not cleaning up old keys
- Session storage growing unbounded

**Solutions**:
- Configure appropriate cleanup intervals
- Monitor connection metrics
- Use Redis for session storage in production

#### Slow Response Times
**Problem**: Gateway responses are slow.

**Diagnostic**:
```bash
# Check request duration metrics
curl http://gateway:9090/metrics | grep request_duration
```

**Solutions**:
- Check upstream service health
- Monitor circuit breaker states
- Review Redis performance
- Scale gateway horizontally

## Configuration Issues

### YAML Parsing Errors
**Problem**: Gateway fails to start with YAML parsing errors.

**Common Issues**:
- Incorrect indentation
- Missing required fields
- Type mismatches

**Solution**: Validate YAML syntax:
```bash
# Check YAML syntax
yamllint /path/to/gateway.yaml

# Validate against expected structure
kubectl create configmap test-config --from-file=gateway.yaml --dry-run=client
```

### Environment Variable Override Issues
**Problem**: Environment variables not overriding config file values.

**Root Cause**: Incorrect environment variable naming.

**Solution**: Use proper naming convention:
```bash
# Config: server.max_connections
export MCP_GATEWAY_SERVER_MAX_CONNECTIONS=10000

# Config: rate_limit.redis.url
export MCP_GATEWAY_RATE_LIMIT_REDIS_URL="redis://redis:6379"
```

### Secret Loading Failures
**Problem**: JWT secrets or Redis passwords not loading.

**Diagnostic**:
```bash
kubectl logs deployment/mcp-gateway | grep -i secret
```

**Solutions**:
- Verify environment variables are set
- Check secret mounting in deployment
- Ensure proper secret permissions

## Prometheus Metrics Issues

### Duplicate Metrics Registration
**Problem**: `duplicate metrics collector registration attempted` panic.

**Root Cause**: Global Prometheus registry conflicts in tests.

**Solution**: Use isolated registries for testing:
```go
// Create isolated registry for tests
reg := prometheus.NewRegistry()
factory := promauto.With(reg)
```

### Metrics Not Updating
**Problem**: Prometheus metrics show stale or zero values.

**Diagnostic**:
```bash
# Check if metrics endpoint is accessible
curl http://gateway:9090/metrics

# Look for specific metrics
curl http://gateway:9090/metrics | grep mcp_gateway
```

**Solutions**:
- Verify metrics are enabled in config
- Check that metrics registry is properly initialized
- Ensure metric collection is not being blocked

## Debugging Tools

### Enable Debug Logging
```yaml
logging:
  level: debug
  format: json
```

### Health Check Endpoints
- `/healthz`: Basic liveness check
- `/ready`: Readiness check including dependencies
- `/health`: Detailed health status with component states

### Metrics Endpoints
- `/metrics`: Prometheus metrics
- `/debug/pprof/`: Go profiling endpoints (if enabled)

### Admin Commands
```bash
# Check gateway status
kubectl exec deployment/mcp-gateway -- mcp-gateway admin status

# List discovered services
kubectl exec deployment/mcp-gateway -- mcp-gateway admin services

# Check circuit breaker states
kubectl exec deployment/mcp-gateway -- mcp-gateway admin circuits
```