package direct

import (
	"time"

	"go.uber.org/zap"
)

// TimeoutProfile represents different timeout configuration profiles.
type TimeoutProfile string

const (
	TimeoutProfileDefault     TimeoutProfile = "default"
	TimeoutProfileFast        TimeoutProfile = "fast"        // For low-latency, reliable networks
	TimeoutProfileRobust      TimeoutProfile = "robust"      // For unreliable networks
	TimeoutProfileBulk        TimeoutProfile = "bulk"        // For bulk operations
	TimeoutProfileInteractive TimeoutProfile = "interactive" // For user-interactive operations
)

// RetryProfile represents different retry configuration profiles.
type RetryProfile string

const (
	RetryProfileDefault        RetryProfile = "default"
	RetryProfileAggressive     RetryProfile = "aggressive"      // More retries, shorter delays
	RetryProfileConservative   RetryProfile = "conservative"    // Fewer retries, longer delays
	RetryProfileBackoff        RetryProfile = "backoff"         // Exponential backoff focused
	RetryProfileCircuitBreaker RetryProfile = "circuit_breaker" // Circuit breaker focused
)

// NetworkCondition represents different network conditions.
type NetworkCondition string

const (
	NetworkConditionExcellent NetworkCondition = "excellent" // <10ms latency, >99.9% reliability
	NetworkConditionGood      NetworkCondition = "good"      // <50ms latency, >99% reliability
	NetworkConditionFair      NetworkCondition = "fair"      // <200ms latency, >95% reliability
	NetworkConditionPoor      NetworkCondition = "poor"      // >200ms latency, <95% reliability
)

const (
	// MinFailureThreshold is the minimum allowable failure threshold for circuit breakers.
	MinFailureThreshold = 2
)

// TimeoutTuningConfig contains advanced timeout and retry tuning configuration.
type TimeoutTuningConfig struct {
	// Profile-based configuration.
	TimeoutProfile   TimeoutProfile   `mapstructure:"timeout_profile"   yaml:"timeout_profile"`
	RetryProfile     RetryProfile     `mapstructure:"retry_profile"     yaml:"retry_profile"`
	NetworkCondition NetworkCondition `mapstructure:"network_condition" yaml:"network_condition"`

	// Dynamic adjustment.
	EnableDynamicAdjustment bool          `mapstructure:"enable_dynamic_adjustment" yaml:"enable_dynamic_adjustment"`
	AdjustmentInterval      time.Duration `mapstructure:"adjustment_interval"       yaml:"adjustment_interval"`
	PerformanceWindow       time.Duration `mapstructure:"performance_window"        yaml:"performance_window"`

	// Protocol-specific overrides.
	ProtocolOverrides map[string]ProtocolTimeoutConfig `mapstructure:"protocol_overrides" yaml:"protocol_overrides"`

	// Advanced features.
	EnableLatencyBasedTimeout bool    `mapstructure:"enable_latency_based_timeout" yaml:"enable_latency_based_timeout"`
	LatencyMultiplier         float64 `mapstructure:"latency_multiplier"           yaml:"latency_multiplier"`
	EnableLoadBasedRetry      bool    `mapstructure:"enable_load_based_retry"      yaml:"enable_load_based_retry"`
	LoadThreshold             float64 `mapstructure:"load_threshold"               yaml:"load_threshold"`
}

// ProtocolTimeoutConfig contains protocol-specific timeout configurations.
type ProtocolTimeoutConfig struct {
	AdaptiveTimeout AdaptiveTimeoutConfig `mapstructure:"adaptive_timeout" yaml:"adaptive_timeout"`
	AdaptiveRetry   AdaptiveRetryConfig   `mapstructure:"adaptive_retry"   yaml:"adaptive_retry"`
}

// TimeoutTuner manages timeout and retry optimization.
type TimeoutTuner struct {
	config TimeoutTuningConfig
	logger *zap.Logger
}

