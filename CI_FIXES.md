# CI Failure Analysis and Fix Plan

**Analysis Date**: October 3, 2025
**Commit**: 8d817d9 (fix: Resolve linter violations in router service)
**Failing Workflows**: 3 out of 3 checked workflows

---

## Executive Summary

Three major CI workflows are failing with a total of 5 distinct root causes:

1. **Comprehensive Security Pipeline** - 3 failures (Docker build, SARIF format, Checkov violations)
2. **Kubernetes E2E Tests** - 1 failure (Gateway CrashLoopBackOff)
3. **Code Audit** - 1 failure (8 test failures - unable to determine exact tests)

---

## 1. Comprehensive Security Pipeline Failures

### 1.1 Runtime Security Analysis - Docker Build Failure

**Status**: ❌ FAILED
**Workflow Step**: Runtime Security Analysis → Setup test environment
**Error**: `process "/bin/sh -c go test -c ./test/performance -o /bin/performance.test" did not complete successfully: exit code: 1`

#### Root Cause
In `test/Dockerfile`:
- Line 7: `WORKDIR /test` sets working directory to `/test`
- Line 18: `COPY test/ ./` copies test directory contents to `/test/`, creating `/test/test/` instead of the intended structure
- Lines 22-25: Build commands reference `./smoke` and `./performance` but actual paths are `./test/smoke` and `./test/performance`

#### Evidence
```
0.441 no Go files in /test/test/performance
0.442 FAIL ./test/performance [setup failed]
```

#### Fix
Update `test/Dockerfile` line 18 to copy contents correctly:
```dockerfile
# Before:
COPY test/ ./

# After:
COPY test/ ./test/
```

Or alternatively, adjust build paths on lines 22-25:
```dockerfile
RUN go test -c ./test/smoke -o /bin/smoke.test
RUN go test -c ./test/performance -o /bin/performance.test
```

#### Verification
```bash
cd /Users/poile/repos/mcp
docker build -f test/Dockerfile -t test-runner:local .
```

---

### 1.2 Static Application Security Testing - Invalid SARIF

**Status**: ❌ FAILED
**Workflow Step**: Static Application Security Testing → Upload Staticcheck results
**Error**: `Invalid SARIF. Missing 'results' array in run.`

#### Root Cause
The staticcheck or golangci-lint SARIF output is malformed or empty, missing required `results` array.

#### Likely Causes
1. Staticcheck not finding any issues (generates minimal SARIF without results array)
2. SARIF output format incompatible with GitHub's expectations
3. Tool version mismatch

#### Fix Options

**Option 1**: Update SARIF generation to ensure valid format even with zero findings
```yaml
# In .github/workflows/security.yml
- name: Run Staticcheck
  run: |
    staticcheck -f sarif ./... > staticcheck.sarif || true
    # Ensure valid SARIF with results array
    if ! jq '.runs[0].results' staticcheck.sarif > /dev/null 2>&1; then
      echo '{"$schema":"https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json","version":"2.1.0","runs":[{"tool":{"driver":{"name":"staticcheck"}},"results":[]}]}' > staticcheck.sarif
    fi
```

**Option 2**: Skip upload if SARIF is invalid (less preferred)
```yaml
- name: Upload Staticcheck results
  if: always()
  uses: github/codeql-action/upload-sarif@v2
  with:
    sarif_file: staticcheck.sarif
  continue-on-error: true
```

#### Verification
```bash
staticcheck -f sarif ./... > staticcheck.sarif
jq . staticcheck.sarif  # Validate JSON
jq '.runs[0].results' staticcheck.sarif  # Check for results array
```

---

### 1.3 Infrastructure Security Scan - Checkov Violations

**Status**: ❌ FAILED (warnings, not blocking)
**Workflow Step**: Infrastructure Security Scan → Run Checkov
**Error**: Multiple Checkov policy violations in `services/gateway/deploy/install.yaml:162-227`

