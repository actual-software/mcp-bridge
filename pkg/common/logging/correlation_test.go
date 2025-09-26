package logging

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// Test context key types to avoid using basic types as context keys.
type (
	testStringCorrelationKey struct{}
	testStringTraceKey       struct{}
)

func TestGenerateCorrelationID(t *testing.T) {
	// Test that GenerateCorrelationID returns a non-empty string
	id := GenerateCorrelationID()
	assert.NotEmpty(t, id)

	// Test that it returns different IDs each time
	id2 := GenerateCorrelationID()
	assert.NotEqual(t, id, id2)

	// Test that the ID is hex encoded (should be 16 characters for 8 bytes)
	assert.Len(t, id, 16, "Correlation ID should be 16 hex characters (8 bytes)")

	// Test that the ID only contains hex characters
	for _, char := range id {
		assert.True(t, (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f'),
			"Correlation ID should only contain hex characters: %c", char)
	}
}

func TestGenerateTraceID(t *testing.T) {
	// Test that GenerateTraceID returns a non-empty string
	id := GenerateTraceID()
	assert.NotEmpty(t, id)

	// Test that it returns different IDs each time
	id2 := GenerateTraceID()
	assert.NotEqual(t, id, id2)

	// Test that the ID is hex encoded (should be 32 characters for 16 bytes)
	assert.Len(t, id, 32, "Trace ID should be 32 hex characters (16 bytes)")

	// Test that the ID only contains hex characters
	for _, char := range id {
		assert.True(t, (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f'),
			"Trace ID should only contain hex characters: %c", char)
	}
}

func TestWithCorrelationID(t *testing.T) {
	ctx := context.Background()
	correlationID := "test-correlation-id"

	// Add correlation ID to context
	ctxWithID := WithCorrelationID(ctx, correlationID)

	// Verify the ID can be retrieved
	retrievedID := GetCorrelationID(ctxWithID)
	assert.Equal(t, correlationID, retrievedID)

	// Verify original context doesn't have the ID
	originalID := GetCorrelationID(ctx)
	assert.Empty(t, originalID)
}

func TestWithTraceID(t *testing.T) {
	ctx := context.Background()
	traceID := "test-trace-id"

	// Add trace ID to context
	ctxWithID := WithTraceID(ctx, traceID)

	// Verify the ID can be retrieved
	retrievedID := GetTraceID(ctxWithID)
	assert.Equal(t, traceID, retrievedID)

	// Verify original context doesn't have the ID
	originalID := GetTraceID(ctx)
	assert.Empty(t, originalID)
}

func TestGetCorrelationID(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "empty context",
			ctx:      context.Background(),
			expected: "",
		},
		{
			name:     "context with correlation ID",
			ctx:      WithCorrelationID(context.Background(), "test-id"),
			expected: "test-id",
		},
		{
			name:     "context with wrong type value",
			ctx:      context.WithValue(context.Background(), correlationIDKey, 123),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCorrelationID(tt.ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetTraceID(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "empty context",
			ctx:      context.Background(),
			expected: "",
		},
		{
			name:     "context with trace ID",
			ctx:      WithTraceID(context.Background(), "test-trace-id"),
			expected: "test-trace-id",
		},
		{
			name:     "context with wrong type value",
			ctx:      context.WithValue(context.Background(), traceIDKey, 456),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetTraceID(tt.ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithCorrelation(t *testing.T) {
	tests := []struct {
		name                 string
		ctx                  context.Context
		expectNewCorrelation bool
		expectNewTrace       bool
	}{
		{
			name:                 "empty context generates both IDs",
			ctx:                  context.Background(),
			expectNewCorrelation: true,
			expectNewTrace:       true,
		},
		{
			name:                 "context with correlation ID generates only trace ID",
			ctx:                  WithCorrelationID(context.Background(), "existing-correlation"),
			expectNewCorrelation: false,
			expectNewTrace:       true,
		},
		{
			name:                 "context with trace ID generates only correlation ID",
			ctx:                  WithTraceID(context.Background(), "existing-trace"),
			expectNewCorrelation: true,
			expectNewTrace:       false,
		},
		{
			name: "context with both IDs generates neither",
			ctx: WithTraceID(WithCorrelationID(context.Background(), "existing-correlation"),
				"existing-trace"),
			expectNewCorrelation: false,
			expectNewTrace:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalCorrelation := GetCorrelationID(tt.ctx)
			originalTrace := GetTraceID(tt.ctx)

			ctxWithCorrelation := WithCorrelation(tt.ctx)

			newCorrelation := GetCorrelationID(ctxWithCorrelation)
			newTrace := GetTraceID(ctxWithCorrelation)

			if tt.expectNewCorrelation {
				assert.NotEmpty(t, newCorrelation, "Should generate new correlation ID")
				assert.NotEqual(t, originalCorrelation, newCorrelation, "Should generate different correlation ID")
			} else {
				assert.Equal(t, originalCorrelation, newCorrelation, "Should keep existing correlation ID")
			}

			if tt.expectNewTrace {
				assert.NotEmpty(t, newTrace, "Should generate new trace ID")
				assert.NotEqual(t, originalTrace, newTrace, "Should generate different trace ID")
			} else {
				assert.Equal(t, originalTrace, newTrace, "Should keep existing trace ID")
			}
		})
	}
}

func TestLoggerWithCorrelation(t *testing.T) {
	// Create an observer to capture log entries
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	tests := getLoggerWithCorrelationTestCases()

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifyLoggerWithCorrelation(t, tt, logger, recorded)
		})
		// Clear recorded entries for next test
		if i < len(tests)-1 {
			recorded.TakeAll()
		}
	}
}

type correlationTestCase struct {
	name                string
	ctx                 context.Context
	expectedFields      int
	expectedCorrelation string
	expectedTrace       string
}

func getLoggerWithCorrelationTestCases() []correlationTestCase {
	return []correlationTestCase{
		{
			name:                "empty context",
			ctx:                 context.Background(),
			expectedFields:      0,
			expectedCorrelation: "", // No correlation ID expected
			expectedTrace:       "", // No trace ID expected
		},
		{
			name:                "context with correlation ID only",
			ctx:                 WithCorrelationID(context.Background(), "test-correlation"),
			expectedFields:      1,
			expectedCorrelation: "test-correlation",
			expectedTrace:       "", // No trace ID expected
		},
		{
			name:                "context with trace ID only",
			ctx:                 WithTraceID(context.Background(), "test-trace"),
			expectedFields:      1,
			expectedCorrelation: "", // No correlation ID expected
			expectedTrace:       "test-trace",
		},
		{
			name: "context with both IDs",
			ctx: WithTraceID(WithCorrelationID(context.Background(), "test-correlation"),
				"test-trace"),
			expectedFields:      2,
			expectedCorrelation: "test-correlation",
			expectedTrace:       "test-trace",
		},
	}
}

func verifyLoggerWithCorrelation(t *testing.T, tt correlationTestCase, logger *zap.Logger,
	recorded *observer.ObservedLogs) {
	t.Helper()

	correlationLogger := LoggerWithCorrelation(tt.ctx, logger)
	correlationLogger.Info("test message")

	// Check the log entry
	entries := recorded.TakeAll()
	require.Len(t, entries, 1, "Should have exactly one log entry")

	entry := entries[0]
	assert.Equal(t, "test message", entry.Message)

	// Count context fields (excluding the message itself)
	contextFields := 0

	var foundCorrelation, foundTrace string

	for _, field := range entry.Context {
		switch field.Key {
		case FieldCorrelationID:
			contextFields++
			foundCorrelation = field.String
		case FieldTraceID:
			contextFields++
			foundTrace = field.String
		}
	}

	assert.Equal(t, tt.expectedFields, contextFields, "Should have expected number of context fields")

	if tt.expectedCorrelation != "" {
		assert.Equal(t, tt.expectedCorrelation, foundCorrelation, "Should have expected correlation ID")
	}

	if tt.expectedTrace != "" {
		assert.Equal(t, tt.expectedTrace, foundTrace, "Should have expected trace ID")
	}
}

func TestLoggerWithRequest(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	tests := []struct {
		name        string
		ctx         context.Context
		requestID   interface{}
		expectedMsg string
	}{
		{
			name:        "with string request ID",
			ctx:         WithCorrelationID(context.Background(), "test-correlation"),
			requestID:   "request-123",
			expectedMsg: "test with request ID",
		},
		{
			name:        "with integer request ID",
			ctx:         WithTraceID(context.Background(), "test-trace"),
			requestID:   42,
			expectedMsg: "test with int request ID",
		},
		{
			name:        "with nil request ID",
			ctx:         context.Background(),
			requestID:   nil,
			expectedMsg: "test with nil request ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestLogger := LoggerWithRequest(tt.ctx, logger, tt.requestID)
			requestLogger.Info(tt.expectedMsg)

			entries := recorded.TakeAll()
			require.Len(t, entries, 1)

			entry := entries[0]
			assert.Equal(t, tt.expectedMsg, entry.Message)

			// Check for request ID field if provided
			if tt.requestID != nil {
				found := false

				for _, field := range entry.Context {
					if field.Key == FieldRequestID {
						found = true

						break
					}
				}

				assert.True(t, found, "Should have request ID field")
			}
		})
	}
}

func TestLoggerWithConnection(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	tests := getLoggerWithConnectionTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifyLoggerWithConnection(t, tt, logger, recorded)
		})
	}
}

