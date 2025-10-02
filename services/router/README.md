# MCP Router

MCP Router provides universal protocol support for the Model Context Protocol, enabling direct connections to any MCP server and acting as an intelligent bridge with automatic protocol detection, connection pooling, and performance optimization.

## Architecture

### Universal Protocol Support
The Router provides comprehensive protocol support with intelligent routing:

**Direct Protocol Clients:**
- **stdio**: Direct subprocess communication with local MCP servers
- **WebSocket**: Real-time bidirectional communication (ws/wss)
- **HTTP**: RESTful API integration with any HTTP-based MCP server
- **SSE**: Server-Sent Events for streaming protocols

**Gateway Integration:**
- Connects to remote MCP servers via WebSocket or TCP Binary protocol
- **Note**: TCP Binary protocol is currently only supported for gateway connections, not for direct MCP server connections
- Handles automatic reconnection and connection management
- **Queues requests when not connected** with configurable backpressure handling
- Supports multiple authentication methods (Bearer token, mTLS, OAuth2)
- Provides request/response correlation for multiplexed connections
- **Operates in passive mode** - does not auto-initialize, waits for client initialization

**Intelligence Features:**
- **Protocol Auto-Detection**: Automatic identification of MCP server protocols
- **Direct Connection Optimization**: Reduced latency by bypassing gateway for local servers
- **Connection Pooling**: Advanced resource management with health checks
- **Performance Optimization**: Memory optimization and GC tuning

## Features

### Core Protocol Support
- **Direct MCP Server Protocols**: stdio, WebSocket, HTTP, SSE
- **Gateway Connection Protocols**: WebSocket, TCP Binary
- **Direct Connections**: Connect directly to local servers without gateway (significant latency improvement)
- **Protocol Auto-Detection**: Automatic identification of MCP server protocols
- **Gateway Fallback**: Seamless fallback to gateway when direct connection unavailable

### Performance & Reliability
- **Connection Pooling**: Advanced connection pooling with fast connection retrieval
- **Memory Optimization**: Reduced memory usage through object pooling
- **Request Queueing**: Intelligent request buffering with configurable backpressure
- **Timeout Tuning**: Profile-based timeout configurations for optimal performance
- **Circuit Breaker**: Automatic failure detection and recovery with adaptive thresholds

### Security & Authentication
- **Multiple Auth Methods**: Bearer tokens, mTLS, OAuth2 with automatic token refresh
- **Secure Token Storage**: Platform-native secure storage (macOS Keychain, Windows Credential Manager, Linux Secret Service)
- **Per-Message Authentication**: Secure token injection on every request
- **TLS Security**: TLS 1.3 by default with custom CA support
- **Rate Limiting**: Token bucket and sliding window algorithms

### Operations & Monitoring
- **Zero Configuration**: Works with sensible defaults, extensive configuration options available
- **Observable**: Comprehensive Prometheus metrics and structured logging with <1ms overhead
- **Health Checks**: Built-in health endpoints for all protocols
- **Event-Driven Architecture**: No polling loops, fully event-driven state management
- **Cross-Platform**: Single binary for macOS, Linux, and Windows
- **Passive Initialization**: Router waits for client to initialize, doesn't auto-initialize

## Installation

### Quick Install (Recommended)

```bash
# Install with one command
curl -sSL https://raw.githubusercontent.com/poiley/mcp-bridge/main/services/router/install.sh | bash

# Run interactive setup
mcp-router setup
```

### Manual Installation

```bash
# Download the latest release
curl -L https://github.com/actual-software/mcp-bridge/releases/latest/download/mcp-router-$(uname -s)-$(uname -m) -o mcp-router

# Make executable
chmod +x mcp-router

# Move to PATH
sudo mv mcp-router /usr/local/bin/
```

### Build from Source

```bash
# Clone the repository
git clone https://github.com/actual-software/mcp-bridge
cd services/router

# Build the binary
make build

# Install to PATH
make install
```

## Configuration

The router can be configured via:
1. Configuration file (YAML)
2. Environment variables
3. Command-line flags

### Configuration File

Create `~/.config/claude-cli/mcp-router.yaml`:

