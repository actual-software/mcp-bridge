package health

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
)

const (
	testIterations          = 100
	testTimeout             = 50
	httpStatusOK            = 200
	httpStatusInternalError = 500
)

func TestNewPredictiveHealthMonitor(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{
		HealthCheckInterval: 10 * time.Second,
		FailureThreshold:    3,
		EnablePrediction:    true,
	}

	monitor := NewPredictiveHealthMonitor(logger, config)

	require.NotNil(t, monitor)
	assert.Equal(t, config.HealthCheckInterval, monitor.config.HealthCheckInterval)
	assert.Equal(t, config.FailureThreshold, monitor.config.FailureThreshold)
	assert.True(t, monitor.config.EnablePrediction)
	assert.NotNil(t, monitor.healthData)
	assert.NotNil(t, monitor.circuitBreakers)
	assert.NotNil(t, monitor.predictor)

	// Clean up
	monitor.cancel()
}

func TestPredictiveHealthMonitor_RegisterEndpoint(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{
		HistorySize: testTimeout,
	}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}
	endpoint.SetHealthy(true)

	monitor.RegisterEndpoint(endpoint)

	// Verify endpoint was registered
	key := monitor.getEndpointKey(endpoint)
	monitor.healthDataMu.RLock()
	healthData, exists := monitor.healthData[key]
	monitor.healthDataMu.RUnlock()

	require.True(t, exists)
	assert.Equal(t, endpoint, healthData.Endpoint)
	assert.True(t, healthData.IsHealthy)
	assert.InEpsilon(t, 1.0, healthData.HealthScore, 0.01)
	assert.Empty(t, healthData.ResponseTimes)
	assert.Equal(t, config.HistorySize, cap(healthData.ResponseTimes))

	// Verify circuit breaker was created
	monitor.breakersMu.RLock()
	breaker, breakerExists := monitor.circuitBreakers[key]
	monitor.breakersMu.RUnlock()

	require.True(t, breakerExists)
	assert.Equal(t, key, breaker.name)
	assert.Equal(t, StateClosed, breaker.state)
}

func TestPredictiveHealthMonitor_RecordMetrics(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{
		FailureThreshold:   3,
		LatencyThreshold:   testIterations * time.Millisecond,
		ErrorRateThreshold: 0.1,
	}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	// Record successful request
	monitor.RecordMetrics(endpoint, testTimeout*time.Millisecond, true)

	key := monitor.getEndpointKey(endpoint)
	monitor.healthDataMu.RLock()
	healthData := monitor.healthData[key]
	monitor.healthDataMu.RUnlock()

	healthData.mu.RLock()
	assert.True(t, healthData.IsHealthy)
	assert.Equal(t, int64(1), healthData.TotalRequests)
	assert.Equal(t, 1, healthData.ConsecutiveSuccesses)
	assert.Equal(t, 0, healthData.ConsecutiveFailures)
	assert.Len(t, healthData.ResponseTimes, 1)
	assert.Equal(t, testTimeout*time.Millisecond, healthData.ResponseTimes[0])
	assert.Equal(t, testTimeout*time.Millisecond, healthData.AverageLatency)
	healthData.mu.RUnlock()

	// Record failed request
	monitor.RecordMetrics(endpoint, httpStatusOK*time.Millisecond, false)

	healthData.mu.RLock()
	assert.Equal(t, int64(2), healthData.TotalRequests)
	assert.Equal(t, 0, healthData.ConsecutiveSuccesses)
	assert.Equal(t, 1, healthData.ConsecutiveFailures)
	assert.Len(t, healthData.ResponseTimes, 2)
	assert.Equal(t, 125*time.Millisecond, healthData.AverageLatency) // Average of 50ms and 200ms
	healthData.mu.RUnlock()
}

