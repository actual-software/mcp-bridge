package errors

import (
	"context"
	"errors"
	"net/http"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// GRPCErrorInterceptor provides gRPC error handling.
type GRPCErrorInterceptor struct {
	logger *zap.Logger
}

// NewGRPCErrorInterceptor creates a new gRPC error interceptor.
func NewGRPCErrorInterceptor(logger *zap.Logger) *GRPCErrorInterceptor {
	return &GRPCErrorInterceptor{
		logger: logger,
	}
}

// UnaryServerInterceptor creates a unary server interceptor for error handling.
func (i *GRPCErrorInterceptor) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Handle panics
		defer func() {
			if err := recover(); err != nil {
				i.logger.Error("Panic in gRPC handler",
					zap.Any("panic", err),
					zap.String("method", info.FullMethod),
				)
			}
		}()

		// Call handler
		resp, err := handler(ctx, req)
		if err != nil {
			return nil, i.handleError(ctx, err, info.FullMethod)
		}

		return resp, nil
	}
}

// StreamServerInterceptor creates a stream server interceptor for error handling.
func (i *GRPCErrorInterceptor) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Handle panics
		defer func() {
			if err := recover(); err != nil {
				i.logger.Error("Panic in gRPC stream handler",
					zap.Any("panic", err),
					zap.String("method", info.FullMethod),
				)
			}
		}()

		// Call handler
		err := handler(srv, ss)
		if err != nil {
			return i.handleError(ss.Context(), err, info.FullMethod)
		}

		return nil
	}
}

// handleError processes and enriches gRPC errors.
func (i *GRPCErrorInterceptor) handleError(ctx context.Context, err error, method string) error {
	// Check if it's already a gRPC status error
	if _, ok := status.FromError(err); ok {
		i.logGRPCError(ctx, err, method)

		return err
	}

	// Check if it's our error type
	var mcpErr *MCPError
	if errors.As(err, &mcpErr) {
		grpcErr := i.convertToGRPCError(ctx, mcpErr)
		i.logGRPCError(ctx, grpcErr, method)

		return grpcErr
	}

	// Unknown error - wrap it
	var wrappedMcpErr *MCPError
	if errors.As(Error(CMN_INT_UNKNOWN, err.Error()), &wrappedMcpErr) {
		grpcErr := i.convertToGRPCError(ctx, wrappedMcpErr)
		i.logGRPCError(ctx, grpcErr, method)

		return grpcErr
	}

	// Fallback - should never happen
	fallbackErr := &MCPError{
		ErrorInfo: ErrorInfo{
			Code:        CMN_INT_UNKNOWN,
			Message:     "Unknown error",
			Details:     nil, // No additional details
			HTTPStatus:  http.StatusInternalServerError,
			Recoverable: false,
			RetryAfter:  0, // No retry
		},
	}
	grpcErr := i.convertToGRPCError(ctx, fallbackErr)
	i.logGRPCError(ctx, grpcErr, method)

	return grpcErr
}

// convertToGRPCError converts our error type to gRPC status.
func (i *GRPCErrorInterceptor) convertToGRPCError(ctx context.Context, mcpErr *MCPError) error {
	code := i.mapToGRPCCode(mcpErr.Code)

	// Create status with details
	st := status.New(code, mcpErr.Message)

	// Add metadata
	md := metadata.Pairs(
		"error-code", string(mcpErr.Code),
		"retryable", formatBool(mcpErr.Recoverable),
	)

	// Attach metadata to context
	if err := grpc.SetTrailer(ctx, md); err != nil {
		i.logger.Warn("Failed to set gRPC trailer", zap.Error(err))
	}

	return st.Err()
}

