# Fix Before Documentation Plan
*Priority fixes needed before updating documentation*

## 🚨 Critical Fixes (Block Documentation)

### 1. Adaptive Timeout Panic
**Problem**: Runtime panic in adaptive timeout tests
**Location**: `services/router/internal/direct/adaptive_test.go`
**Impact**: Feature claimed but crashes
**Test**: `TestAdaptiveTimeout_RealWorldScenario`
```bash
# Reproduces panic:
go test ./services/router/internal/direct -run TestAdaptiveTimeout_RealWorldScenario -v
```
**Options**:
- Fix the panic (likely nil pointer or race condition)
- Disable the feature entirely
- Mark as experimental with warning

### 2. Stdio Client Connection Issues  
**Problem**: Basic connection test fails
**Location**: `services/router/internal/direct/stdio_client.go`
**Impact**: Major feature (direct stdio) non-functional
**Test**: `TestStdioClientConnectAndClose`
```bash
# Currently fails:
go test ./services/router/internal/direct -run TestStdioClientConnectAndClose -v
```
**Options**:
- Implement proper process spawning
- Remove stdio support claims
- Fix the process management code

### 3. Client Default Configuration Tests
**Problem**: Multiple "Defaults" tests failing
**Locations**: 
- `services/router/internal/direct/websocket_client_test.go`
- `services/router/internal/direct/http_client_test.go`
- `services/router/internal/direct/sse_client_test.go`
```bash
# All fail:
go test ./services/router/internal/direct -run ".*Defaults" -v
```
**Impact**: Suggests configuration system broken
**Fix**: Either fix default values or update tests to match reality

## ⚠️ Important Fixes (Should Fix)

### 4. Protocol Auto-Detection
**Problem**: Claims 98% accuracy but tests fail/missing
**Location**: `services/router/internal/direct/protocol_detector.go`
**Impact**: Major advertised feature doesn't work
```bash
# Check detection logic:
go test ./services/router/internal/direct -run ".*Detect.*" -v
```
**Options**:
- Implement proper detection logic
- Remove accuracy claims
- Provide manual protocol configuration only

### 5. SSE Client Incomplete
**Problem**: Partial implementation, tests fail
**Location**: `services/router/internal/direct/sse_client.go`
**Tests Failing**: 
- `TestSSEClientDefaults`
- `TestSSEClientSendRequestNotConnected`
- `TestSSEClientHealthCheck`
**Options**:
- Complete SSE implementation
- Mark as "coming soon"
- Remove from supported protocols

### 6. Health Check Timeouts
**Problem**: HTTP and SSE health checks timeout
**Location**: `services/router/internal/direct/health_check_runner.go`
```bash
# These timeout:
go test ./services/router/internal/direct -run ".*HealthCheck" -v -timeout 30s
```
**Fix**: Implement proper health check logic or disable

## 🧹 Code Cleanup (Nice to Have)

### 7. Remove Dead Code
**Check for**:
- Unused configuration options
- Commented out code
- Unreachable code paths
- Test files with all tests skipped

### 8. Configuration Validation
**Problem**: Config struct has many fields, unclear what works
**Location**: `services/router/internal/direct/manager.go` (DirectConfig struct)
**Action**: 
- Test each config option
- Remove non-functional options
- Add validation for required fields

## 🧪 Testing Improvements

### 9. E2E Test Coverage
**Missing Tests For**:
- Direct stdio connections
- Direct HTTP connections  
- Direct SSE connections
- Protocol auto-detection
- Fallback mechanisms

**Location to Add**: `test/e2e/full_stack/`

### 10. Update Test Reports
**Problem**: Last test report from August 13, 2025
**Action**: 
```bash
# Generate fresh test report:
go test ./... -json > test-report-$(date +%Y%m%d).json
```

## 📝 Version & Metadata

### 11. Version String Updates
**Files to Update**:
- `services/router/cmd/mcp-router/main.go` - Change version constant
- `services/gateway/cmd/mcp-gateway/main.go` - Change version constant
- `helm/mcp-bridge/Chart.yaml` - Update to beta version
- Remove all "rc1" references

## 🎯 Fix Priority Order

### Must Fix Before Any Docs:
1. **Adaptive timeout panic** - Can't document crashing feature
2. **Stdio client** - Either fix or remove entirely
3. **Client defaults** - Core functionality must work

### Should Fix Before Major Docs:
4. **Protocol detection** - Major claimed feature
5. **Health checks** - Important for production
6. **SSE client** - Either complete or remove

### Can Document Around:
7. Version strings (just note current state)
8. Dead code (doesn't affect users)
9. Test coverage (note as "in progress")

## 🔧 Quick Fix Script

Create `fix-critical.sh`:
```bash
#!/bin/bash
echo "Running critical fixes validation..."

# Test critical features
echo "1. Testing adaptive timeout..."
go test ./services/router/internal/direct -run TestAdaptiveTimeout_RealWorldScenario -v -timeout 5s || echo "FAILED: Adaptive timeout"

echo "2. Testing stdio client..."
go test ./services/router/internal/direct -run TestStdioClientConnectAndClose -v -timeout 5s || echo "FAILED: Stdio client"

echo "3. Testing client defaults..."
go test ./services/router/internal/direct -run ".*Defaults" -v -timeout 5s || echo "FAILED: Client defaults"

echo "4. Testing protocol detection..."
go test ./services/router/internal/direct -run ".*Detect.*" -v -timeout 5s || echo "FAILED: Protocol detection"

echo "Summary of issues found above ^^^"
```

## 📊 Decision Matrix

| Feature | Current State | Fix Effort | User Impact | Recommendation |
|---------|--------------|------------|-------------|----------------|
| Adaptive Timeout | Panics | Medium | Low | Fix or disable |
| Stdio Direct | Broken | High | High | Fix or remove |
| Protocol Detection | Not working | Medium | Medium | Fix or manual only |
| SSE Client | Partial | Medium | Low | Complete or defer |
| Health Checks | Timeout | Low | Medium | Fix timeouts |

## ✅ Definition of "Ready to Document"

A feature is ready to document when:
1. Core functionality works (no panics/crashes)
2. Basic tests pass (at least happy path)
3. Configuration is validated
4. Error handling exists
5. At least one E2E test passes

## 🚀 Next Steps

1. **Triage Meeting**: Decide fix vs remove for each item
2. **Fix Critical**: Address items 1-3 first
3. **Test & Validate**: Run fix validation script
4. **Update Status**: Mark what's actually working
5. **Then Document**: Only document working features

## 💡 Quick Wins

If you want to ship docs quickly, consider:
1. **Disable broken features**: Comment out stdio, SSE support
2. **Document WebSocket only**: It mostly works
3. **Mark everything else experimental**
4. **Add "Known Issues" section**
5. **Ship incremental updates** as fixes land

This approach lets you have accurate docs NOW while fixing things incrementally.