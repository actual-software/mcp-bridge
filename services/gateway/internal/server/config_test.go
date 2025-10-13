package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
)

const (
	testIterations          = 100
	testMaxIterations       = 1000
	testTimeout             = 50
	httpStatusInternalError = 500
)

func TestLoadConfig(t *testing.T) {
	// Setup test configuration
	configPath := setupTestConfig(t)
	setupEnvVars(t)

	// Load config
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Validate all sections
	validateBasicConfig(t, cfg)
	validateServerConfig(t, cfg.Server)
	validateAuthConfig(t, cfg.Auth)
	validateRoutingConfig(t, cfg.Routing)
	validateDiscoveryConfig(t, cfg.Discovery)
	validateRedisConfig(t, cfg.RateLimit.Redis)
	validateLoggingConfig(t, cfg.Logging)
	validateMetricsConfig(t, cfg.Metrics)
}

func setupTestConfig(t *testing.T) string {
	t.Helper()

	configContent := `
version: 1
server:
  port: 8443
  metrics_port: 9090
  max_connections: 100
  connection_buffer_size: 65536
auth:
  provider: jwt
  jwt:
    issuer: test-issuer
    audience: test-audience
    public_key_path: /path/to/public.key
routing:
  strategy: round_robin
  health_check_interval: 30s
  circuit_breaker:
    failure_threshold: 5
    success_threshold: 2
    timeout: 30
service_discovery:
  mode: kubernetes
  namespace_selector:
    - mcp-tools
    - default
  label_selector:
    app: mcp-tool
redis:
  url: localhost:6379
  db: 0
logging:
  level: debug
  format: json
metrics:
  enabled: true
  path: /metrics
`
	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	return configPath
}

func setupEnvVars(t *testing.T) {
	t.Helper()
	require.NoError(t, os.Setenv("MCP_GATEWAY_REDIS_URL", "redis://env-redis:6379"))
	require.NoError(t, os.Setenv("MCP_GATEWAY_REDIS_PASSWORD", "env-password"))

	t.Cleanup(func() {
		_ = os.Unsetenv("MCP_GATEWAY_REDIS_URL")
		_ = os.Unsetenv("MCP_GATEWAY_REDIS_PASSWORD")
	})
}

func validateBasicConfig(t *testing.T, cfg *config.Config) {
	t.Helper()

	if cfg.Version != 1 {
		t.Errorf("Expected version 1, got %d", cfg.Version)
	}
}

func validateServerConfig(t *testing.T, srv config.ServerConfig) {
	t.Helper()

	if srv.Port != 8443 {
		t.Errorf("Expected port 8443, got %d", srv.Port)
	}

	if srv.MaxConnections != 100 {
		t.Errorf("Expected max connections 100, got %d", srv.MaxConnections)
	}
}

func validateAuthConfig(t *testing.T, auth config.AuthConfig) {
	t.Helper()

	if auth.Provider != "jwt" {
		t.Errorf("Expected auth provider 'jwt', got %s", auth.Provider)
	}

	if auth.JWT.Issuer != "test-issuer" {
		t.Errorf("Expected issuer 'test-issuer', got %s", auth.JWT.Issuer)
	}
}

func validateRoutingConfig(t *testing.T, routing config.RoutingConfig) {
	t.Helper()

	if routing.Strategy != "round_robin" {
		t.Errorf("Expected routing strategy 'round_robin', got %s", routing.Strategy)
	}

	if routing.CircuitBreaker.FailureThreshold != 5 {
		t.Errorf("Expected failure threshold 5, got %d", routing.CircuitBreaker.FailureThreshold)
	}
}

func validateDiscoveryConfig(t *testing.T, discovery config.ServiceDiscoveryConfig) {
	t.Helper()

	if discovery.Mode != "kubernetes" {
		t.Errorf("Expected service discovery mode 'kubernetes', got %s", discovery.Mode)
	}

	if len(discovery.NamespaceSelector) != 2 {
		t.Errorf("Expected 2 namespace selectors, got %d", len(discovery.NamespaceSelector))
	}
}

func validateRedisConfig(t *testing.T, redis config.RedisConfig) {
	t.Helper()

	if redis.URL != "redis://env-redis:6379" {
		t.Errorf("Expected Redis URL from env, got %s", redis.URL)
	}

	if redis.Password != "env-password" {
		t.Errorf("Expected Redis password from env, got %s", redis.Password)
	}
}

func validateLoggingConfig(t *testing.T, logging config.LoggingConfig) {
	t.Helper()

	if logging.Level != "debug" {
		t.Errorf("Expected log level 'debug', got %s", logging.Level)
	}
}

func validateMetricsConfig(t *testing.T, metrics config.MetricsConfig) {
	t.Helper()

	if !metrics.Enabled {
		t.Error("Expected metrics to be enabled")
	}
}

