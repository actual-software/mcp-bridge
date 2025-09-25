# API Reference

Complete API reference for MCP Bridge Gateway and Router services.

## Overview

MCP Bridge provides multiple API interfaces:

- **HTTP REST API** - For management and health checks
- **WebSocket API** - For MCP protocol communication  
- **Binary TCP API** - High-performance MCP protocol
- **Metrics API** - Prometheus metrics endpoints

## Gateway HTTP API

### Base URL

```
https://gateway.example.com:8443
```

### Authentication

All API endpoints require authentication unless otherwise noted.

```bash
# Bearer token
curl -H "Authorization: Bearer $TOKEN" https://gateway/api/health

# mTLS
curl --cert client.crt --key client.key --cacert ca.crt https://gateway/api/health
```

### Health Check Endpoints

#### GET /health

Basic health check endpoint.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-08-04T12:00:00Z",
  "version": "1.0.0",
  "uptime": "2h30m15s"
}
```

#### GET /health/ready

Readiness probe for Kubernetes.

**Response:**
```json
{
  "ready": true,
  "checks": {
    "database": "healthy",
    "redis": "healthy",
    "backends": "healthy"
  }
}
```

#### GET /health/live

Liveness probe for Kubernetes.

**Response:**
```json
{
  "alive": true,
  "timestamp": "2024-08-04T12:00:00Z"
}
```

### Server Management

#### GET /api/servers

List configured MCP servers.

**Response:**
```json
{
  "servers": [
    {
      "id": "server-1",
      "name": "example-server",
      "url": "http://backend:8080",
      "status": "healthy",
      "last_health_check": "2024-08-04T12:00:00Z",
      "capabilities": {
        "tools": true,
        "resources": true,
        "prompts": false
      }
    }
  ]
}
```

#### GET /api/servers/{id}

Get details for a specific server.

**Parameters:**
- `id` (path) - Server ID

**Response:**
```json
{
  "id": "server-1",
  "name": "example-server",
  "url": "http://backend:8080",
  "status": "healthy",
  "health_check_interval": "30s",
  "timeout": "10s",
  "retry_count": 3,
  "capabilities": {
    "tools": true,
    "resources": true,
    "prompts": false
  },
  "metrics": {
    "requests_total": 1234,
    "errors_total": 5,
    "avg_response_time": "150ms"
  }
}
```

#### POST /api/servers/{id}/health-check

Trigger manual health check for a server.

**Parameters:**
- `id` (path) - Server ID

**Response:**
```json
{
  "server_id": "server-1",
  "status": "healthy",
  "response_time": "120ms",
  "timestamp": "2024-08-04T12:00:00Z"
}
```

### Connection Management

#### GET /api/connections

List active connections.

**Query Parameters:**
- `status` (optional) - Filter by connection status
- `user` (optional) - Filter by user
- `limit` (optional) - Limit results (default: 50)

**Response:**
```json
{
  "connections": [
    {
      "id": "conn-123",
      "user_id": "user-456",
      "client_ip": "192.168.1.100",
      "protocol": "websocket",
      "connected_at": "2024-08-04T11:30:00Z",
      "last_activity": "2024-08-04T11:59:30Z",
      "status": "active",
      "requests_count": 25,
      "bytes_sent": 15432,
      "bytes_received": 8901
    }
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

#### DELETE /api/connections/{id}

Terminate a specific connection.

**Parameters:**
- `id` (path) - Connection ID

**Response:**
```json
{
  "connection_id": "conn-123",
  "status": "terminated",
  "timestamp": "2024-08-04T12:00:00Z"
}
```

### Authentication Management

#### POST /api/auth/validate

Validate an authentication token.

**Request Body:**
```json
{
  "token": "bearer-token-here",
  "type": "bearer"
}
```

**Response:**
```json
{
  "valid": true,
  "user_id": "user-456",
  "expires_at": "2024-08-04T18:00:00Z",
  "scopes": ["mcp:access", "tools:execute"],
  "permissions": ["tools:*", "resources:read"]
}
```

#### POST /api/auth/revoke

Revoke an authentication token.

**Request Body:**
```json
{
  "token": "bearer-token-here",
  "type": "bearer"
}
```

**Response:**
```json
{
  "revoked": true,
  "timestamp": "2024-08-04T12:00:00Z"
}
```

### Configuration Management

#### GET /api/config

Get current configuration (sensitive values masked).

**Response:**
```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8443,
    "tls_enabled": true
  },
  "auth": {
    "methods": ["bearer", "mtls"],
    "bearer": {
      "enabled": true,
      "jwt_issuer": "mcp-bridge"
    }
  },
  "rate_limiting": {
    "enabled": true,
    "requests_per_minute": 1000
  }
}
```

#### PATCH /api/config

Update configuration (requires admin permissions).

**Request Body:**
```json
{
  "rate_limiting": {
    "requests_per_minute": 2000
  }
}
```

**Response:**
```json
{
  "updated": true,
  "timestamp": "2024-08-04T12:00:00Z",
  "changes": ["rate_limiting.requests_per_minute"]
}
```

## WebSocket API

### Connection

Connect to the WebSocket endpoint:

```javascript
const ws = new WebSocket('wss://gateway.example.com:8443/mcp', {
  headers: {
    'Authorization': 'Bearer ' + token
  }
});
```

### MCP Protocol Messages

All MCP protocol messages are sent as JSON-RPC 2.0 over WebSocket.

#### Initialize Connection

```json
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
      "name": "mcp-client",
      "version": "1.0.0"
    }
  }
}
```

#### Tool Operations

```json
// List tools
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/list"
}

// Call tool
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

## Binary TCP API

### Connection

Connect to the binary TCP endpoint:

```
tcp://gateway.example.com:8444
```

### Protocol Format

```
Message Format:
+--------+--------+--------+--------+
| Length | Type   | ID     | Payload|
+--------+--------+--------+--------+
|   4B   |   1B   |   4B   |  Var   |

Message Types:
0x01 - Request
0x02 - Response  
0x03 - Notification
0x04 - Error
0x05 - Ping
0x06 - Pong
```

### Authentication

Binary protocol authentication using header messages:

```
Auth Message:
+--------+--------+--------+--------+--------+
| Length | Type   | ID     | Method | Token  |
+--------+--------+--------+--------+--------+
|   4B   |  0x10  |   4B   |   1B   |  Var   |

Auth Methods:
0x01 - Bearer Token
0x02 - mTLS
0x03 - OAuth2
```

## Router API

### CLI Interface

The router provides a command-line interface:

```bash
# Show status
mcp-router status

# List connections  
mcp-router connections list

# Test connection
mcp-router test-connection

# Show configuration
mcp-router config show

# Update configuration
mcp-router config set gateway.url wss://new-gateway.com
```

### HTTP Management API

Router exposes a local management API:

#### GET http://localhost:9091/status

```json
{
  "status": "connected",
  "gateway_url": "wss://gateway.example.com",
  "connected_at": "2024-08-04T11:30:00Z",
  "uptime": "30m15s",
  "protocol": "websocket",
  "requests_sent": 150,
  "responses_received": 148,
  "errors": 2
}
```

#### GET http://localhost:9091/servers

```json
{
  "servers": [
    {
      "name": "my-server",
      "command": ["/usr/local/bin/my-mcp-server"],
      "status": "running",
      "pid": 12345,
      "started_at": "2024-08-04T11:30:00Z",
      "requests_processed": 75
    }
  ]
}
```

## Metrics API

### Gateway Metrics

#### GET /metrics

Prometheus metrics endpoint:

```
# HELP mcp_gateway_requests_total Total number of requests
# TYPE mcp_gateway_requests_total counter
mcp_gateway_requests_total{method="tools/list"} 1234

# HELP mcp_gateway_connections_active Number of active connections
# TYPE mcp_gateway_connections_active gauge
mcp_gateway_connections_active 42

# HELP mcp_gateway_request_duration_seconds Request duration in seconds
# TYPE mcp_gateway_request_duration_seconds histogram
mcp_gateway_request_duration_seconds_bucket{le="0.1"} 800
mcp_gateway_request_duration_seconds_bucket{le="0.5"} 950
mcp_gateway_request_duration_seconds_bucket{le="1.0"} 990
mcp_gateway_request_duration_seconds_bucket{le="+Inf"} 1000
```

### Router Metrics

#### GET http://localhost:9091/metrics

Router-specific metrics:

```
# HELP mcp_router_gateway_connected Whether router is connected to gateway
# TYPE mcp_router_gateway_connected gauge
mcp_router_gateway_connected 1

# HELP mcp_router_servers_running Number of running MCP servers
# TYPE mcp_router_servers_running gauge
mcp_router_servers_running 3

# HELP mcp_router_requests_proxied_total Total proxied requests
# TYPE mcp_router_requests_proxied_total counter
mcp_router_requests_proxied_total 567
```

## Error Responses

### HTTP Error Format

```json
{
  "error": {
    "code": "GTW_AUTH_001",
    "message": "Missing authentication credentials", 
    "details": "Authorization header not provided",
    "retryable": false,
    "remediation": "Provide valid authentication credentials"
  },
  "request_id": "req-12345",
  "timestamp": "2024-08-04T12:00:00Z"
}
```

### WebSocket Error Format

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
      "retry_after": 30,
      "timestamp": "2024-08-04T12:00:00Z"
    }
  }
}
```

## Rate Limiting

### Headers

Rate limiting information is provided in response headers:

```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 85
X-RateLimit-Reset: 1659600000
X-RateLimit-Retry-After: 15
```

### Rate Limit Exceeded Response

```json
{
  "error": {
    "code": "GTW_RATE_001",
    "message": "Request rate limit exceeded",
    "retryable": true,
    "retry_after": 60
  },
  "rate_limit": {
    "limit": 100,
    "remaining": 0,
    "reset": 1659600060
  }
}
```

## OpenAPI Specification

Complete OpenAPI 3.0 specification available at:

```
GET /api/docs/openapi.json
```

Interactive documentation:

```
GET /api/docs/
```

## SDKs and Client Libraries

### Official SDKs

- **Go**: `github.com/poiley/mcp-bridge-go`
- **Python**: `pip install mcp-bridge-client`
- **Node.js**: `npm install @mcp-bridge/client`
- **Java**: Maven coordinates in documentation

### Example Usage

#### Go Client

```go
import "github.com/poiley/mcp-bridge-go/client"

client := client.New("wss://gateway.example.com", 
    client.WithBearerAuth("token"))

tools, err := client.ListTools(ctx)
if err != nil {
    log.Fatal(err)
}
```

#### Python Client

```python
from mcp_bridge import Client

client = Client("wss://gateway.example.com", 
                auth_token="bearer-token")

tools = await client.list_tools()
```

## Related Documentation

- [Authentication Guide](authentication.md)
- [Protocol Implementation](protocol.md)
- [Error Handling](ERROR_HANDLING.md)
- [Configuration Reference](configuration.md)