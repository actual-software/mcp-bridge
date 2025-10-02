# MCP Bridge API Documentation

## Overview

MCP Bridge provides comprehensive REST and WebSocket APIs with universal protocol support for both the Gateway (server-side) and Router (client-side) components. This documentation covers all available endpoints, authentication methods, protocol-specific behaviors, and usage examples.

## 📚 Documentation Formats

### Interactive API Explorer
Open `api/index.html` in a web browser to access the interactive Swagger UI documentation:
```bash
# From project root
open api/index.html
# Or
python3 -m http.server 8000 -d api
# Then visit http://localhost:8000
```

### OpenAPI Specifications
- **Gateway API**: [`api/openapi/gateway.yaml`](openapi/gateway.yaml)
- **Router API**: [`api/openapi/router.yaml`](openapi/router.yaml)

## 🚀 Quick Start

### Gateway API

The Gateway API is the server-side component with universal protocol support that routes MCP requests to any backend server regardless of protocol.

**Base URL**: `https://gateway.mcp-bridge.io` (production) or `https://localhost:8443` (local)

#### Protocol Support Matrix
The Gateway API supports multiple frontend and backend protocol combinations:

**Frontend Protocols:**
- **WebSocket** (`wss://localhost:8443`) - Primary real-time interface
- **TCP Binary** (`tcps://localhost:8444`) - High-performance binary protocol
- **HTTP REST** (`https://localhost:8443/api/v1`) - RESTful interface
- **stdio** (Unix socket: `/tmp/mcp-gateway.sock`) - Direct local connections

**Backend Integration:**
- Routes to stdio, WebSocket, HTTP, SSE, and TCP Binary servers
- Cross-protocol load balancing with protocol-aware routing
- Automatic protocol detection and conversion

#### Authentication
```bash
# Bearer Token
curl -H "Authorization: Bearer YOUR_TOKEN" https://gateway.mcp-bridge.io/api/v1/execute

# OAuth2
curl -H "Authorization: Bearer OAUTH_ACCESS_TOKEN" https://gateway.mcp-bridge.io/api/v1/execute

# mTLS
curl --cert client.crt --key client.key https://gateway.mcp-bridge.io/api/v1/execute
```

#### Key Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Universal health check |
| `/health/protocols` | GET | Protocol-specific health status |
| `/health/stdio` | GET | stdio backend health |
| `/health/websocket` | GET | WebSocket backend health |
| `/health/http` | GET | HTTP backend health |
| `/health/sse` | GET | SSE backend health |
| `/health/load-balancer` | GET | Cross-protocol load balancer status |
| `/health/discovery` | GET | Service discovery health |
| `/ready` | GET | Readiness check with protocol validation |
| `/metrics` | GET | Prometheus metrics with protocol breakdown |
| `/api/v1/execute` | POST | Execute MCP request with protocol selection |
| `/api/v1/stream` | WebSocket | Streaming MCP connection (WebSocket protocol) |
| `/api/v1/stream-tcp` | TCP | Binary streaming connection (TCP protocol) |
| `/api/v1/sessions` | GET | List active sessions with protocol info |
| `/api/v1/auth/validate` | POST | Validate auth token |
| `/api/v1/protocols` | GET | List supported protocols and capabilities |

### Router API

The Router API provides local management of the client-side component with direct protocol support and performance optimization.

**Base URL**: `http://localhost:9091`

#### Key Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Universal health check |
| `/health/direct-clients` | GET | Direct protocol client status |
| `/health/protocol-detection` | GET | Protocol auto-detection status |
| `/status` | GET | Detailed status with protocol info |
| `/metrics` | GET | Prometheus metrics with protocol breakdown |
| `/api/v1/config` | GET/PUT | Configuration management |
| `/api/v1/connections` | GET | List connections (direct + gateway) |
| `/api/v1/connections/direct` | GET | Direct protocol connections |
| `/api/v1/connections/gateway` | GET | Gateway connections |
| `/api/v1/credentials` | GET/POST | Credential management |
| `/api/v1/protocols` | GET | Supported protocol capabilities |
| `/api/v1/protocols/detect` | POST | Test protocol detection |
| `/api/v1/performance` | GET | Performance metrics and optimization status |

## 🔐 Authentication Methods

