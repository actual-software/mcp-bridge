# MCP Gateway

MCP Gateway provides universal protocol support for the Model Context Protocol, enabling any MCP client to connect to any MCP server regardless of protocol through a high-performance, scalable gateway architecture.

## Features

### Universal Protocol Support
- **Frontend Protocols**: stdio (WebSocket and TCP Binary connections handled through HTTP server layer)
- **Backend Protocols**: stdio, WebSocket, SSE - all supported
- **Protocol Routing**: Intelligent routing across mixed protocol backends
- **Service Discovery**: Automatic backend discovery with health checking

### Core Functionality
- **Multi-Protocol Interface**: WebSocket (8443), TCP Binary (8444), and stdio support
- **Service Discovery**: Kubernetes, Consul, and static configuration support
- **Load Balancing**: Protocol-aware strategies (round-robin, least-connections, weighted)
- **Circuit Breakers**: Failure detection and recovery with graceful degradation
  - Redis circuit breaker with configurable thresholds
  - Graceful degradation to in-memory operations
  - Half-open state for testing recovery
  - Per-endpoint circuit breaker monitoring
- **Session Management**: Redis-backed session storage
- **High Availability**: Horizontal scaling with shared state

### Security Features
- **Authentication**: JWT/OAuth2/mTLS with per-message auth support
- **Input Validation**: Comprehensive validation of all user inputs
  - Request IDs, methods, namespaces protected against injection
  - UTF-8 enforcement with configurable length limits
- **Rate Limiting**: Multi-layer protection with Redis circuit breaker
  - Per-IP connection limits (configurable)
  - Per-user request rate limiting (Redis or in-memory)
  - Sliding window algorithm with burst control
  - Automatic fallback to in-memory when Redis fails
  - Circuit breaker protection for Redis operations
- **Security Headers**: Automatic security headers on all HTTP responses
  - HSTS, CSP, X-Frame-Options, X-Content-Type-Options
- **Request Size Limits**: Configurable limits to prevent DoS
  - HTTP: 1MB body/headers
  - WebSocket: 10MB messages
  - TCP: 10MB payloads
- **TLS Security**: TLS 1.3 with configurable cipher suites
- **security.txt**: RFC 9116 compliant security disclosure endpoint

### Observability
- **Prometheus Metrics**: Comprehensive metrics for monitoring
- **Structured Logging**: JSON logs with trace correlation
- **Health Checks**: HTTP and TCP health check endpoints
- **Distributed Tracing**: OpenTelemetry support

## Architecture

### Universal Protocol Gateway
```
┌─────────────┐   Any Protocol    ┌─────────────────┐   Any Protocol   ┌─────────────┐
│ MCP Client  ├──────────────────►│   MCP Gateway   ├─────────────────►│ MCP Server  │
└─────────────┘                   └─────────┬───────┘                  └─────────────┘
• stdio                                     │                          • stdio
• WebSocket                                 ▼                          • WebSocket  
• HTTP                              ┌─────────────┐                    • HTTP
• SSE                               │    Redis    │                    • SSE
• TCP Binary                        └─────────────┘                    • TCP Binary
```

### Backend Protocol Support
| Backend Protocol | Status | Notes |
|:-----------------|:------:|:------|
| **stdio**        |   ✅   | Subprocess-based MCP servers |
| **WebSocket**    |   ✅   | WebSocket MCP servers |
| **SSE**          |   ✅   | Server-Sent Events MCP servers |

**Client Connection Methods:**
- WebSocket connections (port 8443)
- TCP Binary protocol (port 8444)
- stdio frontend for subprocess integration

## Deployment

### Prerequisites

- Kubernetes 1.24+
- kubectl configured with cluster access

### Quick Deploy

Deploy everything with one command:

```bash
kubectl apply -f https://raw.githubusercontent.com/poiley/mcp-bridge/main/services/gateway/deploy/install.yaml
```

This includes:
- Namespace and RBAC
- Redis for session storage
- Gateway deployment with 3 replicas
- Services and HPA
- Basic configuration

### Post-Deployment Setup

1. **Generate auth tokens:**
   ```bash
   kubectl exec -n mcp-system deployment/mcp-gateway -- mcp-gateway admin token create
   ```

2. **Check status:**
   ```bash
   kubectl exec -n mcp-system deployment/mcp-gateway -- mcp-gateway admin status
   ```

3. **Get gateway URL:**
   ```bash
   kubectl get service mcp-gateway-external -n mcp-system
   ```

### Manual Deployment

For more control, clone the repo and customize:

```bash
# Clone repository
git clone https://github.com/poiley/mcp-bridge
cd mcp-bridge/services/gateway

# Edit configuration
vim deploy/install.yaml

# Deploy
kubectl apply -f deploy/install.yaml
```

