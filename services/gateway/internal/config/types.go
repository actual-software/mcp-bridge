// Package config defines configuration structures for the MCP gateway server.
package config

import (
	"time"
)

// Config represents the complete configuration for the MCP gateway.
type Config struct {
	Version        int                    `mapstructure:"version"`
	Server         ServerConfig           `mapstructure:"server"`
	Auth           AuthConfig             `mapstructure:"auth"`
	Sessions       SessionConfig          `mapstructure:"sessions"`
	Routing        RoutingConfig          `mapstructure:"routing"`
	Discovery      ServiceDiscoveryConfig `mapstructure:"service_discovery"`
	Backends       BackendConfig          `mapstructure:"backends"`
	RateLimit      RateLimitConfig        `mapstructure:"rate_limit"`
	CircuitBreaker CircuitBreakerConfig   `mapstructure:"circuit_breaker"`
	Metrics        MetricsConfig          `mapstructure:"metrics"`
	Logging        LoggingConfig          `mapstructure:"logging"`
	Tracing        TracingConfig          `mapstructure:"tracing"`
}

// ServerConfig represents the HTTP server configuration.
type ServerConfig struct {
	Host                 string                `mapstructure:"host"`
	Port                 int                   `mapstructure:"port"`
	TCPPort              int                   `mapstructure:"tcp_port"`
	TCPHealthPort        int                   `mapstructure:"tcp_health_port"`
	MetricsPort          int                   `mapstructure:"metrics_port"`
	HealthPort           int                   `mapstructure:"health_port"`
	MaxConnections       int                   `mapstructure:"max_connections"`
	MaxConnectionsPerIP  int                   `mapstructure:"max_connections_per_ip"`
	ConnectionBufferSize int                   `mapstructure:"connection_buffer_size"`
	ReadTimeout          int                   `mapstructure:"read_timeout"`
	WriteTimeout         int                   `mapstructure:"write_timeout"`
	IdleTimeout          int                   `mapstructure:"idle_timeout"`
	// Protocol specifies legacy protocol config (deprecated, use Frontends)
	Protocol string `mapstructure:"protocol"`
	AllowedOrigins       []string              `mapstructure:"allowed_origins"`
	TLS                  TLSConfig             `mapstructure:"tls"`
	StdioFrontend        StdioFrontendConfig   `mapstructure:"stdio_frontend"`
	Frontends            []FrontendConfigEntry `mapstructure:"frontends"`
}

// FrontendConfigEntry represents a single frontend configuration entry.
type FrontendConfigEntry struct {
	Name     string                 `mapstructure:"name"`
	Protocol string                 `mapstructure:"protocol"` // "websocket", "http", "sse", "tcp_binary", "stdio"
	Enabled  bool                   `mapstructure:"enabled"`
	Config   map[string]interface{} `mapstructure:"config"`
}

// TLSConfig represents TLS configuration.
type TLSConfig struct {
	Enabled      bool     `mapstructure:"enabled"`
	CertFile     string   `mapstructure:"cert_file"`
	KeyFile      string   `mapstructure:"key_file"`
	CAFile       string   `mapstructure:"ca_file"`
	ClientAuth   string   `mapstructure:"client_auth"` // "none", "request", "require"
	MinVersion   string   `mapstructure:"min_version"`
	CipherSuites []string `mapstructure:"cipher_suites"`
}

// StdioFrontendConfig contains configuration for stdio frontend.
type StdioFrontendConfig struct {
	Enabled bool                       `mapstructure:"enabled"`
	Modes   []StdioFrontendModeConfig  `mapstructure:"modes"`
	Process StdioFrontendProcessConfig `mapstructure:"process_management"`
}

// StdioFrontendModeConfig contains configuration for a specific stdio mode.
type StdioFrontendModeConfig struct {
	Type        string `mapstructure:"type"`        // "unix_socket", "stdin_stdout", "named_pipes"
	Path        string `mapstructure:"path"`        // Socket path or pipe name
	Permissions string `mapstructure:"permissions"` // Unix socket permissions
	Enabled     bool   `mapstructure:"enabled"`
}

// StdioFrontendProcessConfig contains process management settings for stdio frontend.
type StdioFrontendProcessConfig struct {
	MaxConcurrentClients int           `mapstructure:"max_concurrent_clients"`
	ClientTimeout        time.Duration `mapstructure:"client_timeout"`
	AuthRequired         bool          `mapstructure:"auth_required"`
}

// AuthConfig represents authentication configuration.
type AuthConfig struct {
	Provider            string       `mapstructure:"provider"`
	PerMessageAuth      bool         `mapstructure:"per_message_auth"`
	PerMessageAuthCache int          `mapstructure:"per_message_auth_cache"`
	JWT                 JWTConfig    `mapstructure:"jwt"`
	OAuth2              OAuth2Config `mapstructure:"oauth2"`
}

