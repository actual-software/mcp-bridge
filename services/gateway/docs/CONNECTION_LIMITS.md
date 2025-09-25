# Connection Limits and DoS Protection

MCP Gateway provides connection limiting to protect against resource exhaustion and basic DoS attacks while being permissive enough for enterprise and development use cases.

## Configuration

Connection limits are configured in the server section:

```yaml
server:
  max_connections: 50000          # Total connections across all IPs (default: 50000)
  max_connections_per_ip: 1000    # Connections per IP address (default: 1000)
```

## Features

### Global Connection Limit
- Limits total concurrent connections across all clients
- Default: 50,000 connections
- Prevents server resource exhaustion

### Per-IP Connection Limit  
- Limits connections from a single IP address
- Default: 1,000 connections per IP
- Prevents single source from monopolizing resources
- Set to 0 to disable per-IP limiting

## Design Philosophy

The limits are intentionally very generous because:

1. **Enterprise Users**: Won't want to deny their own employees who may be behind a single NAT/proxy
2. **Self-Hosters**: Running their own services don't need strict limits
3. **Developers**: Need to iterate quickly without hitting limits during development

## Connection Tracking

- Connections are tracked in-memory using atomic counters
- IP tracking is automatically cleaned up when count reaches 0
- Both WebSocket and TCP connections are tracked
- Metrics are collected for rejected connections

## Metrics

The following metrics track connection limits:

- `mcp_gateway_connections_total`: Total active connections
- `mcp_gateway_connections_rejected_total`: Connections rejected due to limits
- `mcp_gateway_tcp_connections_total`: Total TCP connections
- `mcp_gateway_tcp_connections_active`: Currently active TCP connections

## Example Scenarios

### Development Environment
```yaml
server:
  max_connections: 1000
  max_connections_per_ip: 100  # Sufficient for local development
```

### Enterprise Deployment
```yaml
server:
  max_connections: 50000
  max_connections_per_ip: 5000  # Many users behind corporate proxy
```

### Public Service
```yaml
server:
  max_connections: 10000
  max_connections_per_ip: 50   # More restrictive for public access
```

## Implementation Details

- IP addresses are extracted from connection remote addresses
- IPv4 and IPv6 are both supported
- Connection counts are tracked using lock-free atomic operations
- Per-IP tracking uses sync.Map for concurrent access
- Graceful handling when limits are reached:
  - HTTP: Returns 503 Service Unavailable or 429 Too Many Requests
  - TCP: Immediately closes connection

## Testing

The implementation includes comprehensive tests for:
- Concurrent connection tracking
- IP address parsing
- Limit enforcement
- Cleanup on disconnect
- Thread safety

## Future Enhancements

While the current implementation provides basic DoS protection, future versions could add:
- Rate limiting per IP (requests/second)
- Geo-based connection limits
- Dynamic limit adjustment based on load
- Connection queuing with priorities
- More sophisticated DoS detection