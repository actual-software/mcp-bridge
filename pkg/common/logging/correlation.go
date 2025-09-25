// Package logging provides correlation ID utilities for request tracing across MCP components.
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"go.uber.org/zap"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

const (
	// CorrelationIDKey is the context key for correlation IDs.
	correlationIDKey contextKey = "correlation_id"
	// TraceIDKey is the context key for trace IDs.
	traceIDKey contextKey = "trace_id"
	// correlationIDBytes is the size of correlation ID in bytes.
	correlationIDBytes = 8
	// traceIDBytes is the size of trace ID in bytes.
	traceIDBytes = 16
	// loggerFieldCapacity is the initial capacity for logger field slices.
	loggerFieldCapacity = 2
)

// GenerateCorrelationID generates a new correlation ID.
func GenerateCorrelationID() string {
	bytes := make([]byte, correlationIDBytes)
	if _, err := rand.Read(bytes); err != nil {
		// This should never happen with crypto/rand, but handle gracefully
		return fmt.Sprintf("corr_fallback_%d", len(bytes))
	}

	return hex.EncodeToString(bytes)
}

// GenerateTraceID generates a new trace ID.
func GenerateTraceID() string {
	bytes := make([]byte, traceIDBytes)
	if _, err := rand.Read(bytes); err != nil {
		// This should never happen with crypto/rand, but handle gracefully
		return fmt.Sprintf("trace_fallback_%d", len(bytes))
	}

	return hex.EncodeToString(bytes)
}

// WithCorrelationID adds a correlation ID to the context.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationIDKey, correlationID)
}

// WithTraceID adds a trace ID to the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// GetCorrelationID retrieves the correlation ID from context.
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey).(string); ok {
		return id
	}

	return ""
}

// GetTraceID retrieves the trace ID from context.
func GetTraceID(ctx context.Context) string {
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}

	return ""
}

// WithCorrelation creates a new context with correlation and trace IDs if they don't exist.
func WithCorrelation(ctx context.Context) context.Context {
	if GetCorrelationID(ctx) == "" {
		ctx = WithCorrelationID(ctx, GenerateCorrelationID())
	}

	if GetTraceID(ctx) == "" {
		ctx = WithTraceID(ctx, GenerateTraceID())
	}

	return ctx
}

// LoggerWithCorrelation returns a logger with correlation fields from context.
func LoggerWithCorrelation(ctx context.Context, logger *zap.Logger) *zap.Logger {
	fields := make([]zap.Field, 0, loggerFieldCapacity)

	if correlationID := GetCorrelationID(ctx); correlationID != "" {
		fields = append(fields, zap.String(FieldCorrelationID, correlationID))
	}

	if traceID := GetTraceID(ctx); traceID != "" {
		fields = append(fields, zap.String(FieldTraceID, traceID))
	}

	if len(fields) > 0 {
		return logger.With(fields...)
	}

	return logger
}

// LoggerWithRequest returns a logger with request-specific fields.
func LoggerWithRequest(ctx context.Context, logger *zap.Logger, requestID interface{}) *zap.Logger {
	logger = LoggerWithCorrelation(ctx, logger)

	if requestID != nil {
		logger = logger.With(zap.Any(FieldRequestID, requestID))
	}

	return logger
}

// LoggerWithConnection returns a logger with connection-specific fields.
func LoggerWithConnection(ctx context.Context, logger *zap.Logger, connectionID, remoteAddr string) *zap.Logger {
	logger = LoggerWithCorrelation(ctx, logger)

	fields := make([]zap.Field, 0, loggerFieldCapacity)
	if connectionID != "" {
		fields = append(fields, zap.String(FieldConnectionID, connectionID))
	}

	if remoteAddr != "" {
		fields = append(fields, zap.String(FieldRemoteAddr, remoteAddr))
	}

	if len(fields) > 0 {
		logger = logger.With(fields...)
	}

	return logger
}
