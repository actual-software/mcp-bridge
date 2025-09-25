
package tracing_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/gateway/internal/tracing"
)

func TestInitOTelTracer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tests := createOTelTracerTests()
	runOTelTracerTests(t, tests, logger)
}

func createOTelTracerTests() []struct {
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
				ServiceName:    "test-gateway",
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
				ServiceName:    "test-gateway",
				ServiceVersion: "1.0.0",
				Environment:    "test",
				ExporterType:   "otlp",
				OTLPEndpoint:   "localhost:4317",
				OTLPInsecure:   true,
				SamplerType:    "traceidratio",
				SamplerParam:   0.5,
			},
			wantErr: false,
		},
		{
			name: "always_off sampler",
			config: config.TracingConfig{
				Enabled:      true,
				ServiceName:  "test-gateway",
				ExporterType: "stdout",
				SamplerType:  "always_off",
			},
			wantErr: false,
		},
	}
}

func runOTelTracerTests(t *testing.T, tests []struct {
	name    string
	config  config.TracingConfig
	wantErr bool
}, logger *zap.Logger) {
	t.Helper()
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer, err := tracing.InitOTelTracer(tt.config, logger)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, tracer)

			// Test shutdown
			ctx := context.Background()
			err = tracer.Shutdown(ctx)
			assert.NoError(t, err)
		})
	}
}

func TestOTelTracerOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := setupOTelTracer(t, logger)
	defer func() { _ = tracer.Shutdown(context.Background()) }()

	runOTelTracerOperationTests(t, tracer)
}

func setupOTelTracer(t *testing.T, logger *zap.Logger) *tracing.OTelTracer {
	t.Helper()
	
	cfg := config.TracingConfig{
		Enabled:        true,
		ServiceName:    "test-gateway",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		ExporterType:   "stdout",
		SamplerType:    "always_on",
	}

	tracer, err := tracing.InitOTelTracer(cfg, logger)
	require.NoError(t, err)
	return tracer
}

func runOTelTracerOperationTests(t *testing.T, tracer *tracing.OTelTracer) {
	t.Helper()

	t.Run("StartSpan", func(t *testing.T) {
		ctx := context.Background()
		spanCtx, span := tracer.StartSpan(ctx, "test-operation")
		assert.NotNil(t, span)
		assert.NotEqual(t, ctx, spanCtx)

		// Check trace and span IDs
		traceID := tracer.GetTraceID(spanCtx)
		spanID := tracer.GetSpanID(spanCtx)

		assert.NotEmpty(t, traceID)
		assert.NotEmpty(t, spanID)

		span.End()
	})

	t.Run("SpanAttributes", func(_ *testing.T) {
		ctx := context.Background()

		spanCtx, span := tracer.StartSpan(ctx, "test-attributes")
		defer span.End()

		attrs := map[string]interface{}{
			"string_attr":  "value",
			"int_attr":     42,
			"float_attr":   3.14,
			"bool_attr":    true,
			"unknown_attr": struct{}{},
		}

		tracer.SetSpanAttributes(spanCtx, attrs)
	})

	t.Run("SpanEvents", func(_ *testing.T) {
		ctx := context.Background()

		spanCtx, span := tracer.StartSpan(ctx, "test-events")
		defer span.End()

		tracer.AddSpanEvent(spanCtx, "test-event", map[string]interface{}{
			"key": "value",
		})
	})

	t.Run("RecordError", func(_ *testing.T) {
		ctx := context.Background()

		spanCtx, span := tracer.StartSpan(ctx, "test-error")
		defer span.End()

		err := assert.AnError
		tracer.RecordError(spanCtx, err)
	})

	runHTTPOperationTests(t, tracer)
	runTraceContextPropagationTest(t, tracer)
}

func runHTTPOperationTests(t *testing.T, tracer *tracing.OTelTracer) {
	t.Helper()

	t.Run("HTTPMiddleware", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check that span exists in context
			span := trace.SpanFromContext(r.Context())
			assert.NotNil(t, span)
			w.WriteHeader(http.StatusOK)
		})

		middleware := tracer.HTTPMiddleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("HTTPClient", func(t *testing.T) {
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for trace headers
			assert.NotEmpty(t, r.Header.Get("Traceparent"))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create traced client
		client := tracer.HTTPClient(&http.Client{})

		// Make request with trace context
		ctx := context.Background()

		spanCtx, span := tracer.StartSpan(ctx, "test-http-client")
		defer span.End()

		req, err := http.NewRequestWithContext(spanCtx, http.MethodGet, server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)

		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func runTraceContextPropagationTest(t *testing.T, tracer *tracing.OTelTracer) {
	t.Helper()

	t.Run("TraceContextPropagation", func(t *testing.T) {
		ctx := context.Background()

		// Create a span
		spanCtx, span := tracer.StartSpan(ctx, "parent-span")
		defer span.End()

		// Inject into headers
		headers := make(http.Header)
		tracer.InjectTraceContext(spanCtx, headers)
		assert.NotEmpty(t, headers.Get("Traceparent"))

		// Extract from headers
		newCtx := tracer.ExtractTraceContext(context.Background(), headers)

		// Create child span from extracted context
		childCtx, childSpan := tracer.StartSpan(newCtx, "child-span")
		defer childSpan.End()

		// Verify parent-child relationship
		parentTraceID := tracer.GetTraceID(spanCtx)
		childTraceID := tracer.GetTraceID(childCtx)
		assert.Equal(t, parentTraceID, childTraceID)
	})
}

func TestOTelTracerDisabled(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.TracingConfig{
		Enabled: false,
	}

	tracer, err := tracing.InitOTelTracer(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, tracer)

	// All operations should be no-ops
	ctx := context.Background()

	spanCtx, span := tracer.StartSpan(ctx, "test")
	assert.Equal(t, ctx, spanCtx) // Should return same context
	assert.NotNil(t, span)

	// These should not panic
	tracer.SetSpanAttributes(ctx, map[string]interface{}{"key": "value"})
	tracer.AddSpanEvent(ctx, "event", nil)
	tracer.RecordError(ctx, assert.AnError)

	// Should return empty strings
	assert.Empty(t, tracer.GetTraceID(ctx))
	assert.Empty(t, tracer.GetSpanID(ctx))

	// Middleware should pass through
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := tracer.HTTPMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}
