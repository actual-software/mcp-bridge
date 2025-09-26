package errors

import (
	"context"
	"net/http"
	"time"
)

// backendContextKey is a type for backend context keys.
type backendContextKey string

// Context keys for backend-specific values.
const (
	backendContextKeyName     backendContextKey = "backend.name"
	backendContextKeyProtocol backendContextKey = "backend.protocol"
)

// Error codes for backend operations.
const (
	ErrCodeConnectionFailed    = "BACKEND_CONNECTION_FAILED"
	ErrCodeConnectionTimeout   = "BACKEND_CONNECTION_TIMEOUT"
	ErrCodeWriteFailed         = "BACKEND_WRITE_FAILED"
	ErrCodeReadFailed          = "BACKEND_READ_FAILED"
	ErrCodeProtocolMismatch    = "BACKEND_PROTOCOL_MISMATCH"
	ErrCodeBackendUnavailable  = "BACKEND_UNAVAILABLE"
	ErrCodeBackendOverloaded   = "BACKEND_OVERLOADED"
	ErrCodeInvalidResponse     = "BACKEND_INVALID_RESPONSE"
	ErrCodeStreamError         = "BACKEND_STREAM_ERROR"
	ErrCodeWebSocketUpgrade    = "BACKEND_WEBSOCKET_UPGRADE"
	ErrCodeSSEConnectionFailed = "BACKEND_SSE_CONNECTION_FAILED"
	ErrCodeStdioFailed         = "BACKEND_STDIO_FAILED"
)

// CreateConnectionError creates an error for connection failures.
func CreateConnectionError(backendName, protocol, message string, cause error) *GatewayError {
	var err *GatewayError
	if cause != nil {
		err = Wrap(cause, message)
	} else {
		err = New(TypeInternal, message)
	}

	return err.
		WithComponent("backend").
		WithOperation("connect").
		WithContext("backend_name", backendName).
		WithContext("protocol", protocol).
		WithContext("code", ErrCodeConnectionFailed).
		WithHTTPStatus(http.StatusServiceUnavailable).
		AsRetryable()
}

// CreateConnectionTimeoutError creates an error for connection timeouts.
func CreateConnectionTimeoutError(backendName string, timeout time.Duration) *GatewayError {
	return NewTimeoutError("connection to backend "+backendName, nil).
		WithComponent("backend").
		WithContext("backend_name", backendName).
		WithContext("timeout", timeout.String()).
		WithContext("code", ErrCodeConnectionTimeout)
}

// WrapWriteError wraps an error that occurred during write operations.
func WrapWriteError(ctx context.Context, err error, backendName string, dataSize int) *GatewayError {
	return WrapContext(ctx, err, "failed to write to backend").
		WithComponent("backend").
		WithOperation("write").
		WithContext("backend_name", backendName).
		WithContext("data_size", dataSize).
		WithContext("code", ErrCodeWriteFailed)
}

// WrapReadError wraps an error that occurred during read operations.
func WrapReadError(ctx context.Context, err error, backendName string) *GatewayError {
	return WrapContext(ctx, err, "failed to read from backend").
		WithComponent("backend").
		WithOperation("read").
		WithContext("backend_name", backendName).
		WithContext("code", ErrCodeReadFailed)
}

// CreateProtocolMismatchError creates an error for protocol mismatches.
func CreateProtocolMismatchError(expected, actual string) *GatewayError {
	return New(TypeValidation, "protocol mismatch: expected "+expected+", got "+actual).
		WithComponent("backend").
		WithContext("expected_protocol", expected).
		WithContext("actual_protocol", actual).
		WithContext("code", ErrCodeProtocolMismatch).
		WithHTTPStatus(http.StatusBadRequest)
}

// CreateBackendUnavailableError creates an error when backend is unavailable.
func CreateBackendUnavailableError(backendName string, reason string) *GatewayError {
	return New(TypeUnavailable, "backend "+backendName+" unavailable: "+reason).
		WithComponent("backend").
		WithContext("backend_name", backendName).
		WithContext("reason", reason).
		WithContext("code", ErrCodeBackendUnavailable).
		WithHTTPStatus(http.StatusServiceUnavailable)
}

// CreateBackendOverloadedError creates an error when backend is overloaded.
func CreateBackendOverloadedError(backendName string, queueSize int) *GatewayError {
	return New(TypeRateLimit, "backend "+backendName+" is overloaded").
		WithComponent("backend").
		WithContext("backend_name", backendName).
		WithContext("queue_size", queueSize).
		WithContext("code", ErrCodeBackendOverloaded).
		WithHTTPStatus(http.StatusServiceUnavailable)
}

// WrapInvalidResponseError wraps an error for invalid backend responses.
func WrapInvalidResponseError(ctx context.Context, err error, backendName string, responseType string) *GatewayError {
	return WrapContext(ctx, err, "invalid response from backend").
		WithComponent("backend").
		WithOperation("process_response").
		WithContext("backend_name", backendName).
		WithContext("response_type", responseType).
		WithContext("code", ErrCodeInvalidResponse)
}

// WrapStreamError wraps streaming-related errors.
func WrapStreamError(ctx context.Context, err error, backendName, operation string) *GatewayError {
	return WrapContextf(ctx, err, "stream %s failed", operation).
		WithComponent("backend").
		WithOperation("stream_"+operation).
		WithContext("backend_name", backendName).
		WithContext("code", ErrCodeStreamError)
}

// WebSocket-specific errors

// WrapWebSocketUpgradeError wraps WebSocket upgrade failures.
func WrapWebSocketUpgradeError(ctx context.Context, err error, backendName string, statusCode int) *GatewayError {
	return WrapContext(ctx, err, "WebSocket upgrade failed").
		WithComponent("backend_websocket").
		WithOperation("upgrade").
		WithContext("backend_name", backendName).
		WithContext("status_code", statusCode).
		WithContext("code", ErrCodeWebSocketUpgrade).
		WithHTTPStatus(http.StatusBadGateway)
}

// SSE-specific errors

// WrapSSEConnectionError wraps SSE connection failures.
func WrapSSEConnectionError(ctx context.Context, err error, backendName string) *GatewayError {
	return WrapContext(ctx, err, "SSE connection failed").
		WithComponent("backend_sse").
		WithOperation("connect").
		WithContext("backend_name", backendName).
		WithContext("code", ErrCodeSSEConnectionFailed).
		WithHTTPStatus(http.StatusBadGateway)
}

// Stdio-specific errors

// WrapStdioError wraps stdio backend errors.
func WrapStdioError(ctx context.Context, err error, operation string) *GatewayError {
	return WrapContextf(ctx, err, "stdio %s failed", operation).
		WithComponent("backend_stdio").
		WithOperation("stdio_"+operation).
		WithContext("code", ErrCodeStdioFailed)
}

// EnrichContextWithBackend adds backend information to context for error tracking.
func EnrichContextWithBackend(ctx context.Context, backendName, protocol string) context.Context {
	if backendName != "" {
		ctx = context.WithValue(ctx, backendContextKeyName, backendName)
	}

	if protocol != "" {
		ctx = context.WithValue(ctx, backendContextKeyProtocol, protocol)
	}

	return ctx
}