### Bearer Token (JWT)
```javascript
// Example JWT payload
{
  "sub": "user-123",
  "aud": "mcp-gateway",
  "iss": "https://auth.example.com",
  "exp": 1234567890,
  "iat": 1234567800,
  "scopes": ["read", "write"]
}
```

### OAuth2
```yaml
# OAuth2 configuration
oauth2:
  authorization_url: https://auth.mcp-bridge.io/oauth/authorize
  token_url: https://auth.mcp-bridge.io/oauth/token
  scopes:
    - read: Read access to MCP resources
    - write: Write access to MCP resources
    - admin: Administrative access
```

### Mutual TLS (mTLS)
```bash
# Generate client certificate
openssl req -x509 -newkey rsa:4096 -keyout client.key -out client.crt -days 365 -nodes

# Use with curl
curl --cert client.crt --key client.key https://gateway.mcp-bridge.io/api/v1/execute
```

## 📝 Example Requests

### Execute MCP Request

#### Basic Request
```bash
curl -X POST https://gateway.mcp-bridge.io/api/v1/execute \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
      "name": "calculator",
      "arguments": {
        "operation": "add",
        "a": 5,
        "b": 3
      }
    },
    "id": "req-123"
  }'
```

#### Protocol-Specific Request
```bash
# Target specific backend protocol
curl -X POST https://gateway.mcp-bridge.io/api/v1/execute \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Target-Protocol: stdio" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
      "name": "weather.get_forecast",
      "arguments": {
        "location": "San Francisco"
      }
    },
    "id": "req-456"
  }'
```

#### Cross-Protocol Load Balancing
```bash
# Let gateway select optimal protocol
curl -X POST https://gateway.mcp-bridge.io/api/v1/execute \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Load-Balance: cross-protocol" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
      "name": "data.query",
      "arguments": {
        "query": "SELECT * FROM users LIMIT 10"
      }
    },
    "id": "req-789"
  }'
```

### WebSocket Connection
```javascript
const ws = new WebSocket('wss://gateway.mcp-bridge.io/api/v1/stream');

ws.onopen = () => {
  // Send initialization
  ws.send(JSON.stringify({
    jsonrpc: "2.0",
    method: "initialize",
    params: {
      version: "1.0.0",
      capabilities: {}
    },
    id: "init-1"
  }));
};

ws.onmessage = (event) => {
  const response = JSON.parse(event.data);
  console.log('Received:', response);
};
```

### Store Credentials (Router)
```bash
curl -X POST http://localhost:9091/api/v1/credentials \
  -H "Content-Type: application/json" \
  -d '{
    "name": "production-gateway",
    "type": "bearer",
    "gateway": "wss://gateway.mcp-bridge.io",
    "token": "YOUR_TOKEN"
  }'
```

## 🔄 Rate Limiting

Rate limits are enforced at multiple levels:

| Level | Limit | Window |
|-------|-------|--------|
| Global | 10,000 req | 1 minute |
| Per IP | 100 req | 1 minute |
| Per User | 1,000 req | 1 minute |
| WebSocket | 10 msg | 1 second |

Rate limit headers:
```http
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 999
X-RateLimit-Reset: 1640995200
```

## 🚨 Error Handling

All errors follow a consistent format:

```json
{
  "code": "ERROR_CODE",
  "message": "Human-readable error message",
  "details": {
    "field": "additional",
    "information": "here"
  }
}
```

### Common Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `BAD_REQUEST` | 400 | Invalid request parameters |
| `UNAUTHORIZED` | 401 | Missing or invalid authentication |
| `FORBIDDEN` | 403 | Insufficient permissions |
| `NOT_FOUND` | 404 | Resource not found |
| `RATE_LIMITED` | 429 | Rate limit exceeded |
| `INTERNAL_ERROR` | 500 | Internal server error |
| `SERVICE_UNAVAILABLE` | 503 | Service temporarily unavailable |

## 📊 Metrics

Both services expose Prometheus metrics:

### Key Metrics

