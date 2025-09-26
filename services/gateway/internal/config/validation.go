package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

const (
	// Socket types.
	socketTypeUnix = "unix_socket"
)

// ValidateConfig validates the entire configuration.
func ValidateConfig(config *Config) error {
	if err := validateBasicConfig(config); err != nil {
		return err
	}

	if err := validateCoreConfig(config); err != nil {
		return err
	}

	return validateOperationalConfig(config)
}

// validateBasicConfig validates basic configuration parameters.
func validateBasicConfig(config *Config) error {
	if config == nil {
		return customerrors.NewValidationError("config cannot be nil").
			WithComponent("config")
	}

	// Validate version
	if config.Version < 1 || config.Version > 1 {
		return customerrors.NewValidationError(fmt.Sprintf("unsupported config version: %d", config.Version)).
			WithComponent("config").
			WithContext("version", config.Version)
	}

	return nil
}

// validateCoreConfig validates core service configuration.
func validateCoreConfig(config *Config) error {
	// Validate server configuration
	if err := ValidateServerConfig(&config.Server); err != nil {
		return customerrors.Wrap(err, "invalid server configuration").
			WithComponent("config")
	}

	// Validate auth configuration
	if err := ValidateAuthConfig(&config.Auth); err != nil {
		return customerrors.Wrap(err, "invalid auth configuration").
			WithComponent("config")
	}

	// Validate session configuration
	if err := ValidateSessionConfig(&config.Sessions); err != nil {
		return customerrors.Wrap(err, "invalid session configuration").
			WithComponent("config")
	}

	// Validate routing configuration
	if err := ValidateRoutingConfig(&config.Routing); err != nil {
		return customerrors.Wrap(err, "invalid routing configuration").
			WithComponent("config")
	}

	// Validate discovery configuration
	if err := ValidateServiceDiscoveryConfig(&config.Discovery); err != nil {
		return customerrors.Wrap(err, "invalid discovery configuration").
			WithComponent("config")
	}

	return nil
}

// validateOperationalConfig validates operational configuration.
func validateOperationalConfig(config *Config) error {
	// Validate rate limit configuration
	if err := ValidateRateLimitConfig(&config.RateLimit); err != nil {
		return customerrors.Wrap(err, "invalid rate limit configuration").
			WithComponent("config")
	}

	// Validate circuit breaker configuration
	if err := ValidateCircuitBreakerConfig(&config.CircuitBreaker); err != nil {
		return customerrors.Wrap(err, "invalid circuit breaker configuration").
			WithComponent("config")
	}

	// Validate metrics configuration
	if err := ValidateMetricsConfig(&config.Metrics); err != nil {
		return customerrors.Wrap(err, "invalid metrics configuration").
			WithComponent("config")
	}

	// Validate logging configuration
	if err := ValidateLoggingConfig(&config.Logging); err != nil {
		return customerrors.Wrap(err, "invalid logging configuration").
			WithComponent("config")
	}

	// Validate tracing configuration
	if err := ValidateTracingConfig(&config.Tracing); err != nil {
		return customerrors.Wrap(err, "invalid tracing configuration").
			WithComponent("config")
	}

	return nil
}

// ValidateServerConfig validates server configuration.
func ValidateServerConfig(config *ServerConfig) error {
	if config.Host == "" {
		return customerrors.NewEmptyFieldError("host")
	}

	// Validate all ports
	if err := validateServerPorts(config); err != nil {
		return err
	}

	// Validate protocol
	if err := validateServerProtocol(config.Protocol); err != nil {
		return err
	}

	// Validate timeouts
	if err := validateServerTimeouts(config); err != nil {
		return err
	}

	// Validate connection limits
	if err := validateConnectionLimits(config); err != nil {
		return err
	}

	// Validate TLS configuration
	if err := ValidateTLSConfig(&config.TLS); err != nil {
		return customerrors.Wrap(err, "invalid TLS configuration").
			WithComponent("config")
	}

	// Validate stdio frontend configuration
	if err := ValidateStdioFrontendConfig(&config.StdioFrontend); err != nil {
		return customerrors.Wrap(err, "invalid stdio frontend configuration").
			WithComponent("config")
	}

	return nil
}

