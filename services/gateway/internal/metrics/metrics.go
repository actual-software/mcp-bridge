// Package metrics provides Prometheus metrics collection and reporting for the MCP gateway.
package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// unknownValue is used when a metric label value is not available.
	unknownValue = "unknown"
)

// Registry holds all Prometheus metrics.
type Registry struct {
	// Connection metrics
	ConnectionsTotal    prometheus.Gauge
	ConnectionsActive   prometheus.Gauge
	ConnectionsRejected prometheus.Counter

	// Request metrics
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RequestsInFlight prometheus.Gauge

	// Authentication metrics
	AuthFailuresTotal *prometheus.CounterVec

	// Routing metrics
	RoutingErrorsTotal    *prometheus.CounterVec
	EndpointRequestsTotal *prometheus.CounterVec

	// Circuit breaker metrics
	CircuitBreakerState *prometheus.GaugeVec

	// WebSocket metrics
	WebSocketMessagesTotal *prometheus.CounterVec
	WebSocketBytesTotal    *prometheus.CounterVec

	// Binary protocol (TCP) metrics
	TCPConnectionsTotal  prometheus.Gauge
	TCPConnectionsActive prometheus.Gauge
	TCPMessagesTotal     *prometheus.CounterVec
	TCPBytesTotal        *prometheus.CounterVec
	TCPProtocolErrors    *prometheus.CounterVec
	TCPMessageDuration   *prometheus.HistogramVec

	// Error metrics
	ErrorsTotal         *prometheus.CounterVec
	ErrorsByType        *prometheus.CounterVec
	ErrorsByComponent   *prometheus.CounterVec
	ErrorRetryable      *prometheus.CounterVec
	ErrorsByHTTPStatus  *prometheus.CounterVec
	ErrorsBySeverity    *prometheus.CounterVec
	ErrorsByOperation   *prometheus.CounterVec
	ErrorRecoveryTotal  *prometheus.CounterVec
	ErrorLatency        *prometheus.HistogramVec
}

// createConnectionMetrics creates connection-related metrics.
// nolint:ireturn // Prometheus interfaces
func createConnectionMetrics(
	factory promauto.Factory,
) (prometheus.Gauge, prometheus.Gauge, prometheus.Counter) {
	connTotal := factory.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_connections_total",
		Help: "Total number of active WebSocket connections",
	})
	connActive := factory.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_connections_active",
		Help: "Number of currently active connections",
	})
	connRejected := factory.NewCounter(prometheus.CounterOpts{
		Name: "mcp_gateway_connections_rejected_total",
		Help: "Total number of rejected connections",
	})

	return connTotal, connActive, connRejected
}

// createRequestMetrics creates request-related metrics.
// nolint:ireturn // Prometheus interfaces
func createRequestMetrics(
	factory promauto.Factory,
) (*prometheus.CounterVec, *prometheus.HistogramVec, prometheus.Gauge) {
	reqTotal := factory.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_requests_total",
		Help: "Total number of MCP requests",
	}, []string{"method", "status"})
	reqDuration := factory.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mcp_gateway_request_duration_seconds",
		Help:    "MCP request duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "status"})
	reqInFlight := factory.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_requests_in_flight",
		Help: "Number of requests currently being processed",
	})

	return reqTotal, reqDuration, reqInFlight
}

// createTCPMetrics creates TCP-related metrics.

func createTCPMetrics(factory promauto.Factory) (
	prometheus.Gauge,
	prometheus.Gauge,
	*prometheus.CounterVec,
	*prometheus.CounterVec,
	*prometheus.CounterVec,
	*prometheus.HistogramVec,
) {
	tcpConnTotal := factory.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_tcp_connections_total",
		Help: "Total number of TCP connections",
	})
	tcpConnActive := factory.NewGauge(prometheus.GaugeOpts{
		Name: "mcp_gateway_tcp_connections_active",
		Help: "Number of currently active TCP connections",
	})
	tcpMsgTotal := factory.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_tcp_messages_total",
		Help: "Total TCP messages by type",
	}, []string{"direction", "type"})
	tcpBytes := factory.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_tcp_bytes_total",
		Help: "Total TCP bytes transferred",
	}, []string{"direction"})
	tcpErrors := factory.NewCounterVec(prometheus.CounterOpts{
		Name: "mcp_gateway_tcp_protocol_errors_total",
		Help: "Total TCP protocol errors by type",
	}, []string{"error_type"})
	tcpDuration := factory.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mcp_gateway_tcp_message_duration_seconds",
		Help:    "TCP message processing duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"message_type"})

	return tcpConnTotal, tcpConnActive, tcpMsgTotal, tcpBytes, tcpErrors, tcpDuration
}