// NewTimeoutTuner creates a new timeout tuner.
func NewTimeoutTuner(config TimeoutTuningConfig, logger *zap.Logger) *TimeoutTuner {
	// Set defaults.
	if config.TimeoutProfile == "" {
		config.TimeoutProfile = TimeoutProfileDefault
	}

	if config.RetryProfile == "" {
		config.RetryProfile = RetryProfileDefault
	}

	if config.NetworkCondition == "" {
		config.NetworkCondition = NetworkConditionGood
	}

	if config.AdjustmentInterval == 0 {
		config.AdjustmentInterval = DefaultAdjustmentInterval
	}

	if config.PerformanceWindow == 0 {
		config.PerformanceWindow = DefaultPerformanceWindow
	}

	if config.LatencyMultiplier == 0 {
		config.LatencyMultiplier = DefaultLatencyMultiplier // 3x average latency as timeout
	}

	if config.LoadThreshold == 0 {
		config.LoadThreshold = DefaultLoadThreshold // 80% load threshold
	}

	if config.ProtocolOverrides == nil {
		config.ProtocolOverrides = make(map[string]ProtocolTimeoutConfig)
	}

	return &TimeoutTuner{
		config: config,
		logger: logger.With(zap.String("component", "timeout_tuner")),
	}
}

// GetOptimizedTimeoutConfig returns an optimized timeout configuration.
func (tt *TimeoutTuner) GetOptimizedTimeoutConfig() AdaptiveTimeoutConfig {
	baseConfig := tt.getTimeoutConfigByProfile()

	// Apply network condition adjustments.
	baseConfig = tt.adjustForNetworkCondition(baseConfig)

	// Apply latency-based adjustments if enabled.
	if tt.config.EnableLatencyBasedTimeout {
		baseConfig = tt.adjustForLatency(baseConfig)
	}

	tt.logger.Debug("generated optimized timeout config",
		zap.String("profile", string(tt.config.TimeoutProfile)),
		zap.String("network_condition", string(tt.config.NetworkCondition)),
		zap.Duration("base_timeout", baseConfig.BaseTimeout),
		zap.Duration("min_timeout", baseConfig.MinTimeout),
		zap.Duration("max_timeout", baseConfig.MaxTimeout))

	return baseConfig
}

// GetOptimizedRetryConfig returns an optimized retry configuration.
func (tt *TimeoutTuner) GetOptimizedRetryConfig() AdaptiveRetryConfig {
	baseConfig := tt.getRetryConfigByProfile()

	// Apply network condition adjustments.
	baseConfig = tt.adjustRetryForNetworkCondition(baseConfig)

	// Apply load-based adjustments if enabled.
	if tt.config.EnableLoadBasedRetry {
		baseConfig = tt.adjustRetryForLoad(baseConfig)
	}

	tt.logger.Debug("generated optimized retry config",
		zap.String("profile", string(tt.config.RetryProfile)),
		zap.String("network_condition", string(tt.config.NetworkCondition)),
		zap.Int("max_retries", baseConfig.MaxRetries),
		zap.Duration("base_delay", baseConfig.BaseDelay),
		zap.Float64("backoff_factor", baseConfig.BackoffFactor))

	return baseConfig
}

// GetProtocolOptimizedConfig returns protocol-specific optimized configuration.
func (tt *TimeoutTuner) GetProtocolOptimizedConfig(protocol string) ProtocolTimeoutConfig {
	// Check for protocol-specific override.
	if override, exists := tt.config.ProtocolOverrides[protocol]; exists {
		return override
	}

	// Generate protocol-optimized config.
	timeoutConfig := tt.GetOptimizedTimeoutConfig()
	retryConfig := tt.GetOptimizedRetryConfig()

	// Apply protocol-specific adjustments.
	switch protocol {
	case SchemeStdio:
		timeoutConfig = tt.adjustForStdio(timeoutConfig)
		retryConfig = tt.adjustRetryForStdio(retryConfig)
	case SchemeHTTP, SchemeHTTPS:
		timeoutConfig = tt.adjustForHTTP(timeoutConfig)
		retryConfig = tt.adjustRetryForHTTP(retryConfig)
	case SchemeWebSocket, SchemeWebSocketSecure:
		timeoutConfig = tt.adjustForWebSocket(timeoutConfig)
		retryConfig = tt.adjustRetryForWebSocket(retryConfig)
	case "sse":
		timeoutConfig = tt.adjustForSSE(timeoutConfig)
		retryConfig = tt.adjustRetryForSSE(retryConfig)
	}

	return ProtocolTimeoutConfig{
		AdaptiveTimeout: timeoutConfig,
		AdaptiveRetry:   retryConfig,
	}
}

