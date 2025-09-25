# Testing Guide

This document provides comprehensive testing guidelines and procedures for MCP Bridge.

## Overview

The MCP Bridge project consists of two main components with comprehensive test coverage:
- **mcp-gateway**: Cloud-native gateway for routing MCP requests
- **mcp-router**: Local CLI router for MCP servers

Both components maintain robust test suites covering unit, integration, load, chaos, and fuzz testing.

## Test Suite Structure

### Unit Tests
Located in each service's test directory:
```bash
# Gateway unit tests
cd services/gateway && make test-unit

# Router unit tests  
cd services/router && make test-unit

# All unit tests
make test-unit
```

### Integration Tests
Cross-service integration testing:
```bash
# Full integration test suite
make test-integration

# Specific integration scenarios
go test -v -tags=integration ./test/integration/...
```

### End-to-End Tests
Complete system validation with Docker containers:
```bash
# Full E2E test suite (15 minutes)
make test-e2e-full

# Quick E2E validation (5 minutes)
make test-e2e-quick

# E2E with enhanced validation
make test-e2e-validated
```

## Test Categories

### Core Functionality Tests
- **Full Stack End-to-End**: Complete MCP protocol flow validation
- **Authentication Security**: JWT, TLS, and token handling
- **Protocol Variations**: WebSocket, TCP, and protocol negotiation

### Reliability Tests  
- **Rate Limiting**: DoS protection and throttling mechanisms
- **Circuit Breakers**: Fault tolerance and graceful degradation
- **Load Balancing**: Multi-backend routing and health checks

### Resilience Tests
- **Redis Session Management**: Session persistence and failover
- **Fault Tolerance**: Service restarts and cascading failure recovery
- **Chaos Testing**: Network partitions and resource constraints

### Performance Tests
- **Load Testing**: High-concurrency request handling
- **Stress Testing**: Resource limits and degradation patterns
- **Benchmark Testing**: Latency and throughput measurements

## Running Tests

### Development Workflow
```bash
# Quick validation for development
make dev-check

# Complete development workflow
make dev-workflow

# Pre-commit validation
make pre-commit
```

### CI/CD Testing
```bash
# Full CI/CD readiness check
make ci-ready

# Coverage reporting
make test-coverage-html
```

### Security Testing
```bash
# Security vulnerability scanning
make security

# Quick security validation
make security-quick
```

## Test Configuration

### Environment Setup
Tests require specific environment variables:
```bash
export TEST_REDIS_URL=redis://localhost:6379
export INTEGRATION_ENABLED=true
export CI=true  # For CI environments
```

### Test Data
Test fixtures and mock data are located in:
- `test/fixtures/` - Common test data
- `services/*/test/fixtures/` - Service-specific test data

## Coverage Requirements

### Minimum Coverage Thresholds
- **Unit Tests**: 80% minimum
- **Integration Tests**: 70% minimum  
- **E2E Tests**: Major user journeys covered

### Coverage Reporting
```bash
# Generate coverage reports
make test-coverage-html

# View coverage in browser
open services/gateway/coverage.html
open services/router/coverage.html
```

## Testing Best Practices

### Test Organization
1. **Arrange-Act-Assert** pattern for unit tests
2. **Given-When-Then** structure for integration tests
3. **Scenario-based** approach for E2E tests

### Test Data Management
1. Use deterministic test data
2. Clean up test resources after each test
3. Isolate tests from external dependencies

### Assertion Guidelines
1. Use specific, descriptive assertions
2. Test both positive and negative cases
3. Validate error conditions and edge cases

## Troubleshooting Tests

### Common Issues
1. **Port conflicts**: Ensure test ports are available
2. **Redis connectivity**: Verify Redis is running for integration tests
3. **Docker resources**: Ensure sufficient memory for E2E tests

### Debug Commands
```bash
# Troubleshoot test environment
make troubleshoot

# Setup E2E environment for debugging
make setup-e2e

# Clean up test resources
make cleanup-e2e
```

### Log Analysis
Test logs are available in:
- `services/gateway/test-logs/`
- `services/router/test-logs/`
- Docker container logs during E2E tests

## Continuous Integration

### GitHub Actions
Tests run automatically on:
- Pull requests to main/develop branches
- Pushes to main/develop branches
- Scheduled nightly runs

### Test Automation
- **Unit tests**: Run on every commit
- **Integration tests**: Run on PR and merge
- **E2E tests**: Run on release candidates
- **Performance tests**: Run on scheduled basis

## Contributing to Tests

