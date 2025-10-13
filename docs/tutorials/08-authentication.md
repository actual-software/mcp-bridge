# Tutorial: Authentication & Security

Configure secure authentication for MCP Bridge Gateway using JWT, OAuth2, Bearer tokens, and mTLS.

## Prerequisites

- MCP Bridge Gateway installed
- Understanding of authentication concepts
- OpenSSL for certificate generation
- 30-35 minutes

## What You'll Learn

- 4 authentication methods supported by MCP Bridge
- How to configure JWT authentication
- OAuth2 integration with token introspection
- mTLS (mutual TLS) setup
- Per-message authentication
- Security best practices

## Authentication Methods Overview

| Method | Use Case | Setup Complexity | Security Level |
|--------|----------|------------------|----------------|
| **Bearer Token** | Development, simple apps | Low | Medium |
| **JWT** | Production apps, stateless | Medium | High |
| **OAuth2** | Enterprise, SSO integration | High | Very High |
| **mTLS** | Service-to-service, zero-trust | High | Very High |

## Method 1: Bearer Token Authentication

**Best for**: Development, testing, simple applications

### Configuration

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket

auth:
  type: bearer
  bearer:
    # Static tokens (for development only)
    tokens:
      - token: "dev-token-12345"
        name: "Development Client"
      - token: "test-token-67890"
        name: "Test Client"
  per_message_auth: false

logging:
  level: info
```

### Client Usage

```bash
# WebSocket with bearer token
wscat -c ws://localhost:8443 -H "Authorization: Bearer dev-token-12345"

# HTTP with bearer token
curl -X POST http://localhost:8080/api/v1/mcp \
  -H "Authorization: Bearer dev-token-12345" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

### Dynamic Token Management

For production-like bearer tokens, use environment variables:

```yaml
auth:
  type: bearer
  bearer:
    secret_key_env: BEARER_SECRET_KEY
    token_expiry: 24h
```

```bash
# Set secret
export BEARER_SECRET_KEY=$(openssl rand -base64 32)

# Start gateway
mcp-gateway --config gateway.yaml
```

## Method 2: JWT Authentication

**Best for**: Production applications, stateless authentication

### Configuration

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket
  tls:
    enabled: true
    cert_file: /etc/tls/server.crt
    key_file: /etc/tls/server.key

auth:
  type: jwt
  jwt:
    # Issuer that created the JWT
    issuer: "mcp-gateway"

    # Expected audience claim
    audience: "mcp-services"

    # Secret key from environment
    secret_key_env: JWT_SECRET_KEY

    # Algorithm (HS256, HS512, RS256, RS512, ES256, ES512)
    algorithm: HS256

    # Optional: Public key file for RS256/ES256
    # public_key_file: /etc/jwt/public.pem

    # Token expiry validation
    validate_expiry: true

    # Clock skew tolerance
    clock_skew_seconds: 60

  # Validate token on every message (vs. once per connection)
  per_message_auth: false

logging:
  level: info
```

### Generate JWT Secret

```bash
# Generate secret key
export JWT_SECRET_KEY=$(openssl rand -base64 32)
echo "JWT_SECRET_KEY=$JWT_SECRET_KEY" >> .env

# Start gateway
mcp-gateway --config gateway.yaml
```

### Create JWT Tokens

#### Using Python (PyJWT)

```python
#!/usr/bin/env python3
import jwt
import os
from datetime import datetime, timedelta

def create_token(user_id: str, expiry_hours: int = 24):
    """Create a JWT token for a user"""
    secret = os.environ['JWT_SECRET_KEY']

    payload = {
        'sub': user_id,              # Subject (user ID)
        'aud': 'mcp-services',       # Audience
        'iss': 'mcp-gateway',        # Issuer
        'iat': datetime.utcnow(),    # Issued at
        'exp': datetime.utcnow() + timedelta(hours=expiry_hours),  # Expiry
        'scope': ['tools:call', 'tools:list'],  # Permissions
    }

    token = jwt.encode(payload, secret, algorithm='HS256')
    return token

# Usage
token = create_token('user-123')
print(f"Token: {token}")
```

#### Using Node.js (jsonwebtoken)

```javascript
const jwt = require('jsonwebtoken');

function createToken(userId, expiryHours = 24) {
  const secret = process.env.JWT_SECRET_KEY;

  const payload = {
    sub: userId,
    aud: 'mcp-services',
    iss: 'mcp-gateway',
    iat: Math.floor(Date.now() / 1000),
    exp: Math.floor(Date.now() / 1000) + (expiryHours * 3600),
    scope: ['tools:call', 'tools:list'],
  };

  return jwt.sign(payload, secret, { algorithm: 'HS256' });
}