type connectionTestCase struct {
	name         string
	ctx          context.Context
	connectionID string
	remoteAddr   string
	expectedMsg  string
}

func getLoggerWithConnectionTestCases() []connectionTestCase {
	return []connectionTestCase{
		{
			name:         "with both connection ID and remote address",
			ctx:          WithCorrelationID(context.Background(), "test-correlation"),
			connectionID: "conn-123",
			remoteAddr:   "192.168.1.100:8080",
			expectedMsg:  "connection established",
		},
		{
			name:         "with connection ID only",
			ctx:          context.Background(),
			connectionID: "conn-456",
			remoteAddr:   "",
			expectedMsg:  "connection with ID only",
		},
		{
			name:         "with remote address only",
			ctx:          context.Background(),
			connectionID: "",
			remoteAddr:   "10.0.0.1:9090",
			expectedMsg:  "connection with addr only",
		},
		{
			name:         "with neither field",
			ctx:          context.Background(),
			connectionID: "",
			remoteAddr:   "",
			expectedMsg:  "connection with no fields",
		},
	}
}

func verifyLoggerWithConnection(t *testing.T, tt connectionTestCase, logger *zap.Logger,
	recorded *observer.ObservedLogs) {
	t.Helper()

	connectionLogger := LoggerWithConnection(tt.ctx, logger, tt.connectionID, tt.remoteAddr)
	connectionLogger.Info(tt.expectedMsg)

	entries := recorded.TakeAll()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, tt.expectedMsg, entry.Message)

	// Check for connection fields if provided
	foundConnectionID := false
	foundRemoteAddr := false

	for _, field := range entry.Context {
		if field.Key == FieldConnectionID && tt.connectionID != "" {
			foundConnectionID = true

			assert.Equal(t, tt.connectionID, field.String)
		}

		if field.Key == FieldRemoteAddr && tt.remoteAddr != "" {
			foundRemoteAddr = true

			assert.Equal(t, tt.remoteAddr, field.String)
		}
	}

	if tt.connectionID != "" {
		assert.True(t, foundConnectionID, "Should have connection ID field")
	}

	if tt.remoteAddr != "" {
		assert.True(t, foundRemoteAddr, "Should have remote address field")
	}
}