## Configuration

### Universal Protocol Configuration

The gateway supports simultaneous multi-protocol operation with Phase 3 universal backend support:

```yaml
version: 1
server:
  host: 0.0.0.0
  port: 8443                   # Primary WebSocket port
  metrics_port: 9090
  max_connections: 50000
  connection_buffer_size: 65536
  protocol: both               # Options: websocket, tcp, both, universal
  tcp_port: 8444              # TCP Binary protocol port 
  tcp_health_port: 9002       # Dedicated TCP health check port (optional)
  
  # stdio Frontend Support
  stdio_frontend:
    enabled: true
    modes:
      - type: "unix_socket"
        path: "/tmp/mcp-gateway.sock"
        enabled: true
      - type: "stdin_stdout"
        enabled: true
  
  # WebSocket Origin Validation
  # Configure allowed origins for CORS/CSRF protection
  allowed_origins:
    - https://app.example.com
    - https://dashboard.example.com
    - http://localhost:3000  # Development
    # - "*"                  # WARNING: Wildcard allows any origin - insecure!
  
  # TLS Configuration
  tls:
    enabled: true
    cert_file: /etc/mcp-gateway/tls/tls.crt
    key_file: /etc/mcp-gateway/tls/tls.key
    ca_file: /etc/mcp-gateway/tls/ca.crt  # For mTLS client verification
    min_version: "1.3"      # Recommended. Use "1.2" only for legacy compatibility
    client_auth: none       # Options: "none", "request", "require"

auth:
  provider: jwt
  jwt:
    issuer: "mcp-gateway"
    audience: "mcp-gateway"
    secret_key_env: JWT_SECRET_KEY
  
  # Per-message authentication (optional)
  # Enhanced security: clients include auth tokens with each message
  per_message_auth: false      # Enable per-message authentication
  per_message_auth_cache: 300  # Cache validated tokens for 5 minutes

routing:
  strategy: least_connections
  health_check_interval: 30s
  circuit_breaker:
    failure_threshold: 5
    success_threshold: 2
    timeout: 30s

# Universal Backend Support - ALL protocols supported
backends:
  - name: "local-stdio-server"
    protocol: "stdio"
    config:
      command: ["python", "examples/weather_server.py"]
      working_dir: "./examples"
      health_check:
        enabled: true
        interval: 30s
        
  - name: "websocket-server"
    protocol: "websocket"
    config:
      endpoints: ["ws://localhost:8080"]
      connection_pool:
        max_size: 10
        
  - name: "http-server"
    protocol: "http"
    config:
      base_url: "http://localhost:8081"
      
  - name: "sse-server"
    protocol: "sse"
    config:
      base_url: "http://localhost:8082"
      stream_endpoint: "/events"
      
  - name: "tcp-binary-server"
    protocol: "tcp_binary"
    config:
      endpoints: ["tcp://localhost:8083"]

# Universal Service Discovery
service_discovery:
  providers:
    - type: "kubernetes"
      config:
        namespace_selector:
          - mcp-servers
        label_selector:
          mcp.bridge/enabled: "true"
        refresh_rate: 30s
        auto_detect_protocols: true
    - type: "consul"
      config:
        address: "consul:8500"
        service_prefix: "mcp-"
        auto_detect_protocols: true
    - type: "static"
      config:
        auto_detect_protocols: true

rate_limit:
  enabled: true
  provider: memory  # Options: memory, redis
  window_size: 60   # Rate limit window in seconds

circuit_breaker:
  enabled: true
  threshold: 5       # Failure threshold before opening circuit
  timeout: 30        # Timeout in seconds before allowing retry

redis:
  url: "redis://redis:6379/0"

# Distributed tracing (optional)
tracing:
  enabled: false
  service_name: mcp-gateway
  sampler_type: const
  sampler_param: 1.0
  agent_host: jaeger-agent
  agent_port: 6831

# Logging configuration
logging:
  level: info      # Options: debug, info, warn, error
  format: json     # Options: json, text
```

### Environment Variables

**Core Configuration:**
- `MCP_JWT_SECRET_KEY`: Secret key for JWT validation (also accepts `JWT_SECRET_KEY` for compatibility)
- `REDIS_URL`: Redis connection URL
- `REDIS_PASSWORD`: Redis password (optional)

**Protocol Support:**
- `SUPPORTED_PROTOCOLS`: Comma-separated list of enabled protocols (stdio,websocket,http,sse,tcp_binary)
- `AUTO_DETECT_PROTOCOLS`: Enable automatic protocol detection (true/false)
- `STDIO_SOCKET_PATH`: Path for Unix socket stdio frontend (/tmp/mcp-gateway.sock)
- `CROSS_PROTOCOL_LOAD_BALANCING`: Enable cross-protocol load balancing (true/false)

