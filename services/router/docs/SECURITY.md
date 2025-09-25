# Security Best Practices

This guide covers security considerations and best practices for deploying and operating MCP Router in production environments.

## Overview

MCP Router handles sensitive authentication tokens and connects to remote infrastructure. Following these security practices helps protect your MCP deployment from common threats.

## Authentication Security

### Token Management

#### ❌ Never Do This
```yaml
# NEVER store tokens directly in config files
gateway:
  auth:
    type: bearer
    token: "sk-1234567890abcdef"  # EXPOSED!
```

#### ✅ Do This Instead

**Option 1: Environment Variables**
```yaml
gateway:
  auth:
    type: bearer
    token_env: MCP_AUTH_TOKEN
```

```bash
# Set in shell profile or secrets manager
export MCP_AUTH_TOKEN="sk-1234567890abcdef"
```

**Option 2: Secure File Storage**
```yaml
gateway:
  auth:
    type: bearer
    token_file: ~/.mcp/token
```

```bash
# Create token file with restricted permissions
echo "sk-1234567890abcdef" > ~/.mcp/token
chmod 600 ~/.mcp/token
```

**Option 3: External Secrets Manager**
```bash
# Use AWS Secrets Manager
export MCP_AUTH_TOKEN=$(aws secretsmanager get-secret-value \
  --secret-id mcp/auth-token \
  --query SecretString --output text)
```

### OAuth2 Security

When using OAuth2:

```yaml
gateway:
  auth:
    type: oauth2
    client_id: "mcp-client"
    client_secret_env: OAUTH_CLIENT_SECRET  # Never hardcode
    token_endpoint: "https://auth.example.com/token"
    scopes: ["mcp:read", "mcp:write"]  # Minimum required scopes
```

Best practices:
- Use `client_credentials` grant type for service accounts
- Request minimal scopes needed
- Rotate client secrets regularly
- Monitor token usage in audit logs

### mTLS Configuration

For maximum security, use mutual TLS:

```yaml
gateway:
  auth:
    type: mtls
    client_cert: /etc/mcp/client.crt
    client_key: /etc/mcp/client.key
  tls:
    ca_cert_path: /etc/mcp/ca.crt
    verify: true
    min_version: "1.3"  # Enforce TLS 1.3
```

Certificate management:
```bash
# Set proper permissions
chmod 644 /etc/mcp/client.crt
chmod 600 /etc/mcp/client.key
chown mcp:mcp /etc/mcp/*

# Verify certificate expiration
openssl x509 -in /etc/mcp/client.crt -noout -enddate
```

## Network Security

### TLS Configuration

#### Enforce Strong TLS
```yaml
gateway:
  tls:
    verify: true  # Always verify certificates
    min_version: "1.3"  # TLS 1.3 minimum
    cipher_suites:  # Restrict to strong ciphers
      - TLS_AES_256_GCM_SHA384
      - TLS_CHACHA20_POLY1305_SHA256
      - TLS_AES_128_GCM_SHA256
```

#### Certificate Pinning
```yaml
gateway:
  tls:
    ca_cert_path: /etc/mcp/pinned-ca.crt  # Pin specific CA
    # OR for public CA
    server_name: "gateway.example.com"  # Verify hostname
```

### Firewall Rules

Restrict outbound connections:

```bash
# Allow only gateway connections
iptables -A OUTPUT -p tcp --dport 8443 -d gateway.example.com -j ACCEPT
iptables -A OUTPUT -p tcp --dport 9443 -d gateway.example.com -j ACCEPT
iptables -A OUTPUT -p tcp --dport 443 -j DROP  # Block other HTTPS
```

## Runtime Security

### Process Isolation

Run router with minimal privileges:

```bash
# Create dedicated user
useradd -r -s /bin/false mcp-router

# Run with dropped privileges
sudo -u mcp-router mcp-router
```

### Container Security

When running in containers:

```dockerfile
FROM alpine:latest
RUN adduser -D -s /bin/false mcp-router
USER mcp-router
COPY --chown=mcp-router:mcp-router mcp-router /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/mcp-router"]
```

Kubernetes security context:
```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  readOnlyRootFilesystem: true
  capabilities:
    drop:
      - ALL
```

### Resource Limits

Prevent resource exhaustion:

