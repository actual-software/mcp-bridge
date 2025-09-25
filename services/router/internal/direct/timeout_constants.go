// Package direct contains timeout and retry configuration constants.
package direct

import "time"

// Timeout profile constants.
const (
	// Fast profile timeouts.
	FastBaseTimeout    = 5 * time.Second
	FastMinTimeout     = 500 * time.Millisecond
	FastMaxTimeout     = 30 * time.Second
	FastWindowSize     = 50
	FastSuccessRatio   = 0.98
	FastAdaptationRate = 0.15
	FastLearningPeriod = 2 * time.Minute

	// Robust profile timeouts.
	RobustBaseTimeout    = 60 * time.Second
	RobustMinTimeout     = 10 * time.Second
	RobustMaxTimeout     = 300 * time.Second
	RobustWindowSize     = 200
	RobustSuccessRatio   = 0.90
	RobustAdaptationRate = 0.05
	RobustLearningPeriod = 10 * time.Minute

	// Bulk profile timeouts.
	BulkBaseTimeout    = 120 * time.Second
	BulkMinTimeout     = 30 * time.Second
	BulkMaxTimeout     = 600 * time.Second
	BulkWindowSize     = 100
	BulkSuccessRatio   = 0.92
	BulkAdaptationRate = 0.08
	BulkLearningPeriod = 15 * time.Minute

	// Interactive profile timeouts.
	InteractiveBaseTimeout    = 10 * time.Second
	InteractiveMinTimeout     = 1 * time.Second
	InteractiveMaxTimeout     = 45 * time.Second
	InteractiveWindowSize     = 30
	InteractiveSuccessRatio   = 0.95
	InteractiveAdaptationRate = 0.12
	InteractiveLearningPeriod = 3 * time.Minute

	// Default profile timeouts.
	DefaultBaseTimeout    = 30 * time.Second
	DefaultMinTimeout     = 2 * time.Second
	DefaultMaxTimeout     = 120 * time.Second
	DefaultWindowSize     = 100
	DefaultSuccessRatio   = 0.95
	DefaultAdaptationRate = 0.10
)

// Retry profile constants.
const (
	// Aggressive retry settings.
	AggressiveMaxRetries       = 5
	AggressiveBaseDelay        = 50 * time.Millisecond
	AggressiveMaxDelay         = 10 * time.Second
	AggressiveBackoffFactor    = 1.5
	AggressiveJitterRatio      = 0.2
	AggressiveFailureThreshold = 8
	AggressiveRecoveryTimeout  = 30 * time.Second

	// Conservative retry settings.
	ConservativeMaxRetries       = 2
	ConservativeBaseDelay        = 500 * time.Millisecond
	ConservativeMaxDelay         = 60 * time.Second
	ConservativeBackoffFactor    = 2.5
	ConservativeJitterRatio      = 0.1
	ConservativeFailureThreshold = 3
	ConservativeRecoveryTimeout  = 120 * time.Second

	// Backoff retry settings.
	BackoffMaxRetries       = 4
	BackoffBaseDelay        = 200 * time.Millisecond
	BackoffMaxDelay         = 30 * time.Second
	BackoffBackoffFactor    = 3.0
	BackoffJitterRatio      = 0.15
	BackoffFailureThreshold = 5
	BackoffRecoveryTimeout  = 60 * time.Second

	// Circuit breaker retry settings.
	CircuitBreakerMaxRetries       = 3
	CircuitBreakerBaseDelay        = 100 * time.Millisecond
	CircuitBreakerMaxDelay         = 15 * time.Second
	CircuitBreakerBackoffFactor    = 2.0
	CircuitBreakerJitterRatio      = 0.1
	CircuitBreakerFailureThreshold = 3
	CircuitBreakerRecoveryTimeout  = 45 * time.Second

	// Default retry settings.
	DefaultRetryMaxRetries       = 3
	DefaultRetryBaseDelay        = 100 * time.Millisecond
	DefaultRetryMaxDelay         = 30 * time.Second
	DefaultRetryBackoffFactor    = 2.0
	DefaultRetryJitterRatio      = 0.1
	DefaultRetryFailureThreshold = 5
	DefaultRetryRecoveryTimeout  = 60 * time.Second
)

// Network condition adjustments.
const (
	// Excellent network adjustments.
	ExcellentTimeoutMultiplier    = 0.7
	ExcellentAdaptationMultiplier = 1.2

	// Poor network adjustments.
	PoorTimeoutMultiplier    = 1.5
	PoorMaxTimeoutMultiplier = 1.3
	PoorAdaptationMultiplier = 0.8

	// Very poor network adjustments.
	VeryPoorTimeoutMultiplier    = 2.0
	VeryPoorMaxTimeoutMultiplier = 1.5
	VeryPoorMinTimeoutMultiplier = 1.5
	VeryPoorAdaptationMultiplier = 0.5
	VeryPoorLearningMultiplier   = 2

	// Retry adjustments.
	ExcellentRetryReduction    = 1
	ExcellentDelayMultiplier   = 0.8
	PoorRetryIncrease          = 1
	PoorDelayMultiplier        = 1.2
	PoorMaxDelayMultiplier     = 1.3
	VeryPoorRetryIncrease      = 2
	VeryPoorDelayMultiplier    = 1.5
	VeryPoorMaxDelayMultiplier = 1.5
	VeryPoorThresholdReduction = 2
	VeryPoorRecoveryMultiplier = 1.5
)

// Dynamic adjustment defaults.
const (
	DefaultAdjustmentInterval = 5 * time.Minute
	DefaultPerformanceWindow  = 15 * time.Minute
	DefaultLatencyMultiplier  = 3.0
	DefaultLoadThreshold      = 0.8

	// Load-based adjustments.
	HighLoadThreshold       = 0.8
	HighLoadRetryReduction  = 1
	HighLoadDelayMultiplier = 1.3
	LowLoadThreshold        = 0.3
	LowLoadRetryIncrease    = 1
	LowLoadDelayMultiplier  = 0.8

	// Safety minimums.
	MinSafeTimeout       = 2 * time.Second
	SafetyTimeoutFactor  = 1.2
	SafetyAdaptationRate = 1.1

	// Default latency estimate.
	DefaultEstimatedLatency = 50 * time.Millisecond
)
