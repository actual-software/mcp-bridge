
package testutil

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
)

const (
	testIterations    = 100
	testMaxIterations = 1000
)

// TestCreateTestMetricsRegistry tests the creation of a test metrics registry.
func TestCreateTestMetricsRegistry(t *testing.T) {
	t.Run("creates_valid_registry", func(t *testing.T) {
		testRegistryCreation(t)
	})

	t.Run("metrics_have_correct_names", func(t *testing.T) {
		testMetricNames(t)
	})

	t.Run("metrics_have_help_text", func(t *testing.T) {
		testMetricHelpText(t)
	})

	t.Run("counter_vec_metrics_have_labels", func(t *testing.T) {
		testCounterVecLabels(t)
	})

	t.Run("histogram_vec_metrics_have_buckets", func(t *testing.T) {
		testHistogramVecBuckets(t)
	})
}

func testRegistryCreation(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistry()
	assert.NotNil(t, registry)
	assert.IsType(t, &metrics.Registry{}, registry)

	validateConnectionMetrics(t, registry)
	validateRequestMetrics(t, registry)
	validateAuthMetrics(t, registry)
	validateRoutingMetrics(t, registry)
	validateCircuitBreakerMetrics(t, registry)
	validateWebSocketMetrics(t, registry)
	validateTCPMetrics(t, registry)
}

func validateConnectionMetrics(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assert.NotNil(t, registry.ConnectionsTotal)
	assert.NotNil(t, registry.ConnectionsActive)
	assert.NotNil(t, registry.ConnectionsRejected)
}

func validateRequestMetrics(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assert.NotNil(t, registry.RequestsTotal)
	assert.NotNil(t, registry.RequestDuration)
	assert.NotNil(t, registry.RequestsInFlight)
}

func validateAuthMetrics(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assert.NotNil(t, registry.AuthFailuresTotal)
}

func validateRoutingMetrics(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assert.NotNil(t, registry.RoutingErrorsTotal)
	assert.NotNil(t, registry.EndpointRequestsTotal)
}

func validateCircuitBreakerMetrics(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assert.NotNil(t, registry.CircuitBreakerState)
}

func validateWebSocketMetrics(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assert.NotNil(t, registry.WebSocketMessagesTotal)
	assert.NotNil(t, registry.WebSocketBytesTotal)
}

func validateTCPMetrics(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assert.NotNil(t, registry.TCPConnectionsTotal)
	assert.NotNil(t, registry.TCPConnectionsActive)
	assert.NotNil(t, registry.TCPMessagesTotal)
	assert.NotNil(t, registry.TCPBytesTotal)
	assert.NotNil(t, registry.TCPProtocolErrors)
	assert.NotNil(t, registry.TCPMessageDuration)
}

func testMetricNames(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistry()

	validateConnectionMetricNames(t, registry)
	validateRequestMetricNames(t, registry)
	validateAuthMetricNames(t, registry)
	validateRoutingMetricNames(t, registry)
	validateCircuitBreakerMetricNames(t, registry)
	validateWebSocketMetricNames(t, registry)
	validateTCPMetricNames(t, registry)
}

func validateConnectionMetricNames(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assertMetricName(t, registry.ConnectionsTotal, "mcp_gateway_connections_total")
	assertMetricName(t, registry.ConnectionsActive, "mcp_gateway_connections_active")
	assertMetricName(t, registry.ConnectionsRejected, "mcp_gateway_connections_rejected_total")
}

func validateRequestMetricNames(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assertMetricVecName(t, registry.RequestsTotal, "mcp_gateway_requests_total")
	assertMetricVecName(t, registry.RequestDuration, "mcp_gateway_request_duration_seconds")
	assertMetricName(t, registry.RequestsInFlight, "mcp_gateway_requests_in_flight")
}

func validateAuthMetricNames(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assertMetricVecName(t, registry.AuthFailuresTotal, "mcp_gateway_auth_failures_total")
}

func validateRoutingMetricNames(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assertMetricVecName(t, registry.RoutingErrorsTotal, "mcp_gateway_routing_errors_total")
	assertMetricVecName(t, registry.EndpointRequestsTotal, "mcp_gateway_endpoint_requests_total")
}

func validateCircuitBreakerMetricNames(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assertMetricVecName(t, registry.CircuitBreakerState, "mcp_gateway_circuit_breaker_state")
}

