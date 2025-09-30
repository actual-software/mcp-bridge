// Package tracing provides OpenTelemetry distributed tracing integration.
package tracing

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/pkg/common/config"
)

const (
	shutdownTimeoutSeconds = 10 // Timeout for graceful shutdown of tracing provider
)

// OTelConfig is now defined in the common config package.
type OTelConfig = config.TracingConfig

// OTelTracer wraps OpenTelemetry tracer provider and configuration.
type OTelTracer struct {
	provider   *sdktrace.TracerProvider
	tracer     trace.Tracer
	config     config.TracingConfig
	logger     *zap.Logger
	shutdownFn func(context.Context) error
}

// InitOTelTracer initializes OpenTelemetry distributed tracing.
func InitOTelTracer(cfg config.TracingConfig, logger *zap.Logger) (*OTelTracer, error) {
	if !cfg.Enabled {
		logger.Info("OpenTelemetry tracing disabled")

		return &OTelTracer{
			config:     cfg,
			logger:     logger,
			shutdownFn: func(context.Context) error { return nil },
		}, nil
	}

	// Create resource with service information
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			semconv.DeploymentEnvironmentKey.String(cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporter
	exporter, err := createExporter(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// Create sampler
	sampler := createSampler(cfg)

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global providers
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create tracer
	tracer := tp.Tracer("mcp-gateway")

	logger.Info("OpenTelemetry tracing initialized",
		zap.String("service", cfg.ServiceName),
		zap.String("version", cfg.ServiceVersion),
		zap.String("environment", cfg.Environment),
		zap.String("exporter", cfg.ExporterType),
		zap.String("sampler", cfg.SamplerType),
	)

	return &OTelTracer{
		provider: tp,
		tracer:   tracer,
		config:   cfg,
		logger:   logger,
		shutdownFn: func(ctx context.Context) error {
			return tp.Shutdown(ctx)
		},
	}, nil
}

// createExporter creates the appropriate trace exporter based on configuration.

func createExporter(cfg config.TracingConfig, logger *zap.Logger) (sdktrace.SpanExporter, error) {
	switch cfg.ExporterType {
	case "otlp", "":
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		}
		if cfg.OTLPInsecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}

		return otlptracegrpc.New(context.Background(), opts...)

	case "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())

	default:
		logger.Warn("Unknown exporter type, falling back to stdout", zap.String("type", cfg.ExporterType))

		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
}

// createSampler creates the appropriate sampler based on configuration.
//nolint:ireturn // Returns OpenTelemetry interface
func createSampler(cfg config.TracingConfig) sdktrace.Sampler {
	switch cfg.SamplerType {
	case "always_on", "":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(cfg.SamplerParam)
	default:
		return sdktrace.AlwaysSample()
	}
}

// StartSpan starts a new span.
//nolint:ireturn // Returns OpenTelemetry interface
func (t *OTelTracer) StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	if t.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}

	return t.tracer.Start(ctx, name)
}

// HTTPMiddleware creates an HTTP middleware for automatic request tracing.
func (t *OTelTracer) HTTPMiddleware(next http.Handler) http.Handler {
	if t.tracer == nil {
		return next
	}

	return otelhttp.NewHandler(next, "mcp-gateway-http",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return fmt.Sprintf("%s %s", r.Method, r.URL.Path)
		}),
	)
}

// HTTPClient creates an HTTP client with automatic request tracing.
func (t *OTelTracer) HTTPClient(client *http.Client) *http.Client {
	if t.tracer == nil || client == nil {
		return client
	}

	// Wrap the transport with OpenTelemetry instrumentation
	if client.Transport == nil {
		client.Transport = http.DefaultTransport
	}

	client.Transport = otelhttp.NewTransport(client.Transport)

	return client
}

// ExtractTraceContext extracts trace context from HTTP headers.
func (t *OTelTracer) ExtractTraceContext(ctx context.Context, headers http.Header) context.Context {
	if t.tracer == nil {
		return ctx
	}

	propagator := otel.GetTextMapPropagator()

	return propagator.Extract(ctx, propagation.HeaderCarrier(headers))
}

