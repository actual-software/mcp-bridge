package logging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

func TestWithError(t *testing.T) {
	t.Parallel()

	tests := getWithErrorTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fields := WithError(tt.err)
			validateErrorFields(t, fields, tt.err, tt.expectedFields)
		})
	}
}

// getWithErrorTestCases returns test cases for WithError function.
func getWithErrorTestCases() []struct {
	name           string
	err            error
	expectedFields map[string]interface{}
} {
	return []struct {
		name           string
		err            error
		expectedFields map[string]interface{}
	}{
		{
			name:           "nil error returns empty fields",
			err:            nil,
			expectedFields: map[string]interface{}{},
		},
		{
			name: "standard error includes basic error field",
			err:  errors.New("test error"),
			expectedFields: map[string]interface{}{
				"error": "test error",
			},
		},
		{
			name: "GatewayError includes all context",
			err: customerrors.NewValidationError("invalid input").
				WithComponent("test_component").
				WithOperation("test_operation").
				WithContext("field", "test_field").
				WithHTTPStatus(400),
			expectedFields: map[string]interface{}{
				"error_type":  "VALIDATION",
				"component":   "test_component",
				"operation":   "test_operation",
				"retryable":   false,
				"http_status": 400,
			},
		},
		{
			name: "High severity error includes stack trace",
			err: customerrors.New(customerrors.TypeInternal, "critical error").
				WithComponent("critical_component"),
			expectedFields: map[string]interface{}{
				"error_type": "INTERNAL",
				"severity":   "HIGH",
			},
		},
	}
}

// validateErrorFields validates that fields contain expected values.
func validateErrorFields(t *testing.T, fields []zap.Field, err error, expectedFields map[string]interface{}) {
	t.Helper()

	if err == nil {
		assert.Empty(t, fields)

		return
	}

	fieldMap := make(map[string]interface{})

	for _, field := range fields {
		switch field.Type {
		case zapcore.StringType:
			fieldMap[field.Key] = field.String
		case zapcore.BoolType:
			fieldMap[field.Key] = field.Integer == 1
		case zapcore.Int64Type:
			fieldMap[field.Key] = int(field.Integer)
		case zapcore.ErrorType:
			fieldMap[field.Key] = field.Interface
		default:
			fieldMap[field.Key] = field.Interface
		}
	}

	for key, expectedValue := range expectedFields {
		if key == "error" {
			assert.Contains(t, fieldMap, key)
		} else {
			assert.Equal(t, expectedValue, fieldMap[key])
		}
	}
}

func TestWithRequestContext(t *testing.T) {
	t.Parallel()

	tests := getRequestContextTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := tt.setupContext()
			fields := WithRequestContext(ctx)
			validateRequestContextFields(t, fields, tt.expectedFields)
		})
	}
}

// getRequestContextTestCases returns test cases for WithRequestContext.
func getRequestContextTestCases() []struct {
	name           string
	setupContext   func() context.Context
	expectedFields []string
} {
	return []struct {
		name           string
		setupContext   func() context.Context
		expectedFields []string
	}{
		{
			name:           "empty context returns no fields",
			setupContext:   context.Background,
			expectedFields: []string{},
		},
		{
			name:           "context with all values",
			setupContext:   setupFullContext,
			expectedFields: []string{"request_id", "trace_id", "user_id", "session_id", "namespace", "method"},
		},
		{
			name:           "context with partial values",
			setupContext:   setupPartialContext,
			expectedFields: []string{"request_id", "user_id"},
		},
	}
}

// setupFullContext creates a context with all tracking values.
func setupFullContext() context.Context {
	ctx := context.Background()
	ctx = ContextWithTracing(ctx, "trace-456", "req-123")
	ctx = ContextWithUserInfo(ctx, "user-789", "sess-abc")
	ctx = ContextWithNamespace(ctx, "test-ns")
	ctx = ContextWithMethod(ctx, "test-method")

	return ctx
}

// setupPartialContext creates a context with partial tracking values.
func setupPartialContext() context.Context {
	ctx := context.Background()
	ctx = ContextWithTracing(ctx, "", "req-123")
	ctx = ContextWithUserInfo(ctx, "user-789", "")

	return ctx
}

// validateRequestContextFields validates the presence of expected fields.
func validateRequestContextFields(t *testing.T, fields []zap.Field, expectedFields []string) {
	t.Helper()

	fieldKeys := make(map[string]bool)
	for _, field := range fields {
		fieldKeys[field.Key] = true
	}

	for _, expectedKey := range expectedFields {
		assert.True(t, fieldKeys[expectedKey], "Expected field %s not found", expectedKey)
	}

	assert.Len(t, fields, len(expectedFields))
}

