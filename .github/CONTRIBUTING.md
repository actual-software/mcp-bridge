# Contributing to MCP Bridge

Thank you for your interest in contributing to MCP Bridge! This document provides guidelines and information for contributors.

## 🚀 Quick Start

1. **Fork the repository** on GitHub
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/YOUR_USERNAME/mcp-bridge.git
   cd mcp-bridge
   ```
3. **Set up development environment**:
   ```bash
   ./quickstart.sh
   ```

## 🔧 Development Setup

### Prerequisites

- **Go 1.21+** - [Install Go](https://golang.org/dl/)
- **Docker** - For running integration tests
- **Make** - For build automation
- **Git** - For version control

### Building and Testing

```bash
# Build all services
make build

# Run tests
make test-short     # Quick tests
make test-all       # Full test suite
make test-coverage  # With coverage report

# Run specific service tests
cd services/gateway && make test
cd services/router && make test
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Run security scan
make security-scan

# Validate commit messages
make validate-commits

# Pre-commit checks
make pre-commit
```

## 📋 Contribution Guidelines

### 📝 Before You Start

1. **Check existing issues** - Look for related issues or discussions
2. **Create an issue** - For new features or bugs, create an issue first
3. **Discuss your approach** - Get feedback before implementing large changes

### 🔀 Branching Strategy

- **main** - Production-ready code
- **develop** - Integration branch for features
- **feature/*** - Feature branches
- **hotfix/*** - Critical bug fixes
- **release/*** - Release preparation

### ✅ Pull Request Process

1. **Create a feature branch**:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes** with:
   - Clear, descriptive commit messages
   - Tests for new functionality
   - Documentation updates if needed

3. **Test your changes**:
   ```bash
   make test-all
   make lint
   ```

4. **Commit your changes**:
   ```bash
   git add .
   git commit -m "feat: add new feature description"
   ```

5. **Push and create PR**:
   ```bash
   git push origin feature/your-feature-name
   ```

6. **Fill out the PR template** with:
   - Description of changes
   - Testing performed
   - Breaking changes (if any)
   - Related issues

### 📝 Commit Message Format

We follow [Conventional Commits](https://www.conventionalcommits.org/) for automated changelog generation:

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

**Types (enforced by commit hooks):**
- `feat` - ✨ New feature (triggers minor version bump)
- `fix` - 🐛 Bug fix (triggers patch version bump)
- `docs` - 📚 Documentation changes
- `style` - 💎 Code style changes (formatting, etc.)
- `refactor` - 📦 Code refactoring (no functional changes)
- `perf` - 🚀 Performance improvements
- `test` - 🚨 Test additions/modifications
- `build` - 🛠 Build system changes
- `ci` - ⚙️ CI/CD configuration changes
- `chore` - ♻️ Maintenance tasks
- `revert` - 🗑 Revert previous commit

**Breaking Changes:**
Mark breaking changes with `!` or `BREAKING CHANGE:` footer (triggers major version bump):
```
feat(api)!: change authentication token format

