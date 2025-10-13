# Tutorial: Protocol Selection Guide

Learn how to choose and configure the right protocol for your MCP Bridge deployment.

## Prerequisites

- MCP Bridge installed ([Installation Guide](../installation-and-setup.md))
- Understanding of network protocols
- 20-25 minutes

## What You'll Learn

- The 5 protocols supported by MCP Bridge Gateway
- When to use each protocol
- How to configure multiple protocols simultaneously
- Performance characteristics of each protocol
- Best practices for protocol selection

## MCP Bridge Protocol Architecture

MCP Bridge Gateway supports 5 frontend protocols **simultaneously**:

| Protocol | Port | Use Case | Bidirectional | Streaming |
|----------|------|----------|---------------|-----------|
| **WebSocket** | 8443 | Web browsers, real-time apps | ✅ Yes | ✅ Yes |
| **HTTP** | 8080 | REST clients, simple integrations | ❌ No | ❌ No |
| **SSE** | 8081 | Server push, monitoring | ⚠️ Server→Client | ✅ Yes |
| **TCP Binary** | 8444 | High-performance, low-latency | ✅ Yes | ✅ Yes |
| **stdio** | - | CLI tools, local integration | ✅ Yes | ✅ Yes |

## Protocol Deep Dive

### 1. WebSocket Protocol

**Best for**: Web applications, browsers, real-time communication

**Characteristics**:
- Full-duplex bidirectional communication
- Low overhead after initial handshake
- Native browser support
- Firewall-friendly (HTTP upgrade)

**Configuration**:

```yaml
server:
  frontends:
    - name: websocket-main
      protocol: websocket
      enabled: true
      config:
        host: 0.0.0.0
        port: 8443
        max_connections: 10000
        read_timeout: 60s
        write_timeout: 60s
        ping_interval: 30s
        pong_timeout: 10s
        max_message_size: 1048576
        allowed_origins:
          - "https://yourdomain.com"
          - "https://*.yourdomain.com"
        tls:
          enabled: true
          cert_file: /certs/server.crt
          key_file: /certs/server.key
```

**Client Example** (JavaScript):

```javascript
const ws = new WebSocket('wss://gateway.example.com:8443');

ws.onopen = () => {
  ws.send(JSON.stringify({
    jsonrpc: "2.0",
    method: "tools/list",
    id: 1
  }));
};

ws.onmessage = (event) => {
  const response = JSON.parse(event.data);
  console.log('Response:', response);
};
```

**When to use**:
- ✅ Browser-based applications
- ✅ Real-time updates required
- ✅ Long-lived connections
- ✅ Need bidirectional communication
- ❌ Simple request/response only

### 2. HTTP Protocol

**Best for**: REST APIs, simple integrations, stateless requests

**Characteristics**:
- Request/response model
- Stateless (no connection reuse)
- Universal client support
- Easy to debug with curl

**Configuration**:

```yaml
server:
  frontends:
    - name: http-api
      protocol: http
      enabled: true
      config:
        host: 0.0.0.0
        port: 8080
        request_path: /api/v1/mcp
        max_request_size: 1048576
        read_timeout: 30s
        write_timeout: 30s
        tls:
          enabled: true
          cert_file: /certs/server.crt
          key_file: /certs/server.key
```

**Client Example** (curl):

```bash
curl -X POST https://gateway.example.com:8080/api/v1/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
      "name": "get_data",
      "arguments": {"id": "123"}
    },
    "id": 1
  }'
```

**When to use**:
- ✅ Simple request/response patterns
- ✅ Existing HTTP infrastructure
- ✅ No streaming required
- ✅ Easy debugging needed
- ❌ Real-time updates required
- ❌ High message frequency

### 3. Server-Sent Events (SSE)

**Best for**: Server-push notifications, monitoring dashboards, event streams

**Characteristics**:
- One-way streaming (server → client)
- Built on HTTP
- Automatic reconnection
- Text-based format

**Configuration**:

```yaml
server:
  frontends:
    - name: sse-stream
      protocol: sse
      enabled: true
      config:
        host: 0.0.0.0
        port: 8081
        stream_endpoint: /events
        request_endpoint: /api/v1/request
        keep_alive: 30s
        buffer_size: 256
        max_connections: 10000
        read_timeout: 0s  # No timeout for streaming
        write_timeout: 30s
        tls:
          enabled: true
          cert_file: /certs/server.crt
          key_file: /certs/server.key
```

