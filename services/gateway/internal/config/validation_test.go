package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants.
const (
	invalidValue = "invalid"
)

func TestConfig_Validation(t *testing.T) {
	tests := createConfigValidationTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runConfigValidationTest(t, tt)
		})
	}
}

type configValidationTestCase struct {
	name    string
	config  *Config
	wantErr bool
	errMsg  string
}

func createConfigValidationTestCases() []configValidationTestCase {
	return []configValidationTestCase{
		{"valid minimal config", createValidMinimalConfig(), false, ""},
		{"valid full config", createValidFullConfig(), false, ""},
		{"nil config", nil, true, "config cannot be nil"},
		{"invalid version", createConfigWithInvalidVersion(), true, "unsupported config version"},
		{"invalid server config", createConfigWithInvalidServer(), true, "invalid server configuration"},
		{"invalid auth config", createConfigWithInvalidAuth(), true, "invalid auth configuration"},
		{"invalid TLS config", createConfigWithInvalidTLS(), true, "invalid TLS configuration"},
		{"invalid session config", createConfigWithInvalidSession(), true, "invalid session configuration"},
		{"invalid routing config", createConfigWithInvalidRouting(), true, "invalid routing configuration"},
		{"invalid discovery config", createConfigWithInvalidDiscovery(), true, "invalid discovery configuration"},
		{"invalid rate limit config", createConfigWithInvalidRateLimit(), true, "invalid rate limit configuration"},
		{
			"invalid circuit breaker config",
			createConfigWithInvalidCircuitBreaker(),
			true,
			"invalid circuit breaker configuration",
		},
		{"invalid metrics config", createConfigWithInvalidMetrics(), true, "invalid metrics configuration"},
		{"invalid logging config", createConfigWithInvalidLogging(), true, "invalid logging configuration"},
		{"invalid tracing config", createConfigWithInvalidTracing(), true, "invalid tracing configuration"},
	}
}

func runConfigValidationTest(t *testing.T, tt configValidationTestCase) {
	t.Helper()

	err := ValidateConfig(tt.config)

	if tt.wantErr {
		require.Error(t, err)

		if tt.errMsg != "" {
			assert.Contains(t, err.Error(), tt.errMsg)
		}
	} else {
		require.NoError(t, err)
	}
}

func TestServerConfig_Validation(t *testing.T) {
	tests := createServerConfigValidationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServerConfig(&tt.config)
			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func createServerConfigValidationTests() []struct {
	name    string
	config  ServerConfig
	wantErr bool
	errMsg  string
} {
	validTests := createValidServerConfigTests()
	invalidTests := createInvalidServerConfigTests()

	var allTests []struct {
		name    string
		config  ServerConfig
		wantErr bool
		errMsg  string
	}

	allTests = append(allTests, validTests...)
	allTests = append(allTests, invalidTests...)

	return allTests
}

func createValidServerConfigTests() []struct {
	name    string
	config  ServerConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  ServerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid server config",
			config:  createValidServerConfig(),
			wantErr: false,
		},
	}
}

func createInvalidServerConfigTests() []struct {
	name    string
	config  ServerConfig
	wantErr bool
	errMsg  string
} {
	tests := createPortValidationTests()
	tests = append(tests, createHostValidationTests()...)
	tests = append(tests, createTimeoutValidationTests()...)

	return tests
}

func createPortValidationTests() []struct {
	name    string
	config  ServerConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  ServerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid port - negative",
			config: ServerConfig{
				Host: "localhost",
				Port: -1,
			},
			wantErr: true,
			errMsg:  "port must be between 1 and 65535",
		},
		{
			name: "invalid port - too high",
			config: ServerConfig{
				Host: "localhost",
				Port: 70000,
			},
			wantErr: true,
			errMsg:  "port must be between 1 and 65535",
		},
	}
}

func createHostValidationTests() []struct {
	name    string
	config  ServerConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  ServerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid host - empty",
			config: ServerConfig{
				Host: "",
				Port: 8080,
			},
			wantErr: true,
			errMsg:  "host cannot be empty",
		},
		{
			name: "invalid protocol",
			config: ServerConfig{
				Host:     "localhost",
				Port:     8080,
				Protocol: invalidValue,
			},
			wantErr: true,
			errMsg:  "invalid protocol",
		},
	}
}

