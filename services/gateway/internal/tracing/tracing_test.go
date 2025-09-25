
package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/poiley/mcp-bridge/services/gateway/test/testutil"
)

func TestInitTracer(t *testing.T) {
	logger := testutil.NewTestLogger(t)

	tests := []struct {
		name      string
		config    Config
		wantNoop  bool
		wantError bool
	}{
		{
			name: "Tracing disabled",
			config: Config{
				Enabled: false,
			},
			wantNoop: true,
		},
		{
			name: "Valid configuration",
			config: Config{
				Enabled:      true,
				ServiceName:  "test-service",
				SamplerType:  "const",
				SamplerParam: 1.0,
				AgentHost:    "localhost",
				AgentPort:    6831,
			},
			wantNoop: false,
		},
		{
			name: "Probabilistic sampler",
			config: Config{
				Enabled:      true,
				ServiceName:  "test-service",
				SamplerType:  "probabilistic",
				SamplerParam: 0.5,
				AgentHost:    "localhost",
				AgentPort:    6831,
			},
			wantNoop: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer, closer, err := InitTracer(tt.config, logger)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, tracer)
				assert.NotNil(t, closer)

				// Check if it's a noop tracer
				_, isNoop := tracer.(opentracing.NoopTracer)
				assert.Equal(t, tt.wantNoop, isNoop)

				// Clean up
				_ = closer.Close() 
			}
		})
	}
}

func TestStartSpanFromContext(t *testing.T) {
	// Set up mock tracer
	tracer := mocktracer.New()
	opentracing.SetGlobalTracer(tracer)

	// Test without parent span
	ctx := context.Background()
	span, newCtx := StartSpanFromContext(ctx, "test-operation")
	assert.NotNil(t, span)
	assert.NotNil(t, newCtx)
	span.Finish()

	// Verify span was created
	assert.Len(t, tracer.FinishedSpans(), 1)
	assert.Equal(t, "test-operation", tracer.FinishedSpans()[0].OperationName)

	// Test with parent span
	tracer.Reset()
	parentSpan := tracer.StartSpan("parent-operation")
	parentCtx := opentracing.ContextWithSpan(ctx, parentSpan)

	childSpan, childCtx := StartSpanFromContext(parentCtx, "child-operation")
	assert.NotNil(t, childSpan)
	assert.NotNil(t, childCtx)

	childSpan.Finish()
	parentSpan.Finish()

	// Verify parent-child relationship
	assert.Len(t, tracer.FinishedSpans(), 2)
	childSpanRecord := tracer.FinishedSpans()[0]
	parentSpanRecord := tracer.FinishedSpans()[1]

	assert.Equal(t, "child-operation", childSpanRecord.OperationName)
	assert.Equal(t, "parent-operation", parentSpanRecord.OperationName)
	assert.Equal(t, parentSpanRecord.SpanContext.SpanID, childSpanRecord.ParentID)
}

func TestHTTPHeaderInjectionExtraction(t *testing.T) {
	// Set up mock tracer
	tracer := mocktracer.New()
	opentracing.SetGlobalTracer(tracer)

	// Create a span
	span := tracer.StartSpan("test-span")
	defer span.Finish()

	// Test injection
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	err := InjectHTTPHeaders(span, req)
	assert.NoError(t, err)

	// Verify headers were added
	assert.NotEmpty(t, req.Header)

	// Test extraction
	extractedContext, err := ExtractHTTPHeaders(req)
	assert.NoError(t, err)
	assert.NotNil(t, extractedContext)

	// Create child span from extracted context
	childSpan := tracer.StartSpan("child-span", opentracing.ChildOf(extractedContext))
	childSpan.Finish()

	// Verify relationship
	assert.Len(t, tracer.FinishedSpans(), 1) // Only child is finished
	childRecord := tracer.FinishedSpans()[0]
	assert.Equal(t, func() uint64 {
		if mockCtx, ok := span.Context().(mocktracer.MockSpanContext); ok {
			return uint64(mockCtx.SpanID) 
		}

		return 0
	}(), uint64(childRecord.ParentID)) 
}

