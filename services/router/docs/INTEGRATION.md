# Gateway Integration Guide

This guide covers integrating MCP Router with modern MCP gateways that support advanced features like binary protocols, per-message authentication, and enhanced observability.

## Gateway Compatibility

MCP Router is compatible with:
- **MCP Gateway v2.0+** - Full feature support
- **MCP Gateway v1.x** - WebSocket only, limited features
- **Custom Gateways** - Must implement MCP wire protocol

## Protocol Selection

### Choosing the Right Protocol

| Protocol | Use When | Advantages | Considerations |
|----------|----------|------------|----------------|
| WebSocket (`wss://`) | Default choice | Wide compatibility, firewall-friendly | Higher overhead |
| TCP/Binary (`tcps://`) | Performance critical | Lower latency, efficient encoding | May require firewall rules |

### Protocol-Specific Configuration

#### WebSocket Configuration
```yaml
gateway:
  url: "wss://gateway.example.com:8443/mcp"
  connection:
    timeout_ms: 5000
    keepalive_interval_ms: 30000  # HTTP keepalive
```

#### TCP/Binary Configuration
```yaml
gateway:
  url: "tcps://gateway.example.com:9443"
  connection:
    timeout_ms: 5000
    keepalive_interval_ms: 30000  # TCP keepalive
```

## Authentication Integration

### 1. Bearer Token with Per-Message Auth

Modern gateways support per-message authentication for enhanced security:

```yaml
gateway:
  auth:
    type: bearer
    token_env: MCP_AUTH_TOKEN
```

The router automatically:
- Includes the token in every message's `AuthToken` field
- Handles token rotation if configured at gateway

### 2. OAuth2 Integration

For gateways with OAuth2 support:

```yaml
gateway:
  auth:
    type: oauth2
    client_id: "mcp-local-router"
    client_secret_env: OAUTH_CLIENT_SECRET
    token_endpoint: "https://auth.example.com/oauth2/token"
    grant_type: "client_credentials"
    scopes: ["mcp.read", "mcp.write", "mcp.admin"]
```

Features:
- Automatic token acquisition on startup
- Token refresh before expiration
- Retry with backoff on auth failures

### 3. mTLS (Mutual TLS)

For zero-trust environments:

```yaml
gateway:
  auth:
    type: mtls
    client_cert: /etc/mcp/client.crt
    client_key: /etc/mcp/client.key
  tls:
    ca_cert_path: /etc/mcp/ca.crt
    verify: true
```

## Gateway Features

### Binary Protocol Frame Format

When using TCP/Binary protocol, messages use this format:

```
+--------+--------+----------------+
| Magic  | Length | Payload        |
| 2 bytes| 4 bytes| Variable       |
+--------+--------+----------------+
```

- **Magic**: `0x4D43` (MC in ASCII)
- **Length**: Big-endian uint32
- **Payload**: JSON-encoded MCP message

### Wire Message Format

All protocols use this enhanced wire format:

```json
{
  "id": "unique-request-id",
  "timestamp": "2024-01-15T10:30:00Z",
  "source": "local-router",
  "target_namespace": "tools",
  "auth_token": "bearer-token-here",  // New field
  "mcp_payload": {
    "jsonrpc": "2.0",
    "method": "tools/list",
    "id": "req-123"
  }
}
```

### Health Checks

The router supports gateway health checks:

#### HTTP Health Endpoint
```bash
curl https://gateway.example.com/health
```

#### Binary Protocol Health
The router sends periodic ping frames (type 0x02) over TCP connections.

## Rate Limiting

### Client-Side Rate Limiting

Configure local rate limits:

```yaml
local:
  rate_limit:
    requests_per_second: 50.0
    burst: 100
```

### Gateway Rate Limit Headers

The router respects gateway rate limit headers:
- `X-RateLimit-Limit`: Request limit
- `X-RateLimit-Remaining`: Requests remaining
- `X-RateLimit-Reset`: Reset timestamp