func createTimeoutValidationTests() []struct {
	name    string
	config  ServerConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  ServerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid timeout - negative",
			config: ServerConfig{
				Host:        "localhost",
				Port:        8080,
				ReadTimeout: -1,
			},
			wantErr: true,
			errMsg:  "timeout cannot be negative",
		},
		{
			name: "invalid max connections - negative",
			config: ServerConfig{
				Host:           "localhost",
				Port:           8080,
				MaxConnections: -1,
			},
			wantErr: true,
			errMsg:  "max connections cannot be negative",
		},
	}
}

func TestTLSConfig_Validation(t *testing.T) {
	tests := createTLSConfigValidationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTLSConfig(&tt.config)
			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func createTLSConfigValidationTests() []struct {
	name    string
	config  TLSConfig
	wantErr bool
	errMsg  string
} {
	validTests := createValidTLSConfigTests()
	invalidTests := createInvalidTLSConfigTests()

	var allTests []struct {
		name    string
		config  TLSConfig
		wantErr bool
		errMsg  string
	}

	allTests = append(allTests, validTests...)
	allTests = append(allTests, invalidTests...)

	return allTests
}

func createValidTLSConfigTests() []struct {
	name    string
	config  TLSConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  TLSConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid TLS config - disabled",
			config: TLSConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid TLS config - enabled with files",
			config: TLSConfig{
				Enabled:  true,
				CertFile: "/path/to/cert.pem",
				KeyFile:  "/path/to/key.pem",
			},
			wantErr: false,
		},
	}
}

func createInvalidTLSConfigTests() []struct {
	name    string
	config  TLSConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  TLSConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid TLS config - enabled without cert",
			config: TLSConfig{
				Enabled: true,
				KeyFile: "/path/to/key.pem",
			},
			wantErr: true,
			errMsg:  "cert file required when TLS is enabled",
		},
		{
			name: "invalid TLS config - enabled without key",
			config: TLSConfig{
				Enabled:  true,
				CertFile: "/path/to/cert.pem",
			},
			wantErr: true,
			errMsg:  "key file required when TLS is enabled",
		},
		{
			name: "invalid client auth mode",
			config: TLSConfig{
				Enabled:    true,
				CertFile:   "/path/to/cert.pem",
				KeyFile:    "/path/to/key.pem",
				ClientAuth: invalidValue,
			},
			wantErr: true,
			errMsg:  "invalid client auth mode",
		},
		{
			name: "invalid TLS version",
			config: TLSConfig{
				Enabled:    true,
				CertFile:   "/path/to/cert.pem",
				KeyFile:    "/path/to/key.pem",
				MinVersion: "TLS1.0",
			},
			wantErr: true,
			errMsg:  "TLS version too old",
		},
	}
}

func TestAuthConfig_Validation(t *testing.T) {
	tests := createAuthConfigValidationTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runAuthConfigValidationTest(t, tt)
		})
	}
}

type authConfigValidationTestCase struct {
	name    string
	config  AuthConfig
	wantErr bool
	errMsg  string
}