// InjectTraceContext injects trace context into HTTP headers.
func (t *OTelTracer) InjectTraceContext(ctx context.Context, headers http.Header) {
	if t.tracer == nil {
		return
	}

	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.HeaderCarrier(headers))
}

// AddSpanEvent adds an event to the current span.
func (t *OTelTracer) AddSpanEvent(ctx context.Context, name string, attributes map[string]interface{}) {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return
	}

	// Convert attributes to OpenTelemetry format
	otelAttrs := make([]trace.EventOption, 0, len(attributes))

	if len(attributes) > 0 {
		kvs := make([]attribute.KeyValue, 0, len(attributes))

		for k, v := range attributes {
			switch val := v.(type) {
			case string:
				kvs = append(kvs, attribute.String(k, val))
			case int:
				kvs = append(kvs, attribute.Int64(k, int64(val)))
			case int64:
				kvs = append(kvs, attribute.Int64(k, val))
			case float64:
				kvs = append(kvs, attribute.Float64(k, val))
			case bool:
				kvs = append(kvs, attribute.Bool(k, val))
			default:
				kvs = append(kvs, attribute.String(k, fmt.Sprintf("%v", val)))
			}
		}

		otelAttrs = append(otelAttrs, trace.WithAttributes(kvs...))
	}

	span.AddEvent(name, otelAttrs...)
}

// SetSpanAttributes sets attributes on the current span.
func (t *OTelTracer) SetSpanAttributes(ctx context.Context, attributes map[string]interface{}) {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return
	}

	// Convert attributes to OpenTelemetry format
	for k, v := range attributes {
		switch val := v.(type) {
		case string:
			span.SetAttributes(attribute.String(k, val))
		case int:
			span.SetAttributes(attribute.Int64(k, int64(val)))
		case int64:
			span.SetAttributes(attribute.Int64(k, val))
		case float64:
			span.SetAttributes(attribute.Float64(k, val))
		case bool:
			span.SetAttributes(attribute.Bool(k, val))
		default:
			span.SetAttributes(attribute.String(k, fmt.Sprintf("%v", val)))
		}
	}
}

// RecordError records an error in the current span.
func (t *OTelTracer) RecordError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if span == nil || err == nil {
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// GetTraceID returns the trace ID from the current span context.
func (t *OTelTracer) GetTraceID(ctx context.Context) string {
	if t.tracer == nil {
		return ""
	}

	span := trace.SpanFromContext(ctx)
	if span == nil {
		return ""
	}

	spanCtx := span.SpanContext()
	if !spanCtx.IsValid() {
		return ""
	}

	return spanCtx.TraceID().String()
}

// GetSpanID returns the span ID from the current span context.
func (t *OTelTracer) GetSpanID(ctx context.Context) string {
	if t.tracer == nil {
		return ""
	}

	span := trace.SpanFromContext(ctx)
	if span == nil {
		return ""
	}

	spanCtx := span.SpanContext()
	if !spanCtx.IsValid() {
		return ""
	}

	return spanCtx.SpanID().String()
}

// Shutdown gracefully shuts down the tracer provider.
func (t *OTelTracer) Shutdown(ctx context.Context) error {
	if t.shutdownFn == nil {
		return nil
	}

	// Create timeout context for shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeoutSeconds*time.Second)
	defer cancel()

	t.logger.Info("Shutting down OpenTelemetry tracer")

	return t.shutdownFn(shutdownCtx)
}

// DefaultOTelConfig returns a default OpenTelemetry configuration.
func DefaultOTelConfig() config.TracingConfig {
	return config.TracingConfig{
		Enabled:        true,
		ServiceName:    "mcp-gateway",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		SamplerType:    "always_on",
		SamplerParam:   1.0,
		ExporterType:   "stdout",
		OTLPEndpoint:   "http://localhost:4317",
		OTLPInsecure:   true,
	}
}
