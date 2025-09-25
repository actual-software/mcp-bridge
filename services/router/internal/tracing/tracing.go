// Package tracing provides OpenTelemetry distributed tracing for the router service.
package tracing

import (
	"context"
	"fmt"
	"net/http"
	"time"

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
	defaultRetryCount = 10
)

// Config is now defined in the common config package.
type Config = config.TracingConfig

// Tracer wraps OpenTelemetry tracer provider and configuration.
type Tracer struct {
	provider   *sdktrace.TracerProvider
	tracer     trace.Tracer
	config     config.TracingConfig
	logger     *zap.Logger
	shutdownFn func(context.Context) error
}

// Init initializes OpenTelemetry distributed tracing.
func Init(cfg config.TracingConfig, logger *zap.Logger) (*Tracer, error) {
	if !cfg.Enabled {
		logger.Info("OpenTelemetry tracing disabled")

		return &Tracer{
			config:     cfg,
			logger:     logger,
			shutdownFn: func(context.Context) error { return nil },
		}, nil
	}

	// Create resource with service information.
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

	// Create exporter.
	exporter, err := createExporter(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// Create sampler.
	sampler := createSampler(cfg)

	// Create tracer provider.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global providers.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create tracer.
	tracer := tp.Tracer("mcp-router")

	logger.Info("OpenTelemetry tracing initialized",
		zap.String("service", cfg.ServiceName),
		zap.String("version", cfg.ServiceVersion),
		zap.String("environment", cfg.Environment),
		zap.String("exporter", cfg.ExporterType),
		zap.String("sampler", cfg.SamplerType),
	)

	return &Tracer{
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
func (t *Tracer) StartSpan(ctx context.Context, name string,
	opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if t.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}

	return t.tracer.Start(ctx, name, opts...)
}

// StartSpanWithKind starts a new span with a specific kind.
func (t *Tracer) StartSpanWithKind(ctx context.Context, name string,
	kind trace.SpanKind) (context.Context, trace.Span) {
	return t.StartSpan(ctx, name, trace.WithSpanKind(kind))
}

// ExtractTraceContext extracts trace context from HTTP headers.
func (t *Tracer) ExtractTraceContext(ctx context.Context, headers http.Header) context.Context {
	if t.tracer == nil {
		return ctx
	}

	propagator := otel.GetTextMapPropagator()

	return propagator.Extract(ctx, propagation.HeaderCarrier(headers))
}

// InjectTraceContext injects trace context into HTTP headers.
func (t *Tracer) InjectTraceContext(ctx context.Context, headers http.Header) {
	if t.tracer == nil {
		return
	}

	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.HeaderCarrier(headers))
}

// ExtractTraceContextFromMap extracts trace context from a string map (for WebSocket headers).
func (t *Tracer) ExtractTraceContextFromMap(ctx context.Context, headers map[string]string) context.Context {
	if t.tracer == nil {
		return ctx
	}

	// Convert map to http.Header
	httpHeaders := make(http.Header)
	for k, v := range headers {
		httpHeaders.Set(k, v)
	}

	return t.ExtractTraceContext(ctx, httpHeaders)
}

// InjectTraceContextToMap injects trace context into a string map (for WebSocket headers).
func (t *Tracer) InjectTraceContextToMap(ctx context.Context, headers map[string]string) {
	if t.tracer == nil {
		return
	}

	// Convert to http.Header, inject, then convert back
	httpHeaders := make(http.Header)
	for k, v := range headers {
		httpHeaders.Set(k, v)
	}

	t.InjectTraceContext(ctx, httpHeaders)

	// Copy back to map.
	for k := range httpHeaders {
		headers[k] = httpHeaders.Get(k)
	}
}

// AddSpanEvent adds an event to the current span.
func (t *Tracer) AddSpanEvent(ctx context.Context, name string, attributes ...trace.EventOption) {
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}

	span.AddEvent(name, attributes...)
}

// SetSpanAttributes sets attributes on the current span.
func (t *Tracer) SetSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}

	span.SetAttributes(attrs...)
}

// RecordError records an error in the current span.
func (t *Tracer) RecordError(ctx context.Context, err error, opts ...trace.EventOption) {
	span := trace.SpanFromContext(ctx)
	if span == nil || err == nil || !span.IsRecording() {
		return
	}

	span.RecordError(err, opts...)
	span.SetStatus(codes.Error, err.Error())
}

// GetTraceID returns the trace ID from the current span context.
func (t *Tracer) GetTraceID(ctx context.Context) string {
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
func (t *Tracer) GetSpanID(ctx context.Context) string {
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

// IsEnabled returns whether tracing is enabled.
func (t *Tracer) IsEnabled() bool {
	return t.config.Enabled && t.tracer != nil
}

// Shutdown gracefully shuts down the tracer provider.
func (t *Tracer) Shutdown(ctx context.Context) error {
	if t.shutdownFn == nil {
		return nil
	}

	// Create timeout context for shutdown.
	shutdownCtx, cancel := context.WithTimeout(ctx, defaultRetryCount * time.Second)
	defer cancel()

	t.logger.Info("Shutting down OpenTelemetry tracer")

	return t.shutdownFn(shutdownCtx)
}

// DefaultConfig returns a default OpenTelemetry configuration.
func DefaultConfig() config.TracingConfig {
	return config.TracingConfig{
		Enabled:        true,
		ServiceName:    "mcp-router",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		SamplerType:    "always_on",
		SamplerParam:   1.0,
		ExporterType:   "stdout",
		OTLPEndpoint:   "http://localhost:4317",
		OTLPInsecure:   true,
	}
}

// SpanAttributesFromError creates span attributes from an error.
func SpanAttributesFromError(err error) []attribute.KeyValue {
	if err == nil {
		return nil
	}

	return []attribute.KeyValue{
		attribute.Bool("error", true),
		attribute.String("error.message", err.Error()),
	}
}

// SpanAttributesFromMap converts a map to span attributes.
func SpanAttributesFromMap(m map[string]interface{}) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(m))

	for k, v := range m {
		switch val := v.(type) {
		case string:
			attrs = append(attrs, attribute.String(k, val))
		case int:
			attrs = append(attrs, attribute.Int64(k, int64(val)))
		case int64:
			attrs = append(attrs, attribute.Int64(k, val))
		case float64:
			attrs = append(attrs, attribute.Float64(k, val))
		case bool:
			attrs = append(attrs, attribute.Bool(k, val))
		default:
			attrs = append(attrs, attribute.String(k, fmt.Sprintf("%v", val)))
		}
	}

	return attrs
}
