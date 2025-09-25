# MCP Gateway Binary Wire Protocol Specification

## Overview

The MCP Gateway Binary Wire Protocol is a frame-based protocol designed for efficient TCP communication between MCP clients and the gateway. It provides lower overhead compared to WebSocket/JSON-RPC while maintaining compatibility with standard MCP message formats.

## Protocol Version

Current protocol version: `0x0001`
Minimum supported version: `0x0001`
Maximum supported version: `0x0001`

## Frame Format

Each message is transmitted as a frame with the following structure:

```
+----------------+----------+-------------+-------------+-----------------+
| Magic Bytes    | Version  | Message Type| Payload Len | Payload         |
| (4 bytes)      | (2 bytes)| (2 bytes)   | (4 bytes)   | (variable)      |
+----------------+----------+-------------+-------------+-----------------+
```

### Field Descriptions

1. **Magic Bytes** (4 bytes)
   - Value: `0x4D435042` ("MCPB" in ASCII)
   - Used to identify MCP binary protocol frames
   - Big-endian byte order

2. **Version** (2 bytes)
   - Current version: `0x0001`
   - Big-endian byte order
   - Allows for future protocol upgrades

3. **Message Type** (2 bytes)
   - Identifies the type of message in the payload
   - Big-endian byte order
   - See Message Types section below

4. **Payload Length** (4 bytes)
   - Length of the payload in bytes
   - Big-endian byte order
   - Maximum payload size: 10MB (10,485,760 bytes)

5. **Payload** (variable length)
   - Message-specific data
   - Format depends on message type
   - Usually JSON-encoded for MCP messages

### Header Size

Fixed header size: 12 bytes

## Message Types

| Type | Value    | Description | Payload Format |
|------|----------|-------------|----------------|
| Request | `0x0001` | MCP request | JSON-encoded `mcp.Request` |
| Response | `0x0002` | MCP response | JSON-encoded `mcp.Response` |
| Control | `0x0003` | Control message | JSON object with control data |
| HealthCheck | `0x0004` | Health check ping/pong | Empty |
| Error | `0x0005` | Protocol error | UTF-8 error message string |
| VersionNegotiation | `0x0006` | Version negotiation request | JSON-encoded version info |
| VersionAck | `0x0007` | Version negotiation response | JSON-encoded agreed version |

## Message Flow

### Connection Establishment

1. Client connects to TCP port (default: gateway port + 1000)
2. Client MUST send version negotiation as first message
3. Server validates version compatibility and responds with agreed version
4. Client MUST send initialization request with authentication
5. Server validates authentication and creates session
6. Server responds with session information
7. Connection is ready for normal message exchange

### Version Negotiation

The first message MUST be a version negotiation request:

```json
{
  "min_version": 1,
  "max_version": 1,
  "preferred_version": 1,
  "supported_versions": [1]
}
```

Server response:
```json
{
  "agreed_version": 1
}
```

If version negotiation fails, the server will send an Error message and close the connection.

### Authentication

After successful version negotiation, the client MUST send an initialization request:

```json
{
  "jsonrpc": "2.0",
  "id": "init-1",
  "method": "initialize",
  "params": {
    "token": "<authentication-token>"
  }
}
```

Response on success:
```json
{
  "jsonrpc": "2.0",
  "id": "init-1",
  "result": {
    "sessionId": "<session-id>",
    "expiresAt": "<ISO-8601-timestamp>"
  }
}
```

### Per-Message Authentication (Optional)

When per-message authentication is enabled, requests can include an auth token in a wrapper message:

```json
{
  "auth_token": "<message-auth-token>",
  "request": {
    "jsonrpc": "2.0",
    "id": "msg-1",
    "method": "tools/call",
    "params": { ... }
  }
}
```

### Health Checks

- Server sends health check frames on read timeout (default: 1 minute)
- Client should respond with health check frame
- Empty payload for both ping and pong

### Graceful Shutdown

1. Server sends control message:
   ```json
   {
     "command": "shutdown"
   }
   ```

2. Client should:
   - Stop sending new requests
   - Wait for pending responses
   - Send acknowledgment:
   ```json
   {
     "command": "shutdown_ack",
     "status": "ok"
   }
   ```

3. Connection is closed

## Error Handling

### Protocol Errors

Protocol-level errors are sent as Error frames (type `0x0005`) with a UTF-8 encoded error message.

### Application Errors

MCP application errors are sent as normal Response frames with an error field:

```json
{
  "jsonrpc": "2.0",
  "id": "request-id",
  "error": {
    "code": -32603,
    "message": "Error description"
  }
}
```

