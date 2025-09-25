// Package metrics defines standardized metrics names, labels, and helper functions for MCP components.
package metrics

// StandardMetrics defines common metrics names and labels for MCP components.
const (
	// Namespace for all MCP metrics.
	Namespace = "mcp"

	// Subsystems.
	SubsystemRouter  = "router"
	SubsystemGateway = "gateway"

	// Common metric names.
	MetricRequestsTotal          = "requests_total"
	MetricResponsesTotal         = "responses_total"
	MetricErrorsTotal            = "errors_total"
	MetricRequestDurationSeconds = "request_duration_seconds"
	MetricResponseSizeBytes      = "response_size_bytes"
	MetricActiveConnections      = "active_connections"
	MetricConnectionsTotal       = "connections_total"
	MetricConnectionRetries      = "connection_retries_total"
	MetricAuthFailures           = "auth_failures_total"
	MetricRateLimitExceeded      = "rate_limit_exceeded_total"
	MetricConnectionErrors       = "connection_errors_total"
	MetricWebsocketMessages      = "websocket_messages_total"

	// Common labels.
	LabelMethod    = "method"
	LabelStatus    = "status"
	LabelErrorType = "error_type"
	LabelState     = "state"
	LabelDirection = "direction"
	LabelType      = "type"
	LabelNamespace = "namespace"
	LabelBackend   = "backend"
	LabelProtocol  = "protocol"
	LabelAuthType  = "auth_type"

	// Status values.
	StatusSuccess = "success"
	StatusError   = "error"
	StatusTimeout = "timeout"

	// State values.
	StateActive = "active"
	StateIdle   = "idle"
	StateError  = "error"

	// Direction values.
	DirectionIn  = "in"
	DirectionOut = "out"

	// Type values.
	TypeRequest  = "request"
	TypeResponse = "response"
)

// MetricName generates a fully qualified metric name.
func MetricName(subsystem, metric string) string {
	return Namespace + "_" + subsystem + "_" + metric
}

// RouterMetric generates a router-specific metric name.
func RouterMetric(metric string) string {
	return MetricName(SubsystemRouter, metric)
}

// GatewayMetric generates a gateway-specific metric name.
func GatewayMetric(metric string) string {
	return MetricName(SubsystemGateway, metric)
}
