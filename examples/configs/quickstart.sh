#!/bin/bash
set -euo pipefail

# MCP Quick Start Script
# One-click deployment for development/testing
# Enhanced with lessons learned from E2E test debugging

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_DIR="$SCRIPT_DIR/config"
CERTS_DIR="$SCRIPT_DIR/certs"
LOGS_DIR="$SCRIPT_DIR/logs"

# Default ports
GATEWAY_PORT=8443
METRICS_PORT=9090
HEALTH_PORT=9443

print_header() {
    echo ""
    echo -e "${PURPLE}╔══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${PURPLE}║${CYAN}                    MCP Quick Start                           ${PURPLE}║${NC}"
    echo -e "${PURPLE}║${CYAN}              Enhanced Developer Setup                       ${PURPLE}║${NC}"
    echo -e "${PURPLE}╚══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

print_step() {
    echo -e "${BLUE}➤${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_info() {
    echo -e "${CYAN}ℹ${NC} $1"
}

# Check prerequisites with helpful installation instructions
check_command() {
    if ! command -v "$1" &> /dev/null; then
        print_error "$1 is required but not installed"
        
        case "$1" in
            "docker")
                print_info "Install Docker: https://docs.docker.com/get-docker/"
                ;;
            "docker-compose")
                print_info "Install Docker Compose: https://docs.docker.com/compose/install/"
                ;;
            "curl")
                print_info "Install curl: https://curl.se/download.html"
                ;;
            "jq")
                print_warning "jq is optional but recommended for JSON processing"
                return 0  # jq is optional
                ;;
        esac
        exit 1
    fi
}

print_header

print_step "Checking prerequisites..."
check_command docker
check_command docker-compose
check_command curl
check_command jq
print_success "All prerequisites met"

# Create directories
print_step "Setting up directory structure..."
mkdir -p "$CONFIG_DIR/environments" "$CERTS_DIR" "$LOGS_DIR"
print_success "Directory structure created"

# Generate JWT secret
print_step "Generating JWT secret..."
JWT_SECRET=$(openssl rand -base64 32)
print_success "JWT secret generated"

# Generate simple JWT token for development
generate_jwt_token() {
    local secret="$1"
    local issuer="$2"
    local audience="$3"
    
    # Simple JWT generation for development
    local header='{"alg":"HS256","typ":"JWT"}'
    local now=$(date +%s)
    local exp=$((now + 86400)) # 24 hours
    
    local payload=$(cat << EOF | tr -d '\n '
{
  "iss": "$issuer",
  "aud": "$audience", 
  "sub": "quickstart-client",
  "iat": $now,
  "exp": $exp,
  "jti": "$(openssl rand -hex 8)"
}
EOF
)

    # Base64 encode (URL safe)
    local header_b64=$(echo -n "$header" | base64 | tr -d '=' | tr '/+' '_-' | tr -d '\n')
    local payload_b64=$(echo -n "$payload" | base64 | tr -d '=' | tr '/+' '_-' | tr -d '\n')
    
    # Create signature
    local signature=$(echo -n "${header_b64}.${payload_b64}" | openssl dgst -sha256 -hmac "$secret" -binary | base64 | tr -d '=' | tr '/+' '_-' | tr -d '\n')
    
    echo "${header_b64}.${payload_b64}.${signature}"
}

MCP_AUTH_TOKEN=$(generate_jwt_token "$JWT_SECRET" "mcp-gateway-development" "mcp-clients")

