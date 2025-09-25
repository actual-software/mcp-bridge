# API Reference

This document describes the APIs and interfaces used by MCP Router.

## Configuration API

### Configuration Structure

The router is configured via YAML, environment variables, or command-line flags.

#### Complete Configuration Schema

```yaml
# Configuration version
version: 1

# Gateway connection settings
gateway:
  # Gateway URL (required)
  # Supported schemes: ws://, wss://, tcp://, tcps://, tcp+tls://
  url: string
  
  # Authentication configuration
  auth:
    # Authentication type: bearer, oauth2, mtls
    type: string
    
    # Bearer token settings
    token: string         # Direct token (not recommended)
    token_env: string     # Environment variable name
    token_file: string    # Path to token file
    token_secure_key: string # Key for secure token storage
    
    # OAuth2 settings
    client_id: string
    client_secret: string      # Direct secret (not recommended)
    client_secret_env: string  # Environment variable name
    client_secret_secure_key: string # Key for secure storage
    token_endpoint: string     # OAuth2 token URL
    grant_type: string         # client_credentials or password
    username: string           # For password grant
    password: string           # For password grant
    password_env: string       # Environment variable name
    password_secure_key: string # Key for secure storage
    scopes: [string]          # OAuth2 scopes
    scope: string             # Alternative to scopes
    
    # mTLS settings
    client_cert: string   # Path to client certificate
    client_key: string    # Path to client key
    
    # Per-message authentication
    per_message_auth: boolean  # Enable per-message authentication (default: false)
    per_message_cache: integer # Cache duration for per-message auth tokens in seconds
  
  # Connection settings
  connection:
    timeout_ms: integer           # Connection timeout (default: 5000)
    keepalive_interval_ms: integer # Keepalive interval (default: 30000)
    
    # Reconnection settings
    reconnect:
      initial_delay_ms: integer   # Initial retry delay (default: 1000)
      max_delay_ms: integer       # Maximum retry delay (default: 60000)
      multiplier: float           # Backoff multiplier (default: 2.0)
      max_attempts: integer       # Max retry attempts (default: -1 unlimited)
      jitter: float              # Jitter factor 0-1 (default: 0.1)
    
    # Connection pooling settings
    pool:
      enabled: boolean            # Enable connection pooling (default: true)
      min_size: integer          # Minimum pool size (default: 2)
      max_size: integer          # Maximum pool size (default: 10)
      max_idle_time_ms: integer  # Max idle before cleanup (default: 300000)
      max_lifetime_ms: integer   # Max connection lifetime (default: 1800000)
      acquire_timeout_ms: integer # Timeout to acquire connection (default: 5000)
      health_check_interval_ms: integer # Health check interval (default: 30000)
  
  # TLS settings
  tls:
    verify: boolean              # Verify certificates (default: true)
    ca_cert_path: string         # Custom CA certificate
    min_version: string          # Minimum TLS version (default: "1.3")
    cipher_suites: [string]      # Allowed cipher suites
    server_name: string          # SNI server name

# Local router settings
local:
  read_buffer_size: integer      # Stdin buffer size (default: 65536)
  write_buffer_size: integer     # Stdout buffer size (default: 65536)
  request_timeout_ms: integer    # Request timeout (default: 30000)
  max_concurrent_requests: integer # Max concurrent (default: 100)
  
  # Rate limiting
  rate_limit:
    requests_per_second: float   # Rate limit (0 = disabled)
    burst: integer               # Burst capacity

# Logging configuration
logging:
  level: string                  # debug, info, warn, error (default: info)
  format: string                 # json or text (default: json)
  output: string                 # stdout, stderr, or file path (default: stderr)
  include_caller: boolean        # Include caller info (default: false)
  
  # Log sampling
  sampling:
    enabled: boolean             # Enable sampling (default: false)
    initial: integer             # Initial samples (default: 100)
    thereafter: integer          # Sample rate after initial (default: 100)

# Metrics configuration
metrics:
  enabled: boolean               # Enable metrics (default: true)
  endpoint: string               # Metrics endpoint (default: localhost:9091)
  labels: {string: string}       # Additional labels

# Advanced settings
advanced:
  # Compression settings
  compression:
    enabled: boolean             # Enable compression (default: true)
    algorithm: string            # gzip or zstd (default: gzip)
    level: integer              # Compression level 1-9 (default: 6)
  
  # Circuit breaker
  circuit_breaker:
    failure_threshold: integer   # Failures to open (default: 5)
    success_threshold: integer   # Successes to close (default: 2)
    timeout_seconds: integer     # Timeout duration (default: 30)
  
  # Request deduplication
  deduplication:
    enabled: boolean            # Enable dedup (default: true)
    cache_size: integer         # Cache size (default: 1000)
    ttl_seconds: integer        # TTL in seconds (default: 60)
  
  # Debug settings
  debug:
    log_frames: boolean         # Log wire frames (default: false)
    save_failures: boolean      # Save failed requests (default: false)
    failure_dir: string         # Directory for failures (default: /tmp/mcp-failures)
    enable_pprof: boolean       # Enable pprof endpoint (default: false)
    pprof_port: integer         # Pprof port (default: 6060)
```