// getTimeoutConfigByProfile returns base timeout config for the selected profile.
func (tt *TimeoutTuner) getTimeoutConfigByProfile() AdaptiveTimeoutConfig {
	switch tt.config.TimeoutProfile {
	case TimeoutProfileFast:
		return AdaptiveTimeoutConfig{
			BaseTimeout:    FastBaseTimeout,
			MinTimeout:     FastMinTimeout,
			MaxTimeout:     FastMaxTimeout,
			WindowSize:     FastWindowSize,
			SuccessRatio:   FastSuccessRatio,
			AdaptationRate: FastAdaptationRate,
			EnableLearning: true,
			LearningPeriod: FastLearningPeriod,
		}
	case TimeoutProfileRobust:
		return AdaptiveTimeoutConfig{
			BaseTimeout:    RobustBaseTimeout,
			MinTimeout:     RobustMinTimeout,
			MaxTimeout:     RobustMaxTimeout,
			WindowSize:     RobustWindowSize,
			SuccessRatio:   RobustSuccessRatio,
			AdaptationRate: RobustAdaptationRate,
			EnableLearning: true,
			LearningPeriod: RobustLearningPeriod,
		}
	case TimeoutProfileBulk:
		return AdaptiveTimeoutConfig{
			BaseTimeout:    BulkBaseTimeout,
			MinTimeout:     BulkMinTimeout,
			MaxTimeout:     BulkMaxTimeout,
			WindowSize:     BulkWindowSize,
			SuccessRatio:   BulkSuccessRatio,
			AdaptationRate: BulkAdaptationRate,
			EnableLearning: true,
			LearningPeriod: BulkLearningPeriod,
		}
	case TimeoutProfileInteractive:
		return AdaptiveTimeoutConfig{
			BaseTimeout:    InteractiveBaseTimeout,
			MinTimeout:     InteractiveMinTimeout,
			MaxTimeout:     InteractiveMaxTimeout,
			WindowSize:     InteractiveWindowSize,
			SuccessRatio:   InteractiveSuccessRatio,
			AdaptationRate: InteractiveAdaptationRate,
			EnableLearning: true,
			LearningPeriod: InteractiveLearningPeriod,
		}
	case TimeoutProfileDefault:
		return AdaptiveTimeoutConfig{
			BaseTimeout:    DefaultBaseTimeout,
			MinTimeout:     DefaultMinTimeout,
			MaxTimeout:     DefaultMaxTimeout,
			WindowSize:     DefaultWindowSize,
			SuccessRatio:   DefaultSuccessRatio,
			AdaptationRate: DefaultAdaptationRate,
			EnableLearning: true,
			LearningPeriod: DefaultLearningPeriod,
		}
	}

	// This should never be reached due to exhaustive cases above.
	return AdaptiveTimeoutConfig{}
}

// getRetryConfigByProfile returns base retry config for the selected profile.
func (tt *TimeoutTuner) getRetryConfigByProfile() AdaptiveRetryConfig {
	switch tt.config.RetryProfile {
	case RetryProfileAggressive:
		return tt.getAggressiveRetryConfig()
	case RetryProfileConservative:
		return tt.getConservativeRetryConfig()
	case RetryProfileBackoff:
		return tt.getBackoffRetryConfig()
	case RetryProfileCircuitBreaker:
		return tt.getCircuitBreakerRetryConfig()
	case RetryProfileDefault:
		return tt.getDefaultRetryConfig()
	}

	// This should never be reached due to exhaustive cases above.
	return AdaptiveRetryConfig{}
}