```yaml
version: 1

# Direct Protocol Clients Configuration
direct_clients:
  enabled: true
  auto_detection:
    enabled: true
    timeout: "1s"
  
  # Connection pooling optimization
  connection_pool:
    max_idle_connections: 20
    max_active_connections: 10
    enable_connection_reuse: true
    
  # Memory optimization
  memory_optimization:
    enable_object_pooling: true
    gc_config:
      enabled: true
      gc_percent: 75

# Server definitions with auto-detection
servers:
  - name: "local-auto-detect"
    connection_mode: "direct"
    auto_detect: true
    candidates:
      - "python examples/weather_server.py"  # stdio
      - "ws://localhost:8080"                # WebSocket
      - "http://localhost:8081"              # HTTP
    fallback: "gateway"

# Gateway Configuration (for remote servers)
gateway:
  # Dual protocol support - WebSocket primary, TCP Binary fallback
  endpoints:
    - url: wss://mcp-gateway.example.com:8443   # Primary WebSocket
    - url: tcps://mcp-gateway.example.com:8444  # Fallback TCP Binary
  protocol_preference: "websocket"
  auto_select: true
  
  auth:
    type: bearer
    token_env: MCP_AUTH_TOKEN
  connection:
    timeout_ms: 5000
    keepalive_interval_ms: 30000
    reconnect:
      initial_delay_ms: 1000
      max_delay_ms: 60000
      multiplier: 2.0
      max_attempts: 10
      jitter: 0.1
    pool:
      enabled: true
      min_size: 2
      max_size: 10
      max_idle_time_ms: 300000  # 5 minutes
      max_lifetime_ms: 1800000  # 30 minutes
      acquire_timeout_ms: 5000
      health_check_interval_ms: 30000
  tls:
    verify: true
    ca_cert_path: /path/to/ca.crt
    min_version: "1.3"
    cipher_suites:
      - TLS_AES_128_GCM_SHA256
      - TLS_AES_256_GCM_SHA384

local:
  request_timeout_ms: 30000
  max_concurrent_requests: 100
  max_queued_requests: 100       # Maximum requests to queue when disconnected
  rate_limit:
    requests_per_second: 10.0
    burst: 20

logging:
  level: info
  format: json
  output: stderr

metrics:
  enabled: true
  endpoint: localhost:9091
```

### Environment Variables

All configuration options can be set via environment variables with the `MCP_` prefix:

```bash
# Gateway Configuration
export MCP_GATEWAY_URL="wss://mcp-gateway.example.com"
export MCP_AUTH_TOKEN="your-auth-token"

# Direct Protocol Features
export MCP_DIRECT_CLIENTS_ENABLED="true"
export MCP_AUTO_DETECT_PROTOCOLS="true"
export MCP_CONNECTION_POOLING_ENABLED="true"
export MCP_MEMORY_OPTIMIZATION_ENABLED="true"

# Performance Tuning
export MCP_OBSERVABILITY_ENABLED="true"
export MCP_LOGGING_LEVEL="info"
export MCP_METRICS_PORT="9091"
```

### Command-Line Flags

```bash
mcp-router --config /path/to/config.yaml --log-level debug
```

## Usage

### Setup Wizard

The easiest way to configure MCP Router is using the interactive setup:

```bash
mcp-router setup
```

This will:
1. Prompt for your Gateway URL
2. Prompt for authentication token
3. Test the connection
4. Save configuration automatically

### Manual Setup

1. Set your authentication token:
   ```bash
   export MCP_AUTH_TOKEN="your-token-here"
   ```

2. Configure Claude CLI to use the router:
   ```bash
   # In Claude CLI configuration
   mcp_server_command: ["mcp-router"]
   ```

3. Use Claude CLI normally - it will automatically connect through the router

### Commands

- `mcp-router` - Start the router (used by Claude CLI)
- `mcp-router setup` - Run interactive setup wizard
- `mcp-router version` - Show version information
- `mcp-router update-check` - Check for available updates

### Uninstalling

To remove MCP Router:

```bash
curl -sSL https://raw.githubusercontent.com/poiley/mcp-bridge/main/services/router/uninstall.sh | sudo bash
```

This will:
- Back up your configuration
- Remove the binary and completions
- Clean up Claude CLI integration

## Protocol Support

MCP Router supports multiple protocols for connecting to gateways:

