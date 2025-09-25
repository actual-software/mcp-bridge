package errors

import (
	"errors"
	"net/http"
	"testing"
)

func TestErrorCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code           ErrorCode
		expectedStatus int
		retryable      bool
	}{
		{CMN_INT_UNKNOWN, http.StatusInternalServerError, false},
		{CMN_INT_TIMEOUT, http.StatusGatewayTimeout, true},
		{GTW_AUTH_MISSING, http.StatusUnauthorized, false},
		{GTW_CONN_TIMEOUT, http.StatusGatewayTimeout, true},
		{RTR_CONN_NO_BACKEND, http.StatusServiceUnavailable, true},
		{CMN_VAL_INVALID_REQ, http.StatusBadRequest, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			t.Parallel()

			info, exists := GetErrorInfo(tt.code)
			if !exists {
				t.Fatalf("Error code %s not found", tt.code)
			}

			if info.HTTPStatus != tt.expectedStatus {
				t.Errorf("Expected HTTP status %d, got %d", tt.expectedStatus, info.HTTPStatus)
			}

			if info.Recoverable != tt.retryable {
				t.Errorf("Expected retryable %v, got %v", tt.retryable, info.Recoverable)
			}

			if info.Code != tt.code {
				t.Errorf("Expected code %s, got %s", tt.code, info.Code)
			}

			if info.Message == "" {
				t.Error("Expected non-empty message")
			}
		})
	}
}

func TestMCPError(t *testing.T) { 
	t.Parallel()
	t.Run("Create error with code", testCreateErrorWithCode)
	t.Run("Create error with details", testCreateErrorWithDetails)
	t.Run("WithDetails method", testWithDetailsMethod)
	t.Run("Unknown error code", testUnknownErrorCode)
}

func testCreateErrorWithCode(t *testing.T) {
	t.Parallel()

	err := Error(GTW_AUTH_MISSING)

	var mcpErr *MCPError

	ok := errors.As(err, &mcpErr)

	if !ok {
		t.Fatal("Expected MCPError type")
	}

	if mcpErr.Code != GTW_AUTH_MISSING {
		t.Errorf("Expected code %s, got %s", GTW_AUTH_MISSING, mcpErr.Code)
	}

	if !mcpErr.HasCode(GTW_AUTH_MISSING) {
		t.Error("Error should match the given code")
	}
}

func testCreateErrorWithDetails(t *testing.T) {
	t.Parallel()

	details := "Token not provided in Authorization header"
	err := Error(GTW_AUTH_MISSING, details)

	var mcpErr *MCPError

	_ = errors.As(err, &mcpErr)

	if mcpErr.Details != details {
		t.Errorf("Expected details %s, got %v", details, mcpErr.Details)
	}

	expectedMsg := "[GTW_AUTH_001] Missing authentication credentials: " + details
	if mcpErr.Error() != expectedMsg {
		t.Errorf("Expected error message %s, got %s", expectedMsg, mcpErr.Error())
	}
}

func testWithDetailsMethod(t *testing.T) {
	t.Parallel()

	err := Error(GTW_AUTH_MISSING)

	var mcpErr *MCPError

	_ = errors.As(err, &mcpErr)

	newErr := mcpErr.WithDetails("Additional context")
	if newErr.Details != "Additional context" {
		t.Errorf("Expected details to be updated")
	}

	// Original error should not be modified
	if mcpErr.Details != nil {
		t.Error("Original error should not be modified")
	}
}

func testUnknownErrorCode(t *testing.T) {
	t.Parallel()

	unknownCode := ErrorCode("UNKNOWN_CODE")
	err := Error(unknownCode)

	var mcpErr *MCPError

	_ = errors.As(err, &mcpErr)

	if mcpErr.Code != CMN_INT_UNKNOWN {
		t.Errorf("Expected fallback to %s, got %s", CMN_INT_UNKNOWN, mcpErr.Code)
	}
}

func TestIsErrorCode(t *testing.T) {
	t.Parallel()
	t.Run("Match MCP error", func(t *testing.T) {
		t.Parallel()

		err := Error(GTW_AUTH_MISSING)

		if !IsErrorCode(err, GTW_AUTH_MISSING) {
			t.Error("Should match the error code")
		}

		if IsErrorCode(err, GTW_AUTH_INVALID) {
			t.Error("Should not match different error code")
		}
	})

	t.Run("Non-MCP error", func(t *testing.T) {
		t.Parallel()

		err := http.ErrServerClosed

		if IsErrorCode(err, GTW_AUTH_MISSING) {
			t.Error("Should not match non-MCP error")
		}
	})
}

