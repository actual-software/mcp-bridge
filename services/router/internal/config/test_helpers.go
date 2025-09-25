package config

import (
	"os"
	"testing"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
)

// TestConfigurationEnvironment provides comprehensive test configuration setup.
type TestConfigurationEnvironment struct {
	t           *testing.T
	configPath  string
	cleanup     func()
	envVars     map[string]string
	originalEnv map[string]string
}

// EstablishTestConfiguration creates a test environment for configuration testing.
func EstablishTestConfiguration(t *testing.T, configContent string) *TestConfigurationEnvironment {
	t.Helper()
	// Create temporary file for config.
	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	configPath := tmpFile.Name()
	_ = tmpFile.Close()

	cleanup := func() {
		_ = os.Remove(configPath) // Ignore remove error
	}

	return &TestConfigurationEnvironment{
		t:           t,
		configPath:  configPath,
		cleanup:     cleanup,
		envVars:     make(map[string]string),
		originalEnv: make(map[string]string),
	}
}

// SetEnvironmentVariable sets an environment variable for the test.
func (env *TestConfigurationEnvironment) SetEnvironmentVariable(key, value string) *TestConfigurationEnvironment {
	// Save original value if not already saved.
	if _, exists := env.originalEnv[key]; !exists {
		if orig, exists := os.LookupEnv(key); exists {
			env.originalEnv[key] = orig
		} else {
			env.originalEnv[key] = "" // Marker for non-existent var
		}
	}

	env.envVars[key] = value
	if err := os.Setenv(key, value); err != nil {
		env.t.Fatalf("Failed to set env: %v", err)
	}

	return env
}

// LoadConfiguration loads the configuration from the test file.
func (env *TestConfigurationEnvironment) LoadConfiguration() (*Config, error) {
	return Load(env.configPath)
}

// Cleanup restores environment and removes temporary files.
func (env *TestConfigurationEnvironment) Cleanup() {
	// Restore environment variables.
	for key, original := range env.originalEnv {
		if original == "" {
			_ = os.Unsetenv(key)
		} else {
			if err := os.Setenv(key, original); err != nil {
				env.t.Fatalf("Failed to set env: %v", err)
			}
		}
	}

	// Clean up temp files.
	if env.cleanup != nil {
		env.cleanup()
	}
}

// ConfigurationAssertion provides assertion helpers for configuration testing.
type ConfigurationAssertion struct {
	t   *testing.T
	cfg *Config
}

// AssertConfiguration creates a new assertion helper.
func AssertConfiguration(t *testing.T, cfg *Config) *ConfigurationAssertion {
	t.Helper()

	return &ConfigurationAssertion{
		t:   t,
		cfg: cfg,
	}
}

// AssertVersion verifies the configuration version.
func (a *ConfigurationAssertion) AssertVersion(expected int) *ConfigurationAssertion {
	if a.cfg.Version != expected {
		a.t.Errorf("Expected version %d, got %d", expected, a.cfg.Version)
	}

	return a
}

// AssertGatewayEndpoints verifies gateway endpoint configuration.
func (a *ConfigurationAssertion) AssertGatewayEndpoints() *GatewayEndpointAssertion {
	endpoints := a.cfg.GetGatewayEndpoints()

	return &GatewayEndpointAssertion{
		t:         a.t,
		endpoints: endpoints,
		parent:    a,
	}
}

// GatewayEndpointAssertion provides assertions for gateway endpoints.
type GatewayEndpointAssertion struct {
	t         *testing.T
	endpoints []GatewayEndpoint
	parent    *ConfigurationAssertion
}

// HasCount verifies the number of endpoints.
func (g *GatewayEndpointAssertion) HasCount(expected int) *GatewayEndpointAssertion {
	if len(g.endpoints) != expected {
		g.t.Errorf("Expected %d endpoint(s), got %d", expected, len(g.endpoints))
	}

	return g
}