func createAuthConfigValidationTestCases() []authConfigValidationTestCase {
	return []authConfigValidationTestCase{
		{
			name:    "valid auth config - none",
			config:  AuthConfig{Provider: "none"},
			wantErr: false,
		},
		{
			name: "valid auth config - JWT",
			config: AuthConfig{
				Provider: "jwt",
				JWT:      JWTConfig{Issuer: "test-issuer", Audience: "test-audience", PublicKeyPath: "/path/to/public.key"},
			},
			wantErr: false,
		},
		{
			name: "valid auth config - OAuth2",
			config: AuthConfig{
				Provider: "oauth2",
				OAuth2: OAuth2Config{
					ClientID:           "test-client",
					TokenEndpoint:      "https://auth.example.com/token",
					IntrospectEndpoint: "https://auth.example.com/introspect",
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid auth provider",
			config:  AuthConfig{Provider: invalidValue},
			wantErr: true, errMsg: "invalid auth provider",
		},
		{
			name: "invalid JWT config - missing issuer",
			config: AuthConfig{
				Provider: "jwt",
				JWT:      JWTConfig{Audience: "test-audience", PublicKeyPath: "/path/to/public.key"},
			},
			wantErr: true, errMsg: "issuer cannot be empty",
		},
		{
			name: "invalid OAuth2 config - missing client ID",
			config: AuthConfig{
				Provider: "oauth2",
				OAuth2: OAuth2Config{
					TokenEndpoint:      "https://auth.example.com/token",
					IntrospectEndpoint: "https://auth.example.com/introspect",
				},
			},
			wantErr: true, errMsg: "OAuth2 client ID required",
		},
		{
			name: "invalid OAuth2 config - malformed URL",
			config: AuthConfig{
				Provider: "oauth2",
				OAuth2:   OAuth2Config{ClientID: "test-client", TokenEndpoint: "://invalid-url-with-missing-scheme"},
			},
			wantErr: true, errMsg: "invalid token endpoint URL",
		},
	}
}

func runAuthConfigValidationTest(t *testing.T, tt authConfigValidationTestCase) {
	t.Helper()

	err := ValidateAuthConfig(&tt.config)

	if tt.wantErr {
		require.Error(t, err)

		if tt.errMsg != "" {
			assert.Contains(t, err.Error(), tt.errMsg)
		}
	} else {
		require.NoError(t, err)
	}
}

func TestServiceDiscoveryConfig_Validation(t *testing.T) {
	tests := createServiceDiscoveryValidationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceDiscoveryConfig(&tt.config)
			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func createServiceDiscoveryValidationTests() []struct {
	name    string
	config  ServiceDiscoveryConfig
	wantErr bool
	errMsg  string
} {
	validTests := createValidServiceDiscoveryTests()
	invalidTests := createInvalidServiceDiscoveryTests()

	var allTests []struct {
		name    string
		config  ServiceDiscoveryConfig
		wantErr bool
		errMsg  string
	}

	allTests = append(allTests, validTests...)
	allTests = append(allTests, invalidTests...)

	return allTests
}

func createValidServiceDiscoveryTests() []struct {
	name    string
	config  ServiceDiscoveryConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  ServiceDiscoveryConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid static discovery",
			config: ServiceDiscoveryConfig{
				Provider: "static",
				Static: StaticDiscoveryConfig{
					Endpoints: map[string][]EndpointConfig{
						"test-service": {
							{URL: "https://example.com", Labels: map[string]interface{}{"env": "test"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid kubernetes discovery",
			config: ServiceDiscoveryConfig{
				Provider: "kubernetes",
				Kubernetes: KubernetesDiscoveryConfig{
					InCluster: true,
				},
			},
			wantErr: false,
		},
	}
}

func createInvalidServiceDiscoveryTests() []struct {
	name    string
	config  ServiceDiscoveryConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  ServiceDiscoveryConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid provider",
			config: ServiceDiscoveryConfig{
				Provider: invalidValue,
			},
			wantErr: true,
			errMsg:  "invalid discovery provider",
		},
		{
			name: "invalid static config - empty endpoints",
			config: ServiceDiscoveryConfig{
				Provider: "static",
				Static: StaticDiscoveryConfig{
					Endpoints: map[string][]EndpointConfig{},
				},
			},
			wantErr: true,
			errMsg:  "static discovery requires at least one endpoint",
		},
		{
			name: "invalid static config - malformed URL",
			config: ServiceDiscoveryConfig{
				Provider: "static",
				Static: StaticDiscoveryConfig{
					Endpoints: map[string][]EndpointConfig{
						"test-service": {
							{URL: "://invalid-url-with-missing-scheme"},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid endpoint URL",
		},
		{
			name: "invalid refresh rate",
			config: ServiceDiscoveryConfig{
				Provider:    "static",
				RefreshRate: -time.Second,
			},
			wantErr: true,
			errMsg:  "refresh rate cannot be negative",
		},
	}
}

func TestCircuitBreakerConfig_Validation(t *testing.T) {
	tests := createCircuitBreakerValidationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCircuitBreakerConfig(&tt.config)
			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func createCircuitBreakerValidationTests() []struct {
	name    string
	config  CircuitBreakerConfig
	wantErr bool
	errMsg  string
} {
	tests := createValidCircuitBreakerTests()
	tests = append(tests, createInvalidCircuitBreakerTests()...)

	return tests
}

func createValidCircuitBreakerTests() []struct {
	name    string
	config  CircuitBreakerConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  CircuitBreakerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid circuit breaker config",
			config: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 5,
				SuccessThreshold: 3,
				TimeoutSeconds:   30,
				MaxRequests:      testIterations,
				IntervalSeconds:  60,
			},
			wantErr: false,
		},
		{
			name: "disabled circuit breaker",
			config: CircuitBreakerConfig{
				Enabled: false,
			},
			wantErr: false,
		},
	}
}

func createInvalidCircuitBreakerTests() []struct {
	name    string
	config  CircuitBreakerConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  CircuitBreakerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid failure threshold - zero",
			config: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 0,
			},
			wantErr: true,
			errMsg:  "failure threshold must be positive",
		},
		{
			name: "invalid success threshold - zero",
			config: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 5,
				SuccessThreshold: 0,
			},
			wantErr: true,
			errMsg:  "success threshold must be positive",
		},
		{
			name: "invalid timeout - negative",
			config: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 5,
				SuccessThreshold: 3,
				TimeoutSeconds:   -1,
			},
			wantErr: true,
			errMsg:  "timeout cannot be negative",
		},
	}
}

func TestRateLimitConfig_Validation(t *testing.T) {
	tests := createRateLimitValidationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRateLimitConfig(&tt.config)
			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func createRateLimitValidationTests() []struct {
	name    string
	config  RateLimitConfig
	wantErr bool
	errMsg  string
} {
	tests := createValidRateLimitTests()
	tests = append(tests, createInvalidRateLimitTests()...)

	return tests
}

func createValidRateLimitTests() []struct {
	name    string
	config  RateLimitConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  RateLimitConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid rate limit config",
			config: RateLimitConfig{
				Enabled:        true,
				Provider:       "memory",
				RequestsPerSec: testIterations,
				Burst:          httpStatusOK,
			},
			wantErr: false,
		},
		{
			name: "disabled rate limit",
			config: RateLimitConfig{
				Enabled: false,
			},
			wantErr: false,
		},
	}
}

func createInvalidRateLimitTests() []struct {
	name    string
	config  RateLimitConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  RateLimitConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid provider",
			config: RateLimitConfig{
				Enabled:  true,
				Provider: invalidValue,
			},
			wantErr: true,
			errMsg:  "invalid rate limit provider",
		},
		{
			name: "invalid requests per second - zero",
			config: RateLimitConfig{
				Enabled:        true,
				Provider:       "memory",
				RequestsPerSec: 0,
			},
			wantErr: true,
			errMsg:  "requests per second must be positive",
		},
		{
			name: "invalid burst - negative",
			config: RateLimitConfig{
				Enabled:        true,
				Provider:       "memory",
				RequestsPerSec: testIterations,
				Burst:          -1,
			},
			wantErr: true,
			errMsg:  "burst cannot be negative",
		},
		{
			name: "burst smaller than requests per second",
			config: RateLimitConfig{
				Enabled:        true,
				Provider:       "memory",
				RequestsPerSec: testIterations,
				Burst:          testTimeout,
			},
			wantErr: true,
			errMsg:  "burst should be at least equal to requests per second",
		},
	}
}

