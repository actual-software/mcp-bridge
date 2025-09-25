# MCP Bridge Testing Infrastructure

Comprehensive testing framework for MCP Bridge including contract testing, performance regression detection, and smoke tests.

## Overview

The MCP Bridge testing infrastructure provides:

- **Contract Testing**: API contract validation and backward compatibility checks
- **Performance Regression Testing**: Automated performance baseline comparison
- **Smoke Testing**: Quick validation of critical functionality
- **Load Testing**: High-concurrency stress testing
- **Integration Testing**: End-to-end workflow validation

## Quick Start

### Running Tests

```bash
# Quick smoke test (< 1 minute)
./run-tests.sh -l quick -t smoke

# Standard test suite
./run-tests.sh -l standard -t all

# Full test suite with report
./run-tests.sh -l full -t all -r

# CI pipeline test
./run-tests.sh -c -l standard -t all
```

### Using Make Targets

```bash
# Run specific test types
make smoke-quick        # Quick smoke tests
make contract          # API contract tests
make performance       # Performance tests
make regression        # Regression tests

# Run all tests
make test-all

# Generate reports
make report
make coverage
```

## Test Types

### 1. Smoke Tests (`smoke/`)

Quick validation tests that verify critical functionality is working.

**Features:**
- Critical path validation
- Service health checks
- Basic functionality tests
- Resource availability checks
- < 1 minute execution time for quick tests

**Test Levels:**
- `quick`: Critical tests only (< 1 minute)
- `full`: All smoke tests (< 5 minutes)
- `critical`: Critical path with fail-fast

**Example:**
```go
func TestQuickSmoke(t *testing.T) {
    suite := NewSmokeTestSuite()
    suite.RunQuickTests(t)
}
```

### 2. Contract Tests (`contract/`)

API contract validation ensuring backward compatibility and specification compliance.

**Features:**
- OpenAPI specification validation
- Response schema validation
- Backward compatibility checks
- Authentication contract testing
- Rate limiting validation
- Error response contracts

**Example:**
```go
func TestHealthEndpointContract(t *testing.T) {
    resp, err := client.Get(baseURL + "/health")
    require.NoError(t, err)
    
    // Validate response matches contract
    assert.Equal(t, http.StatusOK, resp.StatusCode)
    assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
}
```

### 3. Performance Regression Tests (`performance/`)

Automated performance regression detection with baseline comparison.

**Features:**
- Automatic baseline management
- Statistical analysis (mean, p95, p99)
- Regression detection with configurable thresholds
- Memory leak detection
- CPU profile comparison
- Continuous monitoring mode

**Metrics Tracked:**
- Request latency
- Throughput
- Memory usage
- CPU utilization
- Connection time
- Reconnection time

**Example:**
```go
func TestGatewayPerformanceRegression(t *testing.T) {
    baseline := LoadOrCreateBaseline(t, "gateway")
    
    // Run benchmark
    samples := benchmarkGatewayLatency()
    stats := CalculateStats(samples)
    
    // Compare with baseline
    result := CompareMetrics(baseline.Metrics["latency"], stats)
    
    if result.IsRegression {
        t.Errorf("Performance regression: %.2f%% worse", result.ChangePercent)
    }
}
```

## Test Configuration

### Environment Variables

```bash
# Service configuration
export MCP_BASE_URL=http://localhost:8080
export MCP_AUTH_TOKEN=your-auth-token

# Test configuration
export TEST_LEVEL=standard      # quick, standard, full, nightly
export TEST_TIMEOUT=30m         # Overall timeout
export CI=true                  # CI mode

# Performance thresholds
export REGRESSION_THRESHOLD=10  # 10% regression threshold
export MAX_P99_LATENCY=200     # 200ms max P99
```

### Configuration Files

**`test-config.yaml`:**
```yaml
smoke:
  timeout: 60s
  critical_only: false
  fail_fast: true

contract:
  validate_schemas: true
  check_deprecation: true
  openapi_spec: ../api/openapi.yaml

performance:
  baseline_file: baselines/baseline.json
  regression_threshold: 10  # 10% regression
  sample_size: 1000
  warmup_iterations: 100
```

## Performance Baselines

### Managing Baselines

```bash
# Update baseline with current performance
make baseline-update

# Save specific test run as baseline
make baseline-save

# Compare current performance with baseline
make baseline-compare
```

### Baseline Format

```json
{
  "timestamp": "2024-01-20T10:00:00Z",
  "git_commit": "abc123",
  "metrics": {
    "request_latency": {
      "mean": 45.2,
      "p95": 89.5,
      "p99": 120.3,
      "samples": 1000,
      "unit": "ms",
      "thresholds": {
        "max_mean": 50,
        "max_p99": 200,
        "regression_percent": 10
      }
    }
  }
}
```

## CI/CD Integration

### GitHub Actions

```yaml
name: Test Suite

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      
      - name: Run smoke tests
        run: make ci-smoke
        
      - name: Run contract tests
        run: make ci-contract
        
      - name: Run regression tests
        run: make ci-regression
        
      - name: Upload results
        uses: actions/upload-artifact@v2
        with:
          name: test-reports
          path: reports/
```