func (tt *TimeoutTuner) getAggressiveRetryConfig() AdaptiveRetryConfig {
	return AdaptiveRetryConfig{
		MaxRetries:            AggressiveMaxRetries,
		BaseDelay:             AggressiveBaseDelay,
		MaxDelay:              AggressiveMaxDelay,
		BackoffFactor:         AggressiveBackoffFactor,
		EnableJitter:          true,
		JitterRatio:           AggressiveJitterRatio,
		ContextualRetries:     true,
		FailureTypeWeighting:  true,
		CircuitBreakerEnabled: true,
		FailureThreshold:      AggressiveFailureThreshold,
		RecoveryTimeout:       AggressiveRecoveryTimeout,
	}
}

func (tt *TimeoutTuner) getConservativeRetryConfig() AdaptiveRetryConfig {
	return AdaptiveRetryConfig{
		MaxRetries:            ConservativeMaxRetries,
		BaseDelay:             ConservativeBaseDelay,
		MaxDelay:              ConservativeMaxDelay,
		BackoffFactor:         ConservativeBackoffFactor,
		EnableJitter:          true,
		JitterRatio:           ConservativeJitterRatio,
		ContextualRetries:     true,
		FailureTypeWeighting:  true,
		CircuitBreakerEnabled: true,
		FailureThreshold:      ConservativeFailureThreshold,
		RecoveryTimeout:       ConservativeRecoveryTimeout,
	}
}

func (tt *TimeoutTuner) getBackoffRetryConfig() AdaptiveRetryConfig {
	return AdaptiveRetryConfig{
		MaxRetries:            BackoffMaxRetries,
		BaseDelay:             BackoffBaseDelay,
		MaxDelay:              BackoffMaxDelay,
		BackoffFactor:         BackoffBackoffFactor,
		EnableJitter:          true,
		JitterRatio:           BackoffJitterRatio,
		ContextualRetries:     true,
		FailureTypeWeighting:  true,
		CircuitBreakerEnabled: false,
		FailureThreshold:      BackoffFailureThreshold,
		RecoveryTimeout:       BackoffRecoveryTimeout,
	}
}

func (tt *TimeoutTuner) getCircuitBreakerRetryConfig() AdaptiveRetryConfig {
	return AdaptiveRetryConfig{
		MaxRetries:            CircuitBreakerMaxRetries,
		BaseDelay:             CircuitBreakerBaseDelay,
		MaxDelay:              CircuitBreakerMaxDelay,
		BackoffFactor:         CircuitBreakerBackoffFactor,
		EnableJitter:          true,
		JitterRatio:           CircuitBreakerJitterRatio,
		ContextualRetries:     true,
		FailureTypeWeighting:  true,
		CircuitBreakerEnabled: true,
		FailureThreshold:      CircuitBreakerFailureThreshold,
		RecoveryTimeout:       CircuitBreakerRecoveryTimeout,
	}
}

func (tt *TimeoutTuner) getDefaultRetryConfig() AdaptiveRetryConfig {
	return AdaptiveRetryConfig{
		MaxRetries:            DefaultRetryMaxRetries,
		BaseDelay:             DefaultRetryBaseDelay,
		MaxDelay:              DefaultRetryMaxDelay,
		BackoffFactor:         DefaultRetryBackoffFactor,
		EnableJitter:          true,
		JitterRatio:           DefaultRetryJitterRatio,
		ContextualRetries:     true,
		FailureTypeWeighting:  true,
		CircuitBreakerEnabled: true,
		FailureThreshold:      DefaultRetryFailureThreshold,
		RecoveryTimeout:       DefaultRetryRecoveryTimeout,
	}
}

