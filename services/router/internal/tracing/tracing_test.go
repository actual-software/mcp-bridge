package tracing_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/tracing"
)

const (
	testIterations = 100
)


func TestInit(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	tests := createTracingInitTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tracer, err := tracing.Init(tt.config, logger)
			validateTracingInitResult(t, tt, tracer, err)
		})
	}
}

func createTracingInitTests() []struct {
	name    string
	config  config.TracingConfig
	wantErr bool
} {
	return []struct {
		name    string
		config  config.TracingConfig
		wantErr bool
	}{
		{
			name: "disabled tracing",
			config: config.TracingConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "stdout exporter",
			config: config.TracingConfig{
				Enabled:        true,
				ServiceName:    "test-router",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				ExporterType:   "stdout",
				SamplerType:    "always_on",
			},
			wantErr: false,
		},
		{
			name: "otlp exporter",
			config: config.TracingConfig{
				Enabled:        true,
				ServiceName:    "test-router",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				ExporterType:   "otlp",
				OTLPEndpoint:   "localhost:4317",
				OTLPInsecure:   true,
				SamplerType:    "traceidratio",
				SamplerParam:   0.1,
			},
			wantErr: false,
		},
		{
			name: "unknown exporter falls back to stdout",
			config: config.TracingConfig{
				Enabled:      true,
				ServiceName:  "test-router",
				ExporterType: "unknown",
				SamplerType:  "always_on",
			},
			wantErr: false,
		},
	}
}

func validateTracingInitResult(t *testing.T, tt struct {
	name    string
	config  config.TracingConfig
	wantErr bool
}, tracer *tracing.Tracer, err error) {
	t.Helper()

	if tt.wantErr {
		require.Error(t, err)
		return
	}

	require.NoError(t, err)
	require.NotNil(t, tracer)

	// Test shutdown.
	ctx := context.Background()
	err = tracer.Shutdown(ctx)
	require.NoError(t, err)
}

func TestTracerOperations(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	tracer := setupTracerForOperationTests(t, logger)
	defer func() { _ = tracer.Shutdown(context.Background()) }()

	t.Run("StartSpan", func(t *testing.T) {
		testStartSpan(t, tracer)
	})

	t.Run("StartSpanWithKind", func(t *testing.T) {
		testStartSpanWithKind(t, tracer)
	})

	t.Run("SetSpanAttributes", func(t *testing.T) {
		testSetSpanAttributes(t, tracer)
	})

	t.Run("AddSpanEvent", func(t *testing.T) {
		testAddSpanEvent(t, tracer)
	})

	t.Run("RecordError", func(t *testing.T) {
		testRecordError(t, tracer)
	})

	t.Run("HTTPHeaderPropagation", func(t *testing.T) {
		testHTTPHeaderPropagation(t, tracer)
	})

	t.Run("MapPropagation", func(t *testing.T) {
		testMapPropagation(t, tracer)
	})

	t.Run("IsEnabled", func(t *testing.T) {
		testIsEnabled(t, tracer)
	})
}

func setupTracerForOperationTests(t *testing.T, logger *zap.Logger) *tracing.Tracer {
	t.Helper()
	
	cfg := config.TracingConfig{
		Enabled:        true,
		ServiceName:    "test-router",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		ExporterType:   "stdout",
		SamplerType:    "always_on",
	}

	tracer, err := tracing.Init(cfg, logger)
	require.NoError(t, err)
	return tracer
}

func testStartSpan(t *testing.T, tracer *tracing.Tracer) {
	t.Helper()
	
	ctx := context.Background()
	spanCtx, span := tracer.StartSpan(ctx, "test-operation")
	assert.NotNil(t, span)
	assert.NotEqual(t, ctx, spanCtx)

	// Check trace and span IDs.
	traceID := tracer.GetTraceID(spanCtx)
	spanID := tracer.GetSpanID(spanCtx)

	assert.NotEmpty(t, traceID)
	assert.NotEmpty(t, spanID)

	span.End()
}

func testStartSpanWithKind(t *testing.T, tracer *tracing.Tracer) {
	t.Helper()
	
	ctx := context.Background()
	spanCtx, span := tracer.StartSpanWithKind(ctx, "test-client", trace.SpanKindClient)
	assert.NotNil(t, span)
	assert.NotEqual(t, ctx, spanCtx)
	span.End()
}

func testSetSpanAttributes(t *testing.T, tracer *tracing.Tracer) {
	t.Helper()
	
	ctx := context.Background()
	spanCtx, span := tracer.StartSpan(ctx, "test-attributes")
	defer span.End()

	attrs := []attribute.KeyValue{
		attribute.String("string.attr", "value"),
		attribute.Int("int.attr", 42),
		attribute.Float64("float.attr", 3.14),
		attribute.Bool("bool.attr", true),
	}

	tracer.SetSpanAttributes(spanCtx, attrs...)
}

