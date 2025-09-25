#!/bin/bash

# Generate TLS certificates for E2E testing
set -e

CERT_DIR="${1:-./certs}"
DOMAIN="${2:-localhost}"

echo "Starting certificate generation process..."
echo "Certificate directory: $CERT_DIR"
echo "Domain: $DOMAIN"
echo "Timestamp: $(date)"

# Create certificate directory
mkdir -p "$CERT_DIR"
echo "Created certificate directory: $CERT_DIR"

# Generate CA private key
echo "Generating CA private key..."
openssl genrsa -out "$CERT_DIR/ca.key" 2048
echo "CA private key generated: $CERT_DIR/ca.key"

# Generate CA certificate
echo "Generating CA certificate..."
openssl req -new -x509 -days 365 -key "$CERT_DIR/ca.key" -out "$CERT_DIR/ca.crt" -subj "/C=US/ST=CA/L=SF/O=MCP-E2E-Test/CN=Test-CA"
echo "CA certificate generated: $CERT_DIR/ca.crt"

# Generate server private key
echo "Generating server private key..."
openssl genrsa -out "$CERT_DIR/server.key" 2048
echo "Server private key generated: $CERT_DIR/server.key"

# Generate server certificate signing request
echo "Generating server certificate signing request..."
openssl req -new -key "$CERT_DIR/server.key" -out "$CERT_DIR/server.csr" -subj "/C=US/ST=CA/L=SF/O=MCP-E2E-Test/CN=$DOMAIN"
echo "Server CSR generated: $CERT_DIR/server.csr"

# Create extensions file for server certificate
cat > "$CERT_DIR/server.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = $DOMAIN
DNS.2 = gateway
DNS.3 = localhost
IP.1 = 127.0.0.1
EOF

# Generate server certificate signed by CA
echo "Generating server certificate signed by CA..."
openssl x509 -req -in "$CERT_DIR/server.csr" -CA "$CERT_DIR/ca.crt" -CAkey "$CERT_DIR/ca.key" -CAcreateserial -out "$CERT_DIR/server.crt" -days 365 -extfile "$CERT_DIR/server.ext"
echo "Server certificate generated: $CERT_DIR/server.crt"

# Copy for gateway container (expected names)
echo "Copying certificates to expected names..."
cp "$CERT_DIR/server.crt" "$CERT_DIR/tls.crt"
cp "$CERT_DIR/server.key" "$CERT_DIR/tls.key"
echo "Certificates copied: $CERT_DIR/tls.crt, $CERT_DIR/tls.key"

# Set appropriate permissions
echo "Setting file permissions..."
chmod 644 "$CERT_DIR"/*.crt
chmod 600 "$CERT_DIR"/*.key
echo "File permissions set"

echo "Certificate generation completed successfully!"
echo "Generated files:"
echo "  CA Certificate: $CERT_DIR/ca.crt"
echo "  Server Certificate: $CERT_DIR/tls.crt"
echo "  Server Key: $CERT_DIR/tls.key"
echo "Completion timestamp: $(date)"

# Verify certificates
echo "Verifying certificate chain..."
if openssl verify -CAfile "$CERT_DIR/ca.crt" "$CERT_DIR/tls.crt"; then
    echo "Certificate chain verification: PASSED"
else
    echo "Certificate chain verification: FAILED"
    exit 1
fi

# Show certificate details
echo "Certificate details:"
echo "CA Certificate:"
openssl x509 -in "$CERT_DIR/ca.crt" -text -noout | grep -E "(Subject:|Issuer:|Not After|Serial Number)"
echo "Server Certificate:"
openssl x509 -in "$CERT_DIR/tls.crt" -text -noout | grep -E "(Subject:|Issuer:|Not After|Serial Number|DNS:)"

# Clean up temporary files
echo "Cleaning up temporary files..."
rm -f "$CERT_DIR/server.csr" "$CERT_DIR/server.ext" "$CERT_DIR/ca.srl"
echo "Cleanup completed"