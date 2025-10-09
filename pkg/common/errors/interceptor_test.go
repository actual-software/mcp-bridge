package errors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Mock gRPC server stream for testing.
type mockServerStream struct {
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func (m *mockServerStream) SendMsg(_ interface{}) error {
	return nil
}

func (m *mockServerStream) RecvMsg(_ interface{}) error {
	return nil
}

func (m *mockServerStream) SetHeader(metadata.MD) error {
	return nil
}

func (m *mockServerStream) SendHeader(metadata.MD) error {
	return nil
}

func (m *mockServerStream) SetTrailer(metadata.MD) {
}

func TestNewGRPCErrorInterceptor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	if interceptor == nil {
		t.Fatal("NewGRPCErrorInterceptor returned nil")

		return
	}

	if interceptor.logger != logger {
		t.Error("Logger not set correctly")
	}
}

func TestUnaryServerInterceptor_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	// Create interceptor function
	intercept := interceptor.UnaryServerInterceptor()

	// Mock handler that succeeds
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return "success", nil
	}

	// Mock server info
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	ctx := context.Background()

	resp, err := intercept(ctx, "request", info, handler)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if resp != "success" {
		t.Errorf("Expected 'success', got: %v", resp)
	}
}

func TestUnaryServerInterceptor_WithMCPError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	intercept := interceptor.UnaryServerInterceptor()

	// Mock handler that returns MCP error
	mcpErr := &MCPError{
		ErrorInfo: ErrorInfo{
			Code:        GTW_AUTH_INVALID,
			Message:     "Invalid authentication",
			HTTPStatus:  401,
			Recoverable: false,
		},
	}

	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return nil, mcpErr
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	ctx := context.Background()

	resp, err := intercept(ctx, "request", info, handler)

	if resp != nil {
		t.Errorf("Expected nil response, got: %v", resp)
	}

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Check that it's a gRPC status error
	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated code, got: %v", st.Code())
	}

	if st.Message() != "Invalid authentication" {
		t.Errorf("Expected 'Invalid authentication', got: %v", st.Message())
	}
}

func TestUnaryServerInterceptor_WithGenericError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	intercept := interceptor.UnaryServerInterceptor()

	// Mock handler that returns generic error
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return nil, errors.New("generic error")
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	ctx := context.Background()

	resp, err := intercept(ctx, "request", info, handler)

	if resp != nil {
		t.Errorf("Expected nil response, got: %v", resp)
	}

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Should be converted to gRPC status error
	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	// Generic errors should be mapped to Unknown or Internal
	if st.Code() != codes.Unknown && st.Code() != codes.Internal {
		t.Errorf("Expected Unknown or Internal code, got: %v", st.Code())
	}
}

func TestUnaryServerInterceptor_WithExistingGRPCError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	intercept := interceptor.UnaryServerInterceptor()

	// Mock handler that returns existing gRPC error
	grpcErr := status.Error(codes.NotFound, "resource not found")
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return nil, grpcErr
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	ctx := context.Background()

	resp, err := intercept(ctx, "request", info, handler)

	if resp != nil {
		t.Errorf("Expected nil response, got: %v", resp)
	}

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Should preserve existing gRPC error
	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	if st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound code, got: %v", st.Code())
	}
}

func TestUnaryServerInterceptor_WithPanic(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	intercept := interceptor.UnaryServerInterceptor()

	// Mock handler that panics
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		panic("test panic")
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	ctx := context.Background()

	// The panic should be recovered and not crash the test
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Panic was not recovered by interceptor: %v", r)
		}
	}()

	resp, err := intercept(ctx, "request", info, handler)

	// Handler panicked, so we expect nil response and potentially an error
	// The exact behavior depends on implementation details
	_ = resp
	_ = err
}

func TestStreamServerInterceptor_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	intercept := interceptor.StreamServerInterceptor()

	// Mock stream handler that succeeds
	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return nil
	}

	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/TestStreamMethod",
	}

	ctx := context.Background()
	stream := &mockServerStream{ctx: ctx}

	err := intercept("service", stream, info, handler)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestStreamServerInterceptor_WithError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	intercept := interceptor.StreamServerInterceptor()

	// Mock stream handler that returns MCP error
	mcpErr := &MCPError{
		ErrorInfo: ErrorInfo{
			Code:        GTW_RATE_LIMIT_REQ,
			Message:     "Rate limit exceeded",
			HTTPStatus:  429,
			Recoverable: true,
		},
	}

	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return mcpErr
	}

	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/TestStreamMethod",
	}

	ctx := context.Background()
	stream := &mockServerStream{ctx: ctx}

	err := intercept("service", stream, info, handler)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Check that it's a gRPC status error
	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	if st.Code() != codes.ResourceExhausted {
		t.Errorf("Expected ResourceExhausted code, got: %v", st.Code())
	}
}

