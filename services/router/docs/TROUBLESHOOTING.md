# Troubleshooting Guide

This comprehensive guide helps diagnose and resolve common issues with MCP Router.

## Quick Diagnostics

Run this command for a quick health check:
```bash
mcp-router diagnose
```

This will check:
- Configuration validity
- Gateway connectivity
- Authentication status
- TLS certificate validity
- Resource availability

## Connection Issues

### Cannot Connect to Gateway

#### Symptom
```
ERROR: failed to connect to gateway: dial tcp: connection refused
```

#### Diagnosis Steps

1. **Check gateway URL format**:
   ```bash
   # Verify URL in config
   grep "url:" ~/.config/claude-cli/mcp-router.yaml
   ```
   
   Ensure URL includes protocol:
   - ✅ `wss://gateway.example.com:8443`
   - ❌ `gateway.example.com:8443`

2. **Test network connectivity**:
   ```bash
   # For WebSocket
   curl -v https://gateway.example.com:8443/health
   
   # For TCP
   nc -zv gateway.example.com 9443
   
   # DNS resolution
   nslookup gateway.example.com
   ```

3. **Check firewall rules**:
   ```bash
   # Outbound connections
   sudo iptables -L OUTPUT -n | grep 8443
   
   # Test with telnet
   telnet gateway.example.com 8443
   ```

#### Solutions

- **Wrong protocol**: Update URL to include `wss://` or `tcps://`
- **Firewall blocking**: Add outbound rule for gateway port
- **DNS failure**: Use IP address or fix DNS resolution
- **Gateway down**: Contact gateway administrator

### TLS/Certificate Errors

#### Symptom
```
ERROR: x509: certificate signed by unknown authority
```

#### Diagnosis

1. **Check certificate details**:
   ```bash
   # View server certificate
   openssl s_client -connect gateway.example.com:8443 -servername gateway.example.com < /dev/null
   
   # Check expiration
   echo | openssl s_client -connect gateway.example.com:8443 2>/dev/null | openssl x509 -noout -dates
   ```

2. **Verify CA certificate**:
   ```bash
   # If using custom CA
   openssl verify -CAfile /path/to/ca.crt /path/to/server.crt
   ```

#### Solutions

- **Self-signed certificate**: Add CA to trusted store or set `ca_cert_path`
- **Expired certificate**: Contact administrator to renew
- **Hostname mismatch**: Ensure URL matches certificate CN/SAN
- **Development only**: Set `tls.verify: false` (NEVER in production)

### WebSocket Specific Issues

#### Symptom
```
ERROR: websocket: bad handshake
```

#### Diagnosis

1. **Check HTTP headers**:
   ```bash
   curl -v -H "Upgrade: websocket" \
        -H "Connection: Upgrade" \
        -H "Sec-WebSocket-Version: 13" \
        -H "Sec-WebSocket-Key: x3JJHMbDL1EzLkh9GBhXDw==" \
        https://gateway.example.com:8443/
   ```

2. **Verify proxy settings**:
   ```bash
   # Check for proxy interference
   env | grep -i proxy
   ```

#### Solutions

- **Proxy interference**: Configure `NO_PROXY` or use TCP protocol
- **Path required**: Add path to URL (e.g., `/mcp` or `/ws`)
- **Auth required**: Ensure auth headers are sent

## Authentication Issues

### Invalid or Expired Token

#### Symptom
```
ERROR: authentication failed: 401 Unauthorized
```

#### Diagnosis

1. **Check token presence**:
   ```bash
   # Environment variable
   echo $MCP_AUTH_TOKEN | cut -c1-10...
   
   # File permissions
   ls -la ~/.mcp/token
   ```

2. **Validate JWT token** (if applicable):
   ```bash
   # Decode JWT
   echo $MCP_AUTH_TOKEN | cut -d. -f2 | base64 -d | jq
   
   # Check expiration
   echo $MCP_AUTH_TOKEN | cut -d. -f2 | base64 -d | jq '.exp' | xargs -I{} date -d @{}
   ```