func TestMetricsConfig_Validation(t *testing.T) {
	tests := createMetricsConfigValidationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMetricsConfig(&tt.config)
			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func createMetricsConfigValidationTests() []struct {
	name    string
	config  MetricsConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  MetricsConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid metrics config",
			config: MetricsConfig{
				Enabled:  true,
				Endpoint: "localhost:9090",
				Path:     "/metrics",
			},
			wantErr: false,
		},
		{
			name: "disabled metrics",
			config: MetricsConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "invalid endpoint - empty when enabled",
			config: MetricsConfig{
				Enabled:  true,
				Endpoint: "",
			},
			wantErr: true,
			errMsg:  "endpoint required when metrics enabled",
		},
		{
			name: "invalid path - empty when enabled",
			config: MetricsConfig{
				Enabled:  true,
				Endpoint: "localhost:9090",
				Path:     "",
			},
			wantErr: true,
			errMsg:  "path required when metrics enabled",
		},
		{
			name: "invalid path - doesn't start with slash",
			config: MetricsConfig{
				Enabled:  true,
				Endpoint: "localhost:9090",
				Path:     "metrics",
			},
			wantErr: true,
			errMsg:  "path must start with /",
		},
	}
}

func TestLoggingConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  LoggingConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid logging config",
			config: LoggingConfig{
				Level:  "info",
				Format: "json",
			},
			wantErr: false,
		},
		{
			name: "invalid log level",
			config: LoggingConfig{
				Level:  invalidValue,
				Format: "json",
			},
			wantErr: true,
			errMsg:  "invalid log level",
		},
		{
			name: "invalid log format",
			config: LoggingConfig{
				Level:  "info",
				Format: invalidValue,
			},
			wantErr: true,
			errMsg:  "invalid log format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLoggingConfig(&tt.config)

			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTracingConfig_Validation(t *testing.T) {
	tests := createTracingConfigValidationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTracingConfig(&tt.config)
			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func createTracingConfigValidationTests() []struct {
	name    string
	config  TracingConfig
	wantErr bool
	errMsg  string
} {
	var tests []struct {
		name    string
		config  TracingConfig
		wantErr bool
		errMsg  string
	}

	tests = append(tests, createValidTracingTests()...)
	tests = append(tests, createInvalidTracingTests()...)

	return tests
}

func createValidTracingTests() []struct {
	name    string
	config  TracingConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  TracingConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid tracing config",
			config: TracingConfig{
				Enabled:      true,
				ServiceName:  "mcp-gateway",
				ExporterType: "otlp",
				OTLPEndpoint: "http://jaeger:14268/api/traces",
			},
			wantErr: false,
		},
		{
			name: "disabled tracing",
			config: TracingConfig{
				Enabled: false,
			},
			wantErr: false,
		},
	}
}

