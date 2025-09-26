package direct

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestTimeoutTuner_Basic(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultTimeoutTuningConfig()

	tuner := NewTimeoutTuner(config, logger)
	require.NotNil(t, tuner)

	// Test default config values.
	assert.Equal(t, TimeoutProfileDefault, tuner.config.TimeoutProfile)
	assert.Equal(t, RetryProfileDefault, tuner.config.RetryProfile)
	assert.Equal(t, NetworkConditionGood, tuner.config.NetworkCondition)
	assert.True(t, tuner.config.EnableDynamicAdjustment)
}

func TestTimeoutTuner_TimeoutProfiles(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	testCases := []struct {
		name    string
		profile TimeoutProfile
		minBase time.Duration
		maxBase time.Duration
	}{
		{"Fast", TimeoutProfileFast, 1 * time.Second, 10 * time.Second},
		{"Default", TimeoutProfileDefault, 10 * time.Second, 60 * time.Second},
		{"Robust", TimeoutProfileRobust, 30 * time.Second, 120 * time.Second},
		{"Bulk", TimeoutProfileBulk, 60 * time.Second, 300 * time.Second},
		{"Interactive", TimeoutProfileInteractive, 5 * time.Second, 30 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := DefaultTimeoutTuningConfig()
			config.TimeoutProfile = tc.profile

			tuner := NewTimeoutTuner(config, logger)
			timeoutConfig := tuner.GetOptimizedTimeoutConfig()

			assert.GreaterOrEqual(t, timeoutConfig.BaseTimeout, tc.minBase)
			assert.LessOrEqual(t, timeoutConfig.BaseTimeout, tc.maxBase)
			assert.Greater(t, timeoutConfig.MaxTimeout, timeoutConfig.BaseTimeout)
			assert.Less(t, timeoutConfig.MinTimeout, timeoutConfig.BaseTimeout)
			assert.True(t, timeoutConfig.EnableLearning)
		})
	}
}

func TestTimeoutTuner_RetryProfiles(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	testCases := []struct {
		name       string
		profile    RetryProfile
		minRetries int
		maxRetries int
	}{
		{"Conservative", RetryProfileConservative, 1, 3},
		{"Default", RetryProfileDefault, 2, 4},
		{"Aggressive", RetryProfileAggressive, 3, 6},
		{"Backoff", RetryProfileBackoff, 3, 5},
		{"CircuitBreaker", RetryProfileCircuitBreaker, 2, 4},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := DefaultTimeoutTuningConfig()
			config.RetryProfile = tc.profile

			tuner := NewTimeoutTuner(config, logger)
			retryConfig := tuner.GetOptimizedRetryConfig()

			assert.GreaterOrEqual(t, retryConfig.MaxRetries, tc.minRetries)
			assert.LessOrEqual(t, retryConfig.MaxRetries, tc.maxRetries)
			assert.Greater(t, retryConfig.BackoffFactor, 1.0)
			assert.Greater(t, retryConfig.MaxDelay, retryConfig.BaseDelay)
			assert.True(t, retryConfig.EnableJitter)
		})
	}
}

