// Package logging provides structured logging utilities with error context integration.
package logging

import (
	"context"
	stderrors "errors"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

// WithError adds error context to logger fields.
func WithError(err error) []zap.Field {
	if err == nil {
		return []zap.Field{}
	}

	fields := []zap.Field{
		zap.Error(err),
	}

	// If it's a GatewayError, add all context
	var gatewayErr *errors.GatewayError
	if stderrors.As(err, &gatewayErr) {
		fields = append(fields,
			zap.String("error_type", string(gatewayErr.Type)),
			zap.String("error_code", gatewayErr.Code),
			zap.String("component", gatewayErr.Component),
			zap.String("operation", gatewayErr.Operation),
			zap.String("severity", string(gatewayErr.Severity)),
			zap.Bool("retryable", gatewayErr.Retryable),
			zap.Int("http_status", gatewayErr.HTTPStatus),
		)

		// Add context fields
		if len(gatewayErr.Context) > 0 {
			fields = append(fields, zap.Any("error_context", gatewayErr.Context))
		}

		// Add stack trace for high severity errors
		if gatewayErr.Severity == errors.SeverityHigh || gatewayErr.Severity == errors.SeverityCritical {
			if len(gatewayErr.Stack) > 0 {
				fields = append(fields, zap.Strings("stack_trace", gatewayErr.Stack))
			}
		}
	}

	return fields
}

// WithRequestContext adds request context to logger fields.
func WithRequestContext(ctx context.Context) []zap.Field {
	fields := []zap.Field{}

	// Define context keys to extract
	contextKeys := []struct {
		key   loggingContextKey
		field string
	}{
		{loggingContextKeyRequestID, "request_id"},
		{loggingContextKeyTraceID, "trace_id"},
		{loggingContextKeyUserID, "user_id"},
		{loggingContextKeySessionID, "session_id"},
		{loggingContextKeyNamespace, "namespace"},
		{loggingContextKeyMethod, "method"},
	}

	// Extract each context value
	for _, ck := range contextKeys {
		if value := extractStringFromContext(ctx, ck.key); value != "" {
			fields = append(fields, zap.String(ck.field, value))
		}
	}

	return fields
}

// extractStringFromContext safely extracts a string value from context.
func extractStringFromContext(ctx context.Context, key loggingContextKey) string {
	if value := ctx.Value(key); value != nil {
		if str, ok := value.(string); ok {
			return str
		}
	}

	return ""
}

// LogError logs an error with full context.
func LogError(ctx context.Context, logger *zap.Logger, msg string, err error, additionalFields ...zap.Field) {
	fields := WithError(err)
	fields = append(fields, WithRequestContext(ctx)...)
	fields = append(fields, additionalFields...)

	// Determine log level based on error severity
	level := getLogLevelForError(err)

	// Log at appropriate level
	logAtLevel(logger, level, msg, fields)
}

// getLogLevelForError determines the appropriate log level based on error severity.
func getLogLevelForError(err error) zapcore.Level {
	var gatewayErr *errors.GatewayError
	if !stderrors.As(err, &gatewayErr) {
		return zapcore.ErrorLevel
	}

	switch gatewayErr.Severity {
	case errors.SeverityLow:
		return zapcore.WarnLevel
	case errors.SeverityMedium:
		return zapcore.ErrorLevel
	case errors.SeverityHigh, errors.SeverityCritical:
		return zapcore.ErrorLevel
	default:
		return zapcore.ErrorLevel
	}
}

// logAtLevel logs a message at the specified level.
func logAtLevel(logger *zap.Logger, level zapcore.Level, msg string, fields []zap.Field) {
	switch level {
	case zapcore.DebugLevel:
		logger.Debug(msg, fields...)
	case zapcore.InfoLevel:
		logger.Info(msg, fields...)
	case zapcore.WarnLevel:
		logger.Warn(msg, fields...)
	case zapcore.ErrorLevel:
		logger.Error(msg, fields...)
	case zapcore.DPanicLevel:
		logger.DPanic(msg, fields...)
	case zapcore.PanicLevel:
		logger.Panic(msg, fields...)
	case zapcore.FatalLevel:
		logger.Fatal(msg, fields...)
	case zapcore.InvalidLevel:
		logger.Error(msg, fields...)
	default:
		logger.Error(msg, fields...)
	}
}

// LogRequest logs a request with context.
func LogRequest(ctx context.Context, logger *zap.Logger, msg string, additionalFields ...zap.Field) {
	fields := WithRequestContext(ctx)
	fields = append(fields, additionalFields...)
	logger.Info(msg, fields...)
}

// LogDebug logs debug information with context.
func LogDebug(ctx context.Context, logger *zap.Logger, msg string, additionalFields ...zap.Field) {
	fields := WithRequestContext(ctx)
	fields = append(fields, additionalFields...)
	logger.Debug(msg, fields...)
}

// EnhanceLogger creates a new logger with permanent context fields.
func EnhanceLogger(ctx context.Context, logger *zap.Logger) *zap.Logger {
	fields := WithRequestContext(ctx)
	if len(fields) > 0 {
		return logger.With(fields...)
	}

	return logger
}

const (
	// Error sampling configuration.
	errorSampleRate     = 1000 // Logs per second
	errorSampleInitial  = 100  // Initial logs before sampling
	errorSampleInterval = 10   // Log every Nth after initial
)

// ErrorSampler creates a zapcore sampler for errors based on severity.
func ErrorSampler(core zapcore.Core) zapcore.Core {
	// Sample low severity errors more aggressively
	return zapcore.NewSamplerWithOptions(
		core,
		errorSampleRate,
		errorSampleInitial,
		errorSampleInterval,
	)
}