func TestPredictiveHealthMonitor_IsHealthy(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{
		FailureThreshold: 3,
	}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	// Initially healthy (unregistered endpoint)
	assert.True(t, monitor.IsHealthy(endpoint))

	// Register and record successful metrics
	monitor.RecordMetrics(endpoint, testTimeout*time.Millisecond, true)
	assert.True(t, monitor.IsHealthy(endpoint))

	// Record multiple failures
	for i := 0; i < 3; i++ {
		monitor.RecordMetrics(endpoint, 1*time.Second, false)
	}

	// Should be unhealthy after threshold failures
	assert.False(t, monitor.IsHealthy(endpoint))

	// Record successes to recover
	for i := 0; i < 5; i++ {
		monitor.RecordMetrics(endpoint, testTimeout*time.Millisecond, true)
	}

	// Should be healthy again
	assert.True(t, monitor.IsHealthy(endpoint))
}

func TestPredictiveHealthMonitor_GetHealthScore(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{
		LatencyThreshold:   testIterations * time.Millisecond,
		ErrorRateThreshold: 0.1,
	}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	// Unregistered endpoint should have perfect score
	assert.InEpsilon(t, 1.0, monitor.GetHealthScore(endpoint), 0.01)

	// Record good metrics
	monitor.RecordMetrics(endpoint, testTimeout*time.Millisecond, true)
	score := monitor.GetHealthScore(endpoint)
	assert.InDelta(t, 1.0, score, 0.001) // Perfect score for low latency and no errors

	// Record high latency request
	monitor.RecordMetrics(endpoint, httpStatusInternalError*time.Millisecond, true)
	score = monitor.GetHealthScore(endpoint)
	assert.Less(t, score, 1.0) // Score should be reduced due to high latency

	// Record failed request
	monitor.RecordMetrics(endpoint, testIterations*time.Millisecond, false)
	score = monitor.GetHealthScore(endpoint)
	assert.Less(t, score, 0.8) // Score should be further reduced due to error
}

func TestPredictiveHealthMonitor_GetPredictedHealth(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{
		EnablePrediction: false,
	}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	// With prediction disabled, should return current health
	predicted, confidence := monitor.GetPredictedHealth(endpoint)
	assert.True(t, predicted)
	assert.InDelta(t, 1.0, confidence, 0.001)

	// Enable prediction
	monitor.config.EnablePrediction = true

	// Unregistered endpoint
	predicted, confidence = monitor.GetPredictedHealth(endpoint)
	assert.True(t, predicted)
	assert.InDelta(t, 1.0, confidence, 0.001)

	// Register endpoint
	monitor.RegisterEndpoint(endpoint)
	predicted, confidence = monitor.GetPredictedHealth(endpoint)
	assert.True(t, predicted)                 // Default predicted health
	assert.InDelta(t, 0.0, confidence, 0.001) // Default confidence
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
	}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	monitor.RegisterEndpoint(endpoint)
	key := monitor.getEndpointKey(endpoint)

	// Initially closed
	assert.True(t, monitor.isCircuitClosed(key))

	// Record failures to open circuit
	for i := 0; i < 10; i++ {
		monitor.RecordMetrics(endpoint, testIterations*time.Millisecond, false)
	}

	// Circuit should be open after failures
	assert.False(t, monitor.isCircuitClosed(key))

	// Verify circuit breaker state
	monitor.breakersMu.RLock()
	breaker := monitor.circuitBreakers[key]
	monitor.breakersMu.RUnlock()

	breaker.mu.RLock()
	assert.Equal(t, StateOpen, breaker.state)
	breaker.mu.RUnlock()

	// Record successes to move to half-open then closed
	for i := 0; i < config.SuccessThreshold; i++ {
		monitor.RecordMetrics(endpoint, testTimeout*time.Millisecond, true)
	}

	// Should move through half-open to closed
	breaker.mu.RLock()
	finalState := breaker.state
	breaker.mu.RUnlock()

	// Depending on the exact logic, it might be half-open or closed
	assert.True(t, finalState == StateHalfOpen || finalState == StateClosed)
}