func TestTimeoutTuner_NetworkConditions(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	baseConfig := DefaultTimeoutTuningConfig()
	baseTuner := NewTimeoutTuner(baseConfig, logger)
	baseTimeout := baseTuner.GetOptimizedTimeoutConfig()
	baseRetry := baseTuner.GetOptimizedRetryConfig()

	testCases := []struct {
		name          string
		condition     NetworkCondition
		timeoutFactor float64
		retryFactor   float64
	}{
		{"Excellent", NetworkConditionExcellent, 0.7, 0.8},
		{"Good", NetworkConditionGood, 1.0, 1.0},
		{"Fair", NetworkConditionFair, 1.5, 1.2},
		{"Poor", NetworkConditionPoor, 2.0, 1.5},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := DefaultTimeoutTuningConfig()
			config.NetworkCondition = tc.condition

			tuner := NewTimeoutTuner(config, logger)
			timeoutConfig := tuner.GetOptimizedTimeoutConfig()
			retryConfig := tuner.GetOptimizedRetryConfig()

			// Verify timeout adjustments.
			switch tc.condition {
			case NetworkConditionGood:
				// Good should be close to baseline.
				assert.InDelta(t, baseTimeout.BaseTimeout, timeoutConfig.BaseTimeout, float64(5*time.Second))
			case NetworkConditionExcellent:
				// Excellent should be faster.
				assert.Less(t, timeoutConfig.BaseTimeout, baseTimeout.BaseTimeout)
			case NetworkConditionFair:
				// Fair should be slower.
				assert.Greater(t, timeoutConfig.BaseTimeout, baseTimeout.BaseTimeout)
			case NetworkConditionPoor:
				// Poor should be slower.
				assert.Greater(t, timeoutConfig.BaseTimeout, baseTimeout.BaseTimeout)
			}

			// Verify retry adjustments.
			if tc.condition == NetworkConditionPoor {
				assert.GreaterOrEqual(t, retryConfig.MaxRetries, baseRetry.MaxRetries)
			}
		})
	}
}

func TestTimeoutTuner_ProtocolOptimization(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultTimeoutTuningConfig()
	tuner := NewTimeoutTuner(config, logger)

	protocols := []string{"stdio", "http", "https", "ws", "wss", "sse"}

	for _, protocol := range protocols {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()

			protocolConfig := tuner.GetProtocolOptimizedConfig(protocol)

			assert.Positive(t, protocolConfig.AdaptiveTimeout.BaseTimeout)
			assert.Greater(t, protocolConfig.AdaptiveTimeout.MaxTimeout, protocolConfig.AdaptiveTimeout.BaseTimeout)
			assert.Less(t, protocolConfig.AdaptiveTimeout.MinTimeout, protocolConfig.AdaptiveTimeout.BaseTimeout)

			assert.Positive(t, protocolConfig.AdaptiveRetry.MaxRetries)
			assert.Greater(t, protocolConfig.AdaptiveRetry.BackoffFactor, 1.0)
			assert.Greater(t, protocolConfig.AdaptiveRetry.MaxDelay, protocolConfig.AdaptiveRetry.BaseDelay)
		})
	}
}

func TestTimeoutTuner_ProtocolSpecificAdjustments(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultTimeoutTuningConfig()
	tuner := NewTimeoutTuner(config, logger)

	baseConfig := tuner.GetOptimizedTimeoutConfig()
	baseRetryConfig := tuner.GetOptimizedRetryConfig()

	// Test stdio adjustments.
	stdioConfig := tuner.GetProtocolOptimizedConfig("stdio")
	assert.GreaterOrEqual(t, stdioConfig.AdaptiveTimeout.BaseTimeout, baseConfig.BaseTimeout)
	assert.GreaterOrEqual(t, stdioConfig.AdaptiveTimeout.MinTimeout, 2*time.Second)
	assert.LessOrEqual(t, stdioConfig.AdaptiveRetry.MaxRetries, baseRetryConfig.MaxRetries)
	assert.False(t, stdioConfig.AdaptiveRetry.CircuitBreakerEnabled)

	// Test HTTP adjustments.
	httpConfig := tuner.GetProtocolOptimizedConfig("http")
	assert.True(t, httpConfig.AdaptiveRetry.ContextualRetries)
	assert.True(t, httpConfig.AdaptiveRetry.FailureTypeWeighting)

	// Test WebSocket adjustments.
	wsConfig := tuner.GetProtocolOptimizedConfig("ws")
	assert.Greater(t, wsConfig.AdaptiveTimeout.LearningPeriod, baseConfig.LearningPeriod)
	assert.True(t, wsConfig.AdaptiveRetry.CircuitBreakerEnabled)
	assert.LessOrEqual(t, wsConfig.AdaptiveRetry.FailureThreshold, baseRetryConfig.FailureThreshold)

	// Test SSE adjustments.
	sseConfig := tuner.GetProtocolOptimizedConfig("sse")
	assert.GreaterOrEqual(t, sseConfig.AdaptiveTimeout.BaseTimeout, baseConfig.BaseTimeout)
	assert.GreaterOrEqual(t, sseConfig.AdaptiveTimeout.MaxTimeout, baseConfig.MaxTimeout)
	assert.GreaterOrEqual(t, sseConfig.AdaptiveRetry.MaxRetries, baseRetryConfig.MaxRetries)
}