// FirstEndpointHasURL verifies the first endpoint's URL.
func (g *GatewayEndpointAssertion) FirstEndpointHasURL(expected string) *GatewayEndpointAssertion {
	if len(g.endpoints) > 0 && g.endpoints[0].URL != expected {
		g.t.Errorf("Expected gateway URL %s, got %s", expected, g.endpoints[0].URL)
	}

	return g
}

// FirstEndpointHasReconnectMultiplier verifies reconnect multiplier.
func (g *GatewayEndpointAssertion) FirstEndpointHasReconnectMultiplier(expected float64) *GatewayEndpointAssertion {
	if len(g.endpoints) > 0 && g.endpoints[0].Connection.Reconnect.Multiplier != expected {
		g.t.Errorf("Expected reconnect multiplier %f, got %f",
			expected, g.endpoints[0].Connection.Reconnect.Multiplier)
	}

	return g
}

// FirstEndpointHasCipherSuiteCount verifies cipher suite count.
func (g *GatewayEndpointAssertion) FirstEndpointHasCipherSuiteCount(expected int) *GatewayEndpointAssertion {
	if len(g.endpoints) > 0 && len(g.endpoints[0].TLS.CipherSuites) != expected {
		g.t.Errorf("Expected %d cipher suites, got %d",
			expected, len(g.endpoints[0].TLS.CipherSuites))
	}

	return g
}

// AndConfig returns to the parent configuration assertion.
func (g *GatewayEndpointAssertion) AndConfig() *ConfigurationAssertion {
	return g.parent
}

// AssertLoadBalancer verifies load balancer configuration.
func (a *ConfigurationAssertion) AssertLoadBalancer() *LoadBalancerAssertion {
	return &LoadBalancerAssertion{
		t:      a.t,
		config: a.cfg.GetLoadBalancerConfig(),
		parent: a,
	}
}

// LoadBalancerAssertion provides assertions for load balancer config.
type LoadBalancerAssertion struct {
	t      *testing.T
	config LoadBalancerConfig
	parent *ConfigurationAssertion
}

// HasStrategy verifies the load balancing strategy.
func (l *LoadBalancerAssertion) HasStrategy(expected string) *LoadBalancerAssertion {
	if l.config.Strategy != expected {
		l.t.Errorf("Expected load balancer strategy %s, got %s", expected, l.config.Strategy)
	}

	return l
}

// AndConfig returns to the parent configuration assertion.
func (l *LoadBalancerAssertion) AndConfig() *ConfigurationAssertion {
	return l.parent
}

// AssertCircuitBreaker verifies circuit breaker configuration.
func (a *ConfigurationAssertion) AssertCircuitBreaker() *CircuitBreakerAssertion {
	return &CircuitBreakerAssertion{
		t:      a.t,
		config: a.cfg.GetCircuitBreakerConfig(),
		parent: a,
	}
}

// CircuitBreakerAssertion provides assertions for circuit breaker config.
type CircuitBreakerAssertion struct {
	t      *testing.T
	config CircuitBreakerConfig
	parent *ConfigurationAssertion
}

// IsEnabled verifies circuit breaker is enabled.
func (c *CircuitBreakerAssertion) IsEnabled() *CircuitBreakerAssertion {
	if !c.config.Enabled {
		c.t.Error("Expected circuit breaker to be enabled")
	}

	return c
}

// HasFailureThreshold verifies failure threshold.
func (c *CircuitBreakerAssertion) HasFailureThreshold(expected int) *CircuitBreakerAssertion {
	if c.config.FailureThreshold != expected {
		c.t.Errorf("Expected failure threshold %d, got %d", expected, c.config.FailureThreshold)
	}

	return c
}

// AndConfig returns to the parent configuration assertion.
func (c *CircuitBreakerAssertion) AndConfig() *ConfigurationAssertion {
	return c.parent
}

// AssertLocalConfig verifies local configuration settings.
func (a *ConfigurationAssertion) AssertLocalConfig(maxConcurrent int) *ConfigurationAssertion {
	if a.cfg.Local.MaxConcurrentRequests != maxConcurrent {
		a.t.Errorf("Expected max concurrent requests %d, got %d",
			maxConcurrent, a.cfg.Local.MaxConcurrentRequests)
	}

	return a
}

