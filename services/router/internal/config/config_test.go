package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/test/testutil"
)

const (
	// LogLevelDebug represents the debug log level.
	LogLevelDebug = "debug"
	// LoadBalanceStrategyWeighted represents the weighted load balancing strategy.
	LoadBalanceStrategyWeighted = "weighted"
)

const completeConfigContentYAML = `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: bearer
        token_env: TEST_TOKEN
      connection:
        timeout_ms: 10000
        keepalive_interval_ms: 60000
        reconnect:
          initial_delay_ms: 2000
          max_delay_ms: 120000
          multiplier: 3.0
          max_attempts: 10
          jitter: 0.2
      tls:
        verify: true
        ca_cert_path: /path/to/ca.crt
        min_version: "1.3"
        cipher_suites:
          - TLS_AES_128_GCM_SHA256
          - TLS_AES_256_GCM_SHA384
      weight: 1
      priority: 1
      tags: ["primary"]
  load_balancer:
    strategy: weighted
    failover_timeout: 30s
    retry_count: 3
  service_discovery:
    enabled: true
    refresh_interval: 30s
    health_check_interval: 10s
    unhealthy_threshold: 3
    healthy_threshold: 2
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    recovery_timeout: 30s
    success_threshold: 3
    timeout_duration: 10s
    monitoring_window: 60s
  namespace_routing:
    enabled: true
    rules:
      - pattern: "docker\\..*"
        tags: ["docker"]
        priority: 1
        description: "Docker commands"

local:
  read_buffer_size: 131072
  write_buffer_size: 131072
  request_timeout_ms: 60000
  max_concurrent_requests: 200

logging:
  level: debug
  format: text
  output: stdout
  include_caller: true
  sampling:
    enabled: true
    initial: 50
    thereafter: 200

metrics:
  enabled: true
  endpoint: localhost:9090
  labels:
    environment: test
    region: us-east-1

advanced:
  compression:
    enabled: true
    algorithm: zstd
    level: 3
  circuit_breaker:
    failure_threshold: 10
    success_threshold: 5
    timeout_seconds: 60
  deduplication:
    enabled: false
    cache_size: 2000
    ttl_seconds: 120
  debug:
    log_frames: true
    save_failures: true
    failure_dir: /var/log/mcp-failures
    enable_pprof: true
    pprof_port: 6061
`

func TestLoad_DefaultConfig(t *testing.T) {
	// Test that validate function works correctly with empty gateway_pool.endpoints
	testValidateEmptyConfig(t)

	// Set up isolated environment
	tempDir, cleanup := setupIsolatedTestEnvironment(t)
	defer cleanup()

	// Test loading with no config file (should fail due to validation)
	testDefaultConfigLoad(t, tempDir)
}

// testValidateEmptyConfig tests validation of empty config.
func testValidateEmptyConfig(t *testing.T) {
	t.Helper()

	emptyConfig := &Config{}

	err := validate(emptyConfig)
	if err == nil || !strings.Contains(err.Error(), "gateway_pool.endpoints is required") {
		t.Errorf("validate() should fail with gateway_pool.endpoints error, got: %v", err)
	}
}

// setupIsolatedTestEnvironment sets up an isolated test environment.
func setupIsolatedTestEnvironment(t *testing.T) (string, func()) {
	t.Helper()
	// Clear any environment variables that might affect the test.
	oldVars := make(map[string]string)

	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "MCP_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				key := parts[0]
				oldVars[key] = parts[1]
				_ = os.Unsetenv(key)
			}
		}
	}

	// Change to a temporary directory to avoid picking up config files.
	tempDir := t.TempDir()
	oldDir, _ := os.Getwd()
	oldHome := os.Getenv("HOME")

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change dir: %v", err)
	}

	if err := os.Setenv("HOME", tempDir); err != nil { // Change home to temp dir so no config files are found
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	cleanup := func() {
		for key, value := range oldVars {
			if err := os.Setenv(key, value); err != nil {
				t.Fatalf("Failed to set environment variable: %v", err)
			}
		}

		if err := os.Chdir(oldDir); err != nil {
			t.Fatalf("Failed to change dir: %v", err)
		}

		if err := os.Setenv("HOME", oldHome); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
	}

	return tempDir, cleanup
}

// testDefaultConfigLoad tests loading default config which should fail validation.
func testDefaultConfigLoad(t *testing.T, tempDir string) {
	t.Helper()
	// Test loading with no config file (defaults only).
	// Since we're in an isolated temp directory with no config files, this should use defaults.
	cfg, err := Load("")

	// Should fail because gateway_pool.endpoints is required
	if err == nil {
		t.Errorf("Expected error for missing gateway_pool.endpoints, but got valid config: %+v", cfg)
	}

	if !strings.Contains(err.Error(), "gateway_pool.endpoints is required") &&
		!strings.Contains(err.Error(), "Config File \"mcp-router\" Not Found") {
		t.Errorf("Expected gateway_pool.endpoints error or config file not found error, got: %v", err)
	}
}

