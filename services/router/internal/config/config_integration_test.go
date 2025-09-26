package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/test/testutil"
)

const complexBenchmarkConfigYAML = `
version: 1
gateway_pool:
  endpoints:
    - url: wss://primary.example.com
      auth:
        type: bearer
        token_env: BENCHMARK_TOKEN
      weight: 10
      priority: 1
      tags: ["primary", "production"]
      connection:
        timeout_ms: 30000
        keepalive_interval_ms: 60000
      tls:
        verify: true
        min_version: "1.3"
    - url: wss://secondary.example.com
      auth:
        type: oauth2
        client_id: benchmark-client
        client_secret_env: BENCHMARK_SECRET
        token_endpoint: https://auth.example.com/token
        scopes: ["read", "write"]
      weight: 5
      priority: 2
      tags: ["secondary", "backup"]
  load_balancer:
    strategy: weighted
    failover_timeout: 30s
    retry_count: 5
  service_discovery:
    enabled: true
    refresh_interval: 30s
    health_check_interval: 10s
  circuit_breaker:
    enabled: true
    failure_threshold: 10
    recovery_timeout: 60s
  namespace_routing:
    enabled: true
    rules:
      - pattern: "docker\\..*"
        tags: ["docker"]
        priority: 1
      - pattern: "k8s\\..*"
        tags: ["kubernetes"]
        priority: 2
logging:
  level: debug
  format: json
  include_caller: true
  sampling:
    enabled: true
    initial: testIterations
    thereafter: testMaxIterations
metrics:
  enabled: true
  endpoint: localhost:constants.TestMetricsPort
  labels:
    environment: benchmark
    region: us-west-2
tracing:
  enabled: true
  endpoint: localhost:14268
  service_name: mcp-router-benchmark
  sample_rate: 0.1
advanced:
  compression:
    enabled: true
    algorithm: zstd
    level: 6
  circuit_breaker:
    failure_threshold: 20
    success_threshold: 10
    timeout_seconds: 120
  deduplication:
    enabled: true
    cache_size: 10000
    ttl_seconds: 600
  debug:
    log_frames: false
    save_failures: true
    failure_dir: /tmp/benchmark-failures
    enable_pprof: true
    pprof_port: 6060
`

func TestConfig_LoadFromJSON(t *testing.T) {
	setupJSONTestEnv(t)
	configPath := createJSONConfigFile(t)
	cfg := loadAndVerifyJSONConfig(t, configPath)
	verifyJSONConfigContents(t, cfg)
}

func setupJSONTestEnv(t *testing.T) {
	t.Helper()

	if err := os.Setenv("JSON_TEST_TOKEN", "json-token-value"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	t.Cleanup(func() { _ = os.Unsetenv("JSON_TEST_TOKEN") })
}

func createJSONConfigFile(t *testing.T) string {
	t.Helper()

	configJSON := `{
		"version": 1,
		"gateway_pool": {
			"endpoints": [
				{
					"url": "wss://json-test.com",
					"auth": {
						"type": "bearer",
						"token_env": "JSON_TEST_TOKEN"
					},
					"weight": 5,
					"priority": 1,
					"tags": ["json", "test"]
				}
			],
			"load_balancer": {
				"strategy": "round_robin",
				"failover_timeout": "45s",
				"retry_count": 4
			},
			"circuit_breaker": {
				"enabled": true,
				"failure_threshold": 8,
				"recovery_timeout": "40s"
			}
		},
		"logging": {
			"level": "warn",
			"format": "json"
		},
		"metrics": {
			"enabled": true,
			"endpoint": "localhost:9092"
		}
	}`

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	err := os.WriteFile(configPath, []byte(configJSON), 0o600)
	if err != nil {
		t.Fatalf("Failed to write JSON config: %v", err)
	}

	return configPath
}

func loadAndVerifyJSONConfig(t *testing.T, configPath string) *Config {
	t.Helper()

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load JSON config: %v", err)
	}

	return cfg
}

