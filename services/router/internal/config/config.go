package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/actual-software/mcp-bridge/services/router/internal/direct"
	"github.com/actual-software/mcp-bridge/services/router/internal/secure"
)

const (
	defaultTimeoutSeconds    = 30
	defaultMaxTimeoutSeconds = 60
	defaultRetryCount        = 10
	defaultMaxRetries        = 100

	// AuthTypeBearerToken represents bearer token authentication.
	AuthTypeBearerToken = "bearer"
	// AuthTypeOAuth2 represents OAuth2 authentication.
	AuthTypeOAuth2 = "oauth2"
	// AuthTypeMTLS represents mutual TLS authentication.
	AuthTypeMTLS = "mtls"
)

type Config struct {
	Version     int                  `mapstructure:"version"      yaml:"version"`
	GatewayPool GatewayPoolConfig    `mapstructure:"gateway_pool" yaml:"gateway_pool"` // Multi-gateway support only
	Direct      direct.DirectConfig  `mapstructure:"direct"       yaml:"direct"`       // Direct server support
	Local       LocalConfig          `mapstructure:"local"        yaml:"local"`
	Logging     common.LoggingConfig `mapstructure:"logging"      yaml:"logging"`
	Metrics     common.MetricsConfig `mapstructure:"metrics"      yaml:"metrics"`
	Tracing     common.TracingConfig `mapstructure:"tracing"      yaml:"tracing"`
	Advanced    AdvancedConfig       `mapstructure:"advanced"     yaml:"advanced"`
}

// GatewayConfig represents a single gateway configuration (kept for compatibility with client code).
type GatewayConfig struct {
	URL        string                  `mapstructure:"url"`
	Auth       common.AuthConfig       `mapstructure:"auth"`
	Connection common.ConnectionConfig `mapstructure:"connection"`
	TLS        common.TLSConfig        `mapstructure:"tls"`
}

// GatewayEndpoint represents a single gateway instance.
type GatewayEndpoint struct {
	URL        string                  `mapstructure:"url"        yaml:"url"`
	Auth       common.AuthConfig       `mapstructure:"auth"       yaml:"auth"`
	Connection common.ConnectionConfig `mapstructure:"connection" yaml:"connection"`
	TLS        common.TLSConfig        `mapstructure:"tls"        yaml:"tls"`
	Weight     int                     `mapstructure:"weight"     yaml:"weight"`   // For weighted load balancing
	Priority   int                     `mapstructure:"priority"   yaml:"priority"` // For priority-based routing
	Tags       []string                `mapstructure:"tags"       yaml:"tags"`     // For namespace-based routing
}

// LoadBalancerConfig defines load balancing strategy and parameters.
type LoadBalancerConfig struct {
	Strategy        string        `mapstructure:"strategy"`          // round_robin, least_connections, weighted, priority
	HealthCheckPath string        `mapstructure:"health_check_path"` // Optional health check endpoint
	FailoverTimeout time.Duration `mapstructure:"failover_timeout"`  // Time to wait before marking endpoint as failed
	RetryCount      int           `mapstructure:"retry_count"`       // Number of retries before failover
}

// ServiceDiscoveryConfig defines service discovery settings.
type ServiceDiscoveryConfig struct {
	Enabled             bool          `mapstructure:"enabled"`
	RefreshInterval     time.Duration `mapstructure:"refresh_interval"`      // How often to refresh endpoint list
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"` // How often to health check endpoints
	UnhealthyThreshold  int           `mapstructure:"unhealthy_threshold"`   // Failed checks before marking unhealthy
	HealthyThreshold    int           `mapstructure:"healthy_threshold"`     // Successful checks before marking healthy
}