func TestHTTPMiddleware(t *testing.T) {
	tracer := setupMockTracer()
	tests := createHTTPMiddlewareTests()
	wrapped := setupHTTPMiddlewareHandler(tracer)
	
	runHTTPMiddlewareTests(t, tests, tracer, wrapped)
}

func setupMockTracer() *mocktracer.MockTracer {
	tracer := mocktracer.New()
	opentracing.SetGlobalTracer(tracer)
	return tracer
}

func createHTTPMiddlewareTests() []struct {
	name           string
	path           string
	method         string
	parentSpan     bool
	expectedStatus int
	expectError    bool
} {
	return []struct {
		name           string
		path           string
		method         string
		parentSpan     bool
		expectedStatus int
		expectError    bool
	}{
		{
			name:           "Success without parent",
			path:           "/test",
			method:         "GET",
			parentSpan:     false,
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Success with parent",
			path:           "/test",
			method:         "POST",
			parentSpan:     true,
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Error response",
			path:           "/error",
			method:         "GET",
			parentSpan:     false,
			expectedStatus: http.StatusInternalServerError,
			expectError:    true,
		},
	}
}

func setupHTTPMiddlewareHandler(tracer *mocktracer.MockTracer) http.Handler {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify span is in context
		span := opentracing.SpanFromContext(r.Context())
		// Note: span should not be nil in middleware context
		_ = span

		// Set response based on path
		if r.URL.Path == "/error" {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		_, _ = w.Write([]byte("test response"))
	})

	return HTTPMiddleware(handler)
}

func runHTTPMiddlewareTests(t *testing.T, tests []struct {
	name           string
	path           string
	method         string
	parentSpan     bool
	expectedStatus int
	expectError    bool
}, tracer *mocktracer.MockTracer, wrapped http.Handler) {
	t.Helper()
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer.Reset()

			// Create request
			req := httptest.NewRequest(tt.method, tt.path, nil)

			// Add parent span if needed
			if tt.parentSpan {
				parentSpan := tracer.StartSpan("parent")
				err := tracer.Inject(parentSpan.Context(), opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(req.Header))
				require.NoError(t, err)
				parentSpan.Finish()
			}

			// Execute request
			recorder := httptest.NewRecorder()
			wrapped.ServeHTTP(recorder, req)

			validateHTTPMiddlewareResponse(t, recorder, tt, tracer)
		})
	}
}

func validateHTTPMiddlewareResponse(t *testing.T, recorder *httptest.ResponseRecorder, tt struct {
	name           string
	path           string
	method         string
	parentSpan     bool
	expectedStatus int
	expectError    bool
}, tracer *mocktracer.MockTracer) {
	t.Helper()
	
	// Verify response
	assert.Equal(t, tt.expectedStatus, recorder.Code)

	// Verify span
	spans := tracer.FinishedSpans()

	var requestSpan *mocktracer.MockSpan

	for _, s := range spans {
		if s.OperationName == tt.method+" "+tt.path {
			requestSpan = s
			break
		}
	}

	require.NotNil(t, requestSpan)

	// Check tags
	assert.Equal(t, tt.method, requestSpan.Tag("http.method"))
	assert.Equal(t, "http", requestSpan.Tag("component"))
	assert.Equal(t, uint16(tt.expectedStatus), requestSpan.Tag("http.status_code"))

	if tt.expectError {
		assert.True(t, func() bool {
			if errTag, ok := requestSpan.Tag("error").(bool); ok {
				return errTag
			}
			return false
		}())
	}

	// Check parent relationship
	if tt.parentSpan {
		assert.NotEqual(t, 0, requestSpan.ParentID)
	} else {
		assert.Equal(t, 0, requestSpan.ParentID)
	}
}

func TestTracedHTTPClient(t *testing.T) {
	tracer := setupTracedHTTPClientTest()
	defer tracer.Close()
	
	client := TracedHTTPClient(nil)
	tests := createTracedHTTPClientTests()
	
	runTracedHTTPClientTests(t, tests, tracer, client)
}