// JWTConfig represents the JWT configuration.
type JWTConfig struct {
	Issuer        string `mapstructure:"issuer"`
	Audience      string `mapstructure:"audience"`
	SecretKeyEnv  string `mapstructure:"secret_key_env"`
	PublicKeyPath string `mapstructure:"public_key_path"`
}

// OAuth2Config represents the OAuth2 configuration.
type OAuth2Config struct {
	ClientID           string   `mapstructure:"client_id"`
	ClientSecretEnv    string   `mapstructure:"client_secret_env"`
	TokenEndpoint      string   `mapstructure:"token_endpoint"`
	IntrospectEndpoint string   `mapstructure:"introspect_endpoint"`
	JWKSEndpoint       string   `mapstructure:"jwks_endpoint"`
	Scopes             []string `mapstructure:"scopes"`
	Issuer             string   `mapstructure:"issuer"`
	Audience           string   `mapstructure:"audience"`
}

// ServiceDiscoveryConfig represents the service discovery configuration.
type ServiceDiscoveryConfig struct {
	Provider          string                    `mapstructure:"provider"`
	Mode              string                    `mapstructure:"mode"` // Alias for provider
	Kubernetes        KubernetesDiscoveryConfig `mapstructure:"kubernetes"`
	Static            StaticDiscoveryConfig     `mapstructure:"static"`
	Stdio             StdioDiscoveryConfig      `mapstructure:"stdio"`
	WebSocket         WebSocketDiscoveryConfig  `mapstructure:"websocket"`
	SSE               SSEDiscoveryConfig        `mapstructure:"sse"`
	RefreshRate       time.Duration             `mapstructure:"refresh_rate"`
	NamespaceSelector []string                  `mapstructure:"namespace_selector"`
	LabelSelector     map[string]string         `mapstructure:"label_selector"`
}

// KubernetesDiscoveryConfig represents Kubernetes discovery configuration.
type KubernetesDiscoveryConfig struct {
	InCluster        bool              `mapstructure:"in_cluster"`
	ConfigPath       string            `mapstructure:"config_path"`
	NamespacePattern string            `mapstructure:"namespace_pattern"`
	ServiceLabels    map[string]string `mapstructure:"service_labels"`
}

// StaticDiscoveryConfig represents static discovery configuration.
type StaticDiscoveryConfig struct {
	Endpoints map[string][]EndpointConfig `mapstructure:"endpoints"`
}

// EndpointConfig represents a static endpoint configuration.
type EndpointConfig struct {
	URL    string                 `mapstructure:"url"`
	Labels map[string]interface{} `mapstructure:"labels"`
}

// StdioDiscoveryConfig contains configuration for stdio-based service discovery.
type StdioDiscoveryConfig struct {
	Services []StdioServiceConfig `mapstructure:"services"`
}

// StdioServiceConfig contains configuration for a single stdio service.
type StdioServiceConfig struct {
	Name        string                 `mapstructure:"name"`
	Namespace   string                 `mapstructure:"namespace"`
	Command     []string               `mapstructure:"command"`
	WorkingDir  string                 `mapstructure:"working_dir"`
	Env         map[string]string      `mapstructure:"env"`
	Weight      int                    `mapstructure:"weight"`
	Metadata    map[string]string      `mapstructure:"metadata"`
	HealthCheck StdioHealthCheckConfig `mapstructure:"health_check"`
}

// StdioHealthCheckConfig contains health check configuration for stdio services.
type StdioHealthCheckConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// WebSocketDiscoveryConfig contains configuration for WebSocket-based service discovery.
type WebSocketDiscoveryConfig struct {
	Services []WebSocketServiceConfig `mapstructure:"services"`
}

// WebSocketServiceConfig contains configuration for a single WebSocket service.
type WebSocketServiceConfig struct {
	Name        string                     `mapstructure:"name"`
	Namespace   string                     `mapstructure:"namespace"`
	Endpoints   []string                   `mapstructure:"endpoints"`
	Headers     map[string]string          `mapstructure:"headers"`
	Weight      int                        `mapstructure:"weight"`
	Metadata    map[string]string          `mapstructure:"metadata"`
	HealthCheck WebSocketHealthCheckConfig `mapstructure:"health_check"`
	TLS         WebSocketTLSConfig         `mapstructure:"tls"`
	Origin      string                     `mapstructure:"origin"`
}

// WebSocketHealthCheckConfig contains health check configuration for WebSocket services.
type WebSocketHealthCheckConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// WebSocketTLSConfig contains TLS configuration for WebSocket connections.
type WebSocketTLSConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"`
	CertFile           string `mapstructure:"cert_file"`
	KeyFile            string `mapstructure:"key_file"`
	CAFile             string `mapstructure:"ca_file"`
}

