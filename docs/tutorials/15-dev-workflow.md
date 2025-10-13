# Tutorial: Development Workflow

Set up an efficient development workflow for MCP Bridge with local testing, debugging, and CI/CD integration.

## Prerequisites

- Git installed
- Docker and Docker Compose
- Go 1.25+ (for building from source)
- 20-25 minutes

## Local Development Setup

### Step 1: Clone Repository

```bash
git clone https://github.com/actual-software/mcp-bridge.git
cd mcp-bridge
```

### Step 2: Install Dependencies

```bash
# Install Go dependencies
go mod download

# Install development tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

### Step 3: Local Configuration

Create `config/local/gateway.yaml`:

```yaml
version: 1

server:
  host: localhost
  port: 8443
  protocol: websocket
  tls:
    enabled: false

auth:
  type: bearer
  per_message_auth: false

discovery:
  provider: static
  static:
    endpoints:
      default:
        - url: http://localhost:9000

logging:
  level: debug
  format: text  # Easier to read during development
```

### Step 4: Run Locally

```bash
# Terminal 1: Run gateway
cd services/gateway
go run cmd/gateway/main.go --config ../../config/local/gateway.yaml

# Terminal 2: Run your MCP server
cd your-mcp-server
python server.py

# Terminal 3: Test
wscat -c ws://localhost:8443
```

## Testing Workflow

### Unit Tests

```bash
# Run all tests
make test

# Run specific package
go test ./services/gateway/...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Integration Tests

```bash
# Run integration tests
make test-integration

# Run e2e tests
make test-e2e
```

### Load Tests

```bash
# Run performance tests
cd test/performance
k6 run load-test.js
```

## Debugging

### Enable Debug Logging

```yaml
logging:
  level: debug
  format: text
  include_caller: true
```

### Use Delve Debugger

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug gateway
cd services/gateway
dlv debug cmd/gateway/main.go -- --config ../../config/local/gateway.yaml
```

### VS Code Debug Config

Create `.vscode/launch.json`:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug Gateway",
      "type": "go",
      "request": "launch",
      "mode": "debug",
      "program": "${workspaceFolder}/services/gateway/cmd/gateway",
      "args": ["--config", "${workspaceFolder}/config/local/gateway.yaml"]
    }
  ]
}
```

## Hot Reload Development

### Using Air

```bash
# Install air
go install github.com/cosmtrek/air@latest

# Create .air.toml
[build]
  cmd = "go build -o ./tmp/gateway ./cmd/gateway"
  bin = "./tmp/gateway"
  args_bin = ["--config", "../../config/local/gateway.yaml"]

# Run with hot reload
air
```

## Docker Compose Development

Create `docker-compose.dev.yml`:

```yaml
version: '3.8'

services:
  gateway:
    build:
      context: .
      dockerfile: Dockerfile.dev
    volumes:
      - ./services/gateway:/app
      - ./config:/config
    ports:
      - "8443:8443"
      - "9090:9090"
    command: air

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
```

Run:

```bash
docker-compose -f docker-compose.dev.yml up
```

## CI/CD Integration

### GitHub Actions

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3

    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.25'

    - name: Run tests
      run: make test

    - name: Run linters
      run: make lint

    - name: Build
      run: make build

  integration:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3

    - name: Run integration tests
      run: make test-integration
```

## Code Quality

### Pre-commit Hooks

Install pre-commit:

```bash
# Install pre-commit
pip install pre-commit

# Install hooks
pre-commit install
```

Create `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/golangci/golangci-lint
    rev: v1.54.2
    hooks:
      - id: golangci-lint

  - repo: https://github.com/pre-commit/pre-commit-hooks
    rev: v4.4.0
    hooks:
      - id: trailing-whitespace
      - id: end-of-file-fixer
      - id: check-yaml
```

### Linting

```bash
# Run linters
make lint

# Auto-fix issues
golangci-lint run --fix
```

## Release Workflow

### Create Release

```bash
# Tag release
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0

# Build release artifacts
make release
```

### Automated Releases

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3

    - name: Build release
      run: make release

    - name: Create GitHub Release
      uses: softprops/action-gh-release@v1
      with:
        files: |
          dist/*
```

## Best Practices

1. **Always run tests before committing**
2. **Use feature branches**
3. **Write meaningful commit messages**
4. **Keep dependencies updated**
5. **Document changes in CHANGELOG**
6. **Use semantic versioning**
7. **Review code before merging**

## Makefile Targets

```makefile
.PHONY: help
help:  ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: test
test:  ## Run unit tests
	go test -v -race -coverprofile=coverage.out ./...

.PHONY: test-integration
test-integration:  ## Run integration tests
	go test -v -tags=integration ./test/integration/...

.PHONY: lint
lint:  ## Run linters
	golangci-lint run

.PHONY: build
build:  ## Build binaries
	go build -o bin/gateway ./services/gateway/cmd/gateway
	go build -o bin/router ./services/router/cmd/router

.PHONY: run
run:  ## Run locally
	go run services/gateway/cmd/gateway/main.go --config config/local/gateway.yaml

.PHONY: docker-build
docker-build:  ## Build Docker images
	docker build -t mcp-gateway:dev -f services/gateway/Dockerfile .

.PHONY: clean
clean:  ## Clean build artifacts
	rm -rf bin/ dist/ coverage.out
```

## Troubleshooting

### Port Already in Use

```bash
# Find process using port
lsof -i :8443

# Kill process
kill -9 <PID>
```

### Module Issues

```bash
# Clean module cache
go clean -modcache

# Re-download dependencies
go mod download
```

## Next Steps

- [Configuration Reference](../configuration.md)
- [Contributing Guide](../../CONTRIBUTING.md)

## Summary

Development workflow includes:
- ✅ Local development setup
- ✅ Hot reload with Air
- ✅ Debugging with Delve
- ✅ Testing workflow
- ✅ CI/CD integration
- ✅ Code quality tools
- ✅ Release automation