### Environment Variables

All configuration options can be set via environment variables:

| Environment Variable | Config Path | Example |
|---------------------|-------------|---------|
| `MCP_GATEWAY_URL` | `gateway.url` | `wss://gateway.example.com` |
| `MCP_GATEWAY_AUTH_TYPE` | `gateway.auth.type` | `bearer` |
| `MCP_GATEWAY_AUTH_TOKEN` | `gateway.auth.token` | `secret-token` |
| `MCP_GATEWAY_CONNECTION_TIMEOUT_MS` | `gateway.connection.timeout_ms` | `10000` |
| `MCP_LOCAL_RATE_LIMIT_REQUESTS_PER_SECOND` | `local.rate_limit.requests_per_second` | `50.0` |
| `MCP_LOGGING_LEVEL` | `logging.level` | `debug` |
| `MCP_METRICS_ENABLED` | `metrics.enabled` | `true` |

### Command-Line Flags

```bash
mcp-router [flags]

Flags:
  --config string        Config file path
  --log-level string     Log level (debug, info, warn, error)
  --log-format string    Log format (json, text)
  --metrics-addr string  Metrics endpoint address
  --version             Show version information
  --help                Show help
```

## Metrics API

### Prometheus Metrics Endpoint

**Endpoint**: `GET /metrics`  
**Default Address**: `http://localhost:9091/metrics`

### Available Metrics

#### Counter Metrics

```prometheus
# Total number of requests sent to gateway
mcp_router_requests_total{method="initialize"} 145

# Total number of responses received
mcp_router_responses_total{status="success"} 142
mcp_router_responses_total{status="error"} 3

# Total number of errors
mcp_router_errors_total{type="timeout"} 2
mcp_router_errors_total{type="auth"} 1

# Connection retry attempts
mcp_router_connection_retries_total 5

# Rate limit violations
mcp_router_rate_limit_exceeded_total 10

# Connection pool metrics
mcp_router_pool_connections_created_total 45
mcp_router_pool_connections_closed_total 10
mcp_router_pool_connections_failed_total 2
mcp_router_pool_wait_count_total 150
```

#### Gauge Metrics

```prometheus
# Currently active connections
mcp_router_active_connections 1

# Router uptime (always 1 when running)
mcp_router_up 1

# Connection pool gauges
mcp_router_pool_total_connections 8
mcp_router_pool_active_connections 3
mcp_router_pool_idle_connections 5
```

#### Histogram Metrics

```prometheus
# Request duration in seconds
mcp_router_request_duration_seconds_bucket{method="tools/list",le="0.005"} 10
mcp_router_request_duration_seconds_bucket{method="tools/list",le="0.01"} 25
mcp_router_request_duration_seconds_bucket{method="tools/list",le="0.025"} 40
mcp_router_request_duration_seconds_sum{method="tools/list"} 2.5
mcp_router_request_duration_seconds_count{method="tools/list"} 45

# Response size in bytes
mcp_router_response_size_bytes_bucket{le="100"} 5
mcp_router_response_size_bytes_bucket{le="1000"} 30
mcp_router_response_size_bytes_bucket{le="10000"} 42
mcp_router_response_size_bytes_sum 125000
mcp_router_response_size_bytes_count 45
```

### Health Check Endpoint

**Endpoint**: `GET /health`  
**Default Address**: `http://localhost:9091/health`

**Response**:
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:45.123Z",
  "version": "2.0.0",
  "uptime_seconds": 3600,
  "gateway": {
    "connected": true,
    "url": "wss://gateway.example.com",
    "last_ping": "2024-01-15T10:30:40.123Z"
  }
}
```

## Internal APIs

### Gateway Client Interface

The router uses a common interface for different protocols:

```go
type GatewayClient interface {
    // Connect establishes connection to gateway
    Connect(ctx context.Context) error
    
    // SendRequest sends an MCP request
    SendRequest(req *mcp.Request) error
    
    // ReceiveResponse receives next MCP response
    ReceiveResponse() (*mcp.Response, error)
    
    // SendPing sends keepalive ping
    SendPing() error
    
    // Close closes the connection
    Close() error
    
    // IsConnected returns connection status
    IsConnected() bool
}
```

### Rate Limiter Interface

```go
type RateLimiter interface {
    // Allow checks if request is allowed
    Allow(ctx context.Context) error
    
    // Wait blocks until request is allowed
    Wait(ctx context.Context) error
}
```

### Stdio Handler Interface

```go
type StdioHandler interface {
    // Start begins handling stdio
    Start(ctx context.Context, input chan<- []byte, output <-chan []byte) error
}
```

### Connection Pool Interface

```go
type Connection interface {
    // IsAlive checks if the connection is still alive
    IsAlive() bool
    // Close closes the connection
    Close() error
    // GetID returns the connection ID
    GetID() string
}

