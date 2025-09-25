# Running E2E Tests on Slow Networks

## Problem
When running E2E tests on slow network connections (hotspot, satellite, rural internet), tests may fail due to:
- Docker image download timeouts
- DNS resolution failures  
- Package fetch timeouts during Docker builds
- General network latency

## Solutions Implemented

### 1. Extended Timeouts
- Test timeouts increased from 20m to 60m for K8s tests
- Audit script timeouts increased to 60m for E2E tests
- Go test timeouts increased to 50m

### 2. Retry Logic
All Dockerfiles now include retry logic for Alpine package installations:
```dockerfile
RUN set -ex && \
    for i in 1 2 3; do \
        apk add --no-cache ca-certificates tzdata curl && break || \
        (echo "Retry $i/3 failed, waiting 10s..." && sleep 10); \
    done
```

### 3. Pre-pull Script
Use the pre-pull script before running tests to download images when network is stable:
```bash
./test/e2e/pre-pull-images.sh
```

## Recommended Workflow for Slow Networks

1. **Pre-pull images during off-peak hours:**
   ```bash
   ./test/e2e/pre-pull-images.sh
   ```

2. **Run tests with extended timeouts:**
   ```bash
   # For individual test suites
   go test -v -timeout 60m ./test/e2e/k8s
   
   # For full audit
   ./audit.sh  # Already configured with extended timeouts
   ```

3. **If tests still timeout, run them individually:**
   ```bash
   # Run K8s tests separately
   cd test/e2e/k8s
   go test -v -run TestKubernetesEndToEnd -timeout 60m
   
   # Run weather tests separately  
   cd test/e2e/weather
   go test -v -run TestWeatherMCPE2EFullStack -timeout 60m
   ```

4. **Monitor Docker pull progress:**
   ```bash
   # In another terminal, watch Docker events
   docker events
   ```

## Network Speed Requirements

Minimum recommended speeds for reliable test execution:
- **Download:** 1 Mbps (tests will work with less but take longer)
- **Upload:** 0.5 Mbps
- **Latency:** < 500ms

## Troubleshooting

### DNS Issues
If you see DNS lookup errors, try:
1. Restart Docker daemon
2. Configure Docker to use public DNS:
   ```json
   {
     "dns": ["8.8.8.8", "8.8.4.4"]
   }
   ```

### Image Pull Failures
If image pulls consistently fail:
1. Check Docker Hub status
2. Try pulling images manually with retries
3. Consider using a Docker registry mirror

### Build Cache
To speed up builds, ensure Docker build cache is preserved:
```bash
# Don't use --no-cache flag
# Don't run docker system prune -a
```

## Performance Tips

1. **Run tests during low network usage times**
2. **Close unnecessary applications using bandwidth**
3. **Consider tethering to a stronger signal if on mobile**
4. **Use wired connection if possible**
5. **Run tests sequentially rather than in parallel**

## Test Execution Times on Various Networks

| Network Type | Typical Test Duration | 
|-------------|----------------------|
| Fiber (1Gbps) | 5-10 minutes |
| Cable (100Mbps) | 10-15 minutes |
| DSL (10Mbps) | 20-30 minutes |
| Mobile Hotspot (5Mbps) | 30-60 minutes |
| Satellite/Rural (<2Mbps) | 60-90 minutes |

## Alternative: Skip Infrastructure Tests

For development on very slow networks, you can run only unit tests:
```bash
# Skip E2E tests entirely
go test -short ./...

# Or exclude E2E directories
go test $(go list ./... | grep -v e2e)
```