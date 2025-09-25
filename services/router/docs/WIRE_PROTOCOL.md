# Wire Protocol Specification

This document describes the wire protocols used by MCP Router for communication with MCP Gateways.

## Overview

MCP Router supports two wire protocols:
1. **WebSocket Protocol** - JSON messages over WebSocket
2. **TCP/Binary Protocol** - Binary-framed JSON over TCP

Both protocols use the same message format but differ in transport and framing.

## Message Format

### Wire Message Structure

All messages follow this JSON structure:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": "2024-01-15T10:30:45.123Z",
  "source": "local-router",
  "target_namespace": "tools",
  "auth_token": "Bearer eyJ0eXAiOiJKV1Q...",
  "correlation_id": "req-12345",
  "metadata": {
    "client_version": "1.2.0",
    "retry_count": 0
  },
  "mcp_payload": {
    "jsonrpc": "2.0",
    "method": "tools/list",
    "params": {},
    "id": "mcp-req-001"
  }
}
```

### Field Descriptions

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique message identifier (UUID v4) |
| `timestamp` | string | Yes | ISO 8601 timestamp with milliseconds |
| `source` | string | Yes | Message origin identifier |
| `target_namespace` | string | No | Target service namespace |
| `auth_token` | string | No | Per-message authentication token |
| `correlation_id` | string | No | Request correlation for tracing |
| `metadata` | object | No | Additional message metadata |
| `mcp_payload` | object | Yes | MCP protocol message |

### Namespace Extraction

Target namespace is determined by the MCP method:
- `initialize` → `system`
- `tools/*` → `system`
- `namespace.method` → `namespace`
- Default → `default`

## WebSocket Protocol

### Connection Establishment

1. **HTTP Upgrade Request**:
   ```http
   GET /mcp HTTP/1.1
   Host: gateway.example.com:8443
   Upgrade: websocket
   Connection: Upgrade
   Sec-WebSocket-Version: 13
   Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==
   Authorization: Bearer <token>
   ```

2. **HTTP Upgrade Response**:
   ```http
   HTTP/1.1 101 Switching Protocols
   Upgrade: websocket
   Connection: Upgrade
   Sec-WebSocket-Accept: HSmrc0sMlYUkAGmm5OPpG2HaGWk=
   ```

### Message Exchange

Messages are sent as WebSocket text frames containing JSON:

```javascript
// Client → Server
{
  "id": "msg-001",
  "timestamp": "2024-01-15T10:30:45.123Z",
  "source": "local-router",
  "auth_token": "Bearer token-here",
  "mcp_payload": {
    "jsonrpc": "2.0",
    "method": "initialize",
    "params": {
      "protocolVersion": "1.0",
      "clientInfo": {
        "name": "mcp-local-router",
        "version": "2.0.0"
      }
    },
    "id": "init-001"
  }
}

// Server → Client
{
  "id": "msg-001-resp",
  "timestamp": "2024-01-15T10:30:45.234Z",
  "source": "gateway",
  "mcp_payload": {
    "jsonrpc": "2.0",
    "result": {
      "protocolVersion": "1.0",
      "serverInfo": {
        "name": "mcp-gateway",
        "version": "2.1.0"
      },
      "capabilities": {
        "tools": true,
        "resources": true
      }
    },
    "id": "init-001"
  }
}
```

### Keep-Alive

WebSocket ping/pong frames are used for keep-alive:
- Client sends ping every 30 seconds
- Server must respond with pong
- Connection closed if no pong received within 60 seconds

## TCP/Binary Protocol

### Frame Format

Binary protocol uses length-prefixed frames:

```
+--------+--------+--------+----------------+
| Magic  | Type   | Length | Payload        |
| 2 bytes| 1 byte | 4 bytes| Variable       |
+--------+--------+--------+----------------+
```

### Field Descriptions

| Field | Size | Description |
|-------|------|-------------|
| Magic | 2 bytes | `0x4D43` ("MC" in ASCII) |
| Type | 1 byte | Frame type (see below) |
| Length | 4 bytes | Payload length (big-endian) |
| Payload | Variable | Frame payload |

### Frame Types

| Type | Value | Description | Payload Format |
|------|-------|-------------|----------------|
| DATA | 0x01 | Message frame | JSON WireMessage |
| PING | 0x02 | Keep-alive ping | Empty or timestamp |
| PONG | 0x03 | Keep-alive response | Echo of ping payload |
| ERROR | 0x04 | Error frame | JSON error object |
| CLOSE | 0x05 | Close connection | Optional reason string |

### Connection Flow

1. **TCP Connection**:
   ```
   Client → Server: TCP SYN
   Server → Client: TCP SYN-ACK
   Client → Server: TCP ACK
   ```

2. **TLS Handshake** (if using tcps://):
   ```
   Client → Server: ClientHello
   Server → Client: ServerHello, Certificate
   Client → Server: ClientKeyExchange
   Both: ChangeCipherSpec, Finished
   ```

3. **Initial Message**:
   ```
   Client → Server: DATA frame with initialize request
   Server → Client: DATA frame with initialize response
   ```

### Binary Encoding Example

Initialize request frame:
```
4D 43 01 00 00 01 5C    // Magic(MC) Type(DATA) Length(348)
7B 22 69 64 22 3A ...   // JSON payload
```

Ping frame:
```
4D 43 02 00 00 00 08    // Magic(MC) Type(PING) Length(8)
00 00 01 8D 9F 3A 4B 28 // Timestamp (8 bytes)
```

## Authentication

### Bearer Token

Included in `auth_token` field of every message:
```json
{
  "auth_token": "Bearer eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9..."
}
```

### OAuth2

Token obtained via OAuth2 flow and included as bearer token:
```json
{
  "auth_token": "Bearer ya29.a0ARrdaM..."
}
```

### mTLS

Client certificate presented during TLS handshake. No `auth_token` field required.

## Error Handling

### Error Response Format

```json
{
  "id": "error-001",
  "timestamp": "2024-01-15T10:30:45.456Z",
  "source": "gateway",
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "Rate limit exceeded",
    "details": {
      "limit": 100,
      "window": "1m",
      "retry_after": 45
    }
  }
}
```

### Standard Error Codes

| Code | Description | HTTP Equivalent |
|------|-------------|-----------------|
| INVALID_REQUEST | Malformed request | 400 |
| UNAUTHORIZED | Authentication failed | 401 |
| FORBIDDEN | Insufficient permissions | 403 |
| NOT_FOUND | Resource not found | 404 |
| RATE_LIMIT_EXCEEDED | Too many requests | 429 |
| INTERNAL_ERROR | Server error | 500 |
| SERVICE_UNAVAILABLE | Service down | 503 |

## Protocol Negotiation

### Version Negotiation

Client includes supported versions in initialize:
```json
{
  "mcp_payload": {
    "method": "initialize",
    "params": {
      "protocolVersion": "1.0",
      "supportedVersions": ["1.0", "1.1", "2.0"]
    }
  }
}
```

Server responds with selected version:
```json
{
  "mcp_payload": {
    "result": {
      "protocolVersion": "1.0",
      "serverInfo": {
        "supportedVersions": ["1.0", "1.1"]
      }
    }
  }
}
```

### Capability Discovery

Server advertises capabilities in initialize response:
```json
{
  "capabilities": {
    "tools": true,
    "resources": true,
    "streaming": false,
    "binary_protocol": true,
    "per_message_auth": true,
    "rate_limiting": {
      "enabled": true,
      "default_limit": 100,
      "window": "1m"
    }
  }
}
```

## Performance Considerations

### Message Size Limits

- Maximum frame size: 16 MB
- Recommended message size: < 1 MB
- Large payloads should be chunked

### Compression

Binary protocol supports optional gzip compression:
```
+--------+--------+--------+--------+----------------+
| Magic  | Type   | Flags  | Length | Payload        |
+--------+--------+--------+--------+----------------+
                     ^
                     Bit 0: Compressed (1) or not (0)
```

### Connection Pooling

TCP protocol supports multiple concurrent requests:
- Single TCP connection carries multiple message streams
- Messages correlated via `id` field
- No head-of-line blocking

## Security Considerations

### TLS Requirements

- Minimum TLS 1.2, prefer TLS 1.3
- Strong cipher suites only
- Certificate validation required
- SNI support required

### Message Integrity

- All messages include timestamp for replay protection
- Correlation IDs prevent response confusion
- Per-message auth prevents token replay

## Examples

### Complete Request/Response Flow

1. **Client sends tool list request**:
```json
{
  "id": "req-001",
  "timestamp": "2024-01-15T10:30:45.789Z",
  "source": "local-router",
  "target_namespace": "system",
  "auth_token": "Bearer token123",
  "mcp_payload": {
    "jsonrpc": "2.0",
    "method": "tools/list",
    "id": "tools-001"
  }
}
```

2. **Server responds with tool list**:
```json
{
  "id": "resp-001",
  "timestamp": "2024-01-15T10:30:45.890Z",
  "source": "gateway",
  "correlation_id": "req-001",
  "mcp_payload": {
    "jsonrpc": "2.0",
    "result": {
      "tools": [
        {
          "name": "calculator",
          "description": "Perform calculations"
        }
      ]
    },
    "id": "tools-001"
  }
}
```

### Error Response Example

```json
{
  "id": "resp-002",
  "timestamp": "2024-01-15T10:30:46.123Z",
  "source": "gateway",
  "correlation_id": "req-002",
  "mcp_payload": {
    "jsonrpc": "2.0",
    "error": {
      "code": -32601,
      "message": "Method not found",
      "data": {
        "method": "unknown/method"
      }
    },
    "id": "unknown-001"
  }
}
```