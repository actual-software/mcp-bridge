// Package tracing provides OpenTracing integration for distributed tracing support.
package tracing

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	"go.uber.org/zap"
)

const (
	httpErrorStatusThreshold = 400 // HTTP status code threshold for errors
)

// Config represents tracing configuration.
type Config struct {
	Enabled          bool    `mapstructure:"enabled"`
	ServiceName      string  `mapstructure:"service_name"`
	SamplerType      string  `mapstructure:"sampler_type"`
	SamplerParam     float64 `mapstructure:"sampler_param"`
	ReporterLogSpans bool    `mapstructure:"reporter_log_spans"`
	AgentHost        string  `mapstructure:"agent_host"`
	AgentPort        int     `mapstructure:"agent_port"`
}

// InitTracer initializes the OpenTracing tracer.

func InitTracer(cfg Config, logger *zap.Logger) (opentracing.Tracer, io.Closer, error) {
	if !cfg.Enabled {
		logger.Info("Tracing disabled")

		return opentracing.NoopTracer{}, &noopCloser{}, nil
	}

	// Create Jaeger configuration
	jaegerCfg := jaegercfg.Configuration{
		ServiceName: cfg.ServiceName,
		Sampler: &jaegercfg.SamplerConfig{
			Type:  cfg.SamplerType,
			Param: cfg.SamplerParam,
		},
		Reporter: &jaegercfg.ReporterConfig{
			LogSpans:           cfg.ReporterLogSpans,
			LocalAgentHostPort: fmt.Sprintf("%s:%d", cfg.AgentHost, cfg.AgentPort),
		},
	}

	// Create tracer
	tracer, closer, err := jaegerCfg.NewTracer(
		jaegercfg.Logger(jaegerLogger{logger}),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create tracer: %w", err)
	}

	// Set as global tracer
	opentracing.SetGlobalTracer(tracer)

	logger.Info("Tracing initialized",
		zap.String("service", cfg.ServiceName),
		zap.String("sampler", cfg.SamplerType),
		zap.Float64("sampler_param", cfg.SamplerParam),
		zap.String("agent", fmt.Sprintf("%s:%d", cfg.AgentHost, cfg.AgentPort)),
	)

	return tracer, closer, nil
}

// StartSpanFromContext starts a new span from context.

func StartSpanFromContext(ctx context.Context, operationName string, opts ...opentracing.StartSpanOption) (opentracing.Span, context.Context) {
	return opentracing.StartSpanFromContext(ctx, operationName, opts...)
}

// InjectHTTPHeaders injects tracing headers into HTTP request.
func InjectHTTPHeaders(span opentracing.Span, req *http.Request) error {
	return opentracing.GlobalTracer().Inject(
		span.Context(),
		opentracing.HTTPHeaders,
		opentracing.HTTPHeadersCarrier(req.Header),
	)
}

// ExtractHTTPHeaders extracts tracing context from HTTP request.

func ExtractHTTPHeaders(req *http.Request) (opentracing.SpanContext, error) {
	return opentracing.GlobalTracer().Extract(
		opentracing.HTTPHeaders,
		opentracing.HTTPHeadersCarrier(req.Header),
	)
}

// HTTPMiddleware creates an HTTP middleware for tracing.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract parent span context if present
		wireContext, err := ExtractHTTPHeaders(r)

		var span opentracing.Span

		if err == nil {
			// Create span with parent context
			span = opentracing.StartSpan(
				fmt.Sprintf("%s %s", r.Method, r.URL.Path),
				ext.RPCServerOption(wireContext),
			)
		} else {
			// Create root span
			span = opentracing.StartSpan(fmt.Sprintf("%s %s", r.Method, r.URL.Path))
		}

		defer span.Finish()

		// Set standard tags
		ext.HTTPMethod.Set(span, r.Method)
		ext.HTTPUrl.Set(span, r.URL.String())
		ext.Component.Set(span, "http")

		// Add to context
		ctx := opentracing.ContextWithSpan(r.Context(), span)
		r = r.WithContext(ctx)

		// Wrap response writer to capture status code
		wrapped := &responseWrapper{ResponseWriter: w, statusCode: http.StatusOK}

		// Call next handler
		next.ServeHTTP(wrapped, r)

		// Set response tags
		ext.HTTPStatusCode.Set(span, uint16(wrapped.statusCode)) // #nosec G115 - HTTP status codes are bounded 0-999

		if wrapped.statusCode >= httpErrorStatusThreshold {
			ext.Error.Set(span, true)
		}
	})
}

// TracedHTTPClient creates an HTTP client with tracing.
func TracedHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}

	// Wrap transport with tracing
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	client.Transport = &tracedTransport{
		base: transport,
	}

	return client
}

// tracedTransport wraps http.RoundTripper with tracing.
type tracedTransport struct {
	base http.RoundTripper
}

func (t *tracedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Get span from context
	span := opentracing.SpanFromContext(req.Context())
	if span == nil {
		// No parent span, execute without tracing
		return t.base.RoundTrip(req)
	}

	// Create child span for HTTP request
	clientSpan := opentracing.StartSpan(
		"HTTP "+req.Method,
		opentracing.ChildOf(span.Context()),
	)
	defer clientSpan.Finish()

	// Set tags
	ext.HTTPMethod.Set(clientSpan, req.Method)
	ext.HTTPUrl.Set(clientSpan, req.URL.String())
	ext.Component.Set(clientSpan, "http-client")
	ext.SpanKind.Set(clientSpan, ext.SpanKindRPCClientEnum)

	// Inject tracing headers
	if err := InjectHTTPHeaders(clientSpan, req); err != nil {
		clientSpan.LogKV("error", "failed to inject headers", "err", err)
	}

	// Execute request
	resp, err := t.base.RoundTrip(req)

	// Record result
	if err != nil {
		ext.Error.Set(clientSpan, true)
		clientSpan.LogKV("error", err.Error())
	} else {
		ext.HTTPStatusCode.Set(clientSpan, uint16(resp.StatusCode)) // #nosec G115 - HTTP status codes are bounded 0-999

		if resp.StatusCode >= httpErrorStatusThreshold {
			ext.Error.Set(clientSpan, true)
		}
	}

	return resp, err
}

// responseWrapper wraps http.ResponseWriter to capture status code.
type responseWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// jaegerLogger adapts zap.Logger to jaeger.Logger.
type jaegerLogger struct {
	logger *zap.Logger
}

func (l jaegerLogger) Error(msg string) {
	l.logger.Error(msg)
}

func (l jaegerLogger) Infof(msg string, args ...interface{}) {
	l.logger.Info(fmt.Sprintf(msg, args...))
}

// noopCloser implements io.Closer for noop tracer.
type noopCloser struct{}

func (n *noopCloser) Close() error {
	return nil
}