func verifyJSONConfigContents(t *testing.T, cfg *Config) {
	t.Helper()

	if cfg.Version != 1 {
		t.Errorf("Expected version 1, got %d", cfg.Version)
	}

	endpoints := cfg.GetGatewayEndpoints()
	if len(endpoints) != 1 {
		t.Errorf("Expected 1 endpoint, got %d", len(endpoints))
	}

	if endpoints[0].URL != "wss://json-test.com" {
		t.Errorf("Expected JSON URL, got %s", endpoints[0].URL)
	}

	if endpoints[0].Auth.Token != "json-token-value" {
		t.Errorf("Expected token from env var, got %s", endpoints[0].Auth.Token)
	}

	lbConfig := cfg.GetLoadBalancerConfig()
	if lbConfig.FailoverTimeout != 45*time.Second {
		t.Errorf("Expected 45s failover timeout, got %v", lbConfig.FailoverTimeout)
	}
}

func TestConfig_ComplexSecurityScenarios(t *testing.T) {
	runner := CreateSecurityTestRunner(t)

	// Add multi-endpoint scenario.
	runner.AddScenario(BuildMultiEndpointScenario())

	// Add secure storage scenario.
	runner.AddScenario(BuildSecureStorageScenario())

	// Add OAuth2 password scenario.
	runner.AddScenario(BuildOAuth2PasswordScenario())

	// Add missing credentials scenario.
	runner.AddScenario(buildMissingCredentialsScenario())

	// Add complex namespace routing scenario.
	runner.AddScenario(buildComplexNamespaceScenario())

	// Execute all scenarios.
	runner.ExecuteScenarios()
}

