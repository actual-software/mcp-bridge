package direct

import (
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
)

// DirectManagerConfigurationBuilder builds configuration for DirectClientManager with proper separation of concerns.
type DirectManagerConfigurationBuilder struct {
	config               DirectConfig
	logger               *zap.Logger
	defaultsApplied      bool
	performanceOptimized bool
}

// InitializeClientOrchestrator creates a new builder for DirectClientManager with descriptive naming.
func InitializeClientOrchestrator(config DirectConfig, logger *zap.Logger) *DirectManagerConfigurationBuilder {
	return &DirectManagerConfigurationBuilder{
		config: config,
		logger: logger,
	}
}

// ApplyOperationalDefaults sets default values for operational parameters.
func (b *DirectManagerConfigurationBuilder) ApplyOperationalDefaults() *DirectManagerConfigurationBuilder {
	// Timeout defaults.
	if b.config.DefaultTimeout == 0 {
		b.config.DefaultTimeout = constants.DefaultTimeout
	}

	// Connection defaults.
	if b.config.MaxConnections == 0 {
		b.config.MaxConnections = constants.DefaultMaxConnections
	}

	// Health check defaults.
	b.applyHealthCheckDefaults()

	// Auto-detection defaults.
	b.applyAutoDetectionDefaults()

	// Fallback defaults.
	b.applyFallbackDefaults()

	// Connection pool defaults.
	b.applyConnectionPoolDefaults()

	// Memory optimization defaults.
	if b.config.MemoryOptimization == (MemoryOptimizationConfig{}) {
		b.config.MemoryOptimization = DefaultMemoryOptimizationConfig()
	}

	// Timeout tuning defaults.
	if b.config.TimeoutTuning.TimeoutProfile == "" {
		b.config.TimeoutTuning = DefaultTimeoutTuningConfig()
	}

	b.defaultsApplied = true

	return b
}

// applyHealthCheckDefaults sets health check specific defaults.
func (b *DirectManagerConfigurationBuilder) applyHealthCheckDefaults() {
	if b.config.HealthCheck.Interval == 0 {
		b.config.HealthCheck.Interval = constants.DefaultHealthCheckInterval
	}

	if b.config.HealthCheck.Timeout == 0 {
		b.config.HealthCheck.Timeout = constants.DefaultHealthCheckTimeout
	}
}

// applyAutoDetectionDefaults sets auto-detection specific defaults.
func (b *DirectManagerConfigurationBuilder) applyAutoDetectionDefaults() {
	if b.config.AutoDetection.Timeout == 0 {
		b.config.AutoDetection.Timeout = constants.AutoDetectionTimeout
	}

	if len(b.config.AutoDetection.PreferredOrder) == 0 {
		b.config.AutoDetection.PreferredOrder = []string{"http", "websocket", "stdio", "sse"}
	}

	if b.config.AutoDetection.CacheTTL == 0 {
		b.config.AutoDetection.CacheTTL = constants.CleanupTickerInterval
	}
}

// applyFallbackDefaults sets fallback behavior defaults.
func (b *DirectManagerConfigurationBuilder) applyFallbackDefaults() {
	// Check if Fallback struct is completely zero-valued (unconfigured).
	if b.isFallbackUnconfigured() {
		// Only set default enabled=true if fallback is completely unconfigured.
		b.config.Fallback.Enabled = true
	}

	if b.config.Fallback.DirectTimeout == 0 {
		b.config.Fallback.DirectTimeout = DefaultDirectTimeoutSeconds * time.Second
	}

	if b.config.Fallback.MaxRetries == 0 {
		b.config.Fallback.MaxRetries = 2
	}

	if b.config.Fallback.RetryDelay == 0 {
		b.config.Fallback.RetryDelay = 1 * time.Second
	}
}

// isFallbackUnconfigured checks if fallback is completely unconfigured.
func (b *DirectManagerConfigurationBuilder) isFallbackUnconfigured() bool {
	f := b.config.Fallback

	return f.DirectTimeout == 0 &&
		f.MaxRetries == 0 &&
		f.RetryDelay == 0 &&
		len(f.DirectOnlyMethods) == 0 &&
		len(f.GatewayOnlyMethods) == 0
}

// applyConnectionPoolDefaults sets connection pool defaults.
func (b *DirectManagerConfigurationBuilder) applyConnectionPoolDefaults() {
	if b.config.ConnectionPool == (ConnectionPoolConfig{}) {
		b.config.ConnectionPool = DefaultConnectionPoolConfig()
	}

	if b.config.ConnectionPool.MaxActiveConnections == 0 {
		b.config.ConnectionPool.MaxActiveConnections = b.config.MaxConnections
	}
}

