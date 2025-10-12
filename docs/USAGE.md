# MCP Bridge Usage Guide

Complete guide for using MCP Bridge to connect clients to MCP servers through the gateway and router architecture.

## Table of Contents

- [Overview](#overview)
- [Basic Concepts](#basic-concepts)
- [Gateway Usage](#gateway-usage)
- [Router Usage](#router-usage)
- [Common Workflows](#common-workflows)
- [Protocol Support](#protocol-support)
- [Troubleshooting](#troubleshooting)

## Overview

MCP Bridge provides two main components for connecting clients to MCP servers:

- **Gateway**: Server-side component that routes requests to backend MCP servers
- **Router**: Client-side component that connects local clients to gateways or direct MCP servers

```mermaid
graph LR
    Client[MCP Client] --> Router[Router]
    Router --> Gateway[Gateway]
    Gateway --> Server[MCP Server]

    style Client fill:#e1f5ff,stroke:#0066cc,stroke-width:2px
    style Router fill:#fff4e1,stroke:#ff9900,stroke-width:2px
    style Gateway fill:#e1ffe1,stroke:#00cc66,stroke-width:2px
    style Server fill:#ffe1f5,stroke:#cc0099,stroke-width:2px
```

## Basic Concepts

### Authentication

MCP Bridge supports multiple authentication methods:

- **Bearer Token**: Simple token-based authentication
- **JWT**: JSON Web Token authentication with configurable issuer/audience
- **OAuth2**: OAuth2 client credentials or password grant flow
- **mTLS**: Mutual TLS certificate authentication

### Service Discovery

The gateway can discover MCP servers through:

- **Static**: Manually configured endpoints
- **Kubernetes**: Automatic discovery from K8s services
- **stdio**: Local subprocess-based servers
- **WebSocket/SSE**: Remote protocol-based servers

### Protocols

Both components support multiple protocols:

- **stdio**: Local subprocess communication
- **WebSocket**: Bidirectional web protocol
- **HTTP**: Request/response REST-style
- **SSE**: Server-Sent Events streaming
- **TCP Binary**: High-performance wire protocol

## Gateway Usage

### Starting the Gateway

```bash
# Using configuration file
mcp-gateway --config /etc/mcp/gateway.yaml

# Using environment variables
JWT_SECRET_KEY=your-secret mcp-gateway

# Docker deployment
docker run -p 8443:8443 -v /path/to/config.yaml:/etc/mcp/gateway.yaml ghcr.io/actual-software/mcp-bridge/gateway:latest
```

### Gateway Configuration

Basic gateway configuration (`gateway.yaml`):

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket
  tls:
    enabled: true
    cert_file: /etc/mcp/tls/tls.crt
    key_file: /etc/mcp/tls/tls.key

auth:
  provider: jwt
  jwt:
    issuer: mcp-gateway
    audience: mcp-services
    secret_key_env: JWT_SECRET_KEY

service_discovery:
  provider: static
  static:
    endpoints:
      default:
        - url: http://localhost:3000

metrics:
  enabled: true
  endpoint: 0.0.0.0:9090

logging:
  level: info
  format: json
```

See [Configuration Reference](configuration.md) for complete options.

### Adding Backend MCP Servers

#### Static Configuration

```yaml
service_discovery:
  provider: static
  static:
    endpoints:
      # Stdio-based local server
      weather:
        - url: stdio://weather-server
          labels:
            command: ["python", "/app/weather_server.py"]
            working_dir: "/app"

      # WebSocket remote server
      tools:
        - url: ws://tools-server:8080
          labels:
            protocol: websocket

      # HTTP-based server
      data:
        - url: http://data-server:9000
          labels:
            protocol: http
```

#### Kubernetes Discovery

```yaml
service_discovery:
  provider: kubernetes
  kubernetes:
    in_cluster: true
    namespace_pattern: "mcp-*"
    service_labels:
      mcp-enabled: "true"
  refresh_rate: 30s
```

Label your K8s services:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: weather-mcp
  namespace: mcp-production
  labels:
    mcp-enabled: "true"
    mcp-namespace: weather
spec:
  selector:
    app: weather-server
  ports:
  - port: 8080
    protocol: TCP
```

### Health Checks

Check gateway health:

```bash
# Basic health check
curl -k https://localhost:8443/health

# Metrics endpoint
curl http://localhost:9090/metrics

# Check discovered services
curl -k https://localhost:8443/health/services
```

Expected response:
```json
{
  "status": "healthy",
  "uptime": "2h45m30s",
  "connections": 42,
  "backends": 3
}
```

## Router Usage

### Starting the Router

```bash
# Using configuration file
mcp-router --config ~/.config/mcp-router/config.yaml

# Direct mode (connect to local MCP server)
mcp-router --direct --server "python weather_server.py"

# Gateway mode (connect through gateway)
mcp-router --gateway wss://gateway.example.com:8443
```

### Router Configuration

Basic router configuration (`~/.config/mcp-router/config.yaml`):

```yaml
version: 1

# Gateway pool configuration
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com:8443
      auth:
        type: bearer
        token_env: MCP_AUTH_TOKEN
      weight: 1

    - url: wss://gateway-backup.example.com:8443
      auth:
        type: bearer
        token_env: MCP_AUTH_TOKEN
      weight: 1
      priority: 2

  load_balancer:
    strategy: round_robin
    failover_timeout: 30s
    retry_count: 3

  circuit_breaker:
    enabled: true
    failure_threshold: 5
    recovery_timeout: 30s

# Direct server configuration (optional)
direct:
  auto_detection:
    enabled: true
    timeout: 10s

  max_connections: 100

  health_check:
    enabled: true
    interval: 30s

metrics:
  enabled: true
  endpoint: localhost:9091

logging:
  level: info
  format: json
```

### Authentication Setup

#### Bearer Token (Secure Storage)

```bash
# Store token in platform keychain
mcp-router auth set-token --gateway wss://gateway.example.com:8443

# Or use environment variable
export MCP_AUTH_TOKEN=your-token-here
```

#### OAuth2

```yaml
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com:8443
      auth:
        type: oauth2
        grant_type: client_credentials
        client_id: mcp-router-client
        client_secret_env: MCP_CLIENT_SECRET
        token_endpoint: https://auth.example.com/oauth/token
```

### Direct Server Connections

The router can connect directly to MCP servers without a gateway:

```yaml
direct:
  auto_detection:
    enabled: true
    preferred_order: ["http", "websocket", "stdio"]

  # Protocol-specific settings
  stdio:
    process_timeout: 30s
    max_buffer_size: 65536

  websocket:
    handshake_timeout: 10s
    ping_interval: 30s

  http:
    request_timeout: 30s
    max_idle_conns: 100
```

## Common Workflows

### Connecting a Client to Gateway

**Step 1**: Obtain authentication token

```bash
# From your authentication system
TOKEN=$(curl -X POST https://auth.example.com/token \
  -d "grant_type=client_credentials&client_id=my-client&client_secret=secret" \
  | jq -r '.access_token')
```

**Step 2**: Connect using WebSocket

```javascript
// JavaScript client example
const ws = new WebSocket('wss://gateway.example.com:8443', {
  headers: {
    'Authorization': `Bearer ${token}`
  }
});

ws.on('open', () => {
  // Send MCP request
  ws.send(JSON.stringify({
    jsonrpc: '2.0',
    method: 'tools/list',
    id: 1
  }));
});

ws.on('message', (data) => {
  const response = JSON.parse(data);
  console.log('MCP Response:', response);
});
```

**Step 3**: Send MCP requests

```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "weather/get_forecast",
    "arguments": {
      "location": "San Francisco"
    }
  },
  "id": 2
}
```

See [Client Integration Guide](client-integration.md) for more examples.

### Setting Up a New MCP Server

**Step 1**: Create your MCP server

```python
# weather_server.py
import json
import sys

def handle_request(request):
    if request['method'] == 'tools/list':
        return {
            'jsonrpc': '2.0',
            'result': {
                'tools': [{
                    'name': 'get_forecast',
                    'description': 'Get weather forecast',
                    'inputSchema': {
                        'type': 'object',
                        'properties': {
                            'location': {'type': 'string'}
                        }
                    }
                }]
            },
            'id': request['id']
        }

if __name__ == '__main__':
    for line in sys.stdin:
        request = json.loads(line)
        response = handle_request(request)
        print(json.dumps(response))
        sys.stdout.flush()
```

**Step 2**: Add to gateway configuration

```yaml
service_discovery:
  provider: stdio
  stdio:
    services:
      - name: weather
        namespace: default
        command: ["python", "/app/weather_server.py"]
        working_dir: /app
        health_check:
          enabled: true
          interval: 30s
```

**Step 3**: Restart gateway and verify

```bash
# Restart gateway
systemctl restart mcp-gateway

# Verify server is discovered
curl -k https://localhost:8443/health/services | jq '.services[] | select(.name=="weather")'
```

See [Server Integration Guide](server-integration.md) for more details.

### Load Balancing Multiple Servers

Configure multiple backends with weights:

```yaml
service_discovery:
  provider: static
  static:
    endpoints:
      api:
        - url: http://api-server-1:8080
          labels:
            weight: 3
        - url: http://api-server-2:8080
          labels:
            weight: 2
        - url: http://api-server-3:8080
          labels:
            weight: 1

routing:
  strategy: weighted_round_robin
  health_check_interval: 30s
```

### Using Circuit Breakers

Enable circuit breakers to prevent cascade failures:

```yaml
circuit_breaker:
  enabled: true
  failure_threshold: 5      # Open after 5 failures
  success_threshold: 2      # Close after 2 successes
  timeout_seconds: 30       # Try again after 30s
  max_requests: 1           # Allow 1 request in half-open state
```

States:
- **Closed**: Normal operation, requests pass through
- **Open**: Circuit tripped, requests fail fast
- **Half-Open**: Testing if service recovered

Monitor circuit breaker state:

```bash
curl http://localhost:9090/metrics | grep circuit_breaker_state
```

## Protocol Support

### WebSocket Protocol

**Client connection**:
```bash
# Using wscat
wscat -c wss://gateway.example.com:8443 \
  -H "Authorization: Bearer $TOKEN"
```

**Message format**:
```json
{
  "jsonrpc": "2.0",
  "method": "tools/list",
  "id": 1
}
```

### HTTP Protocol

**Client connection**:
```bash
curl -X POST https://gateway.example.com:8443/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/list",
    "id": 1
  }'
```

### SSE Protocol

**Client connection**:
```bash
curl -N https://gateway.example.com:8443/mcp/stream \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: text/event-stream"
```

**Event format**:
```
event: message
data: {"jsonrpc":"2.0","result":{"tools":[...]},"id":1}
```

### stdio Protocol

**Local usage** (router only):
```bash
# Router connects to local MCP server
echo '{"jsonrpc":"2.0","method":"tools/list","id":1}' | mcp-router
```

### TCP Binary Protocol

**High-performance connection**:
```bash
# Connect to TCP port (default: port + 1000)
nc gateway.example.com 9443
```

Binary frame format:
```
[4 bytes: length][N bytes: JSON-RPC message]
```

See [Protocol Documentation](protocol.md) for wire protocol details.

## Troubleshooting

### Connection Issues

**Gateway not responding:**

```bash
# Check if gateway is running
systemctl status mcp-gateway

# Check logs
journalctl -u mcp-gateway -f

# Verify port is listening
netstat -tlnp | grep 8443
```

**Authentication failures:**

```bash
# Verify token format
echo $MCP_AUTH_TOKEN | jwt decode -

# Check gateway auth configuration
grep -A 5 "^auth:" /etc/mcp/gateway.yaml

# Test with curl
curl -k -H "Authorization: Bearer $TOKEN" https://localhost:8443/health
```

**Router cannot connect:**

```bash
# Check router logs
mcp-router --config config.yaml --log-level debug

# Test gateway connectivity
wscat -c wss://gateway.example.com:8443

# Verify DNS resolution
nslookup gateway.example.com
```

### Performance Issues

**High latency:**

```bash
# Check metrics
curl http://localhost:9090/metrics | grep request_duration

# Enable tracing
# Add to gateway.yaml:
tracing:
  enabled: true
  service_name: mcp-gateway
  otlp_endpoint: http://jaeger:4317
```

**Connection pool exhausted:**

```yaml
# Increase connection limits
server:
  max_connections: 10000
  max_connections_per_ip: 100
  connection_buffer_size: 131072
```

**Memory usage:**

```bash
# Check current usage
ps aux | grep mcp-gateway

# Enable pprof profiling
# Router config:
advanced:
  debug:
    enable_pprof: true
    pprof_port: 6060

# Capture heap profile
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof heap.prof
```

### Service Discovery Issues

**Backends not discovered:**

```bash
# For Kubernetes discovery
kubectl get services -l mcp-enabled=true --all-namespaces

# Check gateway logs
journalctl -u mcp-gateway | grep discovery

# Verify refresh rate
grep refresh_rate /etc/mcp/gateway.yaml
```

**Health checks failing:**

```yaml
# Adjust health check intervals
service_discovery:
  static:
    endpoints:
      myservice:
        - url: http://myserver:8080
          labels:
            health_check_interval: 60s
            health_check_timeout: 10s
```

### Getting Help

- **Documentation**: [docs/](../docs/)
- **Configuration**: [docs/configuration.md](configuration.md)
- **Client Integration**: [docs/client-integration.md](client-integration.md)
- **Server Integration**: [docs/server-integration.md](server-integration.md)
- **Troubleshooting**: [docs/troubleshooting.md](troubleshooting.md)
- **GitHub Issues**: [Report issues](https://github.com/actual-software/mcp-bridge/issues)
- **Monitoring Guide**: [docs/monitoring.md](monitoring.md)

## Next Steps

- **[Client Integration Guide](client-integration.md)**: Build clients that connect to MCP Bridge
- **[Server Integration Guide](server-integration.md)**: Add MCP servers to the gateway
- **[Configuration Reference](configuration.md)**: Complete configuration options
- **[Tutorials](tutorials/)**: Step-by-step walkthroughs
- **[Production Deployment](deployment/production.md)**: Deploy to production environments