func TestStreamServerInterceptor_WithPanic(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	intercept := interceptor.StreamServerInterceptor()

	// Mock stream handler that panics
	handler := func(_ interface{}, _ grpc.ServerStream) error {
		panic("stream panic")
	}

	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/TestStreamMethod",
	}

	ctx := context.Background()
	stream := &mockServerStream{ctx: ctx}

	// The panic should be recovered
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Panic was not recovered by stream interceptor: %v", r)
		}
	}()

	err := intercept("service", stream, info, handler)

	// Handler panicked, behavior depends on implementation
	_ = err
}

func TestMapAuthErrorToGRPC(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	tests := []struct {
		errorCode    ErrorCode
		expectedCode codes.Code
		shouldHandle bool
	}{
		{GTW_AUTH_INVALID, codes.Unauthenticated, true},
		{GTW_AUTH_EXPIRED, codes.Unauthenticated, true},
		{GTW_AUTH_MISSING, codes.Unauthenticated, true},
		{GTW_AUTH_INSUFFICIENT, codes.PermissionDenied, true},
		{GTW_SEC_BLOCKED_IP, codes.PermissionDenied, true},
		{CMN_INT_UNKNOWN, codes.OK, false}, // Not an auth error
	}

	for _, tt := range tests {
		t.Run(string(tt.errorCode), func(t *testing.T) {
			code, handled := interceptor.mapAuthErrorToGRPC(tt.errorCode)

			if handled != tt.shouldHandle {
				t.Errorf("Expected handled=%v, got %v", tt.shouldHandle, handled)
			}

			if tt.shouldHandle && code != tt.expectedCode {
				t.Errorf("Expected code %v, got %v", tt.expectedCode, code)
			}
		})
	}
}

func TestMapConnectionErrorToGRPC(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	tests := []struct {
		errorCode    ErrorCode
		expectedCode codes.Code
		shouldHandle bool
	}{
		{GTW_CONN_TIMEOUT, codes.DeadlineExceeded, true},
		{CMN_INT_TIMEOUT, codes.DeadlineExceeded, true},
		{GTW_CONN_REFUSED, codes.Unavailable, true},
		{RTR_CONN_NO_BACKEND, codes.Unavailable, true},
		{GTW_CONN_TLS_FAIL, codes.FailedPrecondition, true},
		{GTW_AUTH_INVALID, codes.OK, false}, // Not a connection error
	}

	for _, tt := range tests {
		t.Run(string(tt.errorCode), func(t *testing.T) {
			code, handled := interceptor.mapConnectionErrorToGRPC(tt.errorCode)

			if handled != tt.shouldHandle {
				t.Errorf("Expected handled=%v, got %v", tt.shouldHandle, handled)
			}

			if tt.shouldHandle && code != tt.expectedCode {
				t.Errorf("Expected code %v, got %v", tt.expectedCode, code)
			}
		})
	}
}

func TestMapToGRPCCode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	tests := []struct {
		errorCode    ErrorCode
		expectedCode codes.Code
	}{
		// Context errors
		{CMN_INT_CONTEXT_CANC, codes.Canceled},

		// Validation errors
		{CMN_VAL_INVALID_REQ, codes.InvalidArgument},
		{CMN_VAL_MISSING_FLD, codes.InvalidArgument},

		// Not found errors
		{RTR_ROUTE_NO_MATCH, codes.NotFound},
		{CMN_PROTO_METHOD_UNK, codes.NotFound},

		// Rate limiting
		{GTW_RATE_LIMIT_REQ, codes.ResourceExhausted},
		{GTW_RATE_LIMIT_CONN, codes.ResourceExhausted},

		// Conflict
		{RTR_ROUTE_CONFLICT, codes.AlreadyExists},

		// Unavailable
		{RTR_ROUTE_DISABLED, codes.Unavailable},
		{RTR_CONN_CIRCUIT_OPEN, codes.Unavailable},

		// Not implemented
		{CMN_INT_NOT_IMPL, codes.Unimplemented},

		// Internal errors
		{CMN_INT_UNKNOWN, codes.Internal},
		{CMN_INT_PANIC, codes.Internal},

		// Unknown error codes should map to Unknown
		{ErrorCode("UNKNOWN_ERROR"), codes.Unknown},
	}

	for _, tt := range tests {
		t.Run(string(tt.errorCode), func(t *testing.T) {
			code := interceptor.mapToGRPCCode(tt.errorCode)

			if code != tt.expectedCode {
				t.Errorf("Expected code %v, got %v", tt.expectedCode, code)
			}
		})
	}
}

func TestConvertToGRPCError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	mcpErr := &MCPError{
		ErrorInfo: ErrorInfo{
			Code:        GTW_AUTH_INVALID,
			Message:     "Authentication failed",
			HTTPStatus:  401,
			Recoverable: false,
		},
	}

	ctx := context.Background()

	grpcErr := interceptor.convertToGRPCError(ctx, mcpErr)
	if grpcErr == nil {
		t.Fatal("Expected error, got nil")
	}

	st, ok := status.FromError(grpcErr)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated code, got: %v", st.Code())
	}

	if st.Message() != "Authentication failed" {
		t.Errorf("Expected 'Authentication failed', got: %v", st.Message())
	}
}