// GatewayPoolConfig defines configuration for multiple gateway support.
type GatewayPoolConfig struct {
	Endpoints        []GatewayEndpoint      `mapstructure:"endpoints"          yaml:"endpoints"`
	DefaultNamespace string                 `mapstructure:"default_namespace"  yaml:"default_namespace"` // Default MCP namespace when not specified
	LoadBalancer     LoadBalancerConfig     `mapstructure:"load_balancer"      yaml:"load_balancer"`
	ServiceDiscovery ServiceDiscoveryConfig `mapstructure:"service_discovery"  yaml:"service_discovery"`
	CircuitBreaker   CircuitBreakerConfig   `mapstructure:"circuit_breaker"    yaml:"circuit_breaker"`
	NamespaceRouting NamespaceRoutingConfig `mapstructure:"namespace_routing"  yaml:"namespace_routing"`
}

// CircuitBreakerConfig defines circuit breaker settings for gateway connections.
type CircuitBreakerConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	FailureThreshold int           `mapstructure:"failure_threshold"`
	RecoveryTimeout  time.Duration `mapstructure:"recovery_timeout"`
	SuccessThreshold int           `mapstructure:"success_threshold"`
	TimeoutDuration  time.Duration `mapstructure:"timeout_duration"`
	MonitoringWindow time.Duration `mapstructure:"monitoring_window"`
}

// NamespaceRoutingRule defines a rule for routing MCP methods to specific endpoints.
type NamespaceRoutingRule struct {
	Pattern     string   `mapstructure:"pattern"`
	Tags        []string `mapstructure:"tags"`
	Priority    int      `mapstructure:"priority"`
	Description string   `mapstructure:"description"`
}

// NamespaceRoutingConfig defines configuration for namespace-based routing.
type NamespaceRoutingConfig struct {
	Enabled bool                   `mapstructure:"enabled"`
	Rules   []NamespaceRoutingRule `mapstructure:"rules"`
}

type PoolConfig struct {
	Enabled               bool `mapstructure:"enabled"`
	MinSize               int  `mapstructure:"min_size"`
	MaxSize               int  `mapstructure:"max_size"`
	MaxIdleTimeMs         int  `mapstructure:"max_idle_time_ms"`
	MaxLifetimeMs         int  `mapstructure:"max_lifetime_ms"`
	AcquireTimeoutMs      int  `mapstructure:"acquire_timeout_ms"`
	HealthCheckIntervalMs int  `mapstructure:"health_check_interval_ms"`
}

type LocalConfig struct {
	ReadBufferSize        int                    `mapstructure:"read_buffer_size"`
	WriteBufferSize       int                    `mapstructure:"write_buffer_size"`
	RequestTimeoutMs      int                    `mapstructure:"request_timeout_ms"`
	MaxConcurrentRequests int                    `mapstructure:"max_concurrent_requests"`
	MaxQueuedRequests     int                    `mapstructure:"max_queued_requests"`
	RateLimit             common.RateLimitConfig `mapstructure:"rate_limit"`
}

type AdvancedConfig struct {
	Compression    CompressionConfig           `mapstructure:"compression"`
	CircuitBreaker common.CircuitBreakerConfig `mapstructure:"circuit_breaker"`
	Deduplication  DeduplicationConfig         `mapstructure:"deduplication"`
	Debug          DebugConfig                 `mapstructure:"debug"`
}

type CompressionConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Algorithm string `mapstructure:"algorithm"`
	Level     int    `mapstructure:"level"`
}

type DeduplicationConfig struct {
	Enabled    bool `mapstructure:"enabled"`
	CacheSize  int  `mapstructure:"cache_size"`
	TTLSeconds int  `mapstructure:"ttl_seconds"`
}

type DebugConfig struct {
	LogFrames    bool   `mapstructure:"log_frames"`
	SaveFailures bool   `mapstructure:"save_failures"`
	FailureDir   string `mapstructure:"failure_dir"`
	EnablePprof  bool   `mapstructure:"enable_pprof"`
	PprofPort    int    `mapstructure:"pprof_port"`
}