// adjustForNetworkCondition adjusts timeout config based on network conditions.
func (tt *TimeoutTuner) adjustForNetworkCondition(config AdaptiveTimeoutConfig) AdaptiveTimeoutConfig {
	switch tt.config.NetworkCondition {
	case NetworkConditionExcellent:
		config.BaseTimeout = time.Duration(float64(config.BaseTimeout) * ExcellentTimeoutMultiplier)
		config.MinTimeout = time.Duration(float64(config.MinTimeout) * ExcellentTimeoutMultiplier)
		config.AdaptationRate *= ExcellentAdaptationMultiplier
	case NetworkConditionGood:
		// Keep defaults.
	case NetworkConditionFair:
		config.BaseTimeout = time.Duration(float64(config.BaseTimeout) * PoorTimeoutMultiplier)
		config.MaxTimeout = time.Duration(float64(config.MaxTimeout) * PoorMaxTimeoutMultiplier)
		config.AdaptationRate *= PoorAdaptationMultiplier
	case NetworkConditionPoor:
		config.BaseTimeout = time.Duration(float64(config.BaseTimeout) * VeryPoorTimeoutMultiplier)
		config.MaxTimeout = time.Duration(float64(config.MaxTimeout) * VeryPoorMaxTimeoutMultiplier)
		config.MinTimeout = time.Duration(float64(config.MinTimeout) * VeryPoorMinTimeoutMultiplier)
		config.AdaptationRate *= VeryPoorAdaptationMultiplier
		config.LearningPeriod *= VeryPoorLearningMultiplier
	}

	return config
}

// adjustRetryForNetworkCondition adjusts retry config based on network conditions.
func (tt *TimeoutTuner) adjustRetryForNetworkCondition(config AdaptiveRetryConfig) AdaptiveRetryConfig {
	switch tt.config.NetworkCondition {
	case NetworkConditionExcellent:
		config.MaxRetries = max(1, config.MaxRetries-ExcellentRetryReduction)
		config.BaseDelay = time.Duration(float64(config.BaseDelay) * ExcellentDelayMultiplier)
	case NetworkConditionGood:
		// Keep defaults.
	case NetworkConditionFair:
		config.MaxRetries += PoorRetryIncrease
		config.BaseDelay = time.Duration(float64(config.BaseDelay) * PoorDelayMultiplier)
		config.MaxDelay = time.Duration(float64(config.MaxDelay) * PoorMaxDelayMultiplier)
	case NetworkConditionPoor:
		config.MaxRetries += VeryPoorRetryIncrease
		config.BaseDelay = time.Duration(float64(config.BaseDelay) * VeryPoorDelayMultiplier)
		config.MaxDelay = time.Duration(float64(config.MaxDelay) * VeryPoorMaxDelayMultiplier)
		config.FailureThreshold = max(MinFailureThreshold, config.FailureThreshold-VeryPoorThresholdReduction)
		config.RecoveryTimeout = time.Duration(float64(config.RecoveryTimeout) * VeryPoorRecoveryMultiplier)
	}

	return config
}

// Protocol-specific timeout adjustments.
func (tt *TimeoutTuner) adjustForStdio(config AdaptiveTimeoutConfig) AdaptiveTimeoutConfig {
	// Stdio processes can be slower to start.
	config.BaseTimeout = time.Duration(float64(config.BaseTimeout) * SafetyTimeoutFactor)
	if config.MinTimeout < MinSafeTimeout {
		config.MinTimeout = MinSafeTimeout
	}

	return config
}

func (tt *TimeoutTuner) adjustForHTTP(config AdaptiveTimeoutConfig) AdaptiveTimeoutConfig {
	// HTTP generally has predictable performance.
	config.AdaptationRate *= SafetyAdaptationRate

	return config
}

func (tt *TimeoutTuner) adjustForWebSocket(config AdaptiveTimeoutConfig) AdaptiveTimeoutConfig {
	// WebSocket connections benefit from longer learning.
	config.LearningPeriod = time.Duration(float64(config.LearningPeriod) * SafetyLearningFactor)

	return config
}