**Service Discovery:**
- `SERVICE_DISCOVERY_ENABLED`: Enable universal service discovery (true/false)
- `CONSUL_ADDRESS`: Consul server address for service discovery
- `KUBERNETES_NAMESPACE`: Kubernetes namespace for service discovery

## Service Discovery

MCP servers are discovered automatically using Kubernetes service annotations.

### Configuration
The service discovery supports both `provider` and `mode` fields for backward compatibility:
- `provider`: Primary field for specifying discovery type
- `mode`: Alias for `provider` (backward compatibility)
- `namespace_selector`: List of namespace patterns to monitor
- `refresh_rate`: How often to refresh service information

### Recommended Namespaces
When deploying MCP Gateway in Kubernetes, use these standard namespace conventions:

- **Production**: `mcp-system` (recommended for production deployments)
- **Staging**: `mcp-staging` (for staging environments)
- **Development**: `mcp-dev` (for development environments)

All examples in this documentation use `mcp-system` as the default namespace.

### Service Annotations
MCP servers are discovered using Kubernetes service annotations:

```yaml
metadata:
  annotations:
    mcp.bridge/enabled: "true"
    mcp.bridge/namespace: "k8s"
    mcp.bridge/tools: |
      ["getPods", "execPod", "getLogs"]
```

## Load Balancing Strategies

### Cross-Protocol Load Balancing
The gateway provides intelligent load balancing across mixed protocol backends:

**Cross-Protocol Strategies:**
- **Protocol-Aware Round Robin**: Distributes requests evenly across all protocols with protocol-specific weighting
- **Cross-Protocol Least Connections**: Routes to the least loaded endpoint regardless of protocol
- **Protocol Preference**: Attempts preferred protocol first, then falls back across protocols
- **Latency-Based**: Routes based on measured protocol-specific latency characteristics

### Traditional Strategies
- **Round Robin**: Distributes requests evenly across all healthy endpoints
- **Least Connections**: Routes to the endpoint with the fewest active connections  
- **Weighted**: Distributes based on endpoint weights specified in annotations
- **Geographic**: Routes based on endpoint geographic proximity (when available)

## Rate Limiting and Circuit Breakers

### Rate Limiting
The gateway provides multi-layer rate limiting protection:

- **Sliding Window Algorithm**: Uses a sliding window counter for accurate rate limiting
- **Burst Control**: Configurable burst limits for handling traffic spikes
- **Multiple Backends**: Supports both Redis and in-memory rate limiting
- **Automatic Fallback**: When Redis fails, automatically falls back to in-memory limiting

Rate limits can be configured per-user via JWT token claims:
```json
{
  "sub": "user123",
  "rate_limit": {
    "requests_per_minute": 60,
    "burst": 10
  }
}
```

### Circuit Breakers
Circuit breakers protect against cascading failures:

- **Redis Protection**: Circuit breaker wraps Redis operations
- **Configurable Thresholds**: Set failure counts and timeout periods
- **Graceful Degradation**: Automatically switches to fallback implementations
- **Health Recovery**: Half-open state allows testing recovery

Circuit breaker states:
- **Closed** (0): Normal operation, requests pass through
- **Open** (1): Circuit is open, requests use fallback
- **Half-Open** (2): Testing recovery, limited requests allowed

## Authentication

The gateway validates JWT tokens in the Authorization header:

```
Authorization: Bearer <jwt-token>
```

Token claims:
- `sub`: User identifier
- `exp`: Expiration time
- `scopes`: Array of authorized scopes
- `rate_limit`: Rate limiting configuration

## Metrics

Prometheus metrics are exposed at `:9090/metrics`:

**Connection Metrics:**
- `mcp_gateway_connections_total`: Active WebSocket connections
- `mcp_gateway_connections_active`: Currently active connections
- `mcp_gateway_connections_rejected_total`: Connections rejected due to limits
- `mcp_gateway_tcp_connections_total`: Total TCP connections
- `mcp_gateway_tcp_connections_active`: Currently active TCP connections

**Request Metrics:**
- `mcp_gateway_requests_total`: Request count by method and status
- `mcp_gateway_request_duration_seconds`: Request latency histogram
- `mcp_gateway_requests_in_flight`: Number of requests currently being processed
- `mcp_gateway_endpoint_requests_total`: Total requests per endpoint

**WebSocket Metrics:**
- `mcp_gateway_websocket_messages_total`: Total WebSocket messages by direction and type
- `mcp_gateway_websocket_bytes_total`: Total WebSocket bytes transferred by direction

