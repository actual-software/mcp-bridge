# Configuration Reference

Complete reference for all configuration options in MCP Router and Gateway.

## Table of Contents

- [Router Configuration](#router-configuration)
- [Gateway Configuration](#gateway-configuration)
- [Environment Variables](#environment-variables)
- [Configuration Precedence](#configuration-precedence)
- [Examples](#examples)

## Router Configuration

### File Locations

The router looks for configuration in the following order:
1. Command line flag: `--config /path/to/config.yaml`
2. `$HOME/.config/claude-cli/mcp-router.yaml`
3. `$HOME/.config/mcp/mcp-router.yaml`
4. `/etc/mcp/mcp-router.yaml`

### Complete Schema

```yaml
# Configuration version (required)
version: 1

# Gateway Pool configuration (multi-gateway support)
gateway_pool:
  # List of gateway endpoints
  endpoints:
    - url: string              # Gateway URL (wss://, ws://, tcp://)
      weight: integer         # Load balancing weight (default: 1)
      priority: integer       # Priority for failover (default: 0)
      tags: [string]         # Tags for namespace routing

      # Per-endpoint authentication
      auth:
        type: string          # Options: bearer, oauth2, mtls
        token_env: string     # Environment variable for bearer token
        # ... (see auth section below)

      # Per-endpoint connection settings
      connection:
        timeout_ms: integer
        # ... (see connection section below)

      # Per-endpoint TLS
      tls:
        # ... (see TLS section below)

  # Load balancing configuration
  load_balancer:
    strategy: string          # Options: round_robin, least_connections, weighted, priority
    health_check_path: string # Optional health check endpoint
    failover_timeout: duration # Default: 30s
    retry_count: integer      # Default: 3

  # Service discovery for gateways
  service_discovery:
    enabled: boolean          # Default: false
    refresh_interval: duration # Default: 30s
    health_check_interval: duration # Default: 10s
    unhealthy_threshold: integer # Default: 3
    healthy_threshold: integer # Default: 2

  # Circuit breaker for gateway connections
  circuit_breaker:
    enabled: boolean          # Default: false
    failure_threshold: integer # Default: 5
    recovery_timeout: duration # Default: 30s
    success_threshold: integer # Default: 3
    timeout_duration: duration # Default: 10s
    monitoring_window: duration # Default: 60s

  # Namespace-based routing
  namespace_routing:
    enabled: boolean          # Default: false
    rules:
      - pattern: string       # Regex pattern for method matching
        tags: [string]       # Route to endpoints with these tags
        priority: integer    # Rule priority (higher = first)
        description: string

# Legacy single gateway configuration (deprecated, use gateway_pool)
gateway:
  # Gateway URL (required)
  url: string  # Format: wss://host:port, ws://host:port, tcp://host:port

  # Authentication configuration
  auth:
    # Authentication type
    type: string  # Options: bearer, oauth2, mtls
    
    # Bearer token options (when type: bearer)
    token: string              # Direct token (not recommended)
    token_env: string         # Environment variable name
    token_file: string        # File path containing token
    token_secure_key: string  # Key for secure storage (recommended)
    
    # OAuth2 options (when type: oauth2)
    client_id: string
    client_secret: string              # Direct secret (not recommended)
    client_secret_env: string         # Environment variable name
    client_secret_secure_key: string  # Key for secure storage
    token_endpoint: string            # OAuth2 token endpoint URL
    scopes: [string]                  # OAuth2 scopes
    grant_type: string                # Options: client_credentials, password
    username: string                  # For password grant
    password: string                  # Direct password (not recommended)
    password_env: string              # Environment variable name
    password_secure_key: string       # Key for secure storage
    
  # Connection settings
  connection:
    timeout_ms: integer           # Default: 5000
    keepalive_interval_ms: integer  # Default: 30000
    
    # Reconnection configuration
    reconnect:
      initial_delay_ms: integer   # Default: 1000
      max_delay_ms: integer       # Default: 60000
      multiplier: float           # Default: 2.0
      max_attempts: integer       # Default: -1 (infinite)
      jitter: float              # Default: 0.1 (0-1 range)
    
    # Connection pooling
    pool:
      enabled: boolean           # Default: false
      min_size: integer         # Default: 2
      max_size: integer         # Default: 10
      max_idle_time_ms: integer # Default: 300000 (5 min)
      max_lifetime_ms: integer  # Default: 1800000 (30 min)
      acquire_timeout_ms: integer # Default: 5000
      health_check_interval_ms: integer # Default: 30000
  
  # TLS configuration
  tls:
    verify: boolean             # Default: true
    ca_file: string            # CA certificate file path
    client_cert: string        # Client certificate for mTLS
    client_key: string         # Client key for mTLS
    min_version: string        # Options: "1.2", "1.3" (default: "1.3")
    cipher_suites: [string]    # Allowed cipher suites

# Direct server connections (bypass gateway)
direct:
  # Auto-detection settings
  auto_detection:
    enabled: boolean           # Default: true
    timeout: duration         # Default: 10s
    cache_results: boolean    # Default: true
    cache_ttl: duration      # Default: 5m
    preferred_order: [string] # Default: ["http", "websocket", "stdio", "sse"]

  # Global settings
  default_timeout: duration    # Default: 30s
  max_connections: integer    # Default: 100

  # Health check configuration
  health_check:
    enabled: boolean          # Default: true
    interval: duration       # Default: 30s
    timeout: duration        # Default: 5s

  # Protocol-specific settings
  stdio:
    process_timeout: duration     # Default: 30s
    max_buffer_size: integer     # Default: 65536

  websocket:
    handshake_timeout: duration  # Default: 10s
    ping_interval: duration     # Default: 30s
    pong_timeout: duration      # Default: 10s
    max_message_size: integer   # Default: 10485760

  http:
    request_timeout: duration    # Default: 30s
    max_idle_conns: integer     # Default: 100
    follow_redirects: boolean   # Default: true

  sse:
    request_timeout: duration    # Default: 30s
    stream_timeout: duration    # Default: 300s
    buffer_size: integer        # Default: 65536

# Local router settings
local:
  read_buffer_size: integer     # Default: 65536
  write_buffer_size: integer    # Default: 65536
  request_timeout_ms: integer   # Default: 30000
  max_concurrent_requests: integer # Default: 100
  max_queued_requests: integer  # Default: 1000

  # Rate limiting
  rate_limit:
    enabled: boolean           # Default: false
    requests_per_sec: float    # Default: 100.0
    burst: integer            # Default: 200

# Logging configuration
logging:
  level: string               # Options: debug, info, warn, error
  format: string              # Options: json, text
  output: string              # Options: stdout, stderr, file path
  include_caller: boolean     # Default: false
  
  # Log sampling
  sampling:
    enabled: boolean          # Default: false
    initial: integer         # Default: 100
    thereafter: integer      # Default: 100

# Metrics configuration
metrics:
  enabled: boolean            # Default: true
  endpoint: string           # Default: "localhost:9091"
  path: string              # Default: "/metrics"
  labels:                   # Additional labels
    key: value

# Advanced settings
advanced:
  protocol: string           # Options: websocket, tcp
  tcp_address: string       # For TCP protocol
  tcp_nodelay: boolean      # Default: true
  
  # Compression
  compression:
    enabled: boolean         # Default: false
    algorithm: string       # Options: gzip, zstd
    level: integer         # 1-9 (gzip), 1-22 (zstd)
    min_size: integer      # Minimum size to compress
  
  # Circuit breaker
  circuit_breaker:
    failure_threshold: integer    # Default: 5
    success_threshold: integer    # Default: 2
    timeout_seconds: integer      # Default: 30
    max_requests: integer         # Default: 10
    interval_seconds: integer     # Default: 60
  
  # Deduplication
  deduplication:
    enabled: boolean             # Default: false
    cache_size: integer         # Default: 1000
    ttl_seconds: integer        # Default: 60
  
  # Debug options
  debug:
    log_frames: boolean         # Default: false
    save_failures: boolean      # Default: false
    failure_dir: string        # Default: "/tmp/mcp-failures"
    enable_pprof: boolean      # Default: false
    pprof_port: integer       # Default: 6060
  
  # Secure storage
  secure_storage:
    backend: string            # Options: auto, keychain, file, custom
    file_path: string         # For file backend
    custom_command: string    # For custom backend
    custom_args: [string]     # For custom backend

# Experimental features
features:
  binary_protocol_v2: boolean    # Default: false
  request_priorities: boolean    # Default: false
  adaptive_rate_limit: boolean   # Default: false
  connection_multiplexing: boolean # Default: false
```

## Gateway Configuration

### Complete Schema

```yaml
# Configuration version
version: 1

# Server configuration
server:
  host: string                   # Default: "0.0.0.0"
  port: integer                  # Default: 8443
  protocol: string               # Deprecated: use frontends. Options: websocket, tcp, both
  tcp_port: integer             # Default: 8444
  tcp_health_port: integer      # Default: 9002
  metrics_port: integer         # Default: 9090
  health_port: integer          # HTTP health check port
  max_connections: integer      # Default: 10000
  max_connections_per_ip: integer # Default: 100
  connection_buffer_size: integer # Default: 65536
  read_timeout: integer         # Seconds, default: 300
  write_timeout: integer        # Seconds, default: 300
  idle_timeout: integer         # Seconds, default: 300
  allowed_origins: [string]     # CORS origins for WebSocket

  # Multi-frontend configuration (recommended)
  frontends:
    - name: string              # Frontend name
      protocol: string          # Options: websocket, http, sse, tcp_binary, stdio
      enabled: boolean          # Default: true
      config:                   # Protocol-specific config (map)
        # For WebSocket/HTTP/SSE:
        host: string           # Listen host
        port: integer          # Listen port
        tls:
          enabled: boolean
          cert_file: string
          key_file: string

        # For stdio:
        mode: string           # Options: unix_socket, stdin_stdout, named_pipes
        socket_path: string    # For unix_socket mode
        permissions: string    # Unix permissions (e.g., "0600")

  # stdio frontend configuration (alternative to frontends)
  stdio_frontend:
    enabled: boolean
    modes:
      - type: string           # Options: unix_socket, stdin_stdout, named_pipes
        path: string           # Socket path or pipe name
        permissions: string    # Unix socket permissions
        enabled: boolean
    process_management:
      max_concurrent_clients: integer
      client_timeout: duration
      auth_required: boolean

  # Security settings
  security:
    max_request_size: integer   # Max HTTP request size in bytes (default: 1MB)
    max_header_size: integer    # Max HTTP header size in bytes (default: 1MB)
    max_message_size: integer   # Max WebSocket message size (default: 10MB)

  # TLS configuration
  tls:
    enabled: boolean            # Default: false
    cert_file: string          # TLS certificate file
    key_file: string           # TLS key file
    ca_file: string            # CA certificate for client verification
    client_auth: string        # Options: none, request, require. Default: none
    min_version: string        # Options: "1.2", "1.3". Default: "1.3" (recommended)
    cipher_suites: [string]    # Allowed cipher suites

# Backend configuration (alternative to service_discovery)
backends:
  backends:
    - name: string              # Backend name
      protocol: string          # Options: stdio, websocket, http, sse
      config:                   # Protocol-specific config (map)
        # For stdio:
        command: [string]      # Command and arguments
        working_dir: string    # Working directory
        env:                  # Environment variables
          key: value
        health_check:
          enabled: boolean
          interval: duration
          timeout: duration

        # For WebSocket:
        endpoints: [string]    # WebSocket URLs
        connection_pool:
          max_size: integer
          idle_timeout: duration
        headers:
          key: value

        # For HTTP:
        base_url: string       # Base URL
        request_timeout: duration
        max_retries: integer

        # For SSE:
        base_url: string
        stream_endpoint: string
        request_endpoint: string

# Authentication configuration
auth:
  type: string                  # Options: bearer, oauth2, jwt
  provider: string             # Alias for type
  per_message_auth: boolean    # Default: false
  per_message_auth_cache: integer   # Cache duration in seconds
  
  # JWT configuration (when type: jwt)
  jwt:
    issuer: string
    audience: string              # Default: "mcp-gateway"
    secret_key_env: string
    public_key_path: string
  
  # OAuth2 configuration (when type: oauth2)
  oauth2:
    client_id: string
    client_secret_env: string
    token_endpoint: string
    introspect_endpoint: string
    jwks_endpoint: string
    scopes: [string]
    issuer: string
    audience: string

# Session management
sessions:
  provider: string              # Options: memory, redis. Default: memory
  ttl: integer                 # Session TTL in seconds. Default: 3600
  cleanup_interval: integer    # Cleanup interval in seconds. Default: 600

  # Redis configuration (when provider: redis)
  redis:
    url: string                # Redis URL. Format: redis://host:port/db
    addresses: [string]        # For Sentinel/Cluster
    password: string
    password_env: string
    db: integer               # Database number. Default: 0
    pool_size: integer        # Connection pool size. Default: 10
    max_retries: integer      # Default: 3
    dial_timeout: integer     # Milliseconds. Default: 5000
    read_timeout: integer     # Milliseconds. Default: 3000
    write_timeout: integer    # Milliseconds. Default: 3000
    sentinel_master_name: string # For Sentinel setup

# Rate limiting
rate_limit:
  enabled: boolean             # Default: false
  provider: string            # Options: memory, redis
  requests_per_sec: float
  burst: integer
  window_size: integer        # Window size in seconds
  
  # Redis config (inherits from sessions.redis)
  redis:
    # Same as sessions.redis

# Circuit breaker
circuit_breaker:
  failure_threshold: integer   # Default: 5
  success_threshold: integer   # Default: 2
  timeout_seconds: integer     # Default: 30
  max_requests: integer       # Default: 100
  interval_seconds: integer   # Default: 60
  success_ratio: float       # Default: 0.6

# Service discovery
service_discovery:
  provider: string            # Options: static, kubernetes, stdio, websocket, sse
  mode: string               # Alias for provider
  refresh_rate: duration     # Default: 30s
  namespace_selector: [string] # Filter by namespaces
  label_selector:           # Filter by labels
    key: value

  # Static discovery
  static:
    endpoints:
      namespace_name:
        - url: string
          labels:
            key: value

  # Kubernetes discovery
  kubernetes:
    in_cluster: boolean      # Default: true
    config_path: string      # Kubeconfig path
    namespace_pattern: string # Regex pattern
    service_labels:         # Required labels
      key: value

  # stdio (subprocess) discovery
  stdio:
    services:
      - name: string          # Service name
        namespace: string     # Service namespace
        command: [string]     # Command and arguments
        working_dir: string   # Working directory
        env:                 # Environment variables
          key: value
        weight: integer      # Load balancing weight
        metadata:           # Custom metadata
          key: value
        health_check:
          enabled: boolean   # Default: true
          interval: duration # Default: 30s
          timeout: duration  # Default: 5s

  # WebSocket discovery
  websocket:
    services:
      - name: string          # Service name
        namespace: string     # Service namespace
        endpoints: [string]   # WebSocket URLs
        headers:             # Custom headers
          key: value
        weight: integer      # Load balancing weight
        metadata:           # Custom metadata
          key: value
        health_check:
          enabled: boolean   # Default: true
          interval: duration # Default: 30s
          timeout: duration  # Default: 5s
        tls:
          enabled: boolean
          insecure_skip_verify: boolean
          cert_file: string
          key_file: string
          ca_file: string
        origin: string        # WebSocket origin header

  # SSE (Server-Sent Events) discovery
  sse:
    services:
      - name: string          # Service name
        namespace: string     # Service namespace
        base_url: string      # Base URL
        stream_endpoint: string # SSE stream path
        request_endpoint: string # Request path
        headers:             # Custom headers
          key: value
        weight: integer      # Load balancing weight
        metadata:           # Custom metadata
          key: value
        health_check:
          enabled: boolean   # Default: true
          interval: duration # Default: 30s
          timeout: duration  # Default: 5s
        timeout: duration     # Default: 300s

# Routing configuration
routing:
  strategy: string           # Options: round_robin, least_conn, random
  health_check_interval: duration # Default: 10s
  
  # Per-namespace circuit breakers
  circuit_breaker:
    # Same schema as global circuit_breaker

# Metrics configuration
metrics:
  enabled: boolean           # Default: true
  path: string              # Default: "/metrics"
  port: integer             # If different from metrics_port

# Logging configuration
logging:
  level: string             # Options: debug, info, warn, error
  format: string            # Options: json, text
  output: string            # Options: stdout, stderr
  include_caller: boolean   # Default: false
  
  # Sampling config
  sampling:
    enabled: boolean
    initial: integer
    thereafter: integer

# Distributed tracing
tracing:
  enabled: boolean          # Default: false
  service_name: string      # Default: "mcp-gateway"
  sampler_type: string      # Options: const, probabilistic, adaptive
  sampler_param: float      # Sampling rate (0.0-1.0)
  reporter_log_spans: boolean # Default: false
  agent_host: string        # Jaeger agent host
  agent_port: integer       # Jaeger agent port (default: 6831)

# Security configuration
security:
  # Input validation limits
  validation:
    max_namespace_length: integer    # Default: 255
    max_method_length: integer       # Default: 255
    max_id_length: integer          # Default: 255
    max_token_length: integer       # Default: 4096
    enforce_utf8: boolean           # Default: true
    
  # Security headers (HTTP/WebSocket only)
  headers:
    hsts_max_age: integer           # Default: 31536000 (1 year)
    hsts_include_subdomains: boolean # Default: true
    x_frame_options: string         # Default: "DENY"
    x_content_type_options: string  # Default: "nosniff"
    x_xss_protection: string        # Default: "1; mode=block"
    referrer_policy: string         # Default: "strict-origin-when-cross-origin"
    content_security_policy: string # Default: "default-src 'none'"
    
  # Binary protocol security
  binary_protocol:
    enforce_version_negotiation: boolean # Default: true
    max_payload_size: integer           # Default: 10485760 (10MB)
    
  # security.txt endpoint
  security_txt:
    enabled: boolean                # Default: true
    contact: string                # Security contact email
    expires: string                # Expiry date (ISO 8601)
    preferred_languages: [string]  # Default: ["en"]
    canonical: string              # Canonical URL
    policy: string                 # Security policy URL
    acknowledgments: string        # Hall of fame URL
```

## Environment Variables

### Router Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_CONFIG` | Configuration file path | Search default locations |
| `MCP_GATEWAY_URL` | Gateway URL | Required |
| `MCP_AUTH_TOKEN` | Bearer auth token | - |
| `MCP_CLIENT_SECRET` | OAuth2 client secret | - |
| `MCP_OAUTH_PASSWORD` | OAuth2 password grant | - |
| `MCP_LOG_LEVEL` | Log level | info |
| `MCP_LOG_FORMAT` | Log format | json |
| `MCP_METRICS_ENABLED` | Enable metrics | true |
| `MCP_METRICS_PORT` | Metrics port | 9091 |

### Gateway Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_GATEWAY_CONFIG` | Configuration file path | /etc/mcp/gateway.yaml |
| `MCP_SERVER_PORT` | Server port | 8443 |
| `MCP_TCP_PORT` | TCP protocol port | 8444 |
| `MCP_MAX_CONNECTIONS` | Max connections | 10000 |
| `MCP_JWT_SECRET` | JWT secret key | - |
| `MCP_OAUTH_CLIENT_SECRET` | OAuth2 client secret | - |
| `MCP_REDIS_URL` | Redis URL | - |
| `MCP_REDIS_PASSWORD` | Redis password | - |
| `MCP_LOG_LEVEL` | Log level | info |
| `MCP_TRACE_ENABLED` | Enable tracing | false |

## Configuration Precedence

Configuration is loaded in the following order (later sources override earlier ones):

1. Default values
2. Configuration file
3. Environment variables
4. Command-line flags

### Router Precedence Example

```yaml
# mcp-router.yaml
gateway:
  url: wss://default.example.com
  auth:
    token_env: MCP_TOKEN
```

```bash
# Override via environment
export MCP_GATEWAY_URL=wss://override.example.com

# Override via command line
mcp-router --gateway-url wss://cli.example.com
```

Result: Gateway URL will be `wss://cli.example.com`

## Examples

### Minimal Router Configuration

```yaml
version: 1
gateway:
  url: wss://gateway.example.com
  auth:
    type: bearer
    token_secure_key: "prod-token"
```

### High-Performance Router Configuration

```yaml
version: 1
gateway:
  url: tcp://gateway.example.com:8444
  auth:
    type: bearer
    token_secure_key: "prod-token"
  connection:
    pool:
      enabled: true
      min_size: 5
      max_size: 20
advanced:
  protocol: tcp
  compression:
    enabled: true
    algorithm: zstd
    level: 1
```

### Minimal Gateway Configuration

```yaml
version: 1
server:
  port: 8443
auth:
  type: bearer
discovery:
  provider: static
  static:
    endpoints:
      default:
        - url: http://mcp-server:3000
```

### Production Gateway Configuration

```yaml
version: 1
server:
  protocol: both
  port: 8443
  tcp_port: 8444
  max_connections: 50000
  tls:
    enabled: true
    cert_file: /tls/tls.crt
    key_file: /tls/tls.key
    min_version: "1.3"

auth:
  type: oauth2
  oauth2:
    client_id: mcp-gateway
    client_secret_env: OAUTH_SECRET
    introspect_endpoint: https://auth.example.com/introspect

sessions:
  provider: redis
  redis:
    addresses:
      - redis-0:26379
      - redis-1:26379
    sentinel_master_name: mymaster
    password_env: REDIS_PASSWORD

rate_limit:
  enabled: true
  provider: redis
  requests_per_sec: 1000
  burst: 2000

discovery:
  provider: kubernetes
  kubernetes:
    namespace_pattern: "mcp-.*"
    service_labels:
      mcp-enabled: "true"

metrics:
  enabled: true

logging:
  level: info
  format: json

tracing:
  enabled: true
  service_name: mcp-gateway-prod
  agent_host: jaeger-agent
```

### Security-Focused Configuration

```yaml
version: 1

server:
  port: 8443
  protocol: both
  max_connections: 10000
  max_connections_per_ip: 100
  
  security:
    max_request_size: 1048576      # 1MB
    max_header_size: 1048576       # 1MB
    max_message_size: 10485760     # 10MB
  
  tls:
    enabled: true
    cert_file: /tls/tls.crt
    key_file: /tls/tls.key
    min_version: "1.3"
    client_auth: require           # mTLS required
    ca_file: /tls/ca.crt

security:
  validation:
    max_namespace_length: 255
    max_method_length: 255
    max_id_length: 255
    max_token_length: 4096
    enforce_utf8: true
    
  headers:
    hsts_max_age: 63072000         # 2 years
    hsts_include_subdomains: true
    x_frame_options: "DENY"
    x_content_type_options: "nosniff"
    x_xss_protection: "1; mode=block"
    referrer_policy: "strict-origin-when-cross-origin"
    content_security_policy: "default-src 'none'; frame-ancestors 'none';"
    
  binary_protocol:
    enforce_version_negotiation: true
    max_payload_size: 10485760     # 10MB
    
  security_txt:
    enabled: true
    contact: "security@example.com"
    expires: "2025-12-31T23:59:59Z"
    preferred_languages: ["en", "es"]
    canonical: "https://example.com/.well-known/security.txt"
    policy: "https://example.com/security-policy"
    acknowledgments: "https://example.com/hall-of-fame"

rate_limit:
  enabled: true
  provider: redis
  requests_per_sec: 100
  burst: 200
  
  # Per-IP rate limiting handled via max_connections_per_ip

auth:
  type: jwt
  per_message_auth: true           # Extra security
  per_message_cache: 300           # 5 minute cache
  
  jwt:
    issuer: "https://auth.example.com"
    audience: "mcp-gateway"
    public_key_path: "/keys/jwt-public.pem"
```