func createInvalidTracingTests() []struct {
	name    string
	config  TracingConfig
	wantErr bool
	errMsg  string
} {
	var tests []struct {
		name    string
		config  TracingConfig
		wantErr bool
		errMsg  string
	}

	tests = append(tests, createInvalidTracingConfigTests()...)
	tests = append(tests, createInvalidTracingParameterTests()...)

	return tests
}

func createInvalidTracingConfigTests() []struct {
	name    string
	config  TracingConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  TracingConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid service name - empty when enabled",
			config: TracingConfig{
				Enabled:     true,
				ServiceName: "",
			},
			wantErr: true,
			errMsg:  "service name required when tracing enabled",
		},
		{
			name: "invalid exporter type",
			config: TracingConfig{
				Enabled:      true,
				ServiceName:  "mcp-gateway",
				ExporterType: invalidValue,
			},
			wantErr: true,
			errMsg:  "invalid exporter type",
		},
		{
			name: "invalid OTLP endpoint",
			config: TracingConfig{
				Enabled:      true,
				ServiceName:  "mcp-gateway",
				ExporterType: "otlp",
				OTLPEndpoint: "not-a-url",
			},
			wantErr: true,
			errMsg:  "OTLP endpoint must be a complete URL with scheme and host",
		},
	}
}

func createInvalidTracingParameterTests() []struct {
	name    string
	config  TracingConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  TracingConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid sampler param - negative",
			config: TracingConfig{
				Enabled:      true,
				ServiceName:  "mcp-gateway",
				SamplerParam: -0.1,
			},
			wantErr: true,
			errMsg:  "sampler param must be between 0 and 1",
		},
		{
			name: "invalid sampler param - too high",
			config: TracingConfig{
				Enabled:      true,
				ServiceName:  "mcp-gateway",
				SamplerParam: 1.1,
			},
			wantErr: true,
			errMsg:  "sampler param must be between 0 and 1",
		},
	}
}

// Helper functions to create test configurations

func createValidMinimalConfig() *Config {
	return &Config{
		Version: 1,
		Server: ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Auth: AuthConfig{
			Provider: "none",
		},
	}
}

func createValidFullConfig() *Config {
	return &Config{
		Version: 1,
		Server:  createFullConfigServerConfig(),
		Auth:    createFullConfigAuthConfig(),
		Sessions: SessionConfig{
			Provider: "memory",
			TTL:      3600,
		},
		Routing: RoutingConfig{
			Strategy: "round_robin",
		},
		Discovery:      createFullConfigDiscoveryConfig(),
		RateLimit:      createFullConfigRateLimitConfig(),
		CircuitBreaker: createFullConfigCircuitBreakerConfig(),
		Metrics:        createFullConfigMetricsConfig(),
		Logging:        createFullConfigLoggingConfig(),
		Tracing:        createFullConfigTracingConfig(),
	}
}