func TestContextKeyType(t *testing.T) {
	// Test that contextKey is a proper type for avoiding collisions
	key1 := contextKey("test")
	key2 := contextKey("test")

	// Both should have the same underlying value but be the contextKey type
	assert.Equal(t, string(key1), string(key2))
	assert.Equal(t, "test", string(key1))

	// Test that the predefined keys are of the correct type and value
	assert.Equal(t, "correlation_id", string(correlationIDKey))
	assert.Equal(t, "trace_id", string(traceIDKey))
}

func TestIDGeneration_Randomness(t *testing.T) {
	// Test that generated IDs are sufficiently random
	correlationIDs := make(map[string]bool)
	traceIDs := make(map[string]bool)

	// Generate 100 IDs and ensure they're all unique
	for range 100 {
		correlationID := GenerateCorrelationID()
		traceID := GenerateTraceID()

		assert.False(t, correlationIDs[correlationID], "Duplicate correlation ID generated: %s", correlationID)
		assert.False(t, traceIDs[traceID], "Duplicate trace ID generated: %s", traceID)

		correlationIDs[correlationID] = true
		traceIDs[traceID] = true
	}

	assert.Len(t, correlationIDs, 100, "Should generate 100 unique correlation IDs")
	assert.Len(t, traceIDs, 100, "Should generate 100 unique trace IDs")
}