## Observability

### Metrics Collection

The router collects and exposes metrics compatible with gateway monitoring:

```yaml
metrics:
  enabled: true
  endpoint: ":9091"
  labels:
    environment: "production"
    region: "us-east-1"
```

### Distributed Tracing

For gateways with tracing support:

```yaml
gateway:
  tracing:
    enabled: true
    endpoint: "http://jaeger:14268/api/traces"
    service_name: "mcp-local-router"
```

### Correlation IDs

All requests include correlation IDs for end-to-end tracking:
- Router generates unique request IDs
- IDs are preserved through gateway
- Visible in logs and metrics

## Advanced Integration

### Circuit Breaker

Protect against gateway failures:

```yaml
advanced:
  circuit_breaker:
    failure_threshold: 5      # Failures to open
    success_threshold: 2      # Successes to close
    timeout_seconds: 30       # Time before retry
```

### Request Deduplication

Prevent duplicate requests:

```yaml
advanced:
  deduplication:
    enabled: true
    cache_size: 1000
    ttl_seconds: 60
```

### Connection Pooling

For high-throughput scenarios:

```yaml
gateway:
  connection:
    pool_size: 10            # Number of connections
    max_idle_per_host: 5     # Idle connections
```

## Gateway-Specific Configurations

### Kubernetes-Hosted Gateway

```yaml
gateway:
  url: "wss://mcp-gateway.namespace.svc.cluster.local:8443"
  tls:
    verify: true
    ca_cert_path: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
```

### AWS API Gateway

```yaml
gateway:
  url: "wss://abcdef.execute-api.us-east-1.amazonaws.com/prod"
  auth:
    type: bearer
    token_env: AWS_API_KEY
```

### Azure Application Gateway

```yaml
gateway:
  url: "wss://mcp-gateway.azurewebsites.net"
  auth:
    type: oauth2
    client_id: "azure-app-id"
    client_secret_env: AZURE_CLIENT_SECRET
    token_endpoint: "https://login.microsoftonline.com/tenant/oauth2/v2.0/token"
```

## Testing Integration

### 1. Connection Test

```bash
# Test basic connectivity
mcp-router test-connection
```

### 2. Authentication Test

```bash
# Test auth flow
MCP_LOGGING_LEVEL=debug mcp-router test-auth
```

### 3. End-to-End Test

```bash
# Send test request
echo '{"jsonrpc":"2.0","method":"initialize","id":"test-1"}' | mcp-router
```

### 4. Load Test

```bash
# Test rate limiting and performance
mcp-router benchmark --requests 1000 --concurrent 10
```

## Troubleshooting Integration

### Common Issues

1. **Protocol Mismatch**
   - Symptom: Connection immediately closed
   - Fix: Verify gateway supports chosen protocol

2. **Authentication Failures**
   - Symptom: 401/403 errors
   - Fix: Check token expiration and scopes

3. **TLS Issues**
   - Symptom: Certificate errors
   - Fix: Verify CA cert and hostname

4. **Rate Limiting**
   - Symptom: 429 errors
   - Fix: Adjust local rate limits or request quota increase

### Debug Mode

Enable comprehensive debugging:

```yaml
advanced:
  debug:
    log_frames: true         # Log all protocol frames
    save_failures: true      # Save failed requests
    failure_dir: "/tmp/mcp-debug"
```

## Best Practices

1. **Use TLS**: Always use `wss://` or `tcps://` in production
2. **Enable Metrics**: Monitor router and gateway health
3. **Set Timeouts**: Configure appropriate timeouts for your use case
4. **Rate Limit**: Set client-side limits below gateway limits
5. **Handle Errors**: Implement proper error handling in Claude CLI
6. **Monitor Logs**: Aggregate logs from router and gateway
7. **Test Failover**: Verify reconnection logic works correctly