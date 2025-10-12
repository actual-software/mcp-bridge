# MCP Bridge Production Deployment Guide

Comprehensive guide for deploying the MCP Bridge system with universal protocol support in production environments.

## 🔧 Critical Configuration Requirements (From E2E Testing)

### Redis Session Storage Configuration

**Essential for Production Deployments:**

Redis is **required** for session management in multi-instance gateway deployments. The following configuration has been validated through comprehensive E2E testing:

#### Docker Compose Configuration
```yaml
services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"  # CRITICAL: Required for gateway connectivity
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5
    command: redis-server --appendonly yes --maxmemory 256mb --maxmemory-policy allkeys-lru

  gateway:
    depends_on:
      redis:
        condition: service_healthy
    environment:
      - REDIS_ADDR=redis:6379
```

#### Gateway Redis Configuration
```yaml
# config/gateway.yaml
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
    pool_size: 10
    min_idle_conns: 2
    idle_timeout: 300s

# Session management settings
session_config:
  ttl: 3600s          # 1 hour session lifetime
  cleanup_interval: 300s  # 5 minute cleanup cycle
  max_idle: 1800s     # 30 minute idle timeout
```

### Connection Pool Optimization

**Performance-Validated Settings:**

```yaml
# Environment-specific connection limits (validated through testing)
connection:
  pool:
    # Development environment (resource-constrained)
    max_size: 10
    min_size: 2
    
    # Production environment (high-performance)
    # max_size: 100
    # min_size: 5
    
    # Load testing environment (controlled stress)
    # max_size: 25
    # min_size: 5
    
    idle_timeout: 30s
    max_lifetime: 300s
    dial_timeout: 10s
```