// validateServerPorts validates all port configurations.
func validateServerPorts(config *ServerConfig) error {
	if err := validatePort("port", config.Port, false); err != nil {
		return err
	}

	if err := validatePort("TCP port", config.TCPPort, true); err != nil {
		return err
	}

	if err := validatePort("metrics port", config.MetricsPort, true); err != nil {
		return err
	}

	if err := validatePort("health port", config.HealthPort, true); err != nil {
		return err
	}

	return nil
}

// validatePort validates a single port value.
func validatePort(name string, port int, allowZero bool) error {
	if port == 0 && allowZero {
		return nil
	}

	if port < 1 || port > 65535 {
		return customerrors.NewValidationError(fmt.Sprintf("%s must be between 1 and 65535, got %d", name, port))
	}

	return nil
}

// validateServerProtocol validates the server protocol.
func validateServerProtocol(protocol string) error {
	if protocol == "" {
		return nil
	}

	validProtocols := []string{"websocket", "tcp", "both", "stdio"}
	if !contains(validProtocols, protocol) {
		return customerrors.NewValidationError(
			fmt.Sprintf("invalid protocol: %s, must be one of %v", protocol, validProtocols))
	}

	return nil
}

// validateServerTimeouts validates all timeout configurations.
func validateServerTimeouts(config *ServerConfig) error {
	if config.ReadTimeout < 0 {
		return customerrors.NewValidationError(fmt.Sprintf("read timeout cannot be negative, got %d", config.ReadTimeout))
	}

	if config.WriteTimeout < 0 {
		return customerrors.NewValidationError(fmt.Sprintf("write timeout cannot be negative, got %d", config.WriteTimeout))
	}

	if config.IdleTimeout < 0 {
		return customerrors.NewValidationError(fmt.Sprintf("idle timeout cannot be negative, got %d", config.IdleTimeout))
	}

	return nil
}

// validateConnectionLimits validates connection limit configurations.
func validateConnectionLimits(config *ServerConfig) error {
	if config.MaxConnections < 0 {
		return customerrors.NewValidationError(
			fmt.Sprintf("max connections cannot be negative, got %d", config.MaxConnections))
	}

	if config.MaxConnectionsPerIP < 0 {
		return customerrors.NewValidationError(
			fmt.Sprintf("max connections per IP cannot be negative, got %d", config.MaxConnectionsPerIP))
	}

	if config.ConnectionBufferSize < 0 {
		return customerrors.NewValidationError(
			fmt.Sprintf("connection buffer size cannot be negative, got %d", config.ConnectionBufferSize))
	}

	return nil
}

// ValidateTLSConfig validates TLS configuration.
func ValidateTLSConfig(config *TLSConfig) error {
	if !config.Enabled {
		return nil // TLS disabled, no validation needed
	}

	if config.CertFile == "" {
		return customerrors.NewValidationError("cert file required when TLS is enabled").
			WithComponent("config")
	}

	if config.KeyFile == "" {
		return customerrors.NewValidationError("key file required when TLS is enabled").
			WithComponent("config")
	}

	// Validate client auth mode
	if config.ClientAuth != "" {
		validModes := []string{"none", "request", "require"}
		if !contains(validModes, config.ClientAuth) {
			return customerrors.NewValidationError(
				fmt.Sprintf("invalid client auth mode: %s, must be one of %v", config.ClientAuth, validModes))
		}
	}

	// Validate minimum TLS version
	if config.MinVersion != "" {
		validVersions := []string{"TLS1.2", "TLS1.3"}
		if !contains(validVersions, config.MinVersion) {
			return customerrors.NewValidationError(
				fmt.Sprintf("TLS version too old or invalid: %s, must be one of %v", config.MinVersion, validVersions))
		}
	}

	return nil
}

// ValidateStdioFrontendConfig validates stdio frontend configuration.
func ValidateStdioFrontendConfig(config *StdioFrontendConfig) error {
	if !config.Enabled {
		return nil
	}

	// Validate modes
	for i, mode := range config.Modes {
		if err := ValidateStdioFrontendModeConfig(&mode); err != nil {
			return customerrors.Wrap(err, fmt.Sprintf("invalid stdio mode config at index %d", i)).
				WithComponent("config")
		}
	}

	// Validate process management
	if config.Process.MaxConcurrentClients < 0 {
		return customerrors.NewValidationError("max concurrent clients cannot be negative").
			WithComponent("config")
	}

	if config.Process.ClientTimeout < 0 {
		return customerrors.NewValidationError("client timeout cannot be negative").
			WithComponent("config")
	}

	return nil
}

