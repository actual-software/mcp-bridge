# MCP Bridge Quick Start Guide

Get up and running with MCP Bridge in under 5 minutes using our automated quick start system.

## Table of Contents

- [Overview](#overview)
- [Quick Start Script](#quick-start-script)
- [Installation Methods](#installation-methods)
- [Script Documentation](#script-documentation)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)
- [Next Steps](#next-steps)

## Overview

MCP Bridge provides several automated scripts to streamline installation and setup:

| Script | Purpose | Use Case |
|--------|---------|----------|
| `quickstart.sh` | One-click developer setup | Local development |
| `scripts/install.sh` | Full system installation | Production deployment |
| `scripts/stop.sh` | Graceful service shutdown | Maintenance |
| `scripts/migrate.sh` | Version upgrades | Updates |
| `scripts/rollback.sh` | Emergency rollback | Recovery |

## Quick Start Script

### Basic Usage

The quickstart script provides the fastest way to get MCP Bridge running:

```bash
# Basic quick start
./quickstart.sh

# Quick start with demo
./quickstart.sh --demo

# Verbose mode for debugging
./quickstart.sh --verbose

# Skip Docker setup (if not needed)
./quickstart.sh --skip-docker
```

### What It Does

1. **System Check**: Verifies OS compatibility and available resources
2. **Dependency Installation**: Checks and installs required tools
3. **Environment Setup**: Creates directories and configuration files
4. **Service Build**: Compiles Gateway and Router binaries
5. **Docker Setup**: Configures supporting services (Redis, Prometheus, Grafana)
6. **Service Start**: Launches all components
7. **Health Verification**: Confirms everything is working
8. **Demo Mode**: Optionally runs test requests

### Command-Line Options

```bash
Usage: ./quickstart.sh [OPTIONS]

Options:
    -v, --verbose       Enable verbose output
    --skip-deps        Skip dependency installation
    --skip-docker      Skip Docker setup
    --skip-build       Skip building binaries
    --demo             Run demo after setup
    --env ENV          Environment (development/staging/production)
    -h, --help         Show help message
```

### Interactive Features

The script provides an interactive experience with:

- 🎨 **Colorful Output**: Clear visual feedback (can be disabled with `EMOJI_ENABLED=false`)
- 🔄 **Progress Indicators**: Spinners for long operations
- ✅ **Status Updates**: Real-time feedback on each step
- 📋 **Smart Defaults**: Works out-of-the-box with sensible configurations
- 🔍 **Dependency Detection**: Automatically identifies missing tools

### Generated Files

After running quickstart, you'll have:

```
mcp-bridge/
├── configs/
│   ├── gateway.yaml      # Gateway configuration
│   └── router.yaml       # Router configuration
├── logs/
│   ├── gateway.log       # Gateway logs
│   └── router.log        # Router logs
├── data/                 # Runtime data
├── backups/              # Backup directory
├── .env                  # Environment variables
├── docker-compose.yml    # Docker services
└── quickstart.log        # Setup log
```

## Installation Methods

### Development Installation

For local development with hot-reload and debugging:

```bash
# Using quickstart (recommended)
./quickstart.sh

# Or using Makefile
make quickstart

# Or using install script
./scripts/install.sh --environment development
```

### Staging Installation

For testing production configurations:

```bash
./scripts/install.sh --environment staging --prefix /opt/mcp
```

### Production Installation

For full production deployment with security hardening:

```bash
# Requires root privileges
sudo ./scripts/install.sh --environment production --yes

# Custom installation directory
sudo ./scripts/install.sh --environment production --prefix /opt/mcp-bridge

# Dry run to preview changes
sudo ./scripts/install.sh --environment production --dry-run
```

### Installation Options

```bash
Usage: ./scripts/install.sh [OPTIONS]

Options:
    -e, --environment ENV    Environment (development/staging/production)
    -p, --prefix PATH       Installation prefix (default: /usr/local)
    -v, --verbose           Enable verbose output
    -y, --yes              Auto-confirm all prompts
    --skip-systemd         Skip systemd service installation
    --skip-config          Skip configuration file creation
    --dry-run              Show what would be installed
    --uninstall            Uninstall MCP Bridge
    -h, --help             Show help message
```

## Script Documentation

### quickstart.sh

**Purpose**: Rapid development environment setup

**Features**:
- Platform detection (macOS/Linux)
- Dependency checking with installation suggestions
- Automatic configuration generation
- Docker Compose setup for supporting services
- Service health verification
- Optional demo mode

**Requirements**:
- Git, Go 1.23.0+, Make
- Optional: Docker, docker-compose, jq, yq

**Output**:
- Compiled binaries in `services/*/bin/`
- Configuration files in `configs/`
- Running services on ports 8080 (Gateway) and 9091 (Router)
- Optional Docker services (Redis, Prometheus, Grafana)

### scripts/install.sh

**Purpose**: Production-grade installation with systemd integration

**Features**:
- Multi-environment support (dev/staging/prod)
- User/group creation for security isolation
- Systemd service files generation
- TLS certificate setup (self-signed for testing)
- Configuration management
- Complete uninstall capability

**Production Installation**:
- Creates `mcp-bridge` user and group
- Installs to `/usr/local/bin/` (configurable)
- Creates systemd services for automatic startup
- Sets up logging to journald
- Implements security hardening

**Generated Systemd Services**:
```bash
# Start services
sudo systemctl start mcp-gateway
sudo systemctl start mcp-router

# Enable at boot
sudo systemctl enable mcp-gateway
sudo systemctl enable mcp-router

# View logs
journalctl -u mcp-gateway -f
journalctl -u mcp-router -f
```

### scripts/stop.sh

**Purpose**: Gracefully stop all MCP Bridge services

**Features**:
- Multi-method service detection
- Graceful shutdown with timeout
- Force stop option for stuck processes
- Port cleanup
- Status reporting

**Usage**:
```bash
# Graceful stop
./scripts/stop.sh

# Force stop
./scripts/stop.sh --force

# Quiet mode
./scripts/stop.sh --quiet
```

**Stop Methods** (in order):
1. Systemd services
2. Docker Compose services
3. Kubernetes deployments
4. PID file tracking
5. Process name matching
6. Port cleanup

## Configuration

### Default Development Configuration

The quickstart script generates sensible defaults with full universal protocol support:

**Gateway** (`configs/gateway.yaml`):
```yaml
server:
  # Multi-frontend support - all protocols available
  frontends:
    - name: websocket-main
      protocol: websocket
      enabled: true
      config:
        host: 0.0.0.0
        port: 8443
        tls:
          enabled: false  # Development only

    - name: http-api
      protocol: http
      enabled: true
      config:
        host: 0.0.0.0
        port: 8080

    - name: sse-stream
      protocol: sse
      enabled: true
      config:
        host: 0.0.0.0
        port: 8081

    - name: tcp-binary
      protocol: tcp_binary
      enabled: true
      config:
        host: 0.0.0.0
        port: 8444
        tls:
          enabled: false

    - name: stdio-cli
      protocol: stdio
      enabled: true
      config:
        mode: unix_socket
        socket_path: /tmp/mcp-gateway.sock

# Universal backend support - ALL protocols supported
backends:
  - name: "local-stdio-server"
    protocol: "stdio"
    config:
      command: ["python", "examples/weather_server.py"]
      working_dir: "./examples"
      health_check:
        enabled: true
        interval: 30s
        
  - name: "websocket-server"
    protocol: "websocket"
    config:
      endpoints: ["ws://localhost:8080"]
      connection_pool:
        max_size: 10
        
  - name: "http-server"
    protocol: "http"
    config:
      base_url: "http://localhost:8081"
      
  - name: "sse-server"
    protocol: "sse"
    config:
      base_url: "http://localhost:8082"
      stream_endpoint: "/events"

# Advanced Features
discovery:
  providers:
    - type: "static"
      config:
        auto_detect_protocols: true

auth:
  - type: bearer
    token: "dev-token-12345"
logging:
  level: debug
```

**Router** (`configs/router.yaml`):
```yaml
# Universal protocol support with auto-detection
direct_clients:
  enabled: true
  auto_detection:
    enabled: true
    timeout: "1s"
  
  # Connection pooling optimization
  connection_pool:
    max_idle_connections: 20
    max_active_connections: 10
    enable_connection_reuse: true
    
  # Memory optimization
  memory_optimization:
    enable_object_pooling: true
    gc_config:
      enabled: true
      gc_percent: 75

# Server definitions with auto-detection
servers:
  - name: "local-auto-detect"
    connection_mode: "direct"
    auto_detect: true
    candidates:
      - "python examples/weather_server.py"  # stdio
      - "ws://localhost:8080"                # WebSocket
      - "http://localhost:8081"              # HTTP
    fallback: "gateway"

gateway_pool:
  endpoints:
    - url: wss://localhost:8443   # Primary WebSocket
    - url: tcps://localhost:8444  # Fallback TCP Binary
  protocol_preference: "websocket"
  auto_select: true

auth:
  type: bearer
  token: "dev-token-12345"
```

### Environment Variables

Generated `.env` file with advanced protocol support:
```bash
ENVIRONMENT=development

# Gateway dual protocol ports
GATEWAY_WEBSOCKET_PORT=8443
GATEWAY_TCP_BINARY_PORT=8444
GATEWAY_STDIO_SOCKET=/tmp/mcp-gateway.sock

# Router with direct clients
ROUTER_PORT=9091
ROUTER_DIRECT_CLIENTS_ENABLED=true

# Protocol support
SUPPORTED_PROTOCOLS=stdio,websocket,http,sse,tcp_binary
AUTO_DETECT_PROTOCOLS=true

# Performance optimization
CONNECTION_POOLING_ENABLED=true
MEMORY_OPTIMIZATION_ENABLED=true
OBSERVABILITY_ENABLED=true

# Discovery and health
HEALTH_MONITORING_ENABLED=true
SERVICE_DISCOVERY_ENABLED=true

# Legacy support
METRICS_PORT=9090
REDIS_URL=localhost:6379
AUTH_TOKEN=dev-token-12345
LOG_LEVEL=debug

# Advanced feature flags
CROSS_PROTOCOL_LOAD_BALANCING=true
PREDICTIVE_HEALTH_MONITORING=true
UNIVERSAL_SERVICE_DISCOVERY=true
PROTOCOL_CONVERSION_OPTIMIZATION=true
```

### Docker Services

Optional `docker-compose.yml` includes:
- **Redis**: Session storage and caching
- **Prometheus**: Metrics collection
- **Grafana**: Metrics visualization (admin/admin)
- **Jaeger**: Distributed tracing

## Troubleshooting

### Common Issues

#### Dependencies Not Found

```bash
# macOS
brew install go make docker docker-compose jq yq

# Ubuntu/Debian
sudo apt-get update
sudo apt-get install -y golang make docker.io docker-compose jq

# Install yq manually
wget -qO /usr/local/bin/yq https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64
chmod +x /usr/local/bin/yq
```

#### Port Already in Use

```bash
# Check what's using the port
lsof -i :8080

# Stop services and clean up ports
./scripts/stop.sh --force
```

#### Services Won't Start

```bash
# Check logs
tail -f logs/gateway.log
tail -f logs/router.log

# Validate configuration
services/gateway/bin/mcp-gateway --config configs/gateway.yaml --validate
services/router/bin/mcp-router --config configs/router.yaml --validate
```

#### Docker Issues

```bash
# Ensure Docker is running
docker info

# Reset Docker services
docker-compose down -v
docker-compose up -d
```

### Debug Mode

Enable verbose logging:

```bash
# In quickstart
./quickstart.sh --verbose

# In configuration
# Edit configs/gateway.yaml and configs/router.yaml
logging:
  level: debug
```

### Health Check Endpoints

Verify universal protocol services are running:

```bash
# Gateway health (universal protocol support)
curl http://localhost:8443/health
curl http://localhost:8444/health

# Protocol-specific health checks
curl http://localhost:8443/health/stdio
curl http://localhost:8443/health/websocket
curl http://localhost:8443/health/http
curl http://localhost:8443/health/sse

# Router health with direct client status
curl http://localhost:9091/health
curl http://localhost:9091/health/direct-clients
curl http://localhost:9091/health/protocol-detection

# Service discovery status
curl http://localhost:8443/health/discovery
curl http://localhost:8443/health/discovery/static

# Metrics with protocol breakdown
curl http://localhost:8443/metrics
curl http://localhost:9091/metrics

# Cross-protocol load balancing status
curl http://localhost:8443/health/load-balancer
```

## Next Steps

After successful setup:

### 1. Access Web Interfaces

- **Grafana**: http://localhost:3000 (admin/admin)
- **Jaeger**: http://localhost:16686
- **Prometheus**: http://localhost:9099

### 2. Test the Universal Protocol Support

**Test WebSocket Gateway (Primary Protocol)**:
```bash
# Using wscat (npm install -g wscat)
echo '{
  "jsonrpc": "2.0",
  "method": "test.echo",
  "params": {"message": "Hello via WebSocket!"},
  "id": "ws-test-1"
}' | wscat -c ws://localhost:8443 -H "Authorization: Bearer dev-token-12345"
```

**Test TCP Binary Gateway (High Performance)**:
```bash
# Using custom binary client
echo '{
  "jsonrpc": "2.0",
  "method": "test.echo", 
  "params": {"message": "Hello via TCP Binary!"},
  "id": "tcp-test-1"
}' | nc localhost 8444
```

**Test stdio Frontend (Direct Local Connection)**:
```bash
# Connect via Unix socket
echo '{
  "jsonrpc": "2.0",
  "method": "test.echo",
  "params": {"message": "Hello via stdio!"},
  "id": "stdio-test-1"
}' | nc -U /tmp/mcp-gateway.sock
```

**Test Protocol Auto-Detection (Router Direct Mode)**:
```bash
# Test router's auto-detection capability
services/router/bin/mcp-router --config configs/router.yaml --test-detection
```

**Test Cross-Protocol Load Balancing**:
```bash
# Send requests to different backend protocols through gateway
for protocol in stdio websocket http sse; do
  curl -X POST http://localhost:8443/mcp \
    -H "Authorization: Bearer dev-token-12345" \
    -H "Content-Type: application/json" \
    -H "X-Target-Protocol: $protocol" \
    -d "{\"jsonrpc\":\"2.0\",\"method\":\"test.echo\",\"params\":{\"message\":\"Hello via $protocol!\"},\"id\":\"$protocol-test\"}"
done
```

**Test Health Monitoring**:
```bash
# Gateway health with protocol-specific checks
curl http://localhost:8443/health

# Router health with direct client status
curl http://localhost:9091/health

# Protocol-specific health endpoints
curl http://localhost:8443/health/stdio
curl http://localhost:8443/health/websocket  
curl http://localhost:8443/health/http
curl http://localhost:8443/health/sse
```

### 3. Development Workflow

```bash
# Run tests
make test

# Run specific service tests
make test-gateway
make test-router

# Format code
make fmt

# Lint code
make lint

# Clean and rebuild
make clean
make build
```

### 4. Production Deployment

See [Production Deployment Guide](./deployment/README.md) for:
- Kubernetes deployment with Helm
- Docker Swarm setup
- High availability configuration
- TLS certificate management
- Monitoring and alerting setup

### 5. Learn More

- [Gateway Documentation](../services/gateway/README.md)
- [Router Documentation](../services/router/README.md)
- [Configuration Reference](./configuration.md)
- [API Documentation](./api.md)
- [Security Guide](./SECURITY.md)
- [Monitoring Guide](./monitoring.md)

## Getting Help

- **Documentation**: Check `/docs` directory
- **Logs**: Review `quickstart.log` for setup issues
- **GitHub Issues**: Report bugs or request features
- **Discord**: Join our community for real-time help

## Script Maintenance

The scripts are designed to be idempotent and safe to run multiple times:

- **Preserves existing work**: Won't overwrite configurations
- **Cleanup friendly**: `make clean-all` removes everything
- **Version aware**: Migration scripts handle upgrades
- **Rollback capable**: Can restore previous state

For script updates:
```bash
git pull origin main
./quickstart.sh  # Safe to re-run
```