func TestTimeoutTuner_ProtocolOverrides(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultTimeoutTuningConfig()

	// Set up protocol override.
	customTimeout := AdaptiveTimeoutConfig{
		BaseTimeout:    15 * time.Second,
		MinTimeout:     3 * time.Second,
		MaxTimeout:     60 * time.Second,
		WindowSize:     25,
		SuccessRatio:   0.90,
		AdaptationRate: 0.15,
		EnableLearning: false,
		LearningPeriod: 2 * time.Minute,
	}

	customRetry := AdaptiveRetryConfig{
		MaxRetries:            2,
		BaseDelay:             250 * time.Millisecond,
		MaxDelay:              45 * time.Second,
		BackoffFactor:         2.5,
		EnableJitter:          false,
		JitterRatio:           0.05,
		ContextualRetries:     false,
		FailureTypeWeighting:  false,
		CircuitBreakerEnabled: false,
		FailureThreshold:      8,
		RecoveryTimeout:       90 * time.Second,
	}

	config.ProtocolOverrides = map[string]ProtocolTimeoutConfig{
		"custom": {
			AdaptiveTimeout: customTimeout,
			AdaptiveRetry:   customRetry,
		},
	}

	tuner := NewTimeoutTuner(config, logger)

	// Test that override is used.
	overrideConfig := tuner.GetProtocolOptimizedConfig("custom")
	assert.Equal(t, customTimeout.BaseTimeout, overrideConfig.AdaptiveTimeout.BaseTimeout)
	assert.Equal(t, customTimeout.MinTimeout, overrideConfig.AdaptiveTimeout.MinTimeout)
	assert.Equal(t, customTimeout.MaxTimeout, overrideConfig.AdaptiveTimeout.MaxTimeout)
	assert.Equal(t, customTimeout.EnableLearning, overrideConfig.AdaptiveTimeout.EnableLearning)

	assert.Equal(t, customRetry.MaxRetries, overrideConfig.AdaptiveRetry.MaxRetries)
	assert.Equal(t, customRetry.BaseDelay, overrideConfig.AdaptiveRetry.BaseDelay)
	assert.Equal(t, customRetry.EnableJitter, overrideConfig.AdaptiveRetry.EnableJitter)
	assert.Equal(t, customRetry.CircuitBreakerEnabled, overrideConfig.AdaptiveRetry.CircuitBreakerEnabled)

	// Test that non-override protocol uses defaults.
	defaultConfig := tuner.GetProtocolOptimizedConfig("http")
	assert.NotEqual(t, customTimeout.BaseTimeout, defaultConfig.AdaptiveTimeout.BaseTimeout)
}

func TestTimeoutTuner_LatencyBasedTimeout(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	// Test with latency-based timeout enabled.
	config := DefaultTimeoutTuningConfig()
	config.EnableLatencyBasedTimeout = true
	config.LatencyMultiplier = 4.0

	tuner := NewTimeoutTuner(config, logger)
	timeoutConfig := tuner.GetOptimizedTimeoutConfig()

	// Should have reasonable timeout values.
	assert.Positive(t, timeoutConfig.BaseTimeout)
	assert.Greater(t, timeoutConfig.MaxTimeout, timeoutConfig.BaseTimeout)
	assert.Less(t, timeoutConfig.MinTimeout, timeoutConfig.BaseTimeout)

	// Test with latency-based timeout disabled.
	config.EnableLatencyBasedTimeout = false
	tuner2 := NewTimeoutTuner(config, logger)
	timeoutConfig2 := tuner2.GetOptimizedTimeoutConfig()

	// Should still have reasonable values.
	assert.Positive(t, timeoutConfig2.BaseTimeout)
	assert.Greater(t, timeoutConfig2.MaxTimeout, timeoutConfig2.BaseTimeout)
}

