# TLS Configuration Guide

Comprehensive guide for configuring TLS encryption and certificate management in MCP Bridge.

## Overview

MCP Bridge uses TLS 1.3 encryption for all network communications, providing:

- **End-to-end encryption** between all components
- **Certificate-based authentication** (mTLS)
- **Perfect forward secrecy** with ephemeral key exchange
- **Automatic certificate renewal** with cert-manager integration

## TLS Configuration

### Gateway TLS Configuration

```yaml
# Gateway TLS settings
server:
  tls:
    enabled: true
    cert_file: "/etc/ssl/certs/gateway.crt"
    key_file: "/etc/ssl/private/gateway.key"
    ca_file: "/etc/ssl/certs/ca.crt"
    
    # TLS version and cipher configuration
    min_version: "TLS1.3"
    max_version: "TLS1.3"
    cipher_suites:
      - "TLS_AES_128_GCM_SHA256"
      - "TLS_AES_256_GCM_SHA384"
      - "TLS_CHACHA20_POLY1305_SHA256"
    
    # Client certificate verification
    client_auth: "require_and_verify"
    client_ca_file: "/etc/ssl/certs/client-ca.crt"
    
    # Performance settings
    session_cache_size: 10000
    session_timeout: "300s"
    
    # Security headers
    hsts_max_age: 31536000
    hsts_include_subdomains: true
```

### Router TLS Configuration

```yaml
# Router TLS settings
gateway:
  tls:
    enabled: true
    cert_file: "/etc/ssl/certs/router.crt"
    key_file: "/etc/ssl/private/router.key"
    ca_file: "/etc/ssl/certs/ca.crt"
    
    # Certificate verification
    insecure_skip_verify: false
    server_name: "gateway.example.com"
    
    # Client certificate for mTLS
    client_cert_file: "/etc/ssl/certs/router-client.crt"
    client_key_file: "/etc/ssl/private/router-client.key"
```

## Certificate Management

### Certificate Generation

#### CA Certificate

```bash
# Generate CA private key
openssl genrsa -out ca.key 4096

# Generate CA certificate
openssl req -new -x509 -days 3650 -key ca.key -out ca.crt \
  -subj "/C=US/ST=CA/L=San Francisco/O=MCP Bridge/CN=MCP Bridge CA"
```

#### Server Certificate

```bash
# Generate server private key
openssl genrsa -out gateway.key 2048

# Create certificate signing request
openssl req -new -key gateway.key -out gateway.csr \
  -subj "/C=US/ST=CA/L=San Francisco/O=MCP Bridge/CN=gateway.example.com"

# Create certificate extensions file
cat > gateway.ext << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = gateway.example.com
DNS.2 = *.gateway.example.com
DNS.3 = localhost
IP.1 = 127.0.0.1
IP.2 = 10.0.0.1
EOF

# Sign the certificate
openssl x509 -req -in gateway.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out gateway.crt -days 365 -extensions v3_req \
  -extfile gateway.ext
```

#### Client Certificate

```bash
# Generate client private key
openssl genrsa -out router.key 2048

# Generate client certificate signing request
openssl req -new -key router.key -out router.csr \
  -subj "/C=US/ST=CA/L=San Francisco/O=MCP Bridge/CN=router.example.com"

# Sign client certificate
openssl x509 -req -in router.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out router.crt -days 365
```

### Kubernetes Certificate Management

#### cert-manager Configuration

```yaml
# Install cert-manager
apiVersion: v1
kind: Namespace
metadata:
  name: cert-manager
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: mcp-bridge-ca-issuer
spec:
  ca:
    secretName: mcp-bridge-ca-secret
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: mcp-gateway-tls
  namespace: mcp-bridge
spec:
  secretName: mcp-gateway-tls-secret
  issuerRef:
    name: mcp-bridge-ca-issuer
    kind: ClusterIssuer
  commonName: gateway.mcp-bridge.svc.cluster.local
  dnsNames:
  - gateway.mcp-bridge.svc.cluster.local
  - gateway.example.com
  - localhost
```

#### Certificate Secret

```yaml
# TLS secret for gateway
apiVersion: v1
kind: Secret
metadata:
  name: mcp-gateway-tls-secret
  namespace: mcp-bridge
type: kubernetes.io/tls
data:
  tls.crt: <base64-encoded-certificate>
  tls.key: <base64-encoded-private-key>
  ca.crt: <base64-encoded-ca-certificate>
```

### Docker Certificate Management

#### Certificate Volume Mounting

```yaml
# docker-compose.yml
version: '3.8'
services:
  gateway:
    image: mcp-bridge/gateway:latest
    volumes:
      - ./certs:/etc/ssl/certs:ro
      - ./private:/etc/ssl/private:ro
    environment:
      - TLS_CERT_FILE=/etc/ssl/certs/gateway.crt
      - TLS_KEY_FILE=/etc/ssl/private/gateway.key
      - TLS_CA_FILE=/etc/ssl/certs/ca.crt
```