// ValidateStdioFrontendModeConfig validates a single stdio frontend mode.
func ValidateStdioFrontendModeConfig(config *StdioFrontendModeConfig) error {
	validTypes := []string{"unix_socket", "stdin_stdout", "named_pipes"}
	if !contains(validTypes, config.Type) {
		return customerrors.NewValidationError(
			fmt.Sprintf("invalid stdio mode type: %s, must be one of %v", config.Type, validTypes))
	}

	if config.Type == socketTypeUnix && config.Path == "" {
		return customerrors.NewEmptyFieldError("path").
			WithComponent("config")
	}

	if config.Type == "named_pipes" && config.Path == "" {
		return customerrors.NewEmptyFieldError("path").
			WithComponent("config")
	}

	return nil
}

// ValidateAuthConfig validates authentication configuration.
func ValidateAuthConfig(config *AuthConfig) error {
	validProviders := []string{"none", "jwt", "oauth2"}
	if !contains(validProviders, config.Provider) {
		return customerrors.NewValidationError(
			fmt.Sprintf("invalid auth provider: %s, must be one of %v", config.Provider, validProviders))
	}

	switch config.Provider {
	case "jwt":
		return ValidateJWTConfig(&config.JWT)
	case "oauth2":
		return ValidateOAuth2Config(&config.OAuth2)
	}

	return nil
}

// ValidateJWTConfig validates JWT configuration.
func ValidateJWTConfig(config *JWTConfig) error {
	if config.Issuer == "" {
		return customerrors.NewEmptyFieldError("issuer").
			WithComponent("config")
	}

	if config.Audience == "" {
		return customerrors.NewValidationError("JWT audience required")
	}

	if config.PublicKeyPath == "" && config.SecretKeyEnv == "" {
		return customerrors.NewValidationError("either public key path or secret key env variable required")
	}

	return nil
}

// ValidateOAuth2Config validates OAuth2 configuration.
func ValidateOAuth2Config(config *OAuth2Config) error {
	if config.ClientID == "" {
		return customerrors.NewValidationError("OAuth2 client ID required")
	}

	if config.TokenEndpoint != "" {
		if _, err := url.Parse(config.TokenEndpoint); err != nil {
			return customerrors.Wrap(err, "invalid token endpoint URL").
				WithComponent("config")
		}
	}

	if config.IntrospectEndpoint != "" {
		if _, err := url.Parse(config.IntrospectEndpoint); err != nil {
			return customerrors.Wrap(err, "invalid introspect endpoint URL").
				WithComponent("config")
		}
	}

	if config.JWKSEndpoint != "" {
		if _, err := url.Parse(config.JWKSEndpoint); err != nil {
			return customerrors.Wrap(err, "invalid JWKS endpoint URL").
				WithComponent("config")
		}
	}

	return nil
}

// ValidateSessionConfig validates session configuration.
func ValidateSessionConfig(config *SessionConfig) error {
	if config.Provider != "" {
		validProviders := []string{"redis", "memory"}
		if !contains(validProviders, config.Provider) {
			return customerrors.NewValidationError(
				fmt.Sprintf("invalid session provider: %s, must be one of %v", config.Provider, validProviders))
		}
	}

	if config.TTL < 0 {
		return customerrors.NewValidationError("session TTL cannot be negative")
	}

	if config.CleanupInterval < 0 {
		return customerrors.NewValidationError("cleanup interval cannot be negative")
	}

	return nil
}

// ValidateRoutingConfig validates routing configuration.
func ValidateRoutingConfig(config *RoutingConfig) error {
	if config.Strategy != "" {
		validStrategies := []string{"round_robin", "least_connections", "weighted", "random", "hash"}
		if !contains(validStrategies, config.Strategy) {
			return customerrors.NewValidationError(
				fmt.Sprintf("invalid routing strategy: %s, must be one of %v", config.Strategy, validStrategies))
		}
	}

	if config.HealthCheckInterval != "" {
		if _, err := time.ParseDuration(config.HealthCheckInterval); err != nil {
			return customerrors.Wrap(err, "invalid health check interval").
				WithComponent("config")
		}
	}

	return ValidateCircuitBreakerConfig(&config.CircuitBreaker)
}