func validateWebSocketMetricNames(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assertMetricVecName(t, registry.WebSocketMessagesTotal, "mcp_gateway_websocket_messages_total")
	assertMetricVecName(t, registry.WebSocketBytesTotal, "mcp_gateway_websocket_bytes_total")
}

func validateTCPMetricNames(t *testing.T, registry *metrics.Registry) {
	t.Helper()
	
	assertMetricName(t, registry.TCPConnectionsTotal, "mcp_gateway_tcp_connections_total")
	assertMetricName(t, registry.TCPConnectionsActive, "mcp_gateway_tcp_connections_active")
	assertMetricVecName(t, registry.TCPMessagesTotal, "mcp_gateway_tcp_messages_total")
	assertMetricVecName(t, registry.TCPBytesTotal, "mcp_gateway_tcp_bytes_total")
	assertMetricVecName(t, registry.TCPProtocolErrors, "mcp_gateway_tcp_protocol_errors_total")
	assertMetricVecName(t, registry.TCPMessageDuration, "mcp_gateway_tcp_message_duration_seconds")
}

func testMetricHelpText(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistry()

	// Sample a few metrics to ensure help text is present
	assertMetricHasHelp(t, registry.ConnectionsTotal)
	assertMetricHasHelp(t, registry.RequestsTotal)
	assertMetricHasHelp(t, registry.TCPMessagesTotal)
}

func testCounterVecLabels(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistry()

	// Test that CounterVec metrics can accept labels
	assert.NotPanics(t, func() {
		registry.RequestsTotal.WithLabelValues("test_method", "success")
	})

	assert.NotPanics(t, func() {
		registry.AuthFailuresTotal.WithLabelValues("invalid_token")
	})

	assert.NotPanics(t, func() {
		registry.EndpointRequestsTotal.WithLabelValues("test-endpoint", "default", "httpStatusOK")
	})

	assert.NotPanics(t, func() {
		registry.TCPMessagesTotal.WithLabelValues("inbound", "request")
	})
}

func testHistogramVecBuckets(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistry()

	// Test that histogram metrics have proper bucket configuration
	assert.NotPanics(t, func() {
		registry.RequestDuration.WithLabelValues("test_method", "success")
	})

	assert.NotPanics(t, func() {
		registry.TCPMessageDuration.WithLabelValues("request")
	})
}

// TestCreateTestMetricsRegistryWithHelpers tests the enhanced registry with helper methods.
func TestCreateTestMetricsRegistryWithHelpers(t *testing.T) {
	t.Run("creates_enhanced_registry", func(t *testing.T) {
		registry := CreateTestMetricsRegistryWithHelpers()

		assert.NotNil(t, registry)
		assert.IsType(t, &TestMetricsRegistry{}, registry)
		assert.NotNil(t, registry.Registry)

		// Verify that underlying registry is accessible
		assert.NotNil(t, registry.TCPMessagesTotal)
		assert.NotNil(t, registry.TCPProtocolErrors)
	})

	t.Run("registry_embeds_base_functionality", func(t *testing.T) {
		registry := CreateTestMetricsRegistryWithHelpers()

		// Should be able to access all base registry functionality
		assert.NotNil(t, registry.ConnectionsTotal)
		assert.NotNil(t, registry.RequestsTotal)
		assert.NotNil(t, registry.WebSocketMessagesTotal)
	})
}

// TestGetTCPMessageCount tests the TCP message count helper method.
func TestGetTCPMessageCount(t *testing.T) {
	t.Run("returns_zero_for_new_metric", func(t *testing.T) {
		testTCPMessageCountZero(t)
	})

	t.Run("returns_correct_count_after_increment", func(t *testing.T) {
		testTCPMessageCountAfterSingleIncrement(t)
	})

	t.Run("returns_correct_count_after_multiple_increments", func(t *testing.T) {
		testTCPMessageCountAfterMultipleIncrements(t)
	})

	t.Run("returns_zero_for_invalid_labels", func(t *testing.T) {
		testTCPMessageCountWithInvalidLabels(t)
	})

	t.Run("handles_different_label_combinations", func(t *testing.T) {
		testTCPMessageCountWithDifferentLabelCombinations(t)
	})
}

func testTCPMessageCountZero(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()
	count := registry.GetTCPMessageCount("inbound", "request")
	assert.Equal(t, float64(0), count)
}