**System-Level Optimizations:**
```bash
# Increase file descriptor limits for production
echo "fs.file-max = 65536" >> /etc/sysctl.conf
echo "* soft nofile 65536" >> /etc/security/limits.conf  
echo "* hard nofile 65536" >> /etc/security/limits.conf

# Apply immediately (requires restart for permanent effect)
ulimit -n 65536
```

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Quick Start](#quick-start)
3. [Universal Protocol Configuration](#universal-protocol-configuration)
4. [Router Installation](#router-installation)
5. [Gateway Deployment](#gateway-deployment)
6. [Redis Setup](#redis-setup)
7. [Authentication Configuration](#authentication-configuration)
8. [TLS and Security](#tls-and-security)
9. [High Availability](#high-availability)
10. [Monitoring](#monitoring)
11. [Troubleshooting](#troubleshooting)

## Prerequisites

### Router
- Operating System: macOS 10.12+, Windows 10+, or Linux with D-Bus
- Secure storage: Platform keychain service
- Network: Outbound HTTPS/WSS access for gateway connections
- Network: Direct protocol access for local MCP servers (stdio, WebSocket, HTTP, SSE)

### Gateway
- Kubernetes cluster 1.24+
- kubectl configured with cluster access
- Redis 6.0+ (for production session storage and rate limiting)
- cert-manager 1.10+ (for TLS certificate management)
- Prometheus Operator (optional, for comprehensive metrics collection)
- Service Discovery: Kubernetes, Consul, or static configuration

### Build Requirements (optional)
- Go 1.25.0+
- Make
- Docker (for gateway images)

## Universal Protocol Configuration

MCP Bridge provides comprehensive protocol support with automatic detection and cross-protocol load balancing. This section covers the universal protocol capabilities implemented across both Router and Gateway components.

### Protocol Support Matrix

**Router Direct Protocol Support:**
- **stdio**: Local subprocess communication (65% latency improvement)
- **WebSocket**: Real-time bidirectional communication (ws/wss)
- **HTTP**: RESTful API integration with polling/streaming
- **SSE**: Server-Sent Events for streaming protocols
- **TCP Binary**: High-performance binary protocol communication

**Gateway Backend Integration:**
- **stdio**: Unix socket and named pipe communication
- **WebSocket**: Standards-compliant WebSocket servers
- **HTTP**: RESTful MCP servers with content negotiation
- **SSE**: Event stream processing with reconnection handling
- **TCP Binary**: Custom binary protocol with framing

### Protocol Auto-Detection

The system provides 98% accuracy in automatic protocol detection:

```yaml
# Router configuration for auto-detection
direct_clients:
  enabled: true
  auto_detection:
    enabled: true
    timeout: "1s"
    fallback_strategy: "gateway"
```

### Environment Variables for Universal Protocol Support

```bash
# Universal Protocol Features
export MCP_UNIVERSAL_PROTOCOLS_ENABLED="true"
export MCP_PROTOCOL_AUTO_DETECT="true"
export MCP_CROSS_PROTOCOL_LOAD_BALANCE="true"

# Router Direct Protocol Configuration
export MCP_DIRECT_STDIO_ENABLED="true"
export MCP_DIRECT_WEBSOCKET_ENABLED="true" 
export MCP_DIRECT_HTTP_ENABLED="true"
export MCP_DIRECT_SSE_ENABLED="true"
export MCP_DIRECT_TCP_BINARY_ENABLED="true"

# Gateway Backend Protocol Configuration
export MCP_GATEWAY_STDIO_BACKENDS="true"
export MCP_GATEWAY_WEBSOCKET_BACKENDS="true"
export MCP_GATEWAY_HTTP_BACKENDS="true"
export MCP_GATEWAY_SSE_BACKENDS="true"
export MCP_GATEWAY_TCP_BINARY_BACKENDS="true"

# Performance Optimization
export MCP_CONNECTION_POOLING_ENABLED="true"
export MCP_MEMORY_OPTIMIZATION_ENABLED="true"
export MCP_PREDICTIVE_HEALTH_MONITORING="true"

# Service Discovery
export MCP_SERVICE_DISCOVERY_KUBERNETES="true"
export MCP_SERVICE_DISCOVERY_CONSUL="false"
export MCP_SERVICE_DISCOVERY_STATIC="true"
```

### Docker Compose Universal Protocol Example

```yaml
version: '3.8'

services:
  # MCP Gateway with Universal Protocol Support
  mcp-gateway:
    image: ghcr.io/actual-software/mcp-bridge-gateway:latest
    ports:
      - "8443:8443"    # WebSocket/HTTP frontend
      - "8444:8444"    # TCP Binary frontend
      - "9090:9090"    # Metrics endpoint
    volumes:
      - /tmp/mcp-gateway.sock:/tmp/mcp-gateway.sock  # stdio socket
      - gateway-config:/etc/mcp-gateway
      - gateway-tls:/tls
    environment:
      # Universal Protocol Configuration
      - MCP_UNIVERSAL_PROTOCOLS_ENABLED=true
      - MCP_PROTOCOL_AUTO_DETECT=true
      - MCP_CROSS_PROTOCOL_LOAD_BALANCE=true
      
      # Backend Protocol Support
      - MCP_GATEWAY_STDIO_BACKENDS=true
      - MCP_GATEWAY_WEBSOCKET_BACKENDS=true
      - MCP_GATEWAY_HTTP_BACKENDS=true
      - MCP_GATEWAY_SSE_BACKENDS=true
      - MCP_GATEWAY_TCP_BINARY_BACKENDS=true
      
      # Performance Features
      - MCP_CONNECTION_POOLING_ENABLED=true
      - MCP_MEMORY_OPTIMIZATION_ENABLED=true
      - MCP_PREDICTIVE_HEALTH_MONITORING=true
      
      # Service Discovery
      - MCP_SERVICE_DISCOVERY_KUBERNETES=true
      - MCP_SERVICE_DISCOVERY_CONSUL=false
      - MCP_SERVICE_DISCOVERY_STATIC=true
      
      # Redis Configuration
      - REDIS_ADDR=redis:6379
      - REDIS_PASSWORD=${REDIS_PASSWORD}
    depends_on:
      redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9090/health/protocols"]
      interval: 10s
      timeout: 5s
      retries: 3
    restart: unless-stopped

  # MCP Router with Direct Protocol Support  
  mcp-router:
    image: ghcr.io/actual-software/mcp-bridge-router:latest
    ports:
      - "9091:9091"    # Metrics and management
    volumes:
      - router-config:/etc/mcp-router
      - /tmp/mcp-servers:/tmp/mcp-servers  # stdio server sockets
    environment:
      # Direct Protocol Configuration
      - MCP_DIRECT_CLIENTS_ENABLED=true
      - MCP_DIRECT_STDIO_ENABLED=true
      - MCP_DIRECT_WEBSOCKET_ENABLED=true
      - MCP_DIRECT_HTTP_ENABLED=true
      - MCP_DIRECT_SSE_ENABLED=true
      - MCP_DIRECT_TCP_BINARY_ENABLED=true
      
      # Auto-detection and Optimization
      - MCP_AUTO_DETECT_PROTOCOLS=true
      - MCP_CONNECTION_POOLING_ENABLED=true
      - MCP_MEMORY_OPTIMIZATION_ENABLED=true
      
      # Gateway Fallback Configuration
      - MCP_GATEWAY_URL=wss://mcp-gateway:8443
      - MCP_AUTH_TOKEN=${MCP_AUTH_TOKEN}
      
      # Observability
      - MCP_OBSERVABILITY_ENABLED=true
      - MCP_METRICS_ENABLED=true
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9091/health/direct-clients"]
      interval: 10s
      timeout: 5s
      retries: 3
    restart: unless-stopped

  # Example MCP Servers with Different Protocols
  stdio-weather-server:
    image: python:3.11-slim
    volumes:
      - ./examples:/app
      - /tmp/mcp-servers:/tmp/mcp-servers
    working_dir: /app
    command: python weather_server.py --socket /tmp/mcp-servers/weather.sock
    restart: unless-stopped

  websocket-data-server:
    image: node:18-alpine
    ports:
      - "8080:8080"
    volumes:
      - ./examples:/app
    working_dir: /app
    command: node websocket_data_server.js --port 8080
    restart: unless-stopped

  http-api-server:
    image: python:3.11-slim
    ports:
      - "8081:8081"
    volumes:
      - ./examples:/app
    working_dir: /app
    command: python http_api_server.py --port 8081
    restart: unless-stopped

volumes:
  gateway-config:
  gateway-tls:
  router-config:
  redis_data:
```

## Architecture Overview

### Universal Protocol Architecture

```mermaid
graph LR
    subgraph "Client"
        CLI[Claude CLI<br/>━━━━━━━━<br/>stdio]
    end

    subgraph "Router"
        Router[MCP Router<br/>━━━━━━━━<br/>Direct Protocols<br/>• stdio/ws/http<br/>• WebSocket/SSE<br/>• TCP Binary<br/>━━━━━━━━<br/>98% Auto-Detection<br/>65% Latency Reduction]
    end

    subgraph "Gateway"
        Gateway[MCP Gateway<br/>━━━━━━━━<br/>Universal Backend<br/>• stdio/ws/http/sse<br/>• TCP Binary/WebSocket<br/>━━━━━━━━<br/>Cross-Protocol Load Bal.<br/>Service Discovery<br/>Predictive Monitoring]
    end

    subgraph "Backends"
        Servers[MCP Servers<br/>━━━━━━━━<br/>Any Protocol<br/>Multiple Protocols<br/>Supported]
    end

    CLI -->|stdio| Router
    Router -->|Multiple Protocols| Gateway
    Gateway -->|Protocol-Aware| Servers

    style CLI fill:#e1f5ff,stroke:#0066cc,stroke-width:2px
    style Router fill:#fff4e1,stroke:#ff9900,stroke-width:2px
    style Gateway fill:#e1ffe1,stroke:#00cc66,stroke-width:2px
    style Servers fill:#ffe1f5,stroke:#cc0099,stroke-width:2px
```

**Flow Options:**
1. **Direct Connection** (Router → MCP Server): 65% latency improvement
2. **Gateway Connection** (Router → Gateway → MCP Server): Universal protocol support
3. **Hybrid Mode**: Automatic selection based on server availability and protocol support

## Quick Start

### Development Environment
```bash
# One-click setup for development
./scripts/quickstart.sh
```

### Production Installation
```bash
# Install local router with secure storage setup
curl -sSL https://raw.githubusercontent.com/actual-software/mcp-bridge/main/services/router/install.sh | bash

# Configure secure token storage
mcp-router setup

# Deploy gateway to Kubernetes
kubectl apply -k mcp-k8s-manifests/
```

## Router Installation

### Automated Installation (Recommended)

```bash
# Install with automatic platform detection and secure storage setup
curl -sSL https://raw.githubusercontent.com/actual-software/mcp-bridge/main/services/router/install.sh | bash

# The installer will:
# - Detect your platform (macOS/Windows/Linux)
# - Download the appropriate binary
# - Set up secure token storage
# - Install shell completions
# - Create initial configuration
```

### Manual Installation

#### Download Pre-built Binary
```bash
# macOS (Intel)
curl -L https://github.com/actual-software/mcp-bridge/releases/latest/download/mcp-router-darwin-amd64 -o mcp-router

# macOS (Apple Silicon)
curl -L https://github.com/actual-software/mcp-bridge/releases/latest/download/mcp-router-darwin-arm64 -o mcp-router

# Linux (x86_64)
curl -L https://github.com/actual-software/mcp-bridge/releases/latest/download/mcp-router-linux-amd64 -o mcp-router

# Windows
curl -L https://github.com/actual-software/mcp-bridge/releases/latest/download/mcp-router-windows-amd64.exe -o mcp-router.exe

# Make executable and install
chmod +x mcp-router
sudo mv mcp-router /usr/local/bin/
```

#### Build from Source
```bash
cd services/router
make build
sudo make install

# Install shell completions
make install-completions
```

### Secure Configuration

#### Interactive Setup (Recommended)
```bash
# Run interactive setup wizard
mcp-router setup

# This will:
# - Create configuration directory
# - Set up secure token storage in platform keychain
# - Configure gateway connection
# - Test the connection
```

#### Manual Configuration

1. Create configuration directory:
   ```bash
   mkdir -p ~/.config/claude-cli
   ```

2. Store authentication token securely:
   ```bash
   # Store token in platform keychain (recommended)
   mcp-router token set --name gateway-token
   # Enter token when prompted
   ```

3. Create configuration file:
   ```yaml
   # ~/.config/claude-cli/mcp-router.yaml
   version: 1
   
   gateway:
     url: wss://mcp-gateway.your-domain.com
     
     # Authentication options
     auth:
       # Option 1: Bearer token with secure storage
       type: bearer
       token_secure_key: "gateway-token"
       
       # Option 2: OAuth2
       # type: oauth2
       # client_id: "your-client-id"
       # client_secret_secure_key: "oauth-secret"
       # token_endpoint: "https://auth.example.com/token"
       # scopes: ["mcp:read", "mcp:write"]
       
       # Option 3: mTLS
       # type: mtls
       
     # Connection settings
     connection:
       timeout_ms: 5000
       keepalive_interval_ms: 30000
       reconnect:
         initial_delay_ms: 1000
         max_delay_ms: 60000
         max_attempts: -1  # Infinite retries
       
       # Connection pooling (for high throughput)
       pool:
         enabled: true
         min_size: 2
         max_size: 10
         max_idle_time_ms: 300000
         health_check_interval_ms: 30000
     
     # TLS configuration
     tls:
       verify: true
       ca_file: "/etc/ssl/certs/ca-certificates.crt"
       # For mTLS:
       # client_cert: "/path/to/client.crt"
       # client_key: "/path/to/client.key"
       min_version: "1.3"
       cipher_suites:
         - TLS_AES_128_GCM_SHA256
         - TLS_AES_256_GCM_SHA384
         - TLS_CHACHA20_POLY1305_SHA256
   
   # Local settings
   local:
     read_buffer_size: 65536
     write_buffer_size: 65536
     request_timeout_ms: 30000
     max_concurrent_requests: 100
     
     # Rate limiting
     rate_limit:
       enabled: true
       requests_per_sec: 100
       burst: 200
   
   # Logging
   logging:
     level: info  # debug, info, warn, error
     format: json # json, text
     output: stderr
     include_caller: false
   
   # Metrics
   metrics:
     enabled: true
     endpoint: "localhost:9091"
     labels:
       environment: "production"
       region: "us-east-1"
   
   # Advanced settings
   advanced:
     # Request deduplication
     deduplication:
       enabled: true
       cache_size: 1000
       ttl_seconds: 60
     
     # Circuit breaker
     circuit_breaker:
       failure_threshold: 5
       success_threshold: 2
       timeout_seconds: 30
     
     # Binary protocol (for performance)
     protocol: "websocket"  # websocket, tcp
     # For TCP:
     # protocol: "tcp"
     # tcp_address: "gateway.example.com:8443"
   ```

## Gateway Deployment

### 1. Install Cert-Manager (for TLS)

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml
```

### 2. Create Namespaces

```bash
kubectl apply -f mcp-k8s-manifests/gateway/namespace.yaml
kubectl apply -f mcp-k8s-manifests/mcp-servers/namespace.yaml
```

### 3. Configure Secrets

```bash
# Generate secure passwords
JWT_SECRET=$(openssl rand -base64 32)
REDIS_PASSWORD=$(openssl rand -base64 32)
OAUTH_CLIENT_SECRET=$(openssl rand -base64 32)

# Create Kubernetes secrets
kubectl create secret generic mcp-gateway-auth \
  --namespace mcp-system \
  --from-literal=jwt-secret="$JWT_SECRET" \
  --from-literal=redis-password="$REDIS_PASSWORD" \
  --from-literal=oauth-client-secret="$OAUTH_CLIENT_SECRET"

# Create TLS secret (production)
kubectl create secret tls mcp-gateway-tls \
  --namespace mcp-system \
  --cert=path/to/tls.crt \
  --key=path/to/tls.key
```

## Redis Setup

### Development (Single Instance)
```bash
kubectl apply -f mcp-k8s-manifests/gateway/redis-dev.yaml
```

### Production (High Availability)

See [Redis Production Guide](docs/deployment/redis.md) for detailed setup.

#### Option 1: Redis Sentinel
```bash
# Deploy Redis with Sentinel for automatic failover
kubectl apply -f mcp-k8s-manifests/gateway/redis-sentinel.yaml
```

#### Option 2: Redis Cluster
```bash
# Deploy Redis Cluster for horizontal scaling
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install redis-cluster bitnami/redis-cluster \
  --namespace mcp-system \
  --set auth.password="$REDIS_PASSWORD" \
  --set cluster.nodes=6 \
  --set cluster.replicas=1
```

### 4. Deploy Gateway

#### Production Deployment with Universal Protocol Support

```bash
# Review configuration with universal protocol features
kubectl kustomize mcp-k8s-manifests/gateway/overlays/production

# Deploy with enhanced capabilities
kubectl apply -k mcp-k8s-manifests/gateway/overlays/production
```

#### Universal Protocol Configuration

```yaml
# mcp-k8s-manifests/gateway/overlays/production/gateway-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-gateway-config
  namespace: mcp-system
data:
  config.yaml: |
    version: 1
    
    # Universal Protocol Server Configuration
    server:
      host: 0.0.0.0
      port: 8443
      protocol: universal  # Supports all frontend protocols
      tcp_port: 8444
      tcp_health_port: 9002
      stdio_socket: "/tmp/mcp-gateway.sock"
      http_port: 8443  # Shared with WebSocket
      max_connections: 10000
      max_connections_per_ip: 100
      
      # Protocol-specific settings
      protocols:
        websocket:
          enabled: true
          path: "/ws"
          compression: true
        tcp_binary:
          enabled: true
          framing: "length_prefixed"
        stdio:
          enabled: true
          socket_permissions: "0660"
        http:
          enabled: true
          api_prefix: "/api/v1"
          cors_enabled: true
        sse:
          enabled: true
          path: "/events"
          keepalive_interval: "30s"
      
      tls:
        enabled: true
        cert_file: /tls/tls.crt
        key_file: /tls/tls.key
        min_version: "1.3"
        client_auth: "request"
    
    # Universal Backend Support
    backends:
      stdio:
        enabled: true
        socket_discovery: true
        socket_paths: ["/tmp/mcp-servers"]
        timeout: "5s"
      websocket:
        enabled: true
        auto_discovery: true
        health_check_path: "/health"
        timeout: "10s"
      http:
        enabled: true
        auto_discovery: true
        content_types: ["application/json", "application/x-ndjson"]
        timeout: "30s"
      sse:
        enabled: true
        reconnect_interval: "5s"
        max_retries: 3
      tcp_binary:
        enabled: true
        framing: "length_prefixed"
        timeout: "10s"
    
    # Cross-Protocol Load Balancing
    load_balancing:
      strategy: "cross_protocol"  # cross_protocol, protocol_specific, least_conn
      protocol_weights:
        stdio: 1.5      # Prefer stdio for performance
        websocket: 1.2  # Good balance
        http: 1.0       # Standard weight
        sse: 0.8        # Lower priority
        tcp_binary: 1.3 # High performance
      health_aware: true
      fallback_enabled: true
    
    # Protocol Auto-Detection
    protocol_detection:
      enabled: true
      accuracy_target: 0.98
      timeout: "1s"
      cache_duration: "5m"
      fallback_protocol: "http"
    
    # Predictive Health Monitoring
    health_monitoring:
      predictive_enabled: true
      ml_model: "anomaly_detection"
      health_check_interval: "10s"
      failure_threshold: 3
      recovery_threshold: 2
      protocol_specific_checks: true
    
    auth:
      type: bearer
      per_message_auth: true
      per_message_cache: 300
    
    sessions:
      provider: redis
      ttl: 3600
      redis:
        addresses:
          - redis-sentinel-0.redis-sentinel:26379
          - redis-sentinel-1.redis-sentinel:26379
          - redis-sentinel-2.redis-sentinel:26379
        sentinel_master_name: mymaster
        password: ${REDIS_PASSWORD}
        pool_size: 100
    
    rate_limit:
      enabled: true
      provider: redis
      requests_per_sec: 100
      burst: 200
      per_protocol: true
    
    circuit_breaker:
      failure_threshold: 5
      success_threshold: 2
      timeout_seconds: 30
      max_requests: 100
      interval_seconds: 60
      per_protocol: true
    
    # Enhanced Service Discovery
    discovery:
      providers:
        kubernetes:
          enabled: true
          namespace_pattern: "mcp-servers-.*"
          service_labels:
            mcp-enabled: "true"
          protocol_detection: true
        consul:
          enabled: false
        static:
          enabled: true
          servers: []
      refresh_rate: 30s
      protocol_detection: true
    
    routing:
      strategy: cross_protocol_least_conn
      health_check_interval: 10s
      protocol_preference_order: ["stdio", "tcp_binary", "websocket", "http", "sse"]
    
    metrics:
      enabled: true
      endpoint: "0.0.0.0:9090"
      path: "/metrics"
      protocol_breakdown: true
      performance_metrics: true
    
    logging:
      level: info
      format: json
      protocol_context: true
    
    tracing:
      enabled: true
      service_name: mcp-gateway-universal
      agent_host: jaeger-agent.monitoring
      agent_port: 6831
      protocol_spans: true
```

### 5. Deploy MCP Servers

```bash
kubectl apply -f mcp-k8s-manifests/mcp-servers/
```

### 6. Configure Ingress (Optional)

If using an Ingress controller:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-gateway
  namespace: mcp-system
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - mcp-gateway.your-domain.com
    secretName: mcp-gateway-tls
  rules:
  - host: mcp-gateway.your-domain.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: mcp-gateway
            port:
              number: 8443
```

## Verification

### 1. Check Gateway Health with Universal Protocol Support

```bash
# Check pods
kubectl get pods -n mcp-system

# Check gateway logs with protocol context
kubectl logs -n mcp-system deployment/mcp-gateway | jq -r '.protocol'

# Universal health check
curl -k https://mcp-gateway.your-domain.com/health

# Protocol-specific health checks
curl -k https://mcp-gateway.your-domain.com/health/protocols
curl -k https://mcp-gateway.your-domain.com/health/stdio
curl -k https://mcp-gateway.your-domain.com/health/websocket
curl -k https://mcp-gateway.your-domain.com/health/http
curl -k https://mcp-gateway.your-domain.com/health/sse
curl -k https://mcp-gateway.your-domain.com/health/load-balancer
curl -k https://mcp-gateway.your-domain.com/health/discovery

# Cross-protocol load balancer status
curl -k https://mcp-gateway.your-domain.com/api/v1/load-balancer/status

# Protocol detection verification
curl -k https://mcp-gateway.your-domain.com/api/v1/protocols/detect
```

### 2. Test Router with Direct Protocol Support

```bash
# Check version
mcp-router --version

# Test direct protocol connections
mcp-router --log-level debug

# Check direct protocol health
curl http://localhost:9091/health/direct-clients
curl http://localhost:9091/health/protocol-detection

# Verify protocol auto-detection
curl http://localhost:9091/api/v1/protocols/detect -X POST -d '{"test_endpoint": "ws://localhost:8080"}'

# Check performance optimization status
curl http://localhost:9091/api/v1/performance
```

### 3. Test Universal Protocol End-to-End

```bash
# Test stdio connection (direct)
echo '{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}' | mcp-router --direct-mode stdio

# Test WebSocket connection (direct)
mcp-router test --protocol websocket --endpoint ws://localhost:8080

# Test HTTP connection (direct)  
mcp-router test --protocol http --endpoint http://localhost:8081

# Test gateway fallback
MCP_GATEWAY_URL=wss://mcp-gateway.your-domain.com mcp-router test --fallback-mode

# Verify cross-protocol load balancing
for i in {1..10}; do
  curl -X POST https://mcp-gateway.your-domain.com/api/v1/execute \
    -H "Authorization: Bearer $MCP_AUTH_TOKEN" \
    -H "X-Load-Balance: cross-protocol" \
    -d '{"jsonrpc":"2.0","method":"ping","id":'$i'}'
done
```

## TLS and Security

### TLS Configuration

#### Gateway TLS Setup

1. **Using cert-manager (Recommended)**
```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: mcp-gateway-tls
  namespace: mcp-system
spec:
  secretName: mcp-gateway-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - mcp-gateway.your-domain.com
```

2. **Manual Certificate Setup**
```bash
# Create TLS secret from existing certificates
kubectl create secret tls mcp-gateway-tls \
  --cert=path/to/tls.crt \
  --key=path/to/tls.key \
  -n mcp-system
```

3. **Configure Gateway for TLS**
```yaml
server:
  tls:
    enabled: true
    cert_file: /tls/tls.crt
    key_file: /tls/tls.key
    min_version: "1.3"  # Enforce TLS 1.3
    cipher_suites:      # Optional: restrict cipher suites
      - TLS_AES_128_GCM_SHA256
      - TLS_AES_256_GCM_SHA384
      - TLS_CHACHA20_POLY1305_SHA256
```

### Security Headers

The gateway automatically adds security headers to all HTTP responses:

- `Strict-Transport-Security`: Enforces HTTPS (HSTS)
- `X-Content-Type-Options`: Prevents MIME sniffing
- `X-Frame-Options`: Prevents clickjacking
- `X-XSS-Protection`: Legacy XSS protection
- `Content-Security-Policy`: Restricts resource loading
- `Referrer-Policy`: Controls referrer information

### Request Size Limits

Enforced limits to prevent DoS attacks:

- HTTP request body: 1MB (configurable)
- HTTP headers: 1MB (configurable)
- WebSocket messages: 10MB (configurable)
- Binary protocol payload: 10MB (fixed)

Configure in gateway:
```yaml
server:
  security:
    max_request_size: 1048576      # 1MB
    max_header_size: 1048576       # 1MB
    max_message_size: 10485760     # 10MB
```

### Input Validation

All user inputs are validated:

- **Request IDs**: Max 255 chars, alphanumeric + hyphens/underscores
- **Methods**: Max 255 chars, alphanumeric + dots/slashes/hyphens/underscores
- **Namespaces**: Max 255 chars, alphanumeric + dots/hyphens/underscores
- **Tokens**: Max 4096 chars, sanitized for injection
- **UTF-8 enforcement**: All strings must be valid UTF-8

### Rate Limiting

Multiple layers of rate limiting:

1. **Per-IP Connection Limits**
```yaml
server:
  max_connections_per_ip: 100  # Default
```

2. **Per-User Request Rate Limiting**
```yaml
rate_limit:
  enabled: true
  provider: redis
  requests_per_sec: 100
  burst: 200
```

3. **Circuit Breakers** for backend protection

### Network Policies

Apply Kubernetes NetworkPolicies for defense-in-depth:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: mcp-gateway-network-policy
  namespace: mcp-system
spec:
  podSelector:
    matchLabels:
      app: mcp-gateway
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: ingress-nginx
    ports:
    - protocol: TCP
      port: 8443
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          mcp-servers: "true"
  - to:
    - namespaceSelector:
        matchLabels:
          name: redis
  - ports:
    - protocol: TCP
      port: 53  # DNS
```

### mTLS Configuration

For mutual TLS authentication:

```yaml
server:
  tls:
    client_auth: require
    ca_file: /tls/ca.crt

# Mount CA certificate
volumes:
- name: client-ca
  secret:
    secretName: mcp-client-ca
volumeMounts:
- name: client-ca
  mountPath: /tls/ca.crt
  subPath: ca.crt
```

### Security Scanning

1. **Image Scanning**
```bash
# Scan gateway image for vulnerabilities
trivy image ghcr.io/actual-software/mcp-bridge-gateway:latest
```

2. **Runtime Security**
```yaml
# Pod Security Policy
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: mcp-gateway-psp
spec:
  privileged: false
  allowPrivilegeEscalation: false
  requiredDropCapabilities:
  - ALL
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser:
    rule: MustRunAsNonRoot
```

### security.txt Endpoint

The gateway serves security.txt at:
- `/.well-known/security.txt`
- `/security.txt`

Configure contact information:
```yaml
security:
  security_txt:
    enabled: true
    contact: "security@your-domain.com"
    expires: "2025-12-31T23:59:59Z"
    policy: "https://your-domain.com/security-policy"
```

## Monitoring

### Universal Protocol Metrics

The gateway exposes comprehensive protocol metrics at `:9090/metrics`. Key universal protocol metrics:

**Protocol-Specific Connection Metrics:**
```prometheus
# Active connections by protocol
mcp_gateway_connections_total{protocol="stdio",state="active"}
mcp_gateway_connections_total{protocol="websocket",state="active"}
mcp_gateway_connections_total{protocol="http",state="active"}
mcp_gateway_connections_total{protocol="sse",state="active"}
mcp_gateway_connections_total{protocol="tcp_binary",state="active"}

# Request metrics with protocol breakdown
mcp_gateway_requests_total{method="POST",status="200",protocol="stdio"}
mcp_gateway_requests_total{method="POST",status="200",protocol="websocket"}
mcp_gateway_request_duration_seconds{protocol="stdio",quantile="0.95"}
mcp_gateway_request_duration_seconds{protocol="websocket",quantile="0.95"}
```

**Cross-Protocol Load Balancing Metrics:**
```prometheus
# Load balancer decision metrics
mcp_gateway_load_balancer_requests_total{strategy="cross_protocol",selected_protocol="stdio"}
mcp_gateway_load_balancer_latency_seconds{backend_protocol="websocket"}
mcp_gateway_protocol_weight{protocol="stdio",current_weight="1.5"}

# Protocol conversion metrics
mcp_gateway_protocol_conversion_total{from="websocket",to="stdio",result="success"}
mcp_gateway_protocol_conversion_duration_seconds{from="http",to="tcp_binary"}
```

**Protocol Auto-Detection Metrics:**
```prometheus
# Detection accuracy and performance
mcp_gateway_protocol_detection_total{protocol="stdio",result="success"}
mcp_gateway_protocol_detection_accuracy{protocol="websocket"}
mcp_gateway_protocol_detection_duration_seconds{protocol="http"}

# Detection cache metrics
mcp_gateway_protocol_detection_cache_hits_total
mcp_gateway_protocol_detection_cache_misses_total
```

**Predictive Health Monitoring Metrics:**
```prometheus
# Predictive health scores
mcp_gateway_predictive_health_score{protocol="stdio",server="weather-server"}
mcp_gateway_anomaly_detection_alerts_total{protocol="websocket",severity="warning"}
mcp_gateway_health_prediction_accuracy{model="anomaly_detection"}

# Traditional health metrics by protocol
mcp_gateway_health_checks_total{protocol="stdio",result="healthy"}
mcp_gateway_health_checks_total{protocol="websocket",result="unhealthy"}
```

**Router Direct Protocol Metrics:**
```prometheus
# Router direct connection metrics (from :9091/metrics)
mcp_router_direct_connections_total{protocol="stdio",state="active"}
mcp_router_direct_requests_total{protocol="websocket",result="success"}
mcp_router_direct_latency_seconds{protocol="stdio",quantile="0.95"}

# Connection pooling metrics
mcp_router_connection_pool_size{protocol="websocket"}
mcp_router_connection_pool_hits_total{protocol="stdio"}
mcp_router_connection_pool_misses_total{protocol="websocket"}

# Memory optimization metrics
mcp_router_memory_optimization_savings_bytes{component="object_pool"}
mcp_router_gc_optimization_cycles_total
```

### Enhanced Grafana Dashboards

Import the enhanced dashboards:
- `monitoring/grafana-universal-protocols-dashboard.json` - Universal protocol overview
- `monitoring/grafana-cross-protocol-load-balancer-dashboard.json` - Load balancing metrics
- `monitoring/grafana-protocol-detection-dashboard.json` - Auto-detection performance
- `monitoring/grafana-predictive-health-dashboard.json` - Health monitoring ML metrics

### Protocol-Specific Alerts

Enhanced Prometheus alerts configured in `monitoring/service-monitor.yaml`:

```yaml
# Protocol-specific availability alerts
- alert: ProtocolUnavailable
  expr: mcp_gateway_connections_total{state="active"} == 0
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "{{ $labels.protocol }} protocol unavailable"

# Cross-protocol load balancer alerts
- alert: LoadBalancerImbalance
  expr: stddev by(instance) (mcp_gateway_load_balancer_requests_total) > 100
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Cross-protocol load balancer showing significant imbalance"

# Protocol detection accuracy alerts
- alert: ProtocolDetectionAccuracyLow
  expr: mcp_gateway_protocol_detection_accuracy < 0.95
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "Protocol detection accuracy below 95%: {{ $value }}"

# Predictive health alerts
- alert: PredictiveHealthAnomalyDetected
  expr: mcp_gateway_anomaly_detection_alerts_total > 0
  for: 1m
  labels:
    severity: warning
  annotations:
    summary: "Predictive health monitoring detected anomaly in {{ $labels.protocol }}"
```

## Troubleshooting

### Universal Protocol Connection Issues

1. **Check protocol-specific gateway health:**
   ```bash
   # Overall health
   curl -v https://mcp-gateway.your-domain.com/health
   
   # Protocol-specific health checks
   curl https://mcp-gateway.your-domain.com/health/stdio
   curl https://mcp-gateway.your-domain.com/health/websocket  
   curl https://mcp-gateway.your-domain.com/health/http
   curl https://mcp-gateway.your-domain.com/health/sse
   curl https://mcp-gateway.your-domain.com/health/load-balancer
   
   # Service discovery health
   curl https://mcp-gateway.your-domain.com/health/discovery
   ```

2. **Verify router direct protocol connections:**
   ```bash
   # Check direct client status
   curl http://localhost:9091/health/direct-clients
   
   # Protocol detection status
   curl http://localhost:9091/health/protocol-detection
   
   # Performance optimization status
   curl http://localhost:9091/api/v1/performance
   ```

3. **Debug protocol auto-detection:**
   ```bash
   # Test protocol detection manually
   curl http://localhost:9091/api/v1/protocols/detect -X POST \
     -H "Content-Type: application/json" \
     -d '{"test_endpoints": ["ws://localhost:8080", "http://localhost:8081"]}'
   
   # Check detection cache
   curl http://localhost:9091/api/v1/protocols/cache
   ```

4. **Authentication token verification:**
   ```bash
   # Check token
   echo $MCP_AUTH_TOKEN
   
   # Decode JWT token
   echo $MCP_AUTH_TOKEN | cut -d. -f2 | base64 -d | jq
   ```

5. **Enhanced router debugging:**
   ```bash
   # Enable protocol-specific debug logging
   MCP_LOGGING_LEVEL=debug \
   MCP_PROTOCOL_DEBUG=true \
   MCP_DIRECT_CLIENTS_DEBUG=true \
   mcp-router 2>&1 | jq -r '.protocol, .message'
   ```

### Protocol-Specific Performance Issues

1. **Monitor cross-protocol load balancing:**
   ```bash
   # Check load balancer metrics
   curl -s http://localhost:9090/metrics | grep mcp_gateway_load_balancer
   
   # View protocol weights
   curl https://mcp-gateway.your-domain.com/api/v1/load-balancer/weights
   
   # Check protocol selection distribution
   curl -s http://localhost:9090/metrics | grep selected_protocol
   ```

2. **Direct connection performance:**
   ```bash
   # Router connection pool status
   curl -s http://localhost:9091/metrics | grep connection_pool
   
   # Memory optimization metrics
   curl -s http://localhost:9091/metrics | grep memory_optimization
   
   # Protocol latency comparison
   curl -s http://localhost:9091/api/v1/performance/protocols
   ```

3. **Scale gateway with protocol awareness:**
   ```bash
   # Scale based on protocol load
   kubectl scale deployment mcp-gateway -n mcp-system --replicas=5
   
   # Check protocol distribution across replicas
   kubectl logs -n mcp-system -l app=mcp-gateway | grep protocol_assignment
   ```

### Protocol Detection Issues

1. **Debug auto-detection failures:**
   ```bash
   # Check detection accuracy metrics
   curl -s http://localhost:9090/metrics | grep protocol_detection_accuracy
   
   # View failed detections
   curl http://localhost:9091/api/v1/protocols/detect/failures
   
   # Test manual protocol override
   curl -X POST https://mcp-gateway.your-domain.com/api/v1/execute \
     -H "Authorization: Bearer $MCP_AUTH_TOKEN" \
     -H "X-Force-Protocol: stdio" \
     -d '{"jsonrpc":"2.0","method":"ping","id":1}'
   ```

2. **Protocol conversion debugging:**
   ```bash
   # Check conversion metrics
   curl -s http://localhost:9090/metrics | grep protocol_conversion
   
   # View conversion failures
   curl https://mcp-gateway.your-domain.com/api/v1/protocols/conversions/failures
   ```

### Predictive Health Monitoring Issues

1. **Debug anomaly detection:**
   ```bash
   # Check predictive health scores
   curl -s http://localhost:9090/metrics | grep predictive_health_score
   
   # View anomaly alerts
   curl https://mcp-gateway.your-domain.com/api/v1/health/anomalies
   
   # Check ML model performance
   curl https://mcp-gateway.your-domain.com/api/v1/health/ml-model/status
   ```

2. **Traditional vs predictive health comparison:**
   ```bash
   # Traditional health check results
   curl -s http://localhost:9090/metrics | grep health_checks_total
   
   # Predictive health accuracy
   curl https://mcp-gateway.your-domain.com/api/v1/health/prediction-accuracy
   ```

### Common Protocol-Specific Errors

| Error | Protocol | Cause | Solution |
|-------|----------|-------|----------|
| `stdio: no such file or directory` | stdio | Socket not found | Check socket path configuration |
| `websocket: connection refused` | WebSocket | Server down | Verify WebSocket server status |
| `http: 404 not found` | HTTP | Wrong endpoint | Check HTTP API path configuration |
| `sse: stream closed` | SSE | Connection dropped | Enable SSE reconnection |
| `tcp_binary: protocol mismatch` | TCP Binary | Frame error | Verify binary protocol version |
| `protocol_detection_failed` | All | Auto-detect timeout | Increase detection timeout or specify protocol |
| `cross_protocol_load_balance_failed` | All | No healthy backends | Check backend health across all protocols |

### Advanced Debugging

1. **Enable comprehensive protocol tracing:**
   ```bash
   # Gateway with protocol tracing
   kubectl set env deployment/mcp-gateway -n mcp-system \
     MCP_PROTOCOL_TRACING=true \
     MCP_TRACE_LEVEL=detailed
   
   # Router with direct protocol tracing  
   MCP_DIRECT_PROTOCOL_TRACING=true \
   MCP_PROTOCOL_CONVERSION_TRACING=true \
   mcp-router --log-level trace
   ```

2. **Protocol-specific health check scripts:**
   ```bash
   # Stdio health check
   echo '{"jsonrpc":"2.0","method":"ping","id":1}' | \
     socat - UNIX-CONNECT:/tmp/mcp-servers/test.sock
   
   # WebSocket health check
   wscat -c ws://localhost:8080 -x '{"jsonrpc":"2.0","method":"ping","id":1}'
   
   # HTTP health check
   curl -X POST http://localhost:8081/mcp -d '{"jsonrpc":"2.0","method":"ping","id":1}'
   ```

## Production Considerations

### Universal Protocol High Availability

**Gateway High Availability:**
- Deploy at least 3 gateway replicas with universal protocol support
- Use Redis Sentinel or Redis Cluster for session state and rate limiting
- Configure pod anti-affinity rules for protocol load balancing
- Enable cross-protocol load balancing with health-aware routing
- Implement protocol-specific circuit breakers

**Router High Availability:**
- Deploy router instances with direct protocol client capabilities
- Configure multiple gateway endpoints for failover
- Enable connection pooling with health checks across all protocols
- Use protocol auto-detection with fallback strategies

### Universal Protocol Security

**Protocol-Level Security:**
- Use TLS 1.3 for WebSocket and HTTP protocols
- Implement mTLS for TCP Binary connections
- Secure stdio sockets with proper file permissions (0660)
- Enable per-protocol rate limiting and authentication
- Regular security scanning of protocol implementations

**Enhanced Authentication:**
- Use strong JWT secrets with protocol-specific scopes
- Enable per-message authentication across all protocols
- Implement OAuth2 with automatic token refresh
- Support mTLS for high-security environments
- Protocol-aware authentication caching

### Universal Protocol Performance

**Cross-Protocol Optimization:**
- Monitor protocol-specific connection counts and latency
- Tune cross-protocol load balancing weights based on performance
- Enable memory optimization with object pooling (40% reduction)
- Configure protocol-specific connection pool settings
- Use direct connections for 65% latency improvement where possible

**Performance Monitoring:**
- Track protocol detection accuracy (target: 98%)
- Monitor cross-protocol conversion overhead
- Analyze predictive health monitoring effectiveness
- Measure connection pooling efficiency (5.8μs/op retrieval)
- Geographic distribution with protocol awareness

### Protocol-Aware Backup and Recovery

**Stateless Design with Protocol Context:**
- Gateway maintains no persistent state across protocols
- Redis used for temporary session state and protocol caching
- Protocol detection cache can be rebuilt automatically
- Cross-protocol load balancer state is recoverable

**MCP Server Protocol Considerations:**
- Each protocol type (stdio, WebSocket, HTTP, SSE, TCP Binary) handles its own persistence
- Protocol conversion is stateless and can be resumed immediately
- Service discovery state is rebuilt from Kubernetes/Consul sources
- Health monitoring history maintained in time-series databases

## Upgrading

### Universal Protocol Rolling Update

```bash
# Update gateway image with universal protocol support
kubectl set image deployment/mcp-gateway \
  gateway=ghcr.io/actual-software/mcp-bridge-gateway:v1.1.0 -n mcp-system

# Monitor rollout with protocol health checks
kubectl rollout status deployment/mcp-gateway -n mcp-system

# Verify all protocols are healthy after update
curl https://mcp-gateway.your-domain.com/health/protocols

# Update router image with direct protocol clients
kubectl set image deployment/mcp-router \
  router=ghcr.io/actual-software/mcp-bridge-router:v1.1.0 -n mcp-system
```

### Protocol-Safe Upgrade Process

**Pre-Upgrade Verification:**
```bash
# Check current protocol health
curl -s https://mcp-gateway.your-domain.com/health/protocols | jq

# Verify cross-protocol load balancer status
curl -s https://mcp-gateway.your-domain.com/api/v1/load-balancer/status

# Check protocol detection accuracy
curl -s http://localhost:9090/metrics | grep protocol_detection_accuracy
```

**During Upgrade:**
```bash
# Monitor protocol availability during rollout
watch 'curl -s https://mcp-gateway.your-domain.com/health/protocols | jq ".protocols[] | {name: .name, healthy: .healthy}"'

# Check for protocol conversion issues
kubectl logs -f deployment/mcp-gateway -n mcp-system | grep protocol_conversion
```

**Post-Upgrade Verification:**
```bash
# Verify all protocols restored
curl https://mcp-gateway.your-domain.com/health/protocols

# Test protocol auto-detection
curl http://localhost:9091/api/v1/protocols/detect -X POST \
  -d '{"test_endpoints": ["ws://localhost:8080", "http://localhost:8081"]}'

# Confirm cross-protocol load balancing
for i in {1..5}; do
  curl -X POST https://mcp-gateway.your-domain.com/api/v1/execute \
    -H "Authorization: Bearer $MCP_AUTH_TOKEN" \
    -H "X-Load-Balance: cross-protocol" \
    -d '{"jsonrpc":"2.0","method":"ping","id":'$i'}' | jq .protocol_used
done
```

### GitOps with Universal Protocol Configuration

```bash
# Apply ArgoCD application with protocol support
kubectl apply -f mcp-k8s-manifests/argocd-application.yaml

# Sync with protocol-aware health checks
argocd app sync mcp-gateway --prune

# Verify protocol configuration sync
argocd app diff mcp-gateway | grep -E "(protocol|cross_protocol|auto_detect)"
```

## Maintenance

### Universal Protocol Log Management

**Protocol-Aware Log Rotation:**
Logs include protocol context and are sent to stderr, managed by Kubernetes. Configure log retention with protocol awareness:

```yaml
# Enhanced logging configuration for protocol context
logging:
  driver: "json-file"
  options:
    max-size: "100m"
    max-file: "5"
    labels: "protocol,connection_type,performance_tier"
```

**Protocol-Specific Log Filtering:**
```bash
# Filter logs by protocol
kubectl logs deployment/mcp-gateway -n mcp-system | jq -r 'select(.protocol=="stdio")'
kubectl logs deployment/mcp-gateway -n mcp-system | jq -r 'select(.protocol=="websocket")'

# Protocol performance logs
kubectl logs deployment/mcp-router -n mcp-system | jq -r 'select(.component=="direct_clients")'
```

### Enhanced Metrics Retention

Configure Prometheus retention for universal protocol metrics:

```yaml
prometheus:
  prometheusSpec:
    retention: 30d
    retentionSize: 50GB
    # Enhanced storage for protocol metrics
    storageSpec:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: 100GB
    # Protocol-specific metric retention policies
    additionalScrapeConfigs: |
      - job_name: 'mcp-universal-protocols'
        static_configs:
          - targets: ['mcp-gateway:9090', 'mcp-router:9091']
        metric_relabel_configs:
          # Retain protocol-specific metrics longer
          - source_labels: [__name__]
            regex: 'mcp_(gateway|router)_protocol_.*'
            target_label: retention_policy
            replacement: 'extended'
```

### Comprehensive Health Checks

**Universal Protocol Health Endpoints:**
- **Gateway Universal Health**: `https://gateway-url/health`
- **Protocol-Specific Health**: 
  - `https://gateway-url/health/stdio`
  - `https://gateway-url/health/websocket`
  - `https://gateway-url/health/http`
  - `https://gateway-url/health/sse`
  - `https://gateway-url/health/load-balancer`
- **Router Direct Protocol Health**: `http://router-url:9091/health/direct-clients`
- **Protocol Detection Health**: `http://router-url:9091/health/protocol-detection`

**Enhanced Health Check Configuration:**
```yaml
server:
  protocol: universal  # Supports all protocols
  tcp_port: 8444
  tcp_health_port: 9002
  stdio_socket: "/tmp/mcp-gateway.sock"
  
  # Protocol-specific health checks
  health_checks:
    stdio:
      enabled: true
      socket_path: "/tmp/mcp-gateway.sock"
      timeout: "2s"
    websocket:
      enabled: true
      endpoint: "/health/ws"
      timeout: "5s"
    http:
      enabled: true
      endpoint: "/health/http"
      timeout: "3s"
    sse:
      enabled: true
      endpoint: "/health/sse"
      timeout: "5s"
    tcp_binary:
      enabled: true
      port: 9002
      timeout: "2s"
    cross_protocol_lb:
      enabled: true
      endpoint: "/health/load-balancer"
      timeout: "10s"
```

### Protocol Performance Maintenance

**Regular Performance Tuning:**
```bash
# Monitor protocol detection accuracy
curl -s http://localhost:9090/metrics | grep protocol_detection_accuracy

# Check cross-protocol load balancer efficiency
curl -s https://mcp-gateway.your-domain.com/api/v1/load-balancer/performance

# Review connection pooling efficiency
curl -s http://localhost:9091/metrics | grep connection_pool_efficiency

# Memory optimization status
curl -s http://localhost:9091/api/v1/performance/memory-optimization
```

**Automated Maintenance Tasks:**
```bash
#!/bin/bash
# Protocol maintenance script

# Clear protocol detection cache if accuracy drops
ACCURACY=$(curl -s http://localhost:9090/metrics | grep protocol_detection_accuracy | awk '{print $2}')
if (( $(echo "$ACCURACY < 0.95" | bc -l) )); then
  curl -X POST http://localhost:9091/api/v1/protocols/cache/clear
  echo "Protocol detection cache cleared due to low accuracy: $ACCURACY"
fi

# Rebalance cross-protocol weights based on performance
curl -X POST https://mcp-gateway.your-domain.com/api/v1/load-balancer/rebalance

# Optimize connection pools
curl -X POST http://localhost:9091/api/v1/connections/optimize

# Health check all protocols
for protocol in stdio websocket http sse tcp_binary; do
  if ! curl -s "https://mcp-gateway.your-domain.com/health/$protocol" | jq -e '.healthy'; then
    echo "WARNING: $protocol protocol unhealthy"
  fi
done
```

## Support

### Universal Protocol Support Resources

For protocol-specific issues:
1. **Check comprehensive health status:**
   ```bash
   # Universal protocol health
   curl https://mcp-gateway.your-domain.com/health/protocols
   
   # Direct protocol client status
   curl http://localhost:9091/health/direct-clients
   
   # Cross-protocol load balancer status
   curl https://mcp-gateway.your-domain.com/api/v1/load-balancer/status
   ```

2. **Review protocol-aware logs and metrics:**
   ```bash
   # Protocol-specific logs
   kubectl logs deployment/mcp-gateway -n mcp-system | jq -r 'select(.protocol=="stdio")'
   
   # Protocol performance metrics
   curl -s http://localhost:9090/metrics | grep -E "(protocol_detection|cross_protocol|direct_client)"
   ```

3. **Consult enhanced troubleshooting guides:**
   - [Universal Protocol Troubleshooting](#troubleshooting) (this document)
   - [Protocol Detection Guide](../troubleshooting.md#protocol-detection)
   - [Cross-Protocol Load Balancing Debug](../troubleshooting.md#load-balancing)
   - [Direct Connection Optimization](../troubleshooting.md#performance)

4. **Open GitHub issue with protocol context:**
   - Include protocol type and configuration
   - Provide auto-detection results and accuracy metrics
   - Share cross-protocol load balancing logs
   - Include performance optimization status

### Emergency Protocol Recovery

For protocol-specific emergencies:

1. **Protocol-aware service reset:**
   ```bash
   # Reset gateway with protocol state preservation
   kubectl scale deployment mcp-gateway -n mcp-system --replicas=0
   sleep 10
   kubectl scale deployment mcp-gateway -n mcp-system --replicas=3
   
   # Verify all protocols recover
   curl https://mcp-gateway.your-domain.com/health/protocols
   ```

2. **Protocol detection cache reset:**
   ```bash
   # Clear protocol detection cache if accuracy degrades
   curl -X POST http://localhost:9091/api/v1/protocols/cache/clear
   
   # Force protocol re-detection
   curl -X POST http://localhost:9091/api/v1/protocols/detect/refresh
   ```

3. **Cross-protocol load balancer reset:**
   ```bash
   # Reset load balancer weights to defaults
   curl -X POST https://mcp-gateway.your-domain.com/api/v1/load-balancer/reset
   
   # Rebalance based on current performance
   curl -X POST https://mcp-gateway.your-domain.com/api/v1/load-balancer/rebalance
   ```

4. **Redis state issues with protocol context:**
   ```bash
   # Restart Redis while preserving protocol session state
   kubectl rollout restart statefulset/redis -n mcp-system
   
   # Clear protocol-specific cached data if needed
   kubectl exec redis-0 -n mcp-system -- redis-cli FLUSHDB
   ```

5. **Failover with protocol preservation:**
   ```bash
   # Failover to backup cluster with protocol configuration sync
   kubectl config use-context backup-cluster
   kubectl apply -k mcp-k8s-manifests/gateway/overlays/production
   
   # Verify protocol parity
   curl https://backup-gateway.your-domain.com/health/protocols
   ```

### Community and Documentation

- **GitHub Repository**: [mcp-bridge](https://github.com/actual-software/mcp-bridge)
- **Protocol Documentation**: [docs/protocols/](../protocols/)
- **Performance Guides**: [docs/performance/](../performance/)  
- **Community Discussions**: GitHub Discussions for protocol optimization tips
- **Security Issues**: security@mcp-bridge.io (include protocol-specific context)