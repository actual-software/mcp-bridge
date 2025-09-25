# MCP Bridge Test Coverage Improvement Plan
## Target: 80% Coverage (Current: 62.9%)

> **Agent**: `/claude/agents/test-writer.md`  
> **No Shortcuts Policy**: Zero tolerance for workarounds, hacks, or cutting corners

## 📊 Current State Analysis

**Current Coverage**: 62.9%  
**Target Coverage**: 80.0%  
**Gap to Close**: 17.1%  
**Estimated Effort**: 6-8 parallel tracks over 2-3 weeks

## 🎯 Strategic Approach

### Phase 1: Foundation & High-Impact Areas (Week 1)
**Parallel Tracks**: 4 agents working simultaneously

### Phase 2: Core Services & Integration (Week 2) 
**Parallel Tracks**: 3 agents working simultaneously

### Phase 3: Edge Cases & Final Push (Week 3)
**Parallel Tracks**: 2 agents working simultaneously

---

## 🚀 Phase 1: Foundation & High-Impact Areas

### Track 1A: Gateway Core Services 
**Agent Focus**: Gateway internal packages with 0% coverage  
**Target**: +8% overall coverage  
**Priority**: Critical infrastructure components

#### Packages to Target:
1. **`services/gateway/internal/backends/factory.go`** (0% → 85%)
   - Test backend creation for all protocols (WebSocket, SSE, stdio)
   - Mock external dependencies properly
   - Test factory pattern with various configurations
   - **Intent**: Ensure backend factory creates correct implementations

2. **`services/gateway/internal/frontends/factory.go`** (0% → 85%)
   - Test frontend factory creation patterns
   - Mock protocol-specific implementations
   - Test error handling for unsupported protocols
   - **Intent**: Verify frontend factory robustness

3. **`services/gateway/internal/auth/oauth2.go`** (partial → 90%)
   - Test JWT validation with proper mocks
   - Test token introspection workflows
   - Test error conditions (expired tokens, invalid signatures)
   - **Intent**: Critical security component must be bulletproof

**Mock Strategy**:
```go
// Example approach - NO shortcuts
type MockHTTPClient struct {
    mock.Mock
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
    args := m.Called(req)
    return args.Get(0).(*http.Response), args.Error(1)
}
```

### Track 1B: Router Core Infrastructure
**Agent Focus**: Router internal packages with lowest coverage  
**Target**: +6% overall coverage  
**Priority**: Connection management and protocol handling

#### Packages to Target:
1. **`services/router/internal/direct/manager.go`** (improve from 56.7%)
   - Test connection lifecycle management
   - Test protocol auto-detection logic
   - Test failover and retry mechanisms
   - **Intent**: Ensure reliable connection management

2. **`services/router/internal/gateway/client.go`** (improve from 66.0%)
   - Test gateway communication protocols
   - Test authentication flows with mocks
   - Test network error handling
   - **Intent**: Robust gateway client communication

3. **`services/router/internal/pool/*.go`** (improve from 78.8%)
   - Test connection pooling algorithms
   - Test concurrent connection management
   - Test pool exhaustion scenarios
   - **Intent**: Efficient resource management

**Testing Standards**:
- All network calls must be mocked
- Test both success and failure paths
- Verify proper resource cleanup
- Test concurrent access patterns

### Track 1C: Common Package Enhancement
**Agent Focus**: Core utilities and shared components  
**Target**: +4% overall coverage  
**Priority**: Foundation components used across services

#### Packages to Target:
1. **`pkg/common/optimization/pool.go`** (improve from 90.5%)
   - Test edge cases in pool management
   - Test error conditions and recovery
   - Test performance characteristics
   - **Intent**: Bulletproof resource pooling

2. **`pkg/common/errors/retry.go`** (fix 0% functions)
   - Test retry logic with various backoff strategies
   - Test circuit breaker integration
   - Test concurrent retry scenarios
   - **Intent**: Reliable error recovery mechanisms

3. **`pkg/common/logging/*.go`** (improve from 95.2%)
   - Test structured logging with various levels
   - Test log filtering and formatting
   - Test concurrent logging scenarios
   - **Intent**: Consistent, reliable logging

### Track 1D: Security & Configuration
**Agent Focus**: Security-critical components  
**Target**: +3% overall coverage  
**Priority**: Security cannot have gaps

#### Packages to Target:
1. **`internal/secure/credential_*.go`** (0% → 75%)
   - Test platform-specific credential storage
   - Mock system keychain/credential APIs
   - Test fallback mechanisms
   - **Intent**: Secure credential management across platforms

2. **`services/gateway/internal/config/*.go`** (no test files)
   - Test configuration loading and validation
   - Test environment variable handling
   - Test configuration merging logic
   - **Intent**: Robust configuration management