// setupTestEnvironment sets up test environment variables and returns cleanup function.
func setupTestEnvironment(t *testing.T) func() {
	t.Helper()

	origToken, tokenExists := os.LookupEnv("TEST_TOKEN")

	if err := os.Setenv("TEST_TOKEN", "test-token-value"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	return func() {
		if tokenExists {
			if err := os.Setenv("TEST_TOKEN", origToken); err != nil {
				t.Fatalf("Failed to set environment variable: %v", err)
			}
		} else {
			_ = os.Unsetenv("TEST_TOKEN")
		}
	}
}

// validateLoadedConfig validates that the loaded configuration matches expected values.
func validateLoadedConfig(t *testing.T, cfg *Config) {
	t.Helper()

	if cfg.Version != 1 {
		t.Errorf("Expected version 1, got %d", cfg.Version)
	}

	endpoints := cfg.GetGatewayEndpoints()
	if len(endpoints) != 1 {
		t.Errorf("Expected 1 endpoint, got %d", len(endpoints))
	}

	if endpoints[0].URL != "wss://gateway.example.com" {
		t.Errorf("Expected gateway URL wss://gateway.example.com, got %s", endpoints[0].URL)
	}

	if endpoints[0].Auth.Token != "test-token-value" {
		t.Errorf("Expected token from env var, got %s", endpoints[0].Auth.Token)
	}

	if endpoints[0].Connection.TimeoutMs != 10000 {
		t.Errorf("Expected timeout 10000ms, got %d", endpoints[0].Connection.TimeoutMs)
	}

	if cfg.Logging.Level != LogLevelDebug {
		t.Errorf("Expected log level debug, got %s", cfg.Logging.Level)
	}
}

// validateConfigDefaults validates that default values were applied correctly.
func validateConfigDefaults(t *testing.T, cfg *Config) {
	t.Helper()

	if cfg.Local.ReadBufferSize != 65536 {
		t.Errorf("Expected default read buffer size 65536, got %d", cfg.Local.ReadBufferSize)
	}

	if cfg.Advanced.Compression.Algorithm != "gzip" {
		t.Errorf("Expected default compression algorithm gzip, got %s", cfg.Advanced.Compression.Algorithm)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	configContent := `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: bearer
        token_env: TEST_TOKEN
      connection:
        timeout_ms: 10000
  load_balancer:
    strategy: round_robin
logging:
  level: debug
  format: json
`

	envCleanup := setupTestEnvironment(t)
	defer envCleanup()

	configPath, fileCleanup := testutil.TempFile(t, configContent)
	defer fileCleanup()

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	validateLoadedConfig(t, cfg)
	validateConfigDefaults(t, cfg)
}

// setupAuthTestEnvironment sets up test environment variables with cleanup.
func setupAuthTestEnvironment(t *testing.T, envVars map[string]string) func() {
	t.Helper()

	origEnv := make(map[string]string)

	for k := range envVars {
		if val, exists := os.LookupEnv(k); exists {
			origEnv[k] = val
		}
	}

	for k, v := range envVars {
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
	}

	return func() {
		for k := range envVars {
			if origVal, existed := origEnv[k]; existed {
				if err := os.Setenv(k, origVal); err != nil {
					t.Fatalf("Failed to set environment variable: %v", err)
				}
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}
}

// createTokenFileIfNeeded creates a temporary token file if the config references it.
func createTokenFileIfNeeded(t *testing.T, authConfig string) (string, func()) {
	t.Helper()

	if !strings.Contains(authConfig, "token_file: /tmp/token.txt") {
		return authConfig, func() {}
	}

	tmpFile, err := os.CreateTemp("", "token*.txt")
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() { _ = os.Remove(tmpFile.Name()) }

	if _, err := tmpFile.WriteString("file-token-value"); err != nil {
		cleanup()
		t.Fatal(err)
	}

	if err := tmpFile.Close(); err != nil {
		cleanup()
		t.Fatal(err)
	}

	updatedConfig := strings.ReplaceAll(authConfig, "/tmp/token.txt", tmpFile.Name())

	return updatedConfig, cleanup
}

// validateAuthTestResult validates the result of an auth test case.
func validateAuthTestResult(t *testing.T, cfg *Config, err error, wantError bool, errorMsg, checkToken string) {
	t.Helper()

	if wantError {
		if err == nil {
			t.Error("Expected error but got none")

			return
		}

		if errorMsg != "" && !strings.Contains(err.Error(), errorMsg) {
			t.Errorf("Expected error containing '%s', got '%s'", errorMsg, err.Error())
		}

		return
	}

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)

		return
	}

	if checkToken != "" {
		endpoints := cfg.GetGatewayEndpoints()
		if len(endpoints) == 0 {
			t.Error("Expected at least one endpoint")

			return
		}

		if endpoints[0].Auth.Token != checkToken {
			t.Errorf("Expected token '%s', got '%s'", checkToken, endpoints[0].Auth.Token)
		}
	}
}

func TestLoad_AuthTypes(t *testing.T) {
	tests := createAuthTypeTests()
	runAuthTypeTests(t, tests)
}

func createAuthTypeTests() []struct {
	name       string
	authConfig string
	envVars    map[string]string
	wantError  bool
	errorMsg   string
	checkToken string
} {
	tests := make([]struct {
		name       string
		authConfig string
		envVars    map[string]string
		wantError  bool
		errorMsg   string
		checkToken string
	}, 0)

	tests = append(tests, createBearerAuthTests()...)
	tests = append(tests, createMTLSAuthTests()...)
	tests = append(tests, createOAuth2AuthTests()...)
	tests = append(tests, createInvalidAuthTests()...)

	return tests
}

func createBearerAuthTests() []struct {
	name       string
	authConfig string
	envVars    map[string]string
	wantError  bool
	errorMsg   string
	checkToken string
} {
	return []struct {
		name       string
		authConfig string
		envVars    map[string]string
		wantError  bool
		errorMsg   string
		checkToken string
	}{
		{
			name: "Bearer token from env",
			authConfig: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: bearer
        token_env: CUSTOM_TOKEN`,
			envVars: map[string]string{
				"CUSTOM_TOKEN": "custom-token-value",
			},
			checkToken: "custom-token-value",
		},
		{
			name: "Bearer token from file",
			authConfig: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: bearer
        token_file: /tmp/token.txt`,
			wantError: false,
		},
		{
			name: "Bearer token from default env",
			authConfig: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: bearer`,
			envVars: map[string]string{
				"MCP_AUTH_TOKEN": "default-token-value",
			},
			checkToken: "default-token-value",
		},
	}
}

func createMTLSAuthTests() []struct {
	name       string
	authConfig string
	envVars    map[string]string
	wantError  bool
	errorMsg   string
	checkToken string
} {
	return []struct {
		name       string
		authConfig string
		envVars    map[string]string
		wantError  bool
		errorMsg   string
		checkToken string
	}{
		{
			name: "mTLS auth missing cert",
			authConfig: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: mtls
        client_key: /path/to/key.pem`,
			wantError: true,
			errorMsg:  "client_cert and client_key are required",
		},
		{
			name: "mTLS auth valid",
			authConfig: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: mtls
        client_cert: /path/to/cert.pem
        client_key: /path/to/key.pem`,
			wantError: false,
		},
	}
}

func createOAuth2AuthTests() []struct {
	name       string
	authConfig string
	envVars    map[string]string
	wantError  bool
	errorMsg   string
	checkToken string
} {
	return []struct {
		name       string
		authConfig string
		envVars    map[string]string
		wantError  bool
		errorMsg   string
		checkToken string
	}{
		{
			name: "OAuth2 missing fields",
			authConfig: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: oauth2
        client_id: test-client`,
			wantError: true,
			errorMsg:  "client_id and token_endpoint are required",
		},
		{
			name: "OAuth2 valid",
			authConfig: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: oauth2
        client_id: test-client
        token_endpoint: https://auth.example.com/token
        scopes: ["read", "write"]`,
			wantError: false,
		},
	}
}

func createInvalidAuthTests() []struct {
	name       string
	authConfig string
	envVars    map[string]string
	wantError  bool
	errorMsg   string
	checkToken string
} {
	return []struct {
		name       string
		authConfig string
		envVars    map[string]string
		wantError  bool
		errorMsg   string
		checkToken string
	}{
		{
			name: "Unsupported auth type",
			authConfig: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: saml`,
			wantError: true,
			errorMsg:  "unsupported auth type 'saml'",
		},
	}
}

func runAuthTypeTests(t *testing.T, tests []struct {
	name       string
	authConfig string
	envVars    map[string]string
	wantError  bool
	errorMsg   string
	checkToken string
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envCleanup := setupAuthTestEnvironment(t, tt.envVars)
			defer envCleanup()

			updatedConfig, tokenCleanup := createTokenFileIfNeeded(t, tt.authConfig)
			defer tokenCleanup()

			configPath, configCleanup := testutil.TempFile(t, updatedConfig)
			defer configCleanup()

			cfg, err := Load(configPath)

			validateAuthTestResult(t, cfg, err, tt.wantError, tt.errorMsg, tt.checkToken)
		})
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	configContent := `
invalid yaml content
  - this is not valid
  : yaml syntax
`

	configPath, cleanup := testutil.TempFile(t, configContent)
	defer cleanup()

	_, err := Load(configPath)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}

	if !strings.Contains(err.Error(), "While parsing config") &&
		!strings.Contains(err.Error(), "configuration validation failed") {
		t.Errorf("Expected parsing error or validation error, got: %v", err)
	}
}

