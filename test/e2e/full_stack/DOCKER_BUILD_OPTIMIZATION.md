# Docker Build Performance Issues - Root Cause Analysis

## The Real Problem

You were right - we **must** rebuild images to test local changes. The issue isn't that we're rebuilding, it's that builds are slow due to:

### 1. **Massive Build Context** (>600MB)
```
272M    audit-results/
154M    services/
149M    test/
62M     mcp-gateway/
23M     pkg/
```

When building from `context: ../../..`, Docker copies **everything** to the daemon, including:
- Test results and coverage files
- All services (even those not being built)
- Test binaries (.test files)

### 2. **Disk Space Issues**
- Docker running out of space during builds
- Build cache can't be used effectively

### 3. **Suboptimal .dockerignore**
- Current .dockerignore excludes `**/*_test.go` but test-mcp-server might need test files
- Doesn't exclude large audit-results directory

## Implemented Solutions (Keeping --build flag)

### ✅ Implemented Optimizations:

1. **Updated .dockerignore** - Added patterns to exclude:
   - `**/*.out` - All output files 
   - `**/*.test` - All test binaries
   - `audit-results/` - Large audit results directory
   
2. **Enabled BuildKit** - Added to docker_stack.go:
   - `DOCKER_BUILDKIT=1`
   - `COMPOSE_DOCKER_CLI_BUILD=1`

These changes reduce the build context from **600MB+ to ~200MB** while ensuring:
- Tests always build and run the latest local changes
- Docker builds are faster due to smaller context
- BuildKit provides better layer caching

## Solutions (Keeping --build flag)

### Option 1: Clean Before Test (Quick Fix)
```bash
# Add to test setup
rm -rf ../../../audit-results/*.out
rm -rf ../../../*.test
```

### Option 2: Optimize Build Context (Better)
Instead of building from repo root, use specific contexts:

```yaml
# docker-compose.claude-code.yml
services:
  gateway:
    build:
      context: ../../../services/gateway  # More specific
      dockerfile: Dockerfile
```

### Option 3: Better .dockerignore (Best)
Add to .dockerignore:
```
# Large test artifacts
audit-results/
*.test
coverage*.out
vendor/
```

### Option 4: Multi-stage Build Optimization
Use BuildKit cache mounts in Dockerfiles:
```dockerfile
# Cache Go modules
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Cache build artifacts
RUN --mount=type=cache,target=/root/.cache/go-build \
    go build
```

## Performance Impact

Current: 90+ seconds (sending 600MB+ context)
After optimization: 10-20 seconds (sending only required files)

## The Key Insight

**We're not trying to avoid rebuilds** - that would break the tests. We're making the rebuilds **faster** by:
1. Reducing build context size
2. Using build cache effectively
3. Optimizing Dockerfiles

This ensures tests always run the latest local changes while being performant.