// ValidateServiceDiscoveryConfig validates service discovery configuration.
func ValidateServiceDiscoveryConfig(config *ServiceDiscoveryConfig) error {
	// Check if discovery is configured
	if !isDiscoveryConfigured(config) {
		return nil // No discovery configured
	}

	// Validate provider
	provider, err := validateDiscoveryProvider(config)
	if err != nil {
		return err
	}

	// Validate refresh rate
	if err := validateRefreshRate(config.RefreshRate); err != nil {
		return err
	}

	// Validate provider-specific configuration
	return validateProviderConfig(provider, config)
}

// isDiscoveryConfigured checks if discovery is configured.
func isDiscoveryConfigured(config *ServiceDiscoveryConfig) bool {
	return config.Provider != "" || config.Mode != ""
}

// validateDiscoveryProvider validates and returns the provider name.
func validateDiscoveryProvider(config *ServiceDiscoveryConfig) (string, error) {
	provider := config.Provider
	if provider == "" {
		provider = config.Mode // Mode is alias for provider
	}

	validProviders := []string{"static", "kubernetes", "consul", "stdio", "websocket", "sse"}
	if !contains(validProviders, provider) {
		return "", fmt.Errorf("invalid discovery provider: %s, must be one of %v", provider, validProviders)
	}

	return provider, nil
}

// validateRefreshRate validates the refresh rate configuration.
func validateRefreshRate(refreshRate time.Duration) error {
	if refreshRate < 0 {
		return customerrors.NewValidationError("refresh rate cannot be negative")
	}

	return nil
}

// validateProviderConfig validates provider-specific configuration.
func validateProviderConfig(provider string, config *ServiceDiscoveryConfig) error {
	switch provider {
	case "static":
		return ValidateStaticDiscoveryConfig(&config.Static)
	case "kubernetes":
		return ValidateKubernetesDiscoveryConfig(&config.Kubernetes)
	case "stdio":
		return ValidateStdioDiscoveryConfig(&config.Stdio)
	case "websocket":
		return ValidateWebSocketDiscoveryConfig(&config.WebSocket)
	case "sse":
		return ValidateSSEDiscoveryConfig(&config.SSE)
	default:
		return nil
	}
}

// ValidateStaticDiscoveryConfig validates static discovery configuration.
func ValidateStaticDiscoveryConfig(config *StaticDiscoveryConfig) error {
	if len(config.Endpoints) == 0 {
		return customerrors.NewValidationError("static discovery requires at least one endpoint")
	}

	for serviceName, endpoints := range config.Endpoints {
		if serviceName == "" {
			return customerrors.NewValidationError("service name cannot be empty")
		}

		for i, endpoint := range endpoints {
			if endpoint.URL == "" {
				return customerrors.NewValidationError(
					fmt.Sprintf("endpoint URL cannot be empty for service %s at index %d", serviceName, i))
			}

			if _, err := url.Parse(endpoint.URL); err != nil {
				return customerrors.Wrap(err, fmt.Sprintf("invalid endpoint URL for service %s at index %d", serviceName, i)).
					WithComponent("config")
			}
		}
	}

	return nil
}

// ValidateKubernetesDiscoveryConfig validates Kubernetes discovery configuration.
func ValidateKubernetesDiscoveryConfig(config *KubernetesDiscoveryConfig) error {
	if !config.InCluster && config.ConfigPath == "" {
		return customerrors.NewValidationError("either in-cluster mode must be enabled or config path must be provided")
	}

	return nil
}

// ValidateStdioDiscoveryConfig validates stdio discovery configuration.
func ValidateStdioDiscoveryConfig(config *StdioDiscoveryConfig) error {
	for i, service := range config.Services {
		if err := ValidateStdioServiceConfig(&service); err != nil {
			return customerrors.Wrap(err, fmt.Sprintf("invalid stdio service config at index %d", i)).
				WithComponent("config")
		}
	}

	return nil
}