**Client Example** (JavaScript):

```javascript
// Subscribe to event stream
const eventSource = new EventSource('https://gateway.example.com:8081/events');

eventSource.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Server event:', data);
};

// Send requests via separate endpoint
fetch('https://gateway.example.com:8081/api/v1/request', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: "2.0",
    method: "tools/call",
    params: { name: "monitor" },
    id: 1
  })
});
```

**When to use**:
- ✅ Server needs to push updates
- ✅ Monitoring dashboards
- ✅ Event notifications
- ✅ Browser-based clients
- ❌ Client needs to send frequent messages
- ❌ Need full bidirectional

### 4. TCP Binary Protocol

**Best for**: High-performance, low-latency, high-throughput applications

**Characteristics**:
- Binary wire protocol
- Lowest overhead
- Version negotiation
- Best performance

**Configuration**:

```yaml
server:
  frontends:
    - name: tcp-binary
      protocol: tcp_binary
      enabled: true
      config:
        host: 0.0.0.0
        port: 8444
        max_connections: 10000
        read_timeout: 60s
        write_timeout: 60s
        tls:
          enabled: true
          cert_file: /certs/server.crt
          key_file: /certs/server.key
```

**Client Example** (Go):

```go
import "github.com/actual-software/mcp-bridge/pkg/protocol/binary"

conn, err := net.Dial("tcp", "gateway.example.com:8444")
if err != nil {
    log.Fatal(err)
}

client := binary.NewClient(conn)

response, err := client.Call(&protocol.Request{
    JSONRPC: "2.0",
    Method:  "tools/call",
    Params:  map[string]interface{}{"name": "compute"},
    ID:      1,
})
```

**When to use**:
- ✅ Performance-critical applications
- ✅ High message frequency
- ✅ Low latency requirements
- ✅ Large message volumes
- ❌ Browser clients
- ❌ Simple debugging needed

### 5. stdio Protocol

**Best for**: CLI tools, local development, process integration

**Characteristics**:
- stdin/stdout communication
- Unix socket alternative
- Local-only (no network)
- Perfect for Claude Code integration

**Configuration**:

```yaml
server:
  frontends:
    - name: stdio-cli
      protocol: stdio
      enabled: true
      config:
        mode: stdio  # or "unix_socket"
        socket_path: /tmp/mcp-gateway.sock
        max_concurrent_clients: 100
        cleanup_interval: 5m
        idle_timeout: 30m
```

**Client Example** (CLI):

```bash
# Direct stdio
echo '{"jsonrpc":"2.0","method":"tools/list","id":1}' | mcp-gateway

# Unix socket
echo '{"jsonrpc":"2.0","method":"tools/list","id":1}' | \
  socat - UNIX-CONNECT:/tmp/mcp-gateway.sock
```

**When to use**:
- ✅ CLI applications
- ✅ Local development
- ✅ Process-based integration
- ✅ CI/CD pipelines
- ❌ Remote clients
- ❌ Web applications

## Multi-Frontend Configuration

Enable **all protocols simultaneously** for maximum flexibility:

```yaml
version: 1

server:
  frontends:
    # WebSocket for web apps
    - name: websocket-main
      protocol: websocket
      enabled: true
      config:
        host: 0.0.0.0
        port: 8443
        max_connections: 10000
        tls:
          enabled: true
          cert_file: /certs/tls.crt
          key_file: /certs/tls.key

    # HTTP for REST clients
    - name: http-api
      protocol: http
      enabled: true
      config:
        host: 0.0.0.0
        port: 8080
        request_path: /api/v1/mcp

    # SSE for monitoring
    - name: sse-events
      protocol: sse
      enabled: true
      config:
        host: 0.0.0.0
        port: 8081
        stream_endpoint: /events

    # TCP Binary for performance
    - name: tcp-fast
      protocol: tcp_binary
      enabled: true
      config:
        host: 0.0.0.0
        port: 8444

    # stdio for CLI
    - name: stdio-local
      protocol: stdio
      enabled: true
      config:
        mode: unix_socket
        socket_path: /var/run/mcp-gateway.sock

# Shared auth applies to all frontends
auth:
  type: jwt
  jwt:
    issuer: mcp-gateway
    audience: mcp-services
    secret_key_env: JWT_SECRET_KEY

# Backend configuration
discovery:
  provider: kubernetes
  kubernetes:
    namespace_selector:
      - mcp-servers

observability:
  metrics:
    enabled: true
    port: 9090
  logging:
    level: info
    format: json
```