// AssertLogging verifies logging configuration.
func (a *ConfigurationAssertion) AssertLogging() *LoggingAssertion {
	return &LoggingAssertion{
		t:      a.t,
		config: a.cfg.Logging,
		parent: a,
	}
}

// LoggingAssertion provides assertions for logging config.
type LoggingAssertion struct {
	t      *testing.T
	config common.LoggingConfig
	parent *ConfigurationAssertion
}

// HasIncludeCaller verifies include caller setting.
func (l *LoggingAssertion) HasIncludeCaller(expected bool) *LoggingAssertion {
	if l.config.IncludeCaller != expected {
		l.t.Errorf("Expected include_caller to be %v", expected)
	}

	return l
}

// HasSamplingInitial verifies sampling initial value.
func (l *LoggingAssertion) HasSamplingInitial(expected int) *LoggingAssertion {
	if l.config.Sampling.Initial != expected {
		l.t.Errorf("Expected sampling initial %d, got %d", expected, l.config.Sampling.Initial)
	}

	return l
}

// AndConfig returns to the parent configuration assertion.
func (l *LoggingAssertion) AndConfig() *ConfigurationAssertion {
	return l.parent
}

// AssertMetrics verifies metrics configuration.
func (a *ConfigurationAssertion) AssertMetrics() *MetricsAssertion {
	return &MetricsAssertion{
		t:      a.t,
		config: a.cfg.Metrics,
		parent: a,
	}
}

// MetricsAssertion provides assertions for metrics config.
type MetricsAssertion struct {
	t      *testing.T
	config common.MetricsConfig
	parent *ConfigurationAssertion
}

// HasLabel verifies a specific metrics label.
func (m *MetricsAssertion) HasLabel(key, expected string) *MetricsAssertion {
	if actual := m.config.Labels[key]; actual != expected {
		m.t.Errorf("Expected %s label '%s', got '%s'", key, expected, actual)
	}

	return m
}

// AndConfig returns to the parent configuration assertion.
func (m *MetricsAssertion) AndConfig() *ConfigurationAssertion {
	return m.parent
}

// AssertAdvanced verifies advanced configuration.
func (a *ConfigurationAssertion) AssertAdvanced() *AdvancedAssertion {
	return &AdvancedAssertion{
		t:      a.t,
		config: a.cfg.Advanced,
		parent: a,
	}
}

// AdvancedAssertion provides assertions for advanced config.
type AdvancedAssertion struct {
	t      *testing.T
	config AdvancedConfig
	parent *ConfigurationAssertion
}

// HasCompressionAlgorithm verifies compression algorithm.
func (a *AdvancedAssertion) HasCompressionAlgorithm(expected string) *AdvancedAssertion {
	if a.config.Compression.Algorithm != expected {
		a.t.Errorf("Expected compression algorithm %s, got %s",
			expected, a.config.Compression.Algorithm)
	}

	return a
}

// HasCircuitBreakerFailureThreshold verifies advanced circuit breaker settings.
func (a *AdvancedAssertion) HasCircuitBreakerFailureThreshold(expected int) *AdvancedAssertion {
	if a.config.CircuitBreaker.FailureThreshold != expected {
		a.t.Errorf("Expected circuit breaker failure threshold %d, got %d",
			expected, a.config.CircuitBreaker.FailureThreshold)
	}

	return a
}

// HasDebugPprofPort verifies debug pprof port.
func (a *AdvancedAssertion) HasDebugPprofPort(expected int) *AdvancedAssertion {
	if a.config.Debug.PprofPort != expected {
		a.t.Errorf("Expected pprof port %d, got %d", expected, a.config.Debug.PprofPort)
	}

	return a
}

// AndConfig returns to the parent configuration assertion.
func (a *AdvancedAssertion) AndConfig() *ConfigurationAssertion {
	return a.parent
}
