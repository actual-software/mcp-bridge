# Migration Guide

This guide helps you migrate from older versions of MCP Router to the latest version with enhanced gateway compatibility.

## Breaking Changes

### Protocol Support

The latest version adds support for TCP/Binary protocol in addition to WebSocket:
- **Old**: Only WebSocket (`ws://`, `wss://`)
- **New**: WebSocket and TCP (`ws://`, `wss://`, `tcp://`, `tcps://`, `tcp+tls://`)

### Initialization Behavior (v2.0+)

The router no longer auto-initializes when connecting to the gateway:
- **Old**: Router automatically sent initialization request upon connection
- **New**: Router operates in passive mode, forwarding client's initialization request
- **Impact**: No changes needed for Claude CLI users (transparent change)
- **Benefit**: Prevents duplicate initialization issues

### Configuration Changes

#### Gateway URL Format
```yaml
# Old
gateway:
  url: "gateway.example.com:8443"  # Assumed wss://

# New - must specify protocol
gateway:
  url: "wss://gateway.example.com:8443"  # WebSocket
  # OR
  url: "tcps://gateway.example.com:9443"  # TCP with TLS
```

#### Authentication Configuration
```yaml
# Old
gateway:
  auth:
    bearer_token: "your-token"  # Direct token in config

# New - more secure options
gateway:
  auth:
    type: bearer
    token_env: MCP_AUTH_TOKEN    # From environment
    # OR
    token_file: ~/.mcp/token     # From file
```

#### New Required Fields
```yaml
# These fields are now required for production use
gateway:
  connection:
    reconnect:
      max_attempts: 10  # Was unlimited, now configurable
      
local:
  request_timeout_ms: 30000  # Was no timeout
```

## Feature Additions

### 1. Per-Message Authentication

All messages now include authentication tokens for enhanced security:
```yaml
# Automatically enabled for bearer and OAuth2
gateway:
  auth:
    type: bearer
    token_env: MCP_AUTH_TOKEN
```

### 2. Rate Limiting

New rate limiting configuration:
```yaml
local:
  rate_limit:
    requests_per_second: 10.0
    burst: 20
```

### 3. OAuth2 Support

Full OAuth2 client credentials flow:
```yaml
gateway:
  auth:
    type: oauth2
    client_id: "your-client-id"
    client_secret_env: OAUTH_CLIENT_SECRET
    token_endpoint: "https://auth.example.com/token"
    grant_type: "client_credentials"
```

### 4. Metrics Endpoint

Prometheus metrics now available:
```yaml
metrics:
  enabled: true
  endpoint: localhost:9091
```

## Step-by-Step Migration

### 1. Backup Current Configuration

```bash
cp ~/.config/claude-cli/mcp-router.yaml ~/.config/claude-cli/mcp-router.yaml.backup
```

### 2. Update Binary

```bash
# Download latest version
curl -sSL https://raw.githubusercontent.com/poiley/mcp-bridge/main/services/router/install.sh | bash
```

### 3. Update Configuration

#### Minimal Changes (Quick Migration)

Add protocol to your gateway URL:
```yaml
gateway:
  url: "wss://your-gateway.example.com:8443"  # Add wss:// prefix
```

#### Recommended Changes

1. **Move tokens to environment**:
   ```bash
   export MCP_AUTH_TOKEN="your-token-here"
   ```

2. **Update config to use token_env**:
   ```yaml
   gateway:
     auth:
       type: bearer
       token_env: MCP_AUTH_TOKEN
   ```

3. **Add timeouts and limits**:
   ```yaml
   local:
     request_timeout_ms: 30000
     max_queued_requests: 100  # New in v2.0+
     rate_limit:
       requests_per_second: 10.0
       burst: 20
   ```

### 4. Test Connection

```bash
# Test with debug logging
MCP_LOGGING_LEVEL=debug mcp-router
```

### 5. Update Claude CLI Configuration

No changes needed - the stdio interface remains the same:
```yaml
# In Claude CLI config
mcp_server_command: ["mcp-router"]
```

## Configuration Examples

### From WebSocket-only to Multi-Protocol

**Before**:
```yaml
gateway:
  url: "gateway.example.com"
  bearer_token: "secret-token"
  timeout: 5000
```

**After**:
```yaml
gateway:
  url: "wss://gateway.example.com:8443"
  auth:
    type: bearer
    token_env: MCP_AUTH_TOKEN
  connection:
    timeout_ms: 5000
    reconnect:
      max_attempts: 10
```

### Adding TCP Support

For gateways that support TCP/Binary:
```yaml
# Keep existing WebSocket config
gateway:
  url: "wss://gateway.example.com:8443"
  
# OR switch to TCP for better performance
gateway:
  url: "tcps://gateway.example.com:9443"
```

### Enabling New Features

Add these sections to enable new features:
```yaml
# Rate limiting
local:
  rate_limit:
    requests_per_second: 50.0
    burst: 100

# Metrics
metrics:
  enabled: true
  endpoint: ":9091"  # Prometheus format

# Advanced debugging
advanced:
  debug:
    log_frames: false  # Set true for protocol debugging
    save_failures: true
    failure_dir: "/var/log/mcp-failures"
```

## Rollback Procedure

If you need to rollback:

1. **Restore old binary**:
   ```bash
   # If you have the old version
   sudo mv /usr/local/bin/mcp-router.old /usr/local/bin/mcp-router
   ```

2. **Restore configuration**:
   ```bash
   cp ~/.config/claude-cli/mcp-router.yaml.backup ~/.config/claude-cli/mcp-router.yaml
   ```

3. **Restart Claude CLI**

## Common Migration Issues

### Issue: "invalid gateway URL"
**Cause**: Missing protocol in URL
**Fix**: Add `wss://` or `tcps://` prefix

### Issue: "no auth token found"
**Cause**: Token not in environment or file
**Fix**: Set `MCP_AUTH_TOKEN` environment variable

### Issue: "connection refused"
**Cause**: Wrong protocol or port
**Fix**: Verify gateway supports the protocol and port

### Issue: Rate limiting errors
**Cause**: Default rate limits too low
**Fix**: Increase `requests_per_second` and `burst` values

## New Features in v2.0+

### Request Queueing
The router now queues requests when not connected to the gateway:
- Configurable queue size via `max_queued_requests`
- Automatic processing when connection is established
- Backpressure handling when queue is full
- No polling loops - fully event-driven

### Event-Driven Architecture
All state management is now event-driven:
- No polling loops anywhere in the codebase
- State changes trigger immediate actions
- More efficient CPU usage
- Lower latency for state transitions

### Passive Initialization Mode
The router no longer auto-initializes:
- Client controls the initialization flow
- Prevents duplicate initialization requests
- Router acts as transparent proxy
- Better compatibility with all MCP clients

## Support

For migration assistance:
- Check [Troubleshooting Guide](../README.md#troubleshooting)
- Open an issue on GitHub
- Enable debug logging for detailed diagnostics