// ValidateStdioServiceConfig validates stdio service configuration.
func ValidateStdioServiceConfig(config *StdioServiceConfig) error {
	if config.Name == "" {
		return customerrors.NewValidationError("service name cannot be empty")
	}

	if len(config.Command) == 0 {
		return customerrors.NewValidationError("command cannot be empty")
	}

	if config.Weight < 0 {
		return customerrors.NewValidationError("weight cannot be negative")
	}

	return nil
}

// ValidateWebSocketDiscoveryConfig validates WebSocket discovery configuration.
func ValidateWebSocketDiscoveryConfig(config *WebSocketDiscoveryConfig) error {
	for i, service := range config.Services {
		if err := ValidateWebSocketServiceConfig(&service); err != nil {
			return customerrors.Wrap(err, fmt.Sprintf("invalid WebSocket service config at index %d", i)).
				WithComponent("config")
		}
	}

	return nil
}

// ValidateWebSocketServiceConfig validates WebSocket service configuration.
func ValidateWebSocketServiceConfig(config *WebSocketServiceConfig) error {
	if config.Name == "" {
		return customerrors.NewValidationError("service name cannot be empty")
	}

	if len(config.Endpoints) == 0 {
		return customerrors.NewValidationError("at least one endpoint required")
	}

	for i, endpoint := range config.Endpoints {
		if _, err := url.Parse(endpoint); err != nil {
			return customerrors.Wrap(err, fmt.Sprintf("invalid endpoint URL at index %d", i)).
				WithComponent("config")
		}
	}

	if config.Weight < 0 {
		return customerrors.NewValidationError("weight cannot be negative")
	}

	return nil
}

// ValidateSSEDiscoveryConfig validates SSE discovery configuration.
func ValidateSSEDiscoveryConfig(config *SSEDiscoveryConfig) error {
	for i, service := range config.Services {
		if err := ValidateSSEServiceConfig(&service); err != nil {
			return customerrors.Wrap(err, fmt.Sprintf("invalid SSE service config at index %d", i)).
				WithComponent("config")
		}
	}

	return nil
}

// ValidateSSEServiceConfig validates SSE service configuration.
func ValidateSSEServiceConfig(config *SSEServiceConfig) error {
	if config.Name == "" {
		return customerrors.NewValidationError("service name cannot be empty")
	}

	if config.BaseURL == "" {
		return customerrors.NewValidationError("base URL cannot be empty")
	}

	if _, err := url.Parse(config.BaseURL); err != nil {
		return customerrors.Wrap(err, "invalid base URL").
			WithComponent("config")
	}

	if config.Weight < 0 {
		return customerrors.NewValidationError("weight cannot be negative")
	}

	if config.Timeout < 0 {
		return customerrors.NewValidationError("timeout cannot be negative")
	}

	return nil
}

// ValidateCircuitBreakerConfig validates circuit breaker configuration.
func ValidateCircuitBreakerConfig(config *CircuitBreakerConfig) error {
	if !config.Enabled {
		return nil // Circuit breaker disabled, no validation needed
	}

	if config.FailureThreshold <= 0 {
		return customerrors.NewValidationError(
			fmt.Sprintf("failure threshold must be positive, got %d", config.FailureThreshold))
	}

	if config.SuccessThreshold <= 0 {
		return customerrors.NewValidationError(
			fmt.Sprintf("success threshold must be positive, got %d", config.SuccessThreshold))
	}

	if config.TimeoutSeconds < 0 {
		return customerrors.NewValidationError(fmt.Sprintf("timeout cannot be negative, got %d", config.TimeoutSeconds))
	}

	if config.MaxRequests < 0 {
		return customerrors.NewValidationError(fmt.Sprintf("max requests cannot be negative, got %d", config.MaxRequests))
	}

	if config.IntervalSeconds < 0 {
		return customerrors.NewValidationError(fmt.Sprintf("interval cannot be negative, got %d", config.IntervalSeconds))
	}

	return nil
}

