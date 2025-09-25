# Installation Prerequisites and Environment Setup

This document covers comprehensive setup instructions for the MCP Bridge system across different environments and use cases.

## Table of Contents
- [System Requirements](#system-requirements)
- [Development Environment Setup](#development-environment-setup)
- [Testing Environment Setup](#testing-environment-setup)
- [Production Deployment Setup](#production-deployment-setup)
- [CI/CD Environment Setup](#cicd-environment-setup)
- [Verification and Health Checks](#verification-and-health-checks)
- [Troubleshooting](#troubleshooting)

## System Requirements

### Hardware Requirements

**Minimum (Development):**
- **CPU**: 2 cores, 2.0 GHz
- **RAM**: 4 GB
- **Storage**: 10 GB free space
- **Network**: Standard internet connectivity

**Recommended (Production):**
- **CPU**: 4+ cores, 2.4 GHz
- **RAM**: 8+ GB (16 GB for high-load scenarios)
- **Storage**: 50+ GB SSD
- **Network**: Low-latency connection (< 50ms to target services)

### Software Prerequisites

**Required for all environments:**
- **Go**: 1.21+ (latest stable recommended)
- **Make**: 3.81+ (GNU Make)
- **Git**: 2.30+

**Platform-specific requirements:**
- **Linux**: glibc 2.17+ (CentOS 7+ / Ubuntu 16.04+)
- **macOS**: macOS 10.15+ (Catalina or newer)
- **Windows**: Windows 10+ with WSL2 (recommended) or native Go support

**Optional but recommended:**
- **Docker**: 20.10+ (for containerized testing)
- **Docker Compose**: 2.0+ (for integration testing)
- **golangci-lint**: Latest version (for code quality checks)

## Development Environment Setup

### Quick Start (Recommended)

The fastest way to set up a development environment:

```bash
# Clone and auto-setup
git clone https://github.com/your-org/mcp-bridge.git
cd mcp-bridge
./quickstart.sh
```

This automated script will:
- Check system requirements
- Install missing dependencies
- Generate configurations
- Build binaries
- Start services
- Verify health

See [Quick Start Guide](./QUICKSTART.md) for detailed options.

### Manual Setup

For manual setup or customization:

### 1. Go Installation and Configuration

```bash
# Verify Go installation
go version  # Should show 1.21+

# Configure Go environment
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin

# Verify GOPATH is set correctly
go env GOPATH
```

### 2. Clone and Initialize Repository

```bash
# Clone the repository
git clone https://github.com/your-org/mcp-bridge.git
cd mcp-bridge

# Initialize Go modules
go mod download
cd services/gateway && go mod download
cd ../router && go mod download
cd ../..
```

### 3. Automated Installation

For system-wide installation with proper paths and permissions:

```bash
# Development environment
./scripts/install.sh --environment development

# Staging environment
./scripts/install.sh --environment staging

# Production environment (requires root)
sudo ./scripts/install.sh --environment production --yes
```

The install script provides:
- Binary installation to system paths
- Configuration file management
- Systemd service setup (staging/production)
- User/group creation (production)
- TLS certificate generation
- Complete uninstall option

### 4. Install Development Tools

```bash
# Install linting tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Install testing tools
go install gotest.tools/gotestsum@latest

# Install coverage tools
go install github.com/axw/gocov/gocov@latest
go install github.com/AlekSi/gocov-xml@latest

# Verify installations
golangci-lint version
gotestsum --version
```

### 4. Development Environment Verification

```bash
# Run quick development setup
make dev-install

# Verify build capability
make build

# Run unit tests
make test-unit

# Run linting checks
make lint
```

### 5. IDE Configuration

**Visual Studio Code:**
```json
// .vscode/settings.json
{
    "go.gopath": "${workspaceRoot}",
    "go.testFlags": ["-v", "-race"],
    "go.lintTool": "golangci-lint",
    "go.lintFlags": ["--fast"],
    "go.formatTool": "goimports"
}
```

**GoLand/IntelliJ:**
- Set GOPATH to project root
- Enable Go modules support
- Configure golangci-lint as external tool

## Testing Environment Setup

### 1. Unit Testing Prerequisites

```bash
# Ensure all dependencies are available
go mod tidy
cd services/gateway && go mod tidy
cd ../router && go mod tidy
cd ../..

# Install test dependencies
go install github.com/stretchr/testify@latest
go install go.uber.org/goleak@latest
```

### 2. Integration Testing Setup

```bash
# Install Docker (if not already installed)
# Linux (Ubuntu/Debian):
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# macOS (with Homebrew):
brew install docker docker-compose

# Windows (WSL2):
# Follow Docker Desktop installation guide
```

### 3. End-to-End Testing Environment

```bash
# Start required services for E2E tests
make e2e-setup

# Verify E2E environment
make test-e2e-quick

# Full E2E test suite (takes longer)
make test-e2e
```

### 4. Performance Testing Setup

```bash
# Install performance testing tools
go install golang.org/x/perf/cmd/benchstat@latest

# Run performance benchmarks
make benchmark

# Generate performance reports
make benchmark-report
```

### 5. Testing Configuration

Create test configuration files:

```bash
# Create test environment file
cat > .env.test << EOF
# Test Environment Configuration
MCP_LOG_LEVEL=debug
MCP_METRICS_PORT=9091
MCP_GATEWAY_PORT=8081
MCP_ROUTER_PORT=8082
MCP_TEST_TIMEOUT=30s
EOF
```

## Production Deployment Setup

### 1. Production Prerequisites

**System Configuration:**
```bash
# Set system limits (Linux)
echo "* soft nofile 65536" >> /etc/security/limits.conf
echo "* hard nofile 65536" >> /etc/security/limits.conf

# Configure kernel parameters
echo "net.core.somaxconn = 1024" >> /etc/sysctl.conf
echo "net.ipv4.tcp_max_syn_backlog = 1024" >> /etc/sysctl.conf
sysctl -p
```

**User and Permissions:**
```bash
# Create service user
sudo useradd -r -s /bin/false mcp-service

# Create service directories
sudo mkdir -p /opt/mcp/{bin,config,logs,data}
sudo chown -R mcp-service:mcp-service /opt/mcp
```

### 2. Binary Installation

```bash
# Build production binaries
make build

# Install binaries
sudo cp services/gateway/bin/mcp-gateway /opt/mcp/bin/
sudo cp services/router/bin/mcp-router /opt/mcp/bin/
sudo chmod +x /opt/mcp/bin/*
```

### 3. Configuration Management

```bash
# Create production configuration
sudo mkdir -p /opt/mcp/config

# Gateway configuration
sudo tee /opt/mcp/config/gateway.yaml << EOF
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: "30s"
  write_timeout: "30s"

logging:
  level: "info"
  format: "json"
  output: "/opt/mcp/logs/gateway.log"

metrics:
  enabled: true
  port: 9090
  path: "/metrics"
EOF

# Router configuration  
sudo tee /opt/mcp/config/router.yaml << EOF
router:
  host: "0.0.0.0"
  port: 8090

gateway:
  url: "ws://localhost:8080"
  reconnect_interval: "5s"
  max_retries: 5

logging:
  level: "info"
  format: "json" 
  output: "/opt/mcp/logs/router.log"
EOF
```

### 4. Service Management (systemd)

```bash
# Gateway service
sudo tee /etc/systemd/system/mcp-gateway.service << EOF
[Unit]
Description=MCP Gateway Service
After=network.target

[Service]
Type=simple
User=mcp-service
Group=mcp-service
ExecStart=/opt/mcp/bin/mcp-gateway --config /opt/mcp/config/gateway.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# Router service
sudo tee /etc/systemd/system/mcp-router.service << EOF
[Unit]
Description=MCP Router Service
After=network.target mcp-gateway.service
Requires=mcp-gateway.service

[Service]
Type=simple
User=mcp-service
Group=mcp-service
ExecStart=/opt/mcp/bin/mcp-router --config /opt/mcp/config/router.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# Enable and start services
sudo systemctl daemon-reload
sudo systemctl enable mcp-gateway mcp-router
sudo systemctl start mcp-gateway mcp-router
```

### 5. Monitoring and Logging

```bash
# Configure log rotation
sudo tee /etc/logrotate.d/mcp << EOF
/opt/mcp/logs/*.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    create 644 mcp-service mcp-service
    postrotate
        systemctl reload mcp-gateway mcp-router
    endscript
}
EOF

# Set up Prometheus monitoring (optional)
# Add to prometheus.yml:
# - job_name: 'mcp-gateway'
#   static_configs:
#     - targets: ['localhost:9090']
```

## CI/CD Environment Setup

### 1. GitHub Actions Prerequisites

Create `.github/workflows/ci.yml`:

```yaml
name: CI/CD Pipeline

on:
  push:
    branches: [ main, develop ]
  pull_request:
    branches: [ main ]

env:
  GO_VERSION: '1.21'

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      redis:
        image: redis:7-alpine
        ports:
          - 6379:6379
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
    - uses: actions/checkout@v4
    
    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: ${{ env.GO_VERSION }}
    
    - name: Cache Go modules
      uses: actions/cache@v3
      with:
        path: ~/go/pkg/mod
        key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
        restore-keys: |
          ${{ runner.os }}-go-
    
    - name: Install dependencies
      run: |
        go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
        make install-deps
    
    - name: Run linting
      run: make lint
    
    - name: Run tests
      run: make test-all
    
    - name: Generate coverage
      run: make test-coverage
    
    - name: Upload coverage to Codecov
      uses: codecov/codecov-action@v3
      with:
        file: ./coverage.out
        flags: unittests
        name: codecov-umbrella
```

### 2. GitLab CI Configuration

Create `.gitlab-ci.yml`:

```yaml
stages:
  - test
  - build
  - deploy

variables:
  GO_VERSION: "1.21"
  DOCKER_DRIVER: overlay2

before_script:
  - apt-get update -qq && apt-get install -y -qq git make
  - go version

test:
  stage: test
  image: golang:${GO_VERSION}
  services:
    - redis:7-alpine
  script:
    - make install-deps
    - make lint
    - make test-all
    - make test-coverage
  coverage: '/coverage: \d+\.\d+% of statements/'
  artifacts:
    reports:
      coverage_report:
        coverage_format: cobertura
        path: coverage.xml

build:
  stage: build
  image: golang:${GO_VERSION}
  script:
    - make build
  artifacts:
    paths:
      - services/gateway/bin/mcp-gateway
      - services/router/bin/mcp-router
    expire_in: 1 hour
```

### 3. Jenkins Pipeline Setup

Create `Jenkinsfile`:

```groovy
pipeline {
    agent any
    
    environment {
        GO_VERSION = '1.21'
        PATH = "${env.PATH}:/usr/local/go/bin:${env.WORKSPACE}/bin"
    }
    
    stages {
        stage('Setup') {
            steps {
                sh 'make install-deps'
            }
        }
        
        stage('Lint') {
            steps {
                sh 'make lint'
            }
        }
        
        stage('Test') {
            parallel {
                stage('Unit Tests') {
                    steps {
                        sh 'make test-unit'
                    }
                }
                stage('Integration Tests') {
                    steps {
                        sh 'make test-integration'
                    }
                }
            }
        }
        
        stage('Build') {
            steps {
                sh 'make build'
                archiveArtifacts artifacts: 'services/*/bin/*', fingerprint: true
            }
        }
        
        stage('Deploy') {
            when { branch 'main' }
            steps {
                sh 'make deploy'
            }
        }
    }
    
    post {
        always {
            publishTestResults testResultsPattern: 'test-results.xml'
            publishCoverage adapters: [coberturaAdapter('coverage.xml')], sourceFileResolver: sourceFiles('STORE_ALL_BUILD')
        }
    }
}
```

### 4. Container-based CI/CD

Create `Dockerfile` for CI:

```dockerfile
FROM golang:1.21-alpine AS builder

# Install system dependencies
RUN apk add --no-cache git make gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
COPY services/gateway/go.mod services/gateway/go.sum ./services/gateway/
COPY services/router/go.mod services/router/go.sum ./services/router/

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build binaries
RUN make build

# Final image
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy binaries
COPY --from=builder /app/services/gateway/bin/mcp-gateway .
COPY --from=builder /app/services/router/bin/mcp-router .

# Expose ports
EXPOSE 8080 8090 9090

CMD ["./mcp-gateway"]
```

## Verification and Health Checks

### 1. Build Verification

```bash
# Verify all components build successfully
make verify-build

# Check binary functionality
./services/gateway/bin/mcp-gateway --version
./services/router/bin/mcp-router --version
```

### 2. Service Health Checks

```bash
# Gateway health check
curl -f http://localhost:8080/health || echo "Gateway not responding"

# Router health check  
curl -f http://localhost:8090/health || echo "Router not responding"

# Metrics availability
curl -f http://localhost:9090/metrics || echo "Metrics not available"
```

### 3. Integration Verification

```bash
# Run production tests with Docker (recommended)
make -f Makefile.test test-docker

# Full integration test with real services
make -f Makefile.test test-integration

# Quick smoke tests
make -f Makefile.test test-docker-quick

# Performance baseline
make benchmark

# Load testing with real services
make -f Makefile.test test-load
```

See [Production Testing Guide](../test/PRODUCTION_TESTING.md) for comprehensive testing.

## Troubleshooting

### Common Issues

**1. Go Module Issues:**
```bash
# Clear module cache
go clean -modcache
go mod download
```

**2. Build Failures:**
```bash
# Clean all build artifacts
make clean
make build
```

**3. Test Failures:**
```bash
# Run tests with verbose output
go test -v ./...

# Run specific test
go test -v -run TestSpecificFunction ./path/to/package
```

**4. Port Conflicts:**
```bash
# Check what's using the port
lsof -i :8080
netstat -tlnp | grep :8080

# Kill conflicting processes
pkill -f mcp-gateway
```

**5. Permission Issues:**
```bash
# Fix binary permissions
chmod +x services/gateway/bin/mcp-gateway
chmod +x services/router/bin/mcp-router

# Fix log directory permissions
sudo chown -R mcp-service:mcp-service /opt/mcp/logs
```

### Getting Help

- **Documentation**: Check other docs in `docs/` directory
- **Issues**: Report problems via GitHub Issues
- **Logs**: Check service logs for detailed error information
- **Monitoring**: Use metrics endpoints for operational insights

### Environment-Specific Notes

**Development:**
- Use `make dev-install` for quick setup
- Enable debug logging for detailed output
- Use file-based configuration for easy changes

**Testing:**
- Ensure isolated test environments 
- Use Docker for consistent integration testing
- Clean up test artifacts between runs

**Production:**
- Always use released binaries
- Implement proper monitoring and alerting
- Follow security best practices for configuration
- Plan for graceful shutdown and restart procedures

**CI/CD:**
- Cache dependencies for faster builds
- Use parallel testing when possible
- Implement proper artifact management
- Follow security scanning practices

This document should be updated as the system evolves and new requirements are identified.