3. **Test authentication**:
   ```bash
   # Direct API test
   curl -H "Authorization: Bearer $MCP_AUTH_TOKEN" \
        https://gateway.example.com/api/v1/health
   ```

#### Solutions

- **Missing token**: Set `MCP_AUTH_TOKEN` environment variable
- **Expired token**: Refresh or obtain new token
- **Wrong format**: Ensure "Bearer " prefix if required
- **Insufficient permissions**: Check token scopes/roles

### OAuth2 Issues

#### Symptom
```
ERROR: failed to obtain access token: 400 Bad Request
```

#### Diagnosis

1. **Test OAuth2 endpoint**:
   ```bash
   curl -X POST https://auth.example.com/token \
     -d "grant_type=client_credentials" \
     -d "client_id=$CLIENT_ID" \
     -d "client_secret=$CLIENT_SECRET" \
     -v
   ```

2. **Check configuration**:
   ```bash
   # Verify all OAuth2 fields are set
   grep -A5 "oauth2" ~/.config/claude-cli/mcp-router.yaml
   ```

#### Solutions

- **Invalid credentials**: Verify client ID and secret
- **Wrong endpoint**: Check token endpoint URL
- **Missing scopes**: Add required scopes to request
- **Grant type mismatch**: Use correct grant_type

## Performance Issues

### High Latency

#### Diagnosis

1. **Check metrics**:
   ```bash
   # Request duration
   curl -s http://localhost:9091/metrics | grep mcp_router_request_duration_seconds
   
   # Active connections
   curl -s http://localhost:9091/metrics | grep mcp_router_active_connections
   ```

2. **Network latency**:
   ```bash
   # Ping test (ICMP)
   ping -c 10 gateway.example.com
   
   # TCP connection time
   time nc -zv gateway.example.com 8443
   ```

3. **Resource usage**:
   ```bash
   # CPU and memory
   top -p $(pgrep mcp-router)
   
   # File descriptors
   lsof -p $(pgrep mcp-router) | wc -l
   ```

#### Solutions

- **Network latency**: Consider TCP protocol or closer gateway
- **Rate limiting**: Increase rate limit configuration
- **Resource constraints**: Increase system resources
- **Connection pooling**: Enable connection pooling (future feature)

### Rate Limiting

#### Symptom
```
ERROR: rate limit exceeded
```

#### Diagnosis

1. **Check current limits**:
   ```yaml
   # In config
   local:
     rate_limit:
       requests_per_second: 10.0
       burst: 20
   ```

2. **Monitor rate limit metrics**:
   ```bash
   watch -n 1 'curl -s http://localhost:9091/metrics | grep rate_limit'
   ```

#### Solutions

- **Increase limits**: Adjust `requests_per_second` and `burst`
- **Gateway limits**: Check if gateway has lower limits
- **Optimize requests**: Batch operations where possible

## Runtime Errors

### Panic/Crash

#### Symptom
```
panic: runtime error: invalid memory address
```

#### Diagnosis

1. **Check logs**:
   ```bash
   # System logs
   journalctl -u mcp-router -n 100
   
   # Application logs
   tail -f /var/log/mcp-router.log
   ```

2. **Enable debug mode**:
   ```bash
   MCP_LOGGING_LEVEL=debug \
   MCP_ADVANCED_DEBUG_LOG_FRAMES=true \
   mcp-router 2>&1 | tee debug.log
   ```

3. **Get stack trace**:
   ```bash
   # If running, send SIGQUIT
   kill -QUIT $(pgrep mcp-router)
   ```

#### Solutions

- **Update version**: Check for known issues and updates
- **Report bug**: File issue with stack trace
- **Workaround**: Try different protocol or configuration

### Memory Leaks

#### Symptom
Increasing memory usage over time

#### Diagnosis