// InitializeMetricsRegistry creates and configures a metrics collection registry.
func InitializeMetricsRegistry() *Registry {
	reg := prometheus.NewRegistry()
	factory := promauto.With(reg)

	connTotal, connActive, connRejected := createConnectionMetrics(factory)
	reqTotal, reqDuration, reqInFlight := createRequestMetrics(factory)
	tcpConnTotal, tcpConnActive, tcpMsgTotal, tcpBytes, tcpErrors, tcpDuration := createTCPMetrics(factory)
	
	errorMetrics := createErrorMetrics(factory)
	authMetrics := createAuthMetrics(factory)
	routingMetrics := createRoutingMetrics(factory)
	wsMetrics := createWebSocketMetrics(factory)

	return &Registry{
		ConnectionsTotal:       connTotal,
		ConnectionsActive:      connActive,
		ConnectionsRejected:    connRejected,
		RequestsTotal:          reqTotal,
		RequestDuration:        reqDuration,
		RequestsInFlight:       reqInFlight,
		AuthFailuresTotal:      authMetrics.authFailures,
		RoutingErrorsTotal:     routingMetrics.routingErrors,
		EndpointRequestsTotal:  routingMetrics.endpointRequests,
		CircuitBreakerState:    routingMetrics.circuitBreaker,
		WebSocketMessagesTotal: wsMetrics.messages,
		WebSocketBytesTotal:    wsMetrics.bytes,
		TCPConnectionsTotal:    tcpConnTotal,
		TCPConnectionsActive:   tcpConnActive,
		TCPMessagesTotal:       tcpMsgTotal,
		TCPBytesTotal:          tcpBytes,
		TCPProtocolErrors:      tcpErrors,
		TCPMessageDuration:     tcpDuration,
		ErrorsTotal:            errorMetrics.total,
		ErrorsByType:           errorMetrics.byType,
		ErrorsByComponent:      errorMetrics.byComponent,
		ErrorRetryable:         errorMetrics.retryable,
		ErrorsByHTTPStatus:     errorMetrics.byHTTPStatus,
		ErrorsBySeverity:       errorMetrics.bySeverity,
		ErrorsByOperation:      errorMetrics.byOperation,
		ErrorRecoveryTotal:     errorMetrics.recovery,
		ErrorLatency:           errorMetrics.latency,
	}
}

type errorMetricsSet struct {
	total        *prometheus.CounterVec
	byType       *prometheus.CounterVec
	byComponent  *prometheus.CounterVec
	retryable    *prometheus.CounterVec
	byHTTPStatus *prometheus.CounterVec
	bySeverity   *prometheus.CounterVec
	byOperation  *prometheus.CounterVec
	recovery     *prometheus.CounterVec
	latency      *prometheus.HistogramVec
}

func createErrorMetrics(factory promauto.Factory) errorMetricsSet {
	return errorMetricsSet{
		total: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_errors_total",
			Help: "Total number of errors by code and component",
		}, []string{"code", "component", "operation"}),
		byType: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_errors_by_type_total",
			Help: "Total number of errors by error type",
		}, []string{"type"}),
		byComponent: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_errors_by_component_total",
			Help: "Total number of errors by component",
		}, []string{"component"}),
		retryable: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_errors_retryable_total",
			Help: "Total number of retryable vs non-retryable errors",
		}, []string{"retryable"}),
		byHTTPStatus: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_errors_by_http_status_total",
			Help: "Total number of errors by HTTP status code",
		}, []string{"status_code", "status_class"}),
		bySeverity: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_errors_by_severity_total",
			Help: "Total number of errors by severity level",
		}, []string{"severity"}),
		byOperation: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_errors_by_operation_total",
			Help: "Total number of errors by operation",
		}, []string{"operation", "component"}),
		recovery: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_error_recovery_total",
			Help: "Total number of error recovery attempts and successes",
		}, []string{"recovered", "error_type"}),
		latency: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mcp_gateway_error_handling_duration_seconds",
			Help:    "Time spent handling errors in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"error_type", "component"}),
	}
}