```yaml
# Configuration limits
local:
  max_concurrent_requests: 100  # Limit concurrent requests
  rate_limit:
    requests_per_second: 50.0   # Rate limiting
    burst: 100

# System limits (systemd)
[Service]
LimitNOFILE=1024
LimitNPROC=512
MemoryLimit=512M
CPUQuota=50%
```

## Secrets Protection

### Environment Variable Security

```bash
# Use systemd credentials
[Service]
LoadCredential=mcp-token:/etc/mcp/token
Environment="MCP_AUTH_TOKEN=%d/mcp-token"
```

### Configuration File Permissions

```bash
# Secure configuration directory
mkdir -p ~/.config/claude-cli
chmod 700 ~/.config/claude-cli

# Secure config file
chmod 600 ~/.config/claude-cli/mcp-router.yaml
```

### Log Sanitization

Ensure sensitive data isn't logged:

```yaml
logging:
  level: info  # Don't use debug in production
  sanitize: true  # Enable log sanitization
  
advanced:
  debug:
    log_frames: false  # Disable in production
```

## Monitoring and Auditing

### Security Metrics

Monitor these security-relevant metrics:

```bash
# Authentication failures
curl -s http://localhost:9091/metrics | grep mcp_router_auth_failures

# Rate limit violations  
curl -s http://localhost:9091/metrics | grep mcp_router_rate_limit_exceeded

# Connection errors
curl -s http://localhost:9091/metrics | grep mcp_router_connection_errors
```

### Audit Logging

Enable comprehensive audit logging:

```yaml
logging:
  audit:
    enabled: true
    include_auth: true  # Log auth attempts
    include_requests: false  # Don't log request bodies
    output: /var/log/mcp/audit.log
```

### Intrusion Detection

Watch for suspicious patterns:
- Rapid authentication failures
- Unusual request patterns
- Connections from unexpected sources
- Certificate validation failures

## Incident Response

### Security Checklist

If you suspect a security incident:

1. **Immediate Actions**
   - [ ] Revoke compromised tokens
   - [ ] Rotate credentials
   - [ ] Review audit logs
   - [ ] Check for unauthorized access

2. **Investigation**
   ```bash
   # Check recent auth failures
   grep "auth.*fail" /var/log/mcp/router.log
   
   # Review connection sources
   ss -tan | grep :9091
   
   # Examine rate limit violations
   journalctl -u mcp-router | grep "rate limit"
   ```

3. **Remediation**
   - Update to latest version
   - Apply security patches
   - Review and update firewall rules
   - Enhance monitoring

### Token Rotation

Implement regular token rotation:

```bash
#!/bin/bash
# Token rotation script
NEW_TOKEN=$(generate-new-token)
echo "$NEW_TOKEN" > ~/.mcp/token.new
chmod 600 ~/.mcp/token.new
mv ~/.mcp/token.new ~/.mcp/token
systemctl reload mcp-router
```

## Compliance

### Data Protection

- Router doesn't persist sensitive data
- Tokens are only held in memory
- Request/response bodies can be excluded from logs

### Regulatory Compliance

For regulated environments:

```yaml
# HIPAA/PCI compliance settings
logging:
  pii_sanitization: true
  exclude_bodies: true
  
advanced:
  compliance:
    fips_mode: true  # Use FIPS-validated crypto
    audit_all: true  # Comprehensive audit trail
```

## Security Updates

### Staying Current

1. **Subscribe to security notifications**:
   ```bash
   # Check for updates
   mcp-router update-check
   ```

2. **Regular updates**:
   ```bash
   # Update to latest version
   curl -sSL https://raw.githubusercontent.com/poiley/mcp-bridge/main/services/router/install.sh | bash
   ```

3. **Security patches**:
   - Apply patches within 24 hours for critical issues
   - Test in staging before production deployment

## Hardening Checklist

- [ ] Tokens stored securely (env vars or files with 600 permissions)
- [ ] TLS 1.3 enforced with strong ciphers
- [ ] Certificate verification enabled
- [ ] Rate limiting configured
- [ ] Metrics endpoint secured or disabled
- [ ] Debug logging disabled in production
- [ ] Running as non-root user
- [ ] Resource limits applied
- [ ] Audit logging enabled
- [ ] Regular security updates scheduled
- [ ] Incident response plan documented
- [ ] Token rotation implemented

## Reporting Security Issues

If you discover a security vulnerability:

1. **Do NOT** open a public issue
2. Email security@example.com with details
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

We aim to respond within 24 hours and provide fixes for confirmed issues within 7 days.