func TestTimeoutTuner_LoadBasedRetry(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	testCases := []struct {
		name          string
		enableLoad    bool
		loadThreshold float64
		expectChange  bool
	}{
		{"Disabled", false, 0.5, false},
		{"LowLoad", true, 0.2, true},
		{"HighLoad", true, 0.9, true},
		{"MediumLoad", true, 0.5, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := DefaultTimeoutTuningConfig()
			config.EnableLoadBasedRetry = tc.enableLoad
			config.LoadThreshold = tc.loadThreshold

			tuner := NewTimeoutTuner(config, logger)
			retryConfig := tuner.GetOptimizedRetryConfig()

			assert.Positive(t, retryConfig.MaxRetries)
			assert.Positive(t, retryConfig.BaseDelay)
			assert.Greater(t, retryConfig.BackoffFactor, 1.0)
		})
	}
}

func TestTimeoutTuner_ConfigValidation(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	// Test with empty config - should apply defaults.
	config := TimeoutTuningConfig{}
	tuner := NewTimeoutTuner(config, logger)

	assert.Equal(t, TimeoutProfileDefault, tuner.config.TimeoutProfile)
	assert.Equal(t, RetryProfileDefault, tuner.config.RetryProfile)
	assert.Equal(t, NetworkConditionGood, tuner.config.NetworkCondition)
	assert.Equal(t, 5*time.Minute, tuner.config.AdjustmentInterval)
	assert.Equal(t, 15*time.Minute, tuner.config.PerformanceWindow)
	assert.InEpsilon(t, 3.0, tuner.config.LatencyMultiplier, 0.001)
	assert.InEpsilon(t, 0.8, tuner.config.LoadThreshold, 0.001)
	assert.NotNil(t, tuner.config.ProtocolOverrides)
}

func TestTimeoutTuner_Defaults(t *testing.T) {
	t.Parallel()

	defaultConfig := DefaultTimeoutTuningConfig()

	assert.Equal(t, TimeoutProfileDefault, defaultConfig.TimeoutProfile)
	assert.Equal(t, RetryProfileDefault, defaultConfig.RetryProfile)
	assert.Equal(t, NetworkConditionGood, defaultConfig.NetworkCondition)
	assert.True(t, defaultConfig.EnableDynamicAdjustment)
	assert.Equal(t, 5*time.Minute, defaultConfig.AdjustmentInterval)
	assert.Equal(t, 15*time.Minute, defaultConfig.PerformanceWindow)
	assert.True(t, defaultConfig.EnableLatencyBasedTimeout)
	assert.InEpsilon(t, 3.0, defaultConfig.LatencyMultiplier, 0.001)
	assert.False(t, defaultConfig.EnableLoadBasedRetry)
	assert.InEpsilon(t, 0.8, defaultConfig.LoadThreshold, 0.001)
	assert.NotNil(t, defaultConfig.ProtocolOverrides)
}

// Benchmark timeout configuration generation.
func BenchmarkTimeoutTuner_GetOptimizedTimeout(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultTimeoutTuningConfig()
	tuner := NewTimeoutTuner(config, logger)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = tuner.GetOptimizedTimeoutConfig()
	}
}

func BenchmarkTimeoutTuner_GetOptimizedRetry(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultTimeoutTuningConfig()
	tuner := NewTimeoutTuner(config, logger)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = tuner.GetOptimizedRetryConfig()
	}
}