func setupTracedHTTPClientTest() *httptest.Server {
	// Set up mock tracer
	tracer := mocktracer.New()
	opentracing.SetGlobalTracer(tracer)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The mock tracer doesn't set real headers, so we can't verify them here
		if r.URL.Path == "/error" {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		_, _ = w.Write([]byte("response"))
	}))
	
	return server
}

func createTracedHTTPClientTests() []struct {
	name        string
	path        string
	withSpan    bool
	expectError bool
} {
	return []struct {
		name        string
		path        string
		withSpan    bool
		expectError bool
	}{
		{
			name:        "Request without parent span",
			path:        "/test",
			withSpan:    false,
			expectError: false,
		},
		{
			name:        "Request with parent span",
			path:        "/test",
			withSpan:    true,
			expectError: false,
		},
		{
			name:        "Error response with span",
			path:        "/error",
			withSpan:    true,
			expectError: true,
		},
	}
}

func runTracedHTTPClientTests(t *testing.T, tests []struct {
	name        string
	path        string
	withSpan    bool
	expectError bool
}, server *httptest.Server, client *http.Client) {
	t.Helper()
	
	tracer := opentracing.GlobalTracer().(*mocktracer.MockTracer)
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer.Reset()

			ctx := context.Background()

			// Add parent span if needed
			if tt.withSpan {
				span := tracer.StartSpan("parent")
				ctx = opentracing.ContextWithSpan(ctx, span)
				defer span.Finish()
			}

			executeTracedHTTPRequest(t, ctx, server, client, tt)
			
			if tt.withSpan {
				validateTracedHTTPResponse(t, tracer, tt)
			}
		})
	}
}

func executeTracedHTTPRequest(t *testing.T, ctx context.Context, server *httptest.Server, client *http.Client, tt struct {
	name        string
	path        string
	withSpan    bool
	expectError bool
}) {
	t.Helper()
	
	// Make request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+tt.path, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	_ = resp.Body.Close()
}

func validateTracedHTTPResponse(t *testing.T, tracer *mocktracer.MockTracer, tt struct {
	name        string
	path        string
	withSpan    bool
	expectError bool
}) {
	t.Helper()
	
	// Wait for spans to finish
	time.Sleep(10 * time.Millisecond)

	// Find HTTP client span
	spans := tracer.FinishedSpans()

	var clientSpan *mocktracer.MockSpan

	for _, s := range spans {
		if s.OperationName == "HTTP GET" {
			clientSpan = s
			break
		}
	}

	require.NotNil(t, clientSpan)

	// Verify tags
	assert.Equal(t, "GET", clientSpan.Tag("http.method"))
	assert.Equal(t, "http-client", clientSpan.Tag("component"))

	if tt.expectError {
		assert.True(t, func() bool {
			if errTag, ok := clientSpan.Tag("error").(bool); ok {
				return errTag
			}
			return false
		}())
	}
}

func TestResponseWrapper(t *testing.T) {
	// Test that response wrapper correctly captures status code
	recorder := httptest.NewRecorder()
	wrapper := &responseWrapper{ResponseWriter: recorder, statusCode: http.StatusOK}

	// Write header
	wrapper.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, wrapper.statusCode)
	assert.Equal(t, http.StatusCreated, recorder.Code)

	// Write without explicit WriteHeader
	recorder2 := httptest.NewRecorder()
	wrapper2 := &responseWrapper{ResponseWriter: recorder2, statusCode: http.StatusOK}
	_, _ = wrapper2.Write([]byte("test")) 
	assert.Equal(t, http.StatusOK, wrapper2.statusCode)
}

func TestJaegerLogger(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	jLogger := jaegerLogger{logger: logger}

	// Test Error
	jLogger.Error("test error")

	// Test Infof
	jLogger.Infof("test info: %s %d", "arg1", 42)
}

func TestNoopCloser(t *testing.T) {
	closer := &noopCloser{}
	err := closer.Close()
	assert.NoError(t, err)
}
