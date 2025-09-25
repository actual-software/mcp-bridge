package errors

import (
	"errors"
	"time"
	
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
)

const (
	// unknownValue is used when a metric label value is not available.
	unknownValue = "unknown"
)

// RecordErrorMetrics records comprehensive error metrics based on GatewayError details.
func RecordErrorMetrics(err *GatewayError, registry *metrics.Registry) {
	if err == nil || registry == nil {
		return
	}

	// Get error code from context
	code := ""
	if codeVal, ok := err.Context["code"].(string); ok {
		code = codeVal
	}
	
	if code == "" {
		code = unknownValue
	}

	// Get component and operation, with defaults
	component := err.Component
	if component == "" {
		component = unknownValue
	}
	
	operation := err.Operation
	if operation == "" {
		operation = unknownValue
	}

	// Record basic error metrics
	registry.IncrementErrors(code, component, operation)
	registry.IncrementErrorsByType(string(err.Type))
	registry.IncrementErrorsByComponent(component)
	registry.IncrementRetryableErrors(err.Retryable)

	// Record HTTP status if available
	if err.HTTPStatus > 0 {
		registry.IncrementErrorsByHTTPStatus(err.HTTPStatus)
	}

	// Record severity
	severityStr := getSeverityString(err.Severity)
	registry.IncrementErrorsBySeverity(severityStr)

	// Record by operation
	registry.IncrementErrorsByOperation(operation, component)
}

// RecordError is a helper to record error metrics if the error is a GatewayError.
func RecordError(err error, registry *metrics.Registry) {
	var gwErr *GatewayError
	if errors.As(err, &gwErr) {
		RecordErrorMetrics(gwErr, registry)
	}
}

// RecordErrorWithLatency records error metrics with latency measurement.
func RecordErrorWithLatency(err error, registry *metrics.Registry, startTime time.Time) {
	var gwErr *GatewayError
	if errors.As(err, &gwErr) {
		RecordErrorMetrics(gwErr, registry)
		
		// Record error handling latency
		duration := time.Since(startTime)
		registry.RecordErrorLatency(string(gwErr.Type), gwErr.Component, duration)
	}
}

// RecordErrorRecovery records when an error is recovered from.
func RecordErrorRecovery(err error, recovered bool, registry *metrics.Registry) {
	var gwErr *GatewayError
	
	errorType := unknownValue
	if errors.As(err, &gwErr) {
		errorType = string(gwErr.Type)
	}
	
	registry.IncrementErrorRecovery(recovered, errorType)
}

// getSeverityString converts severity enum to string.
func getSeverityString(severity Severity) string {
	switch severity {
	case SeverityLow:
		return "low"
	case SeverityMedium:
		return "medium"
	case SeverityHigh:
		return "high"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown_" + string(severity )
	}
}
