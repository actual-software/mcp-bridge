package errors

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestBackendErrorCodes tests that all error codes are properly defined.
func TestBackendErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{
			name:     "Connection Failed",
			code:     ErrCodeConnectionFailed,
			expected: "BACKEND_CONNECTION_FAILED",
		},
		{
			name:     "Connection Timeout",
			code:     ErrCodeConnectionTimeout,
			expected: "BACKEND_CONNECTION_TIMEOUT",
		},
		{
			name:     "Write Failed",
			code:     ErrCodeWriteFailed,
			expected: "BACKEND_WRITE_FAILED",
		},
		{
			name:     "Read Failed",
			code:     ErrCodeReadFailed,
			expected: "BACKEND_READ_FAILED",
		},
		{
			name:     "Protocol Mismatch",
			code:     ErrCodeProtocolMismatch,
			expected: "BACKEND_PROTOCOL_MISMATCH",
		},
		{
			name:     "Backend Unavailable",
			code:     ErrCodeBackendUnavailable,
			expected: "BACKEND_UNAVAILABLE",
		},
		{
			name:     "Backend Overloaded",
			code:     ErrCodeBackendOverloaded,
			expected: "BACKEND_OVERLOADED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.code)
		})
	}
}

// TestCreateConnectionError tests the CreateConnectionError function.
func TestCreateConnectionError(t *testing.T) {
	cause := errors.New("connection refused")
	err := CreateConnectionError("test-backend", "websocket", "failed to connect", cause)

	assert.NotNil(t, err)
	assert.Equal(t, "backend", err.Component)
	assert.Equal(t, "connect", err.Operation)
	assert.Equal(t, "test-backend", err.Context["backend_name"])
	assert.Equal(t, "websocket", err.Context["protocol"])
	assert.Equal(t, ErrCodeConnectionFailed, err.Context["code"])
	assert.Equal(t, 503, err.HTTPStatus)
	assert.True(t, err.Retryable)
}

// TestCreateConnectionTimeoutError tests the CreateConnectionTimeoutError function.
func TestCreateConnectionTimeoutError(t *testing.T) {
	timeout := 30 * time.Second
	err := CreateConnectionTimeoutError("test-backend", timeout)

	assert.NotNil(t, err)
	assert.Equal(t, "backend", err.Component)
	assert.Equal(t, "test-backend", err.Context["backend_name"])
	assert.Equal(t, "30s", err.Context["timeout"])
	assert.Equal(t, ErrCodeConnectionTimeout, err.Context["code"])
	assert.Equal(t, TypeTimeout, err.Type)
	assert.True(t, err.Retryable)
}

// TestWrapWriteError tests the WrapWriteError function.
func TestWrapWriteError(t *testing.T) {
	ctx := context.Background()
	cause := errors.New("broken pipe")
	err := WrapWriteError(ctx, cause, "test-backend", 1024)

	assert.NotNil(t, err)
	assert.Equal(t, "backend", err.Component)
	assert.Equal(t, "write", err.Operation)
	assert.Equal(t, "test-backend", err.Context["backend_name"])
	assert.Equal(t, 1024, err.Context["data_size"])
	assert.Equal(t, ErrCodeWriteFailed, err.Context["code"])
}

// TestCreateProtocolMismatchError tests the CreateProtocolMismatchError function.
func TestCreateProtocolMismatchError(t *testing.T) {
	err := CreateProtocolMismatchError("http", "websocket")

	assert.NotNil(t, err)
	assert.Equal(t, "backend", err.Component)
	assert.Equal(t, "http", err.Context["expected_protocol"])
	assert.Equal(t, "websocket", err.Context["actual_protocol"])
	assert.Equal(t, ErrCodeProtocolMismatch, err.Context["code"])
	assert.Equal(t, 400, err.HTTPStatus)
	assert.Equal(t, TypeValidation, err.Type)
	assert.False(t, err.Retryable)
}

// TestCreateBackendUnavailableError tests the CreateBackendUnavailableError function.
func TestCreateBackendUnavailableError(t *testing.T) {
	err := CreateBackendUnavailableError("test-backend", "circuit breaker open")

	assert.NotNil(t, err)
	assert.Equal(t, "backend", err.Component)
	assert.Equal(t, "test-backend", err.Context["backend_name"])
	assert.Equal(t, "circuit breaker open", err.Context["reason"])
	assert.Equal(t, ErrCodeBackendUnavailable, err.Context["code"])
	assert.Equal(t, 503, err.HTTPStatus)
	assert.Equal(t, TypeUnavailable, err.Type)
	assert.True(t, err.Retryable)
}

// TestWrapWebSocketUpgradeError tests the WrapWebSocketUpgradeError function.
func TestWrapWebSocketUpgradeError(t *testing.T) {
	ctx := context.Background()
	cause := errors.New("upgrade failed")
	err := WrapWebSocketUpgradeError(ctx, cause, "test-backend", 426)

	assert.NotNil(t, err)
	assert.Equal(t, "backend_websocket", err.Component)
	assert.Equal(t, "upgrade", err.Operation)
	assert.Equal(t, "test-backend", err.Context["backend_name"])
	assert.Equal(t, 426, err.Context["status_code"])
	assert.Equal(t, ErrCodeWebSocketUpgrade, err.Context["code"])
	assert.Equal(t, 502, err.HTTPStatus)
}

// TestWrapSSEConnectionError tests the WrapSSEConnectionError function.
func TestWrapSSEConnectionError(t *testing.T) {
	ctx := context.Background()
	cause := errors.New("connection failed")
	err := WrapSSEConnectionError(ctx, cause, "test-backend")

	assert.NotNil(t, err)
	assert.Equal(t, "backend_sse", err.Component)
	assert.Equal(t, "connect", err.Operation)
	assert.Equal(t, "test-backend", err.Context["backend_name"])
	assert.Equal(t, ErrCodeSSEConnectionFailed, err.Context["code"])
	assert.Equal(t, 502, err.HTTPStatus)
}

// TestWrapStdioError tests the WrapStdioError function.
func TestWrapStdioError(t *testing.T) {
	ctx := context.Background()
	cause := errors.New("process exited")
	err := WrapStdioError(ctx, cause, "start")

	assert.NotNil(t, err)
	assert.Equal(t, "backend_stdio", err.Component)
	assert.Equal(t, "stdio_start", err.Operation)
	assert.Equal(t, ErrCodeStdioFailed, err.Context["code"])
}

// TestEnrichContextWithBackend tests the EnrichContextWithBackend function.
func TestEnrichContextWithBackend(t *testing.T) {
	ctx := context.Background()
	ctx = EnrichContextWithBackend(ctx, "test-backend", "http")

	assert.Equal(t, "test-backend", ctx.Value(backendContextKeyName))
	assert.Equal(t, "http", ctx.Value(backendContextKeyProtocol))
}