// Usage
const token = createToken('user-123');
console.log(`Token: ${token}`);
```

#### Using CLI (jwt-cli)

```bash
# Install jwt-cli
cargo install jwt-cli

# Create token
jwt encode \
  --secret "$JWT_SECRET_KEY" \
  --sub "user-123" \
  --aud "mcp-services" \
  --iss "mcp-gateway" \
  --exp "+24h" \
  '{"scope":["tools:call","tools:list"]}'
```

### Client Usage with JWT

```bash
# Generate token
TOKEN=$(python3 create_token.py)

# Connect with JWT
wscat -c wss://gateway.example.com:8443 \
  -H "Authorization: Bearer $TOKEN"

# HTTP request with JWT
curl -X POST https://gateway.example.com:8080/api/v1/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

### JWT with RSA Keys (RS256)

For environments requiring public/private key pairs:

```bash
# Generate RSA key pair
openssl genrsa -out private.pem 4096
openssl rsa -in private.pem -pubout -out public.pem

# Gateway config
```

```yaml
auth:
  type: jwt
  jwt:
    issuer: "mcp-gateway"
    audience: "mcp-services"
    algorithm: RS256
    public_key_file: /etc/jwt/public.pem
```

```python
# Create token with private key
with open('private.pem', 'r') as f:
    private_key = f.read()

token = jwt.encode(payload, private_key, algorithm='RS256')
```

## Method 3: OAuth2 Authentication

**Best for**: Enterprise deployments, SSO integration, third-party auth providers

### Configuration

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket
  tls:
    enabled: true
    cert_file: /etc/tls/server.crt
    key_file: /etc/tls/server.key

auth:
  type: oauth2
  oauth2:
    # Token introspection endpoint
    introspection_url: "https://auth.example.com/oauth2/introspect"

    # OAuth2 client credentials
    client_id_env: OAUTH2_CLIENT_ID
    client_secret_env: OAUTH2_CLIENT_SECRET

    # Expected scopes
    required_scopes:
      - "mcp:read"
      - "mcp:write"

    # Token cache TTL (avoid repeated introspection)
    cache_ttl: 5m

    # Maximum cache size
    cache_size: 10000

  per_message_auth: false

logging:
  level: info
```

### OAuth2 Provider Setup

#### Example: Keycloak Integration

```bash
# Set OAuth2 credentials
export OAUTH2_CLIENT_ID="mcp-gateway"
export OAUTH2_CLIENT_SECRET="your-client-secret"

# Keycloak introspection URL
# https://keycloak.example.com/auth/realms/mcp/protocol/openid-connect/token/introspect
```

#### Example: Auth0 Integration

```yaml
auth:
  type: oauth2
  oauth2:
    introspection_url: "https://YOUR_DOMAIN.auth0.com/oauth/token/introspect"
    client_id_env: AUTH0_CLIENT_ID
    client_secret_env: AUTH0_CLIENT_SECRET
    required_scopes:
      - "read:tools"
      - "call:tools"
```

### Client Usage with OAuth2

```bash
# 1. Obtain access token from OAuth2 provider
TOKEN=$(curl -X POST https://auth.example.com/oauth2/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "client_id=your-client-id" \
  -d "client_secret=your-client-secret" \
  -d "scope=mcp:read mcp:write" | jq -r '.access_token')

# 2. Connect to gateway with access token
wscat -c wss://gateway.example.com:8443 \
  -H "Authorization: Bearer $TOKEN"
```

## Method 4: Mutual TLS (mTLS)

**Best for**: Service-to-service authentication, zero-trust networks

### Generate Certificates

```bash
# 1. Create CA
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 1825 \
  -out ca.crt -subj "/CN=MCP-Bridge-CA"

# 2. Create server certificate
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr \
  -subj "/CN=gateway.example.com"
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt -days 825 -sha256

# 3. Create client certificate
openssl genrsa -out client.key 4096
openssl req -new -key client.key -out client.csr \
  -subj "/CN=client-001"
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out client.crt -days 825 -sha256
```

### Gateway Configuration

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket
  tls:
    enabled: true
    cert_file: /etc/tls/server.crt
    key_file: /etc/tls/server.key
    # Require client certificates
    client_auth: require
    client_ca_file: /etc/tls/ca.crt

auth:
  type: mtls
  mtls:
    # Verify certificate chain
    verify_chain: true

    # Allowed certificate subjects (optional)
    allowed_subjects:
      - "CN=client-001"
      - "CN=service-*"

    # Certificate revocation list (optional)
    crl_file: /etc/tls/revoked.crl

  per_message_auth: false

logging:
  level: info
```

### Client Usage with mTLS