// mapAuthErrorToGRPC maps authentication and authorization errors to gRPC codes.
func (i *GRPCErrorInterceptor) mapAuthErrorToGRPC(code ErrorCode) (codes.Code, bool) {
	switch code { 
	case GTW_AUTH_MISSING, GTW_AUTH_INVALID, GTW_AUTH_EXPIRED, GTW_AUTH_REVOKED,
		GTW_AUTH_METHOD_UNK, GTW_AUTH_CERT_FAIL, GTW_AUTH_OAUTH_FAIL:
		return codes.Unauthenticated, true
	case GTW_AUTH_INSUFFICIENT, GTW_SEC_BLOCKED_IP, GTW_SEC_BLOCKED_UA, GTW_SEC_MALICIOUS, GTW_SEC_CSRF_FAIL:
		return codes.PermissionDenied, true
	// Non-auth errors - return false to indicate not handled
	case CMN_INT_UNKNOWN, CMN_INT_PANIC, CMN_INT_TIMEOUT, CMN_INT_CONTEXT_CANC, CMN_INT_NOT_IMPL,
		CMN_VAL_INVALID_REQ, CMN_VAL_MISSING_FLD, CMN_VAL_INVALID_TYPE, CMN_VAL_OUT_OF_RANGE, CMN_VAL_PATTERN_FAIL,
		CMN_PROTO_INVALID_VER, CMN_PROTO_PARSE_ERR, CMN_PROTO_MARSHAL_ERR, CMN_PROTO_METHOD_UNK, CMN_PROTO_BATCH_ERR,
		GTW_CONN_REFUSED, GTW_CONN_TIMEOUT, GTW_CONN_CLOSED, GTW_CONN_LIMIT, GTW_CONN_TLS_FAIL, GTW_CONN_UPGRADE_FAIL,
		GTW_RATE_LIMIT_REQ, GTW_RATE_LIMIT_CONN, GTW_RATE_LIMIT_BURST, GTW_RATE_LIMIT_QUOTA,
		RTR_CONN_NO_BACKEND, RTR_CONN_BACKEND_ERR, RTR_CONN_POOL_FULL, RTR_CONN_UNHEALTHY, RTR_CONN_CIRCUIT_OPEN,
		RTR_ROUTE_NO_MATCH, RTR_ROUTE_CONFLICT, RTR_ROUTE_DISABLED, RTR_ROUTE_REDIRECT,
		RTR_PROTO_MISMATCH, RTR_PROTO_TRANSFORM, RTR_PROTO_SIZE_LIMIT:
		return codes.OK, false
	}
	// Should never reach here - all error codes handled above
	return codes.OK, false
}

// mapConnectionErrorToGRPC maps connection-related errors to gRPC codes.
func (i *GRPCErrorInterceptor) mapConnectionErrorToGRPC(code ErrorCode) (codes.Code, bool) {
	switch code { 
	case CMN_INT_TIMEOUT, GTW_CONN_TIMEOUT:
		return codes.DeadlineExceeded, true
	case GTW_CONN_REFUSED, GTW_CONN_CLOSED, GTW_CONN_LIMIT,
		RTR_CONN_NO_BACKEND, RTR_CONN_BACKEND_ERR, RTR_CONN_UNHEALTHY, RTR_CONN_POOL_FULL:
		return codes.Unavailable, true
	case GTW_CONN_TLS_FAIL, GTW_CONN_UPGRADE_FAIL:
		return codes.FailedPrecondition, true
	case RTR_CONN_CIRCUIT_OPEN:
		return codes.Unavailable, true
	// Non-connection errors - return false to indicate not handled  
	case CMN_INT_UNKNOWN, CMN_INT_PANIC, CMN_INT_CONTEXT_CANC, CMN_INT_NOT_IMPL,
		CMN_VAL_INVALID_REQ, CMN_VAL_MISSING_FLD, CMN_VAL_INVALID_TYPE, CMN_VAL_OUT_OF_RANGE, CMN_VAL_PATTERN_FAIL,
		CMN_PROTO_INVALID_VER, CMN_PROTO_PARSE_ERR, CMN_PROTO_MARSHAL_ERR, CMN_PROTO_METHOD_UNK, CMN_PROTO_BATCH_ERR,
		GTW_AUTH_MISSING, GTW_AUTH_INVALID, GTW_AUTH_EXPIRED, GTW_AUTH_INSUFFICIENT, GTW_AUTH_REVOKED,
		GTW_AUTH_METHOD_UNK, GTW_AUTH_CERT_FAIL, GTW_AUTH_OAUTH_FAIL,
		GTW_RATE_LIMIT_REQ, GTW_RATE_LIMIT_CONN, GTW_RATE_LIMIT_BURST, GTW_RATE_LIMIT_QUOTA,
		GTW_SEC_BLOCKED_IP, GTW_SEC_BLOCKED_UA, GTW_SEC_MALICIOUS, GTW_SEC_CSRF_FAIL,
		RTR_ROUTE_NO_MATCH, RTR_ROUTE_CONFLICT, RTR_ROUTE_DISABLED, RTR_ROUTE_REDIRECT,
		RTR_PROTO_MISMATCH, RTR_PROTO_TRANSFORM, RTR_PROTO_SIZE_LIMIT:
		return codes.OK, false
	}
	// Should never reach here - all error codes handled above
	return codes.OK, false
}