#### Root Cause
Kubernetes deployment manifest has 8 security policy violations detected by Checkov at lines 162-227.

#### Common Checkov Violations in K8s Manifests
- Missing resource limits/requests
- Running as root (no securityContext)
- Privileged containers
- hostNetwork/hostPID enabled
- No readOnlyRootFilesystem
- Missing liveness/readiness probes
- Containers running as UID 0

#### Fix
Need to inspect `services/gateway/deploy/install.yaml:162-227` and add appropriate security controls:

```yaml
# Example security hardening
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        fsGroup: 1000
      containers:
      - name: gateway
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          runAsUser: 1000
          capabilities:
            drop:
              - ALL
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "200m"
```

#### Verification
```bash
checkov -f services/gateway/deploy/install.yaml
```

---

## 2. Kubernetes E2E Test Failures

**Status**: ❌ FAILED (all 3 test scenarios)
**Failing Tests**:
- TestKubernetesEndToEnd (Basic E2E) - 344.29s
- TestKubernetesFailover (Failover) - 277.74s
- TestKubernetesPerformance (Performance) - 258.24s

### Root Cause
Gateway pod in CrashLoopBackOff state, preventing all E2E tests from running.

#### Evidence
```
Current pod status:
NAME                               READY   STATUS             RESTARTS      AGE
mcp-gateway-7d7f9747b4-m6pgm       0/1     CrashLoopBackOff   3 (30s ago)   72s
redis-8646f4655d-8xmnd             1/1     Running            0             73s
test-mcp-server-6c699df5bc-2xm7l   1/1     Running            0             73s
test-mcp-server-6c699df5bc-zdjj6   1/1     Running            0             73s

Error: HTTP endpoint https://localhost:30443/healthz did not become ready
Test: TestKubernetesEndToEnd
Messages: Gateway HTTPS health check failed
```

### Investigation Required
The gateway pod is crashing on startup. Need to:

1. Check gateway Docker image build in K8s E2E workflow
2. Review gateway startup logs (available in test artifacts: `gateway-logs.txt`)
3. Check pod describe output (available in test artifacts: `describe-mcp-gateway-*.txt`)

### Likely Causes
1. **Missing configuration**: Gateway config missing required fields
2. **Port binding issue**: Port 8443 or 8080 not available/misconfigured
3. **Dependencies**: Redis or router not connecting properly
4. **Image build issue**: Recent code changes broke gateway Docker build
5. **Environment variables**: Missing required env vars in K8s manifest

### Fix Strategy

**Step 1**: Download and review test artifacts
```bash
# Check latest K8s E2E run artifacts
gh run list --workflow "Kubernetes E2E Tests" --limit 1
gh run view <run-id> --log | grep -A 50 "gateway.*error\|gateway.*fatal\|gateway.*panic"
```

**Step 2**: Review gateway logs locally
```bash
# Run gateway locally to reproduce
cd services/gateway
go run cmd/gateway/main.go
```

**Step 3**: Test Docker image build
```bash
# Build gateway image as K8s E2E does
docker build -t mcp-gateway:test -f services/gateway/Dockerfile .
docker run --rm mcp-gateway:test
```

**Step 4**: Review K8s deployment manifest
```bash
# Check services/gateway/deploy/install.yaml for issues
kubectl apply --dry-run=client -f services/gateway/deploy/install.yaml
```

### Verification
```bash
# After fix, run K8s E2E locally
cd test/e2e/k8s
go test -v -run TestKubernetesEndToEnd -timeout 20m
```

---

## 3. Code Audit Test Failures

**Status**: ❌ FAILED
**Error**: `make: *** [Makefile:184: audit-test] Error 8`
**Details**: 8 tests failed out of 2896 total tests

### Summary
- Total Tests: 2896
- Passed: 2879
- **Failed: 8**
- Skipped: 9
- Duration: Unknown

### Issue
Test summary shows "No failed tests details found" and "No failed packages found", suggesting:
1. Race detector failures (not parsed as normal test failures)
2. Test summary script parsing issue
3. Timeout failures