### Adding New Tests
1. Follow existing test patterns and naming conventions
2. Add both positive and negative test cases
3. Ensure tests are deterministic and isolated
4. Update documentation for new test scenarios

### Test Review Process
1. All test changes require peer review
2. New tests must pass in CI environment
3. Performance tests require baseline validation
4. Security tests require security team review

## Linting Policies

### Code Quality Standards

MCP Bridge maintains **zero tolerance for linting errors** in production code and enforces strict code quality standards through comprehensive static analysis.

### Linting Configuration

We use `golangci-lint` with 50+ enabled linters configured in `.golangci.yml`:

```bash
# Run all linters across the codebase
make lint

# Fix auto-correctable issues
make lint-fix

# Run linting on specific paths
~/go/bin/golangci-lint run ./services/gateway/...
~/go/bin/golangci-lint run ./services/router/...
```

### Enabled Linters

**Core Quality Linters:**
- `errcheck` - Ensures all errors are handled
- `gosec` - Security vulnerability detection
- `govet` - Official Go static analysis
- `staticcheck` - Advanced Go static analysis
- `unused` - Detects unused code
- `ineffassign` - Finds ineffective assignments

**Code Style & Formatting:**
- `gofmt` - Standard Go formatting
- `gofumpt` - Stricter formatting (extra-rules enabled)
- `goimports` - Import organization
- `godot` - Comment formatting
- `whitespace` - Whitespace consistency
- `wsl` - Whitespace and line separation

**Complexity & Design:**
- `gocyclo` - Cyclomatic complexity (max 15)
- `gocognit` - Cognitive complexity
- `dupl` - Code duplication detection
- `goconst` - Detects repeated string constants
- `nestif` - Nested if statement depth

**Security & Best Practices:**
- `gosec` - Security analysis
- `bodyclose` - HTTP response body closure
- `noctx` - HTTP request context usage
- `forcetypeassert` - Type assertion safety

### Test Code Linting Policy

Test code follows **pragmatic linting** with operational exclusions for legitimate testing patterns:

**Excluded from Test Files (`*_test.go`):**
- `errcheck` - Test helpers intentionally ignore cleanup errors
- `gosec` - Tests use hardcoded credentials and paths in controlled environments
- `gocyclo`/`gocognit` - Test functions naturally have higher complexity
- `dupl` - Test patterns legitimately duplicate across scenarios
- `lll` - Long lines common for test URLs and data
- `mnd` - Magic numbers acceptable for test timeouts and ports
- `goconst` - Test strings intentionally not constants
- `depguard` - Tests import additional libraries (testify, redis, websocket)
- `bodyclose` - Test HTTP requests have different cleanup patterns

**Still Enforced in Tests:**
- `gofmt`/`gofumpt` - Formatting standards maintained
- `wsl` - Whitespace consistency required
- `revive` - Code quality standards apply
- `nolintlint` - Proper nolint directive usage

### Zero Error Policy

**Production Code:** Absolutely **zero linting errors** allowed
- All linting issues must be resolved before merge
- No `//nolint` directives without exceptional justification
- CI/CD pipeline blocks on any linting failures

**Test Code:** **Zero errors** for enforced linters
- Test-specific exclusions are pre-configured in `.golangci.yml`
- Remaining linters still require zero errors
- Focus on maintainability while recognizing test code patterns

### Enforcement

**Development Workflow:**
```bash
# Pre-commit linting check
make lint

# Auto-fix formatting issues
make lint-fix

# Comprehensive development check
make dev-check
```

**CI/CD Integration:**
- Linting runs on every pull request
- Zero tolerance policy enforced in CI
- Automated formatting checks prevent merge

**IDE Integration:**
Configure your IDE with `golangci-lint` for real-time feedback:
- VS Code: Go extension with golangci-lint integration
- GoLand: Built-in golangci-lint support
- Vim/Neovim: ALE or coc-go plugins

### Rationale

This linting policy ensures:
1. **Security** - gosec catches vulnerabilities early
2. **Reliability** - errcheck prevents uncaught errors
3. **Maintainability** - consistent formatting and complexity limits
4. **Performance** - detects inefficient patterns
5. **Team Collaboration** - unified coding standards

The test-specific exclusions reflect industry best practices, recognizing that test code has different quality requirements while maintaining essential standards for readability and maintainability.

## Related Documentation

- [Contributing Guide](../../CONTRIBUTING.md)
- [Architecture Overview](../architecture.md)
- [API Documentation](../api.md)
- [Troubleshooting Guide](../troubleshooting.md)