```bash
# WebSocket with mTLS (using curl)
curl --cert client.crt --key client.key --cacert ca.crt \
  -X POST https://gateway.example.com:8443 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'

# Using wscat with mTLS
wscat -c wss://gateway.example.com:8443 \
  --cert client.crt \
  --key client.key \
  --ca ca.crt
```

## Per-Message Authentication

For high-security scenarios, validate auth on **every message**:

```yaml
auth:
  type: jwt
  jwt:
    issuer: "mcp-gateway"
    audience: "mcp-services"
    secret_key_env: JWT_SECRET_KEY

  # Validate on every message instead of once per connection
  per_message_auth: true

  # Cache validated tokens to reduce overhead
  per_message_auth_cache: 10000

logging:
  level: info
```

Include token in each message:

```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "get_data",
    "arguments": {"id": "123"}
  },
  "id": 1,
  "_auth": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
  }
}
```

## Security Best Practices

### 1. Use TLS in Production

```yaml
server:
  tls:
    enabled: true
    cert_file: /etc/tls/server.crt
    key_file: /etc/tls/server.key
    # Use TLS 1.3
    min_version: "1.3"
    # Strong cipher suites only
    cipher_suites:
      - TLS_AES_256_GCM_SHA384
      - TLS_CHACHA20_POLY1305_SHA256
```

### 2. Rotate Secrets Regularly

```bash
# Rotate JWT secret monthly
NEW_SECRET=$(openssl rand -base64 32)

# Update in Kubernetes
kubectl create secret generic mcp-gateway-secrets \
  --from-literal=jwt-secret-key="$NEW_SECRET" \
  --dry-run=client -o yaml | kubectl apply -f -

# Rolling restart
kubectl rollout restart deployment/mcp-gateway -n mcp-system
```

### 3. Rate Limiting

```yaml
rate_limit:
  enabled: true
  requests_per_sec: 100
  burst: 200
  per_ip: true
  # Different limits per user
  per_user: true
  whitelist:
    - "127.0.0.1"
    - "10.0.0.0/8"
```

### 4. Token Expiry

Set appropriate expiry times:

```python
# Short-lived tokens for high-security
token = create_token(user_id, expiry_hours=1)

# Implement refresh token flow
refresh_token = create_token(user_id, expiry_hours=720)  # 30 days
```

### 5. Audit Logging

```yaml
observability:
  logging:
    level: info
    format: json
    # Log all authentication attempts
    audit:
      enabled: true
      include_failed: true
      include_token_info: false  # Don't log actual tokens
```

## Testing Authentication

### Test JWT Validation

```bash
# Valid token
TOKEN=$(python3 create_token.py)
curl -X POST http://localhost:8080/api/v1/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'

# Expired token (should fail)
EXPIRED_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
curl -X POST http://localhost:8080/api/v1/mcp \
  -H "Authorization: Bearer $EXPIRED_TOKEN" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'

# Invalid signature (should fail)
INVALID_TOKEN="eyJhbGciOiJub25lIn0..."
curl -X POST http://localhost:8080/api/v1/mcp \
  -H "Authorization: Bearer $INVALID_TOKEN" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

### Monitor Authentication Metrics

```bash
curl http://localhost:9090/metrics | grep -E 'mcp_auth|mcp_gateway_auth'
```

Key metrics:
- `mcp_gateway_auth_success_total` - Successful authentications
- `mcp_gateway_auth_failure_total` - Failed authentications
- `mcp_gateway_auth_duration_seconds` - Auth validation time

## Troubleshooting

### "Invalid token" error

```bash
# Check token structure
echo $TOKEN | cut -d. -f2 | base64 -d | jq .

# Verify claims
jwt decode $TOKEN

# Check secret key matches
echo "Expected: mcp-gateway"
echo "Got: $(jwt decode $TOKEN | jq -r .iss)"
```

### "Token expired" error

```bash
# Check token expiry
jwt decode $TOKEN | jq .exp

# Compare with current time
date +%s
```

### mTLS handshake failure

```bash
# Verify certificate chain
openssl verify -CAfile ca.crt client.crt

# Check certificate validity
openssl x509 -in client.crt -noout -dates

# Test connection
openssl s_client -connect gateway.example.com:8443 \
  -cert client.crt -key client.key -CAfile ca.crt
```

## Next Steps

- [Monitoring & Observability](10-monitoring.md) - Monitor authentication metrics
- [Kubernetes Deployment](04-kubernetes-deployment.md) - Deploy with secure auth
- [Configuration Reference](../configuration.md) - All authentication options

## Summary

This tutorial covered:
- 4 authentication methods (Bearer, JWT, OAuth2, mTLS)
- Configuration for each method
- Token generation and validation
- Per-message authentication
- Security best practices
- Testing and troubleshooting