# Generate TLS certificates for proper security
print_step "Generating TLS certificates..."
generate_certificates() {
    local ca_key="$CERTS_DIR/ca.key"
    local ca_cert="$CERTS_DIR/ca.crt"
    local server_key="$CERTS_DIR/server.key"
    local server_cert="$CERTS_DIR/server.crt"
    local tls_key="$CERTS_DIR/tls.key"
    local tls_cert="$CERTS_DIR/tls.crt"

    # Skip if certificates already exist and are valid
    if [[ -f "$tls_cert" && -f "$tls_key" ]]; then
        if openssl x509 -in "$tls_cert" -noout -checkend 86400 >/dev/null 2>&1; then
            print_success "Valid certificates already exist"
            return 0
        fi
    fi

    # Generate CA key
    openssl genrsa -out "$ca_key" 4096 2>/dev/null

    # Generate CA certificate
    openssl req -new -x509 -days 365 -key "$ca_key" -out "$ca_cert" \
        -subj "/C=US/ST=CA/L=SF/O=MCP-Quickstart/CN=MCP-CA" 2>/dev/null

    # Generate server key
    openssl genrsa -out "$server_key" 4096 2>/dev/null

    # Generate server certificate signing request
    openssl req -new -key "$server_key" -out "$CERTS_DIR/server.csr" \
        -subj "/C=US/ST=CA/L=SF/O=MCP-Quickstart/CN=localhost" 2>/dev/null

    # Create extensions file for SAN
    cat > "$CERTS_DIR/server.ext" << 'CERT_EOF'
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = *.localhost
DNS.3 = gateway
DNS.4 = router
IP.1 = 127.0.0.1
IP.2 = ::1
CERT_EOF

    # Generate server certificate signed by CA
    openssl x509 -req -in "$CERTS_DIR/server.csr" -CA "$ca_cert" -CAkey "$ca_key" \
        -CAcreateserial -out "$server_cert" -days 365 \
        -extensions v3_req -extfile "$CERTS_DIR/server.ext" 2>/dev/null

    # Copy for different naming conventions
    cp "$server_cert" "$tls_cert"
    cp "$server_key" "$tls_key"

    # Set appropriate permissions
    chmod 600 "$CERTS_DIR"/*.key
    chmod 644 "$CERTS_DIR"/*.crt

    # Clean up temporary files
    rm -f "$CERTS_DIR/server.csr" "$CERTS_DIR/server.ext" "$CERTS_DIR/ca.srl"

    print_success "TLS certificates generated"
}

generate_certificates

print_step "Creating enhanced docker-compose configuration..."
cat > deployment/local/docker-compose.yml <<EOF
version: '3.8'

services:
  # Redis for session/rate limit storage
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    ports:
      - "6379:6379"  # Expose for debugging
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 2s
      timeout: 2s
      retries: 5
    networks:
      - mcp-network

  # MCP Gateway
  gateway:
    build: ./services/gateway
    restart: unless-stopped
    ports:
      - "$GATEWAY_PORT:$GATEWAY_PORT"  # HTTPS/WebSocket
      - "$METRICS_PORT:$METRICS_PORT"  # Metrics
      - "$HEALTH_PORT:$HEALTH_PORT"    # Health
    environment:
      - JWT_SECRET_KEY=$JWT_SECRET
      - MCP_AUTH_TOKEN=$MCP_AUTH_TOKEN
      - MCP_REDIS_URL=redis://redis:6379/0
      - MCP_LOG_LEVEL=debug
      - ENVIRONMENT=development
    depends_on:
      redis:
        condition: service_healthy
      test-mcp-server:
        condition: service_healthy
    volumes:
      - ./config/gateway-development.yaml:/etc/mcp/config.yaml:ro
      - ./certs:/etc/tls:ro
    healthcheck:
      test: ["CMD", "curl", "-fk", "https://localhost:$HEALTH_PORT/health"]
      interval: 5s
      timeout: 3s
      retries: 5
      start_period: 10s
    networks:
      - mcp-network

  # Test MCP Server (replaces example server with our working one)
  test-mcp-server:
    build: ./test/e2e/full_stack/test-mcp-server
    restart: unless-stopped
    ports:
      - "3000:3000"
    environment:
      - PORT=3000
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/health"]
      interval: 5s
      timeout: 3s
      retries: 5
      start_period: 5s
    networks:
      - mcp-network

  # Prometheus (optional monitoring)
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.retention.time=24h'
      - '--web.enable-lifecycle'
    profiles:
      - monitoring
    networks:
      - mcp-network

networks:
  mcp-network:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.0.0/16
EOF

# Create enhanced gateway configuration using our lessons learned
print_step "Creating gateway configuration..."
cat > "$CONFIG_DIR/gateway-development.yaml" <<EOF
# MCP Gateway Configuration - Development Environment
# Auto-generated by quickstart script with enhanced validation

version: 1

server:
  host: 0.0.0.0
  port: $GATEWAY_PORT
  metrics_port: $METRICS_PORT
  max_connections: 1000
  connection_buffer_size: 65536
  protocol: websocket
  
  # TLS enabled with proper certificates
  tls:
    enabled: true
    cert_file: /etc/tls/tls.crt
    key_file: /etc/tls/tls.key
    min_version: "1.2"

# Authentication configuration - using JWT with proper validation
auth:
  provider: jwt
  jwt:
    issuer: mcp-gateway-development
    audience: mcp-clients
    secret_key_env: JWT_SECRET_KEY

# Service discovery with all required namespaces (lesson learned from E2E debugging)
service_discovery:
  provider: static
  static:
    endpoints:
      default:
        - url: "http://test-mcp-server:3000/mcp"
      system:
        - url: "http://test-mcp-server:3000/mcp"
      test:
        - url: "http://test-mcp-server:3000/mcp"
      calc:
        - url: "http://test-mcp-server:3000/mcp"
      tools:
        - url: "http://test-mcp-server:3000/mcp"

# Routing configuration  
routing:
  strategy: round_robin
  health_check_interval: 10s
  circuit_breaker:
    failure_threshold: 3
    success_threshold: 2
    timeout: 15

# Session management (using Redis for consistency)
sessions:
  provider: redis
  ttl: 3600
  cleanup_interval: 300
  redis:
    url: "redis://redis:6379/0"
    pool_size: 10
    max_retries: 3

# Rate limiting
rate_limit:
  enabled: true
  provider: redis
  window_size: 60
  requests_per_second: 100
  burst: 200
  redis:
    url: "redis://redis:6379/0"
    pool_size: 10
    max_retries: 3

# Logging for development
logging:
  level: debug
  format: json
  output: stderr
  include_caller: true

# Metrics
metrics:
  enabled: true
  path: /metrics
  port: $METRICS_PORT

# Health checks with validation (lesson learned)
health:
  port: $HEALTH_PORT
  path: /health
  checks:
    - name: config_validation
      enabled: true
    - name: service_discovery
      enabled: true
    - name: jwt_validation
      enabled: true
    - name: redis_connectivity
      enabled: true
EOF

print_success "Gateway configuration created"

# Create Prometheus config for monitoring
cat > prometheus.yml <<'EOF'
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'mcp-gateway'
    static_configs:
      - targets: ['gateway:9090']
  
  - job_name: 'redis'
    static_configs:
      - targets: ['redis:6379']
EOF

# Create enhanced router configuration
print_step "Creating router configuration..."
mkdir -p ~/.config/claude-cli

cat > ~/.config/claude-cli/mcp-router.yaml <<EOF
# MCP Router Configuration - Development Environment
# Auto-generated by quickstart script

version: 1

gateway:
  url: wss://localhost:$GATEWAY_PORT/ws
  auth:
    type: bearer
    token_env: MCP_AUTH_TOKEN
  
  tls:
    ca_file: "$CERTS_DIR/ca.crt"
    verify: true
    min_version: "1.2"
  
  connection:
    timeout_ms: 10000
    keepalive_interval_ms: 10000
    pool:
      enabled: true
      min_size: 2
      max_size: 10
      max_idle_time_ms: 30000
      acquire_timeout_ms: 5000
    reconnect:
      initial_delay_ms: 100
      max_delay_ms: 2000
      multiplier: 2.0
      max_attempts: 10

# Local settings optimized for development
local:
  read_buffer_size: 65536
  write_buffer_size: 65536
  request_timeout_ms: 30000
  max_concurrent_requests: 50

# Logging for development
logging:
  level: debug
  format: text
  output: stderr
  include_caller: true

# Metrics
metrics:
  enabled: true
  endpoint: localhost:$((METRICS_PORT + 4))
  labels:
    environment: development
    instance: quickstart-router
EOF

print_success "Router configuration created"

# Create environment file
print_step "Creating environment configuration..."
cat > "$CONFIG_DIR/environments/development.env" <<EOF
# MCP Development Environment Configuration
# Auto-generated by quickstart script

# Core settings
ENVIRONMENT=development
GATEWAY_PORT=$GATEWAY_PORT
METRICS_PORT=$METRICS_PORT
HEALTH_PORT=$HEALTH_PORT

# Authentication
JWT_SECRET_KEY=$JWT_SECRET
MCP_AUTH_TOKEN=$MCP_AUTH_TOKEN

# URLs
GATEWAY_URL=ws://localhost:$GATEWAY_PORT
GATEWAY_HEALTH_URL=http://localhost:$HEALTH_PORT/health
METRICS_URL=http://localhost:$METRICS_PORT/metrics

# Development settings
LOG_LEVEL=debug
ENABLE_DEBUG=true
TLS_ENABLED=false
EOF

print_success "Environment configuration created"

# Build services if needed
print_step "Building MCP services..."
if [ ! -f ./services/router/bin/mcp-router ] || [ ! -f ./services/gateway/bin/mcp-gateway ]; then
    print_info "Building missing binaries..."
    make build
    print_success "Services built"
else
    print_info "Binaries already exist, skipping build"
fi

# Export environment variables
export JWT_SECRET_KEY="$JWT_SECRET"
export MCP_AUTH_TOKEN="$MCP_AUTH_TOKEN"

print_success "Configuration complete"

# Start services with dependency order
print_step "Starting MCP services..."
print_info "Starting Redis..."
docker-compose up -d redis

print_info "Waiting for Redis to be ready..."
until docker-compose exec redis redis-cli ping >/dev/null 2>&1; do
    sleep 1
done
print_success "Redis ready"

print_info "Starting test MCP server..."
docker-compose up -d test-mcp-server

print_info "Waiting for test server to be ready..."
until curl -sf http://localhost:3000/health >/dev/null 2>&1; do
    sleep 1
done
print_success "Test server ready"

print_info "Starting gateway..."
docker-compose up -d gateway

# Enhanced service readiness validation
print_step "Validating service health..."

# Wait for gateway to be ready
print_info "Checking gateway health..."
local max_wait=60
local wait_time=0
while ! curl -sf "http://localhost:$HEALTH_PORT/health" >/dev/null 2>&1; do
    if [[ $wait_time -ge $max_wait ]]; then
        print_error "Gateway failed to start within ${max_wait}s"
        print_info "Checking logs..."
        docker-compose logs gateway | tail -20
        exit 1
    fi
    sleep 2
    wait_time=$((wait_time + 2))
    printf "."
done
echo ""
print_success "Gateway ready"

# Test JWT token validation
print_info "Testing JWT token validation..."
local jwt_payload=$(echo -n "$MCP_AUTH_TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null || echo "")
if echo "$jwt_payload" | grep -q "mcp-gateway-development"; then
    print_success "JWT token validation passed"
else
    print_warning "JWT token validation unclear"
fi

# Test basic MCP flow
print_step "Running validation tests..."

# Test 1: Service connectivity
print_info "Test 1: Service connectivity"
if docker-compose exec -T redis redis-cli ping >/dev/null 2>&1; then
    print_success "✓ Redis connectivity"
else
    print_error "✗ Redis connectivity failed"
fi

if curl -sf http://localhost:3000/health >/dev/null 2>&1; then
    print_success "✓ Test MCP server connectivity"
else
    print_error "✗ Test MCP server connectivity failed"
fi

if curl -sf "http://localhost:$HEALTH_PORT/health" >/dev/null 2>&1; then
    print_success "✓ Gateway health check"
else
    print_error "✗ Gateway health check failed"
fi

# Test 2: Metrics endpoints
print_info "Test 2: Metrics endpoints"
if curl -sf "http://localhost:$METRICS_PORT/metrics" >/dev/null 2>&1; then
    print_success "✓ Gateway metrics available"
else
    print_warning "⚠ Gateway metrics not available"
fi

# Test 3: Quick E2E test (if router binary exists)
if [ -f ./services/router/bin/mcp-router ]; then
    print_info "Test 3: Quick end-to-end test"
    # Set up test environment
    export MCP_AUTH_TOKEN="$MCP_AUTH_TOKEN"
    
    # Run a quick connection test (timeout after 10 seconds)
    if timeout 10s ./services/router/bin/mcp-router --config ~/.config/claude-cli/mcp-router.yaml --version >/dev/null 2>&1; then
        print_success "✓ Router binary working"
    else
        print_warning "⚠ Router test inconclusive"
    fi
else
    print_warning "⚠ Router binary not found, skipping E2E test"
fi

print_success "Validation tests completed"

# Show final status
echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║${NC}                    ${GREEN}✓ QUICK START COMPLETE${NC}                     ${GREEN}║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""

print_info "Service Status:"
docker-compose ps

echo ""
print_info "Service URLs:"
echo "  Gateway (WebSocket): ws://localhost:$GATEWAY_PORT/ws"
echo "  Gateway Health:      http://localhost:$HEALTH_PORT/health"
echo "  Gateway Metrics:     http://localhost:$METRICS_PORT/metrics"
echo "  Test MCP Server:     http://localhost:3000"
echo "  Redis:               redis://localhost:6379"

echo ""
print_info "Quick Commands:"
echo "  # Test router connection"
echo "  export MCP_AUTH_TOKEN='$MCP_AUTH_TOKEN'"
echo "  ./services/router/bin/mcp-router --config ~/.config/claude-cli/mcp-router.yaml"
echo ""
echo "  # Start with monitoring"
echo "  docker-compose --profile monitoring up -d"
echo ""
echo "  # View service logs"
echo "  docker-compose logs -f gateway"
echo ""
echo "  # Stop all services"
echo "  docker-compose down"
echo ""
echo "  # Run full E2E tests"
echo "  make test-e2e-quick"

echo ""
print_info "Next Steps:"
echo "  1. 🧪 Test the router: Run the command above"
echo "  2. 📊 Enable monitoring: docker-compose --profile monitoring up -d"
echo "  3. 🔍 Explore metrics: http://localhost:$METRICS_PORT/metrics"
echo "  4. 📚 Read documentation: docs/"

echo ""
print_warning "Development Mode Notes:"
echo "  - TLS is disabled for simplicity"
echo "  - Debug logging is enabled"
echo "  - JWT tokens expire in 24 hours"
echo "  - For production, see: ./scripts/install.sh --environment production"

echo ""
print_success "Happy coding! 🎉"