#### Certificate Generation Script

```bash
#!/bin/bash
# scripts/generate-certs.sh

set -e

CERT_DIR="./certs"
PRIVATE_DIR="./private"

mkdir -p "$CERT_DIR" "$PRIVATE_DIR"

# Generate CA
openssl genrsa -out "$PRIVATE_DIR/ca.key" 4096
openssl req -new -x509 -days 3650 -key "$PRIVATE_DIR/ca.key" \
  -out "$CERT_DIR/ca.crt" \
  -subj "/C=US/ST=CA/L=San Francisco/O=MCP Bridge/CN=MCP Bridge CA"

# Generate gateway certificate
openssl genrsa -out "$PRIVATE_DIR/gateway.key" 2048
openssl req -new -key "$PRIVATE_DIR/gateway.key" \
  -out "$PRIVATE_DIR/gateway.csr" \
  -subj "/C=US/ST=CA/L=San Francisco/O=MCP Bridge/CN=gateway.example.com"

# Certificate extensions
cat > "$PRIVATE_DIR/gateway.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = gateway.example.com
DNS.2 = localhost
IP.1 = 127.0.0.1
EOF

openssl x509 -req -in "$PRIVATE_DIR/gateway.csr" \
  -CA "$CERT_DIR/ca.crt" -CAkey "$PRIVATE_DIR/ca.key" \
  -CAcreateserial -out "$CERT_DIR/gateway.crt" \
  -days 365 -extensions v3_req -extfile "$PRIVATE_DIR/gateway.ext"

echo "Certificates generated successfully in $CERT_DIR"
```

## Mutual TLS (mTLS)

### mTLS Configuration

```yaml
# Gateway mTLS configuration
auth:
  mtls:
    enabled: true
    client_auth_type: "require_and_verify"
    client_ca_file: "/etc/ssl/certs/client-ca.crt"
    
    # Certificate validation options
    verify_client_cert: true
    allowed_dns_names:
      - "*.clients.example.com"
    allowed_organizations:
      - "MCP Bridge Clients"
    
    # Certificate revocation
    crl_file: "/etc/ssl/crl/client-crl.pem"
    ocsp_enabled: true
```

### Client Certificate Authentication

```yaml
# Router client certificate configuration
gateway:
  auth:
    type: mtls
    mtls:
      cert_file: "/etc/ssl/certs/client.crt"
      key_file: "/etc/ssl/private/client.key"
      ca_file: "/etc/ssl/certs/ca.crt"
      
      # Certificate verification
      insecure_skip_verify: false
      server_name: "gateway.example.com"
```

## Certificate Rotation

### Automatic Rotation

```yaml
# Certificate rotation configuration
certificate_rotation:
  enabled: true
  check_interval: "24h"
  renewal_threshold: "720h"  # 30 days
  
  # Notification settings
  notifications:
    webhook_url: "https://monitoring.example.com/webhook"
    email_addresses:
      - "ops@example.com"
  
  # Backup settings
  backup:
    enabled: true
    backup_dir: "/etc/ssl/backup"
    retention_days: 90
```

### Manual Rotation Process

```bash
#!/bin/bash
# Certificate rotation script

CERT_DIR="/etc/ssl/certs"
PRIVATE_DIR="/etc/ssl/private"
BACKUP_DIR="/etc/ssl/backup"

# Backup current certificates
mkdir -p "$BACKUP_DIR/$(date +%Y%m%d)"
cp "$CERT_DIR/gateway.crt" "$BACKUP_DIR/$(date +%Y%m%d)/"
cp "$PRIVATE_DIR/gateway.key" "$BACKUP_DIR/$(date +%Y%m%d)/"

# Generate new certificate
./scripts/generate-certs.sh

# Reload gateway service
systemctl reload mcp-gateway

# Verify certificate
openssl x509 -in "$CERT_DIR/gateway.crt" -text -noout | grep "Not After"
```

## Security Best Practices

### TLS Configuration Hardening

```yaml
# Security-hardened TLS configuration
tls:
  # Use only TLS 1.3
  min_version: "TLS1.3"
  max_version: "TLS1.3"
  
  # Strong cipher suites only
  cipher_suites:
    - "TLS_AES_256_GCM_SHA384"
    - "TLS_CHACHA20_POLY1305_SHA256"
  
  # Disable vulnerable features
  compression: false
  renegotiation: false
  
  # Security headers
  security_headers:
    strict_transport_security: "max-age=31536000; includeSubDomains"
    content_security_policy: "default-src 'self'"
    x_frame_options: "DENY"
    x_content_type_options: "nosniff"
```

### Certificate Security