func TestLogGRPCError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	tests := []struct {
		name     string
		code     codes.Code
		message  string
		method   string
		metadata map[string]string
	}{
		{
			name:    "internal_error",
			code:    codes.Internal,
			message: "Internal server error",
			method:  "/test.Service/TestMethod",
		},
		{
			name:    "auth_error",
			code:    codes.Unauthenticated,
			message: "Authentication required",
			method:  "/test.Service/SecureMethod",
		},
		{
			name:    "not_found",
			code:    codes.NotFound,
			message: "Resource not found",
			method:  "/test.Service/GetResource",
		},
		{
			name:    "with_request_id",
			code:    codes.InvalidArgument,
			message: "Bad request",
			method:  "/test.Service/ValidateRequest",
			metadata: map[string]string{
				"request-id": "12345",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			ctx := context.Background()

			// Add metadata if provided
			if tt.metadata != nil {
				md := metadata.New(tt.metadata)
				ctx = metadata.NewIncomingContext(ctx, md)
			}

			err := status.Error(tt.code, tt.message)

			// This should not panic and should log appropriately
			interceptor.logGRPCError(ctx, err, tt.method)
		})
	}
}

func TestHandleError_FallbackPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	// Create a generic error that's not MCP or gRPC
	genericErr := errors.New("some random error")

	ctx := context.Background()

	result := interceptor.handleError(ctx, genericErr, "/test.Service/TestMethod")
	if result == nil {
		t.Fatal("Expected error, got nil")
	}

	st, ok := status.FromError(result)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	// Should use fallback mapping
	if st.Code() != codes.Internal && st.Code() != codes.Unknown {
		t.Errorf("Expected Internal or Unknown code, got: %v", st.Code())
	}
}

func TestFormatBool(t *testing.T) {
	tests := []struct {
		input    bool
		expected string
	}{
		{true, "true"},
		{false, "false"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("bool_%v", tt.input), func(t *testing.T) {
			result := formatBool(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestErrorDetails(t *testing.T) {
	details := ErrorDetails{
		Code:      "TEST_ERROR",
		Details:   "Test error details",
		Retryable: true,
		Timestamp: "2023-01-01T00:00:00Z",
	}

	if details.Code != "TEST_ERROR" {
		t.Errorf("Expected Code 'TEST_ERROR', got %s", details.Code)
	}

	if details.Details != "Test error details" {
		t.Errorf("Expected Details 'Test error details', got %s", details.Details)
	}

	if !details.Retryable {
		t.Error("Expected Retryable to be true")
	}

	if details.Timestamp != "2023-01-01T00:00:00Z" {
		t.Errorf("Expected Timestamp '2023-01-01T00:00:00Z', got %s", details.Timestamp)
	}
}

// Integration test showing realistic usage.
func TestInterceptor_IntegrationFlow(t *testing.T) {
	logger := zaptest.NewLogger(t)
	interceptor := NewGRPCErrorInterceptor(logger)

	// Set up integration test components
	handler, info, ctx := setupIntegrationFlowTest()

	// Execute interceptor flow
	resp, err := executeInterceptorFlow(interceptor, ctx, handler, info)

	// Verify complete integration flow results
	verifyIntegrationFlowResults(t, resp, err)
}

func setupIntegrationFlowTest() (grpc.UnaryHandler, *grpc.UnaryServerInfo, context.Context) {
	// Handler that returns an MCP authentication error
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return nil, Error(GTW_AUTH_EXPIRED, "Token has expired")
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/auth.Service/ValidateToken",
	}

	// Add request metadata
	md := metadata.Pairs("request-id", "req-123", "user-id", "user-456")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	return handler, info, ctx
}

func executeInterceptorFlow(interceptor *GRPCErrorInterceptor, ctx context.Context,
	handler grpc.UnaryHandler, info *grpc.UnaryServerInfo) (interface{}, error) {
	// Simulate a complete flow from unary interceptor to error conversion
	unaryIntercept := interceptor.UnaryServerInterceptor()

	return unaryIntercept(ctx, map[string]string{"token": "expired-token"}, info, handler)
}

func verifyIntegrationFlowResults(t *testing.T, resp interface{}, err error) {
	t.Helper()
	// Verify the response
	if resp != nil {
		t.Errorf("Expected nil response, got: %v", resp)
	}

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// Verify it's properly converted to gRPC error
	st, ok := status.FromError(err)
	if !ok {
		t.Fatal("Expected gRPC status error")
	}

	if st.Code() != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated, got: %v", st.Code())
	}

	if st.Message() != "Authentication token has expired" {
		t.Errorf("Expected 'Authentication token has expired', got: %v", st.Message())
	}
}