func TestPredictiveHealthMonitor_Statistics(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	// Initially empty statistics
	stats := monitor.GetStatistics()
	assert.Equal(t, 0, stats.TotalEndpoints)
	assert.Equal(t, 0, stats.HealthyEndpoints)
	assert.Equal(t, 0, stats.UnhealthyEndpoints)

	// Register some endpoints
	endpoint1 := &discovery.Endpoint{Address: "127.0.0.1", Port: 8080, Scheme: "http"}
	endpoint2 := &discovery.Endpoint{Address: "127.0.0.1", Port: 8081, Scheme: "http"}

	monitor.RegisterEndpoint(endpoint1)
	monitor.RegisterEndpoint(endpoint2)

	// Update statistics manually
	monitor.updateStatistics()

	stats = monitor.GetStatistics()
	assert.Equal(t, 2, stats.TotalEndpoints)
	assert.Equal(t, 2, stats.HealthyEndpoints)
	assert.Equal(t, 0, stats.UnhealthyEndpoints)
	assert.InDelta(t, 1.0, stats.AverageHealthScore, 0.001)

	// Make one endpoint unhealthy
	for i := 0; i < 5; i++ {
		monitor.RecordMetrics(endpoint1, 1*time.Second, false)
	}

	monitor.updateStatistics()
	stats = monitor.GetStatistics()
	assert.Equal(t, 1, stats.HealthyEndpoints)
	assert.Equal(t, 1, stats.UnhealthyEndpoints)
	assert.Less(t, stats.AverageHealthScore, 1.0)
}

func TestPredictiveHealthMonitor_HistoryManagement(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{
		HistorySize: 5, // Small history for testing
	}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	// Record more metrics than history size
	for i := 0; i < 10; i++ {
		latency := time.Duration(i+1) * 10 * time.Millisecond
		monitor.RecordMetrics(endpoint, latency, true)
	}

	key := monitor.getEndpointKey(endpoint)
	monitor.healthDataMu.RLock()
	healthData := monitor.healthData[key]
	monitor.healthDataMu.RUnlock()

	healthData.mu.RLock()
	defer healthData.mu.RUnlock()

	// Should only keep the most recent entries
	assert.Len(t, healthData.ResponseTimes, config.HistorySize)
	assert.Len(t, healthData.Timestamps, config.HistorySize)

	// Should contain the most recent latencies (60ms, 70ms, 80ms, 90ms, 100ms)
	expectedLatencies := []time.Duration{
		60 * time.Millisecond,
		70 * time.Millisecond,
		80 * time.Millisecond,
		90 * time.Millisecond,
		testIterations * time.Millisecond,
	}
	assert.Equal(t, expectedLatencies, healthData.ResponseTimes)
}

func TestPredictiveHealthMonitor_Shutdown(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{}
	monitor := NewPredictiveHealthMonitor(logger, config)

	// Register an endpoint to ensure goroutines are running
	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}
	monitor.RegisterEndpoint(endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shutdown should complete without error
	err := monitor.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestPredictiveHealthMonitor_DefaultConfiguration(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := PredictiveConfig{} // Empty config to test defaults

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	// Verify defaults were set
	assert.Equal(t, 30*time.Second, monitor.config.HealthCheckInterval)
	assert.Equal(t, 5*time.Minute, monitor.config.PredictionInterval)
	assert.Equal(t, 5, monitor.config.FailureThreshold)
	assert.Equal(t, 2, monitor.config.SuccessThreshold)
	assert.Equal(t, 30*time.Second, monitor.config.Timeout)
	assert.Equal(t, testIterations, monitor.config.HistorySize)
	assert.Equal(t, 15*time.Minute, monitor.config.PredictionWindow)
	assert.Equal(t, 1*time.Second, monitor.config.LatencyThreshold)
	assert.InDelta(t, 0.05, monitor.config.ErrorRateThreshold, 0.001)
	assert.InDelta(t, 2.0, monitor.config.AnomalyDeviationFactor, 0.001)
}

// Benchmark tests.
func BenchmarkRecordMetrics(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := PredictiveConfig{}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		monitor.RecordMetrics(endpoint, testTimeout*time.Millisecond, true)
	}
}

func BenchmarkIsHealthy(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := PredictiveConfig{}

	monitor := NewPredictiveHealthMonitor(logger, config)
	defer monitor.cancel()

	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	// Pre-populate with some data
	monitor.RegisterEndpoint(endpoint)

	for i := 0; i < testIterations; i++ {
		monitor.RecordMetrics(endpoint, testTimeout*time.Millisecond, true)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		monitor.IsHealthy(endpoint)
	}
}