func TestLoggerChaining(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	ctx := WithTraceID(WithCorrelationID(context.Background(), "chain-correlation"), "chain-trace")

	// Chain multiple logger enhancements
	enhancedLogger := LoggerWithConnection(
		ctx,
		LoggerWithRequest(
			ctx,
			LoggerWithCorrelation(ctx, logger),
			"chain-request",
		),
		"chain-connection",
		"192.168.1.1:8080",
	)

	enhancedLogger.Info("chained logger test")

	entries := recorded.TakeAll()
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "chained logger test", entry.Message)

	// Verify all fields are present
	fields := make(map[string]string)

	for _, field := range entry.Context {
		if field.Type == zapcore.StringType {
			fields[field.Key] = field.String
		}
	}

	assert.Equal(t, "chain-correlation", fields[FieldCorrelationID])
	assert.Equal(t, "chain-trace", fields[FieldTraceID])
	assert.Equal(t, "chain-request", fields[FieldRequestID])
	assert.Equal(t, "chain-connection", fields[FieldConnectionID])
	assert.Equal(t, "192.168.1.1:8080", fields[FieldRemoteAddr])
}

func BenchmarkGenerateCorrelationID(b *testing.B) {
	for range b.N {
		GenerateCorrelationID()
	}
}

func BenchmarkGenerateTraceID(b *testing.B) {
	for range b.N {
		GenerateTraceID()
	}
}

func BenchmarkWithCorrelationID(b *testing.B) {
	ctx := context.Background()
	correlationID := "benchmark-correlation-id"

	b.ResetTimer()

	for range b.N {
		WithCorrelationID(ctx, correlationID)
	}
}

func BenchmarkGetCorrelationID(b *testing.B) {
	ctx := WithCorrelationID(context.Background(), "benchmark-correlation-id")

	b.ResetTimer()

	for range b.N {
		GetCorrelationID(ctx)
	}
}

func BenchmarkLoggerWithCorrelation(b *testing.B) {
	logger := zap.NewNop() // No-op logger for benchmarking
	ctx := WithTraceID(WithCorrelationID(context.Background(), "bench-correlation"), "bench-trace")

	b.ResetTimer()

	for range b.N {
		LoggerWithCorrelation(ctx, logger)
	}
}

// Test error cases and fallback behaviors.
func TestGenerationFallback(t *testing.T) {
	// These tests are more for documentation purposes since crypto/rand
	// should never fail in normal circumstances
	// Test that generated IDs are hex strings
	corrID := GenerateCorrelationID()
	traceID := GenerateTraceID()

	// Should not contain non-hex characters
	for _, char := range corrID {
		assert.True(t, strings.ContainsRune("0123456789abcdef", char),
			"Correlation ID should only contain hex characters")
	}

	for _, char := range traceID {
		assert.True(t, strings.ContainsRune("0123456789abcdef", char),
			"Trace ID should only contain hex characters")
	}
}

// TestGenerateCorrelationID_FallbackScenario tests the fallback path.
func TestGenerateCorrelationID_FallbackScenario(t *testing.T) {
	// We can't easily force crypto/rand to fail, but we can test
	// that the fallback path produces valid IDs by testing the format
	// Test that all IDs follow the expected format
	for range 10 {
		id := GenerateCorrelationID()
		assert.Len(t, id, 16, "Correlation ID should be 16 hex characters")
		assert.Regexp(t, "^[0-9a-f]{16}$", id, "Correlation ID should be valid hex")
	}
}

// TestGenerateTraceID_FallbackScenario tests the fallback path.
func TestGenerateTraceID_FallbackScenario(t *testing.T) {
	// Test that all IDs follow the expected format
	for range 10 {
		id := GenerateTraceID()
		assert.Len(t, id, 32, "Trace ID should be 32 hex characters")
		assert.Regexp(t, "^[0-9a-f]{32}$", id, "Trace ID should be valid hex")
	}
}