type Factory interface {
    // Create creates a new connection
    Create(ctx context.Context) (Connection, error)
    // Validate validates an existing connection
    Validate(conn Connection) error
}

type Pool interface {
    // Acquire gets a connection from the pool
    Acquire(ctx context.Context) (Connection, error)
    // Release returns a connection to the pool
    Release(conn Connection) error
    // Close closes the pool and all connections
    Close() error
    // Stats returns pool statistics
    Stats() Stats
}
```

## MCP Protocol Types

### Request Structure

```go
type Request struct {
    JSONRPC string      `json:"jsonrpc"`
    Method  string      `json:"method"`
    Params  interface{} `json:"params,omitempty"`
    ID      interface{} `json:"id"`
}
```

### Response Structure

```go
type Response struct {
    JSONRPC string      `json:"jsonrpc"`
    Result  interface{} `json:"result,omitempty"`
    Error   *Error      `json:"error,omitempty"`
    ID      interface{} `json:"id"`
}
```

### Error Structure

```go
type Error struct {
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
}
```

### Standard Error Codes

| Code | Constant | Description |
|------|----------|-------------|
| -32700 | ParseError | Invalid JSON |
| -32600 | InvalidRequest | Invalid request |
| -32601 | MethodNotFound | Method not found |
| -32602 | InvalidParams | Invalid parameters |
| -32603 | InternalError | Internal error |

## Usage Examples

### Basic Configuration

```yaml
# Minimal configuration
gateway:
  url: "wss://gateway.example.com"
  auth:
    type: bearer
    token_env: MCP_AUTH_TOKEN
```

### Advanced Configuration

```yaml
# Production configuration with all features
version: 1

gateway:
  url: "tcps://gateway.example.com:9443"
  auth:
    type: oauth2
    client_id: "mcp-router-prod"
    client_secret_env: OAUTH_SECRET
    token_endpoint: "https://auth.example.com/oauth2/token"
    grant_type: "client_credentials"
    scopes: ["mcp.read", "mcp.write"]
  connection:
    timeout_ms: 10000
    keepalive_interval_ms: 30000
    reconnect:
      initial_delay_ms: 1000
      max_delay_ms: 60000
      multiplier: 2.0
      max_attempts: 50
    pool:
      enabled: true
      min_size: 5
      max_size: 20
      max_idle_time_ms: 600000
      max_lifetime_ms: 3600000
  tls:
    verify: true
    ca_cert_path: "/etc/ssl/company-ca.crt"
    min_version: "1.3"

local:
  request_timeout_ms: 60000
  max_concurrent_requests: 200
  rate_limit:
    requests_per_second: 100.0
    burst: 200

logging:
  level: info
  format: json
  output: stderr

metrics:
  enabled: true
  endpoint: ":9091"
  labels:
    environment: "production"
    region: "us-east-1"

advanced:
  circuit_breaker:
    failure_threshold: 10
    success_threshold: 5
    timeout_seconds: 60
  compression:
    enabled: true
    algorithm: "zstd"
    level: 3
```

### Programmatic Health Check

```bash
#!/bin/bash
# Health check script

HEALTH_URL="http://localhost:9091/health"
RESPONSE=$(curl -s -w "\n%{http_code}" $HEALTH_URL)
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)

if [ "$HTTP_CODE" = "200" ]; then
    STATUS=$(echo "$BODY" | jq -r '.status')
    if [ "$STATUS" = "healthy" ]; then
        echo "Router is healthy"
        exit 0
    fi
fi

echo "Router is unhealthy"
exit 1
```

### Metrics Collection

```python
# Prometheus query examples

# Request rate
rate(mcp_router_requests_total[5m])

# Error rate
rate(mcp_router_errors_total[5m])

# 95th percentile latency
histogram_quantile(0.95, 
  rate(mcp_router_request_duration_seconds_bucket[5m])
)

# Connection failures
increase(mcp_router_connection_retries_total[1h])
```