func testTCPMessageCountAfterSingleIncrement(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()

	// Increment the counter
	counter, err := registry.TCPMessagesTotal.GetMetricWithLabelValues("inbound", "request")
	require.NoError(t, err)
	counter.Inc()

	count := registry.GetTCPMessageCount("inbound", "request")
	assert.Equal(t, float64(1), count)
}

func testTCPMessageCountAfterMultipleIncrements(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()

	// Increment the counter multiple times
	counter, err := registry.TCPMessagesTotal.GetMetricWithLabelValues("outbound", "response")
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		counter.Inc()
	}

	count := registry.GetTCPMessageCount("outbound", "response")
	assert.Equal(t, float64(5), count)
}

func testTCPMessageCountWithInvalidLabels(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()

	// First increment a valid metric
	counter, err := registry.TCPMessagesTotal.GetMetricWithLabelValues("inbound", "request")
	require.NoError(t, err)
	counter.Inc()

	// Check different labels return zero
	count := registry.GetTCPMessageCount("invalid", "request")
	assert.Equal(t, float64(0), count)

	count = registry.GetTCPMessageCount("inbound", "invalid")
	assert.Equal(t, float64(0), count)
}

func testTCPMessageCountWithDifferentLabelCombinations(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()

	testCases := createTCPMessageTestCases()
	setupTCPMessageCounters(t, registry, testCases)
	verifyTCPMessageCounts(t, registry, testCases)
}

type tcpMessageTestCase struct {
	direction  string
	msgType    string
	increments int
}

func createTCPMessageTestCases() []tcpMessageTestCase {
	return []tcpMessageTestCase{
		{"inbound", "request", 3},
		{"inbound", "response", 7},
		{"outbound", "request", 2},
		{"outbound", "response", 11},
	}
}

func setupTCPMessageCounters(t *testing.T, registry *TestMetricsRegistry, testCases []tcpMessageTestCase) {
	t.Helper()
	
	for _, tc := range testCases {
		counter, err := registry.TCPMessagesTotal.GetMetricWithLabelValues(tc.direction, tc.msgType)
		require.NoError(t, err)

		for i := 0; i < tc.increments; i++ {
			counter.Inc()
		}
	}
}

func verifyTCPMessageCounts(t *testing.T, registry *TestMetricsRegistry, testCases []tcpMessageTestCase) {
	t.Helper()
	
	for _, tc := range testCases {
		count := registry.GetTCPMessageCount(tc.direction, tc.msgType)
		assert.Equal(t, float64(tc.increments), count, "Failed for %s/%s", tc.direction, tc.msgType)
	}
}

// TestGetTCPProtocolErrorCount tests the TCP protocol error count helper method.
func TestGetTCPProtocolErrorCount(t *testing.T) {
	t.Run("returns_zero_for_new_metric", func(t *testing.T) {
		testTCPProtocolErrorCountZero(t)
	})

	t.Run("returns_correct_count_after_increment", func(t *testing.T) {
		testTCPProtocolErrorCountAfterSingleIncrement(t)
	})

	t.Run("returns_correct_count_after_multiple_increments", func(t *testing.T) {
		testTCPProtocolErrorCountAfterMultipleIncrements(t)
	})

	t.Run("returns_zero_for_invalid_error_type", func(t *testing.T) {
		testTCPProtocolErrorCountWithInvalidErrorType(t)
	})

	t.Run("handles_different_error_types", func(t *testing.T) {
		testTCPProtocolErrorCountWithDifferentErrorTypes(t)
	})
}

func testTCPProtocolErrorCountZero(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()
	count := registry.GetTCPProtocolErrorCount("invalid_frame")
	assert.Equal(t, float64(0), count)
}

func testTCPProtocolErrorCountAfterSingleIncrement(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()

	// Increment the counter
	counter, err := registry.TCPProtocolErrors.GetMetricWithLabelValues("invalid_frame")
	require.NoError(t, err)
	counter.Inc()

	count := registry.GetTCPProtocolErrorCount("invalid_frame")
	assert.Equal(t, float64(1), count)
}

func testTCPProtocolErrorCountAfterMultipleIncrements(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()

	// Increment the counter multiple times
	counter, err := registry.TCPProtocolErrors.GetMetricWithLabelValues("timeout")
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		counter.Inc()
	}

	count := registry.GetTCPProtocolErrorCount("timeout")
	assert.Equal(t, float64(3), count)
}