func createFullConfigServerConfig() ServerConfig {
	return ServerConfig{
		Host:                 "0.0.0.0",
		Port:                 8080,
		MaxConnections:       testMaxIterations,
		MaxConnectionsPerIP:  testIterations,
		ConnectionBufferSize: 4096,
		ReadTimeout:          30,
		WriteTimeout:         30,
		IdleTimeout:          60,
		Protocol:             "websocket",
		AllowedOrigins:       []string{"*"},
		TLS: TLSConfig{
			Enabled:    true,
			CertFile:   "/path/to/cert.pem",
			KeyFile:    "/path/to/key.pem",
			ClientAuth: "none",
			MinVersion: "TLS1.2",
		},
	}
}

func createFullConfigAuthConfig() AuthConfig {
	return AuthConfig{
		Provider: "jwt",
		JWT: JWTConfig{
			Issuer:        "mcp-gateway",
			Audience:      "mcp-services",
			PublicKeyPath: "/path/to/public.key",
		},
	}
}

func createFullConfigDiscoveryConfig() ServiceDiscoveryConfig {
	return ServiceDiscoveryConfig{
		Provider: "static",
		Static: StaticDiscoveryConfig{
			Endpoints: map[string][]EndpointConfig{
				"test-service": {
					{URL: "https://example.com"},
				},
			},
		},
	}
}

func createFullConfigRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Enabled:        true,
		Provider:       "memory",
		RequestsPerSec: testIterations,
		Burst:          httpStatusOK,
	}
}

func createFullConfigCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 5,
		SuccessThreshold: 3,
		TimeoutSeconds:   30,
	}
}

func createFullConfigMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled:  true,
		Endpoint: "localhost:9090",
		Path:     "/metrics",
	}
}

func createFullConfigLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:  "info",
		Format: "json",
	}
}

func createFullConfigTracingConfig() TracingConfig {
	return TracingConfig{
		Enabled:     true,
		ServiceName: "mcp-gateway",
	}
}

func createConfigWithInvalidVersion() *Config {
	config := createValidMinimalConfig()
	config.Version = 999

	return config
}

func createConfigWithInvalidServer() *Config {
	config := createValidMinimalConfig()
	config.Server.Port = -1

	return config
}

func createConfigWithInvalidAuth() *Config {
	config := createValidMinimalConfig()
	config.Auth.Provider = invalidValue

	return config
}

func createConfigWithInvalidTLS() *Config {
	config := createValidMinimalConfig()
	config.Server.TLS = TLSConfig{
		Enabled: true,
		// Missing cert and key files
	}

	return config
}

func createConfigWithInvalidSession() *Config {
	config := createValidMinimalConfig()
	config.Sessions.Provider = invalidValue

	return config
}

func createConfigWithInvalidRouting() *Config {
	config := createValidMinimalConfig()
	config.Routing.Strategy = invalidValue

	return config
}

func createConfigWithInvalidDiscovery() *Config {
	config := createValidMinimalConfig()
	config.Discovery.Provider = invalidValue

	return config
}

func createConfigWithInvalidRateLimit() *Config {
	config := createValidMinimalConfig()
	config.RateLimit = RateLimitConfig{
		Enabled:        true,
		Provider:       "memory",
		RequestsPerSec: 0, // Invalid
	}

	return config
}

func createConfigWithInvalidCircuitBreaker() *Config {
	config := createValidMinimalConfig()
	config.CircuitBreaker = CircuitBreakerConfig{
		Enabled:          true,
		FailureThreshold: 0, // Invalid
	}

	return config
}

func createConfigWithInvalidMetrics() *Config {
	config := createValidMinimalConfig()
	config.Metrics = MetricsConfig{
		Enabled: true,
		// Missing endpoint
	}

	return config
}

func createConfigWithInvalidLogging() *Config {
	config := createValidMinimalConfig()
	config.Logging.Level = invalidValue

	return config
}

func createConfigWithInvalidTracing() *Config {
	config := createValidMinimalConfig()
	config.Tracing = TracingConfig{
		Enabled: true,
		// Missing service name
	}

	return config
}

func createValidServerConfig() ServerConfig {
	return ServerConfig{
		Host:                 "localhost",
		Port:                 8080,
		MaxConnections:       testMaxIterations,
		MaxConnectionsPerIP:  testIterations,
		ConnectionBufferSize: 4096,
		ReadTimeout:          30,
		WriteTimeout:         30,
		IdleTimeout:          60,
		Protocol:             "websocket",
	}
}
