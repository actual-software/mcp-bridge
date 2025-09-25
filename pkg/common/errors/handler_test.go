package errors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// errorHandlerTestCase represents a test case for error handler testing.
type errorHandlerTestCase struct {
	name           string
	err            error
	requestID      string
	expectedStatus int
	expectedCode   string
}

// setupTestRequest creates a test request with optional request ID in context.
func setupTestRequest(requestID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)

	if requestID != "" {
		ctx := context.WithValue(req.Context(), requestIDKey, requestID)
		req = req.WithContext(ctx)
	}

	return req
}

// validateHTTPResponse validates the HTTP status code and headers.
func validateHTTPResponse(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int,
	expectedCode, requestID string) {
	t.Helper()
	// Check status code
	if w.Code != expectedStatus {
		t.Errorf("Expected status %d, got %d", expectedStatus, w.Code)
	}

	// Check content type
	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Expected JSON content type")
	}

	// Check error code header
	if w.Header().Get("X-Error-Code") != expectedCode {
		t.Errorf("Expected error code header %s, got %s", expectedCode, w.Header().Get("X-Error-Code"))
	}

	// Check request ID header
	if requestID != "" {
		if w.Header().Get("X-Request-ID") != requestID {
			t.Errorf("Expected request ID header %s, got %s", requestID, w.Header().Get("X-Request-ID"))
		}
	}
}

// validateErrorResponse validates the parsed JSON error response.
func validateErrorResponse(t *testing.T, response *ErrorResponse, expectedCode, requestID string) {
	t.Helper()
	// Check response fields
	if response.Error.Code != expectedCode {
		t.Errorf("Expected error code %s, got %s", expectedCode, response.Error.Code)
	}

	if response.RequestID != requestID {
		t.Errorf("Expected request ID %s, got %s", requestID, response.RequestID)
	}

	if response.Timestamp == "" {
		t.Error("Expected non-empty timestamp")
	}
}

// parseErrorResponse parses the HTTP response body into an ErrorResponse struct.
func parseErrorResponse(t *testing.T, w *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()

	var response ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	return response
}

// getErrorHandlerTestCases returns the test cases for error handler testing.
func getErrorHandlerTestCases() []errorHandlerTestCase {
	return []errorHandlerTestCase{
		{
			name:           "MCP error",
			err:            Error(GTW_AUTH_MISSING),
			requestID:      "test-123", // Test request ID
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "GTW_AUTH_001",
		},
		{
			name:           "MCP error with details",
			err:            Error(GTW_CONN_TIMEOUT, "Connection to backend failed"),
			requestID:      "test-456", // Test request ID
			expectedStatus: http.StatusGatewayTimeout,
			expectedCode:   "GTW_CONN_002",
		},
		{
			name:           "Unknown error",
			err:            http.ErrServerClosed,
			requestID:      "test-789", // Test request ID
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "CMN_INT_001",
		},
		{
			name:           "With request ID",
			err:            Error(GTW_AUTH_MISSING),
			requestID:      "req-123",
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "GTW_AUTH_001",
		},
	}
}

func TestErrorHandler_HandleError(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	handler := NewErrorHandler(logger)
	tests := getErrorHandlerTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			req := setupTestRequest(tt.requestID)

			handler.HandleError(w, req, tt.err)

			validateHTTPResponse(t, w, tt.expectedStatus, tt.expectedCode, tt.requestID)
			response := parseErrorResponse(t, w)
			validateErrorResponse(t, &response, tt.expectedCode, tt.requestID)
		})
	}
}

