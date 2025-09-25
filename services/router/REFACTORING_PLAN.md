# Comprehensive Refactoring Plan for MCP Router

## Goal
Achieve 0 cyclomatic complexity issues while establishing professional, descriptive naming conventions and unified design patterns.

## Current State
- 69 cyclomatic complexity issues remaining
- Mix of generic "New*" constructors and ad-hoc patterns
- Test functions with excessive complexity (up to 23)

## Design Principles

### 1. Naming Conventions

#### Constructors/Factories
- ❌ BAD: `NewClient`, `NewManager`, `NewPool`
- ✅ GOOD: 
  - `EstablishWebSocketConnection` - Creates and connects
  - `InitializeClientManager` - Sets up with defaults
  - `BuildAuthenticatedClient` - Constructs with auth
  - `CreateConnectionPool` - Instantiates pool
  - `ConfigureGatewayEndpoint` - Sets up endpoint

#### Builders
- ❌ BAD: `NewBuilder`, `Builder`
- ✅ GOOD:
  - `ClientConfigurationBuilder` - Builds client config
  - `ConnectionSpecBuilder` - Builds connection specs
  - `AuthenticationChainBuilder` - Builds auth chain

#### Handlers/Processors
- ❌ BAD: `Handler`, `Process`, `Handle`
- ✅ GOOD:
  - `RequestProcessor` - Processes requests
  - `ResponseValidator` - Validates responses
  - `MessageRouter` - Routes messages
  - `AuthenticationVerifier` - Verifies auth
  - `ConnectionRecoveryManager` - Manages recovery

#### Test Helpers
- ❌ BAD: `testSetup`, `helper`, `util`
- ✅ GOOD:
  - `ConfigureTestEnvironment` - Sets up test env
  - `GenerateMockResponse` - Creates mock data
  - `AssertConnectionState` - Validates state
  - `SimulateNetworkFailure` - Simulates failures

## Unified Patterns

### 1. Builder Pattern Template

```go
type [Resource]ConfigurationBuilder struct {
    // Descriptive field names
    targetEndpoint   string
    authentication   AuthenticationConfig
    performanceTuning PerformanceConfig
    errorHandling    ErrorHandlingStrategy
}

func Configure[Resource]() *[Resource]ConfigurationBuilder
func (b *[Resource]ConfigurationBuilder) WithAuthentication(auth AuthenticationConfig) *[Resource]ConfigurationBuilder
func (b *[Resource]ConfigurationBuilder) WithPerformanceTuning(perf PerformanceConfig) *[Resource]ConfigurationBuilder
func (b *[Resource]ConfigurationBuilder) Build[Resource]() ([Resource], error)
```

### 2. Handler Pattern Template

```go
type [Operation]Processor struct {
    validator    InputValidator
    transformer  DataTransformer
    executor     OperationExecutor
    errorHandler ErrorRecoveryStrategy
}

func Initialize[Operation]Processor(deps Dependencies) *[Operation]Processor
func (p *[Operation]Processor) Process[Operation](ctx context.Context, input Input) (Output, error)
func (p *[Operation]Processor) ValidateInput(input Input) error
func (p *[Operation]Processor) TransformData(data Data) TransformedData
func (p *[Operation]Processor) ExecuteOperation(ctx context.Context, data TransformedData) Result
func (p *[Operation]Processor) HandleError(err error) error
```

### 3. Test Helper Pattern

```go
type Test[Resource]Environment struct {
    resource     [Resource]
    mockServices map[string]MockService
    assertions   AssertionHelper
}

func EstablishTest[Resource]Environment(t *testing.T) *Test[Resource]Environment
func (env *Test[Resource]Environment) Configure[Scenario](scenario ScenarioConfig)
func (env *Test[Resource]Environment) Execute[Operation](operation Operation) Result
func (env *Test[Resource]Environment) Assert[Condition](condition Condition)
func (env *Test[Resource]Environment) Cleanup()
```