// SSEDiscoveryConfig contains configuration for SSE-based service discovery.
type SSEDiscoveryConfig struct {
	Services []SSEServiceConfig `mapstructure:"services"`
}

// SSEServiceConfig contains configuration for a single SSE service.
type SSEServiceConfig struct {
	Name            string               `mapstructure:"name"`
	Namespace       string               `mapstructure:"namespace"`
	BaseURL         string               `mapstructure:"base_url"`
	StreamEndpoint  string               `mapstructure:"stream_endpoint"`
	RequestEndpoint string               `mapstructure:"request_endpoint"`
	Headers         map[string]string    `mapstructure:"headers"`
	Weight          int                  `mapstructure:"weight"`
	Metadata        map[string]string    `mapstructure:"metadata"`
	HealthCheck     SSEHealthCheckConfig `mapstructure:"health_check"`
	Timeout         time.Duration        `mapstructure:"timeout"`
}

// SSEHealthCheckConfig contains health check configuration for SSE services.
type SSEHealthCheckConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// BackendConfig contains configuration for backend protocol support.
type BackendConfig struct {
	Backends []BackendInstanceConfig `mapstructure:"backends"`
}

// BackendInstanceConfig contains configuration for a single backend instance.
type BackendInstanceConfig struct {
	Name     string                 `mapstructure:"name"`
	Protocol string                 `mapstructure:"protocol"`
	Config   map[string]interface{} `mapstructure:"config"`
}

// RedisConfig represents Redis configuration.
type RedisConfig struct {
	URL         string   `mapstructure:"url"`
	Addresses   []string `mapstructure:"addresses"`
	Password    string   `mapstructure:"password"`
	DB          int      `mapstructure:"db"`
	PoolSize    int      `mapstructure:"pool_size"`
	MaxRetries  int      `mapstructure:"max_retries"`
	DialTimeout int      `mapstructure:"dial_timeout"`
}

// MemoryConfig represents in-memory configuration.
type MemoryConfig struct {
	CleanupInterval int `mapstructure:"cleanup_interval"`
}

// SessionConfig represents session management configuration.
type SessionConfig struct {
	Provider        string       `mapstructure:"provider"` // "redis" or "memory"
	TTL             int          `mapstructure:"ttl"`      // Session TTL in seconds
	CleanupInterval int          `mapstructure:"cleanup_interval"`
	Redis           RedisConfig  `mapstructure:"redis"`
	Memory          MemoryConfig `mapstructure:"memory"`
}

// RoutingConfig represents the routing configuration.
type RoutingConfig struct {
	Strategy            string               `mapstructure:"strategy"`
	HealthCheckInterval string               `mapstructure:"health_check_interval"`
	CircuitBreaker      CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

// CircuitBreakerConfig represents circuit breaker configuration.
type CircuitBreakerConfig struct {
	Enabled          bool `mapstructure:"enabled"`
	FailureThreshold int  `mapstructure:"failure_threshold"`
	SuccessThreshold int  `mapstructure:"success_threshold"`
	TimeoutSeconds   int  `mapstructure:"timeout_seconds"`
	MaxRequests      int  `mapstructure:"max_requests"`
	IntervalSeconds  int  `mapstructure:"interval_seconds"`
}

// RateLimitConfig represents rate limiting configuration.
type RateLimitConfig struct {
	Enabled         bool        `mapstructure:"enabled"`
	Provider        string      `mapstructure:"provider"` // "redis" or "memory"
	RequestsPerSec  int         `mapstructure:"requests_per_sec"`
	Burst           int         `mapstructure:"burst"`
	CleanupInterval int         `mapstructure:"cleanup_interval"`
	Redis           RedisConfig `mapstructure:"redis"`
}

// MetricsConfig represents metrics configuration.
type MetricsConfig struct {
	Enabled  bool              `mapstructure:"enabled"`
	Endpoint string            `mapstructure:"endpoint"`
	Path     string            `mapstructure:"path"`
	Labels   map[string]string `mapstructure:"labels"`
}

// LoggingConfig represents logging configuration.
type LoggingConfig struct {
	Level         string `mapstructure:"level"`
	Format        string `mapstructure:"format"`
	IncludeCaller bool   `mapstructure:"include_caller"`
}

// TracingConfig represents the distributed tracing configuration.
type TracingConfig struct {
	Enabled        bool    `mapstructure:"enabled"`
	ServiceName    string  `mapstructure:"service_name"`
	ServiceVersion string  `mapstructure:"service_version"`
	Environment    string  `mapstructure:"environment"`
	SamplerType    string  `mapstructure:"sampler_type"`
	SamplerParam   float64 `mapstructure:"sampler_param"`
	ExporterType   string  `mapstructure:"exporter_type"`
	OTLPEndpoint   string  `mapstructure:"otlp_endpoint"`
	OTLPInsecure   bool    `mapstructure:"otlp_insecure"`
}