func TestErrorHandler_HandleJSONRPCError(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	handler := NewErrorHandler(logger)

	tests := []struct {
		name         string
		err          error
		expectedCode int
	}{
		{
			name:         "Protocol parse error",
			err:          Error(CMN_PROTO_PARSE_ERR),
			expectedCode: -32700,
		},
		{
			name:         "Unknown method",
			err:          Error(CMN_PROTO_METHOD_UNK),
			expectedCode: -32601,
		},
		{
			name:         "Validation error",
			err:          Error(CMN_VAL_INVALID_REQ),
			expectedCode: -32602,
		},
		{
			name:         "Internal error",
			err:          Error(CMN_INT_UNKNOWN),
			expectedCode: -32603,
		},
		{
			name:         "Application error",
			err:          Error(GTW_AUTH_MISSING),
			expectedCode: -32000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := handler.HandleJSONRPCError(tt.err)

			code, ok := result["code"].(int)
			if !ok {
				t.Fatal("Expected code to be int")
			}

			if code != tt.expectedCode {
				t.Errorf("Expected JSON-RPC code %d, got %d", tt.expectedCode, code)
			}

			if result["message"] == nil {
				t.Error("Expected non-nil message")
			}

			if result["data"] == nil {
				t.Error("Expected non-nil data")
			}
		})
	}
}

func TestErrorHandlerMiddleware(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	handler := NewErrorHandler(logger)
	middleware := ErrorHandlerMiddleware(handler)

	t.Run("Normal handler", func(t *testing.T) {
		t.Parallel()

		normalHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		})

		wrappedHandler := middleware(normalHandler)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)

		wrappedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		if w.Body.String() != "OK" {
			t.Errorf("Expected body 'OK', got %s", w.Body.String())
		}
	})

	t.Run("Panic recovery", func(t *testing.T) {
		t.Parallel()

		panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic("test panic")
		})

		wrappedHandler := middleware(panicHandler)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)

		wrappedHandler.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", w.Code)
		}

		// Should contain error response
		var response ErrorResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode error response: %v", err)
		}

		if response.Error.Code != "CMN_INT_002" {
			t.Errorf("Expected error code CMN_INT_002, got %s", response.Error.Code)
		}
	})
}

func TestResponseWriterWrapper(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	handler := NewErrorHandler(logger)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)

	wrapper := &responseWriterWrapper{
		ResponseWriter: w,
		handler:        handler,
		request:        req,
		wroteHeader:    false, // Initialize header tracking
	}

	t.Run("Write header once", func(t *testing.T) {
		t.Parallel()
		wrapper.WriteHeader(http.StatusOK)
		wrapper.WriteHeader(http.StatusBadRequest) // Should be ignored

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("Write without explicit header", func(t *testing.T) {
		t.Parallel()

		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)

		wrapper2 := &responseWriterWrapper{
			ResponseWriter: w2,
			handler:        handler,
			request:        req2,
			wroteHeader:    false, // Initialize header tracking
		}

		_, _ = wrapper2.Write([]byte("test"))

		if w2.Code != http.StatusOK {
			t.Errorf("Expected default status 200, got %d", w2.Code)
		}

		if w2.Body.String() != "test" {
			t.Errorf("Expected body 'test', got %s", w2.Body.String())
		}
	})
}

func TestGetHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{
			name:     "MCP error",
			err:      Error(GTW_AUTH_MISSING),
			expected: http.StatusUnauthorized,
		},
		{
			name:     "Non-MCP error",
			err:      http.ErrServerClosed,
			expected: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := GetHTTPStatus(tt.err)

			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetErrorCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected ErrorCode
	}{
		{
			name:     "MCP error",
			err:      Error(GTW_AUTH_MISSING),
			expected: GTW_AUTH_MISSING,
		},
		{
			name:     "Non-MCP error",
			err:      http.ErrServerClosed,
			expected: CMN_INT_UNKNOWN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := GetErrorCode(tt.err)

			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func BenchmarkErrorHandler(b *testing.B) {
	logger := zap.NewNop()
	handler := NewErrorHandler(logger)

	b.Run("Handle MCP error", func(b *testing.B) {
		err := Error(GTW_AUTH_MISSING)

		for range b.N {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
			handler.HandleError(w, req, err)
		}
	})

	b.Run("Handle JSON-RPC error", func(b *testing.B) {
		err := Error(CMN_PROTO_PARSE_ERR)
		for range b.N {
			_ = handler.HandleJSONRPCError(err)
		}
	})
}