// Load loads configuration from file or environment.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults.
	setDefaults(v)

	// Set config name and paths.
	if err := setupViperConfig(v, configPath); err != nil {
		return nil, err
	}

	// Enable environment variable support.
	setupViperEnvironment(v)

	// Explicitly bind environment variables.
	if err := bindEnvironmentVariables(v); err != nil {
		return nil, err
	}

	// Try to read config file.
	if err := readConfigFile(v); err != nil {
		return nil, err
	}

	// Unmarshal and validate configuration.
	cfg, err := unmarshalAndValidateConfig(v)
	if err != nil {
		return nil, err
	}

	// Handle auth token from environment or file.
	if err := loadAuthToken(cfg); err != nil {
		return nil, fmt.Errorf("failed to load auth token: %w", err)
	}

	return cfg, nil
}

// setupViperConfig configures viper for config file paths.
func setupViperConfig(v *viper.Viper, configPath string) error {
	if configPath != "" {
		v.SetConfigFile(configPath)
		// Always set config type to yaml for consistency.
		v.SetConfigType("yaml")
	} else {
		v.SetConfigName("mcp-router")
		v.SetConfigType("yaml")

		// Add config search paths.
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.config/claude-cli")
		v.AddConfigPath("$HOME/.config/mcp")
		v.AddConfigPath("/etc/mcp")
	}

	return nil
}

