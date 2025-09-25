// Package logging defines standardized logging field names and service identifiers for MCP components.
package logging

// StandardFields defines common logging field names for MCP components.
const (
	// Service identification.
	FieldService   = "service"
	FieldComponent = "component"
	FieldVersion   = "version"

	// Connection and network.
	FieldConnectionID   = "connection_id"
	FieldConnectionType = "connection_type"
	FieldRemoteAddr     = "remote_addr"
	FieldLocalAddr      = "local_addr"
	FieldProtocol       = "protocol"
	FieldURL            = "url"
	FieldEndpoint       = "endpoint"
	FieldGateway        = "gateway"
	FieldBackend        = "backend"

	// Request/Response.
	FieldRequestID     = "request_id"
	FieldCorrelationID = "correlation_id"
	FieldTraceID       = "trace_id"
	FieldSpanID        = "span_id"
	FieldMethod        = "method"
	FieldNamespace     = "namespace"
	FieldStatus        = "status"
	FieldStatusCode    = "status_code"
	FieldDuration      = "duration_ms"
	FieldSize          = "size_bytes"
	FieldTimeout       = "timeout_ms"

	// Session and authentication.
	FieldSessionID = "session_id"
	FieldUserID    = "user_id"
	FieldUser      = "user"
	FieldAuthType  = "auth_type"
	FieldClientID  = "client_id"

	// Error handling.
	FieldError      = "error"
	FieldErrorType  = "error_type"
	FieldErrorCode  = "error_code"
	FieldRetryCount = "retry_count"
	FieldAttempt    = "attempt"

	// Pool and resource management.
	FieldPoolSize    = "pool_size"
	FieldPoolMin     = "pool_min"
	FieldPoolMax     = "pool_max"
	FieldActiveConns = "active_connections"
	FieldIdleConns   = "idle_connections"
	FieldTotalConns  = "total_connections"

	// Rate limiting.
	FieldRateLimit     = "rate_limit"
	FieldRateBurst     = "rate_burst"
	FieldRateExceeded  = "rate_exceeded"
	FieldRateRemaining = "rate_remaining"

	// Metrics and monitoring.
	FieldMetricName  = "metric_name"
	FieldMetricValue = "metric_value"
	FieldHealthy     = "healthy"
	FieldState       = "state"

	// Debugging.
	FieldTrace     = "trace"
	FieldDebug     = "debug"
	FieldCallStack = "call_stack"
)

// ServiceNames for standard service identification.
const (
	ServiceRouter  = "mcp-router"
	ServiceGateway = "mcp-gateway"
)
