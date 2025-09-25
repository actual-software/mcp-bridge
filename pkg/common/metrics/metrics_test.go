package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetricName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		subsystem string
		metric    string
		expected  string
	}{
		{
			name:      "router metric",
			subsystem: SubsystemRouter,
			metric:    MetricRequestsTotal,
			expected:  "mcp_router_requests_total",
		},
		{
			name:      "gateway metric",
			subsystem: SubsystemGateway,
			metric:    MetricResponsesTotal,
			expected:  "mcp_gateway_responses_total",
		},
		{
			name:      "custom subsystem and metric",
			subsystem: "custom",
			metric:    "custom_metric",
			expected:  "mcp_custom_custom_metric",
		},
		{
			name:      "empty subsystem",
			subsystem: "",
			metric:    "test_metric",
			expected:  "mcp__test_metric",
		},
		{
			name:      "empty metric",
			subsystem: "test",
			metric:    "",
			expected:  "mcp_test_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := MetricName(tt.subsystem, tt.metric)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRouterMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metric   string
		expected string
	}{
		{
			name:     "requests total",
			metric:   MetricRequestsTotal,
			expected: "mcp_router_requests_total",
		},
		{
			name:     "active connections",
			metric:   MetricActiveConnections,
			expected: "mcp_router_active_connections",
		},
		{
			name:     "connection errors",
			metric:   MetricConnectionErrors,
			expected: "mcp_router_connection_errors_total",
		},
		{
			name:     "custom metric",
			metric:   "custom_metric",
			expected: "mcp_router_custom_metric",
		},
		{
			name:     "empty metric",
			metric:   "",
			expected: "mcp_router_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := RouterMetric(tt.metric)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGatewayMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metric   string
		expected string
	}{
		{
			name:     "responses total",
			metric:   MetricResponsesTotal,
			expected: "mcp_gateway_responses_total",
		},
		{
			name:     "auth failures",
			metric:   MetricAuthFailures,
			expected: "mcp_gateway_auth_failures_total",
		},
		{
			name:     "websocket messages",
			metric:   MetricWebsocketMessages,
			expected: "mcp_gateway_websocket_messages_total",
		},
		{
			name:     "custom metric",
			metric:   "custom_gateway_metric",
			expected: "mcp_gateway_custom_gateway_metric",
		},
		{
			name:     "empty metric",
			metric:   "",
			expected: "mcp_gateway_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := GatewayMetric(tt.metric)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConstants(t *testing.T) {
	t.Parallel()
	// Test that all constants are properly defined
	assert.Equal(t, "mcp", Namespace)
	assert.Equal(t, "router", SubsystemRouter)
	assert.Equal(t, "gateway", SubsystemGateway)

	// Test metric name constants
	assert.Equal(t, "requests_total", MetricRequestsTotal)
	assert.Equal(t, "responses_total", MetricResponsesTotal)
	assert.Equal(t, "errors_total", MetricErrorsTotal)
	assert.Equal(t, "request_duration_seconds", MetricRequestDurationSeconds)
	assert.Equal(t, "response_size_bytes", MetricResponseSizeBytes)
	assert.Equal(t, "active_connections", MetricActiveConnections)
	assert.Equal(t, "connections_total", MetricConnectionsTotal)
	assert.Equal(t, "connection_retries_total", MetricConnectionRetries)
	assert.Equal(t, "auth_failures_total", MetricAuthFailures)
	assert.Equal(t, "rate_limit_exceeded_total", MetricRateLimitExceeded)
	assert.Equal(t, "connection_errors_total", MetricConnectionErrors)
	assert.Equal(t, "websocket_messages_total", MetricWebsocketMessages)

	// Test label constants
	assert.Equal(t, "method", LabelMethod)
	assert.Equal(t, "status", LabelStatus)
	assert.Equal(t, "error_type", LabelErrorType)
	assert.Equal(t, "state", LabelState)
	assert.Equal(t, "direction", LabelDirection)
	assert.Equal(t, "type", LabelType)
	assert.Equal(t, "namespace", LabelNamespace)
	assert.Equal(t, "backend", LabelBackend)
	assert.Equal(t, "protocol", LabelProtocol)
	assert.Equal(t, "auth_type", LabelAuthType)

	// Test status values
	assert.Equal(t, "success", StatusSuccess)
	assert.Equal(t, "error", StatusError)
	assert.Equal(t, "timeout", StatusTimeout)

	// Test state values
	assert.Equal(t, "active", StateActive)
	assert.Equal(t, "idle", StateIdle)
	assert.Equal(t, "error", StateError)

	// Test direction values
	assert.Equal(t, "in", DirectionIn)
	assert.Equal(t, "out", DirectionOut)

	// Test type values
	assert.Equal(t, "request", TypeRequest)
	assert.Equal(t, "response", TypeResponse)
}

func TestMetricNameConsistency(t *testing.T) {
	t.Parallel()
	// Ensure RouterMetric and GatewayMetric are consistent with MetricName
	testMetric := "test_metric"

	routerResult := RouterMetric(testMetric)
	expectedRouter := MetricName(SubsystemRouter, testMetric)
	assert.Equal(t, expectedRouter, routerResult, "RouterMetric should be consistent with MetricName")

	gatewayResult := GatewayMetric(testMetric)
	expectedGateway := MetricName(SubsystemGateway, testMetric)
	assert.Equal(t, expectedGateway, gatewayResult, "GatewayMetric should be consistent with MetricName")
}

func BenchmarkMetricName(b *testing.B) {
	for range b.N {
		MetricName(SubsystemRouter, MetricRequestsTotal)
	}
}

func BenchmarkRouterMetric(b *testing.B) {
	for range b.N {
		RouterMetric(MetricRequestsTotal)
	}
}

func BenchmarkGatewayMetric(b *testing.B) {
	for range b.N {
		GatewayMetric(MetricResponsesTotal)
	}
}