## Performance Comparison

Based on MCP Bridge benchmarks with 10k concurrent connections:

| Protocol | Latency (P50) | Latency (P99) | Throughput | CPU Usage |
|----------|---------------|---------------|------------|-----------|
| TCP Binary | **1.8ms** | 5.2ms | **112k RPS** | Low |
| WebSocket | 2.3ms | 6.1ms | 95k RPS | Medium |
| HTTP | 3.1ms | 8.7ms | 75k RPS | Medium |
| SSE | 2.8ms | 7.4ms | 82k RPS | Medium |
| stdio | 0.9ms | 2.1ms | Local only | Minimal |

## Decision Tree

```
Need network access?
├─ No → Use stdio
└─ Yes
   └─ Browser client?
      ├─ Yes
      │  └─ Need server push?
      │     ├─ Yes → Use SSE
      │     └─ No → Use WebSocket
      └─ No
         └─ Performance critical?
            ├─ Yes → Use TCP Binary
            └─ No
               └─ Simple integration?
                  ├─ Yes → Use HTTP
                  └─ No → Use WebSocket
```

## Testing Your Configuration

### Test WebSocket

```bash
npm install -g wscat
wscat -c wss://gateway.example.com:8443 \
  -H "Authorization: Bearer TOKEN"
> {"jsonrpc":"2.0","method":"tools/list","id":1}
```

### Test HTTP

```bash
curl -X POST https://gateway.example.com:8080/api/v1/mcp \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

### Test SSE

```bash
curl -N https://gateway.example.com:8081/events \
  -H "Authorization: Bearer TOKEN"
```

### Test TCP Binary

```bash
# Using custom client or telnet for basic test
telnet gateway.example.com 8444
```

### Test stdio

```bash
echo '{"jsonrpc":"2.0","method":"tools/list","id":1}' | mcp-gateway
```

## Best Practices

### 1. Protocol Selection

- **Production Web Apps**: WebSocket + HTTP fallback
- **Mobile Apps**: WebSocket with automatic reconnection
- **Microservices**: TCP Binary for inter-service
- **Monitoring Dashboards**: SSE for real-time updates
- **CLI Tools**: stdio for local execution

### 2. Security

- **Always enable TLS** for network protocols in production
- Use **strong cipher suites** (TLS 1.3)
- Configure **CORS** properly for WebSocket/HTTP
- Set **allowed_origins** for WebSocket

### 3. Performance

- Use **TCP Binary** for high-throughput scenarios
- Enable **connection pooling** on clients
- Set appropriate **timeouts** per protocol
- Monitor **connection counts** and adjust limits

### 4. Reliability

- Implement **retry logic** on clients
- Use **circuit breakers** for backend protection
- Configure **health checks** appropriately
- Set up **monitoring** for all protocols

## Troubleshooting

### Protocol Not Accepting Connections

```bash
# Check if port is listening
netstat -tlnp | grep 8443

# Check gateway logs
journalctl -u mcp-gateway -f | grep frontend

# Verify configuration
mcp-gateway --config gateway.yaml --validate
```

### High Latency on Specific Protocol

```bash
# Check metrics for that protocol
curl http://gateway:9090/metrics | grep mcp_gateway_request_duration_seconds

# Enable debug logging
# In gateway.yaml:
observability:
  logging:
    level: debug
```

### WebSocket Connection Drops

```bash
# Increase ping interval
server:
  frontends:
    - name: websocket-main
      protocol: websocket
      config:
        ping_interval: 30s
        pong_timeout: 10s
        read_timeout: 120s
```

## Next Steps

- [Authentication & Security](08-authentication.md) - Secure your protocols
- [Load Balancing](07-load-balancing.md) - Scale across multiple backends
- [Monitoring & Observability](10-monitoring.md) - Monitor protocol performance
- [Configuration Reference](../configuration.md) - Complete protocol options

## Summary

You've learned:
- ✅ The 5 protocols MCP Bridge supports
- ✅ When to use each protocol
- ✅ How to configure multiple protocols simultaneously
- ✅ Performance characteristics
- ✅ Best practices for production deployments

MCP Bridge's multi-frontend architecture lets you support all client types from a single gateway instance!