type authMetricsSet struct {
	authFailures *prometheus.CounterVec
}

func createAuthMetrics(factory promauto.Factory) authMetricsSet {
	return authMetricsSet{
		authFailures: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_auth_failures_total",
			Help: "Total number of authentication failures",
		}, []string{"reason"}),
	}
}

type routingMetricsSet struct {
	routingErrors    *prometheus.CounterVec
	endpointRequests *prometheus.CounterVec
	circuitBreaker   *prometheus.GaugeVec
}

func createRoutingMetrics(factory promauto.Factory) routingMetricsSet {
	return routingMetricsSet{
		routingErrors: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_routing_errors_total",
			Help: "Total number of routing errors",
		}, []string{"reason"}),
		endpointRequests: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_endpoint_requests_total",
			Help: "Total requests per endpoint",
		}, []string{"endpoint", "namespace", "status"}),
		circuitBreaker: factory.NewGaugeVec(prometheus.GaugeOpts{
			Name: "mcp_gateway_circuit_breaker_state",
			Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
		}, []string{"endpoint"}),
	}
}

type webSocketMetricsSet struct {
	messages *prometheus.CounterVec
	bytes    *prometheus.CounterVec
}

func createWebSocketMetrics(factory promauto.Factory) webSocketMetricsSet {
	return webSocketMetricsSet{
		messages: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_websocket_messages_total",
			Help: "Total WebSocket messages",
		}, []string{"direction", "type"}),
		bytes: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "mcp_gateway_websocket_bytes_total",
			Help: "Total WebSocket bytes transferred",
		}, []string{"direction"}),
	}
}

// IncrementConnections increments the active connections count.
func (r *Registry) IncrementConnections() {
	r.ConnectionsTotal.Inc()
	r.ConnectionsActive.Inc()
}

// DecrementConnections decrements the active connections count.
func (r *Registry) DecrementConnections() {
	r.ConnectionsActive.Dec()
}

// IncrementConnectionsRejected increments rejected connections.
func (r *Registry) IncrementConnectionsRejected() {
	r.ConnectionsRejected.Inc()
}

// IncrementRequests increments request count.
func (r *Registry) IncrementRequests(method, status string) {
	r.RequestsTotal.WithLabelValues(method, status).Inc()
}

// RecordRequestDuration records request duration.
func (r *Registry) RecordRequestDuration(method, status string, duration time.Duration) {
	r.RequestDuration.WithLabelValues(method, status).Observe(duration.Seconds())
}

// IncrementRequestsInFlight increments in-flight requests.
func (r *Registry) IncrementRequestsInFlight() {
	r.RequestsInFlight.Inc()
}

// DecrementRequestsInFlight decrements in-flight requests.
func (r *Registry) DecrementRequestsInFlight() {
	r.RequestsInFlight.Dec()
}

// IncrementAuthFailures increments authentication failures.
func (r *Registry) IncrementAuthFailures(reason string) {
	r.AuthFailuresTotal.WithLabelValues(reason).Inc()
}

// IncrementRoutingErrors increments routing errors.
func (r *Registry) IncrementRoutingErrors(reason string) {
	r.RoutingErrorsTotal.WithLabelValues(reason).Inc()
}

// IncrementEndpointRequests increments endpoint request count.
func (r *Registry) IncrementEndpointRequests(endpoint, namespace, status string) {
	r.EndpointRequestsTotal.WithLabelValues(endpoint, namespace, status).Inc()
}

// SetCircuitBreakerState sets circuit breaker state.
func (r *Registry) SetCircuitBreakerState(endpoint string, state float64) {
	r.CircuitBreakerState.WithLabelValues(endpoint).Set(state)
}

// IncrementWebSocketMessages increments WebSocket message count.
func (r *Registry) IncrementWebSocketMessages(direction, msgType string) {
	r.WebSocketMessagesTotal.WithLabelValues(direction, msgType).Inc()
}