### WebSocket Protocol
- **URL schemes**: `ws://` (plain), `wss://` (TLS)
- **Use case**: Standard MCP deployments, browser-compatible
- **Features**: Text-based JSON messages, standard HTTP upgrade

### TCP/Binary Protocol
- **URL schemes**: `tcp://` (plain), `tcps://` or `tcp+tls://` (TLS)
- **Use case**: High-performance deployments, reduced overhead
- **Features**: Binary framing, efficient encoding, multiplexing

Example configurations:
```yaml
# WebSocket
gateway:
  url: wss://gateway.example.com:8443

# TCP with TLS
gateway:
  url: tcps://gateway.example.com:9443

# TCP without TLS (not recommended)
gateway:
  url: tcp://internal-gateway:9000
```

## Authentication Methods

### Bearer Token

```yaml
gateway:
  auth:
    type: bearer
    # Option 1: Secure storage (recommended)
    token_secure_key: my-api-token  # Store/retrieve from platform keychain
    
    # Option 2: Environment variable
    token_env: MCP_AUTH_TOKEN  # Read from environment
    
    # Option 3: File
    token_file: ~/.mcp/token  # Read from file
```

**Secure Token Storage**: The router supports platform-native secure storage:
- **macOS**: Keychain Access
- **Windows**: Credential Manager  
- **Linux**: Secret Service (requires `secret-tool`)
- **Fallback**: Encrypted file storage with machine-specific encryption

To store a token securely:
```bash
# Token will be prompted and stored securely
mcp-router store-token --key my-api-token

# Or provide via stdin
echo "your-secret-token" | mcp-router store-token --key my-api-token
```

**Per-Message Authentication**: When using bearer tokens, the router automatically includes the token in every message sent to the gateway for enhanced security.

### mTLS (Mutual TLS)

```yaml
gateway:
  auth:
    type: mtls
    client_cert: ~/.mcp/client.crt
    client_key: ~/.mcp/client.key
  tls:
    ca_cert_path: ~/.mcp/ca.crt  # Custom CA if needed
```

### OAuth2

```yaml
gateway:
  auth:
    type: oauth2
    client_id: "your-client-id"
    
    # Client secret - use secure storage
    client_secret_secure_key: oauth-client-secret
    # OR environment variable
    client_secret_env: OAUTH_CLIENT_SECRET
    
    token_endpoint: "https://auth.example.com/token"
    grant_type: "client_credentials"  # or "password"
    scopes: ["mcp:read", "mcp:write"]
    
    # For password grant type
    username: "user@example.com"
    password_secure_key: oauth-password  # Secure storage
    # OR
    password_env: OAUTH_PASSWORD
```

**Automatic Token Refresh**: The router automatically refreshes OAuth2 tokens before they expire, ensuring uninterrupted service.

**Secure Credential Storage**: OAuth2 client secrets and passwords can be stored securely using the same platform-native storage as bearer tokens.

## Connection Pooling

MCP Router includes advanced connection pooling to optimize resource usage and improve performance:

### Features
- **Automatic Connection Management**: Maintains a pool of pre-established connections
- **Health Checks**: Periodic validation of idle connections
- **Lifecycle Management**: Automatic cleanup of expired connections
- **Concurrent Access**: Thread-safe connection acquisition and release
- **Metrics**: Detailed pool statistics for monitoring

### Configuration
```yaml
gateway:
  connection:
    pool:
      enabled: true               # Enable/disable pooling
      min_size: 2                # Minimum connections to maintain
      max_size: 10               # Maximum concurrent connections
      max_idle_time_ms: 300000   # Max idle time before cleanup (5 min)
      max_lifetime_ms: 1800000   # Max connection lifetime (30 min)
      acquire_timeout_ms: 5000   # Timeout waiting for connection
      health_check_interval_ms: 30000  # Health check frequency
```

### Benefits
- **Performance**: Eliminates connection establishment overhead
- **Resource Efficiency**: Reuses connections across multiple requests
- **Reliability**: Automatic recovery from connection failures
- **Scalability**: Handles burst traffic with connection queueing

## Request Queueing

The router implements intelligent request queueing to handle requests that arrive before the gateway connection is established:

