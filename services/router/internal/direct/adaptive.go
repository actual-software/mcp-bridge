package direct

import (
	"context"
	"sync"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
)

// AdaptiveTimeoutConfig contains configuration for adaptive timeout mechanisms.
type AdaptiveTimeoutConfig struct {
	// Base timeout values.
	BaseTimeout time.Duration `mapstructure:"base_timeout" yaml:"base_timeout"`
	MinTimeout  time.Duration `mapstructure:"min_timeout"  yaml:"min_timeout"`
	MaxTimeout  time.Duration `mapstructure:"max_timeout"  yaml:"max_timeout"`

	// Adaptation parameters.
	WindowSize     int     `mapstructure:"window_size"     yaml:"window_size"`     // Number of requests to consider
	SuccessRatio   float64 `mapstructure:"success_ratio"   yaml:"success_ratio"`   // Target success ratio
	AdaptationRate float64 `mapstructure:"adaptation_rate" yaml:"adaptation_rate"` // How quickly to adapt (0-1)

	// Learning parameters.
	EnableLearning bool          `mapstructure:"enable_learning" yaml:"enable_learning"`
	LearningPeriod time.Duration `mapstructure:"learning_period" yaml:"learning_period"`
}

// AdaptiveRetryConfig contains configuration for adaptive retry mechanisms.
type AdaptiveRetryConfig struct {
	// Base retry settings.
	MaxRetries    int           `mapstructure:"max_retries"    yaml:"max_retries"`
	BaseDelay     time.Duration `mapstructure:"base_delay"     yaml:"base_delay"`
	MaxDelay      time.Duration `mapstructure:"max_delay"      yaml:"max_delay"`
	BackoffFactor float64       `mapstructure:"backoff_factor" yaml:"backoff_factor"`

	// Adaptive behavior.
	EnableJitter         bool    `mapstructure:"enable_jitter"          yaml:"enable_jitter"`
	JitterRatio          float64 `mapstructure:"jitter_ratio"           yaml:"jitter_ratio"`
	ContextualRetries    bool    `mapstructure:"contextual_retries"     yaml:"contextual_retries"`
	FailureTypeWeighting bool    `mapstructure:"failure_type_weighting" yaml:"failure_type_weighting"`

	// Circuit breaker integration.
	CircuitBreakerEnabled bool          `mapstructure:"circuit_breaker_enabled" yaml:"circuit_breaker_enabled"`
	FailureThreshold      int           `mapstructure:"failure_threshold"       yaml:"failure_threshold"`
	RecoveryTimeout       time.Duration `mapstructure:"recovery_timeout"        yaml:"recovery_timeout"`
}

// RequestMetrics tracks metrics for a single request.
type RequestMetrics struct {
	StartTime    time.Time
	Duration     time.Duration
	Success      bool
	ErrorType    string
	RetryAttempt int
}

// AdaptiveTimeout manages adaptive timeout behavior for clients.
type AdaptiveTimeout struct {
	config AdaptiveTimeoutConfig
	logger *zap.Logger
	mu     sync.RWMutex

	// Request history for adaptation.
	requestHistory []RequestMetrics
	currentTimeout time.Duration

	// Statistics.
	totalRequests   int64
	successfulReqs  int64
	avgResponseTime time.Duration
	lastAdaptation  time.Time
}

// NewAdaptiveTimeout creates a new adaptive timeout manager.
func NewAdaptiveTimeout(config AdaptiveTimeoutConfig, logger *zap.Logger) *AdaptiveTimeout {
	// Set defaults.
	if config.BaseTimeout == 0 {
		config.BaseTimeout = defaultTimeoutSeconds * time.Second
	}

	if config.MinTimeout == 0 {
		config.MinTimeout = constants.ShortTimeout
	}

	if config.MaxTimeout == 0 {
		config.MaxTimeout = constants.LongStreamTimeout
	}

	if config.WindowSize == 0 {
		config.WindowSize = 100
	}

	if config.SuccessRatio == 0 {
		config.SuccessRatio = 0.95 // 95% success target
	}

	if config.AdaptationRate == 0 {
		config.AdaptationRate = 0.1 // 10% adaptation rate
	}

	if config.LearningPeriod == 0 {
		config.LearningPeriod = DefaultLearningPeriod
	}

	return &AdaptiveTimeout{
		config:         config,
		logger:         logger.With(zap.String("component", "adaptive_timeout")),
		requestHistory: make([]RequestMetrics, 0, config.WindowSize),
		currentTimeout: config.BaseTimeout,
		lastAdaptation: time.Now(),
	}
}

// GetTimeout returns the current adaptive timeout.
func (at *AdaptiveTimeout) GetTimeout(ctx context.Context) time.Duration {
	at.mu.RLock()
	defer at.mu.RUnlock()

	// Check if we should adapt based on learning period.
	if at.config.EnableLearning && time.Since(at.lastAdaptation) > at.config.LearningPeriod {
		go at.adaptTimeout()
	}

	// Respect context deadline if it's shorter.
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < at.currentTimeout {
			return remaining
		}
	}

	return at.currentTimeout
}

// RecordRequest records the outcome of a request for adaptive learning.
func (at *AdaptiveTimeout) RecordRequest(metrics RequestMetrics) {
	at.mu.Lock()
	defer at.mu.Unlock()

	// Add to history.
	if len(at.requestHistory) >= at.config.WindowSize {
		// Remove oldest entry.
		at.requestHistory = at.requestHistory[1:]
	}

	at.requestHistory = append(at.requestHistory, metrics)

	// Update statistics.
	at.totalRequests++
	if metrics.Success {
		at.successfulReqs++
	}

	// Update average response time.
	if at.totalRequests == 1 {
		at.avgResponseTime = metrics.Duration
	} else {
		// Simple moving average.
		at.avgResponseTime = (at.avgResponseTime + metrics.Duration) / DivisionFactorTwo
	}
}

// adaptTimeout adapts the timeout based on recent request patterns.
func (at *AdaptiveTimeout) adaptTimeout() {
	adapter := CreateTimeoutAdapter(at)
	adapter.AdaptTimeout()
}

// GetStats returns current adaptive timeout statistics.
func (at *AdaptiveTimeout) GetStats() map[string]interface{} {
	at.mu.RLock()
	defer at.mu.RUnlock()

	successRatio := float64(0)
	if at.totalRequests > 0 {
		successRatio = float64(at.successfulReqs) / float64(at.totalRequests)
	}

	return map[string]interface{}{
		"current_timeout":   at.currentTimeout,
		"total_requests":    at.totalRequests,
		"success_ratio":     successRatio,
		"avg_response_time": at.avgResponseTime,
		"window_size":       len(at.requestHistory),
		"last_adaptation":   at.lastAdaptation,
	}
}

// Helper function for absolute value of duration.
func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}

	return d
}