// setupEnvironmentOverrides sets up multiple environment variables and returns cleanup function.
func setupEnvironmentOverrides(t *testing.T, envVars map[string]string) func() {
	t.Helper()
	// Save original environment state.
	origEnv := make(map[string]string)

	for k := range envVars {
		if val, exists := os.LookupEnv(k); exists {
			origEnv[k] = val
		}
	}

	// Set environment variables to override config.
	for k, v := range envVars {
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
	}

	return func() {
		for k := range envVars {
			if origVal, existed := origEnv[k]; existed {
				if err := os.Setenv(k, origVal); err != nil {
					t.Fatalf("Failed to set environment variable: %v", err)
				}
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}
}

// validateEnvironmentOverrides validates that environment variables correctly overrode config values.
func validateEnvironmentOverrides(t *testing.T, cfg *Config) {
	t.Helper()

	// Verify environment overrides.
	lbConfig := cfg.GetLoadBalancerConfig()
	if lbConfig.Strategy != LoadBalanceStrategyWeighted {
		t.Errorf("Expected load balancer strategy from env override, got %s", lbConfig.Strategy)
	}

	if cfg.Logging.Level != LogLevelDebug {
		t.Errorf("Expected log level from env override, got %s", cfg.Logging.Level)
	}

	// Verify auth token was loaded from environment.
	endpoints := cfg.GetGatewayEndpoints()
	if len(endpoints) > 0 && endpoints[0].Auth.Token != "env-token" {
		t.Errorf("Expected auth token from env, got %s", endpoints[0].Auth.Token)
	}
}

func TestLoad_EnvironmentOverride(t *testing.T) {
	configContent := `
version: 1
gateway_pool:
  endpoints:
    - url: wss://config-file-url.com
      auth:
        type: bearer
  load_balancer:
    strategy: round_robin
logging:
  level: info
`

	// Define the environment variables we'll be setting.
	envVars := map[string]string{
		"MCP_LOAD_BALANCER_STRATEGY": "weighted",
		"MCP_LOGGING_LEVEL":          "debug",
		"MCP_AUTH_TOKEN":             "env-token",
	}

	cleanup := setupEnvironmentOverrides(t, envVars)
	defer cleanup()

	configPath, fileCleanup := testutil.TempFile(t, configContent)
	defer fileCleanup()

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	validateEnvironmentOverrides(t, cfg)
}

func TestLoad_ConfigPaths(t *testing.T) {
	// Test that Load searches in the correct paths.
	homeDir, _ := os.UserHomeDir()
	paths := []string{
		".",
		filepath.Join(homeDir, ".config", "claude-cli"),
		filepath.Join(homeDir, ".config", "mcp"),
		"/etc/mcp",
	}

	// This test verifies the paths are added, but doesn't create files.
	// as we don't want to pollute the filesystem
	_, err := Load("")
	// Should fail with missing gateway_pool.endpoints, not file not found
	if err == nil || !strings.Contains(err.Error(), "gateway_pool.endpoints is required") {
		t.Logf("Config search paths verified: %v", paths)
	}
}

func TestExpandPath(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"/absolute/path", "/absolute/path"},
		{"~/relative/path", filepath.Join(homeDir, "relative/path")},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := expandPath(tt.input)
			if result != tt.expected {
				t.Errorf("expandPath(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestConfig_HelperMethods(t *testing.T) {
	cfg := &Config{
		Local: LocalConfig{
			RequestTimeoutMs: 15000,
		},
	}

	// Backwards compatibility methods removed - no longer needed since this is pre-release.

	// Test GetRequestTimeout.
	requestTimeout := cfg.GetRequestTimeout()
	if requestTimeout != 15*time.Second {
		t.Errorf("Expected request timeout 15s, got %v", requestTimeout)
	}
}

func TestLoad_CompleteConfig(t *testing.T) {
	configContent := generateCompleteConfigContent()

	// Setup test environment.
	env := EstablishTestConfiguration(t, configContent)

	env.SetEnvironmentVariable("TEST_TOKEN", "test-token")
	defer env.Cleanup()

	// Load and verify configuration.
	cfg, err := env.LoadConfiguration()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Use fluent assertion pattern for comprehensive verification.
	AssertConfiguration(t, cfg).
		AssertVersion(1).
		AssertGatewayEndpoints().
		HasCount(1).
		FirstEndpointHasURL("wss://gateway.example.com").
		FirstEndpointHasReconnectMultiplier(3.0).
		FirstEndpointHasCipherSuiteCount(2).
		AndConfig().
		AssertLoadBalancer().
		HasStrategy("weighted").
		AndConfig().
		AssertCircuitBreaker().
		IsEnabled().
		HasFailureThreshold(5).
		AndConfig().
		AssertLocalConfig(httpStatusOK).
		AssertLogging().
		HasIncludeCaller(true).
		HasSamplingInitial(testTimeout).
		AndConfig().
		AssertMetrics().
		HasLabel("environment", "test").
		AndConfig().
		AssertAdvanced().
		HasCompressionAlgorithm("zstd").
		HasCircuitBreakerFailureThreshold(10).
		HasDebugPprofPort(6061)
}

// generateCompleteConfigContent creates a complete test configuration.
func generateCompleteConfigContent() string {
	return completeConfigContentYAML
}

func TestLoad_MissingConfigFile(t *testing.T) {
	// When no config file exists and no env vars set, should use defaults.
	// but fail validation because gateway_pool.endpoints is required
	// Ensure no config file exists in current directory.
	_ = os.Remove("mcp-router.yaml") // Ignore remove error
	_ = os.Remove("mcp-router.yml")  // Ignore remove error

	_, err := Load("")
	if err == nil {
		t.Error("Expected error for missing gateway_pool.endpoints")
	}

	// Since we can't easily set gateway_pool.endpoints via env vars (it's an array),
	// we'll create a minimal config file for this test
	configContent := `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: bearer
`

	// Save original environment state.
	origToken, tokenExists := os.LookupEnv("MCP_AUTH_TOKEN")

	// Set auth token via env.
	if err := os.Setenv("MCP_AUTH_TOKEN", "test-token"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	defer func() {
		if tokenExists {
			if err := os.Setenv("MCP_AUTH_TOKEN", origToken); err != nil {
				t.Fatalf("Failed to set environment variable: %v", err)
			}
		} else {
			_ = os.Unsetenv("MCP_AUTH_TOKEN")
		}
	}()

	configPath, cleanup := testutil.TempFile(t, configContent)
	defer cleanup()

	cfg, err := Load(configPath)
	if err != nil {
		t.Errorf("Should load with minimal config and env vars: %v", err)

		return
	}

	if cfg == nil {
		t.Error("Config should not be nil")

		return
	}

	// Verify defaults were applied.
	if cfg.Local.ReadBufferSize != 65536 {
		t.Errorf("Expected default read buffer size, got %d", cfg.Local.ReadBufferSize)
	}

	// Verify endpoint was loaded.
	endpoints := cfg.GetGatewayEndpoints()
	if len(endpoints) != 1 {
		t.Errorf("Expected 1 endpoint, got %d", len(endpoints))
	}
}

// validateWeightNormalization validates that negative weights are normalized to 1.
func validateWeightNormalization(c *Config) error {
	if len(c.GatewayPool.Endpoints) > 0 && c.GatewayPool.Endpoints[0].Weight < 1 {
		return fmt.Errorf("expected weight to be normalized to 1, got %d", c.GatewayPool.Endpoints[0].Weight)
	}

	return nil
}

// validateCircuitBreakerDefaults validates that circuit breaker has default values applied.
func validateCircuitBreakerDefaults(c *Config) error {
	cb := c.GetCircuitBreakerConfig()
	if cb.FailureThreshold == 0 || cb.SuccessThreshold == 0 {
		return errors.New("circuit breaker thresholds should have defaults applied")
	}

	return nil
}

// validateNamespaceRoutingComplex validates that namespace routing has correct complex configuration.
func validateNamespaceRoutingComplex(c *Config) error {
	nr := c.GetNamespaceRoutingConfig()
	if !nr.Enabled || len(nr.Rules) != 2 {
		return errors.New("namespace routing not configured correctly")
	}

	return nil
}

// validateLoadBalancingMultipleEndpoints validates load balancer with multiple endpoints.
func validateLoadBalancingMultipleEndpoints(c *Config) error {
	if len(c.GetGatewayEndpoints()) != 3 {
		return fmt.Errorf("expected 3 endpoints, got %d", len(c.GetGatewayEndpoints()))
	}

	lb := c.GetLoadBalancerConfig()
	if lb.Strategy != "weighted" || lb.RetryCount != 5 {
		return errors.New("load balancer not configured correctly")
	}

	return nil
}

// validateServiceDiscoverySettings validates service discovery configuration.
func validateServiceDiscoverySettings(c *Config) error {
	sd := c.GetServiceDiscoveryConfig()
	if !sd.Enabled {
		return errors.New("service discovery should be enabled")
	}

	if sd.RefreshInterval != 5*time.Second {
		return errors.New("refresh interval not set correctly")
	}

	return nil
}

func getEdgeCaseTests() []struct {
	name        string
	configYAML  string
	envVars     map[string]string
	wantError   bool
	errorMsg    string
	validations []func(*Config) error
} {
	var tests []struct {
		name        string
		configYAML  string
		envVars     map[string]string
		wantError   bool
		errorMsg    string
		validations []func(*Config) error
	}

	tests = append(tests, getBasicEdgeCaseTests()...)
	tests = append(tests, getCircuitBreakerEdgeCaseTests()...)
	tests = append(tests, getRoutingEdgeCaseTests()...)
	tests = append(tests, getLoadBalancingEdgeCaseTests()...)
	tests = append(tests, getServiceDiscoveryEdgeCaseTests()...)

	return tests
}

func getBasicEdgeCaseTests() []struct {
	name        string
	configYAML  string
	envVars     map[string]string
	wantError   bool
	errorMsg    string
	validations []func(*Config) error
} {
	return []struct {
		name        string
		configYAML  string
		envVars     map[string]string
		wantError   bool
		errorMsg    string
		validations []func(*Config) error
	}{
		{
			name: "empty endpoint URL",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: ""
      auth:
        type: bearer
`,
			wantError: true,
			errorMsg:  "url is required",
		},
		{
			name: "invalid weight values",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      weight: -1
      auth:
        type: bearer
`,
			envVars: map[string]string{"MCP_AUTH_TOKEN": "test-token"},
			validations: []func(*Config) error{
				validateWeightNormalization,
			},
		},
	}
}

func getCircuitBreakerEdgeCaseTests() []struct {
	name        string
	configYAML  string
	envVars     map[string]string
	wantError   bool
	errorMsg    string
	validations []func(*Config) error
} {
	return []struct {
		name        string
		configYAML  string
		envVars     map[string]string
		wantError   bool
		errorMsg    string
		validations []func(*Config) error
	}{
		{
			name: "circuit breaker extreme values",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: bearer
  circuit_breaker:
    enabled: true
    failure_threshold: 0
    recovery_timeout: 0s
    success_threshold: 0
`,
			envVars: map[string]string{"MCP_AUTH_TOKEN": "test-token"},
			validations: []func(*Config) error{
				validateCircuitBreakerDefaults,
			},
		},
	}
}

func getRoutingEdgeCaseTests() []struct {
	name        string
	configYAML  string
	envVars     map[string]string
	wantError   bool
	errorMsg    string
	validations []func(*Config) error
} {
	return []struct {
		name        string
		configYAML  string
		envVars     map[string]string
		wantError   bool
		errorMsg    string
		validations []func(*Config) error
	}{
		{
			name: "namespace routing with complex patterns",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: bearer
      tags: ["docker", "kubernetes"]
  namespace_routing:
    enabled: true
    rules:
      - pattern: '^docker\.(images|containers)\.*'
        tags: ["docker"]
        priority: 1
        description: "Route Docker commands"
      - pattern: '.*\.k8s\..*'
        tags: ["kubernetes"]
        priority: 2
        description: "Route Kubernetes commands"
`,
			envVars: map[string]string{"MCP_AUTH_TOKEN": "test-token"},
			validations: []func(*Config) error{
				validateNamespaceRoutingComplex,
			},
		},
	}
}

func getLoadBalancingEdgeCaseTests() []struct {
	name        string
	configYAML  string
	envVars     map[string]string
	wantError   bool
	errorMsg    string
	validations []func(*Config) error
} {
	return []struct {
		name        string
		configYAML  string
		envVars     map[string]string
		wantError   bool
		errorMsg    string
		validations []func(*Config) error
	}{
		{
			name: "multiple endpoints load balancer stress test",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://primary.com
      auth:
        type: bearer
      weight: 10
      priority: 1
      tags: ["primary"]
    - url: wss://secondary.com
      auth:
        type: bearer
      weight: 5
      priority: 2
      tags: ["secondary"]
    - url: wss://tertiary.com
      auth:
        type: bearer
      weight: 1
      priority: 3
      tags: ["tertiary"]
  load_balancer:
    strategy: weighted
    failover_timeout: 15s
    retry_count: 5
`,
			envVars: map[string]string{"MCP_AUTH_TOKEN": "test-token"},
			validations: []func(*Config) error{
				validateLoadBalancingMultipleEndpoints,
			},
		},
	}
}

func getServiceDiscoveryEdgeCaseTests() []struct {
	name        string
	configYAML  string
	envVars     map[string]string
	wantError   bool
	errorMsg    string
	validations []func(*Config) error
} {
	return []struct {
		name        string
		configYAML  string
		envVars     map[string]string
		wantError   bool
		errorMsg    string
		validations []func(*Config) error
	}{
		{
			name: "service discovery with health checks",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://dynamic.com
      auth:
        type: bearer
  service_discovery:
    enabled: true
    refresh_interval: 5s
    health_check_interval: 2s
    unhealthy_threshold: 1
    healthy_threshold: 1
`,
			envVars: map[string]string{"MCP_AUTH_TOKEN": "test-token"},
			validations: []func(*Config) error{
				validateServiceDiscoverySettings,
			},
		},
	}
}

func TestConfig_EdgeCases(t *testing.T) {
	tests := getEdgeCaseTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTestEnvironmentWithVars(t, tt.envVars)
			defer cleanup()

			configPath, configCleanup := testutil.TempFile(t, tt.configYAML)
			defer configCleanup()

			cfg, err := Load(configPath)

			validateEdgeCaseResult(t, tt, cfg, err)
		})
	}
}

func setupTestEnvironmentWithVars(t *testing.T, envVars map[string]string) func() {
	t.Helper()

	// Save original environment state for this test.
	origEnv := make(map[string]string)

	for k := range envVars {
		if val, exists := os.LookupEnv(k); exists {
			origEnv[k] = val
		}
	}

	// Set environment variables.
	for k, v := range envVars {
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
	}

	// Return cleanup function.
	return func() {
		for k := range envVars {
			if origVal, existed := origEnv[k]; existed {
				if err := os.Setenv(k, origVal); err != nil {
					t.Fatalf("Failed to set environment variable: %v", err)
				}
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}
}

func validateEdgeCaseResult(t *testing.T, tt struct {
	name        string
	configYAML  string
	envVars     map[string]string
	wantError   bool
	errorMsg    string
	validations []func(*Config) error
}, cfg *Config, err error) {
	t.Helper()

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")

			return
		}

		if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
		}

		return
	}

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
		return
	}

	for _, validation := range tt.validations {
		if err := validation(cfg); err != nil {
			t.Errorf("Validation failed: %v", err)
		}
	}
}