// OptimizePerformanceParameters configures performance optimization for all protocols.
func (b *DirectManagerConfigurationBuilder) OptimizePerformanceParameters() *DirectManagerConfigurationBuilder {
	setStdioPerformanceDefaults(&b.config.Stdio.Performance)
	setWebSocketPerformanceDefaults(&b.config.WebSocket.Performance)
	setHTTPPerformanceDefaults(&b.config.HTTP.Performance)
	setSSEPerformanceDefaults(&b.config.SSE.Performance)

	b.performanceOptimized = true

	return b
}

// BuildClientOrchestrator creates the final DirectClientManager with all configurations applied.

func (b *DirectManagerConfigurationBuilder) BuildClientOrchestrator() *DirectClientManager {
	// Ensure defaults are applied.
	if !b.defaultsApplied {
		b.ApplyOperationalDefaults()
	}

	// Ensure performance is optimized.
	if !b.performanceOptimized {
		b.OptimizePerformanceParameters()
	}

	// Create infrastructure components.
	infrastructure := b.createInfrastructureComponents()

	// Create adaptive components.
	adaptiveComponents := b.createAdaptiveComponents(infrastructure)

	// Build the manager.
	manager := b.assembleClientManager(infrastructure, adaptiveComponents)

	// Initialize status monitor after manager is created.
	manager.statusMonitor = NewStatusMonitor(
		manager,
		infrastructure.observability,
		b.logger,
	)

	return manager
}

// InfrastructureComponents holds core infrastructure for the manager.
type InfrastructureComponents struct {
	connectionPool  *ConnectionPool
	observability   *ObservabilityManager
	memoryOptimizer *MemoryOptimizer
}

// AdaptiveComponents holds adaptive mechanisms for the manager.
type AdaptiveComponents struct {
	timeoutTuner    *TimeoutTuner
	adaptiveTimeout *AdaptiveTimeout
	adaptiveRetry   *AdaptiveRetry
	timeoutConfig   AdaptiveTimeoutConfig
	retryConfig     AdaptiveRetryConfig
}

// createInfrastructureComponents creates core infrastructure components.
func (b *DirectManagerConfigurationBuilder) createInfrastructureComponents() InfrastructureComponents {
	return InfrastructureComponents{
		connectionPool:  NewConnectionPool(b.config.ConnectionPool, b.logger),
		observability:   NewObservabilityManager(b.config.Observability, b.logger),
		memoryOptimizer: NewMemoryOptimizer(b.config.MemoryOptimization, b.logger),
	}
}

// createAdaptiveComponents creates adaptive mechanism components.
func (b *DirectManagerConfigurationBuilder) createAdaptiveComponents(
	infra InfrastructureComponents,
) AdaptiveComponents {
	timeoutTuner := NewTimeoutTuner(b.config.TimeoutTuning, b.logger)

	// Use tuned configurations if available.
	timeoutConfig := b.config.AdaptiveTimeout
	retryConfig := b.config.AdaptiveRetry

	// Override with optimized configurations if enabled.
	if b.config.TimeoutTuning.EnableDynamicAdjustment {
		timeoutConfig = timeoutTuner.GetOptimizedTimeoutConfig()
		retryConfig = timeoutTuner.GetOptimizedRetryConfig()

		b.logOptimizationSettings()
	}

	return AdaptiveComponents{
		timeoutTuner:    timeoutTuner,
		adaptiveTimeout: NewAdaptiveTimeout(timeoutConfig, b.logger),
		adaptiveRetry:   NewAdaptiveRetry(retryConfig, b.logger),
		timeoutConfig:   timeoutConfig,
		retryConfig:     retryConfig,
	}
}

// logOptimizationSettings logs the optimization configuration being used.
func (b *DirectManagerConfigurationBuilder) logOptimizationSettings() {
	b.logger.Info("using optimized timeout and retry configurations",
		zap.String("timeout_profile", string(b.config.TimeoutTuning.TimeoutProfile)),
		zap.String("retry_profile", string(b.config.TimeoutTuning.RetryProfile)),
		zap.String("network_condition", string(b.config.TimeoutTuning.NetworkCondition)))
}

// assembleClientManager creates the final DirectClientManager with all components.
func (b *DirectManagerConfigurationBuilder) assembleClientManager(
	infra InfrastructureComponents,
	adaptive AdaptiveComponents,
) *DirectClientManager {
	return &DirectClientManager{
		config:          b.config,
		logger:          b.logger.With(zap.String("component", "direct_client_manager")),
		connectionPool:  infra.connectionPool,
		protocolCache:   make(map[string]ClientType),
		cacheTimers:     make(map[string]*time.Timer),
		adaptiveTimeout: adaptive.adaptiveTimeout,
		adaptiveRetry:   adaptive.adaptiveRetry,
		timeoutTuner:    adaptive.timeoutTuner,
		observability:   infra.observability,
		memoryOptimizer: infra.memoryOptimizer,
		shutdown:        make(chan struct{}),
		metrics: ManagerMetrics{
			LastUpdate: time.Now(),
		},
	}
}