1. **Use strong key sizes**: RSA 2048+ or ECDSA P-256+
2. **Short certificate lifetimes**: 90 days or less
3. **Proper certificate chains**: Include intermediate certificates
4. **Certificate transparency**: Log certificates to CT logs
5. **OCSP stapling**: Enable for revocation checking

### Key Management

```yaml
# Secure key storage
key_management:
  # Hardware security modules
  hsm:
    enabled: true
    provider: "pkcs11"
    library: "/usr/lib/libpkcs11.so"
    slot: 0
  
  # Key rotation
  rotation:
    enabled: true
    interval: "90d"
    algorithm: "ecdsa-p256"
    
  # Key backup
  backup:
    enabled: true
    encryption: "aes-256-gcm"
    storage: "s3://backup-bucket/keys/"
```

## Performance Optimization

### TLS Performance Tuning

```yaml
# Performance-optimized TLS settings
tls:
  # Session resumption
  session_cache_size: 100000
  session_timeout: "3600s"
  session_tickets: true
  
  # Buffer sizes
  read_buffer_size: 32768
  write_buffer_size: 32768
  
  # Cipher selection for performance
  prefer_server_ciphers: true
  cipher_suites:
    - "TLS_AES_128_GCM_SHA256"  # Fastest
    - "TLS_CHACHA20_POLY1305_SHA256"
    - "TLS_AES_256_GCM_SHA384"
```

### CPU Optimization

```bash
# Enable AES-NI instructions
echo 'AES-NI support:' $(grep -o aes /proc/cpuinfo | wc -l)

# Optimize TLS libraries
export OPENSSL_ia32cap="~0x200000200000000"  # Disable vulnerable features
```

## Monitoring and Alerting

### Certificate Monitoring

```yaml
# Prometheus rules for certificate monitoring
groups:
  - name: tls-certificates
    rules:
      - alert: CertificateExpiringSoon
        expr: (x509_cert_not_after - time()) / 86400 < 30
        for: 0m
        labels:
          severity: warning
        annotations:
          summary: "Certificate expiring in less than 30 days"
          description: "Certificate {{ $labels.filename }} expires in {{ $value }} days"
          
      - alert: CertificateExpired
        expr: x509_cert_not_after < time()
        for: 0m
        labels:
          severity: critical
        annotations:
          summary: "Certificate has expired"
          description: "Certificate {{ $labels.filename }} has expired"
```

### TLS Health Checks

```bash
#!/bin/bash
# TLS health check script

GATEWAY_URL="https://gateway.example.com:8443"

# Check certificate validity
echo "Checking certificate validity..."
echo | openssl s_client -connect gateway.example.com:8443 -servername gateway.example.com 2>/dev/null | \
  openssl x509 -noout -dates

# Check TLS version
echo "Checking TLS version..."
echo | openssl s_client -connect gateway.example.com:8443 -servername gateway.example.com 2>/dev/null | \
  grep "Protocol\|Cipher"

# Check certificate chain
echo "Checking certificate chain..."
openssl s_client -connect gateway.example.com:8443 -servername gateway.example.com -showcerts </dev/null
```

## Troubleshooting

### Common TLS Issues

#### Certificate Verification Failed

```bash
# Debug certificate issues
openssl verify -CAfile ca.crt gateway.crt

# Check certificate details
openssl x509 -in gateway.crt -text -noout

# Test TLS connection
openssl s_client -connect gateway.example.com:8443 -servername gateway.example.com
```

#### Handshake Failures

```bash
# Debug TLS handshake
openssl s_client -connect gateway.example.com:8443 -debug -msg

# Check supported cipher suites
nmap --script ssl-enum-ciphers -p 8443 gateway.example.com
```

#### Performance Issues

```bash
# Benchmark TLS performance
echo | openssl s_time -connect gateway.example.com:8443 -time 30

# Check TLS session reuse
curl -w "%{ssl_verify_result}\n" https://gateway.example.com/health
```

### Debug Logging

```yaml
# Enable TLS debug logging
logging:
  level: debug
  tls_debug: true
  components:
    - tls
    - certificate
    - handshake
```

## Testing

### TLS Testing Tools

```bash
# SSL Labs test (external)
curl -s "https://api.ssllabs.com/api/v3/analyze?host=gateway.example.com"

# testssl.sh comprehensive test
./testssl.sh gateway.example.com:8443

# Check certificate transparency
curl -s "https://crt.sh/?q=gateway.example.com&output=json"
```

### Automated Testing

```yaml
# GitHub Actions TLS testing
name: TLS Security Test
on: [push, pull_request]

jobs:
  tls-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Test TLS Configuration
        run: |
          ./scripts/generate-certs.sh
          docker-compose up -d gateway
          sleep 10
          ./scripts/test-tls.sh
```

## Related Documentation

- [Authentication Guide](authentication.md)
- [Security Implementation](SECURITY.md)
- [Configuration Reference](configuration.md)
- [Troubleshooting Guide](troubleshooting.md)