# MCP Protocol Implementation

This document describes how MCP Bridge implements the Model Context Protocol (MCP) specification.

## Overview

MCP Bridge provides a comprehensive implementation of the [Model Context Protocol](https://modelcontextprotocol.io), enabling secure, scalable access to MCP servers through a gateway architecture.

## Protocol Versions

MCP Bridge supports multiple protocol versions:

- **MCP 1.0**: Full compatibility with the current specification
- **MCP 2.0**: Enhanced features with backward compatibility
- **Binary Protocol**: High-performance binary transport (MCP Bridge extension)

## Transport Layers

### WebSocket Transport

The primary transport mechanism using WebSocket connections:

```yaml
# Router configuration
gateway:
  url: "wss://gateway.example.com/mcp"
  protocol: "websocket"
  
# Gateway configuration  
server:
  websocket:
    enabled: true
    port: 8443
    path: "/mcp"
```

### Binary TCP Transport

High-performance binary protocol for low-latency scenarios:

```yaml
# Router configuration
gateway:
  url: "tcp://gateway.example.com:8444"
  protocol: "binary"

# Gateway configuration
server:
  binary:
    enabled: true
    port: 8444
```

### HTTP/HTTPS Transport

RESTful HTTP transport for simple integrations:

```yaml
# Gateway configuration
server:
  http:
    enabled: true
    port: 8080
    path: "/api/mcp"
```

## Message Format

### JSON-RPC 2.0 Base

All MCP messages follow the JSON-RPC 2.0 specification:

```json
{
  "jsonrpc": "2.0",
  "id": "request-123",
  "method": "tools/list",
  "params": {}
}
```

### Binary Protocol Format

The binary protocol uses a compact format for performance:

```
+--------+--------+--------+--------+
| Length | Type   | ID     | Payload|
+--------+--------+--------+--------+
|   4B   |   1B   |   4B   |  Var   |
```

## Core MCP Methods

### Server Information

```json
// Initialize request
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "1.0",
    "capabilities": {
      "tools": {},
      "resources": {},
      "prompts": {}
    },
    "clientInfo": {
      "name": "mcp-bridge-router",
      "version": "1.0.0"
    }
  }
}
```

### Tool Operations

```json
// List available tools
{
  "jsonrpc": "2.0", 
  "id": 2,
  "method": "tools/list"
}

// Call a tool
{
  "jsonrpc": "2.0",
  "id": 3, 
  "method": "tools/call",
  "params": {
    "name": "calculator",
    "arguments": {
      "operation": "add",
      "a": 5,
      "b": 3
    }
  }
}
```

### Resource Operations

```json
// List available resources
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "resources/list"
}

// Read a resource
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "resources/read", 
  "params": {
    "uri": "file:///documents/report.pdf"
  }
}
```

### Prompt Operations

```json
// List available prompts
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "prompts/list"
}

// Get a prompt
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "prompts/get",
  "params": {
    "name": "code-review",
    "arguments": {
      "language": "go",
      "file": "main.go"
    }
  }
}
```

## Protocol Extensions

### Authentication

MCP Bridge extends the protocol with authentication capabilities:

```json
// Authentication request
{
  "jsonrpc": "2.0",
  "id": "auth-1",
  "method": "auth/authenticate",
  "params": {
    "type": "bearer",
    "token": "eyJhbGciOiJIUzI1NiIs..."
  }
}
```

### Batch Operations

Support for batched requests to improve performance:

```json
[
  {"jsonrpc": "2.0", "id": 1, "method": "tools/list"},
  {"jsonrpc": "2.0", "id": 2, "method": "resources/list"},
  {"jsonrpc": "2.0", "id": 3, "method": "prompts/list"}
]
```

### Streaming Responses

Long-running operations support streaming responses:

```json
// Request with streaming
{
  "jsonrpc": "2.0",
  "id": "stream-1", 
  "method": "tools/call",
  "params": {
    "name": "large-data-processor",
    "arguments": {"dataset": "large.csv"},
    "stream": true
  }
}

// Streaming response chunks
{
  "jsonrpc": "2.0",
  "id": "stream-1",
  "result": {
    "chunk": 1,
    "data": "...",
    "final": false
  }
}
```

## Error Handling

### Standard MCP Errors

```json
{
  "jsonrpc": "2.0",
  "id": "request-123",
  "error": {
    "code": -32601,
    "message": "Method not found",
    "data": {
      "method": "invalid/method"
    }
  }
}
```

### MCP Bridge Extended Errors

See [Error Handling Documentation](ERROR_HANDLING.md) for comprehensive error codes and handling.

```json
{
  "jsonrpc": "2.0", 
  "id": "request-123",
  "error": {
    "code": -32000,
    "message": "Gateway error",
    "data": {
      "error_code": "GTW_CONN_002",
      "retryable": true,
      "retry_after": 30
    }
  }
}
```

## Connection Lifecycle

### Connection Establishment

1. **Client connects** to router via stdio
2. **Router establishes** WebSocket/TCP connection to gateway
3. **Authentication** exchange (if required)
4. **Capability negotiation** via initialize method
5. **Ready state** - normal message exchange

### Keep-alive and Health Checks

```json
// Ping message (every 30 seconds)
{
  "jsonrpc": "2.0",
  "id": "ping-1",
  "method": "ping"
}

// Pong response
{
  "jsonrpc": "2.0", 
  "id": "ping-1",
  "result": "pong"
}
```

### Connection Termination

1. **Graceful shutdown** - Close connection after pending requests
2. **Emergency shutdown** - Immediate connection termination
3. **Automatic reconnection** - Router attempts reconnection with backoff

## Performance Optimizations

### Message Compression

WebSocket connections support compression:

```yaml
gateway:
  websocket:
    compression: true
    compression_level: 6
```

### Connection Pooling

Router maintains connection pools:

```yaml
connection_pool:
  max_connections: 10
  idle_timeout: 300s
  max_lifetime: 3600s
```

### Request Batching

Automatic batching of concurrent requests:

```yaml
batching:
  enabled: true
  max_batch_size: 10
  batch_timeout: 10ms
```

## Security Considerations

### Message Validation

All messages are validated against the MCP specification:

- JSON-RPC 2.0 format compliance
- Method name validation
- Parameter type checking
- Size limits enforcement

### Rate Limiting

Per-connection rate limiting:

```yaml
rate_limiting:
  requests_per_second: 100
  burst_size: 50
  window_size: 60s
```

### Encryption

All connections use TLS 1.3 encryption:

- Certificate-based authentication
- Perfect forward secrecy
- Strong cipher suites only

## Monitoring and Observability

### Protocol Metrics

- Message rate and latency
- Error rates by method
- Connection count and state
- Protocol version distribution

### Distributed Tracing

Full OpenTelemetry integration:

- Request tracing across services
- Protocol method spans
- Error attribution
- Performance profiling

## Testing

### Protocol Compliance Tests

Comprehensive test suite ensuring MCP specification compliance:

```bash
# Run protocol compliance tests
make test-protocol-compliance

# Test specific protocol features
make test-tools
make test-resources  
make test-prompts
```

### Performance Tests

Protocol performance validation:

```bash
# Protocol performance tests
make test-protocol-performance

# Load testing
make test-protocol-load
```

## Related Documentation

- [Gateway Documentation](../services/gateway/README.md)
- [Router Documentation](../services/router/README.md)
- [Error Handling](ERROR_HANDLING.md)
- [Security Implementation](SECURITY.md)
- [Performance Guide](performance.md)
- [API Reference](api.md)