func testAddSpanEvent(t *testing.T, tracer *tracing.Tracer) {
	t.Helper()
	
	ctx := context.Background()
	spanCtx, span := tracer.StartSpan(ctx, "test-events")
	defer span.End()

	tracer.AddSpanEvent(spanCtx, "test-event",
		trace.WithAttributes(attribute.String("key", "value")))
}

func testRecordError(t *testing.T, tracer *tracing.Tracer) {
	t.Helper()
	
	ctx := context.Background()
	spanCtx, span := tracer.StartSpan(ctx, "test-error")
	defer span.End()

	err := assert.AnError
	tracer.RecordError(spanCtx, err)
}

func testHTTPHeaderPropagation(t *testing.T, tracer *tracing.Tracer) {
	t.Helper()
	
	ctx := context.Background()

	// Create a span.
	spanCtx, span := tracer.StartSpan(ctx, "parent-span")
	defer span.End()

	// Inject into headers.
	headers := make(http.Header)
	tracer.InjectTraceContext(spanCtx, headers)
	assert.NotEmpty(t, headers.Get("Traceparent"))

	// Extract from headers.
	newCtx := tracer.ExtractTraceContext(context.Background(), headers)

	// Create child span from extracted context.
	childCtx, childSpan := tracer.StartSpan(newCtx, "child-span")
	defer childSpan.End()

	// Verify parent-child relationship.
	parentTraceID := tracer.GetTraceID(spanCtx)
	childTraceID := tracer.GetTraceID(childCtx)
	assert.Equal(t, parentTraceID, childTraceID)
}

func testMapPropagation(t *testing.T, tracer *tracing.Tracer) {
	t.Helper()
	
	ctx := context.Background()

	// Create a span.
	spanCtx, span := tracer.StartSpan(ctx, "parent-span")
	defer span.End()

	// Inject into map (for WebSocket headers).
	headers := make(map[string]string)
	tracer.InjectTraceContextToMap(spanCtx, headers)
	
	// Check for the header (case-insensitive).
	found := false
	for k := range headers {
		if strings.EqualFold(k, "traceparent") {
			found = true
			break
		}
	}
	assert.True(t, found, "traceparent header not found")

	// Extract from map.
	newCtx := tracer.ExtractTraceContextFromMap(context.Background(), headers)

	// Create child span.
	childCtx, childSpan := tracer.StartSpan(newCtx, "child-span")
	defer childSpan.End()

	// Verify trace continuity.
	parentTraceID := tracer.GetTraceID(spanCtx)
	childTraceID := tracer.GetTraceID(childCtx)
	assert.Equal(t, parentTraceID, childTraceID)
}

func testIsEnabled(t *testing.T, tracer *tracing.Tracer) {
	t.Helper()
	
	assert.True(t, tracer.IsEnabled())
}

func TestTracerDisabled(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)

	cfg := config.TracingConfig{
		Enabled: false,
	}

	tracer, err := tracing.Init(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, tracer)

	// All operations should be no-ops.
	ctx := context.Background()

	spanCtx, span := tracer.StartSpan(ctx, "test")
	assert.Equal(t, ctx, spanCtx) // Should return same context
	assert.NotNil(t, span)

	// These should not panic.
	tracer.SetSpanAttributes(ctx, attribute.String("key", "value"))
	tracer.AddSpanEvent(ctx, "event")
	tracer.RecordError(ctx, assert.AnError)

	// Should return empty strings.
	assert.Empty(t, tracer.GetTraceID(ctx))
	assert.Empty(t, tracer.GetSpanID(ctx))

	// Should be disabled.
	assert.False(t, tracer.IsEnabled())
}

func TestSpanAttributesFromError(t *testing.T) { 
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: 0,
		},
		{
			name:     "with error",
			err:      assert.AnError,
			expected: 2, // error=true, error.message
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attrs := tracing.SpanAttributesFromError(tt.err)
			assert.Len(t, attrs, tt.expected)
		})
	}
}

func TestSpanAttributesFromMap(t *testing.T) { 
	t.Parallel()

	m := map[string]interface{}{
		"string":  "value",
		"int":     42,
		"int64":   int64(testIterations),
		"float":   3.14,
		"bool":    true,
		"unknown": struct{}{},
	}

	attrs := tracing.SpanAttributesFromMap(m)
	assert.Len(t, attrs, len(m))

	// Verify attribute conversion.
	for _, attr := range attrs {
		switch string(attr.Key) {
		case "string":
			assert.Equal(t, attribute.STRING, attr.Value.Type())
		case "int", "int64":
			assert.Equal(t, attribute.INT64, attr.Value.Type())
		case "float":
			assert.Equal(t, attribute.FLOAT64, attr.Value.Type())
		case "bool":
			assert.Equal(t, attribute.BOOL, attr.Value.Type())
		case "unknown":
			assert.Equal(t, attribute.STRING, attr.Value.Type())
		}
	}
}