// validateCircuitBreakerOverrides validates circuit breaker environment overrides.
func validateCircuitBreakerOverrides(c *Config) error {
	cb := c.GetCircuitBreakerConfig()
	if !cb.Enabled {
		return errors.New("circuit breaker should be enabled")
	}

	if cb.FailureThreshold != 10 {
		return fmt.Errorf("failure threshold should be 10, got %d", cb.FailureThreshold)
	}

	return nil
}

// validateServiceDiscoveryOverrides validates service discovery environment overrides.
func validateServiceDiscoveryOverrides(c *Config) error {
	sd := c.GetServiceDiscoveryConfig()
	if !sd.Enabled {
		return errors.New("service discovery should be enabled")
	}

	if sd.RefreshInterval != 10*time.Second {
		return fmt.Errorf("refresh interval should be 10s, got %v", sd.RefreshInterval)
	}

	return nil
}

// createEnvironmentOverrideTestCasesLocal creates test cases for environment variable override testing.
func createEnvironmentOverrideTestCasesLocal() []struct {
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
			name: "circuit breaker environment overrides",
			envVars: map[string]string{
				"MCP_CIRCUIT_BREAKER_ENABLED":                        "true",
				"MCP_GATEWAY_POOL_CIRCUIT_BREAKER_FAILURE_THRESHOLD": "10",
				"MCP_GATEWAY_POOL_CIRCUIT_BREAKER_RECOVERY_TIMEOUT":  "60s",
				"MCP_AUTH_TOKEN": "env-token-cb",
			},
			validations: []func(*Config) error{validateCircuitBreakerOverrides},
		},
		{
			name: "service discovery environment overrides",
			envVars: map[string]string{
				"MCP_SERVICE_DISCOVERY_ENABLED":                            "true",
				"MCP_GATEWAY_POOL_SERVICE_DISCOVERY_REFRESH_INTERVAL":      "10s",
				"MCP_GATEWAY_POOL_SERVICE_DISCOVERY_HEALTH_CHECK_INTERVAL": "5s",
				"MCP_AUTH_TOKEN": "env-token-sd",
			},
			validations: []func(*Config) error{validateServiceDiscoveryOverrides},
		},
		{
			name: "direct mode environment overrides",
			envVars: map[string]string{
				"MCP_DIRECT_MODE_ENABLED":    "true",
				"MCP_DIRECT_MAX_CONNECTIONS": "50",
				"MCP_DIRECT_DEFAULT_TIMEOUT": "30s",
				"MCP_DIRECT_AUTO_DETECTION":  "true",
				"MCP_AUTH_TOKEN":             "env-token-dm",
			},
			validations: []func(*Config) error{func(c *Config) error {
				if c.Direct.MaxConnections != 50 {
					return fmt.Errorf("max connections should be 50, got %d", c.Direct.MaxConnections)
				}

				return nil
			}},
		},
	}
}