### Jenkins Pipeline

```groovy
pipeline {
    agent any
    
    stages {
        stage('Smoke Tests') {
            steps {
                sh './run-tests.sh -c -l quick -t smoke'
            }
        }
        
        stage('Contract Tests') {
            steps {
                sh 'make contract'
            }
        }
        
        stage('Performance Tests') {
            when { branch 'main' }
            steps {
                sh 'make regression'
            }
        }
    }
    
    post {
        always {
            publishHTML([
                reportDir: 'reports',
                reportFiles: 'test-report.html',
                reportName: 'Test Report'
            ])
        }
    }
}
```

## Test Reports

### Report Generation

```bash
# Generate JSON reports
make report

# Generate HTML report
make report-html

# Generate coverage report
make coverage
```

### Report Format

Reports are generated in multiple formats:

1. **JSON**: Machine-readable test results
2. **HTML**: Human-readable test report with charts
3. **Summary**: Text summary of test results
4. **Coverage**: Code coverage report

### Example Report Structure

```
reports/
├── smoke.json           # Smoke test results
├── contract.json        # Contract test results
├── performance.json     # Performance test results
├── test-report.html     # Combined HTML report
├── coverage.html        # Code coverage report
└── summary.txt         # Text summary
```

## Test Development

### Adding New Smoke Tests

```go
// Add to smoke/smoke_test.go
{
    Name:        "New_Feature_Test",
    Description: "Verify new feature works",
    Critical:    true,
    Timeout:     5 * time.Second,
    Test: func(t *testing.T) error {
        // Test implementation
        resp, err := http.Get(baseURL + "/new-feature")
        if err != nil {
            return err
        }
        if resp.StatusCode != http.StatusOK {
            return fmt.Errorf("unexpected status: %d", resp.StatusCode)
        }
        return nil
    },
}
```

### Adding New Contract Tests

```go
// Add to contract/contract_test.go
func (s *ContractTestSuite) TestNewEndpointContract() {
    resp, err := s.client.Get(s.baseURL + "/new-endpoint")
    s.Require().NoError(err)
    
    // Validate contract
    s.Assert().Equal(http.StatusOK, resp.StatusCode)
    
    var body map[string]interface{}
    err = json.NewDecoder(resp.Body).Decode(&body)
    s.Require().NoError(err)
    
    // Validate required fields
    s.Assert().Contains(body, "required_field")
}
```

### Adding Performance Tests

```go
// Add to performance/regression_test.go
{
    name:      "new_metric",
    benchmark: benchmarkNewMetric,
    threshold: ThresholdConfig{
        MaxMean:           100,
        MaxP99:            500,
        RegressionPercent: 15,
    },
}

func benchmarkNewMetric() []float64 {
    samples := make([]float64, 1000)
    // Benchmark implementation
    return samples
}
```

## Troubleshooting

### Common Issues

**Services Not Available:**
```bash
# Check service health
curl http://localhost:8080/health
curl http://localhost:9091/health

# Start services
docker-compose up -d
```

**Test Timeouts:**
```bash
# Increase timeout
TEST_TIMEOUT=60m make test-all

# Run with shorter test suite
make test-quick
```

**Baseline Comparison Failures:**
```bash
# Update baseline if performance improved
make baseline-update

# Or adjust thresholds in config
REGRESSION_THRESHOLD=20 make regression
```

### Debug Mode

```bash
# Run with debug output
GODEBUG=http2debug=2 make contract

# Run specific test with verbose output
go test -v ./smoke -run TestQuickSmoke

# Generate CPU/memory profiles
make profile-performance
```

## Best Practices

### Test Writing

1. **Keep tests fast**: Smoke tests < 1 min, contract tests < 10 min
2. **Use meaningful names**: Describe what is being tested
3. **Fail fast**: Critical tests should stop on first failure
4. **Clean up**: Always clean up resources after tests
5. **Document thresholds**: Explain why specific thresholds were chosen

### Performance Testing

1. **Warm up**: Run warmup iterations before measuring
2. **Statistical significance**: Use sufficient sample size (1000+)
3. **Isolate tests**: Run performance tests in isolation
4. **Monitor resources**: Track CPU, memory, network during tests
5. **Version baselines**: Keep baselines with git commits

### CI/CD Integration

1. **Fail fast in CI**: Run quick tests first
2. **Parallel execution**: Run independent tests in parallel
3. **Archive results**: Keep test results for trend analysis
4. **Gate deployments**: Block deployment on test failures
5. **Monitor trends**: Track performance over time

## Contributing

### Adding Tests

1. Choose appropriate test type (smoke/contract/performance)
2. Follow existing patterns and conventions
3. Add test to appropriate file
4. Update Makefile if needed
5. Document new tests in README

### Review Checklist

- [ ] Tests pass locally
- [ ] Tests are documented
- [ ] Thresholds are reasonable
- [ ] No flaky tests
- [ ] Cleanup is proper
- [ ] CI integration works

## Support

For issues or questions:

1. Check troubleshooting section
2. Review test logs in `reports/`
3. Run tests with debug output
4. Contact the platform team

## License

See [LICENSE](../LICENSE) file for details.