### Investigation Required
Unable to determine exact failing tests from CI logs or test summary artifact. Need to:

1. Run audit-test locally: `make audit-test`
2. Check for race detector output: `grep "WARNING: DATA RACE" audit-results/*`
3. Review test output files: `ls -la audit-results/`

### Likely Causes
1. **Race conditions**: Remaining data races in stdio client or other components
2. **Flaky tests**: Timing-sensitive tests failing intermittently
3. **New test failures**: Recent changes broke existing tests
4. **Test infrastructure**: Test harness or audit script issues

### Fix Strategy

**Step 1**: Run locally to identify failures
```bash
cd /Users/poile/repos/mcp
make audit-test
cat audit-results/test-summary-*.txt
```

**Step 2**: Check for race detector output
```bash
grep -r "WARNING: DATA RACE" audit-results/
grep -r "race detected during execution" audit-results/
```

**Step 3**: Review individual module test outputs
```bash
find audit-results/ -name "*-test-*.txt" -exec grep -l "FAIL" {} \;
```

**Step 4**: Fix identified issues
- If race conditions: Add proper mutex synchronization
- If flaky tests: Increase timeouts or fix timing assumptions
- If test failures: Fix broken code or update tests

### Verification
```bash
make audit-test
# Ensure exit code 0 and "Failed: 0" in summary
```

---

## Fix Priority and Order

### Priority 1: Critical Blockers (Must Fix)
1. **test/Dockerfile path fix** (Comprehensive Security Pipeline)
   - Impact: Blocks Runtime Security Analysis
   - Effort: 5 minutes
   - Risk: Low

2. **Gateway CrashLoopBackOff** (K8s E2E Tests)
   - Impact: Blocks all E2E tests
   - Effort: 1-4 hours (investigation + fix)
   - Risk: Medium

3. **Code Audit test failures** (8 tests)
   - Impact: Blocks test audit
   - Effort: 1-3 hours (investigation + fix)
   - Risk: Medium

### Priority 2: Quality/Security (Should Fix)
4. **SARIF format issue** (Security Pipeline)
   - Impact: Blocks static analysis upload
   - Effort: 30 minutes
   - Risk: Low

5. **Checkov violations** (Security Pipeline)
   - Impact: Security best practices
   - Effort: 1-2 hours
   - Risk: Low (security hardening)

---

## Implementation Plan

### Phase 1: Quick Wins (30 minutes)
1. Fix test/Dockerfile path issue
2. Fix SARIF generation/validation
3. Commit and push

### Phase 2: Investigation (1-2 hours)
1. Run `make audit-test` locally to identify 8 failing tests
2. Build and test gateway Docker image locally
3. Review gateway startup logs from K8s E2E artifacts
4. Document findings

### Phase 3: Fixes (2-4 hours)
1. Fix identified test failures
2. Fix gateway startup issue
3. Add Checkov suppressions or fix security violations
4. Test all fixes locally

### Phase 4: Verification (30 minutes)
1. Run `make audit-test` - ensure 0 failures
2. Run `docker build -f test/Dockerfile .` - ensure success
3. Run K8s E2E locally if possible
4. Commit all fixes with detailed messages

### Phase 5: CI Validation (wait for CI)
1. Push all changes
2. Monitor all 3 workflows
3. Review any new failures
4. Iterate if needed

---

## Success Criteria

All three workflows must pass:
- ✅ Code Audit: 0 test failures, 0 linting issues
- ✅ Comprehensive Security Pipeline: All security scans pass
- ✅ Kubernetes E2E Tests: All 3 test scenarios pass

---

## Notes

- **No cutting corners**: Every issue must be properly root-caused and fixed
- **Test locally first**: Verify all fixes work before pushing
- **Document everything**: Commit messages must explain what was fixed and why
- **One fix at a time**: Don't mix multiple unrelated fixes in one commit