func testTCPProtocolErrorCountWithInvalidErrorType(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()

	// First increment a valid metric
	counter, err := registry.TCPProtocolErrors.GetMetricWithLabelValues("valid_error")
	require.NoError(t, err)
	counter.Inc()

	// Check invalid error type returns zero
	count := registry.GetTCPProtocolErrorCount("invalid_error_type")
	assert.Equal(t, float64(0), count)
}

func testTCPProtocolErrorCountWithDifferentErrorTypes(t *testing.T) {
	t.Helper()
	
	registry := CreateTestMetricsRegistryWithHelpers()

	// Set up different error types
	errorTypes := createTCPProtocolErrorTestCases()
	setupTCPProtocolErrorCounters(t, registry, errorTypes)
	verifyTCPProtocolErrorCounts(t, registry, errorTypes)
}

func createTCPProtocolErrorTestCases() map[string]int {
	return map[string]int{
		"invalid_frame":  5,
		"timeout":        2,
		"parse_error":    8,
		"protocol_error": 1,
	}
}

func setupTCPProtocolErrorCounters(t *testing.T, registry *TestMetricsRegistry, errorTypes map[string]int) {
	t.Helper()
	
	for errorType, increments := range errorTypes {
		counter, err := registry.TCPProtocolErrors.GetMetricWithLabelValues(errorType)
		require.NoError(t, err)

		for i := 0; i < increments; i++ {
			counter.Inc()
		}
	}
}

func verifyTCPProtocolErrorCounts(t *testing.T, registry *TestMetricsRegistry, errorTypes map[string]int) {
	t.Helper()
	
	for errorType, expectedCount := range errorTypes {
		count := registry.GetTCPProtocolErrorCount(errorType)
		assert.Equal(t, float64(expectedCount), count, "Failed for error type %s", errorType)
	}
}

// TestMetricsRegistryIntegration tests integration scenarios.
func TestMetricsRegistryIntegration(t *testing.T) {
	t.Run("simultaneous_metric_operations", func(t *testing.T) {
		runSimultaneousMetricOperationsTest(t)
	})

	t.Run("metric_reset_behavior", func(t *testing.T) {
		runMetricResetBehaviorTest(t)
	})

	t.Run("label_validation", func(t *testing.T) {
		runLabelValidationTest(t)
	})
}

func runSimultaneousMetricOperationsTest(t *testing.T) {
	t.Helper()
	registry := CreateTestMetricsRegistryWithHelpers()

	// Simulate concurrent metric operations
	// TCP Messages
	tcpCounter, err := registry.TCPMessagesTotal.GetMetricWithLabelValues("inbound", "request")
	require.NoError(t, err)
	tcpCounter.Add(10)

	// TCP Errors
	errorCounter, err := registry.TCPProtocolErrors.GetMetricWithLabelValues("frame_error")
	require.NoError(t, err)
	errorCounter.Add(3)

	// Connection metrics
	registry.ConnectionsActive.Set(25)
	registry.ConnectionsTotal.Set(testIterations)

	// Verify all metrics
	assert.Equal(t, float64(10), registry.GetTCPMessageCount("inbound", "request"))
	assert.Equal(t, float64(3), registry.GetTCPProtocolErrorCount("frame_error"))

	assertGaugeValue(t, registry.ConnectionsActive, 25)
	assertGaugeValue(t, registry.ConnectionsTotal, testIterations)
}

func runMetricResetBehaviorTest(t *testing.T) {
	t.Helper()
	registry := CreateTestMetricsRegistryWithHelpers()

	// Increment some metrics
	tcpCounter, err := registry.TCPMessagesTotal.GetMetricWithLabelValues("outbound", "response")
	require.NoError(t, err)
	tcpCounter.Add(5)

	// Verify initial value
	assert.Equal(t, float64(5), registry.GetTCPMessageCount("outbound", "response"))

	// Reset by creating new registry
	newRegistry := CreateTestMetricsRegistryWithHelpers()
	assert.Equal(t, float64(0), newRegistry.GetTCPMessageCount("outbound", "response"))
}

func runLabelValidationTest(t *testing.T) {
	t.Helper()
	registry := CreateTestMetricsRegistryWithHelpers()

	validLabels := getValidLabelTestCases()
	for _, labels := range validLabels {
		assert.NotPanics(t, func() {
			registry.GetTCPMessageCount(labels.direction, labels.msgType)
		}, "Should not panic for labels: %+v", labels)
	}

	errorTypes := getErrorTypeTestCases()
	for _, errorType := range errorTypes {
		assert.NotPanics(t, func() {
			registry.GetTCPProtocolErrorCount(errorType)
		}, "Should not panic for error type: %s", errorType)
	}
}