// mapToGRPCCode maps our error codes to gRPC codes.
//

func (i *GRPCErrorInterceptor) mapToGRPCCode(code ErrorCode) codes.Code {
	// Try authentication/authorization errors first
	if grpcCode, handled := i.mapAuthErrorToGRPC(code); handled {
		return grpcCode
	}

	// Try connection errors
	if grpcCode, handled := i.mapConnectionErrorToGRPC(code); handled {
		return grpcCode
	}

	// Try validation and protocol errors
	if grpcCode, handled := i.mapValidationErrorToGRPC(code); handled {
		return grpcCode
	}

	// Try routing errors
	if grpcCode, handled := i.mapRoutingErrorToGRPC(code); handled {
		return grpcCode
	}

	// Handle remaining specific errors
	if grpcCode, handled := i.mapInternalErrorToGRPC(code); handled {
		return grpcCode
	}

	return codes.Unknown
}

func (i *GRPCErrorInterceptor) mapValidationErrorToGRPC(code ErrorCode) (codes.Code, bool) {
	switch code {
	case CMN_VAL_INVALID_REQ, CMN_VAL_INVALID_TYPE, CMN_VAL_MISSING_FLD, CMN_VAL_OUT_OF_RANGE, CMN_VAL_PATTERN_FAIL:
		return codes.InvalidArgument, true
	case CMN_PROTO_INVALID_VER, CMN_PROTO_PARSE_ERR, CMN_PROTO_MARSHAL_ERR, CMN_PROTO_BATCH_ERR:
		return codes.InvalidArgument, true
	case RTR_PROTO_MISMATCH, RTR_PROTO_TRANSFORM, RTR_PROTO_SIZE_LIMIT:
		return codes.InvalidArgument, true
	// Non-validation errors - return false to indicate not handled
	case CMN_INT_UNKNOWN, CMN_INT_PANIC, CMN_INT_TIMEOUT, CMN_INT_CONTEXT_CANC, CMN_INT_NOT_IMPL,
		CMN_PROTO_METHOD_UNK,
		GTW_AUTH_MISSING, GTW_AUTH_INVALID, GTW_AUTH_EXPIRED, GTW_AUTH_INSUFFICIENT, GTW_AUTH_REVOKED,
		GTW_AUTH_METHOD_UNK, GTW_AUTH_CERT_FAIL, GTW_AUTH_OAUTH_FAIL,
		GTW_CONN_REFUSED, GTW_CONN_TIMEOUT, GTW_CONN_CLOSED, GTW_CONN_LIMIT, GTW_CONN_TLS_FAIL, GTW_CONN_UPGRADE_FAIL,
		GTW_RATE_LIMIT_REQ, GTW_RATE_LIMIT_CONN, GTW_RATE_LIMIT_BURST, GTW_RATE_LIMIT_QUOTA,
		GTW_SEC_BLOCKED_IP, GTW_SEC_BLOCKED_UA, GTW_SEC_MALICIOUS, GTW_SEC_CSRF_FAIL,
		RTR_CONN_NO_BACKEND, RTR_CONN_BACKEND_ERR, RTR_CONN_POOL_FULL, RTR_CONN_UNHEALTHY, RTR_CONN_CIRCUIT_OPEN,
		RTR_ROUTE_NO_MATCH, RTR_ROUTE_CONFLICT, RTR_ROUTE_DISABLED, RTR_ROUTE_REDIRECT:
		return codes.OK, false
	}
	// Should never reach here - all error codes handled above
	return codes.OK, false
}