func (tt *TimeoutTuner) adjustForSSE(config AdaptiveTimeoutConfig) AdaptiveTimeoutConfig {
	// SSE streams can have long-running connections.
	config.BaseTimeout = time.Duration(float64(config.BaseTimeout) * SafetyLearningFactor)
	config.MaxTimeout = time.Duration(float64(config.MaxTimeout) * VeryPoorTimeoutMultiplier)

	return config
}

// Protocol-specific retry adjustments.
func (tt *TimeoutTuner) adjustRetryForStdio(config AdaptiveRetryConfig) AdaptiveRetryConfig {
	// Stdio failures are often fatal.
	config.MaxRetries = max(1, config.MaxRetries-1)
	config.CircuitBreakerEnabled = false // Process management handles failures

	return config
}

func (tt *TimeoutTuner) adjustRetryForHTTP(config AdaptiveRetryConfig) AdaptiveRetryConfig {
	// HTTP retries are generally safe.
	config.ContextualRetries = true
	config.FailureTypeWeighting = true

	return config
}

func (tt *TimeoutTuner) adjustRetryForWebSocket(config AdaptiveRetryConfig) AdaptiveRetryConfig {
	// WebSocket connection failures need more careful handling.
	config.BaseDelay = time.Duration(float64(config.BaseDelay) * SafetyLearningFactor)
	config.CircuitBreakerEnabled = true
	config.FailureThreshold = max(MinFailureThreshold, config.FailureThreshold-1)

	return config
}

func (tt *TimeoutTuner) adjustRetryForSSE(config AdaptiveRetryConfig) AdaptiveRetryConfig {
	// SSE connections need fast reconnection.
	config.BaseDelay = time.Duration(float64(config.BaseDelay) * ExcellentDelayMultiplier)
	config.MaxRetries += 1

	return config
}

// Advanced adjustment methods.
func (tt *TimeoutTuner) adjustForLatency(config AdaptiveTimeoutConfig) AdaptiveTimeoutConfig {
	// This would use actual measured latency data.
	// For now, we'll use the multiplier with a reasonable assumption.
	estimatedLatency := DefaultEstimatedLatency
	calculatedTimeout := time.Duration(float64(estimatedLatency) * tt.config.LatencyMultiplier)

	if calculatedTimeout > config.MinTimeout && calculatedTimeout < config.MaxTimeout {
		config.BaseTimeout = calculatedTimeout
	}

	return config
}

func (tt *TimeoutTuner) adjustRetryForLoad(config AdaptiveRetryConfig) AdaptiveRetryConfig {
	// This would use actual system load data.
	// For now, we'll adjust based on configured threshold.
	if tt.config.LoadThreshold > HighLoadThreshold {
		config.MaxRetries = max(1, config.MaxRetries-HighLoadRetryReduction)
		config.BaseDelay = time.Duration(float64(config.BaseDelay) * HighLoadDelayMultiplier)
	} else if tt.config.LoadThreshold < LowLoadThreshold {
		config.MaxRetries += LowLoadRetryIncrease
		config.BaseDelay = time.Duration(float64(config.BaseDelay) * LowLoadDelayMultiplier)
	}

	return config
}

// DefaultTimeoutTuningConfig returns sensible defaults for timeout tuning.
func DefaultTimeoutTuningConfig() TimeoutTuningConfig {
	return TimeoutTuningConfig{
		TimeoutProfile:            TimeoutProfileDefault,
		RetryProfile:              RetryProfileDefault,
		NetworkCondition:          NetworkConditionGood,
		EnableDynamicAdjustment:   true,
		AdjustmentInterval:        DefaultAdjustmentInterval,
		PerformanceWindow:         DefaultPerformanceWindow,
		ProtocolOverrides:         make(map[string]ProtocolTimeoutConfig),
		EnableLatencyBasedTimeout: true,
		LatencyMultiplier:         DefaultLatencyMultiplier,
		EnableLoadBasedRetry:      false, // Disabled by default as it requires load monitoring
		LoadThreshold:             DefaultLoadThreshold,
	}
}

// Helper functions.
func max(a, b int) int {
	if a > b {
		return a
	}

	return b
}