func TestLoadConfig_InvalidFile(t *testing.T) {
	_, err := LoadConfig("/non/existent/file.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	configContent := `
invalid yaml content
  - this is not valid
  : yaml syntax
`

	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}

	if !strings.Contains(err.Error(), "While parsing config") {
		t.Errorf("Expected parsing error, got: %v", err)
	}
}

func TestSetDefaults(t *testing.T) {
	v := viper.New()
	setDefaults(v)

	// Check server defaults
	if v.GetInt("server.port") != 8443 {
		t.Errorf("Expected default port 8443, got %d", v.GetInt("server.port"))
	}

	if v.GetInt("server.max_connections") != 50000 {
		t.Errorf("Expected default max connections 50000, got %d", v.GetInt("server.max_connections"))
	}

	// Check auth defaults
	if v.GetString("auth.provider") != "jwt" {
		t.Errorf("Expected default auth provider 'jwt', got %s", v.GetString("auth.provider"))
	}

	// Check routing defaults
	if v.GetString("routing.strategy") != "least_connections" {
		t.Errorf("Expected default routing strategy 'least_connections', got %s", v.GetString("routing.strategy"))
	}

	if v.GetInt("routing.circuit_breaker.failure_threshold") != 5 {
		t.Errorf("Expected default failure threshold 5, got %d", v.GetInt("routing.circuit_breaker.failure_threshold"))
	}

	// Check service discovery defaults
	if v.GetString("service_discovery.mode") != "kubernetes" {
		t.Errorf("Expected default service discovery mode 'kubernetes', got %s", v.GetString("service_discovery.mode"))
	}

	// Check logging defaults
	if v.GetString("logging.level") != "info" {
		t.Errorf("Expected default log level 'info', got %s", v.GetString("logging.level"))
	}

	// Check metrics defaults
	if !v.GetBool("metrics.enabled") {
		t.Error("Expected metrics to be enabled by default")
	}
}

func TestValidate(t *testing.T) {
	tests := createValidationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testConfigValidation(t, tt)
		})
	}
}

func createValidationTests() []struct {
	name          string
	config        *config.Config
	wantError     bool
	errorContains string
} {
	var tests []struct {
		name          string
		config        *config.Config
		wantError     bool
		errorContains string
	}

	tests = append(tests, createValidConfigTests()...)
	tests = append(tests, createPortValidationTests()...)
	tests = append(tests, createConnectionValidationTests()...)
	tests = append(tests, createAuthValidationTests()...)
	tests = append(tests, createDiscoveryValidationTests()...)

	return tests
}

func createValidConfigTests() []struct {
	name          string
	config        *config.Config
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		config        *config.Config
		wantError     bool
		errorContains string
	}{
		{
			name: "Valid config",
			config: &config.Config{
				Server: config.ServerConfig{
					Port:           8443,
					MaxConnections: testMaxIterations,
				},
				Auth: config.AuthConfig{
					Provider: "jwt",
				},
				Discovery: config.ServiceDiscoveryConfig{
					Provider:          "kubernetes",
					NamespaceSelector: []string{"default"},
				},
			},
			wantError: false,
		},
	}
}

func createPortValidationTests() []struct {
	name          string
	config        *config.Config
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		config        *config.Config
		wantError     bool
		errorContains string
	}{
		{
			name: "Invalid port - zero",
			config: &config.Config{
				Server: config.ServerConfig{
					Port:           0,
					MaxConnections: testMaxIterations,
				},
				Auth: config.AuthConfig{
					Provider: "jwt",
				},
				Discovery: config.ServiceDiscoveryConfig{
					Mode:              "kubernetes",
					NamespaceSelector: []string{"default"},
				},
			},
			wantError:     true,
			errorContains: "invalid server port",
		},
		{
			name: "Invalid port - too high",
			config: &config.Config{
				Server: config.ServerConfig{
					Port:           70000,
					MaxConnections: testMaxIterations,
				},
				Auth: config.AuthConfig{
					Provider: "jwt",
				},
				Discovery: config.ServiceDiscoveryConfig{
					Mode:              "kubernetes",
					NamespaceSelector: []string{"default"},
				},
			},
			wantError:     true,
			errorContains: "invalid server port",
		},
	}
}

func createConnectionValidationTests() []struct {
	name          string
	config        *config.Config
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		config        *config.Config
		wantError     bool
		errorContains string
	}{
		{
			name: "Invalid max connections",
			config: &config.Config{
				Server: config.ServerConfig{
					Port:           8443,
					MaxConnections: 0,
				},
				Auth: config.AuthConfig{
					Provider: "jwt",
				},
				Discovery: config.ServiceDiscoveryConfig{
					Mode:              "kubernetes",
					NamespaceSelector: []string{"default"},
				},
			},
			wantError:     true,
			errorContains: "max_connections must be positive",
		},
	}
}

func createAuthValidationTests() []struct {
	name          string
	config        *config.Config
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		config        *config.Config
		wantError     bool
		errorContains string
	}{
		{
			name: "Unsupported auth provider",
			config: &config.Config{
				Server: config.ServerConfig{
					Port:           8443,
					MaxConnections: testMaxIterations,
				},
				Auth: config.AuthConfig{
					Provider: "oauth2",
				},
				Discovery: config.ServiceDiscoveryConfig{
					Mode:              "kubernetes",
					NamespaceSelector: []string{"default"},
				},
			},
			wantError:     true,
			errorContains: "unsupported auth provider",
		},
	}
}

func createDiscoveryValidationTests() []struct {
	name          string
	config        *config.Config
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		config        *config.Config
		wantError     bool
		errorContains string
	}{
		{
			name: "Unsupported service discovery mode",
			config: &config.Config{
				Server: config.ServerConfig{
					Port:           8443,
					MaxConnections: testMaxIterations,
				},
				Auth: config.AuthConfig{
					Provider: "jwt",
				},
				Discovery: config.ServiceDiscoveryConfig{
					Provider:          "invalid-provider",
					NamespaceSelector: []string{"default"},
				},
			},
			wantError:     true,
			errorContains: "unsupported service discovery mode",
		},
		{
			name: "Empty namespace selector",
			config: &config.Config{
				Server: config.ServerConfig{
					Port:           8443,
					MaxConnections: testMaxIterations,
				},
				Auth: config.AuthConfig{
					Provider: "jwt",
				},
				Discovery: config.ServiceDiscoveryConfig{
					Provider:          "kubernetes",
					NamespaceSelector: []string{},
				},
			},
			wantError:     true,
			errorContains: "namespace_selector cannot be empty",
		},
	}
}

func testConfigValidation(t *testing.T, tt struct {
	name          string
	config        *config.Config
	wantError     bool
	errorContains string
}) {
	t.Helper()

	err := validate(tt.config)

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")
		} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
		}
	} else {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	}
}

func TestLoadSecrets(t *testing.T) {
	tests := createLoadSecretsTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLoadSecretsScenario(t, tt)
		})
	}
}

func createLoadSecretsTests() []struct {
	name      string
	envVars   map[string]string
	config    *config.Config
	wantRedis string
	wantPass  string
} {
	return []struct {
		name      string
		envVars   map[string]string
		config    *config.Config
		wantRedis string
		wantPass  string
	}{
		{
			name: "Load from environment",
			envVars: map[string]string{
				"MCP_GATEWAY_REDIS_URL":      "redis://test:6379",
				"MCP_GATEWAY_REDIS_PASSWORD": "test-password",
			},
			config: &config.Config{
				RateLimit: config.RateLimitConfig{
					Redis: config.RedisConfig{
						URL:      "original-url",
						Password: "original-password",
					},
				},
			},
			wantRedis: "redis://test:6379",
			wantPass:  "test-password",
		},
		{
			name:    "No environment variables",
			envVars: map[string]string{},
			config: &config.Config{
				RateLimit: config.RateLimitConfig{
					Redis: config.RedisConfig{
						URL:      "original-url",
						Password: "original-password",
					},
				},
			},
			wantRedis: "original-url",
			wantPass:  "original-password",
		},
	}
}

func testLoadSecretsScenario(t *testing.T, tt struct {
	name      string
	envVars   map[string]string
	config    *config.Config
	wantRedis string
	wantPass  string
}) {
	t.Helper()

	originalViper := viper.GetViper()
	defer viper.Reset()

	for k, v := range tt.envVars {
		require.NoError(t, os.Setenv(k, v))
		defer func(key string) { _ = os.Unsetenv(key) }(k)
	}

	v := viper.New()
	v.SetEnvPrefix("MCP_GATEWAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	viper.Set("redis.url", v.GetString("redis.url"))
	viper.Set("redis.password", v.GetString("redis.password"))

	if err := loadSecrets(tt.config); err != nil {
		t.Fatalf("loadSecrets() error = %v", err)
	}

	if tt.config.RateLimit.Redis.URL != tt.wantRedis {
		t.Errorf("RateLimit Redis URL = %s, want %s", tt.config.RateLimit.Redis.URL, tt.wantRedis)
	}

	if tt.config.RateLimit.Redis.Password != tt.wantPass {
		t.Errorf("RateLimit Redis password = %s, want %s", tt.config.RateLimit.Redis.Password, tt.wantPass)
	}

	viper.Set("", originalViper)
}

func BenchmarkLoadConfig(b *testing.B) {
	configContent := `
version: 1
server:
  port: 8443
  max_connections: testMaxIterations
auth:
  provider: jwt
  jwt:
    issuer: test-issuer
    audience: test-audience
service_discovery:
  mode: kubernetes
  namespace_selector:
    - default
`

	tmpDir := b.TempDir()

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		b.Fatalf("Failed to write config file: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := LoadConfig(configPath)
		if err != nil {
			b.Fatal(err)
		}
	}
}