## Implementation Notes

### Encoding

- All multi-byte integers use big-endian byte order
- JSON payloads must be valid UTF-8
- Maximum frame size: 10MB + 12 bytes (header)

### Concurrency

- Multiple requests can be in-flight simultaneously
- Responses may arrive out of order
- Use request IDs to match responses

### Connection Management

- Connections are persistent
- Automatic health checks prevent idle timeout
- Server tracks active connections for graceful shutdown

## Example Client Implementation

### Go Example

```go
// Read a frame
func readFrame(conn net.Conn) (*Frame, error) {
    // Read header
    header := make([]byte, 12)
    if _, err := io.ReadFull(conn, header); err != nil {
        return nil, err
    }
    
    // Verify magic bytes
    magic := binary.BigEndian.Uint32(header[0:4])
    if magic != 0x4D435042 {
        return nil, errors.New("invalid magic bytes")
    }
    
    // Parse header
    version := binary.BigEndian.Uint16(header[4:6])
    msgType := binary.BigEndian.Uint16(header[6:8])
    payloadLen := binary.BigEndian.Uint32(header[8:12])
    
    // Read payload
    payload := make([]byte, payloadLen)
    if _, err := io.ReadFull(conn, payload); err != nil {
        return nil, err
    }
    
    return &Frame{
        Version:     version,
        MessageType: MessageType(msgType),
        Payload:     payload,
    }, nil
}

// Write a frame
func writeFrame(conn net.Conn, frame *Frame) error {
    buf := new(bytes.Buffer)
    
    // Write header
    binary.Write(buf, binary.BigEndian, uint32(0x4D435042)) // Magic
    binary.Write(buf, binary.BigEndian, frame.Version)
    binary.Write(buf, binary.BigEndian, frame.MessageType)
    binary.Write(buf, binary.BigEndian, uint32(len(frame.Payload)))
    
    // Write payload
    buf.Write(frame.Payload)
    
    // Send to connection
    _, err := conn.Write(buf.Bytes())
    return err
}
```

## Security Considerations

1. **Version Negotiation**: All connections MUST perform version negotiation before authentication
2. **Authentication**: Always validate tokens before creating sessions
3. **TLS**: Use TLS for production deployments (tcps:// scheme)
4. **Payload Size**: Maximum payload size enforced at 10MB to prevent DoS
5. **Rate Limiting**: Per-user and per-IP rate limits are enforced
6. **Input Validation**: All inputs are validated:
   - Request IDs: Max 255 chars, alphanumeric + hyphens/underscores
   - Methods: Max 255 chars, alphanumeric + dots/slashes/hyphens/underscores
   - Namespaces: Max 255 chars, alphanumeric + dots/hyphens/underscores
   - Tokens: Max 4096 chars, validated for injection patterns
   - All strings must be valid UTF-8
7. **Connection Limits**: Per-IP connection limits prevent resource exhaustion
8. **Header Validation**: Protocol version must be within supported range

## Performance Characteristics

- Lower overhead than WebSocket (no HTTP handshake, no WebSocket framing)
- Binary header is more efficient than text protocols
- Supports message pipelining for better throughput
- Direct TCP connection reduces latency

## Health Checks

The binary protocol supports TCP-based health checks:

### Main TCP Port Health Checks
The main TCP port responds to health check messages (0x0004) and returns the current connection state.

### Dedicated Health Check Port
For load balancers and monitoring systems, you can configure a dedicated TCP health check port:

```yaml
server:
  tcp_health_port: 9002  # Dedicated health check port
```

Health check request format:
```json
{
  "check": "simple"  // Options: "simple" (boolean), "detailed" (full status)
}
```

Health check response format:
```json
{
  "healthy": true,
  "checks": {
    "service_discovery": {"healthy": true, "message": "..."},
    "endpoints": {"healthy": true, "message": "..."},
    "tcp_service": {"healthy": true, "message": "..."}
  },
  "timestamp": 1234567890
}
```

## Metrics

The following metrics are collected for the binary protocol:

- `mcp_gateway_tcp_connections_total`: Total TCP connections
- `mcp_gateway_tcp_connections_active`: Currently active connections
- `mcp_gateway_tcp_messages_total`: Messages by type and direction (including health_check)
- `mcp_gateway_tcp_bytes_total`: Bytes transferred by direction
- `mcp_gateway_tcp_protocol_errors_total`: Protocol errors by type
- `mcp_gateway_tcp_message_duration_seconds`: Message processing duration