3. **Gateway validation components** (improve existing)
   - Test input sanitization thoroughly
   - Test security header validation
   - Test rate limiting edge cases
   - **Intent**: Prevent security vulnerabilities

---

## 🔧 Phase 2: Core Services & Integration

### Track 2A: Gateway Advanced Features
**Agent Focus**: Protocol handling and advanced features  
**Target**: +6% overall coverage

#### Packages to Target:
1. **Gateway Discovery Services** (`internal/discovery/*.go`)
   - Test Kubernetes service discovery with k8s API mocks
   - Test Consul integration with proper consul mocks
   - Test static configuration handling
   - **Intent**: Reliable service discovery across platforms

2. **Gateway Health Monitoring** (`internal/health/*.go`)
   - Test predictive health checks
   - Test health check aggregation
   - Test failure detection and recovery
   - **Intent**: Proactive system health management

3. **Gateway Load Balancing** (`pkg/loadbalancer/*.go`)
   - Test hybrid load balancing algorithms
   - Test backend health integration
   - Test traffic distribution fairness
   - **Intent**: Optimal traffic distribution

### Track 2B: Router Advanced Features  
**Agent Focus**: Router protocol implementations  
**Target**: +4% overall coverage

#### Packages to Target:
1. **Router Direct Connections** (`internal/direct/*.go`)
   - Test HTTP/WebSocket/SSE client implementations
   - Test connection retry and recovery
   - Test protocol negotiation
   - **Intent**: Reliable direct connections

2. **Router Message Routing** (`internal/router/*.go`)
   - Test message routing logic
   - Test connection management
   - Test error propagation
   - **Intent**: Accurate message delivery

### Track 2C: Integration Testing
**Agent Focus**: Component interaction testing  
**Target**: +2% overall coverage

#### Integration Scenarios:
1. **End-to-End Protocol Flows**
   - Gateway ↔ Router communication
   - Authentication flows
   - Error propagation chains
   - **Intent**: Verify system-wide reliability

2. **Performance Integration Tests**
   - Load testing with mocked backends
   - Memory usage validation
   - Connection scaling tests
   - **Intent**: Performance under load

---

## ⚡ Phase 3: Edge Cases & Final Push

### Track 3A: Edge Cases & Error Scenarios
**Agent Focus**: Comprehensive error testing  
**Target**: +4% overall coverage

#### Focus Areas:
1. **Network Failure Scenarios**
   - Connection timeouts
   - Partial message transmission
   - Network partitions
   - **Intent**: Graceful degradation

2. **Resource Exhaustion Tests**
   - Memory pressure scenarios
   - File descriptor limits
   - Connection pool exhaustion
   - **Intent**: Resource management under stress

### Track 3B: CLI & Admin Interface Testing
**Agent Focus**: Command-line interfaces  
**Target**: +2% overall coverage

#### Packages to Target:
1. **Gateway Admin Commands** (`cmd/mcp-gateway/admin.go`)
   - Test all CLI commands with mocked dependencies
   - Test configuration validation
   - Test admin API interactions
   - **Intent**: Reliable administrative interface

2. **Router CLI Interface** (`cmd/mcp-router/*.go`)
   - Test command parsing and validation
   - Test configuration file handling
   - Test interactive setup flows
   - **Intent**: User-friendly CLI experience

---

## 🛠 Implementation Strategy

### Agent Task Structure
Each agent should follow this workflow:

```markdown
## Agent Task: [Package Name] Test Implementation

### 1. Analysis Phase (30 minutes)
- [ ] Read and understand all source code in the package
- [ ] Identify all functions, methods, and code paths
- [ ] Document external dependencies that need mocking
- [ ] Identify error conditions and edge cases
- [ ] Document the business/technical intent of each component

### 2. Planning Phase (45 minutes)  
- [ ] Create comprehensive test plan covering all scenarios
- [ ] Design mock strategy for external dependencies
- [ ] Plan test data and fixtures
- [ ] Define success criteria and assertions
- [ ] Document test intent for each test case

### 3. Implementation Phase (2-4 hours)
- [ ] Set up proper test structure with clear naming
- [ ] Implement comprehensive mocks (no real external dependencies)
- [ ] Write tests for happy path scenarios
- [ ] Write tests for all error conditions
- [ ] Write tests for edge cases and boundary values
- [ ] Add clear documentation explaining test intent

### 4. Validation Phase (30 minutes)
- [ ] Run tests multiple times to ensure consistency  
- [ ] Verify test coverage meets target (80%+ for the package)
- [ ] Ensure tests fail appropriately when they should
- [ ] Validate mock interactions are correct
- [ ] Check that tests are independent

### 5. Iteration Phase (1-2 hours)
- [ ] Fix any failing tests by addressing root causes
- [ ] Refactor tests for clarity and maintainability
- [ ] Improve mock implementations if needed
- [ ] Add missing test scenarios identified during validation
- [ ] Final verification of test quality
```

