package errors

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
)

func TestRecordErrorMetrics(t *testing.T) {
	t.Parallel()

	tests := getErrorMetricsTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := metrics.InitializeMetricsRegistry()
			require.NotNil(t, registry)

			RecordErrorMetrics(tt.err, registry)

			tt.validate(t, registry)
		})
	}
}

// getErrorMetricsTestCases returns test cases for error metrics recording.
func getErrorMetricsTestCases() []struct {
	name     string
	err      *GatewayError
	validate func(t *testing.T, registry *metrics.Registry)
} {
	validateRegistry := getValidateRegistryFunc()
	basicCases := getBasicErrorTestCases(validateRegistry)
	advancedCases := getAdvancedErrorTestCases(validateRegistry)
	
	basicCases = append(basicCases, advancedCases...)
	
	return basicCases
}

func getValidateRegistryFunc() func(t *testing.T, registry *metrics.Registry) {
	return func(t *testing.T, registry *metrics.Registry) {
		t.Helper()
		assert.NotNil(t, registry)
	}
}

func getBasicErrorTestCases(validateRegistry func(t *testing.T, registry *metrics.Registry)) []struct {
	name     string
	err      *GatewayError
	validate func(t *testing.T, registry *metrics.Registry)
} {
	return []struct {
		name     string
		err      *GatewayError
		validate func(t *testing.T, registry *metrics.Registry)
	}{
		{
			name: "records basic error metrics",
			err: NewValidationError("invalid input").
				WithComponent("validator").
				WithOperation("validate_request").
				WithContext("code", "VALIDATION_ERROR"),
			validate: validateRegistry,
		},
		{
			name: "records error with HTTP status",
			err: NewUnauthorizedError("invalid token").
				WithComponent("auth").
				WithOperation("authenticate").
				WithHTTPStatus(401),
			validate: validateRegistry,
		},
		{
			name: "records retryable error",
			err: NewTimeoutError("database_query", nil).
				AsRetryable().
				WithComponent("database"),
			validate: validateRegistry,
		},
		{
			name:     "handles nil error gracefully",
			err:      nil,
			validate: validateRegistry,
		},
	}
}

func getAdvancedErrorTestCases(validateRegistry func(t *testing.T, registry *metrics.Registry)) []struct {
	name     string
	err      *GatewayError
	validate func(t *testing.T, registry *metrics.Registry)
} {
	return []struct {
		name     string
		err      *GatewayError
		validate func(t *testing.T, registry *metrics.Registry)
	}{
		{
			name: "records error with all fields",
			err: New(TypeInternal, "database connection failed").
				WithComponent("database").
				WithOperation("connect").
				WithContext("code", "DB_CONNECTION_ERROR").
				WithContext("retry_count", 3).
				WithHTTPStatus(500).
				AsRetryable(),
			validate: validateRegistry,
		},
		{
			name: "handles missing code context",
			err: NewValidationError("missing field").
				WithComponent("parser").
				WithOperation("parse"),
			validate: validateRegistry,
		},
		{
			name:     "handles empty component and operation",
			err:      New(TypeNotFound, "resource not found"),
			validate: validateRegistry,
		},
	}
}

func TestRecordError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "records GatewayError",
			err: NewInternalError("system failure").
				WithComponent("system").
				WithContext("code", "SYSTEM_ERROR"),
		},
		{
			name: "ignores non-GatewayError",
			err:  errors.New("standard error"),
		},
		{
			name: "handles wrapped GatewayError",
			err: Wrap(
				NewValidationError("invalid format"),
				"request validation failed",
			),
		},
		{
			name: "handles nil error",
			err:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := metrics.InitializeMetricsRegistry()
			require.NotNil(t, registry)

			// Should not panic
			RecordError(tt.err, registry)
		})
	}
}

func TestRecordErrorWithLatency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		startTime time.Time
	}{
		{
			name: "records error with latency",
			err: NewTimeoutError("api_call", nil).
				WithComponent("api").
				WithOperation("call"),
			startTime: time.Now().Add(-100 * time.Millisecond),
		},
		{
			name:      "handles non-GatewayError",
			err:       errors.New("standard error"),
			startTime: time.Now().Add(-50 * time.Millisecond),
		},
		{
			name:      "handles nil error",
			err:       nil,
			startTime: time.Now(),
		},
		{
			name: "records error with zero latency",
			err: NewRateLimitError("too many requests").
				WithComponent("rate_limiter"),
			startTime: time.Now(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := metrics.InitializeMetricsRegistry()
			require.NotNil(t, registry)

			// Should not panic
			RecordErrorWithLatency(tt.err, registry, tt.startTime)
		})
	}
}

func TestRecordErrorRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		recovered bool
	}{
		{
			name:      "records successful recovery",
			err:       NewTimeoutError("retry_operation", nil),
			recovered: true,
		},
		{
			name:      "records failed recovery",
			err:       NewInternalError("critical failure"),
			recovered: false,
		},
		{
			name:      "handles non-GatewayError recovery",
			err:       errors.New("standard error"),
			recovered: true,
		},
		{
			name:      "handles nil error",
			err:       nil,
			recovered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := metrics.InitializeMetricsRegistry()
			require.NotNil(t, registry)

			// Should not panic
			RecordErrorRecovery(tt.err, tt.recovered, registry)
		})
	}
}

func TestGetSeverityString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		severity Severity
		expected string
	}{
		{
			name:     "low severity",
			severity: SeverityLow,
			expected: "low",
		},
		{
			name:     "medium severity",
			severity: SeverityMedium,
			expected: "medium",
		},
		{
			name:     "high severity",
			severity: SeverityHigh,
			expected: "high",
		},
		{
			name:     "critical severity",
			severity: SeverityCritical,
			expected: "critical",
		},
		{
			name:     "unknown severity",
			severity: Severity("unknown"),
			expected: "unknown_unknown",
		},
		{
			name:     "numeric severity",
			severity: Severity("5"),
			expected: "unknown_5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := getSeverityString(tt.severity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMetricsIntegration(t *testing.T) {
	t.Parallel()

	registry := metrics.InitializeMetricsRegistry()
	require.NotNil(t, registry)

	// Test various error scenarios
	errors := []error{
		NewValidationError("missing field").
			WithComponent("validator").
			WithOperation("validate_input").
			WithContext("code", "MISSING_FIELD"),
		NewUnauthorizedError("invalid credentials").
			WithComponent("auth").
			WithOperation("login").
			WithContext("code", "INVALID_CREDS"),
		NewTimeoutError("database_query", nil).
			WithComponent("database").
			AsRetryable(),
		NewRateLimitError("API rate exceeded").
			WithComponent("api").
			WithOperation("rate_check"),
		NewInternalError("system error").
			WithComponent("system").
			WithContext("code", "SYS_ERROR"),
	}

	// Record all errors
	for _, err := range errors {
		RecordError(err, registry)
	}

	// Record some with latency
	startTime := time.Now().Add(-200 * time.Millisecond)
	for _, err := range errors[:3] {
		RecordErrorWithLatency(err, registry, startTime)
	}

	// Record some recoveries
	for i, err := range errors {
		RecordErrorRecovery(err, i%2 == 0, registry)
	}

	// Verify registry is still functional
	assert.NotNil(t, registry.ErrorsTotal)
	assert.NotNil(t, registry.ErrorsByType)
	assert.NotNil(t, registry.ErrorsByComponent)
	assert.NotNil(t, registry.ErrorRetryable)
	assert.NotNil(t, registry.ErrorsByHTTPStatus)
	assert.NotNil(t, registry.ErrorsBySeverity)
	assert.NotNil(t, registry.ErrorsByOperation)
	assert.NotNil(t, registry.ErrorRecoveryTotal)
	assert.NotNil(t, registry.ErrorLatency)
}

func BenchmarkRecordErrorMetrics(b *testing.B) {
	registry := metrics.InitializeMetricsRegistry()
	err := NewValidationError("benchmark error").
		WithComponent("benchmark").
		WithOperation("bench_op").
		WithContext("code", "BENCH_ERROR").
		WithHTTPStatus(400)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			RecordErrorMetrics(err, registry)
		}
	})
}

func BenchmarkRecordErrorWithLatency(b *testing.B) {
	registry := metrics.InitializeMetricsRegistry()
	err := NewTimeoutError("benchmark_timeout", nil).
		WithComponent("benchmark")
	startTime := time.Now().Add(-100 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			RecordErrorWithLatency(err, registry, startTime)
		}
	})
}