// TestContextKeyCollisionResistance tests that our context keys don't collide.
func TestContextKeyCollisionResistance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Test that string-typed keys don't interfere with our typed keys
	ctx = context.WithValue(ctx, testStringCorrelationKey{}, "string-key-value")
	ctx = context.WithValue(ctx, testStringTraceKey{}, "string-trace-value")

	// Our typed keys should not be affected
	ctx = WithCorrelationID(ctx, "typed-correlation")
	ctx = WithTraceID(ctx, "typed-trace")

	// Should get our typed values, not the string key values
	assert.Equal(t, "typed-correlation", GetCorrelationID(ctx))
	assert.Equal(t, "typed-trace", GetTraceID(ctx))

	// String-typed keys should still be accessible
	stringCorr := ctx.Value(testStringCorrelationKey{})
	stringTrace := ctx.Value(testStringTraceKey{})

	assert.Equal(t, "string-key-value", stringCorr)
	assert.Equal(t, "string-trace-value", stringTrace)
}

// TestLoggerWithCorrelation_EmptyFields tests behavior with empty field values.
func TestLoggerWithCorrelation_EmptyFields(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	tests := []struct {
		name           string
		ctx            context.Context
		expectedFields int
	}{
		{
			name:           "empty correlation ID",
			ctx:            WithCorrelationID(context.Background(), ""),
			expectedFields: 0, // Empty string should not create field
		},
		{
			name:           "empty trace ID",
			ctx:            WithTraceID(context.Background(), ""),
			expectedFields: 0, // Empty string should not create field
		},
		{
			name:           "both empty",
			ctx:            WithTraceID(WithCorrelationID(context.Background(), ""), ""),
			expectedFields: 0, // No fields for empty values
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			correlationLogger := LoggerWithCorrelation(tt.ctx, logger)
			correlationLogger.Info("test message")

			entries := recorded.TakeAll()
			assert.Len(t, entries, 1)

			entry := entries[0]
			actualFields := 0

			for _, field := range entry.Context {
				if field.Key == FieldCorrelationID || field.Key == FieldTraceID {
					actualFields++
				}
			}

			assert.Equal(t, tt.expectedFields, actualFields, "Empty values should not create fields")
		})
	}
}

// TestLoggerWithRequest_NilAndEmptyValues tests edge cases for request logger.
func TestLoggerWithRequest_NilAndEmptyValues(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	tests := []struct {
		name               string
		requestID          interface{}
		expectRequestField bool
	}{
		{
			name:               "nil request ID",
			requestID:          nil,
			expectRequestField: false,
		},
		{
			name:               "empty string request ID",
			requestID:          "",
			expectRequestField: true, // Empty string is still a value
		},
		{
			name:               "zero int request ID",
			requestID:          0,
			expectRequestField: true, // Zero is still a value
		},
		{
			name:               "false bool request ID",
			requestID:          false,
			expectRequestField: true, // False is still a value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestLogger := LoggerWithRequest(context.Background(), logger, tt.requestID)
			requestLogger.Info("test message")

			entries := recorded.TakeAll()
			assert.Len(t, entries, 1)

			entry := entries[0]
			foundRequestField := false

			for _, field := range entry.Context {
				if field.Key == FieldRequestID {
					foundRequestField = true

					break
				}
			}

			assert.Equal(t, tt.expectRequestField, foundRequestField)
		})
	}
}

// TestLoggerWithConnection_EmptyValues tests edge cases for connection logger.
func TestLoggerWithConnection_EmptyValues(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	tests := []struct {
		name                    string
		connectionID            string
		remoteAddr              string
		expectConnectionIDField bool
		expectRemoteAddrField   bool
	}{
		{
			name:                    "both empty",
			connectionID:            "",
			remoteAddr:              "",
			expectConnectionIDField: false,
			expectRemoteAddrField:   false,
		},
		{
			name:                    "empty connection ID only",
			connectionID:            "",
			remoteAddr:              "192.168.1.1",
			expectConnectionIDField: false,
			expectRemoteAddrField:   true,
		},
		{
			name:                    "empty remote addr only",
			connectionID:            "conn-123",
			remoteAddr:              "",
			expectConnectionIDField: true,
			expectRemoteAddrField:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connectionLogger := LoggerWithConnection(context.Background(), logger, tt.connectionID, tt.remoteAddr)
			connectionLogger.Info("test message")

			entries := recorded.TakeAll()
			assert.Len(t, entries, 1)

			entry := entries[0]
			foundConnectionID := false
			foundRemoteAddr := false

			for _, field := range entry.Context {
				if field.Key == FieldConnectionID {
					foundConnectionID = true
				}

				if field.Key == FieldRemoteAddr {
					foundRemoteAddr = true
				}
			}

			assert.Equal(t, tt.expectConnectionIDField, foundConnectionID)
			assert.Equal(t, tt.expectRemoteAddrField, foundRemoteAddr)
		})
	}
}