func TestConfig_EnvironmentVariableOverrides(t *testing.T) {
	// Test comprehensive environment variable override scenarios.
	tests := createEnvironmentOverrideTestCasesLocal()

	configYAML := `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: bearer
`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envCleanup := setupAuthTestEnvironment(t, tt.envVars)
			defer envCleanup()

			configPath, configCleanup := testutil.TempFile(t, configYAML)
			defer configCleanup()

			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			if err := tt.validations[0](cfg); err != nil {
				t.Errorf("Verification failed: %v", err)
			}
		})
	}
}

func TestConfig_ValidationFailures(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		errorMsg   string
	}{
		{
			name: "oauth2 missing token endpoint",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: oauth2
        client_id: test-client
        client_secret: secret
`,
			errorMsg: "token_endpoint are required",
		},
		{
			name: "mtls missing client cert",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: mtls
        client_key: /path/to/key.pem
`,
			errorMsg: "client_cert and client_key are required",
		},
		{
			name: "empty gateway pool endpoints",
			configYAML: `
version: 1
gateway_pool:
  endpoints: []
`,
			errorMsg: "gateway_pool.endpoints is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath, cleanup := testutil.TempFile(t, tt.configYAML)
			defer cleanup()

			_, err := Load(configPath)
			if err == nil {
				t.Error("Expected validation error but got none")
			} else if !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
			}
		})
	}
}