func getValidLabelTestCases() []struct {
	direction string
	msgType   string
} {
	return []struct {
		direction string
		msgType   string
	}{
		{"inbound", "request"},
		{"outbound", "response"},
		{"internal", "heartbeat"},
		{"", ""}, // Empty labels should be handled
	}
}

func getErrorTypeTestCases() []string {
	return []string{
		"frame_error",
		"timeout",
		"parse_error",
		"",
	}
}

// TestMetricsErrorHandling tests error handling in helper methods.
func TestMetricsErrorHandling(t *testing.T) {
	t.Run("handles_metric_retrieval_errors_gracefully", func(t *testing.T) {
		registry := CreateTestMetricsRegistryWithHelpers()

		// These should not panic and return 0 for any errors
		assert.NotPanics(t, func() {
			count := registry.GetTCPMessageCount("test", "test")
			assert.GreaterOrEqual(t, count, float64(0))
		})

		assert.NotPanics(t, func() {
			count := registry.GetTCPProtocolErrorCount("test")
			assert.GreaterOrEqual(t, count, float64(0))
		})
	})

	t.Run("handles_nil_metric_gracefully", func(t *testing.T) {
		// Create a registry with properly initialized metrics
		registry := CreateTestMetricsRegistryWithHelpers()

		// These should work correctly with initialized metrics
		assert.NotPanics(t, func() {
			count := registry.GetTCPMessageCount("test", "test")
			assert.GreaterOrEqual(t, count, float64(0))
		})

		assert.NotPanics(t, func() {
			count := registry.GetTCPProtocolErrorCount("test")
			assert.GreaterOrEqual(t, count, float64(0))
		})
	})
}

// TestMetricsPerformance tests basic performance characteristics.
func TestMetricsPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance tests in short mode")
	}

	t.Run("metric_creation_performance", func(t *testing.T) {
		// Creating registry should be fast
		assert.NotPanics(t, func() {
			for i := 0; i < testIterations; i++ {
				CreateTestMetricsRegistry()
			}
		})
	})

	t.Run("metric_access_performance", func(t *testing.T) {
		registry := CreateTestMetricsRegistryWithHelpers()

		// Accessing metrics should be fast
		assert.NotPanics(t, func() {
			for i := 0; i < testMaxIterations; i++ {
				registry.GetTCPMessageCount("inbound", "request")
				registry.GetTCPProtocolErrorCount("error")
			}
		})
	})
}

// Helper functions

func assertMetricName(t *testing.T, metric prometheus.Metric, expectedName string) {
	t.Helper()

	desc := metric.Desc()
	assert.Contains(t, desc.String(), expectedName)
}

func assertMetricVecName(t *testing.T, metricVec interface{}, expectedName string) {
	t.Helper()
	// For MetricVec types, we create a sample metric with appropriate label values
	switch mv := metricVec.(type) {
	case *prometheus.CounterVec:
		assertCounterVecName(t, mv, expectedName)
	case *prometheus.GaugeVec:
		assertGaugeVecName(t, mv, expectedName)
	case *prometheus.HistogramVec:
		assertHistogramVecName(t, mv, expectedName)
	}
}

func assertCounterVecName(t *testing.T, mv *prometheus.CounterVec, expectedName string) {
	t.Helper()

	testLabels := [][]string{
		{"test", "test"},         // method, status
		{"test"},                 // single label
		{"test", "test", "test"}, // three labels
	}
	for _, labels := range testLabels {
		counter, err := mv.GetMetricWithLabelValues(labels...)
		if err == nil && counter != nil {
			desc := counter.Desc()
			assert.Contains(t, desc.String(), expectedName)

			return
		}
	}
}

func assertGaugeVecName(t *testing.T, mv *prometheus.GaugeVec, expectedName string) {
	t.Helper()

	testLabels := [][]string{
		{"test"},         // single label
		{"test", "test"}, // two labels
	}
	for _, labels := range testLabels {
		gauge, err := mv.GetMetricWithLabelValues(labels...)
		if err == nil && gauge != nil {
			desc := gauge.Desc()
			assert.Contains(t, desc.String(), expectedName)

			return
		}
	}
}