```prometheus
# Universal Protocol Request Metrics
mcp_request_duration_seconds{method="POST",status="200",endpoint="/api/v1/execute",protocol="stdio"}
mcp_request_total{method="POST",status="200",endpoint="/api/v1/execute",backend_protocol="websocket"}
mcp_protocol_conversion_duration_seconds{from="websocket",to="stdio"}

# Connection metrics by protocol
mcp_active_connections{type="websocket",mode="direct"}
mcp_active_connections{type="stdio",mode="direct"}
mcp_active_connections{type="http",mode="direct"}
mcp_connection_duration_seconds{protocol="websocket",connection_type="direct"}

# Protocol Detection Metrics  
mcp_protocol_detection_total{protocol="stdio",result="success"}
mcp_protocol_detection_accuracy{protocol="websocket"}

# Cross-Protocol Load Balancing
mcp_load_balancer_requests_total{strategy="cross_protocol",target_protocol="stdio"}
mcp_load_balancer_latency_seconds{backend_protocol="websocket"}

# Performance Optimization Metrics
mcp_connection_pool_size{protocol="websocket"}
mcp_connection_pool_hits_total{protocol="stdio"}
mcp_memory_optimization_savings_bytes{component="object_pool"}

# Authentication metrics
mcp_auth_attempts_total{method="bearer",result="success"}
mcp_auth_duration_seconds{method="bearer"}

# Rate limiting metrics
mcp_rate_limit_exceeded_total{limit_type="per_user"}

# Circuit breaker metrics (per protocol)
mcp_circuit_breaker_state{name="stdio_backend",state="closed"}
mcp_circuit_breaker_state{name="websocket_backend",state="closed"}
```

## 🧰 Client SDKs

Official SDKs provide idiomatic interfaces for each language:

### Go
```go
import "github.com/actual-software/mcp-bridge/sdk/go"

client := mcp.NewClient("https://gateway.mcp-bridge.io", "YOUR_TOKEN")
response, err := client.Execute(ctx, &mcp.Request{
    Method: "tools/call",
    Params: map[string]interface{}{
        "name": "calculator",
        "arguments": map[string]int{"a": 5, "b": 3},
    },
})
```

### Python
```python
from mcp_bridge import Client

client = Client("https://gateway.mcp-bridge.io", token="YOUR_TOKEN")
response = client.execute(
    method="tools/call",
    params={
        "name": "calculator",
        "arguments": {"a": 5, "b": 3}
    }
)
```

### TypeScript
```typescript
import { MCPClient } from '@mcp-bridge/client';

const client = new MCPClient({
  url: 'https://gateway.mcp-bridge.io',
  token: 'YOUR_TOKEN'
});

const response = await client.execute({
  method: 'tools/call',
  params: {
    name: 'calculator',
    arguments: { a: 5, b: 3 }
  }
});
```

## 🔧 Development Tools

### API Testing with curl
```bash
# Health check
curl https://gateway.mcp-bridge.io/health

# With authentication
curl -H "Authorization: Bearer YOUR_TOKEN" \
     https://gateway.mcp-bridge.io/api/v1/execute

# With request body
curl -X POST \
     -H "Authorization: Bearer YOUR_TOKEN" \
     -H "Content-Type: application/json" \
     -d @request.json \
     https://gateway.mcp-bridge.io/api/v1/execute
```

### API Testing with Postman
Import the OpenAPI specifications into Postman:
1. Open Postman
2. Click Import → File
3. Select `api/openapi/gateway.yaml` or `api/openapi/router.yaml`
4. Configure environment variables for authentication

### Generate Client Code
```bash
# Generate Go client
openapi-generator generate -i api/openapi/gateway.yaml -g go -o sdk/go/

# Generate Python client
openapi-generator generate -i api/openapi/gateway.yaml -g python -o sdk/python/

# Generate TypeScript client
openapi-generator generate -i api/openapi/gateway.yaml -g typescript-axios -o sdk/typescript/
```

## 📚 Additional Resources

- [Gateway Documentation](../services/gateway/README.md)
- [Router Documentation](../services/router/README.md)
- [Authentication Guide](../docs/authentication.md)
- [Security Best Practices](../docs/SECURITY.md)
- [Troubleshooting Guide](../docs/troubleshooting.md)
- [Model Context Protocol Spec](https://modelcontextprotocol.io)

## 🤝 Support

- **Issues**: [GitHub Issues](https://github.com/actual-software/mcp-bridge/issues)
- **Discussions**: [GitHub Discussions](https://github.com/actual-software/mcp-bridge/discussions)
- **Security**: Report security issues to security@mcp-bridge.io