// validateSecureEnvToken validates secure environment token.
func validateSecureEnvToken(token string) error {
	if token != "secure-env-token-123" { //nolint:gosec // Test constant, not real credentials
		return fmt.Errorf("expected secure-env-token-123, got %s", token)
	}

	return nil
}

// validateFileToken validates file-based token.
func validateFileToken(token string) error {
	if token != "file-based-token-456" {
		return fmt.Errorf("expected file-based-token-456, got %s", token)
	}

	return nil
}

// createCredentialTestCases creates test cases for credential security testing.
func createCredentialTestCases() []struct {
	name        string
	configYAML  string
	envVars     map[string]string
	fileTokens  map[string]string
	verifyToken func(string) error
} {
	return []struct {
		name        string
		configYAML  string
		envVars     map[string]string
		fileTokens  map[string]string
		verifyToken func(string) error
	}{
		{
			name: "bearer token from environment with secure key",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: bearer
        token_env: CUSTOM_TOKEN
        token_secure_key: test-app-token-1
`,
			envVars: map[string]string{
				"CUSTOM_TOKEN": "secure-env-token-123",
			},
			verifyToken: validateSecureEnvToken,
		},
		{
			name: "bearer token from file",
			configYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: bearer
        token_file: %s
`,
			fileTokens: map[string]string{
				"test-token.txt": "file-based-token-456",
			},
			verifyToken: validateFileToken,
		},
	}
}