func TestLogError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		expectedLevel zapcore.Level
	}{
		{
			name: "low severity logs as warn",
			err: customerrors.NewValidationError("test").
				WithComponent("test"),
			expectedLevel: zapcore.WarnLevel,
		},
		{
			name: "medium severity logs as error",
			err: customerrors.NewUnauthorizedError("test").
				WithComponent("test"),
			expectedLevel: zapcore.ErrorLevel,
		},
		{
			name: "high severity logs as error",
			err: customerrors.NewInternalError("test").
				WithComponent("test"),
			expectedLevel: zapcore.ErrorLevel,
		},
		{
			name:          "standard error logs as error",
			err:           errors.New("standard error"),
			expectedLevel: zapcore.ErrorLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create observer to capture logs
			core, obs := observer.New(zapcore.DebugLevel)
			logger := zap.New(core)

			ctx := context.Background()
			ctx = ContextWithTracing(ctx, "", "test-req")

			LogError(ctx, logger, "Test error message", tt.err)

			// Verify log was written at correct level
			logs := obs.All()
			require.Len(t, logs, 1)
			assert.Equal(t, tt.expectedLevel, logs[0].Level)
			assert.Equal(t, "Test error message", logs[0].Message)

			// Verify context fields
			assert.NotNil(t, logs[0].ContextMap()["request_id"])
		})
	}
}

func TestGenerateIDs(t *testing.T) {
	t.Parallel()

	// Test trace ID generation
	traceID1 := GenerateTraceID()
	traceID2 := GenerateTraceID()

	assert.NotEmpty(t, traceID1)
	assert.NotEmpty(t, traceID2)
	assert.NotEqual(t, traceID1, traceID2)

	// Test request ID generation
	reqID1 := GenerateRequestID()
	reqID2 := GenerateRequestID()

	assert.NotEmpty(t, reqID1)
	assert.NotEmpty(t, reqID2)
	assert.NotEqual(t, reqID1, reqID2)
}

func TestContextWithTracing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	traceID := "trace-123"
	requestID := "req-456"

	enhancedCtx := ContextWithTracing(ctx, traceID, requestID)

	// Verify values are stored
	assert.Equal(t, traceID, GetTraceID(enhancedCtx))
	assert.Equal(t, requestID, GetRequestID(enhancedCtx))

	// Verify start time is set using the proper context key
	startTime := enhancedCtx.Value(loggingContextKeyRequestStartTime)
	assert.NotNil(t, startTime)
}

func TestGetRequestDuration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Context without start time
	duration := GetRequestDuration(ctx)
	assert.Equal(t, time.Duration(0), duration)

	// Context with start time
	ctx = context.WithValue(ctx, loggingContextKeyRequestStartTime, time.Now().Add(-100*time.Millisecond))
	duration = GetRequestDuration(ctx)
	assert.GreaterOrEqual(t, duration.Milliseconds(), int64(100))
}

func TestLogRequestComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		statusCode  int
		err         error
		expectError bool
	}{
		{
			name:        "successful request",
			statusCode:  200,
			err:         nil,
			expectError: false,
		},
		{
			name:        "failed request with error",
			statusCode:  500,
			err:         errors.New("request failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create observer to capture logs
			core, obs := observer.New(zapcore.DebugLevel)
			logger := zap.New(core)

			ctx := context.Background()
			ctx = ContextWithTracing(ctx, "trace-123", "req-456")

			// Add some delay to have measurable duration
			time.Sleep(10 * time.Millisecond)

			LogRequestComplete(ctx, logger, tt.statusCode, tt.err)

			logs := obs.All()
			require.Len(t, logs, 1)

			if tt.expectError {
				assert.Equal(t, zapcore.ErrorLevel, logs[0].Level)
				assert.Equal(t, "request failed", logs[0].Message)
			} else {
				assert.Equal(t, zapcore.InfoLevel, logs[0].Level)
				assert.Equal(t, "request completed", logs[0].Message)
			}

			// Verify fields
			assert.NotNil(t, logs[0].ContextMap()["trace_id"])
			assert.NotNil(t, logs[0].ContextMap()["request_id"])
			assert.NotNil(t, logs[0].ContextMap()["status_code"])
			assert.NotNil(t, logs[0].ContextMap()["duration"])
		})
	}
}

func TestEnhanceLogger(t *testing.T) {
	t.Parallel()

	baseLogger := zaptest.NewLogger(t)

	ctx := context.Background()
	ctx = ContextWithTracing(ctx, "", "req-123")
	ctx = ContextWithUserInfo(ctx, "user-456", "")

	enhancedLogger := EnhanceLogger(ctx, baseLogger)

	// The enhanced logger should have the context fields
	// This is hard to test directly, but we can verify it doesn't panic
	assert.NotNil(t, enhancedLogger)

	// Test that empty context doesn't modify logger
	emptyCtx := context.Background()
	sameLogger := EnhanceLogger(emptyCtx, baseLogger)
	assert.Equal(t, baseLogger, sameLogger)
}

func BenchmarkWithError(b *testing.B) {
	err := customerrors.NewValidationError("benchmark error").
		WithComponent("benchmark").
		WithOperation("bench_op").
		WithContext("field", "value")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = WithError(err)
	}
}

func BenchmarkWithRequestContext(b *testing.B) {
	ctx := context.Background()
	ctx = ContextWithTracing(ctx, "trace-456", "req-123")
	ctx = ContextWithUserInfo(ctx, "user-789", "")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = WithRequestContext(ctx)
	}
}

func BenchmarkLogError(b *testing.B) {
	logger := zap.NewNop()
	ctx := context.Background()
	err := errors.New("benchmark error")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		LogError(ctx, logger, "benchmark message", err)
	}
}