### Quality Standards (Non-Negotiable)

#### Mock Requirements
```go
// ✅ CORRECT: Proper mock with clear expectations
mockClient := mocks.NewHTTPClient(t)
mockClient.On("Do", mock.MatchedBy(func(req *http.Request) bool {
    return req.URL.Path == "/api/auth" && req.Method == "POST"
})).Return(&http.Response{
    StatusCode: 200,
    Body:       io.NopCloser(strings.NewReader(`{"token":"valid"}`)),
}, nil)

// ❌ WRONG: Testing against real services
http.Get("https://real-auth-service.com/token") // NEVER DO THIS
```

#### Test Documentation
```go
// TestAuthService_ValidateToken_ExpiredToken tests that the authentication
// service properly rejects expired JWT tokens and returns an appropriate
// error. This test ensures that:
// 1. Expired tokens are detected correctly
// 2. The service returns a specific expiration error
// 3. No access is granted for expired credentials
//
// Intent: Maintain security by preventing access with expired tokens,
// which is critical for maintaining session security and preventing
// unauthorized access to protected resources.
func TestAuthService_ValidateToken_ExpiredToken(t *testing.T) {
    // Implementation...
}
```

#### Error Testing Requirements
```go
func TestService_ErrorConditions(t *testing.T) {
    tests := []struct {
        name           string
        setupMocks     func(*mocks.MockDependency)
        input          InputType
        expectedError  string
        expectedResult interface{}
    }{
        {
            name: "network timeout error",
            setupMocks: func(m *mocks.MockDependency) {
                m.On("Call").Return(nil, &net.OpError{
                    Op:  "dial",
                    Err: context.DeadlineExceeded,
                })
            },
            expectedError: "timeout",
        },
        {
            name: "invalid input validation",
            input: InputType{Invalid: "data"},
            expectedError: "validation failed",
        },
        // Add more error scenarios...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation with proper assertions
        })
    }
}
```

### Coordination Strategy

#### Daily Standups (15 minutes)
- Each agent reports: packages completed, current focus, blockers
- Identify overlapping dependencies that need coordination
- Adjust priorities based on coverage gains

#### Weekly Reviews (1 hour)
- Review coverage improvements and quality metrics
- Identify high-impact areas for next phase
- Refactor and improve existing tests based on learnings

#### Parallel Execution Guidelines
1. **Avoid conflicts**: Agents work on different packages simultaneously
2. **Share mock patterns**: Document reusable mock implementations
3. **Coordinate dependencies**: Communicate when testing shared components
4. **Review each other's work**: Cross-review for quality and consistency

### Expected Timeline & Milestones

#### Week 1 Milestones
- [ ] Phase 1 complete: +21% coverage improvement (62.9% → 84%)
- [ ] All 0% coverage packages have initial tests
- [ ] Mock patterns established for major dependencies
- [ ] Test documentation standards implemented

#### Week 2 Milestones  
- [ ] Phase 2 complete: Additional refinement and integration tests
- [ ] Coverage sustained above 80%
- [ ] Performance tests implemented
- [ ] Integration test suite functional

#### Week 3 Milestones
- [ ] Phase 3 complete: Edge cases and final polish
- [ ] 80%+ coverage achieved and sustained
- [ ] All tests pass consistently
- [ ] Documentation complete and comprehensive

### Success Criteria

#### Quantitative Goals
- [ ] **Overall coverage ≥ 80%**
- [ ] **No package below 70% coverage** (except platform-specific code)
- [ ] **All critical paths have 100% coverage**
- [ ] **Zero failing tests in CI**

#### Qualitative Goals  
- [ ] **All tests have clear intent documentation**
- [ ] **External dependencies properly mocked**
- [ ] **Error conditions comprehensively tested**
- [ ] **Tests are maintainable and readable**
- [ ] **No shortcuts or workarounds in test implementations**

## 🚫 Absolutely Forbidden

### Unacceptable Shortcuts
- ❌ Skipping tests for "hard to test" code
- ❌ Using `t.Skip()` for anything other than platform limitations
- ❌ Testing against real external services
- ❌ Adding sleeps instead of proper synchronization
- ❌ Commenting out failing assertions
- ❌ Reducing test coverage requirements to meet deadlines

### Quality Enforcement
- All agents must follow the test-writer agent guidelines
- Code reviews required for all test implementations
- Coverage cannot decrease from current levels
- Tests must pass consistently across multiple runs

---

This plan provides a clear roadmap to achieve 80% test coverage through rigorous, parallel testing efforts with zero tolerance for shortcuts or compromises in quality.