func (i *GRPCErrorInterceptor) mapRoutingErrorToGRPC(code ErrorCode) (codes.Code, bool) {
	switch code {
	case RTR_ROUTE_NO_MATCH, CMN_PROTO_METHOD_UNK:
		return codes.NotFound, true
	case RTR_ROUTE_CONFLICT:
		return codes.AlreadyExists, true
	case RTR_ROUTE_DISABLED, RTR_CONN_CIRCUIT_OPEN:
		return codes.Unavailable, true
	case RTR_ROUTE_REDIRECT:
		return codes.FailedPrecondition, true
	case GTW_RATE_LIMIT_REQ, GTW_RATE_LIMIT_CONN, GTW_RATE_LIMIT_BURST, GTW_RATE_LIMIT_QUOTA:
		return codes.ResourceExhausted, true
	// Non-routing errors - return false to indicate not handled
	case CMN_INT_UNKNOWN, CMN_INT_PANIC, CMN_INT_TIMEOUT, CMN_INT_CONTEXT_CANC, CMN_INT_NOT_IMPL,
		CMN_VAL_INVALID_REQ, CMN_VAL_MISSING_FLD, CMN_VAL_INVALID_TYPE, CMN_VAL_OUT_OF_RANGE, CMN_VAL_PATTERN_FAIL,
		CMN_PROTO_INVALID_VER, CMN_PROTO_PARSE_ERR, CMN_PROTO_MARSHAL_ERR, CMN_PROTO_BATCH_ERR,
		GTW_AUTH_MISSING, GTW_AUTH_INVALID, GTW_AUTH_EXPIRED, GTW_AUTH_INSUFFICIENT, GTW_AUTH_REVOKED,
		GTW_AUTH_METHOD_UNK, GTW_AUTH_CERT_FAIL, GTW_AUTH_OAUTH_FAIL,
		GTW_CONN_REFUSED, GTW_CONN_TIMEOUT, GTW_CONN_CLOSED, GTW_CONN_LIMIT, GTW_CONN_TLS_FAIL, GTW_CONN_UPGRADE_FAIL,
		GTW_SEC_BLOCKED_IP, GTW_SEC_BLOCKED_UA, GTW_SEC_MALICIOUS, GTW_SEC_CSRF_FAIL,
		RTR_CONN_NO_BACKEND, RTR_CONN_BACKEND_ERR, RTR_CONN_POOL_FULL, RTR_CONN_UNHEALTHY,
		RTR_PROTO_MISMATCH, RTR_PROTO_TRANSFORM, RTR_PROTO_SIZE_LIMIT:
		return codes.OK, false
	}
	// Should never reach here - all error codes handled above
	return codes.OK, false
}

