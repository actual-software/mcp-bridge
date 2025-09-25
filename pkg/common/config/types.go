// Package config provides configuration types and utilities for the MCP bridge.
package config

import "time"

// TLSConfig represents standardized TLS configuration across all components.
type TLSConfig struct {
	Enabled      bool     `mapstructure:"enabled"`
	Verify       bool     `mapstructure:"verify"`        // For client-side verification
	CertFile     string   `mapstructure:"cert_file"`     // Server cert
	KeyFile      string   `mapstructure:"key_file"`      // Server key
	CAFile       string   `mapstructure:"ca_file"`       // CA cert for verification
	ClientCert   string   `mapstructure:"client_cert"`   // Client cert for mTLS
	ClientKey    string   `mapstructure:"client_key"`    // Client key for mTLS
	ClientAuth   string   `mapstructure:"client_auth"`   // Options: "none", "request", "require"
	MinVersion   string   `mapstructure:"min_version"`   // Options: "1.2", "1.3"
	CipherSuites []string `mapstructure:"cipher_suites"` // Optional: specific cipher suites
	CACertPath   string   `mapstructure:"ca_cert_path"`  // CA certificate path
}

// AuthConfig represents standardized authentication configuration.
type AuthConfig struct {
	Type            string   `mapstructure:"type"` // "bearer", "oauth2", "mtls"
	Token           string   `mapstructure:"-"`    // Not from config file
	TokenEnv        string   `mapstructure:"token_env"`
	TokenFile       string   `mapstructure:"token_file"`
	TokenSecureKey  string   `mapstructure:"token_secure_key"`
	ClientID        string   `mapstructure:"client_id"`
	ClientSecret    string   `mapstructure:"-"` // Not from config file
	ClientSecretEnv string   `mapstructure:"client_secret_env"`
	ClientSecretKey string   `mapstructure:"client_secret_secure_key"`
	TokenEndpoint   string   `mapstructure:"token_endpoint"`
	Scopes          []string `mapstructure:"scopes"`
	Scope           string   `mapstructure:"scope"`
	GrantType       string   `mapstructure:"grant_type"`
	Username        string   `mapstructure:"username"`
	Password        string   `mapstructure:"-"` // Not from config file
	PasswordEnv     string   `mapstructure:"password_env"`
	PasswordKey     string   `mapstructure:"password_secure_key"`
	ClientCert      string   `mapstructure:"client_cert"`
	ClientKey       string   `mapstructure:"client_key"`
	PerMessageAuth  bool     `mapstructure:"per_message_auth"`
	PerMessageCache int      `mapstructure:"per_message_cache"` // Cache duration in seconds
}

// ConnectionConfig represents standardized connection configuration.
type ConnectionConfig struct {
	TimeoutMs           int             `mapstructure:"timeout_ms"`
	KeepaliveIntervalMs int             `mapstructure:"keepalive_interval_ms"`
	MaxConnections      int             `mapstructure:"max_connections"`
	MaxConnectionsPerIP int             `mapstructure:"max_connections_per_ip"`
	BufferSize          int             `mapstructure:"buffer_size"`
	Reconnect           ReconnectConfig `mapstructure:"reconnect"`
}

// ReconnectConfig represents standardized reconnect configuration.
type ReconnectConfig struct {
	InitialDelayMs int     `mapstructure:"initial_delay_ms"`
	MaxDelayMs     int     `mapstructure:"max_delay_ms"`
	Multiplier     float64 `mapstructure:"multiplier"`
	MaxAttempts    int     `mapstructure:"max_attempts"`
	Jitter         float64 `mapstructure:"jitter"`
}

// RateLimitConfig represents standardized rate limiting configuration.
type RateLimitConfig struct {
	Enabled        bool    `mapstructure:"enabled"`
	Provider       string  `mapstructure:"provider"` // "memory", "redis"
	RequestsPerSec float64 `mapstructure:"requests_per_sec"`
	Burst          int     `mapstructure:"burst"`
	WindowSize     int     `mapstructure:"window_size"` // Window size in seconds
}

// CircuitBreakerConfig represents standardized circuit breaker configuration.
type CircuitBreakerConfig struct {
	FailureThreshold int     `mapstructure:"failure_threshold"`
	SuccessThreshold int     `mapstructure:"success_threshold"`
	TimeoutSeconds   int     `mapstructure:"timeout_seconds"`
	MaxRequests      int     `mapstructure:"max_requests"`
	IntervalSeconds  int     `mapstructure:"interval_seconds"`
	SuccessRatio     float64 `mapstructure:"success_ratio"`
}

// MetricsConfig represents standardized metrics configuration.
type MetricsConfig struct {
	Enabled  bool              `mapstructure:"enabled"`
	Endpoint string            `mapstructure:"endpoint"` // Host:port for metrics endpoint
	Path     string            `mapstructure:"path"`     // URL path for metrics (default: /metrics)
	Labels   map[string]string `mapstructure:"labels"`   // Additional labels
}

// LoggingConfig represents standardized logging configuration.
type LoggingConfig struct {
	Level         string         `mapstructure:"level"`  // debug, info, warn, error
	Format        string         `mapstructure:"format"` // json, text
	Output        string         `mapstructure:"output"` // stdout, stderr, file path
	IncludeCaller bool           `mapstructure:"include_caller"`
	Sampling      SamplingConfig `mapstructure:"sampling"`
}

// SamplingConfig represents standardized log sampling configuration.
type SamplingConfig struct {
	Enabled    bool `mapstructure:"enabled"`
	Initial    int  `mapstructure:"initial"`
	Thereafter int  `mapstructure:"thereafter"`
}

// GetTimeout returns the timeout duration for the connection.
func (c *ConnectionConfig) GetTimeout() time.Duration {
	return time.Duration(c.TimeoutMs) * time.Millisecond
}

// GetKeepaliveInterval returns the keepalive interval for the connection.
func (c *ConnectionConfig) GetKeepaliveInterval() time.Duration {
	return time.Duration(c.KeepaliveIntervalMs) * time.Millisecond
}

// GetTimeout returns the timeout duration for the circuit breaker.
func (c *CircuitBreakerConfig) GetTimeout() time.Duration {
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// GetInterval returns the reset interval for the circuit breaker.
func (c *CircuitBreakerConfig) GetInterval() time.Duration {
	return time.Duration(c.IntervalSeconds) * time.Second
}

// TracingConfig represents standardized distributed tracing configuration.
type TracingConfig struct {
	Enabled        bool    `mapstructure:"enabled"`
	ServiceName    string  `mapstructure:"service_name"`
	ServiceVersion string  `mapstructure:"service_version"`
	Environment    string  `mapstructure:"environment"`
	SamplerType    string  `mapstructure:"sampler_type"`  // "always_on", "always_off", "traceidratio"
	SamplerParam   float64 `mapstructure:"sampler_param"` // For traceidratio sampler
	ExporterType   string  `mapstructure:"exporter_type"` // "otlp", "stdout"
	OTLPEndpoint   string  `mapstructure:"otlp_endpoint"`
	OTLPInsecure   bool    `mapstructure:"otlp_insecure"`
}