BREAKING CHANGE: JWT tokens now require 'Bearer' prefix
```

**Examples:**
```
feat(gateway): add OAuth2 token introspection
fix(router): resolve connection pool leak in WebSocket handler
docs: update installation guide with Docker instructions
perf(gateway): optimize request routing with caching
test(router): add integration tests for connection pooling
build: update Go version to 1.21
ci: add security scanning workflow
```

**Commit Validation:**
- Commits are validated automatically via git hooks
- PR commits are checked by GitHub Actions
- Run `make validate-commits` to check locally

## 🧪 Testing Guidelines

### Test Categories

1. **Unit Tests** - Test individual functions/methods
   - Location: `*_test.go` alongside source code
   - Coverage: >90% for new code
   - Fast execution: <5 seconds total

2. **Integration Tests** - Test component interactions
   - Location: `test/integration/`
   - Include external dependencies (Redis, etc.)
   - Use Docker Compose for consistency

3. **End-to-End Tests** - Full system testing
   - Location: `test/e2e/`
   - Categories: load, chaos, fuzz, security
   - Require explicit enablement

### Writing Good Tests

```go
func TestGateway_HandleRequest_ValidToken_ReturnsSuccess(t *testing.T) {
    // Arrange
    gateway := setupTestGateway(t)
    validToken := generateValidJWT(t)
    request := &Request{Token: validToken}
    
    // Act
    response, err := gateway.HandleRequest(request)
    
    // Assert
    require.NoError(t, err)
    assert.Equal(t, StatusOK, response.Status)
    assert.NotEmpty(t, response.Data)
}
```

**Best Practices:**
- Use descriptive test names: `TestComponent_Method_Condition_ExpectedResult`
- Follow AAA pattern: Arrange, Act, Assert
- Use table-driven tests for multiple scenarios
- Clean up resources with `t.Cleanup()` or defer
- Test both success and failure cases

### Test Configuration

```bash
# Run specific test categories
TEST_VERBOSE=true make test-unit
LOAD_TEST_ENABLED=true make test-load
CHAOS_ENABLED=true make test-chaos

# Run tests with external services
docker-compose -f test/docker-compose.yml up -d
make test-integration
docker-compose -f test/docker-compose.yml down
```

## 🏗️ Architecture Guidelines

### Code Organization

```
services/
├── gateway/               # Gateway service
│   ├── cmd/              # CLI entry points
│   ├── internal/         # Private packages
│   ├── pkg/              # Public packages
│   └── docs/             # Service-specific docs
├── router/               # Local router service
│   └── ...               # Same structure
pkg/common/               # Shared packages
docs/                     # Project documentation
deployments/              # Deployment configs
```

### Design Principles

1. **Single Responsibility** - Each package has one clear purpose
2. **Dependency Injection** - Use interfaces for testability
3. **Error Handling** - Return errors, don't panic
4. **Configuration** - Environment-based configuration
5. **Observability** - Metrics, logging, tracing built-in

### Package Guidelines

```go
// Good: Clear interface definition
type TokenValidator interface {
    Validate(ctx context.Context, token string) (*Claims, error)
}

// Good: Proper error handling
func (v *JWTValidator) Validate(ctx context.Context, token string) (*Claims, error) {
    if token == "" {
        return nil, ErrEmptyToken
    }
    
    claims, err := v.parseToken(token)
    if err != nil {
        return nil, fmt.Errorf("failed to parse token: %w", err)
    }
    
    return claims, nil
}

// Good: Context usage
func (c *Client) Request(ctx context.Context, req *Request) (*Response, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
        return c.doRequest(req)
    }
}
```

## 🚀 Releases & Versioning

### Version Management

The project uses [Semantic Versioning](https://semver.org/):
- **Major** (x.0.0): Breaking changes
- **Minor** (0.x.0): New features (backward compatible)
- **Patch** (0.0.x): Bug fixes and minor improvements

The `VERSION` file is the single source of truth for the project version.

### Creating a Release

```bash
# Preview changelog for next release
make changelog-preview

# Create a release (auto-detects version)
make release

# Create specific version release
make release-patch  # Bug fixes (0.0.x)
make release-minor  # New features (0.x.0)
make release-major  # Breaking changes (x.0.0)

