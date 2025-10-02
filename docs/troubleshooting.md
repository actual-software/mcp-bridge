# MCP Bridge Troubleshooting Guide with Universal Protocol Support

This guide helps diagnose and resolve common issues with the MCP Bridge system featuring universal protocol support (stdio, WebSocket, HTTP, SSE, TCP Binary), based on real debugging experiences from development and E2E testing.

## Table of Contents

- [Quick Start Troubleshooting](#quick-start-troubleshooting)
- [E2E Test Debugging (Real Cases)](#e2e-test-debugging-real-cases)
- [Universal Protocol Debugging](#universal-protocol-debugging)
- [Connection Issues](#connection-issues)
- [Authentication Failures](#authentication-failures)
- [Configuration Issues](#configuration-issues)
- [Performance Problems](#performance-problems)
- [Secure Storage Issues](#secure-storage-issues)
- [Gateway Issues](#gateway-issues)
- [Binary Protocol Issues](#binary-protocol-issues)
- [Debug Tools](#debug-tools)
- [Enhanced Monitoring](#enhanced-monitoring)
- [Common Error Messages](#common-error-messages)

## Quick Start Troubleshooting

### Run Automated Diagnostics

```bash
# Get system status and diagnostics
make troubleshoot

# Reset development environment completely
make dev-reset

# Quick validation of your setup
make dev-check

# Full E2E test validation
make test-e2e-validated
```

## E2E Test Debugging (Real Cases)

### Case Study 1: "client not initialized" Errors

**Symptoms:**
- E2E tests failing with "client not initialized"
- Tests timing out during BasicMCPFlow
- Connection established but requests fail

**Root Cause Analysis:**
This wasn't actually a client initialization issue but underlying connection problems masked by the symptom.

**Debugging Steps:**
```bash
# Step 1: Check service health
curl -f http://localhost:9443/health
curl -f http://localhost:3000/health

# Step 2: Examine gateway logs for real errors
docker-compose logs gateway | grep -E "(error|ERROR|failed)"

# Step 3: Test basic router connectivity
timeout 10s ./services/router/bin/mcp-router --config ~/.config/claude-cli/mcp-router.yaml --version
```

**Solution Found:**
The issue was TLS certificate verification failures, not client initialization.

### Case Study 2: TLS Certificate Verification Failures

**Symptoms:**
```
ERROR x509: 'localhost' certificate is not trusted
ERROR Failed to connect to gateway: tls: bad certificate
```

**Root Causes:**
1. Absolute paths in certificate configuration
2. Wrong TLS configuration structure in YAML
3. Certificate verification enabled in development

**Solutions Applied:**

1. **Fixed Certificate Paths:**
```yaml
# Before (failed):
ca_file: /Users/poile/repos/mcp/test/e2e/full_stack/certs/ca.crt

# After (works):
ca_file: "./certs/ca.crt"
```

2. **Fixed TLS Configuration Structure:**
```yaml
# Before (wrong structure):
connection:
  tls:
    verify: false

# After (correct structure):
tls:
  verify: false
```

3. **Proper TLS Configuration (RECOMMENDED):**
```yaml
# ALWAYS use proper TLS verification - even in development
tls:
  ca_file: "./certs/ca.crt"
  verify: true
  min_version: "1.2"
```

**🔒 SECURITY BEST PRACTICE:** Never disable TLS verification with `verify: false`. Generate proper certificates instead using:
```bash
# Generate certificates with proper SAN entries
./scripts/install.sh --environment development
# OR
make quickstart  # Auto-generates certificates
```

### Case Study 3: JWT Authentication Mismatch

**Symptoms:**
```
ERROR HTTP 401 Unauthorized
ERROR websocket: bad handshake
```

**Debugging Process:**
```bash
# Decode JWT token to inspect claims
echo $MCP_AUTH_TOKEN | cut -d. -f2 | base64 -d | jq

# Expected claims (from gateway config):
# - issuer: "mcp-gateway-e2e" 
# - audience: "mcp-clients"

# Common mismatches:
# - iss vs issuer field names (both valid)
# - Different issuer strings
# - Missing or incorrect audience
```

**Solution - Regenerate JWT with Correct Claims:**
```bash
# Generate proper JWT token
JWT_SECRET=$(openssl rand -base64 32)
./scripts/generate-jwt.sh "$JWT_SECRET" "mcp-gateway-e2e" "mcp-clients"
```

**Working JWT Payload:**
```json
{
  "iss": "mcp-gateway-e2e",
  "aud": "mcp-clients", 
  "sub": "test-client",
  "iat": 1640995200,
  "exp": 1641081600,
  "jti": "unique-token-id"
}
```

### Case Study 4: Service Discovery Namespace Missing

**Symptoms:**
- Authentication works but requests fail
- Gateway logs show "namespace not found"
- MCP initialize requests failing

**Root Cause:**
Gateway wasn't loading the "system" namespace, which is required for MCP protocol initialization.

**Required Namespaces:**
```yaml
service_discovery:
  static:
    endpoints:
      default:
        - url: "http://test-mcp-server:3000/mcp"
      system:  # CRITICAL - Required for MCP initialize
        - url: "http://test-mcp-server:3000/mcp"
      test:
        - url: "http://test-mcp-server:3000/mcp"
      calc:
        - url: "http://test-mcp-server:3000/mcp"
      tools:
        - url: "http://test-mcp-server:3000/mcp"
```

### Case Study 5: Race Conditions in Concurrent Requests

**Symptoms:**
- Intermittent test failures
- Duplicate response handling
- Goroutine panics during concurrent tests

**Root Cause:**
Improper synchronization in response handling - multiple goroutines processing the same response ID.

**Solution - Fixed Race Condition:**
```go
// Before (race condition):
rc.pendingMu.Lock()
if ch, exists := rc.pending[respID]; exists {
    rc.pendingMu.Unlock()
    ch <- line  // Race: might send to closed channel
    delete(rc.pending, respID)  // Race: delete after unlock
}

// After (fixed):
rc.pendingMu.Lock()
if ch, exists := rc.pending[respID]; exists {
    delete(rc.pending, respID) // Remove BEFORE sending
    rc.pendingMu.Unlock()
    
    select {
    case ch <- line:
        // Success
    default:
        // Channel full/closed - handle gracefully
    }
} else {
    rc.pendingMu.Unlock()
}
```

### Case Study 6: Gateway Error Propagation Bug - Null ID Timeouts

**Symptoms:**
- E2E tests timing out after 10-30 seconds
- Gateway receiving requests but not responding
- Client hanging on error responses

**Root Cause:**
Critical gateway bug where errors were propagated with `"id": null` instead of the request ID, causing clients to wait indefinitely for responses that would never match their pending requests.

**Debugging Process:**
```bash
# Step 1: Check gateway error handling
docker-compose logs gateway | grep -A5 -B5 '"id":null'

# Step 2: Monitor client request/response matching
# Troubleshooting only - NOT for production:
# For troubleshooting only:
mcp-router --log-level debug --debug.log-frames | grep -E "(request_id|response_id)"

# Step 3: Measure actual response times
time curl -X POST http://localhost:9443/invalid-endpoint
```

**Solution - Fixed Error ID Propagation:**
```go
// Before (bug - null ID):
func (h *Handler) sendError(ctx context.Context, err error) {
    response := ErrorResponse{
        ID:    nil,  // BUG: Should preserve request ID
        Error: err.Error(),
    }
    h.writeResponse(ctx, response)
}

// After (fixed - preserve request ID):
func (h *Handler) sendError(ctx context.Context, requestID interface{}, err error) {
    response := ErrorResponse{
        ID:    requestID,  // FIXED: Preserve original request ID
        Error: err.Error(),
    }
    h.writeResponse(ctx, response)
}
```

**Performance Impact:**
- **Before**: 10-30 second timeouts on error responses
- **After**: ~22ms immediate error responses
- **Improvement**: 1000x faster error handling

### Case Study 7: Redis Session Management - Docker Port Mapping

**Symptoms:**
- Authentication works but session persistence fails
- Tests passing individually but failing in session cleanup
- Redis connection errors in gateway logs: `dial tcp: connection refused`

**Root Cause:**
Docker Compose configuration missing Redis port mapping, preventing gateway from connecting to Redis for session storage.

**Debugging Steps:**
```bash
# Step 1: Check Redis container status
docker-compose ps redis

# Step 2: Test Redis connectivity from gateway
docker-compose exec gateway redis-cli -h redis -p 6379 ping

# Step 3: Check port mapping
docker-compose port redis 6379
```

**Solution - Fixed Docker Compose Configuration:**
```yaml
# Before (missing port mapping):
services:
  redis:
    image: redis:7-alpine
    # Missing port mapping

# After (proper port mapping):
services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"  # Required for gateway connectivity
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5
```

**Additional Redis Configuration:**
```yaml
# Gateway configuration for Redis sessions
session:
  store: redis
  redis:
    addr: "redis:6379"
    password: ""
    db: 0
    max_retries: 3
    dial_timeout: 5s
    read_timeout: 3s
    write_timeout: 3s
```

### Case Study 8: Connection Resource Exhaustion and Tuning

**Symptoms:**
- System becoming unresponsive during concurrent tests
- "Too many open files" errors
- Connection pool exhaustion messages

**Root Cause:**
Concurrent tests opening 150+ connections simultaneously, exhausting system file descriptors and connection pools.

**Resource Analysis:**
```bash
# Check current connection limits
ulimit -n

# Monitor open connections during tests
lsof -p $(pgrep mcp-router) | grep -c TCP

# Check connection pool status
curl http://localhost:9091/metrics | grep connection_pool
```

**Solution - Optimized Connection Management:**
```yaml
# Before (resource exhaustion):
advanced:
  connection:
    pool:
      max_size: 150  # Too high for testing
      min_size: 50

# After (optimized for stability):
advanced:
  connection:
    pool:
      max_size: 10   # Reduced to prevent exhaustion
      min_size: 2
      idle_timeout: 30s
      max_lifetime: 300s
```

**Performance Tuning Guidelines:**
```yaml
# Development environment (resource-constrained)
connection:
  pool:
    max_size: 10
    concurrent_requests: 5

# Production environment (high-performance)
connection:
  pool:
    max_size: 100
    concurrent_requests: 50

# Load testing environment (controlled stress)
connection:
  pool:
    max_size: 25
    concurrent_requests: 15
```

**System-Level Tuning:**
```bash
# Increase file descriptor limits for production
echo "fs.file-max = 65536" >> /etc/sysctl.conf
echo "* soft nofile 65536" >> /etc/security/limits.conf
echo "* hard nofile 65536" >> /etc/security/limits.conf

# Verify limits after restart
ulimit -n
```

### Case Study 9: RouterController Authentication Pattern Updates

**Symptoms:**
- Protocol tests failing authentication
- Inconsistent JWT token handling across test suites
- WebSocket handshake failures with valid tokens

**Root Cause:**
Test infrastructure not using proper RouterController authentication patterns, leading to token mismatches and authentication bypass attempts.

**Debugging Authentication Flow:**
```bash
# Step 1: Validate JWT token structure
echo $MCP_AUTH_TOKEN | cut -d. -f2 | base64 -d | jq .

# Step 2: Check RouterController initialization
grep -r "RouterController" test/ | grep -v ".go:"

# Step 3: Verify authentication headers
curl -I -H "Authorization: Bearer $MCP_AUTH_TOKEN" https://localhost:9443/
```

**Solution - Proper RouterController Integration:**
```go
// Before (inconsistent authentication):
func setupTest() *TestClient {
    // Various ad-hoc authentication methods
    client := &TestClient{
        token: hardcodedToken,
    }
    return client
}

// After (consistent RouterController pattern):
func setupTest() *RouterController {
    rc := &RouterController{
        gatewayURL: testGatewayURL,
        authToken:  generateValidJWT(),
        config:     loadTestConfig(),
    }
    
    // Ensure proper authentication flow
    err := rc.authenticate()
    if err != nil {
        t.Fatalf("Authentication failed: %v", err)
    }
    
    return rc
}
```

**Updated JWT Token Generation:**
```go
// Consistent token generation for all tests
func generateValidJWT() string {
    claims := jwt.MapClaims{
        "iss": "mcp-gateway-e2e",      // Must match gateway config
        "aud": "mcp-clients",          // Must match expected audience
        "sub": "test-e2e-client",      // Consistent subject
        "iat": time.Now().Unix(),
        "exp": time.Now().Add(24*time.Hour).Unix(),
        "jti": uuid.New().String(),    // Unique token ID
    }
    
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    tokenString, _ := token.SignedString([]byte(jwtSecret))
    return tokenString
}
```

**Authentication Best Practices:**
```yaml
# Test configuration - consistent with gateway
auth:
  provider: jwt
  jwt:
    issuer: "mcp-gateway-e2e"
    audience: ["mcp-clients"]
    signing_key: "test-jwt-secret-key"
    token_lifetime: "24h"
```

## Universal Protocol Debugging

### Protocol Auto-Detection Issues

#### Symptoms
- "protocol_detection_failed" errors
- Inconsistent protocol selection
- Connections falling back to wrong protocols
- Poor auto-detection accuracy (< 95%)

#### Debugging Protocol Detection
```bash
# Check current detection accuracy
curl -s http://localhost:9090/metrics | grep protocol_detection_accuracy

# Test manual protocol detection
curl -X POST http://localhost:9091/api/v1/protocols/detect \
  -H "Content-Type: application/json" \
  -d '{
    "test_endpoints": [
      "ws://localhost:8080",
      "http://localhost:8081",
      "/tmp/mcp-servers/weather.sock"
    ],
    "timeout": "2s"
  }'

# Check protocol detection cache
curl http://localhost:9091/api/v1/protocols/cache

# View failed detections
curl http://localhost:9091/api/v1/protocols/detect/failures
```

#### Solutions

1. **Increase detection timeout:**
   ```yaml
   direct_clients:
     auto_detection:
       timeout: "3s"  # Increased from default 1s
       retry_attempts: 3
       cache_duration: "10m"
   ```

2. **Clear protocol detection cache:**
   ```bash
   # Clear stale cache entries
   curl -X POST http://localhost:9091/api/v1/protocols/cache/clear
   
   # Force re-detection
   curl -X POST http://localhost:9091/api/v1/protocols/detect/refresh
   ```

3. **Manual protocol override:**
   ```bash
   # Force specific protocol for debugging
   curl -X POST https://mcp-gateway.your-domain.com/api/v1/execute \
     -H "Authorization: Bearer $MCP_AUTH_TOKEN" \
     -H "X-Force-Protocol: stdio" \
     -d '{"jsonrpc":"2.0","method":"ping","id":1}'
   ```

### Cross-Protocol Load Balancing Issues

#### Symptoms
- Uneven request distribution across protocols
- High latency despite protocol optimization
- Load balancer selecting suboptimal protocols
- "cross_protocol_load_balance_failed" errors

#### Debugging Load Balancing
```bash
# Check load balancer status
curl https://mcp-gateway.your-domain.com/api/v1/load-balancer/status

# View protocol weights and selection
curl https://mcp-gateway.your-domain.com/api/v1/load-balancer/weights

# Monitor protocol selection distribution
curl -s http://localhost:9090/metrics | grep selected_protocol | sort

# Check protocol performance metrics
curl -s http://localhost:9090/metrics | grep protocol_latency
```

#### Solutions

1. **Rebalance protocol weights:**
   ```bash
   # Automatic rebalancing based on performance
   curl -X POST https://mcp-gateway.your-domain.com/api/v1/load-balancer/rebalance
   
   # Manual weight adjustment
   curl -X PUT https://mcp-gateway.your-domain.com/api/v1/load-balancer/weights \
     -H "Content-Type: application/json" \
     -d '{
       "stdio": 1.5,
       "websocket": 1.2,
       "http": 1.0,
       "sse": 0.8,
       "tcp_binary": 1.3
     }'
   ```

2. **Reset load balancer to defaults:**
   ```bash
   curl -X POST https://mcp-gateway.your-domain.com/api/v1/load-balancer/reset
   ```

3. **Configure load balancing strategy:**
   ```yaml
   load_balancing:
     strategy: "performance_aware"  # cross_protocol, protocol_specific, least_conn
     health_aware: true
     protocol_weights:
       stdio: 1.5      # Prefer stdio for performance
       tcp_binary: 1.3 # High performance binary
       websocket: 1.2  # Good balance
       http: 1.0       # Standard weight
       sse: 0.8        # Lower priority
   ```

### Direct Connection Optimization Issues

#### Symptoms
- Expected 65% latency improvement not achieved
- Direct connections falling back to gateway
- Connection pool inefficiency
- Memory optimization not active

#### Debugging Direct Connections
```bash
# Check direct client status
curl http://localhost:9091/health/direct-clients

# Monitor connection pool efficiency
curl -s http://localhost:9091/metrics | grep connection_pool

# Check memory optimization status
curl http://localhost:9091/api/v1/performance/memory-optimization

# Analyze protocol latency comparison
curl http://localhost:9091/api/v1/performance/protocols
```

#### Solutions

1. **Optimize connection pooling:**
   ```yaml
   direct_clients:
     connection_pool:
       max_idle_connections: 20
       max_active_connections: 10
       enable_connection_reuse: true
       health_check_interval: "30s"
   ```

2. **Enable memory optimization:**
   ```yaml
   direct_clients:
     memory_optimization:
       enable_object_pooling: true
       gc_config:
         enabled: true
         gc_percent: 75
   ```

3. **Force direct connection mode:**
   ```bash
   # Test direct connection bypassing gateway
   mcp-router --direct-mode stdio test --endpoint /tmp/mcp-servers/weather.sock
   ```

### Protocol-Specific Connection Issues

#### stdio Protocol Issues

**Symptoms:**
- "no such file or directory" errors
- Socket permission denied
- Subprocess communication failures

**Debugging:**
```bash
# Check socket existence and permissions
ls -la /tmp/mcp-servers/
ls -la /tmp/mcp-gateway.sock

# Test socket connectivity
echo '{"jsonrpc":"2.0","method":"ping","id":1}' | \
  socat - UNIX-CONNECT:/tmp/mcp-servers/weather.sock

# Check subprocess status
ps aux | grep weather_server.py
lsof /tmp/mcp-servers/weather.sock
```

**Solutions:**
```yaml
# Proper stdio configuration
backends:
  stdio:
    enabled: true
    socket_discovery: true
    socket_paths: ["/tmp/mcp-servers", "/var/run/mcp"]
    socket_permissions: "0666"
    timeout: "5s"
```

#### WebSocket Protocol Issues

**Symptoms:**
- Connection refused errors
- WebSocket handshake failures
- Compression negotiation failures

**Debugging:**
```bash
# Test WebSocket connectivity
wscat -c ws://localhost:8080 -x '{"jsonrpc":"2.0","method":"ping","id":1}'

# Check WebSocket server health
curl http://localhost:8080/health

# Monitor WebSocket frame errors
curl -s http://localhost:9090/metrics | grep websocket_frame_errors
```

**Solutions:**
```yaml
# Enhanced WebSocket configuration
backends:
  websocket:
    enabled: true
    compression: true
    max_frame_size: 1048576  # 1MB
    ping_interval: "30s"
    pong_timeout: "10s"
```

#### HTTP Protocol Issues

**Symptoms:**
- 404 not found errors
- Content-Type negotiation failures
- HTTP/2 compatibility issues

**Debugging:**
```bash
# Test HTTP endpoint
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"ping","id":1}'

# Check HTTP server configuration
curl -I http://localhost:8081/health

# Test content type negotiation
curl -H "Accept: application/json" http://localhost:8081/mcp
```

**Solutions:**
```yaml
# HTTP protocol configuration
backends:
  http:
    enabled: true
    api_prefix: "/mcp"
    content_types: ["application/json", "application/x-ndjson"]
    http_version: "1.1"  # or "2.0"
    timeout: "30s"
```

#### SSE (Server-Sent Events) Issues

**Symptoms:**
- Stream disconnections
- Event parsing failures
- Reconnection loops

**Debugging:**
```bash
# Test SSE endpoint
curl -N -H "Accept: text/event-stream" http://localhost:8082/events

# Monitor SSE reconnections
curl -s http://localhost:9090/metrics | grep sse_reconnection
```

**Solutions:**
```yaml
# SSE configuration
backends:
  sse:
    enabled: true
    reconnect_interval: "5s"
    max_retries: 5
    keepalive_interval: "30s"
```

#### TCP Binary Protocol Issues

**Symptoms:**
- Protocol frame errors
- Binary framing mismatches
- High CPU usage during binary processing

**Debugging:**
```bash
# Test TCP binary connection
nc -zv localhost 8444

# Check binary framing metrics
curl -s http://localhost:9090/metrics | grep tcp_binary_frame

# Monitor binary protocol performance
curl http://localhost:9091/api/v1/performance/tcp-binary
```

**Solutions:**
```yaml
# TCP Binary configuration
backends:
  tcp_binary:
    enabled: true
    framing: "length_prefixed"
    max_frame_size: 10485760  # 10MB
    compression: true
    timeout: "10s"
```

### Predictive Health Monitoring Issues

#### Symptoms
- False positive health alerts
- Anomaly detection not triggering
- ML model performance degradation
- Predictive accuracy below expectations

#### Debugging Predictive Health
```bash
# Check predictive health scores
curl -s http://localhost:9090/metrics | grep predictive_health_score

# View anomaly alerts
curl https://mcp-gateway.your-domain.com/api/v1/health/anomalies

# Check ML model status
curl https://mcp-gateway.your-domain.com/api/v1/health/ml-model/status

# Compare predictive vs traditional health
curl https://mcp-gateway.your-domain.com/api/v1/health/prediction-accuracy
```

#### Solutions

1. **Tune anomaly detection sensitivity:**
   ```yaml
   health_monitoring:
     predictive_enabled: true
     sensitivity: 0.8  # Reduce false positives
     training_window: "7d"
     min_data_points: 100
   ```

2. **Reset ML model with fresh data:**
   ```bash
   curl -X POST https://mcp-gateway.your-domain.com/api/v1/health/ml-model/retrain
   ```

3. **Fallback to traditional health monitoring:**
   ```yaml
   health_monitoring:
     predictive_enabled: false
     traditional_enabled: true
     health_check_interval: "10s"
   ```

## Connection Issues

### Router Cannot Connect to Gateway

#### Symptoms
```
ERROR Failed to connect to gateway: dial tcp: connection refused
```

#### Diagnose
```bash
# Test gateway connectivity
curl -v https://gateway.example.com/health

# Check DNS resolution
nslookup gateway.example.com

# Test WebSocket connectivity
wscat -c wss://gateway.example.com

# Check router logs
# For troubleshooting only:
mcp-router --log-level debug
```

#### Solutions

1. **Gateway is down**
   ```bash
   # Check gateway pods
   kubectl get pods -n mcp-system
   kubectl logs -n mcp-system deployment/mcp-gateway
   ```

2. **Firewall blocking connection**
   ```bash
   # Test connectivity
   telnet gateway.example.com 443
   
   # Check firewall rules
   sudo iptables -L -n | grep 443
   ```

3. **TLS certificate issues**
   ```yaml
   # Disable TLS verification (dev only)
   gateway:
     tls:
       verify: false
   ```

### Frequent Disconnections

#### Symptoms
- Connection drops every few minutes
- "WebSocket closed unexpectedly" errors

#### Diagnose
```bash
# Monitor connection stability
mcp-router test --continuous

# Check keepalive settings
grep keepalive ~/.config/claude-cli/mcp-router.yaml
```

#### Solutions

1. **Adjust keepalive interval**
   ```yaml
   gateway:
     connection:
       keepalive_interval_ms: 30000  # 30 seconds
   ```

2. **Network instability**
   ```bash
   # Test network stability
   ping -c 100 gateway.example.com
   mtr --report gateway.example.com
   ```

3. **Proxy interference**
   ```bash
   # Bypass proxy for testing
   export NO_PROXY=gateway.example.com
   mcp-router test
   ```

## Authentication Failures

### "Unauthorized" Error

#### Symptoms
```
ERROR Authentication failed: 401 Unauthorized
```

#### Diagnose
```bash
# Check token storage
mcp-router token list
mcp-router token doctor

# Verify token validity
mcp-router token get --name gateway-token | jwt decode -
```

#### Solutions

1. **Expired token**
   ```bash
   # Update token
   mcp-router token set --name gateway-token
   ```

2. **Wrong token type**
   ```yaml
   # Ensure correct auth type
   gateway:
     auth:
       type: bearer  # or oauth2, mtls
   ```

3. **Keychain access denied (macOS)**
   ```bash
   # Reset keychain access
   security unlock-keychain
   mcp-router token repair --name gateway-token
   ```

### OAuth2 Token Refresh Failures

#### Symptoms
```
ERROR Failed to refresh OAuth2 token: invalid_grant
```

#### Solutions

1. **Update client credentials**
   ```bash
   mcp-router token set --name oauth-secret
   ```

2. **Check token endpoint**
   ```bash
   curl -X POST https://auth.example.com/token \
     -d "grant_type=client_credentials" \
     -d "client_id=YOUR_CLIENT_ID" \
     -d "client_secret=YOUR_SECRET"
   ```

## Configuration Issues

### Common YAML Configuration Errors

#### Incorrect Nesting Structure

**Symptom:** Services start but connections fail with configuration errors

**Common Mistakes:**
```yaml
# WRONG - TLS nested under connection
gateway:
  connection:
    tls:
      verify: false

# CORRECT - TLS at gateway level
gateway:
  tls:
    verify: false
```

#### Absolute vs Relative Paths

**Symptom:** Certificate or file not found errors

**Fix:**
```yaml
# WRONG - Absolute paths break in containers
ca_file: /Users/username/project/certs/ca.crt

# CORRECT - Relative paths work everywhere
ca_file: "./certs/ca.crt"
```

### Environment Variable Issues

#### Missing Required Variables

**Symptoms:**
- Services fail to start
- Authentication errors
- Empty configuration values

**Debug:**
```bash
# Check environment variables
env | grep -E "(MCP_|JWT_|REDIS_)"

# Validate required variables
./scripts/install.sh --validate-environment
```

**Common Missing Variables:**
```bash
# Development environment
export JWT_SECRET_KEY="your-secret-here"
export MCP_AUTH_TOKEN="your-jwt-token"

# Production environment
export TLS_CERT_FILE="/path/to/cert.pem"
export TLS_KEY_FILE="/path/to/key.pem"
export REDIS_URL="redis://localhost:6379"
```

### Configuration Validation

#### Use Built-in Validation

```bash
# Validate configuration files
./scripts/install.sh --validate-config config/gateway.yaml

# Test configuration loading
./services/gateway/bin/mcp-gateway --config config/gateway.yaml --validate

# Check template generation
./scripts/install.sh --environment development --template --dry-run
```

#### Common Configuration Patterns

**Development Environment:**
```yaml
# Development configuration - NOT for production use
tls:
  enabled: false  # NEVER disable TLS in production
logging:
  level: debug    # Use 'info' or 'warn' in production
auth:
  provider: jwt
  jwt:
    issuer: mcp-gateway-development  # Use proper issuer in production
```

**Production Environment:**
```yaml
# Maximum security, minimal logging
tls:
  enabled: true
  min_version: "1.3"
  verify: true
  ca_file: "/path/to/ca.crt"
  cert_file: "/path/to/server.crt"
  key_file: "/path/to/server.key"
logging:
  level: info
  include_caller: false
auth:
  provider: oauth2  # Or mtls
```

### 🔒 TLS Security Requirements

**CRITICAL:** Always use proper TLS certificates. Never disable verification.

#### Certificate Generation
```bash
# Use the install script (recommended)
./scripts/install.sh --environment production

# Or generate manually with proper SANs
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 365 -key ca.key -out ca.crt \
  -subj "/C=US/ST=CA/L=SF/O=MCP/CN=MCP-CA"

# Server certificate with Subject Alternative Names
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr \
  -subj "/C=US/ST=CA/L=SF/O=MCP/CN=localhost"

# Create SAN extension file
cat > server.ext << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = *.localhost  
DNS.3 = gateway
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

# Sign with CA
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt -days 365 \
  -extensions v3_req -extfile server.ext
```

#### TLS Verification
```yaml
# ✅ CORRECT - Always verify certificates
tls:
  ca_file: "./certs/ca.crt"
  verify: true
  min_version: "1.2"

# ❌ WRONG - Never disable verification
tls:
  verify: false  # DON'T DO THIS
```

## Performance Problems

### High Latency

#### Symptoms
- Requests take > 5 seconds
- Timeouts on simple operations

#### Diagnose
```bash
# Measure latency
mcp-router benchmark --requests 100

# Check metrics
curl http://localhost:9091/metrics | grep request_duration

# Profile CPU usage
mcp-router --debug.enable-pprof --debug.pprof-port 6060 &
go tool pprof http://localhost:6060/debug/pprof/profile
```

#### Solutions

1. **Enable connection pooling**
   ```yaml
   gateway:
     connection:
       pool:
         enabled: true
         min_size: 5
         max_size: 20
   ```

2. **Switch to binary protocol**
   ```yaml
   advanced:
     protocol: tcp
   gateway:
     url: tcp://gateway.example.com:8444
   ```

3. **Enable compression**
   ```yaml
   advanced:
     compression:
       enabled: true
       algorithm: zstd
       level: 1
   ```

### Memory Leaks

#### Symptoms
- Router memory usage grows continuously
- System becomes unresponsive

#### Diagnose
```bash
# Monitor memory usage
while true; do
  ps aux | grep mcp-router | grep -v grep
  sleep 10
done

# Heap profile
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof heap.prof
```

#### Solutions

1. **Adjust cache sizes**
   ```yaml
   advanced:
     deduplication:
       cache_size: 100  # Reduce from default 1000
   ```

2. **Limit concurrent requests**
   ```yaml
   local:
     max_concurrent_requests: 50  # Reduce from 100
   ```

## Secure Storage Issues

### Platform-Specific Problems

#### macOS: "Keychain access denied"
```bash
# Reset keychain access
security unlock-keychain
security delete-generic-password -s com.poiley.mcp-router

# Re-add token
mcp-router token set --name gateway-token
```

#### Windows: "Credential Manager error"
```powershell
# Check credential manager service
sc query VaultSvc

# Restart if needed
sc stop VaultSvc
sc start VaultSvc

# Clear old credentials
cmdkey /delete:MCP-Router:gateway-token
```

#### Linux: "Secret service not available"
```bash
# Check D-Bus
echo $DBUS_SESSION_BUS_ADDRESS

# Start gnome-keyring
eval $(gnome-keyring-daemon --start --components=secrets)

# Fallback to file storage
mcp-router token set --name gateway-token --force-file
```

## Gateway Issues

### Redis Connection Failures

#### Symptoms
```
ERROR Failed to connect to Redis: dial tcp: i/o timeout
```

#### Solutions

1. **Check Redis status**
   ```bash
   kubectl get pods -n mcp-system -l app=redis
   kubectl exec -n mcp-system redis-0 -- redis-cli ping
   ```

2. **Verify Redis password**
   ```bash
   kubectl get secret -n mcp-system redis-secret -o yaml
   ```

3. **Test Redis connectivity**
   ```bash
   kubectl run redis-test --rm -it --image=redis:7 -- \
     redis-cli -h redis.mcp-system -a $REDIS_PASSWORD ping
   ```

### Rate Limiting Issues

#### Symptoms
```
ERROR Rate limit exceeded
```

#### Diagnose
```bash
# Check current limits
curl http://gateway:9090/metrics | grep rate_limit

# Monitor rate limit hits
watch 'kubectl logs -n mcp-system deployment/mcp-gateway | grep "rate limit"'
```

#### Solutions

1. **Increase limits**
   ```yaml
   rate_limit:
     requests_per_sec: 1000
     burst: 2000
   ```

2. **Per-user limits**
   ```yaml
   auth:
     jwt:
       claims:
         rate_limit: 500  # Per-user override
   ```

## Binary Protocol Issues

### "Invalid magic bytes" Error

#### Symptoms
```
ERROR Failed to read frame: invalid magic bytes: 0x00000000
```

#### Solutions

1. **Protocol mismatch**
   ```yaml
   # Ensure both sides use same protocol
   advanced:
     protocol: tcp  # Not websocket
   ```

2. **Corrupted connection**
   ```bash
   # Reset connection pool
   mcp-router pool reset
   ```

### Frame Size Exceeded

#### Symptoms
```
ERROR Payload too large: 15728640 bytes (max 10485760)
```

#### Solutions
```yaml
# Increase frame size (gateway)
server:
  tcp:
    max_frame_size: 20971520  # 20MB
```

## Debug Tools

### Enable Debug Logging
```bash
# Router debug mode
# For troubleshooting only:
mcp-router --log-level debug --debug.log-frames

# Gateway debug mode
kubectl set env deployment/mcp-gateway -n mcp-system LOG_LEVEL=debug
```

### Capture Network Traffic
```bash
# Capture WebSocket traffic
tcpdump -i any -w mcp.pcap 'port 443'

# Analyze with Wireshark
wireshark mcp.pcap
```

### Test Individual Components

#### Test Secure Storage
```bash
mcp-router token doctor
mcp-router token test --name gateway-token
```

#### Test Gateway Connection
```bash
mcp-router test --gateway-only
```

#### Test MCP Protocol
```bash
mcp-router test --method initialize
mcp-router test --method tools/list
```

### Generate Debug Bundle
```bash
# Collect all debug info
mcp-router debug bundle --output debug.tar.gz

# Contents:
# - Configuration (sanitized)
# - Logs (last 1000 lines)
# - Metrics snapshot
# - System info
# - Connection state
```

### Enhanced Developer Tools

#### Quick Setup and Reset
```bash
# One-click development setup
make quickstart

# Complete development validation
make dev-workflow

# Reset everything and start fresh
make dev-reset
make quickstart
```

#### Environment-Specific Testing
```bash
# Test development environment
./scripts/install.sh --environment development --validate

# Test production configuration
./scripts/install.sh --environment production --validate-only

# Generate configuration from templates
./scripts/install.sh --environment staging --template
```

#### Automated Diagnostics
```bash
# Comprehensive system check
make troubleshoot

# Service health validation
make verify-build

# Quick validation (for git hooks)
make dev-check
```

#### E2E Testing with Debugging
```bash
# Quick E2E test (5 minutes)
make test-e2e-quick

# Full E2E test with validation (15 minutes)
make test-e2e-validated

# Setup E2E environment for manual debugging
make setup-e2e
# ... debug manually ...
make cleanup-e2e
```

## Enhanced Monitoring

### Universal Protocol Metrics

#### Real-Time Protocol Monitoring
```bash
# Monitor all protocol health in real-time
watch 'curl -s https://mcp-gateway.your-domain.com/health/protocols | jq ".protocols[] | {name: .name, healthy: .healthy, latency_ms: .latency_ms}"'

# Protocol-specific performance monitoring
watch 'curl -s http://localhost:9090/metrics | grep -E "(protocol_detection_accuracy|cross_protocol_load_balance|direct_client_latency)"'

# Connection pool monitoring across protocols
watch 'curl -s http://localhost:9091/metrics | grep -E "(connection_pool_size|connection_pool_hits|connection_pool_misses)" | grep -E "(stdio|websocket|http|sse|tcp_binary)"'
```

#### Protocol Performance Dashboards
```bash
# Gateway universal protocol dashboard
curl -s http://localhost:9090/metrics | grep -E "mcp_gateway_protocol_" | \
  awk '{print $1 "	" $2}' | sort

# Router direct protocol dashboard
curl -s http://localhost:9091/metrics | grep -E "mcp_router_direct_" | \
  awk '{print $1 "	" $2}' | sort

# Cross-protocol load balancer status
curl https://mcp-gateway.your-domain.com/api/v1/load-balancer/dashboard | jq
```

### Predictive Health Monitoring

#### ML Model Performance Tracking
```bash
# Check anomaly detection model accuracy
curl https://mcp-gateway.your-domain.com/api/v1/health/ml-model/accuracy

# View anomaly detection training status
curl https://mcp-gateway.your-domain.com/api/v1/health/ml-model/training-status

# Compare predictive vs traditional health accuracy
curl https://mcp-gateway.your-domain.com/api/v1/health/comparison | jq '{
  predictive_accuracy: .predictive.accuracy,
  traditional_accuracy: .traditional.accuracy,
  improvement: .improvement_percentage
}'
```

#### Anomaly Alert Management
```bash
# View current anomalies by protocol
curl https://mcp-gateway.your-domain.com/api/v1/health/anomalies | \
  jq '.anomalies[] | {protocol: .protocol, severity: .severity, detected_at: .detected_at}'

# Set anomaly detection sensitivity
curl -X PUT https://mcp-gateway.your-domain.com/api/v1/health/ml-model/config \
  -H "Content-Type: application/json" \
  -d '{"sensitivity": 0.85, "training_window": "14d"}'

# Clear false positive anomalies
curl -X POST https://mcp-gateway.your-domain.com/api/v1/health/anomalies/clear \
  -d '{"protocol": "stdio", "severity": "warning"}'
```

### Protocol Detection Analytics

#### Detection Accuracy Monitoring
```bash
# Protocol detection accuracy by protocol type
curl -s http://localhost:9090/metrics | \
  grep protocol_detection_accuracy | \
  awk -F'[{}]' '{print $2}' | \
  sed 's/protocol="//' | sed 's/"//' | \
  while read line; do
    protocol=$(echo "$line" | cut -d',' -f1)
    accuracy=$(curl -s http://localhost:9090/metrics | grep "protocol_detection_accuracy{protocol=\"$protocol\"}" | awk '{print $2}')
    echo "$protocol: $accuracy"
  done

# Detection cache performance
curl -s http://localhost:9091/api/v1/protocols/cache/stats | jq '{
  hit_rate: .hit_rate,
  miss_rate: .miss_rate,
  cache_size: .current_size,
  max_size: .max_size
}'

# Failed detection analysis
curl http://localhost:9091/api/v1/protocols/detect/failures | \
  jq '.failures[] | {endpoint: .endpoint, attempted_protocols: .attempted_protocols, error: .error}'
```

#### Detection Performance Optimization
```bash
# Optimize detection timeout based on metrics
AVERAGE_DETECTION_TIME=$(curl -s http://localhost:9090/metrics | \
  grep protocol_detection_duration_seconds | \
  grep quantile | grep '0.95' | \
  awk '{print $2}')

echo "Consider setting detection timeout to: ${AVERAGE_DETECTION_TIME}s"

# Cache optimization recommendations
curl http://localhost:9091/api/v1/protocols/cache/optimization-recommendations
```

### Cross-Protocol Load Balancer Analytics

#### Load Distribution Analysis
```bash
# Protocol request distribution
curl -s http://localhost:9090/metrics | \
  grep mcp_gateway_load_balancer_requests_total | \
  grep selected_protocol | \
  awk -F'selected_protocol="' '{print $2}' | \
  awk -F'"' '{print $1}' | \
  sort | uniq -c | \
  awk '{print $2 ": " $1 " requests"}'

# Protocol latency comparison
curl -s http://localhost:9090/metrics | \
  grep mcp_gateway_load_balancer_latency_seconds | \
  grep backend_protocol | \
  awk -F'backend_protocol="' '{print $2}' | \
  awk -F'"' '{printf "%s: %s
", $1, $3}' | \
  sort -k2 -n
```

#### Load Balancer Health Assessment
```bash
# Check for load imbalance
PROTOCOL_DISTRIBUTION=$(curl -s http://localhost:9090/metrics | \
  grep mcp_gateway_load_balancer_requests_total | \
  awk '{sum+=$2} END {print sum}')

echo "Total requests: $PROTOCOL_DISTRIBUTION"

# Protocol weight effectiveness
curl https://mcp-gateway.your-domain.com/api/v1/load-balancer/effectiveness | jq '{
  weight_adherence: .weight_adherence_percentage,
  performance_improvement: .performance_improvement_percentage,
  recommendation: .recommendation
}'
```

### Memory Optimization Monitoring

#### Router Memory Performance
```bash
# Memory optimization savings
curl -s http://localhost:9091/metrics | \
  grep mcp_router_memory_optimization_savings_bytes | \
  awk '{printf "Memory saved: %.2f MB
", $2/1024/1024}'

# Garbage collection optimization
curl -s http://localhost:9091/metrics | \
  grep mcp_router_gc_optimization_cycles_total | \
  awk '{print "GC optimization cycles: " $2}'

# Object pool efficiency
curl http://localhost:9091/api/v1/performance/memory-optimization | jq '{
  object_pool_hit_rate: .object_pool.hit_rate,
  memory_reduction_percentage: .memory_reduction_percentage,
  gc_pause_reduction: .gc_pause_reduction_ms
}'
```

#### Gateway Memory Analysis
```bash
# Protocol-specific memory usage
curl -s http://localhost:9090/metrics | \
  grep mcp_gateway_memory_usage_bytes | \
  grep protocol | \
  awk -F'protocol="' '{print $2}' | \
  awk -F'"' '{printf "%s: %.2f MB
", $1, $3/1024/1024}'

# Connection pool memory efficiency
curl -s http://localhost:9090/metrics | \
  grep mcp_gateway_connection_pool_memory_bytes | \
  awk '{printf "Connection pool memory: %.2f MB
", $2/1024/1024}'
```

### Service Discovery Monitoring

#### Discovery Health by Provider
```bash
# Kubernetes service discovery health
curl https://mcp-gateway.your-domain.com/health/discovery | \
  jq '.providers.kubernetes | {healthy: .healthy, discovered_services: .discovered_services, last_update: .last_update}'

# Static configuration health
curl https://mcp-gateway.your-domain.com/health/discovery | \
  jq '.providers.static | {healthy: .healthy, configured_endpoints: .configured_endpoints}'

# Consul discovery health (if enabled)
curl https://mcp-gateway.your-domain.com/health/discovery | \
  jq '.providers.consul | {healthy: .healthy, consul_services: .consul_services}'
```

#### Discovery Performance Metrics
```bash
# Service discovery refresh performance
curl -s http://localhost:9090/metrics | \
  grep mcp_gateway_service_discovery_refresh_duration_seconds | \
  awk '{print "Discovery refresh time: " $2 "s"}'

# Protocol detection during discovery
curl -s http://localhost:9090/metrics | \
  grep mcp_gateway_service_discovery_protocol_detection_success_total | \
  awk '{print "Successful protocol detections during discovery: " $2}'
```

### Alerting and Notifications

#### Critical Protocol Alerts
```bash
# Set up protocol-specific alerting
curl -X POST https://mcp-gateway.your-domain.com/api/v1/alerts/configure \
  -H "Content-Type: application/json" \
  -d '{
    "protocol_unavailable": {
      "threshold": 0,
      "duration": "2m",
      "severity": "critical"
    },
    "detection_accuracy_low": {
      "threshold": 0.95,
      "duration": "10m", 
      "severity": "warning"
    },
    "cross_protocol_imbalance": {
      "threshold": 0.3,
      "duration": "5m",
      "severity": "warning"
    }
  }'

# Check current alert status
curl https://mcp-gateway.your-domain.com/api/v1/alerts/status | \
  jq '.active_alerts[] | {alert: .name, protocol: .protocol, severity: .severity, duration: .duration}'
```

#### Performance Threshold Monitoring
```bash
# Monitor performance thresholds
curl https://mcp-gateway.your-domain.com/api/v1/performance/thresholds | jq '{
  latency_sla_breaches: .latency_sla_breaches,
  availability_sla_status: .availability_sla_status,
  performance_degradation_alerts: .performance_degradation_alerts
}'

# Set custom performance thresholds
curl -X PUT https://mcp-gateway.your-domain.com/api/v1/performance/thresholds \
  -H "Content-Type: application/json" \
  -d '{
    "max_latency_ms": 100,
    "min_availability_percentage": 99.9,
    "max_error_rate_percentage": 1.0
  }'
```

## Common Error Messages

### Universal Protocol Error Messages

#### "protocol_detection_failed"
**Cause**: Auto-detection unable to identify protocol
**Fix**: Specify protocol manually or increase timeout
```yaml
direct_clients:
  auto_detection:
    timeout: "3s"  # Increase from default 1s
```
```bash
# Manual protocol override
curl -H "X-Force-Protocol: stdio" https://gateway.example.com/api/v1/execute
```

#### "cross_protocol_load_balance_failed"
**Cause**: No healthy backends across any protocol
**Fix**: Check protocol-specific backend health
```bash
# Check each protocol health
curl https://gateway.example.com/health/stdio
curl https://gateway.example.com/health/websocket
curl https://gateway.example.com/health/http
```

#### "stdio: no such file or directory"
**Cause**: stdio socket not found
**Fix**: Verify socket path and permissions
```bash
ls -la /tmp/mcp-servers/
chmod 666 /tmp/mcp-servers/*.sock
```

#### "websocket: bad handshake"
**Cause**: WebSocket protocol negotiation failed
**Fix**: Check WebSocket server compatibility
```bash
wscat -c ws://localhost:8080
curl -I http://localhost:8080/health
```

#### "http: 404 not found"
**Cause**: HTTP endpoint not available
**Fix**: Verify HTTP API path configuration
```yaml
backends:
  http:
    api_prefix: "/mcp"  # Ensure correct path
```

#### "sse: stream closed"
**Cause**: Server-Sent Events connection dropped
**Fix**: Enable reconnection
```yaml
backends:
  sse:
    reconnect_interval: "5s"
    max_retries: 3
```

#### "tcp_binary: protocol mismatch"
**Cause**: Binary protocol version incompatibility
**Fix**: Verify protocol versions match
```bash
# Check protocol versions
curl http://localhost:9091/api/v1/protocols | jq '.tcp_binary.version'
```

#### "predictive_health_anomaly_detected"
**Cause**: ML model detected performance anomaly
**Fix**: Investigate specific protocol performance
```bash
# Check anomaly details
curl https://gateway.example.com/api/v1/health/anomalies
# Clear false positives
curl -X POST https://gateway.example.com/api/v1/health/anomalies/clear
```

### Traditional Error Messages

#### "Context deadline exceeded"
**Cause**: Request timeout
**Fix**: Increase timeout
```yaml
local:
  request_timeout_ms: 60000  # 60 seconds
```

#### "Connection pool exhausted"
**Cause**: All connections in use
**Fix**: Increase pool size
```yaml
connection:
  pool:
    max_size: 50
```

#### "Circuit breaker open"
**Cause**: Too many failures
**Fix**: Check backend health
```bash
kubectl get pods -n mcp-servers
kubectl logs -n mcp-servers -l app=mcp-server
```

#### "Invalid session"
**Cause**: Session expired
**Fix**: Reconnect
```bash
mcp-router reconnect
```

### Performance-Related Errors

#### "memory_optimization_failed"
**Cause**: Object pooling or GC optimization issues
**Fix**: Reset memory optimization
```bash
curl -X POST http://localhost:9091/api/v1/performance/memory-optimization/reset
```

#### "connection_pool_efficiency_low"
**Cause**: Poor connection reuse
**Fix**: Optimize pool configuration
```yaml
direct_clients:
  connection_pool:
    enable_connection_reuse: true
    health_check_interval: "30s"
```

#### "protocol_conversion_overhead_high"
**Cause**: Expensive protocol conversions
**Fix**: Use direct connections or optimize conversion
```bash
# Check conversion metrics
curl -s http://localhost:9090/metrics | grep protocol_conversion_duration
# Enable direct mode
mcp-router --direct-mode stdio
```

## Getting Help

### Collect Diagnostics
```bash
# Generate report
mcp-router diagnose --verbose > diagnosis.txt

# Include:
# - Router version
# - OS and platform
# - Configuration (sanitized)
# - Recent errors
# - Connection state
```

### Useful Log Queries

#### Gateway logs
```bash
# Auth failures
kubectl logs -n mcp-system deployment/mcp-gateway | jq 'select(.msg | contains("auth"))'

# Request errors
kubectl logs -n mcp-system deployment/mcp-gateway | jq 'select(.level=="error")'

# Specific user
kubectl logs -n mcp-system deployment/mcp-gateway | jq 'select(.user=="user@example.com")'
```

#### Router logs
```bash
# Connection events
mcp-router logs --filter connection

# Error summary
mcp-router logs --level error --summary
```

### Community Support

1. Check documentation: https://github.com/poiley/mcp-bridge/tree/main/docs
2. Search issues: https://github.com/poiley/mcp-bridge/issues
3. Ask in discussions: https://github.com/poiley/mcp-bridge/discussions