func buildMissingCredentialsScenario() SecurityTestScenario {
	return SecurityTestScenario{
		Name: "Missing required OAuth2 credentials",
		ConfigYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://oauth2-missing.com
      auth:
        type: oauth2
        grant_type: password
        client_id: missing-client
        username: testuser
        token_endpoint: https://auth.example.com/token
`,
		ExpectError: true,
	}
}

func buildComplexNamespaceScenario() SecurityTestScenario {
	return BuildComplexNamespaceRoutingScenario()
}

// cleanupEnvironment removes all MCP_ environment variables.
func cleanupEnvironment(t *testing.T) {
	t.Helper()

	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "MCP_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				if err := os.Unsetenv(parts[0]); err != nil {
					t.Logf("Failed to unset env: %v", err)
				}
			}
		}
	}
}

// setupTestEnvironmentVars sets test environment variables and returns cleanup functions.
func setupTestEnvironmentVars(t *testing.T, envVars map[string]string) func() {
	t.Helper()

	cleanupFuncs := make([]func(), 0, len(envVars))

	for k, v := range envVars {
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}

		cleanupFuncs = append(cleanupFuncs, func(key string) func() {
			return func() { _ = os.Unsetenv(key) }
		}(k))
	}

	return func() {
		for _, cleanup := range cleanupFuncs {
			cleanup()
		}
	}
}

// executeConfigValidations runs all validation functions against the config.
func executeConfigValidations(t *testing.T, cfg *Config, validations []func(*Config) error) {
	t.Helper()

	for i, validation := range validations {
		if err := validation(cfg); err != nil {
			t.Errorf("Validation %d failed: %v", i, err)
		}
	}
}

// runConfigOverrideTest executes a single environment override test case.
func runConfigOverrideTest(
	t *testing.T,
	configYAML string,
	envVars map[string]string,
	validations []func(*Config) error,
) {
	t.Helper()

	cleanupEnvironment(t)

	envCleanup := setupTestEnvironmentVars(t, envVars)
	defer envCleanup()

	configPath, fileCleanup := testutil.TempFile(t, configYAML)
	defer fileCleanup()

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	executeConfigValidations(t, cfg, validations)
}

// validateNoOverrides validates config with no environment overrides.
func validateNoOverrides(c *Config) error {
	if c.Logging.Level != "info" {
		return fmt.Errorf("expected info level, got %s", c.Logging.Level)
	}

	return nil
}

// validatePartialOverrides validates config with partial environment overrides.
func validatePartialOverrides(c *Config) error {
	if c.Logging.Level != "debug" {
		return fmt.Errorf("expected debug level from env, got %s", c.Logging.Level)
	}

	if !c.Metrics.Enabled {
		return errors.New("metrics should be enabled from env")
	}

	if c.Logging.Format != "text" {
		return fmt.Errorf("format should remain from config file: %s", c.Logging.Format)
	}

	return nil
}

// validateComprehensiveOverrides validates config with comprehensive environment overrides.
func validateComprehensiveOverrides(c *Config) error {
	if c.Logging.Level != "error" {
		return fmt.Errorf("expected error level from env, got %s", c.Logging.Level)
	}

	if c.Logging.Format != "json" {
		return fmt.Errorf("expected json format from env, got %s", c.Logging.Format)
	}

	if c.Metrics.Endpoint != "localhost:9999" {
		return fmt.Errorf("expected metrics endpoint from env, got %s", c.Metrics.Endpoint)
	}

	lbConfig := c.GetLoadBalancerConfig()
	if lbConfig.Strategy != "weighted" {
		return fmt.Errorf("expected weighted strategy from env, got %s", lbConfig.Strategy)
	}

	cbConfig := c.GetCircuitBreakerConfig()
	if !cbConfig.Enabled {
		return errors.New("circuit breaker should be enabled from env")
	}

	endpoints := c.GetGatewayEndpoints()
	if len(endpoints) > 0 && endpoints[0].Auth.Token != "env-override-token" {
		return fmt.Errorf("expected token from env override, got %s", endpoints[0].Auth.Token)
	}

	return nil
}

// validateTypeCoercion validates environment variable type coercion.
func validateTypeCoercion(c *Config) error {
	lbConfig := c.GetLoadBalancerConfig()
	if lbConfig.RetryCount != 10 {
		return fmt.Errorf("expected retry count 10 from env, got %d", lbConfig.RetryCount)
	}

	cbConfig := c.GetCircuitBreakerConfig()
	if cbConfig.FailureThreshold != 15 {
		return fmt.Errorf("expected failure threshold 15 from env, got %d", cbConfig.FailureThreshold)
	}

	return nil
}

// createEnvironmentOverrideTestCases creates the test cases for environment override testing.
func createEnvironmentOverrideTestCases() []struct {
	name        string
	envVars     map[string]string
	validations []func(*Config) error
} {
	return []struct {
		name        string
		envVars     map[string]string
		validations []func(*Config) error
	}{
		{
			name:        "no environment overrides",
			validations: []func(*Config) error{validateNoOverrides},
		},
		{
			name: "partial environment overrides",
			envVars: map[string]string{
				"MCP_LOGGING_LEVEL":   "debug",
				"MCP_METRICS_ENABLED": "true",
			},
			validations: []func(*Config) error{validatePartialOverrides},
		},
		{
			name: "comprehensive environment overrides",
			envVars: map[string]string{
				"MCP_LOGGING_LEVEL":           "error",
				"MCP_LOGGING_FORMAT":          "json",
				"MCP_METRICS_ENABLED":         "true",
				"MCP_METRICS_ENDPOINT":        "localhost:9999",
				"MCP_LOAD_BALANCER_STRATEGY":  "weighted",
				"MCP_CIRCUIT_BREAKER_ENABLED": "true",
				"MCP_AUTH_TOKEN":              "env-override-token",
			},
			validations: []func(*Config) error{validateComprehensiveOverrides},
		},
		{
			name: "environment variable type coercion",
			envVars: map[string]string{
				"MCP_GATEWAY_POOL_LOAD_BALANCER_RETRY_COUNT":         "10",
				"MCP_GATEWAY_POOL_CIRCUIT_BREAKER_FAILURE_THRESHOLD": "15",
			},
			validations: []func(*Config) error{validateTypeCoercion},
		},
	}
}

func TestConfig_EnvironmentOverridePrecedence(t *testing.T) {
	// Test environment variable override precedence and inheritance.
	baseConfig := `
version: 1
gateway_pool:
  endpoints:
    - url: wss://config-file.com
      auth:
        type: bearer
        token_file: %s
      connection:
        timeout_ms: 5000
  load_balancer:
    strategy: round_robin
    retry_count: 2
  circuit_breaker:
    enabled: false
    failure_threshold: 3
logging:
  level: info
  format: text
metrics:
  enabled: false
  endpoint: localhost:constants.TestMetricsPort
`

	// Create token file.
	tempDir := t.TempDir()
	tokenPath := filepath.Join(tempDir, "token.txt")

	err := os.WriteFile(tokenPath, []byte("file-token"), 0o600)
	if err != nil {
		t.Fatalf("Failed to create token file: %v", err)
	}

	configYAML := fmt.Sprintf(baseConfig, tokenPath)

	tests := createEnvironmentOverrideTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runConfigOverrideTest(t, configYAML, tt.envVars, tt.validations)
		})
	}
}

func TestConfig_ConfigFileFormats(t *testing.T) {
	setupFormatTestEnv(t)

	configData := createFormatTestConfigData()
	tests := createFileFormatTests()
	runFileFormatTests(t, configData, tests)
}

func setupFormatTestEnv(t *testing.T) {
	t.Helper()

	if err := os.Setenv("FORMAT_TEST_TOKEN", "format-token"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	t.Cleanup(func() { _ = os.Unsetenv("FORMAT_TEST_TOKEN") })
}

func createFormatTestConfigData() map[string]interface{} {
	return map[string]interface{}{
		"version": 1,
		"gateway_pool": map[string]interface{}{
			"endpoints": []map[string]interface{}{
				{
					"url": "wss://format-test.com",
					"auth": map[string]interface{}{
						"type":      "bearer",
						"token_env": "FORMAT_TEST_TOKEN",
					},
				},
			},
		},
		"logging": map[string]interface{}{
			"level":  "debug",
			"format": "json",
		},
	}
}

func createFileFormatTests() []struct {
	name     string
	filename string
	marshal  func(interface{}) ([]byte, error)
} {
	return []struct {
		name     string
		filename string
		marshal  func(interface{}) ([]byte, error)
	}{
		{
			name:     "YAML format",
			filename: "config.yaml",
			marshal: func(v interface{}) ([]byte, error) {
				// Simple YAML marshaling for test
				return []byte(`
version: 1
gateway_pool:
  endpoints:
    - url: wss://format-test.com
      auth:
        type: bearer
        token_env: FORMAT_TEST_TOKEN
logging:
  level: debug
  format: json
`), nil
			},
		},
		{
			name:     "JSON format",
			filename: "config.json",
			marshal:  json.Marshal,
		},
	}
}

func runFileFormatTests(t *testing.T, configData map[string]interface{}, tests []struct {
	name     string
	filename string
	marshal  func(interface{}) ([]byte, error)
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTestConfigFile(t, tt, configData)
			cfg := loadAndValidateConfig(t, configPath, tt.name)
			verifyFormatTestProperties(t, cfg)
		})
	}
}

func createTestConfigFile(t *testing.T, tt struct {
	name     string
	filename string
	marshal  func(interface{}) ([]byte, error)
}, configData map[string]interface{}) string {
	t.Helper()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, tt.filename)

	configBytes, err := tt.marshal(configData)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	err = os.WriteFile(configPath, configBytes, 0o600)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	return configPath
}

func loadAndValidateConfig(t *testing.T, configPath, formatName string) *Config {
	t.Helper()

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load %s config: %v", formatName, err)
	}

	return cfg
}

func verifyFormatTestProperties(t *testing.T, cfg *Config) {
	t.Helper()

	// Verify common properties
	if cfg.Version != 1 {
		t.Errorf("Expected version 1, got %d", cfg.Version)
	}

	if cfg.Logging.Level != "debug" {
		t.Errorf("Expected debug level, got %s", cfg.Logging.Level)
	}

	endpoints := cfg.GetGatewayEndpoints()
	if len(endpoints) != 1 {
		t.Errorf("Expected 1 endpoint, got %d", len(endpoints))
	}

	if endpoints[0].Auth.Token != "format-token" {
		t.Errorf("Expected token from env, got %s", endpoints[0].Auth.Token)
	}
}

func TestConfig_SecurityValidation(t *testing.T) {
	// Test security-related validation scenarios.
	securityTests := createSecurityValidationTests()
	runSecurityValidationTests(t, securityTests)
}

func createSecurityValidationTests() []struct {
	name        string
	configYAML  string
	envVars     map[string]string
	expectError bool
	errorMsg    string
} {
	return []struct {
		name        string
		configYAML  string
		envVars     map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name: "insecure file permissions warning",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: bearer
        token_file: %s
`,
			// This would be tested with actual file permission checks in a real scenario.
		},
		{
			name: "missing authentication credentials",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: bearer
`,
			expectError: true,
			errorMsg:    "no auth token found",
		},
		{
			name: "malformed OAuth2 configuration",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: oauth2
        client_id: test
`,
			expectError: true,
			errorMsg:    "token_endpoint are required",
		},
	}
}