// setupViperEnvironment configures viper for environment variables.
func setupViperEnvironment(v *viper.Viper) {
	v.SetEnvPrefix("MCP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
}

// bindEnvironmentVariables binds specific environment variables.
func bindEnvironmentVariables(v *viper.Viper) error {
	envBindings := map[string]string{
		"gateway_pool.load_balancer.strategy":    "MCP_LOAD_BALANCER_STRATEGY",
		"gateway_pool.circuit_breaker.enabled":   "MCP_CIRCUIT_BREAKER_ENABLED",
		"gateway_pool.service_discovery.enabled": "MCP_SERVICE_DISCOVERY_ENABLED",
		"direct.auto_detection.enabled":          "MCP_DIRECT_AUTO_DETECTION_ENABLED",
		"direct.health_check.enabled":            "MCP_DIRECT_HEALTH_CHECK_ENABLED",
		"direct.max_connections":                 "MCP_DIRECT_MAX_CONNECTIONS",
		"logging.level":                          "MCP_LOGGING_LEVEL",
	}

	for key, envVar := range envBindings {
		if err := v.BindEnv(key, envVar); err != nil {
			return fmt.Errorf("failed to bind environment variable %s: %w", envVar, err)
		}
	}

	return nil
}

// readConfigFile attempts to read the configuration file.
func readConfigFile(v *viper.Viper) error {
	if err := v.ReadInConfig(); err != nil {
		// It's okay if config file doesn't exist, we'll use defaults and env vars.
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	return nil
}

// unmarshalAndValidateConfig unmarshals and validates the configuration.
func unmarshalAndValidateConfig(v *viper.Viper) (*Config, error) {
	// Unmarshal configuration.
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Validate configuration.
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	setGatewayPoolDefaults(v)
	setDirectDefaults(v)
	setLocalDefaults(v)
	setLoggingDefaults(v)
	setMetricsDefaults(v)
	setAdvancedDefaults(v)
}

func setGatewayPoolDefaults(v *viper.Viper) {
	// Gateway Pool defaults.
	v.SetDefault("gateway_pool.default_namespace", "default")
	v.SetDefault("gateway_pool.load_balancer.strategy", "round_robin")
	v.SetDefault("gateway_pool.load_balancer.failover_timeout", "30s")
	v.SetDefault("gateway_pool.load_balancer.retry_count", constants.DefaultMaxRetries)
	v.SetDefault("gateway_pool.service_discovery.enabled", false)
	v.SetDefault("gateway_pool.service_discovery.refresh_interval", "30s")
	v.SetDefault("gateway_pool.service_discovery.health_check_interval", "10s")
	v.SetDefault("gateway_pool.service_discovery.unhealthy_threshold", constants.DefaultMaxRetries)
	v.SetDefault("gateway_pool.service_discovery.healthy_threshold", constants.DefaultSuccessThreshold)
	v.SetDefault("gateway_pool.circuit_breaker.enabled", false)
	v.SetDefault("gateway_pool.circuit_breaker.failure_threshold", constants.DefaultFailureThreshold)
	v.SetDefault("gateway_pool.circuit_breaker.recovery_timeout", "30s")
	v.SetDefault("gateway_pool.circuit_breaker.success_threshold", constants.DefaultMaxRetries)
	v.SetDefault("gateway_pool.circuit_breaker.timeout_duration", "10s")
	v.SetDefault("gateway_pool.circuit_breaker.monitoring_window", "60s")
	v.SetDefault("gateway_pool.namespace_routing.enabled", false)
}

func setDirectDefaults(v *viper.Viper) {
	// Direct server defaults.
	v.SetDefault("direct.default_timeout", "30s")
	v.SetDefault("direct.max_connections", constants.DefaultMaxConnections)
	v.SetDefault("direct.health_check.enabled", true)
	v.SetDefault("direct.health_check.interval", "30s")
	v.SetDefault("direct.health_check.timeout", "5s")
	v.SetDefault("direct.auto_detection.enabled", true)
	v.SetDefault("direct.auto_detection.timeout", "10s")
	v.SetDefault("direct.auto_detection.cache_results", true)
	v.SetDefault("direct.auto_detection.cache_ttl", "5m")
	v.SetDefault("direct.auto_detection.preferred_order", []string{"http", "websocket", "stdio", "sse"})

	// Direct protocol-specific defaults.
	v.SetDefault("direct.stdio.process_timeout", "30s")
	v.SetDefault("direct.stdio.max_buffer_size", constants.DefaultSmallBufferSize)
	v.SetDefault("direct.websocket.handshake_timeout", "10s")
	v.SetDefault("direct.websocket.ping_interval", "30s")
	v.SetDefault("direct.websocket.pong_timeout", "10s")
	v.SetDefault("direct.websocket.max_message_size", constants.MaxBufferSize)
	v.SetDefault("direct.http.request_timeout", "30s")
	v.SetDefault("direct.http.max_idle_conns", constants.DefaultMaxConnectionsPerHost)
	v.SetDefault("direct.http.follow_redirects", true)
	v.SetDefault("direct.sse.request_timeout", "30s")
	v.SetDefault("direct.sse.stream_timeout", "300s")
	v.SetDefault("direct.sse.buffer_size", constants.DefaultSmallBufferSize)
}

func setLocalDefaults(v *viper.Viper) {
	// Local defaults.
	v.SetDefault("local.read_buffer_size", constants.DefaultReadBufferSize)
	v.SetDefault("local.write_buffer_size", constants.DefaultWriteBufferSize)
	v.SetDefault("local.request_timeout_ms", constants.DefaultMaxMessageSize)
	v.SetDefault("local.max_concurrent_requests", constants.DefaultMaxConcurrency)
	v.SetDefault("local.max_queued_requests", constants.DefaultBufferSize)
}

func setLoggingDefaults(v *viper.Viper) {
	// Logging defaults.
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("logging.output", "stderr")
	v.SetDefault("logging.include_caller", false)
	v.SetDefault("logging.sampling.enabled", false)
	v.SetDefault("logging.sampling.initial", constants.DefaultBufferSize)
	v.SetDefault("logging.sampling.thereafter", constants.DefaultBufferSize)
}

func setMetricsDefaults(v *viper.Viper) {
	// Metrics defaults.
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.endpoint", "localhost:9091")
}

func setAdvancedDefaults(v *viper.Viper) {
	// Advanced defaults.
	v.SetDefault("advanced.compression.enabled", true)
	v.SetDefault("advanced.compression.algorithm", "gzip")
	v.SetDefault("advanced.compression.level", constants.DefaultCompressionLevel)
	v.SetDefault("advanced.circuit_breaker.failure_threshold", constants.DefaultFailureThreshold)
	v.SetDefault("advanced.circuit_breaker.success_threshold", constants.DefaultSuccessThreshold)
	v.SetDefault("advanced.circuit_breaker.timeout_seconds", int(constants.CircuitBreakerOpenDuration.Seconds()))
	v.SetDefault("advanced.deduplication.enabled", true)
	v.SetDefault("advanced.deduplication.cache_size", constants.DefaultDeduplicationCacheSize)
	v.SetDefault("advanced.deduplication.ttl_seconds", constants.DefaultDeduplicationTTLSeconds)
	v.SetDefault("advanced.debug.log_frames", false)
	v.SetDefault("advanced.debug.save_failures", false)
	v.SetDefault("advanced.debug.failure_dir", "/tmp/mcp-failures")
	v.SetDefault("advanced.debug.enable_pprof", false)
	v.SetDefault("advanced.debug.pprof_port", constants.DefaultDebugPort)
}

// validateLegacy preserves the old implementation for reference.

func loadAuthToken(cfg *Config) error {
	// Create secure token store.
	store, err := secure.NewTokenStore("mcp-router")
	if err != nil {
		// Log warning but continue with traditional methods.
		// Ignoring error: writing to stderr in error path.
		_, _ = fmt.Fprintf(os.Stderr, "Warning: Failed to initialize secure token store: %v\n", err)
	}

	// Load auth tokens for each endpoint.
	for i, endpoint := range cfg.GatewayPool.Endpoints {
		switch endpoint.Auth.Type {
		case AuthTypeBearerToken:
			token, err := loadBearerToken(store, &endpoint.Auth, i)
			if err != nil {
				return fmt.Errorf("failed to load bearer token for endpoint %d: %w", i, err)
			}

			cfg.GatewayPool.Endpoints[i].Auth.Token = token

		case AuthTypeOAuth2:
			if err := loadOAuth2Credentials(store, &cfg.GatewayPool.Endpoints[i].Auth, i); err != nil {
				return fmt.Errorf("failed to load OAuth2 credentials for endpoint %d: %w", i, err)
			}
		}
	}

	return nil
}

func loadBearerToken(store secure.TokenStore, auth *common.AuthConfig, endpointIndex int) (string, error) {
	// Use the descriptive bearer token resolver instead of complex inline logic.
	resolver := InitializeBearerTokenResolver(store, auth, endpointIndex)

	return resolver.ResolveToken()
}

func loadOAuth2Credentials(store secure.TokenStore, auth *common.AuthConfig, endpointIndex int) error {
	// Load client secret from multiple sources.
	loadCredential(store, &credentialRequest{
		targetField:   &auth.ClientSecret,
		secureKey:     auth.ClientSecretKey,
		envVar:        auth.ClientSecretEnv,
		defaultEnvVar: "MCP_CLIENT_SECRET",
		defaultKeyFmt: "endpoint-%d-oauth2-client-secret",
		endpointIndex: endpointIndex,
	})

	// Load password for password grant type.
	if auth.GrantType == "password" {
		loadCredential(store, &credentialRequest{
			targetField:   &auth.Password,
			secureKey:     auth.PasswordKey,
			envVar:        auth.PasswordEnv,
			defaultEnvVar: "MCP_OAUTH_PASSWORD",
			defaultKeyFmt: "endpoint-%d-oauth2-password",
			endpointIndex: endpointIndex,
		})

		if auth.Password == "" {
			return errors.New(
				"no password found for OAuth2 password grant (check password_secure_key or MCP_OAUTH_PASSWORD env var)")
		}
	}

	// Set default grant type if not specified.
	if auth.GrantType == "" {
		auth.GrantType = "client_credentials"
	}

	return nil
}

// credentialRequest contains parameters for loading a credential from multiple sources.
type credentialRequest struct {
	targetField   *string
	secureKey     string
	envVar        string
	defaultEnvVar string
	defaultKeyFmt string
	endpointIndex int
}

// loadCredential loads a credential from secure storage, environment variables, or defaults.
func loadCredential(store secure.TokenStore, req *credentialRequest) {
	// Use the descriptive credential resolver instead of complex inline logic.
	resolver := InitializeCredentialResolver(store)
	resolver.ResolveCredential(req)
}

func expandPath(path string) string {
	if path == "" {
		return path
	}

	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}

	return path
}

func (c *Config) GetRequestTimeout() time.Duration {
	return time.Duration(c.Local.RequestTimeoutMs) * time.Millisecond
}

// IsMultiGatewayMode returns true if multi-gateway configuration is enabled.
func (c *Config) IsMultiGatewayMode() bool {
	return true // Always multi-gateway mode now
}

// GetGatewayEndpoints returns the list of gateway endpoints to use.
func (c *Config) GetGatewayEndpoints() []GatewayEndpoint {
	return c.GatewayPool.Endpoints
}

// GetLoadBalancerConfig returns the load balancer configuration with defaults.
func (c *Config) GetLoadBalancerConfig() LoadBalancerConfig {
	config := c.GatewayPool.LoadBalancer
	// Set defaults if not configured.
	if config.Strategy == "" {
		config.Strategy = "round_robin"
	}

	if config.FailoverTimeout == 0 {
		config.FailoverTimeout = defaultTimeoutSeconds * time.Second
	}

	if config.RetryCount == 0 {
		config.RetryCount = 3
	}

	return config
}

// GetServiceDiscoveryConfig returns the service discovery configuration with defaults.
func (c *Config) GetServiceDiscoveryConfig() ServiceDiscoveryConfig {
	config := c.GatewayPool.ServiceDiscovery
	// Set defaults if not configured.
	if config.RefreshInterval == 0 {
		config.RefreshInterval = defaultTimeoutSeconds * time.Second
	}

	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = defaultRetryCount * time.Second
	}

	if config.UnhealthyThreshold == 0 {
		config.UnhealthyThreshold = 3
	}

	if config.HealthyThreshold == 0 {
		config.HealthyThreshold = 2
	}

	return config
}