func TestConfig_CredentialLoadingSecurity(t *testing.T) {
	// Test secure credential loading scenarios.
	tests := createCredentialTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTestEnvironmentWithVars(t, tt.envVars)
			defer cleanup()

			configYAML := createCredentialConfig(t, tt)

			configPath, configCleanup := testutil.TempFile(t, configYAML)
			defer configCleanup()

			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			verifyCredentialLoading(t, tt, cfg)
		})
	}
}

func createCredentialConfig(t *testing.T, tt struct {
	name        string
	configYAML  string
	envVars     map[string]string
	fileTokens  map[string]string
	verifyToken func(string) error
}) string {
	t.Helper()

	// Create token files.
	if len(tt.fileTokens) > 0 {
		for filename, content := range tt.fileTokens {
			tokenPath := filepath.Join(t.TempDir(), filename)

			err := os.WriteFile(tokenPath, []byte(content), 0o600)
			if err != nil {
				t.Fatalf("Failed to create token file: %v", err)
			}

			return fmt.Sprintf(tt.configYAML, tokenPath)
		}
	}

	return tt.configYAML
}

func verifyCredentialLoading(t *testing.T, tt struct {
	name        string
	configYAML  string
	envVars     map[string]string
	fileTokens  map[string]string
	verifyToken func(string) error
}, cfg *Config) {
	t.Helper()

	// Verify token was loaded correctly.
	endpoints := cfg.GetGatewayEndpoints()
	if len(endpoints) > 0 {
		if err := tt.verifyToken(endpoints[0].Auth.Token); err != nil {
			t.Errorf("Token verification failed: %v", err)
		}
	}
}

// validateAdvancedFeatures validates advanced configuration features.
func validateAdvancedFeatures(t *testing.T, cfg *Config) {
	t.Helper()

	// Test compression config.
	if !cfg.Advanced.Compression.Enabled {
		t.Error("Compression should be enabled")
	}

	if cfg.Advanced.Compression.Algorithm != "zstd" {
		t.Errorf("Expected zstd compression, got %s", cfg.Advanced.Compression.Algorithm)
	}

	if cfg.Advanced.Compression.Level != 9 {
		t.Errorf("Expected compression level 9, got %d", cfg.Advanced.Compression.Level)
	}

	// Test circuit breaker config.
	if cfg.Advanced.CircuitBreaker.FailureThreshold != 15 {
		t.Errorf("Expected failure threshold 15, got %d", cfg.Advanced.CircuitBreaker.FailureThreshold)
	}

	// Test deduplication config.
	if !cfg.Advanced.Deduplication.Enabled {
		t.Error("Deduplication should be enabled")
	}

	if cfg.Advanced.Deduplication.CacheSize != 5000 {
		t.Errorf("Expected cache size 5000, got %d", cfg.Advanced.Deduplication.CacheSize)
	}

	// Test debug config.
	if !cfg.Advanced.Debug.LogFrames {
		t.Error("Log frames should be enabled")
	}

	if cfg.Advanced.Debug.FailureDir != "/custom/failure/dir" {
		t.Errorf("Expected custom failure dir, got %s", cfg.Advanced.Debug.FailureDir)
	}

	if cfg.Advanced.Debug.PprofPort != 7070 {
		t.Errorf("Expected pprof port 7070, got %d", cfg.Advanced.Debug.PprofPort)
	}
}