// AddWebSocketBytes adds to WebSocket bytes counter.
func (r *Registry) AddWebSocketBytes(direction string, bytes int) {
	r.WebSocketBytesTotal.WithLabelValues(direction).Add(float64(bytes))
}

// IncrementTCPConnections increments the active TCP connections count.
func (r *Registry) IncrementTCPConnections() {
	r.TCPConnectionsTotal.Inc()
	r.TCPConnectionsActive.Inc()
}

// DecrementTCPConnections decrements the active TCP connections count.
func (r *Registry) DecrementTCPConnections() {
	r.TCPConnectionsActive.Dec()
}

// IncrementTCPMessages increments TCP message count.
func (r *Registry) IncrementTCPMessages(direction, msgType string) {
	r.TCPMessagesTotal.WithLabelValues(direction, msgType).Inc()
}

// AddTCPBytes adds to TCP bytes counter.
func (r *Registry) AddTCPBytes(direction string, bytes int) {
	r.TCPBytesTotal.WithLabelValues(direction).Add(float64(bytes))
}

// IncrementTCPProtocolErrors increments TCP protocol error count.
func (r *Registry) IncrementTCPProtocolErrors(errorType string) {
	r.TCPProtocolErrors.WithLabelValues(errorType).Inc()
}

// RecordTCPMessageDuration records TCP message processing duration.
func (r *Registry) RecordTCPMessageDuration(msgType string, duration time.Duration) {
	r.TCPMessageDuration.WithLabelValues(msgType).Observe(duration.Seconds())
}

// IncrementErrors increments error count with detailed labels.
func (r *Registry) IncrementErrors(code, component, operation string) {
	r.ErrorsTotal.WithLabelValues(code, component, operation).Inc()
}

// IncrementErrorsByType increments errors by type.
func (r *Registry) IncrementErrorsByType(errorType string) {
	r.ErrorsByType.WithLabelValues(errorType).Inc()
}

// IncrementErrorsByComponent increments errors by component.
func (r *Registry) IncrementErrorsByComponent(component string) {
	r.ErrorsByComponent.WithLabelValues(component).Inc()
}

// IncrementRetryableErrors increments retryable/non-retryable error count.
func (r *Registry) IncrementRetryableErrors(retryable bool) {
	retryableStr := "false"
	if retryable {
		retryableStr = "true"
	}

	r.ErrorRetryable.WithLabelValues(retryableStr).Inc()
}

// IncrementErrorsByHTTPStatus increments errors by HTTP status code.
func (r *Registry) IncrementErrorsByHTTPStatus(statusCode int) {
	statusClass := getStatusClass(statusCode)
	r.ErrorsByHTTPStatus.WithLabelValues(
		strconv.Itoa(statusCode),
		statusClass,
	).Inc()
}

// IncrementErrorsBySeverity increments errors by severity level.
func (r *Registry) IncrementErrorsBySeverity(severity string) {
	r.ErrorsBySeverity.WithLabelValues(severity).Inc()
}

// IncrementErrorsByOperation increments errors by operation.
func (r *Registry) IncrementErrorsByOperation(operation, component string) {
	if operation == "" {
		operation = unknownValue
	}
	
	if component == "" {
		component = unknownValue
	}
	
	r.ErrorsByOperation.WithLabelValues(operation, component).Inc()
}

// IncrementErrorRecovery tracks error recovery attempts.
func (r *Registry) IncrementErrorRecovery(recovered bool, errorType string) {
	recoveredStr := "false"
	if recovered {
		recoveredStr = "true"
	}
	
	r.ErrorRecoveryTotal.WithLabelValues(recoveredStr, errorType).Inc()
}

// RecordErrorLatency records time spent handling an error.
func (r *Registry) RecordErrorLatency(errorType, component string, duration time.Duration) {
	r.ErrorLatency.WithLabelValues(errorType, component).Observe(duration.Seconds())
}

// getStatusClass returns the HTTP status class (2xx, 3xx, 4xx, 5xx).
func getStatusClass(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return "2xx"
	case statusCode >= 300 && statusCode < 400:
		return "3xx"
	case statusCode >= 400 && statusCode < 500:
		return "4xx"
	case statusCode >= 500 && statusCode < 600:
		return "5xx"
	default:
		return unknownValue
	}
}