// GetCircuitBreakerConfig returns the circuit breaker configuration with defaults.
func (c *Config) GetCircuitBreakerConfig() CircuitBreakerConfig {
	config := c.GatewayPool.CircuitBreaker
	// Set defaults if not configured.
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}

	if config.RecoveryTimeout == 0 {
		config.RecoveryTimeout = defaultTimeoutSeconds * time.Second
	}

	if config.SuccessThreshold == 0 {
		config.SuccessThreshold = 3
	}

	if config.TimeoutDuration == 0 {
		config.TimeoutDuration = defaultRetryCount * time.Second
	}

	if config.MonitoringWindow == 0 {
		config.MonitoringWindow = defaultMaxTimeoutSeconds * time.Second
	}

	return config
}

// GetNamespaceRoutingConfig returns the namespace routing configuration with defaults.
func (c *Config) GetNamespaceRoutingConfig() NamespaceRoutingConfig {
	return c.GatewayPool.NamespaceRouting
}

// GetDirectConfig returns the direct server configuration with defaults applied.
func (c *Config) GetDirectConfig() direct.DirectConfig {
	return c.Direct
}

// IsDirectMode returns true if the router should operate in direct server mode.
// This is determined by checking if direct configuration is enabled and valid.
func (c *Config) IsDirectMode() bool {
	// Check if direct configuration has meaningful settings.
	return c.Direct.MaxConnections > 0
}