### Features
- **Automatic Queueing**: Requests are automatically queued when the router is not connected
- **Configurable Queue Size**: Set maximum queue size to prevent memory issues
- **Backpressure Handling**: Rejects new requests when queue is full
- **FIFO Processing**: Queued requests are processed in order when connection is established
- **Timeout Support**: Queued requests respect their original timeout constraints
- **Event-Driven**: No polling loops - queue processing triggered by connection state changes

### Configuration
```yaml
local:
  max_queued_requests: 100  # Maximum number of requests to queue (default: 100)
  request_timeout_ms: 30000  # Timeout applies even while queued
```

### Behavior
1. When a request arrives and the router is not connected to the gateway:
   - The request is added to the queue (if space available)
   - The client receives a response once the request is processed or times out
2. When the connection is established:
   - All queued requests are immediately processed in order
   - New requests bypass the queue and are processed directly
3. If the queue is full:
   - New requests are rejected with a "queue full" error
   - This provides backpressure to prevent memory exhaustion

## Passive Initialization Mode

The router operates in **passive mode** when acting as a stdio bridge:

### Key Behaviors
- **No Auto-Initialization**: The router does NOT automatically send initialization when connecting to the gateway
- **Client-Controlled**: The client (Claude CLI) controls when to send the initialization request
- **Request Forwarding**: The router simply forwards the client's initialization request to the gateway
- **Transparent Operation**: The router acts as a true proxy, not initiating any protocol handshakes

### Why Passive Mode?
This design ensures that:
- The client maintains full control over the MCP protocol flow
- No duplicate initialization requests are sent
- The router remains transparent to the protocol layer
- Compatibility with all MCP clients is maintained

## Development

### Prerequisites

- Go 1.23.0 or higher (toolchain 1.24.5 recommended)
- Make

### Building

```bash
# Build for current platform
make build

# Build for all platforms
make release

# Run tests
make test

# Run with debug logging
make run-debug
```

### Project Structure

```
services/router/
├── cmd/mcp-router/      # Main application entry point
├── internal/            # Private application code
│   ├── config/         # Configuration management
│   ├── gateway/        # Gateway clients (WebSocket & TCP)
│   │   ├── client.go   # WebSocket client implementation
│   │   ├── tcp_client.go # TCP/Binary client
│   │   ├── binary_frame.go # Binary protocol framing
│   │   ├── factory.go  # Client factory pattern
│   │   └── oauth2.go   # OAuth2 token management
│   ├── pool/           # Connection pooling
│   │   ├── pool.go     # Core pool implementation
│   │   ├── websocket_pool.go # WebSocket-specific pooling
│   │   ├── tcp_pool.go # TCP-specific pooling
│   │   └── manager.go  # Pool manager interface
│   ├── router/         # Core routing logic
│   │   ├── router.go   # Main router implementation
│   │   ├── message_router.go # Message routing and correlation
│   │   ├── connection_manager.go # Connection state management
│   │   └── request_queue.go # Request queueing system
│   ├── stdio/          # Stdio handling
│   ├── ratelimit/      # Rate limiting implementations
│   └── metrics/        # Prometheus metrics exporter
├── pkg/mcp/            # MCP protocol types
├── test/               # Test utilities and integration tests
├── Makefile            # Build automation
└── README.md           # This file
```

## Monitoring

### Prometheus Metrics

The router exposes Prometheus metrics at `http://localhost:9091/metrics`:

- `mcp_router_requests_total` - Total number of requests
- `mcp_router_responses_total` - Total number of responses
- `mcp_router_errors_total` - Total number of errors
- `mcp_router_request_duration_seconds` - Request duration histogram
- `mcp_router_response_size_bytes` - Response size histogram
- `mcp_router_active_connections` - Current active connections
- `mcp_router_connection_retries_total` - Connection retry count

### Health Check

Check router health at `http://localhost:9091/health`:
```bash
curl http://localhost:9091/health
# {"status":"healthy"}
```

## Troubleshooting

### Connection Issues

1. **Check gateway connectivity**:
   ```bash
   # For HTTPS endpoints
   curl -k https://mcp-gateway.example.com/health
   
   # For WebSocket, use wscat
   wscat -c wss://mcp-gateway.example.com
   
   # For TCP, use telnet or nc
   nc -zv gateway.example.com 9443
   ```