func BenchmarkTimeoutTuner_GetProtocolOptimized(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultTimeoutTuningConfig()
	tuner := NewTimeoutTuner(config, logger)

	protocols := []string{"stdio", "http", "ws", "sse"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			protocol := protocols[i%len(protocols)]
			_ = tuner.GetProtocolOptimizedConfig(protocol)
			i++
		}
	})
}

// Test profile combinations.
func TestTimeoutTuner_ProfileCombinations(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	timeoutProfiles := []TimeoutProfile{
		TimeoutProfileFast, TimeoutProfileDefault, TimeoutProfileRobust,
		TimeoutProfileBulk, TimeoutProfileInteractive,
	}

	retryProfiles := []RetryProfile{
		RetryProfileAggressive, RetryProfileDefault, RetryProfileConservative,
		RetryProfileBackoff, RetryProfileCircuitBreaker,
	}

	networkConditions := []NetworkCondition{
		NetworkConditionExcellent, NetworkConditionGood,
		NetworkConditionFair, NetworkConditionPoor,
	}

	// Test a sampling of combinations.
	for i, timeoutProfile := range timeoutProfiles {
		for j, retryProfile := range retryProfiles {
			if i%2 == 0 && j%2 == 0 { // Test every other combination
				for k, networkCondition := range networkConditions {
					if k%2 == 0 { // Test every other condition
						t.Run(string(timeoutProfile)+"_"+string(retryProfile)+"_"+string(networkCondition), func(t *testing.T) {
							t.Parallel()

							config := DefaultTimeoutTuningConfig()
							config.TimeoutProfile = timeoutProfile
							config.RetryProfile = retryProfile
							config.NetworkCondition = networkCondition

							tuner := NewTimeoutTuner(config, logger)
							timeoutConfig := tuner.GetOptimizedTimeoutConfig()
							retryConfig := tuner.GetOptimizedRetryConfig()

							// Basic validation.
							assert.Positive(t, timeoutConfig.BaseTimeout)
							assert.Greater(t, timeoutConfig.MaxTimeout, timeoutConfig.BaseTimeout)
							assert.Less(t, timeoutConfig.MinTimeout, timeoutConfig.BaseTimeout)

							assert.Positive(t, retryConfig.MaxRetries)
							assert.Greater(t, retryConfig.BackoffFactor, 1.0)
							assert.Greater(t, retryConfig.MaxDelay, retryConfig.BaseDelay)
						})
					}
				}
			}
		}
	}
}

// Test edge cases.
func TestTimeoutTuner_EdgeCases(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	// Test with extreme values.
	config := DefaultTimeoutTuningConfig()
	config.LatencyMultiplier = 0.1 // Very low
	config.LoadThreshold = 0.01    // Very low
	config.EnableLatencyBasedTimeout = true
	config.EnableLoadBasedRetry = true

	tuner := NewTimeoutTuner(config, logger)
	timeoutConfig := tuner.GetOptimizedTimeoutConfig()
	retryConfig := tuner.GetOptimizedRetryConfig()

	// Should still produce valid configs.
	assert.Positive(t, timeoutConfig.BaseTimeout)
	assert.Positive(t, retryConfig.MaxRetries)

	// Test with very high values.
	config.LatencyMultiplier = 100.0
	config.LoadThreshold = 0.99

	tuner2 := NewTimeoutTuner(config, logger)
	timeoutConfig2 := tuner2.GetOptimizedTimeoutConfig()
	retryConfig2 := tuner2.GetOptimizedRetryConfig()

	// Should still be reasonable.
	assert.Greater(t, timeoutConfig2.BaseTimeout, time.Duration(0))
	assert.Less(t, timeoutConfig2.BaseTimeout, 1*time.Hour) // Shouldn't be crazy high
	assert.Positive(t, retryConfig2.MaxRetries)
	assert.Less(t, retryConfig2.MaxRetries, 20) // Shouldn't be crazy high
}