func TestIsRetryable(t *testing.T) {
	t.Parallel()
	t.Run("Retryable error", func(t *testing.T) {
		t.Parallel()

		err := Error(GTW_CONN_TIMEOUT)

		if !IsRetryable(err) {
			t.Error("Connection timeout should be retryable")
		}
	})

	t.Run("Non-retryable error", func(t *testing.T) {
		t.Parallel()

		err := Error(GTW_AUTH_MISSING)

		if IsRetryable(err) {
			t.Error("Auth missing should not be retryable")
		}
	})

	t.Run("Non-MCP error", func(t *testing.T) {
		t.Parallel()

		err := http.ErrServerClosed

		if IsRetryable(err) {
			t.Error("Non-MCP error should not be retryable by default")
		}
	})
}

func TestErrorCategories(t *testing.T) {
	t.Parallel()

	// Test that all error codes follow naming convention
	testCases := []struct {
		category string
		codes    []ErrorCode
	}{
		{"Common", []ErrorCode{CMN_INT_UNKNOWN, CMN_VAL_INVALID_REQ, CMN_PROTO_PARSE_ERR}},
		{"Gateway Auth", []ErrorCode{GTW_AUTH_MISSING, GTW_AUTH_INVALID, GTW_AUTH_EXPIRED}},
		{"Gateway Conn", []ErrorCode{GTW_CONN_REFUSED, GTW_CONN_TIMEOUT, GTW_CONN_CLOSED}},
		{"Gateway Rate", []ErrorCode{GTW_RATE_LIMIT_REQ, GTW_RATE_LIMIT_CONN, GTW_RATE_LIMIT_BURST}},
		{"Gateway Sec", []ErrorCode{GTW_SEC_BLOCKED_IP, GTW_SEC_BLOCKED_UA, GTW_SEC_MALICIOUS}},
		{"Router Conn", []ErrorCode{RTR_CONN_NO_BACKEND, RTR_CONN_BACKEND_ERR, RTR_CONN_POOL_FULL}},
		{"Router Route", []ErrorCode{RTR_ROUTE_NO_MATCH, RTR_ROUTE_CONFLICT, RTR_ROUTE_DISABLED}},
		{"Router Proto", []ErrorCode{RTR_PROTO_MISMATCH, RTR_PROTO_TRANSFORM, RTR_PROTO_SIZE_LIMIT}},
	}

	for _, tc := range testCases {
		t.Run(tc.category, func(t *testing.T) {
			t.Parallel()

			for _, code := range tc.codes {
				info, exists := GetErrorInfo(code)
				if !exists {
					t.Errorf("Error code %s not found in registry", code)
				}

				if info.Message == "" {
					t.Errorf("Error code %s has empty message", code)
				}

				if info.HTTPStatus == 0 {
					t.Errorf("Error code %s has no HTTP status", code)
				}
			}
		})
	}
}

func TestRetryAfterValues(t *testing.T) {
	t.Parallel()

	retryableCodes := []ErrorCode{
		CMN_INT_TIMEOUT,
		GTW_CONN_REFUSED,
		GTW_CONN_TIMEOUT,
		GTW_CONN_CLOSED,
		GTW_CONN_LIMIT,
		GTW_RATE_LIMIT_REQ,
		GTW_RATE_LIMIT_CONN,
		GTW_RATE_LIMIT_BURST,
		GTW_RATE_LIMIT_QUOTA,
		RTR_CONN_NO_BACKEND,
		RTR_CONN_BACKEND_ERR,
		RTR_CONN_POOL_FULL,
		RTR_CONN_UNHEALTHY,
		RTR_CONN_CIRCUIT_OPEN,
	}

	for _, code := range retryableCodes {
		t.Run(string(code), func(t *testing.T) {
			t.Parallel()

			info, exists := GetErrorInfo(code)

			if !exists {
				t.Fatalf("Error code %s not found", code)
			}

			if !info.Recoverable {
				t.Errorf("Error code %s should be recoverable", code)
			}

			if info.RetryAfter == 0 {
				t.Errorf("Recoverable error %s should have RetryAfter value", code)
			}

			if info.RetryAfter < 0 {
				t.Errorf("RetryAfter should not be negative for %s", code)
			}
		})
	}
}

func BenchmarkErrorCreation(b *testing.B) {
	b.Run("Create error", func(b *testing.B) {
		for range b.N {
			_ = Error(GTW_AUTH_MISSING)
		}
	})

	b.Run("Create error with details", func(b *testing.B) {
		for range b.N {
			_ = Error(GTW_AUTH_MISSING, "Token missing")
		}
	})

	b.Run("Check error code", func(b *testing.B) {
		err := Error(GTW_AUTH_MISSING)

		b.ResetTimer()

		for range b.N {
			_ = IsErrorCode(err, GTW_AUTH_MISSING)
		}
	})
}
