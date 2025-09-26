package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// loggingContextKey is a type for logging-specific context keys.
type loggingContextKey string

const (
	// ID generation sizes.
	traceIDSize   = 16 // Bytes for trace ID
	requestIDSize = 8  // Bytes for request ID

	// Context keys for logging and tracing.
	loggingContextKeyTraceID          loggingContextKey = "trace_id"
	loggingContextKeyRequestID        loggingContextKey = "request_id"
	loggingContextKeyRequestStartTime loggingContextKey = "request_start_time"
	loggingContextKeyUserID           loggingContextKey = "user_id"
	loggingContextKeySessionID        loggingContextKey = "session_id"
	loggingContextKeyNamespace        loggingContextKey = "namespace"
	loggingContextKeyMethod           loggingContextKey = "method"
)

// GenerateTraceID generates a unique trace ID.
func GenerateTraceID() string {
	b := make([]byte, traceIDSize)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("trace_%d", time.Now().UnixNano())
	}

	return hex.EncodeToString(b)
}

// GenerateRequestID generates a unique request ID.
func GenerateRequestID() string {
	b := make([]byte, requestIDSize)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("req_%d", time.Now().UnixNano())
	}

	return hex.EncodeToString(b)
}

// ContextWithTracing adds tracing information to context.
func ContextWithTracing(ctx context.Context, traceID, requestID string) context.Context {
	ctx = context.WithValue(ctx, loggingContextKeyTraceID, traceID)
	ctx = context.WithValue(ctx, loggingContextKeyRequestID, requestID)
	ctx = context.WithValue(ctx, loggingContextKeyRequestStartTime, time.Now())

	return ctx
}

// ContextWithUserInfo adds user information to context.
func ContextWithUserInfo(ctx context.Context, userID, sessionID string) context.Context {
	if userID != "" {
		ctx = context.WithValue(ctx, loggingContextKeyUserID, userID)
	}

	if sessionID != "" {
		ctx = context.WithValue(ctx, loggingContextKeySessionID, sessionID)
	}

	return ctx
}

// ContextWithNamespace adds namespace information to context.
func ContextWithNamespace(ctx context.Context, namespace string) context.Context {
	if namespace != "" {
		ctx = context.WithValue(ctx, loggingContextKeyNamespace, namespace)
	}

	return ctx
}

// ContextWithMethod adds method information to context.
func ContextWithMethod(ctx context.Context, method string) context.Context {
	if method != "" {
		ctx = context.WithValue(ctx, loggingContextKeyMethod, method)
	}

	return ctx
}

// GetTraceID retrieves trace ID from context.
func GetTraceID(ctx context.Context) string {
	if traceID := ctx.Value(loggingContextKeyTraceID); traceID != nil {
		if traceIDStr, ok := traceID.(string); ok {
			return traceIDStr
		}
	}

	return ""
}

// GetRequestID retrieves request ID from context.
func GetRequestID(ctx context.Context) string {
	if requestID := ctx.Value(loggingContextKeyRequestID); requestID != nil {
		if reqIDStr, ok := requestID.(string); ok {
			return reqIDStr
		}
	}

	return ""
}

// GetRequestDuration calculates request duration from context.
func GetRequestDuration(ctx context.Context) time.Duration {
	if startTime := ctx.Value(loggingContextKeyRequestStartTime); startTime != nil {
		if startTimeVal, ok := startTime.(time.Time); ok {
			return time.Since(startTimeVal)
		}
	}

	return 0
}

// LogRequestComplete logs request completion with duration.
func LogRequestComplete(ctx context.Context, logger *zap.Logger, statusCode int, err error) {
	duration := GetRequestDuration(ctx)

	fields := WithRequestContext(ctx)
	fields = append(fields,
		zap.Duration("duration", duration),
		zap.Int("status_code", statusCode),
	)

	if err != nil {
		fields = append(fields, WithError(err)...)
		logger.Error("request failed", fields...)
	} else {
		logger.Info("request completed", fields...)
	}
}