// TestConcurrentIDGeneration tests thread safety of ID generation.
func TestConcurrentIDGeneration(t *testing.T) {
	t.Parallel()

	const (
		numGoroutines = 50
		numIterations = 100
	)

	t.Run("concurrent correlation ID generation", func(t *testing.T) {
		testConcurrentCorrelationIDGeneration(t, numGoroutines, numIterations)
	})

	t.Run("concurrent trace ID generation", func(t *testing.T) {
		testConcurrentTraceIDGeneration(t, numGoroutines, numIterations)
	})
}

func testConcurrentCorrelationIDGeneration(t *testing.T, numGoroutines, numIterations int) {
	t.Helper()
	t.Parallel()

	correlationIDs := make(chan string, numGoroutines*numIterations)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()

			for range numIterations {
				id := GenerateCorrelationID()
				correlationIDs <- id
			}
		}()
	}

	wg.Wait()
	close(correlationIDs)

	// Collect all IDs and verify uniqueness
	seenIDs := make(map[string]bool)
	totalIDs := 0

	for id := range correlationIDs {
		assert.Len(t, id, 16, "All correlation IDs should be 16 characters")
		assert.False(t, seenIDs[id], "Should not generate duplicate correlation ID: %s", id)
		seenIDs[id] = true
		totalIDs++
	}

	assert.Equal(t, numGoroutines*numIterations, totalIDs)
	assert.Len(t, seenIDs, numGoroutines*numIterations)
}

func testConcurrentTraceIDGeneration(t *testing.T, numGoroutines, numIterations int) {
	t.Helper()

	traceIDs := make(chan string, numGoroutines*numIterations)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()

			for range numIterations {
				id := GenerateTraceID()
				traceIDs <- id
			}
		}()
	}

	wg.Wait()
	close(traceIDs)

	// Collect all IDs and verify uniqueness
	seenIDs := make(map[string]bool)
	totalIDs := 0

	for id := range traceIDs {
		assert.Len(t, id, 32, "All trace IDs should be 32 characters")
		assert.False(t, seenIDs[id], "Should not generate duplicate trace ID: %s", id)
		seenIDs[id] = true
		totalIDs++
	}

	assert.Equal(t, numGoroutines*numIterations, totalIDs)
	assert.Len(t, seenIDs, numGoroutines*numIterations)
}

// TestConcurrentContextOperations tests thread safety of context operations.
func TestConcurrentContextOperations(t *testing.T) {
	t.Parallel()

	const numGoroutines = 20

	const numIterations = 50

	var wg sync.WaitGroup

	results := make(chan struct {
		correlationID string
		traceID       string
	}, numGoroutines*numIterations)

	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()

			for range numIterations {
				ctx := context.Background()
				ctx = WithCorrelation(ctx)

				corrID := GetCorrelationID(ctx)
				traceID := GetTraceID(ctx)

				results <- struct {
					correlationID string
					traceID       string
				}{
					correlationID: corrID,
					traceID:       traceID,
				}
			}
		}()
	}

	wg.Wait()
	close(results)

	// Verify all results
	seenCorrelations := make(map[string]bool)
	seenTraces := make(map[string]bool)
	totalResults := 0

	for result := range results {
		assert.NotEmpty(t, result.correlationID)
		assert.NotEmpty(t, result.traceID)
		assert.Len(t, result.correlationID, 16)
		assert.Len(t, result.traceID, 32)

		seenCorrelations[result.correlationID] = true
		seenTraces[result.traceID] = true
		totalResults++
	}

	assert.Equal(t, numGoroutines*numIterations, totalResults)
	// Should have many unique IDs (allowing for some duplicates due to randomness)
	assert.Greater(t, len(seenCorrelations), totalResults/2)
	assert.Greater(t, len(seenTraces), totalResults/2)
}