func runSecurityValidationTests(t *testing.T, securityTests []struct {
	name        string
	configYAML  string
	envVars     map[string]string
	expectError bool
	errorMsg    string
}) {
	t.Helper()

	for _, tt := range securityTests {
		t.Run(tt.name, func(t *testing.T) {
			setupSecurityTestEnvironment(t, tt.envVars)
			configYAML := processSecurityConfigYAML(t, tt.configYAML)
			validateSecurityConfig(t, configYAML, tt.expectError, tt.errorMsg)
		})
	}
}

func setupSecurityTestEnvironment(t *testing.T, envVars map[string]string) {
	t.Helper()

	// Set environment variables.
	for k, v := range envVars {
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
		defer func(key string) { _ = os.Unsetenv(key) }(k)
	}
}

func processSecurityConfigYAML(t *testing.T, configYAML string) string {
	t.Helper()

	if strings.Contains(configYAML, "%s") {
		// Create a token file if needed.
		tempDir := t.TempDir()
		tokenPath := filepath.Join(tempDir, "token.txt")
		_ = os.WriteFile(tokenPath, []byte("test-token"), 0o600)
		return fmt.Sprintf(configYAML, tokenPath)
	}
	return configYAML
}

func validateSecurityConfig(t *testing.T, configYAML string, expectError bool, errorMsg string) {
	t.Helper()

	configPath, cleanup := testutil.TempFile(t, configYAML)
	defer cleanup()

	_, err := Load(configPath)

	if expectError {
		if err == nil {
			t.Error("Expected security validation error but got none")
		} else if errorMsg != "" && !strings.Contains(err.Error(), errorMsg) {
			t.Errorf("Expected error containing '%s', got '%s'", errorMsg, err.Error())
		}
	} else if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func BenchmarkConfig_LoadComplex(b *testing.B) {
	// Benchmark loading a complex configuration.
	complexConfig := generateComplexBenchmarkConfig()

	setupIntegrationBenchmarkEnvironment(b)

	defer cleanupBenchmarkEnvironment()

	configPath, cleanup := testutil.TempFile(b, complexConfig)
	defer cleanup()

	runComplexConfigBenchmark(b, configPath)
}

func generateComplexBenchmarkConfig() string {
	return complexBenchmarkConfigYAML
}

func setupIntegrationBenchmarkEnvironment(b *testing.B) {
	b.Helper()

	if err := os.Setenv("BENCHMARK_TOKEN", "benchmark-token"); err != nil {
		b.Fatalf("Failed to set environment variable: %v", err)
	}

	if err := os.Setenv("BENCHMARK_SECRET", "benchmark-secret"); err != nil {
		b.Fatalf("Failed to set environment variable: %v", err)
	}
}

func cleanupBenchmarkEnvironment() {
	_ = os.Unsetenv("BENCHMARK_TOKEN")
	_ = os.Unsetenv("BENCHMARK_SECRET")
}

func runComplexConfigBenchmark(b *testing.B, configPath string) {
	b.Helper()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := Load(configPath)
		if err != nil {
			b.Fatalf("Load failed: %v", err)
		}
	}
}