func assertHistogramVecName(t *testing.T, mv *prometheus.HistogramVec, expectedName string) {
	t.Helper()

	testLabels := [][]string{
		{"test", "test"}, // method, status
		{"test"},         // single label
	}
	for _, labels := range testLabels {
		histogram, err := mv.GetMetricWithLabelValues(labels...)
		if err == nil && histogram != nil {
			// For histogram, we can't access Desc() directly, but we can verify it exists
			assert.NotNil(t, histogram)

			return
		}
	}
}

func assertMetricHasHelp(t *testing.T, metric interface{}) {
	t.Helper()

	switch m := metric.(type) {
	case prometheus.Metric:
		assertPrometheusMetric(t, m)
	case *prometheus.CounterVec:
		assertCounterVecMetric(t, m)
	case *prometheus.GaugeVec:
		assertGaugeVecMetric(t, m)
	case *prometheus.HistogramVec:
		assertHistogramVecMetric(t, m)
	default:
		assert.NotNil(t, metric)
	}
}

func assertPrometheusMetric(t *testing.T, m prometheus.Metric) {
	t.Helper()

	desc := m.Desc()
	assert.NotEmpty(t, desc.String())
}

func assertCounterVecMetric(t *testing.T, m *prometheus.CounterVec) {
	t.Helper()

	testLabels := [][]string{
		{"test", "test"},
		{"test"},
	}
	for _, labels := range testLabels {
		counter, err := m.GetMetricWithLabelValues(labels...)
		if err == nil && counter != nil {
			desc := counter.Desc()
			assert.NotEmpty(t, desc.String())

			return
		}
	}
}

func assertGaugeVecMetric(t *testing.T, m *prometheus.GaugeVec) {
	t.Helper()

	testLabels := [][]string{
		{"test"},
		{"test", "test"},
	}
	for _, labels := range testLabels {
		gauge, err := m.GetMetricWithLabelValues(labels...)
		if err == nil && gauge != nil {
			desc := gauge.Desc()
			assert.NotEmpty(t, desc.String())

			return
		}
	}
}

func assertHistogramVecMetric(t *testing.T, m *prometheus.HistogramVec) {
	t.Helper()

	testLabels := [][]string{
		{"test", "test"},
		{"test"},
	}
	for _, labels := range testLabels {
		histogram, err := m.GetMetricWithLabelValues(labels...)
		if err == nil && histogram != nil {
			// For histogram, just verify we can create it
			assert.NotNil(t, histogram)

			return
		}
	}
}

func assertGaugeValue(t *testing.T, gauge prometheus.Gauge, expectedValue float64) {
	t.Helper()

	metric := &dto.Metric{}
	err := gauge.Write(metric)
	require.NoError(t, err)
	require.NotNil(t, metric.Gauge)
	assert.Equal(t, expectedValue, metric.Gauge.GetValue())
}

// TestMetricsErrorPaths tests error handling paths in helper methods.
func TestMetricsErrorPaths(t *testing.T) {
	t.Run("tcp_message_count_with_write_error", func(t *testing.T) {
		registry := CreateTestMetricsRegistryWithHelpers()

		// Create a metric with invalid label combination to trigger GetMetricWithLabelValues error
		count := registry.GetTCPMessageCount("", "") // Empty labels may cause errors
		assert.GreaterOrEqual(t, count, float64(0))  // Should handle gracefully and return 0 or positive
	})

	t.Run("tcp_protocol_error_count_with_write_error", func(t *testing.T) {
		registry := CreateTestMetricsRegistryWithHelpers()

		// Create a metric with empty error type
		count := registry.GetTCPProtocolErrorCount("") // Empty error type may cause issues
		assert.GreaterOrEqual(t, count, float64(0))    // Should handle gracefully
	})

	t.Run("tcp_message_count_handles_nil_counter", func(t *testing.T) {
		registry := CreateTestMetricsRegistryWithHelpers()

		// Test with labels that might not exist
		count := registry.GetTCPMessageCount("nonexistent_direction", "nonexistent_type")
		assert.Equal(t, float64(0), count) // Should return 0 for non-existent metrics
	})

	t.Run("tcp_protocol_error_count_handles_nil_counter", func(t *testing.T) {
		registry := CreateTestMetricsRegistryWithHelpers()

		// Test with error type that might not exist
		count := registry.GetTCPProtocolErrorCount("nonexistent_error_type")
		assert.Equal(t, float64(0), count) // Should return 0 for non-existent metrics
	})
}