// ValidateRateLimitConfig validates rate limiting configuration.
func ValidateRateLimitConfig(config *RateLimitConfig) error {
	if !config.Enabled {
		return nil // Rate limiting disabled, no validation needed
	}

	validProviders := []string{"redis", "memory"}
	if !contains(validProviders, config.Provider) {
		return customerrors.NewValidationError(
			fmt.Sprintf("invalid rate limit provider: %s, must be one of %v", config.Provider, validProviders))
	}

	if config.RequestsPerSec <= 0 {
		return customerrors.NewValidationError(
			fmt.Sprintf("requests per second must be positive, got %d", config.RequestsPerSec))
	}

	if config.Burst < 0 {
		return customerrors.NewValidationError(fmt.Sprintf("burst cannot be negative, got %d", config.Burst))
	}

	if config.Burst < config.RequestsPerSec {
		return customerrors.NewValidationError(
			fmt.Sprintf(
				"burst should be at least equal to requests per second, got burst=%d rps=%d",
				config.Burst,
				config.RequestsPerSec,
			),
		)
	}

	if config.CleanupInterval < 0 {
		return customerrors.NewValidationError(
			fmt.Sprintf("cleanup interval cannot be negative, got %d", config.CleanupInterval))
	}

	return nil
}

// ValidateMetricsConfig validates metrics configuration.
func ValidateMetricsConfig(config *MetricsConfig) error {
	if !config.Enabled {
		return nil // Metrics disabled, no validation needed
	}

	if config.Endpoint == "" {
		return customerrors.NewValidationError("endpoint required when metrics enabled")
	}

	if config.Path == "" {
		return customerrors.NewValidationError("path required when metrics enabled")
	}

	if !strings.HasPrefix(config.Path, "/") {
		return customerrors.NewValidationError("path must start with /, got: " + config.Path)
	}

	return nil
}

// ValidateLoggingConfig validates logging configuration.
func ValidateLoggingConfig(config *LoggingConfig) error {
	if config.Level != "" {
		validLevels := []string{"debug", "info", "warn", "error", "fatal", "panic"}
		if !contains(validLevels, config.Level) {
			return customerrors.NewValidationError(
				fmt.Sprintf("invalid log level: %s, must be one of %v", config.Level, validLevels))
		}
	}

	if config.Format != "" {
		validFormats := []string{"json", "text", "console"}
		if !contains(validFormats, config.Format) {
			return customerrors.NewValidationError(
				fmt.Sprintf("invalid log format: %s, must be one of %v", config.Format, validFormats))
		}
	}

	return nil
}

// ValidateTracingConfig validates tracing configuration.
func ValidateTracingConfig(config *TracingConfig) error {
	if !config.Enabled {
		return nil // Tracing disabled, no validation needed
	}

	if config.ServiceName == "" {
		return customerrors.NewValidationError("service name required when tracing enabled")
	}

	if err := validateTracingExporter(config); err != nil {
		return err
	}

	if err := validateTracingSampler(config); err != nil {
		return err
	}

	return nil
}

// validateTracingExporter validates the tracing exporter configuration.
func validateTracingExporter(config *TracingConfig) error {
	if config.ExporterType == "" {
		return nil
	}

	validExporters := []string{"otlp", "jaeger", "zipkin", "stdout"}
	if !contains(validExporters, config.ExporterType) {
		return customerrors.NewValidationError(
			fmt.Sprintf("invalid exporter type: %s, must be one of %v", config.ExporterType, validExporters))
	}

	if config.ExporterType == "otlp" && config.OTLPEndpoint != "" {
		return validateOTLPEndpoint(config.OTLPEndpoint)
	}

	return nil
}

// validateOTLPEndpoint validates the OTLP endpoint URL.
func validateOTLPEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return customerrors.Wrap(err, "invalid OTLP endpoint URL").
			WithComponent("config")
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return customerrors.NewValidationError("OTLP endpoint must be a complete URL with scheme and host")
	}

	return nil
}

// validateTracingSampler validates the tracing sampler configuration.
func validateTracingSampler(config *TracingConfig) error {
	if config.SamplerParam < 0 || config.SamplerParam > 1 {
		return customerrors.NewValidationError(
			fmt.Sprintf("sampler param must be between 0 and 1, got %f", config.SamplerParam))
	}

	if config.SamplerType != "" {
		validSamplers := []string{"const", "probabilistic", "rateLimiting", "remote"}
		if !contains(validSamplers, config.SamplerType) {
			return customerrors.NewValidationError(
				fmt.Sprintf("invalid sampler type: %s, must be one of %v", config.SamplerType, validSamplers))
		}
	}

	return nil
}

// Helper function to check if a slice contains a string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}

	return false
}