## Refactoring Strategy

### Phase 1: Core Infrastructure (Priority: HIGH)
1. **Manager Components** (4 issues in manager.go)
   - `NewDirectClientManager` → `InitializeClientOrchestrator`
   - Extract `ProtocolNegotiator`, `ConnectionPoolManager`, `HealthMonitor`

2. **Client Components** (Multiple files, ~15 issues)
   - `NewSSEClient` → `EstablishServerSentEventStream`
   - `NewWebSocketClient` → `EstablishBidirectionalWebSocket`
   - `NewHTTPClient` → `ConfigureHTTPTransport`
   - `NewStdioClient` → `InitializeProcessCommunication`

3. **Gateway Components** (7 issues)
   - `NewGatewayClient` → `ConfigureGatewayConnection`
   - `NewTCPClient` → `EstablishTCPChannel`
   - Extract `LoadBalancerStrategy`, `CircuitBreakerManager`

### Phase 2: Request/Response Pipeline (Priority: HIGH)
1. **Request Processing** (10 issues)
   - Extract `RequestValidator`, `RequestTransformer`, `RequestRouter`
   - Create `RequestPipelineOrchestrator`

2. **Response Handling** (8 issues)
   - Extract `ResponseParser`, `ResponseValidator`, `ResponseTransformer`
   - Create `ResponsePipelineOrchestrator`

### Phase 3: Configuration & Security (Priority: MEDIUM)
1. **Config Validation** (6 issues)
   - Extract `EndpointValidator`, `AuthenticationConfigValidator`, `TLSConfigValidator`
   - Create `ConfigurationValidationChain`

2. **Security Components** (10 issues in tests)
   - Extract `TokenManager`, `CredentialVault`, `CertificateValidator`
   - Create test helpers: `SimulateSecurityScenario`, `AssertSecurityState`

### Phase 4: Test Refactoring (Priority: MEDIUM)
1. **Test Helpers** (~25 issues)
   - Create `TestScenarioBuilder`, `MockServiceOrchestrator`
   - Extract common assertions into `AssertionLibrary`
   - Create `TestDataGenerator` for mock data

### Phase 5: Observability & Metrics (Priority: LOW)
1. **Monitoring Components** (5 issues)
   - Extract `MetricsCollector`, `HealthChecker`, `TracingManager`
   - Create `ObservabilityPipeline`

## Implementation Order

1. **Week 1**: Core Infrastructure
   - Refactor manager.go (4 issues)
   - Refactor client constructors (15 issues)
   - Expected reduction: 19 issues

2. **Week 2**: Request/Response Pipeline
   - Refactor request processing (10 issues)
   - Refactor response handling (8 issues)
   - Expected reduction: 18 issues

3. **Week 3**: Configuration & Security
   - Refactor config validation (6 issues)
   - Refactor security components (10 issues)
   - Expected reduction: 16 issues

4. **Week 4**: Test Refactoring
   - Extract test helpers (25 issues)
   - Expected reduction: 25 issues

5. **Week 5**: Final Cleanup
   - Observability components (5 issues)
   - Any remaining issues
   - Expected: 0 issues

## Success Metrics
- ✅ 0 cyclomatic complexity issues
- ✅ No generic "New*" constructors
- ✅ All functions have descriptive, intention-revealing names
- ✅ Unified patterns across the codebase
- ✅ Test complexity reduced through helper extraction
- ✅ Clear separation of concerns
- ✅ Professional, maintainable code structure

## Anti-Patterns to Eliminate
- ❌ Generic names: `New*`, `Create*`, `Make*` without context
- ❌ Verb-only names: `Process()`, `Handle()`, `Execute()`
- ❌ Mixed responsibilities in single functions
- ❌ Deeply nested if/else chains
- ❌ Switch statements with 10+ cases
- ❌ Test functions doing setup, execution, and assertion in one function
- ❌ Magic strings and numbers
- ❌ Unclear error handling paths