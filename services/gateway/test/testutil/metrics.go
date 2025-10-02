// Package testutil provides testing utilities and helpers for the MCP gateway services.
package testutil

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
)

// createTestConnectionMetrics creates test connection metrics.
//
//nolint:ireturn // Test helper returns Prometheus interfaces
func createTestConnectionMetrics() (prometheus.Gauge, prometheus.Gauge, prometheus.Counter) {
	connTotal := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_connections_total",
		Help: "Total number of active WebSocket connections",
	})
	connActive := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_connections_active",
		Help: "Number of currently active connections",
	})
	connRejected := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "mcp_gateway_connections_rejected_total",
		Help: "Total number of rejected connections",
	})

	return connTotal, connActive, connRejected
}

// createTestRequestMetrics creates test request metrics.
//
//nolint:ireturn // Test helper returns Prometheus interfaces
func createTestRequestMetrics() (*prometheus.CounterVec, *prometheus.HistogramVec, prometheus.Gauge) {
	reqTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_requests_total",
		Help: "Total number of MCP requests",
	}, []string{"method", "status"})
	reqDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mcp_gateway_request_duration_seconds",
		Help:    "MCP request duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "status"})
	reqInFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_requests_in_flight",
		Help: "Number of requests currently being processed",
	})

	return reqTotal, reqDuration, reqInFlight
}

// createTestTCPMetrics creates test TCP metrics.
//
//nolint:ireturn // Test helper returns prometheus interfaces
func createTestTCPMetrics() (
	prometheus.Gauge,
	prometheus.Gauge,
	*prometheus.CounterVec,
	*prometheus.CounterVec,
	*prometheus.CounterVec,
	*prometheus.HistogramVec,
) {
	tcpConnTotal := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_tcp_connections_total",
		Help: "Total number of TCP connections",
	})
	tcpConnActive := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_tcp_connections_active",
		Help: "Number of currently active TCP connections",
	})
	tcpMsgTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_tcp_messages_total",
		Help: "Total TCP messages by type",
	}, []string{"direction", "type"})
	tcpBytes := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_tcp_bytes_total",
		Help: "Total TCP bytes transferred",
	}, []string{"direction"})
	tcpErrors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_tcp_protocol_errors_total",
		Help: "Total TCP protocol errors by type",
	}, []string{"error_type"})
	tcpDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mcp_gateway_tcp_message_duration_seconds",
		Help:    "TCP message processing duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"message_type"})

	return tcpConnTotal, tcpConnActive, tcpMsgTotal, tcpBytes, tcpErrors, tcpDuration
}

// CreateTestMetricsRegistry creates a metrics registry for testing purposes.
// It uses an isolated Prometheus registry to avoid conflicts.
func CreateTestMetricsRegistry() *metrics.Registry {
	connTotal, connActive, connRejected := createTestConnectionMetrics()
	reqTotal, reqDuration, reqInFlight := createTestRequestMetrics()
	tcpConnTotal, tcpConnActive, tcpMsgTotal, tcpBytes, tcpErrors, tcpDuration := createTestTCPMetrics()

	registry := &metrics.Registry{
		ConnectionsTotal:     connTotal,
		ConnectionsActive:    connActive,
		ConnectionsRejected:  connRejected,
		RequestsTotal:        reqTotal,
		RequestDuration:      reqDuration,
		RequestsInFlight:     reqInFlight,
		TCPConnectionsTotal:  tcpConnTotal,
		TCPConnectionsActive: tcpConnActive,
		TCPMessagesTotal:     tcpMsgTotal,
		TCPBytesTotal:        tcpBytes,
		TCPProtocolErrors:    tcpErrors,
		TCPMessageDuration:   tcpDuration,
	}

	addAuthAndRoutingMetrics(registry)
	addWebSocketMetrics(registry)
	addErrorMetrics(registry)

	return registry
}

func addAuthAndRoutingMetrics(registry *metrics.Registry) {
	registry.AuthFailuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_auth_failures_total",
		Help: "Total number of authentication failures",
	}, []string{"reason"})

	registry.RoutingErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_routing_errors_total",
		Help: "Total number of routing errors",
	}, []string{"reason"})

	registry.EndpointRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_endpoint_requests_total",
		Help: "Total requests per endpoint",
	}, []string{"endpoint", "namespace", "status"})

	registry.CircuitBreakerState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mcp_gateway_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
	}, []string{"endpoint"})
}

func addWebSocketMetrics(registry *metrics.Registry) {
	registry.WebSocketMessagesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_websocket_messages_total",
		Help: "Total WebSocket messages",
	}, []string{"direction", "type"})

	registry.WebSocketBytesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_websocket_bytes_total",
		Help: "Total WebSocket bytes transferred",
	}, []string{"direction"})
}

func addErrorMetrics(registry *metrics.Registry) {
	registry.ErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_errors_total",
		Help: "Total number of errors",
	}, []string{"code", "component", "operation"})

	registry.ErrorsByType = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_errors_by_type_total",
		Help: "Total errors by type",
	}, []string{"type"})

	registry.ErrorsByComponent = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_errors_by_component_total",
		Help: "Total errors by component",
	}, []string{"component"})

	addAdditionalErrorMetrics(registry)
}

func addAdditionalErrorMetrics(registry *metrics.Registry) {
	registry.ErrorRetryable = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_errors_retryable_total",
		Help: "Total retryable/non-retryable errors",
	}, []string{"retryable"})

	registry.ErrorsByHTTPStatus = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_errors_by_http_status_total",
		Help: "Total errors by HTTP status code",
	}, []string{"status_code", "status_class"})

	registry.ErrorsBySeverity = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_errors_by_severity_total",
		Help: "Total errors by severity",
	}, []string{"severity"})

	registry.ErrorsByOperation = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_errors_by_operation_total",
		Help: "Total errors by operation",
	}, []string{"operation", "component"})

	registry.ErrorRecoveryTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_error_recovery_total",
		Help: "Total error recovery attempts",
	}, []string{"recovered", "error_type"})

	registry.ErrorLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "mcp_gateway_error_latency_seconds",
		Help: "Time spent handling errors",
	}, []string{"error_type", "component"})
}

// TestMetricsRegistry extends Registry with test helper methods.
type TestMetricsRegistry struct {
	*metrics.Registry
}

// CreateTestMetricsRegistryWithHelpers creates a test metrics registry with helper methods.
func CreateTestMetricsRegistryWithHelpers() *TestMetricsRegistry {
	return &TestMetricsRegistry{
		Registry: CreateTestMetricsRegistry(),
	}
}

// GetTCPMessageCount returns the count for a specific TCP message type.
func (r *TestMetricsRegistry) GetTCPMessageCount(direction, msgType string) float64 {
	metric := &dto.Metric{}

	counter, err := r.TCPMessagesTotal.GetMetricWithLabelValues(direction, msgType)
	if err != nil {
		return 0
	}

	if err := counter.Write(metric); err != nil {
		return 0
	}

	if metric.Counter == nil {
		return 0
	}

	return metric.Counter.GetValue()
}

// GetTCPProtocolErrorCount returns the count for a specific TCP protocol error.
func (r *TestMetricsRegistry) GetTCPProtocolErrorCount(errorType string) float64 {
	metric := &dto.Metric{}

	counter, err := r.TCPProtocolErrors.GetMetricWithLabelValues(errorType)
	if err != nil {
		return 0
	}

	if err := counter.Write(metric); err != nil {
		return 0
	}

	if metric.Counter == nil {
		return 0
	}

	return metric.Counter.GetValue()
}
