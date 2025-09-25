// Package server implements the MCP gateway HTTP and TCP server with WebSocket and binary protocol support.
package server

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
)

const (
	defaultRetryCount              = 10
	defaultTimeoutSeconds          = 30
	defaultMaxConnections          = 5
	defaultMaxTimeoutSeconds       = 60
	defaultHTTPPort                = 8080
	defaultHTTPSPort               = 8443
	defaultMetricsPort             = 9090
	defaultMaxConnectionsGlobal    = 50000
	defaultMaxConnectionsPerIP     = 1000
	defaultConnectionBufferSize    = 65536
	defaultIdleTimeoutSeconds      = 120
	defaultSessionTTLSeconds       = 86400
	defaultCleanupIntervalSeconds  = 300
	defaultCircuitSuccessThreshold = 2
	defaultCircuitSuccessRatio     = 0.6

	// AuthProviderJWT represents the JWT authentication provider type.
	AuthProviderJWT = "jwt"
	// AuthProviderKubernetes represents the Kubernetes authentication provider type.
	AuthProviderKubernetes = "kubernetes"
	// AuthProviderStatic represents the static authentication provider type.
	AuthProviderStatic = "static"

	// ProtocolTCP represents the TCP protocol type.
	ProtocolTCP = "tcp"
	// ProtocolBoth represents support for both TCP and WebSocket protocols.
	ProtocolBoth = "both"
)

// LoadConfig loads configuration from file.
func LoadConfig(configPath string) (*config.Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Set config file
	v.SetConfigFile(configPath)

	// Enable environment variable support
	v.SetEnvPrefix("MCP_GATEWAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	// Unmarshal configuration
	var cfg config.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	// Load secrets from environment
	if err := loadSecretsWithViper(v, &cfg); err != nil {
		return nil, fmt.Errorf("failed to load secrets: %w", err)
	}

	// Validate configuration
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &cfg, nil
}

// setServerDefaults sets server-related defaults.
func setServerDefaults(v *viper.Viper) {
	v.SetDefault("server.port", defaultHTTPSPort)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.metrics_port", defaultMetricsPort)
	v.SetDefault("server.health_port", defaultHTTPPort)
	v.SetDefault("server.max_connections", defaultMaxConnectionsGlobal)
	v.SetDefault("server.max_connections_per_ip", defaultMaxConnectionsPerIP)
	v.SetDefault("server.connection_buffer_size", defaultConnectionBufferSize)
	v.SetDefault("server.read_timeout", defaultRetryCount)
	v.SetDefault("server.write_timeout", defaultRetryCount)
	v.SetDefault("server.idle_timeout", defaultIdleTimeoutSeconds)
	v.SetDefault("server.allowed_origins", []string{"http://localhost", "https://localhost"})
	v.SetDefault("server.tls.enabled", true)
	v.SetDefault("server.tls.cert_file", "/etc/mcp-gateway/tls/tls.crt")
	v.SetDefault("server.tls.key_file", "/etc/mcp-gateway/tls/tls.key")
	v.SetDefault("server.tls.min_version", "1.2")
	v.SetDefault("server.tls.client_auth", "none")
}

// setServiceDefaults sets service-related defaults.
func setServiceDefaults(v *viper.Viper) {
	v.SetDefault("auth.provider", AuthProviderJWT)
	v.SetDefault("auth.jwt.issuer", "mcp-gateway")
	v.SetDefault("auth.jwt.audience", "mcp-gateway")
	v.SetDefault("sessions.provider", "redis")
	v.SetDefault("sessions.ttl", defaultSessionTTLSeconds)
	v.SetDefault("sessions.cleanup_interval", defaultCleanupIntervalSeconds)
	v.SetDefault("routing.strategy", "least_connections")
	v.SetDefault("routing.health_check_interval", "30s")
	v.SetDefault("routing.circuit_breaker.failure_threshold", defaultMaxConnections)
	v.SetDefault("routing.circuit_breaker.success_threshold", defaultCircuitSuccessThreshold)
	v.SetDefault("routing.circuit_breaker.timeout", defaultTimeoutSeconds)
	v.SetDefault("service_discovery.mode", AuthProviderKubernetes)
	v.SetDefault("service_discovery.refresh_rate", "30s")
	v.SetDefault("service_discovery.kubernetes.in_cluster", true)
	v.SetDefault("service_discovery.kubernetes.namespace_pattern", "mcp-*")
	v.SetDefault("service_discovery.namespace_selector", []string{"mcp-*"})
}

// setOperationalDefaults sets operational defaults.
func setOperationalDefaults(v *viper.Viper) {
	v.SetDefault("rate_limit.provider", "memory")
	v.SetDefault("rate_limit.window_size", defaultMaxTimeoutSeconds)
	v.SetDefault("rate_limit.memory.cleanup_interval", defaultCleanupIntervalSeconds)
	v.SetDefault("circuit_breaker.threshold", defaultMaxConnections)
	v.SetDefault("circuit_breaker.timeout", defaultTimeoutSeconds)
	v.SetDefault("circuit_breaker.max_requests", 1)
	v.SetDefault("circuit_breaker.interval", defaultMaxTimeoutSeconds)
	v.SetDefault("circuit_breaker.success_ratio", defaultCircuitSuccessRatio)
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.path", "/metrics")
	v.SetDefault("metrics.port", defaultMetricsPort)
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
}

func setDefaults(v *viper.Viper) {
	setServerDefaults(v)
	setServiceDefaults(v)
	setOperationalDefaults(v)
}

func loadSecretsWithViper(v *viper.Viper, cfg *config.Config) error {
	// Load Redis URL from environment if set
	if redisURL := v.GetString("redis.url"); redisURL != "" {
		cfg.RateLimit.Redis.URL = redisURL
	}

	// Load Redis password from environment if set
	if redisPass := v.GetString("redis.password"); redisPass != "" {
		cfg.RateLimit.Redis.Password = redisPass
	}

	return nil
}

func loadSecrets(cfg *config.Config) error {
	return loadSecretsWithViper(viper.GetViper(), cfg)
}

func validate(cfg *config.Config) error {
	// Validate server configuration
	if err := validateServerPort(cfg.Server.Port); err != nil {
		return err
	}

	if err := validateMaxConnections(cfg.Server.MaxConnections); err != nil {
		return err
	}

	// Validate auth provider
	if err := validateAuthProvider(cfg.Auth.Provider); err != nil {
		return err
	}

	// Validate discovery configuration
	return validateDiscovery(&cfg.Discovery)
}

// validateServerPort validates the server port.
func validateServerPort(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid server port: %d", port)
	}

	return nil
}

