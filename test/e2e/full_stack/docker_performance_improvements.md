# Docker Performance Improvements for E2E Tests

## Current Performance Issues

### 1. Forced Rebuilds
**Problem**: `docker-compose up -d --build` forces rebuild every time
**Solution**: Use conditional rebuilds:
```bash
# Only rebuild if source changed
docker-compose up -d  # Remove --build flag
# Or use:
docker-compose up -d --no-recreate  # Reuse existing containers
```

### 2. Certificate Generation
**Problem**: TLS certificates regenerated on every run
**Solution**: Check if certificates exist before regenerating:
```go
if !certificatesExist() {
    generateTLSCertificates()
}
```

### 3. Full Teardown
**Problem**: `down -v --remove-orphans` removes everything
**Solution**: Use softer cleanup:
```bash
docker-compose stop  # Just stop, don't remove
# Or:
docker-compose down  # Remove containers but keep volumes
```

### 4. Large Build Context
**Problem**: Building from repo root includes unnecessary files
**Solution**: 
- Add comprehensive `.dockerignore`
- Use multi-stage builds more efficiently
- Consider pre-built base images

### 5. Build Cache Not Used
**Problem**: Docker build cache not being utilized
**Solution**: 
```bash
# Enable BuildKit for better caching
export DOCKER_BUILDKIT=1
export COMPOSE_DOCKER_CLI_BUILD=1

# Use cache mount points in Dockerfile
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
```

## Recommended Implementation Priority

### Quick Wins (5 min fix):
1. Remove `--build` flag from docker-compose up
2. Skip certificate generation if files exist
3. Use `docker-compose stop` instead of `down -v`

### Medium Effort (30 min):
1. Add proper `.dockerignore` files
2. Enable BuildKit in test environment
3. Add container health check optimizations

### Longer Term:
1. Pre-build and publish base images
2. Implement layer caching strategy
3. Consider using docker-compose profiles for different test scenarios

## Expected Performance Improvements

- **Current**: 90+ seconds for full stack startup
- **After Quick Wins**: 20-30 seconds (reusing existing containers)
- **After Full Optimization**: 5-10 seconds (with warm cache)

## Test Command for Benchmarking

```bash
# Measure current performance
time docker-compose -f docker-compose.claude-code.yml up -d --build

# Measure optimized performance
time docker-compose -f docker-compose.claude-code.yml up -d

# Compare with container reuse
time docker-compose -f docker-compose.claude-code.yml start
```