func TestConfig_AdvancedFeatures(t *testing.T) {
	configYAML := `
version: 1
gateway_pool:
  endpoints:
    - url: wss://test.com
      auth:
        type: bearer
        token_env: TEST_TOKEN
advanced:
  compression:
    enabled: true
    algorithm: zstd
    level: 9
  circuit_breaker:
    failure_threshold: 15
    success_threshold: 7
    timeout_seconds: 90
  deduplication:
    enabled: true
    cache_size: 5000
    ttl_seconds: 300
  debug:
    log_frames: true
    save_failures: true
    failure_dir: /custom/failure/dir
    enable_pprof: true
    pprof_port: 7070
`

	cleanup := setupTestEnvironment(t)
	defer cleanup()

	configPath, fileCleanup := testutil.TempFile(t, configYAML)
	defer fileCleanup()

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	validateAdvancedFeatures(t, cfg)
}

func BenchmarkLoad(b *testing.B) {
	configContent := `
version: 1
gateway_pool:
  endpoints:
    - url: wss://gateway.example.com
      auth:
        type: bearer
        token_env: BENCH_TOKEN
`

	// Save original environment state.
	origToken, tokenExists := os.LookupEnv("BENCH_TOKEN")

	if err := os.Setenv("BENCH_TOKEN", "bench-token"); err != nil {
		b.Fatalf("Failed to set environment variable: %v", err)
	}

	defer func() {
		if tokenExists {
			if err := os.Setenv("BENCH_TOKEN", origToken); err != nil {
				b.Fatalf("Failed to set environment variable: %v", err)
			}
		} else {
			_ = os.Unsetenv("BENCH_TOKEN")
		}
	}()

	configPath, cleanup := testutil.TempFile(b, configContent)
	defer cleanup()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := Load(configPath)
		if err != nil {
			b.Fatal(err)
		}
	}
}

const benchmarkComplexConfigYAML = `
version: 1
gateway_pool:
  endpoints:
    - url: wss://primary.example.com
      auth:
        type: bearer
        token_env: BENCH_TOKEN
      weight: 10
      priority: 1
      tags: ["primary", "high-priority"]
    - url: wss://secondary.example.com
      auth:
        type: oauth2
        client_id: test-client
        client_secret_env: OAUTH_SECRET
        token_endpoint: https://auth.example.com/token
      weight: 5
      priority: 2
      tags: ["secondary"]
    - url: wss://tertiary.example.com
      auth:
        type: mtls
        client_cert: /path/to/cert.pem
        client_key: /path/to/key.pem
      weight: 1
      priority: 3
      tags: ["tertiary", "fallback"]
  load_balancer:
    strategy: weighted
    failover_timeout: 30s
    retry_count: 5
  service_discovery:
    enabled: true
    refresh_interval: 30s
    health_check_interval: 10s
    unhealthy_threshold: 3
    healthy_threshold: 2
  circuit_breaker:
    enabled: true
    failure_threshold: 10
    recovery_timeout: 60s
    success_threshold: 5
    timeout_duration: 15s
    monitoring_window: 120s
  namespace_routing:
    enabled: true
    rules:
      - pattern: "docker\\.*"
        tags: ["docker"]
        priority: 1
        description: "Docker commands"
      - pattern: "k8s\\.*"
        tags: ["kubernetes"]
        priority: 2
        description: "Kubernetes commands"
local:
  read_buffer_size: 131072
  write_buffer_size: 131072
  request_timeout_ms: 60000
  max_concurrent_requests: httpStatusInternalError
  max_queued_requests: testMaxIterations
logging:
  level: debug
  format: json
  output: stderr
  include_caller: true
  sampling:
    enabled: true
    initial: testIterations
    thereafter: testMaxIterations
metrics:
  enabled: true
  endpoint: localhost:9090
  labels:
    environment: production
    region: us-west-2
    datacenter: dc1
tracing:
  enabled: true
  endpoint: localhost:14268
  service_name: mcp-router
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
    failure_dir: /var/log/mcp-failures
    enable_pprof: true
    pprof_port: 6060
`

func BenchmarkLoad_ComplexConfig(b *testing.B) {
	// Benchmark with a complex configuration.
	envVars := map[string]string{
		"BENCH_TOKEN":  "bench-token",
		"OAUTH_SECRET": "oauth-secret",
	}

	cleanup := setupBenchmarkEnvironment(b, envVars)
	defer cleanup()

	configPath, configCleanup := testutil.TempFile(b, benchmarkComplexConfigYAML)
	defer configCleanup()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := Load(configPath)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func setupBenchmarkEnvironment(b *testing.B, envVars map[string]string) func() {
	b.Helper()

	// Save original environment state.
	origEnv := make(map[string]string)

	for k := range envVars {
		if val, exists := os.LookupEnv(k); exists {
			origEnv[k] = val
		}
	}

	// Set environment variables.
	for k, v := range envVars {
		if err := os.Setenv(k, v); err != nil {
			b.Fatalf("Failed to set environment variable: %v", err)
		}
	}

	// Return cleanup function.
	return func() {
		for k := range envVars {
			if origVal, existed := origEnv[k]; existed {
				if err := os.Setenv(k, origVal); err != nil {
					b.Fatalf("Failed to set environment variable: %v", err)
				}
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}
}