func (i *GRPCErrorInterceptor) mapInternalErrorToGRPC(code ErrorCode) (codes.Code, bool) {
	switch code {
	case CMN_INT_CONTEXT_CANC:
		return codes.Canceled, true
	case CMN_INT_NOT_IMPL:
		return codes.Unimplemented, true
	case CMN_INT_UNKNOWN, CMN_INT_PANIC:
		return codes.Internal, true
	case CMN_INT_TIMEOUT:
		return codes.DeadlineExceeded, true
	// Non-internal errors - return false to indicate not handled
	case CMN_VAL_INVALID_REQ, CMN_VAL_MISSING_FLD, CMN_VAL_INVALID_TYPE, CMN_VAL_OUT_OF_RANGE, CMN_VAL_PATTERN_FAIL,
		CMN_PROTO_INVALID_VER, CMN_PROTO_PARSE_ERR, CMN_PROTO_MARSHAL_ERR, CMN_PROTO_METHOD_UNK, CMN_PROTO_BATCH_ERR,
		GTW_AUTH_MISSING, GTW_AUTH_INVALID, GTW_AUTH_EXPIRED, GTW_AUTH_INSUFFICIENT, GTW_AUTH_REVOKED,
		GTW_AUTH_METHOD_UNK, GTW_AUTH_CERT_FAIL, GTW_AUTH_OAUTH_FAIL,
		GTW_CONN_REFUSED, GTW_CONN_TIMEOUT, GTW_CONN_CLOSED, GTW_CONN_LIMIT, GTW_CONN_TLS_FAIL, GTW_CONN_UPGRADE_FAIL,
		GTW_RATE_LIMIT_REQ, GTW_RATE_LIMIT_CONN, GTW_RATE_LIMIT_BURST, GTW_RATE_LIMIT_QUOTA,
		GTW_SEC_BLOCKED_IP, GTW_SEC_BLOCKED_UA, GTW_SEC_MALICIOUS, GTW_SEC_CSRF_FAIL,
		RTR_CONN_NO_BACKEND, RTR_CONN_BACKEND_ERR, RTR_CONN_POOL_FULL, RTR_CONN_UNHEALTHY, RTR_CONN_CIRCUIT_OPEN,
		RTR_ROUTE_NO_MATCH, RTR_ROUTE_CONFLICT, RTR_ROUTE_DISABLED, RTR_ROUTE_REDIRECT,
		RTR_PROTO_MISMATCH, RTR_PROTO_TRANSFORM, RTR_PROTO_SIZE_LIMIT:
		return codes.OK, false
	}
	// Should never reach here - all error codes handled above
	return codes.OK, false
}

// logGRPCError logs gRPC errors with context.
func (i *GRPCErrorInterceptor) logGRPCError(ctx context.Context, err error, method string) {
	st, _ := status.FromError(err)

	fields := []zap.Field{
		zap.String("method", method),
		zap.String("grpc_code", st.Code().String()),
		zap.String("message", st.Message()),
	}

	// Add request ID if available
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if requestIDs := md.Get("request-id"); len(requestIDs) > 0 {
			fields = append(fields, zap.String("request_id", requestIDs[0]))
		}
	}

	// Log based on severity
	switch st.Code() {
	case codes.Internal, codes.Unknown, codes.DataLoss:
		i.logger.Error("gRPC error", fields...)
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		i.logger.Warn("gRPC error", fields...)
	case codes.OK:
		// OK status - no need to log as error
		i.logger.Debug("gRPC success", fields...)
	case codes.Canceled, codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.FailedPrecondition, codes.Aborted, codes.OutOfRange,
		codes.Unimplemented, codes.Unauthenticated:
		i.logger.Info("gRPC error", fields...)
	default:
		i.logger.Info("gRPC error", fields...)
	}
}

// ErrorDetails contains additional error information.
type ErrorDetails struct {
	Code      string `json:"code"`
	Details   string `json:"details,omitempty"`
	Retryable bool   `json:"retryable"`
	Timestamp string `json:"timestamp"`
}

// formatBool converts bool to string.
func formatBool(b bool) string {
	if b {
		return "true"
	}

	return "false"
}