**TCP Protocol Metrics:**
- `mcp_gateway_tcp_messages_total`: Total TCP messages by type and direction
- `mcp_gateway_tcp_bytes_total`: Total TCP bytes transferred by direction
- `mcp_gateway_tcp_protocol_errors_total`: Protocol errors by type

**Auth & Security Metrics:**
- `mcp_gateway_auth_failures_total`: Authentication failures by reason
- `mcp_gateway_rate_limit_exceeded_total`: Rate limit violations by key

**Routing & Circuit Breaker Metrics:**
- `mcp_gateway_routing_errors_total`: Total routing errors by reason
- `mcp_gateway_circuit_breaker_state`: Circuit breaker states (0=closed, 1=open, 2=half-open)

## Health Checks

The gateway provides health monitoring endpoints for Kubernetes and other orchestration systems:

**Available Health Endpoints:**
- `/healthz`: Basic health check (returns 200 OK when gateway is running)
- `/ready`: Readiness check (returns 200 OK when gateway can accept traffic)
- `/health`: Detailed health status with component checks

**Health Check Behavior:**
- Health endpoints check Redis connectivity, backend availability, and internal component status
- Failed health checks return appropriate HTTP status codes for orchestration systems
- Configurable health check intervals and timeouts

## Documentation

- [Circuit Breakers and Resilience](docs/CIRCUIT_BREAKERS.md)
- [Connection Limits and DoS Protection](docs/CONNECTION_LIMITS.md)
- [Binary Protocol Specification](docs/BINARY_PROTOCOL.md)
- [Troubleshooting Guide](docs/TROUBLESHOOTING.md)

## Development

### Building

```bash
# Build binary
make build

# Run tests
make test

# Run locally
make run-debug
```

### Testing

```bash
# Unit tests
make test

# Generate mocks
make mocks

# Load testing
k6 run scripts/load-test.js
```

### Code Structure

```
mcp-gateway/
├── cmd/mcp-gateway/      # Application entry point
├── internal/
│   ├── auth/            # Authentication & rate limiting
│   ├── config/          # Configuration structures
│   ├── discovery/       # Service discovery
│   ├── health/          # Health checks
│   ├── metrics/         # Prometheus metrics (with isolation)
│   ├── ratelimit/       # Rate limiting with circuit breakers
│   ├── router/          # Request routing
│   ├── server/          # WebSocket & TCP servers
│   └── session/         # Session management
└── pkg/
    ├── circuit/         # Circuit breaker implementation
    ├── loadbalancer/    # Load balancing algorithms
    └── wire/            # Binary protocol transport
```

## Troubleshooting

For detailed troubleshooting information, see the [Troubleshooting Guide](docs/TROUBLESHOOTING.md).

### Quick Diagnostics

1. **Check WebSocket connectivity**:
   ```bash
   curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" \
     -H "Authorization: Bearer $TOKEN" \
     https://gateway:8443/
   ```

2. **Verify service discovery**:
   ```bash
   kubectl logs -n mcp-system deployment/mcp-gateway | grep discovery
   ```

3. **Check circuit breaker status**:
   ```bash
   curl http://gateway:9090/metrics | grep circuit_breaker_state
   ```

4. **Monitor rate limiting**:
   ```bash
   curl http://gateway:9090/metrics | grep rate_limit
   ```

### Performance Tuning

1. **Increase connection limits**:
   ```yaml
   server:
     max_connections: 100000
     max_connections_per_ip: 5000
   ```

2. **Configure rate limiting**:
   ```yaml
   rate_limit:
     provider: redis  # Use Redis for shared state
     window_size: 60
   ```

3. **Tune circuit breakers**:
   ```yaml
   circuit_breaker:
     threshold: 10      # More tolerant of failures
     timeout: 60        # Longer recovery time
   ```

4. **Scale horizontally**:
   ```bash
   kubectl scale deployment mcp-gateway -n mcp-system --replicas=5
   ```

## Current Limitations

The following features are documented in planning materials but not yet fully implemented:

- **Frontend Protocols**: Currently only stdio frontend is fully implemented. WebSocket and TCP Binary connections are handled through the HTTP server layer
- **Granular Health Endpoints**: Only `/health`, `/healthz`, and `/ready` are implemented. Per-protocol and per-component health endpoints are planned
- **HTTP/SSE Backends**: Backend support is currently limited to stdio, WebSocket, and SSE protocols

These limitations do not affect the core functionality of routing MCP requests to backend servers.

## Security Considerations

- Always use TLS for WebSocket connections
- Rotate JWT secrets regularly
- Implement proper RBAC for service accounts
- Use network policies to restrict traffic
- Enable audit logging for security events

## License

MIT License - see LICENSE file for details