# Sync version across all files
./scripts/sync-version.sh
```

### Changelog Generation

Changelog is automatically generated from commit messages:
- Uses conventional commits format
- Updates on merge to main branch
- Generates release notes automatically

See [Changelog Guide](docs/changelog-guide.md) for details.

## 📚 Documentation

### Required Documentation

1. **Code Comments** - Public APIs must have godoc comments
2. **README Updates** - Update service READMEs for significant changes
3. **API Documentation** - Update if APIs change
4. **Configuration** - Document new config options
5. **Changelog** - Automatic via conventional commits

### Documentation Standards

```go
// TokenValidator validates authentication tokens and returns claims.
// It supports JWT tokens with RS256 and ES256 signing methods.
//
// Example usage:
//   validator := NewJWTValidator(publicKey)
//   claims, err := validator.Validate(ctx, tokenString)
//   if err != nil {
//       // handle error
//   }
type TokenValidator interface {
    // Validate parses and validates a token, returning claims if valid.
    // Returns ErrInvalidToken if the token is malformed or expired.
    Validate(ctx context.Context, token string) (*Claims, error)
}
```

## 🔒 Security Guidelines

### Security Requirements

1. **Input Validation** - Validate all external inputs
2. **Authentication** - Verify caller identity
3. **Authorization** - Check permissions
4. **Encryption** - Use TLS for all network communication
5. **Secrets** - Never log or expose secrets

### Secure Coding Practices

```go
// Good: Input validation
func (h *Handler) HandleRequest(req *Request) error {
    if err := validateRequest(req); err != nil {
        return fmt.Errorf("invalid request: %w", err)
    }
    // ... handle request
}

// Good: Context timeouts
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

// Bad: Logging secrets
log.Info("processing request", "token", req.Token) // DON'T DO THIS

// Good: Safe logging
log.Info("processing request", "user_id", req.UserID)
```

### Security Review Process

1. **Automated Scanning** - Gosec runs on all PRs
2. **Dependency Scanning** - Dependabot monitors vulnerabilities
3. **Code Review** - Security-focused review for sensitive changes
4. **Penetration Testing** - Regular security assessments

## 🐛 Bug Reports

### Before Reporting

1. **Search existing issues** - Check if already reported
2. **Reproduce the issue** - Verify it's reproducible
3. **Gather information** - Logs, configuration, environment

### Bug Report Template

```markdown
## Bug Description
Brief description of the issue

## Steps to Reproduce
1. Step one
2. Step two
3. Expected vs actual behavior

## Environment
- OS: [e.g., Ubuntu 22.04]
- Go Version: [e.g., 1.21.0]
- MCP Bridge Version: [e.g., v1.0.0]
- Service: [gateway/router]

## Logs
```
Relevant log output
```

## Additional Context
Any additional information
```

## 💡 Feature Requests

### Feature Request Template

```markdown
## Feature Description
Clear description of the proposed feature

## Use Case
Why is this feature needed? What problem does it solve?

## Proposed Solution
How should this feature work?

## Alternatives Considered
Other approaches you've considered

## Additional Context
Screenshots, mockups, or other relevant information
```

## 👥 Community

### Getting Help

- **GitHub Discussions** - General questions and discussions
- **GitHub Issues** - Bug reports and feature requests
- **Documentation** - Check docs/ directory first

### Code of Conduct

We follow the [Contributor Covenant](https://www.contributor-covenant.org/). Please be respectful and inclusive in all interactions.

### Recognition

Contributors are recognized in:
- Release notes for significant contributions
- CONTRIBUTORS.md file
- GitHub repository insights

## 📋 Checklist for Contributors

Before submitting a PR, ensure:

- [ ] Code builds successfully (`make build`)
- [ ] All tests pass (`make test-all`)
- [ ] Code is properly formatted (`make fmt`)
- [ ] Linting passes (`make lint`)
- [ ] Documentation is updated if needed
- [ ] Commit messages follow conventional format
- [ ] PR description is complete and clear
- [ ] Breaking changes are documented

## 🎯 Areas for Contribution

We especially welcome contributions in:

- **Performance optimization** - Improving latency and throughput
- **Security enhancements** - Additional security features
- **Documentation** - Improving guides and examples
- **Testing** - Expanding test coverage
- **Platform support** - Windows, ARM64 support
- **Monitoring** - Enhanced observability features

Thank you for contributing to MCP Bridge! 🚀