// validateMaxConnections validates max connections setting.
func validateMaxConnections(maxConnections int) error {
	if maxConnections <= 0 {
		return errors.New("max_connections must be positive")
	}

	return nil
}

// validateAuthProvider validates the authentication provider.
func validateAuthProvider(provider string) error {
	if provider != "" && provider != AuthProviderJWT {
		return fmt.Errorf("unsupported auth provider: %s", provider)
	}

	return nil
}

// validateDiscovery validates discovery configuration.
func validateDiscovery(discovery *config.ServiceDiscoveryConfig) error {
	// Get the effective provider
	provider := getEffectiveProvider(discovery)

	// Validate provider type
	if err := validateDiscoveryProvider(provider); err != nil {
		return err
	}

	// Validate Kubernetes-specific settings
	return validateKubernetesDiscovery(provider, discovery)
}

// getEffectiveProvider returns the effective provider name.
func getEffectiveProvider(discovery *config.ServiceDiscoveryConfig) string {
	if discovery.Provider != "" {
		return discovery.Provider
	}

	return discovery.Mode
}

// validateDiscoveryProvider validates the discovery provider type.
func validateDiscoveryProvider(provider string) error {
	if provider != "" && provider != AuthProviderKubernetes && provider != AuthProviderStatic {
		return fmt.Errorf("unsupported service discovery mode: %s", provider)
	}

	return nil
}

// validateKubernetesDiscovery validates Kubernetes-specific discovery settings.
func validateKubernetesDiscovery(provider string, discovery *config.ServiceDiscoveryConfig) error {
	if provider == AuthProviderKubernetes && len(discovery.NamespaceSelector) == 0 {
		return errors.New("namespace_selector cannot be empty for kubernetes discovery")
	}

	return nil
}