2. **Verify authentication**:
   ```bash
   # Check JWT token (if applicable)
   echo $MCP_AUTH_TOKEN | cut -d. -f2 | base64 -d | jq
   
   # Test OAuth2 endpoint
   curl -X POST https://auth.example.com/token \
     -d "grant_type=client_credentials" \
     -d "client_id=$CLIENT_ID" \
     -d "client_secret=$CLIENT_SECRET"
   ```

3. **Enable debug logging**:
   ```bash
   MCP_LOGGING_LEVEL=debug mcp-router
   ```

4. **Check TLS certificates**:
   ```bash
   # Verify server certificate
   openssl s_client -connect gateway.example.com:8443 -servername gateway.example.com
   
   # Check client certificate (for mTLS)
   openssl x509 -in ~/.mcp/client.crt -text -noout
   ```

### Performance Issues

1. **Monitor metrics**:
   ```bash
   # Key metrics to watch
   curl -s http://localhost:9091/metrics | grep -E "mcp_router_(requests|errors|duration)"
   ```

2. **Check rate limiting**:
   ```bash
   # See if requests are being rate limited
   curl -s http://localhost:9091/metrics | grep mcp_router_rate_limit_exceeded
   ```

3. **Connection pool status**:
   ```bash
   # Monitor active connections
   watch -n 1 'curl -s http://localhost:9091/metrics | grep mcp_router_active_connections'
   ```

### Common Error Messages

| Error | Cause | Solution |
|-------|-------|----------|
| `connection refused` | Gateway is down | Check gateway status and URL |
| `certificate verify failed` | TLS certificate issue | Verify CA cert or disable verification (dev only) |
| `401 unauthorized` | Invalid auth token | Check token and expiration |
| `rate limit exceeded` | Too many requests | Adjust rate limit configuration |
| `context deadline exceeded` | Request timeout | Increase `request_timeout_ms` |
| `no such host` | DNS resolution failed | Verify gateway hostname |

### Debug Mode

For detailed troubleshooting, run with full debug output:
```bash
MCP_LOGGING_LEVEL=debug \
MCP_ADVANCED_DEBUG_LOG_FRAMES=true \
mcp-router 2>&1 | jq -r .msg
```

## Security Features

### Transport Security
- **TLS 1.3 by Default**: All connections use modern TLS with configurable cipher suites
- **Custom CA Support**: Configure trusted certificate authorities for internal PKI
- **Certificate Pinning**: Optional certificate pinning for enhanced security
- **Protocol Version Negotiation**: Secure handshake for binary protocol connections

### Authentication & Authorization
- **Multiple Auth Methods**: Bearer tokens, OAuth2 with auto-refresh, mTLS
- **Secure Token Storage**: Platform-native keychains (macOS Keychain, Windows Credential Manager, Linux Secret Service)
- **Per-Message Authentication**: Tokens validated on every request for enhanced security
- **Token Rotation**: Support for automatic token refresh and rotation

### Application Security
- **Input Validation**: All user inputs validated to prevent injection attacks
- **Request Size Limits**: Configurable limits to prevent DoS attacks
- **Rate Limiting**: Token bucket algorithm with configurable rates
- **Circuit Breakers**: Automatic failure detection and recovery
- **No Credential Logging**: Authentication tokens never appear in logs

### Operational Security
- **Secure Configuration**: Sensitive values stored in platform keychains
- **Encrypted Storage Fallback**: Machine-specific encryption when keychain unavailable
- **Memory Protection**: Credentials cleared from memory after use
- **Audit Trail**: Security events logged for monitoring

## Current Limitations

The following limitations apply to the current implementation:

- **TCP Binary Protocol**: Only supported for gateway connections, not for direct MCP server connections. Direct connections support stdio, WebSocket, HTTP, and SSE protocols
- **OAuth2 Scope**: OAuth2 authentication is currently only available for gateway connections, not for direct server connections
- **Protocol Detection**: While protocol auto-detection works well, detection accuracy depends on server behavior and network conditions. Manual protocol specification is recommended for production use

These limitations do not affect the core functionality of routing MCP requests to servers via the gateway or through direct connections.

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Contributing

Contributions are welcome! Please read CONTRIBUTING.md for guidelines.

## Support

For issues and feature requests, please visit:
- **Issues**: https://github.com/actual-software/mcp-bridge/issues
- **Discussions**: https://github.com/actual-software/mcp-bridge/discussions