package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConnectionConfig_GetTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		timeoutMs int
		expected  time.Duration
	}{
		{
			name:      "zero timeout",
			timeoutMs: 0,
			expected:  0,
		},
		{
			name:      "one second timeout",
			timeoutMs: 1000,
			expected:  time.Second,
		},
		{
			name:      "5 second timeout",
			timeoutMs: 5000,
			expected:  5 * time.Second,
		},
		{
			name:      "100ms timeout",
			timeoutMs: 100,
			expected:  100 * time.Millisecond,
		},
		{
			name:      "negative timeout",
			timeoutMs: -1000,
			expected:  -time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := &ConnectionConfig{
				TimeoutMs:           tt.timeoutMs,
				KeepaliveIntervalMs: 0, // Not used in this test
				MaxConnections:      0, // Not used in this test
				MaxConnectionsPerIP: 0, // Not used in this test
				BufferSize:          0, // Not used in this test
				Reconnect: ReconnectConfig{
					InitialDelayMs: 0, // Not used in this test
					MaxDelayMs:     0, // Not used in this test
					Multiplier:     0, // Not used in this test
					MaxAttempts:    0, // Not used in this test
					Jitter:         0, // Not used in this test
				}, // Not used in this test
			}
			result := config.GetTimeout()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConnectionConfig_GetKeepaliveInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		keepaliveIntervalMs int
		expected            time.Duration
	}{
		{
			name:                "zero keepalive",
			keepaliveIntervalMs: 0,
			expected:            0,
		},
		{
			name:                "30 second keepalive",
			keepaliveIntervalMs: 30000,
			expected:            30 * time.Second,
		},
		{
			name:                "5 second keepalive",
			keepaliveIntervalMs: 5000,
			expected:            5 * time.Second,
		},
		{
			name:                "500ms keepalive",
			keepaliveIntervalMs: 500,
			expected:            500 * time.Millisecond,
		},
		{
			name:                "negative keepalive",
			keepaliveIntervalMs: -5000,
			expected:            -5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := &ConnectionConfig{
				TimeoutMs:           0, // Not used in this test
				KeepaliveIntervalMs: tt.keepaliveIntervalMs,
				MaxConnections:      0, // Not used in this test
				MaxConnectionsPerIP: 0, // Not used in this test
				BufferSize:          0, // Not used in this test
				Reconnect: ReconnectConfig{
					InitialDelayMs: 0, // Not used in this test
					MaxDelayMs:     0, // Not used in this test
					Multiplier:     0, // Not used in this test
					MaxAttempts:    0, // Not used in this test
					Jitter:         0, // Not used in this test
				}, // Not used in this test
			}
			result := config.GetKeepaliveInterval()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCircuitBreakerConfig_GetTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		timeoutSeconds int
		expected       time.Duration
	}{
		{
			name:           "zero timeout",
			timeoutSeconds: 0,
			expected:       0,
		},
		{
			name:           "5 second timeout",
			timeoutSeconds: 5,
			expected:       5 * time.Second,
		},
		{
			name:           "30 second timeout",
			timeoutSeconds: 30,
			expected:       30 * time.Second,
		},
		{
			name:           "1 second timeout",
			timeoutSeconds: 1,
			expected:       time.Second,
		},
		{
			name:           "negative timeout",
			timeoutSeconds: -10,
			expected:       -10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := &CircuitBreakerConfig{
				FailureThreshold: 0, // Not used in this test
				SuccessThreshold: 0, // Not used in this test
				TimeoutSeconds:   tt.timeoutSeconds,
				MaxRequests:      0, // Not used in this test
				IntervalSeconds:  0, // Not used in this test
				SuccessRatio:     0, // Not used in this test
			}
			result := config.GetTimeout()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCircuitBreakerConfig_GetInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		intervalSeconds int
		expected        time.Duration
	}{
		{
			name:            "zero interval",
			intervalSeconds: 0,
			expected:        0,
		},
		{
			name:            "10 second interval",
			intervalSeconds: 10,
			expected:        10 * time.Second,
		},
		{
			name:            "60 second interval",
			intervalSeconds: 60,
			expected:        time.Minute,
		},
		{
			name:            "1 second interval",
			intervalSeconds: 1,
			expected:        time.Second,
		},
		{
			name:            "negative interval",
			intervalSeconds: -5,
			expected:        -5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := &CircuitBreakerConfig{
				FailureThreshold: 0, // Not used in this test
				SuccessThreshold: 0, // Not used in this test
				TimeoutSeconds:   0, // Not used in this test
				MaxRequests:      0, // Not used in this test
				IntervalSeconds:  tt.intervalSeconds,
				SuccessRatio:     0, // Not used in this test
			}
			result := config.GetInterval()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTLSConfig_Structure(t *testing.T) {
	t.Parallel()

	config := TLSConfig{
		Enabled:      true,
		Verify:       true,
		CertFile:     "/path/to/cert.pem",
		KeyFile:      "/path/to/key.pem",
		CAFile:       "/path/to/ca.pem",
		ClientCert:   "/path/to/client.pem",
		ClientKey:    "/path/to/client-key.pem",
		ClientAuth:   "require",
		MinVersion:   "1.2",
		CipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"},
		CACertPath:   "/path/to/ca-cert.pem",
	}

	assert.True(t, config.Enabled)
	assert.True(t, config.Verify)
	assert.Equal(t, "/path/to/cert.pem", config.CertFile)
	assert.Equal(t, "/path/to/key.pem", config.KeyFile)
	assert.Equal(t, "/path/to/ca.pem", config.CAFile)
	assert.Equal(t, "/path/to/client.pem", config.ClientCert)
	assert.Equal(t, "/path/to/client-key.pem", config.ClientKey)
	assert.Equal(t, "require", config.ClientAuth)
	assert.Equal(t, "1.2", config.MinVersion)
	assert.Len(t, config.CipherSuites, 1)
	assert.Equal(t, "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384", config.CipherSuites[0])
	assert.Equal(t, "/path/to/ca-cert.pem", config.CACertPath)
}

func TestAuthConfig_Structure(t *testing.T) {
	t.Parallel()

	config := AuthConfig{
		Type:            "oauth2",
		Token:           "", // Not loaded from config file
		TokenEnv:        "AUTH_TOKEN",
		TokenFile:       "/path/to/token",
		TokenSecureKey:  "token_key",
		ClientID:        "client123",
		ClientSecret:    "", // Not loaded from config file
		ClientSecretEnv: "CLIENT_SECRET",
		ClientSecretKey: "client_secret_key",
		TokenEndpoint:   "https://auth.example.com/token",
		Scopes:          []string{"read", "write"},
		Scope:           "read write",
		GrantType:       "client_credentials",
		Username:        "user123",
		Password:        "", // Not loaded from config file
		PasswordEnv:     "PASSWORD",
		PasswordKey:     "password_key",
		ClientCert:      "/path/to/client.pem",
		ClientKey:       "/path/to/client-key.pem",
		PerMessageAuth:  true,
		PerMessageCache: 300,
	}

	assert.Equal(t, "oauth2", config.Type)
	assert.Empty(t, config.Token) // Should be empty for config file test
	assert.Equal(t, "AUTH_TOKEN", config.TokenEnv)
	assert.Equal(t, "/path/to/token", config.TokenFile)
	assert.Equal(t, "token_key", config.TokenSecureKey)
	assert.Equal(t, "client123", config.ClientID)
	assert.Empty(t, config.ClientSecret) // Should be empty for config file test
	assert.Equal(t, "CLIENT_SECRET", config.ClientSecretEnv)
	assert.Equal(t, "client_secret_key", config.ClientSecretKey)
	assert.Equal(t, "https://auth.example.com/token", config.TokenEndpoint)
	assert.Equal(t, []string{"read", "write"}, config.Scopes)
	assert.Equal(t, "read write", config.Scope)
	assert.Equal(t, "client_credentials", config.GrantType)
	assert.Equal(t, "user123", config.Username)
	assert.Empty(t, config.Password) // Should be empty for config file test
	assert.Equal(t, "PASSWORD", config.PasswordEnv)
	assert.Equal(t, "password_key", config.PasswordKey)
	assert.Equal(t, "/path/to/client.pem", config.ClientCert)
	assert.Equal(t, "/path/to/client-key.pem", config.ClientKey)
	assert.True(t, config.PerMessageAuth)
	assert.Equal(t, 300, config.PerMessageCache)
}

func TestConnectionConfig_Structure(t *testing.T) {
	t.Parallel()

	reconnectConfig := ReconnectConfig{
		InitialDelayMs: 1000,
		MaxDelayMs:     30000,
		Multiplier:     2.0,
		MaxAttempts:    5,
		Jitter:         0.1,
	}

	config := ConnectionConfig{
		TimeoutMs:           5000,
		KeepaliveIntervalMs: 30000,
		MaxConnections:      100,
		MaxConnectionsPerIP: 10,
		BufferSize:          8192,
		Reconnect:           reconnectConfig,
	}

	assert.Equal(t, 5000, config.TimeoutMs)
	assert.Equal(t, 30000, config.KeepaliveIntervalMs)
	assert.Equal(t, 100, config.MaxConnections)
	assert.Equal(t, 10, config.MaxConnectionsPerIP)
	assert.Equal(t, 8192, config.BufferSize)
	assert.Equal(t, reconnectConfig, config.Reconnect)
}

func TestReconnectConfig_Structure(t *testing.T) {
	t.Parallel()

	config := ReconnectConfig{
		InitialDelayMs: 500,
		MaxDelayMs:     60000,
		Multiplier:     1.5,
		MaxAttempts:    10,
		Jitter:         0.2,
	}

	assert.Equal(t, 500, config.InitialDelayMs)
	assert.Equal(t, 60000, config.MaxDelayMs)
	assert.InDelta(t, 1.5, config.Multiplier, 0.001)
	assert.Equal(t, 10, config.MaxAttempts)
	assert.InDelta(t, 0.2, config.Jitter, 0.001)
}

func TestRateLimitConfig_Structure(t *testing.T) {
	t.Parallel()

	config := RateLimitConfig{
		Enabled:        true,
		Provider:       "redis",
		RequestsPerSec: 100.5,
		Burst:          50,
		WindowSize:     60,
	}

	assert.True(t, config.Enabled)
	assert.Equal(t, "redis", config.Provider)
	assert.InDelta(t, 100.5, config.RequestsPerSec, 0.001)
	assert.Equal(t, 50, config.Burst)
	assert.Equal(t, 60, config.WindowSize)
}

func TestCircuitBreakerConfig_Structure(t *testing.T) {
	t.Parallel()

	config := CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		TimeoutSeconds:   30,
		MaxRequests:      100,
		IntervalSeconds:  60,
		SuccessRatio:     0.8,
	}

	assert.Equal(t, 5, config.FailureThreshold)
	assert.Equal(t, 3, config.SuccessThreshold)
	assert.Equal(t, 30, config.TimeoutSeconds)
	assert.Equal(t, 100, config.MaxRequests)
	assert.Equal(t, 60, config.IntervalSeconds)
	assert.InDelta(t, 0.8, config.SuccessRatio, 0.001)
}

func TestMetricsConfig_Structure(t *testing.T) {
	t.Parallel()

	config := MetricsConfig{
		Enabled:  true,
		Endpoint: "localhost:9090",
		Path:     "/metrics",
		Labels: map[string]string{
			"service": "mcp-gateway",
			"env":     "prod",
		},
	}

	assert.True(t, config.Enabled)
	assert.Equal(t, "localhost:9090", config.Endpoint)
	assert.Equal(t, "/metrics", config.Path)
	assert.Len(t, config.Labels, 2)
	assert.Equal(t, "mcp-gateway", config.Labels["service"])
	assert.Equal(t, "prod", config.Labels["env"])
}

func TestLoggingConfig_Structure(t *testing.T) {
	t.Parallel()

	samplingConfig := SamplingConfig{
		Enabled:    true,
		Initial:    100,
		Thereafter: 10,
	}

	config := LoggingConfig{
		Level:         "info",
		Format:        "json",
		Output:        "/var/log/app.log",
		IncludeCaller: true,
		Sampling:      samplingConfig,
	}

	assert.Equal(t, "info", config.Level)
	assert.Equal(t, "json", config.Format)
	assert.Equal(t, "/var/log/app.log", config.Output)
	assert.True(t, config.IncludeCaller)
	assert.Equal(t, samplingConfig, config.Sampling)
}

func TestSamplingConfig_Structure(t *testing.T) {
	t.Parallel()

	config := SamplingConfig{
		Enabled:    false,
		Initial:    50,
		Thereafter: 5,
	}

	assert.False(t, config.Enabled)
	assert.Equal(t, 50, config.Initial)
	assert.Equal(t, 5, config.Thereafter)
}

func BenchmarkConnectionConfig_GetTimeout(b *testing.B) {
	config := &ConnectionConfig{
		TimeoutMs:           5000,
		KeepaliveIntervalMs: 0, // Not used in this benchmark
		MaxConnections:      0, // Not used in this benchmark
		MaxConnectionsPerIP: 0, // Not used in this benchmark
		BufferSize:          0, // Not used in this benchmark
		Reconnect: ReconnectConfig{
			InitialDelayMs: 0, // Not used in this benchmark
			MaxDelayMs:     0, // Not used in this benchmark
			Multiplier:     0, // Not used in this benchmark
			MaxAttempts:    0, // Not used in this benchmark
			Jitter:         0, // Not used in this benchmark
		}, // Not used in this benchmark
	}
	for range b.N {
		config.GetTimeout()
	}
}

func BenchmarkConnectionConfig_GetKeepaliveInterval(b *testing.B) {
	config := &ConnectionConfig{
		TimeoutMs:           0, // Not used in this benchmark
		KeepaliveIntervalMs: 30000,
		MaxConnections:      0, // Not used in this benchmark
		MaxConnectionsPerIP: 0, // Not used in this benchmark
		BufferSize:          0, // Not used in this benchmark
		Reconnect: ReconnectConfig{
			InitialDelayMs: 0, // Not used in this benchmark
			MaxDelayMs:     0, // Not used in this benchmark
			Multiplier:     0, // Not used in this benchmark
			MaxAttempts:    0, // Not used in this benchmark
			Jitter:         0, // Not used in this benchmark
		}, // Not used in this benchmark
	}
	for range b.N {
		config.GetKeepaliveInterval()
	}
}

func BenchmarkCircuitBreakerConfig_GetTimeout(b *testing.B) {
	config := &CircuitBreakerConfig{
		FailureThreshold: 0, // Not used in this benchmark
		SuccessThreshold: 0, // Not used in this benchmark
		TimeoutSeconds:   30,
		MaxRequests:      0, // Not used in this benchmark
		IntervalSeconds:  0, // Not used in this benchmark
		SuccessRatio:     0, // Not used in this benchmark
	}
	for range b.N {
		config.GetTimeout()
	}
}

func BenchmarkCircuitBreakerConfig_GetInterval(b *testing.B) {
	config := &CircuitBreakerConfig{
		FailureThreshold: 0, // Not used in this benchmark
		SuccessThreshold: 0, // Not used in this benchmark
		TimeoutSeconds:   0, // Not used in this benchmark
		MaxRequests:      0, // Not used in this benchmark
		IntervalSeconds:  60,
		SuccessRatio:     0, // Not used in this benchmark
	}
	for range b.N {
		config.GetInterval()
	}
}