1. **Monitor memory**:
   ```bash
   # Watch memory usage
   watch -n 10 'ps aux | grep mcp-router'
   
   # Detailed memory map
   pmap -x $(pgrep mcp-router)
   ```

2. **Check for goroutine leaks**:
   ```bash
   # Enable pprof
   curl http://localhost:6060/debug/pprof/goroutine?debug=1
   ```

#### Solutions

- **Restart periodically**: Use systemd restart policy
- **Update version**: Check for memory leak fixes
- **Limit requests**: Reduce concurrent request limit

## Configuration Issues

### Invalid Configuration

#### Symptom
```
ERROR: configuration validation failed
```

#### Diagnosis

1. **Validate YAML syntax**:
   ```bash
   # Check YAML syntax
   yamllint ~/.config/claude-cli/mcp-router.yaml
   
   # Or with yq
   yq eval '.' ~/.config/claude-cli/mcp-router.yaml
   ```

2. **Check required fields**:
   ```bash
   # Minimal required config
   cat << EOF
   gateway:
     url: "wss://gateway.example.com"
   EOF
   ```

#### Solutions

- **Fix syntax**: Correct YAML indentation and structure
- **Add required fields**: Ensure gateway.url is present
- **Use examples**: Copy from documentation examples

### Environment Variable Issues

#### Symptom
Configuration not picking up environment variables

#### Diagnosis

1. **Check variable export**:
   ```bash
   # Ensure exported
   export | grep MCP_
   
   # Check specific var
   echo $MCP_GATEWAY_URL
   ```

2. **Verify variable names**:
   ```bash
   # Correct format: MCP_<SECTION>_<KEY>
   MCP_GATEWAY_URL="wss://gateway.example.com"
   MCP_LOGGING_LEVEL="debug"
   ```

#### Solutions

- **Export variables**: Use `export` not just assignment
- **Check spelling**: Ensure correct variable names
- **Order matters**: Env vars override config file

## Debug Mode

### Comprehensive Debugging

Enable all debug features:

```bash
# Full debug configuration
cat << EOF > debug-config.yaml
logging:
  level: debug
  include_caller: true
  
advanced:
  debug:
    log_frames: true
    save_failures: true
    failure_dir: "/tmp/mcp-debug"
    enable_pprof: true
    pprof_port: 6060
EOF

# Run with debug config
MCP_CONFIG_FILE=debug-config.yaml \
MCP_LOGGING_LEVEL=debug \
mcp-router 2>&1 | tee debug.log
```

### Analyzing Debug Output

1. **Filter relevant logs**:
   ```bash
   # Connection events
   grep -E "(connect|disconnect|reconnect)" debug.log
   
   # Authentication
   grep -i "auth" debug.log
   
   # Errors only
   grep -E "(ERROR|FATAL)" debug.log
   ```

2. **Decode protocol frames**:
   ```bash
   # Extract and decode frames
   grep "frame:" debug.log | cut -d' ' -f3- | base64 -d | jq
   ```

## Getting Help

If issues persist:

1. **Collect diagnostic info**:
   ```bash
   mcp-router diagnose > diagnostic-report.txt
   ```

2. **Include in bug report**:
   - Diagnostic report
   - Configuration (sanitized)
   - Debug logs
   - Steps to reproduce

3. **File issue**: https://github.com/actual-software/mcp-bridge/issues

## Common Error Reference

| Error Message | Likely Cause | Quick Fix |
|---------------|--------------|-----------|
| `connection refused` | Gateway down or wrong port | Check gateway status and URL |
| `certificate signed by unknown authority` | Self-signed cert | Add CA cert to config |
| `401 unauthorized` | Invalid/expired token | Refresh auth token |
| `rate limit exceeded` | Too many requests | Increase rate limits |
| `context deadline exceeded` | Request timeout | Increase timeout values |
| `no such host` | DNS resolution failure | Check hostname/DNS |
| `EOF` | Connection closed by gateway | Check gateway logs |
| `bad handshake` | Protocol mismatch | Verify WebSocket endpoint |