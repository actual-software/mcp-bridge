package wire

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

func TestNewAuthRequest(t *testing.T) {
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-123",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "test.tool",
			"args": map[string]interface{}{"key": "value"},
		},
	}
	authToken := "bearer-token-12345"

	authMsg := NewAuthRequest(req, authToken)

	require.NotNil(t, authMsg)
	assert.Equal(t, authToken, authMsg.AuthToken)
	assert.Equal(t, req, authMsg.Message)
}

func TestNewAuthResponse(t *testing.T) {
	resp := &mcp.Response{
		JSONRPC: "2.0",
		ID:      "test-456",
		Result: map[string]interface{}{
			"status": "success",
			"data":   "test result",
		},
	}
	authToken := "bearer-token-67890"

	authMsg := NewAuthResponse(resp, authToken)

	require.NotNil(t, authMsg)
	assert.Equal(t, authToken, authMsg.AuthToken)
	assert.Equal(t, resp, authMsg.Message)
}

func TestNewAuthRequest_EmptyToken(t *testing.T) {
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-empty-token",
		Method:  "test/method",
	}

	authMsg := NewAuthRequest(req, "")

	require.NotNil(t, authMsg)
	assert.Empty(t, authMsg.AuthToken)
	assert.Equal(t, req, authMsg.Message)
}

func TestNewAuthResponse_EmptyToken(t *testing.T) {
	resp := &mcp.Response{
		JSONRPC: "2.0",
		ID:      "test-empty-token-resp",
		Error: &mcp.Error{
			Code:    -32600,
			Message: "Invalid Request",
		},
	}

	authMsg := NewAuthResponse(resp, "")

	require.NotNil(t, authMsg)
	assert.Empty(t, authMsg.AuthToken)
	assert.Equal(t, resp, authMsg.Message)
}

func TestUnmarshalAuthMessage_Valid(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
	}{
		{
			name: "Auth message with request",
			jsonData: `{
				"auth_token": "test-token-123",
				"message": {
					"jsonrpc": "2.0",
					"id": "req-1",
					"method": "tools/list"
				}
			}`,
		},
		{
			name: "Auth message with response",
			jsonData: `{
				"auth_token": "test-token-456",
				"message": {
					"jsonrpc": "2.0",
					"id": "resp-1",
					"result": {"status": "ok"}
				}
			}`,
		},
		{
			name: "Auth message without token",
			jsonData: `{
				"message": {
					"jsonrpc": "2.0",
					"id": "no-token",
					"method": "test"
				}
			}`,
		},
		{
			name: "Auth message with empty token",
			jsonData: `{
				"auth_token": "",
				"message": {
					"jsonrpc": "2.0",
					"id": "empty-token",
					"method": "test"
				}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authMsg, err := UnmarshalAuthMessage([]byte(tt.jsonData))
			require.NoError(t, err)
			require.NotNil(t, authMsg)
			assert.NotNil(t, authMsg.Message)
		})
	}
}

func TestUnmarshalAuthMessage_Invalid(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
	}{
		{
			name:     "Invalid JSON",
			jsonData: `{"auth_token": "test", "message": invalid json}`,
		},
		{
			name:     "Empty JSON",
			jsonData: ``,
		},
		{
			name:     "Null JSON",
			jsonData: `null`,
		},
		{
			name:     "Missing message field",
			jsonData: `{"auth_token": "test"}`,
		},
		{
			name:     "Malformed structure",
			jsonData: `"not an object"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authMsg, err := UnmarshalAuthMessage([]byte(tt.jsonData))
			if tt.name == "Missing message field" || tt.name == "Null JSON" {
				// These should not error in unmarshaling, but message will be nil
				require.NoError(t, err)
				assert.NotNil(t, authMsg)
				assert.Nil(t, authMsg.Message)
			} else {
				require.Error(t, err)
				assert.Nil(t, authMsg)
			}
		})
	}
}

func TestAuthMessage_ExtractRequest(t *testing.T) {
	testValidRequestExtraction(t)
	testRawMessageExtraction(t)
	testInvalidRequestExtraction(t)
	testNilMessageExtraction(t)
}

func testValidRequestExtraction(t *testing.T) {
	t.Helper()

	t.Run("Valid request extraction", func(t *testing.T) {
		originalReq := &mcp.Request{
			JSONRPC: "2.0",
			ID:      "extract-test-1",
			Method:  "tools/call",
			Params: map[string]interface{}{
				"name": "test.tool",
				"arguments": map[string]interface{}{
					"param1": "value1",
					"param2": 42,
				},
			},
		}

		authMsg := NewAuthRequest(originalReq, "extract-token")
		extractedReq, err := authMsg.ExtractRequest()

		require.NoError(t, err)
		require.NotNil(t, extractedReq)
		assert.Equal(t, originalReq.JSONRPC, extractedReq.JSONRPC)
		assert.Equal(t, originalReq.ID, extractedReq.ID)
		assert.Equal(t, originalReq.Method, extractedReq.Method)
		assert.NotNil(t, extractedReq.Params)
	})
}

func testRawMessageExtraction(t *testing.T) {
	t.Helper()

	t.Run("Extract request from raw message data", func(t *testing.T) {
		// Create auth message with raw JSON data
		rawMessage := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "extract-raw-1",
			"method":  "test/method",
			"params": map[string]interface{}{
				"key": "value",
			},
		}

		authMsg := &AuthMessage{
			AuthToken: "raw-token",
			Message:   rawMessage,
		}

		extractedReq, err := authMsg.ExtractRequest()
		require.NoError(t, err)
		require.NotNil(t, extractedReq)
		assert.Equal(t, "2.0", extractedReq.JSONRPC)
		assert.Equal(t, "extract-raw-1", extractedReq.ID)
		assert.Equal(t, "test/method", extractedReq.Method)
	})
}

func testInvalidRequestExtraction(t *testing.T) {
	t.Helper()

	t.Run("Invalid request extraction", func(t *testing.T) {
		// Create auth message with non-request data
		authMsg := &AuthMessage{
			AuthToken: "invalid-token",
			Message:   "not a request object",
		}

		extractedReq, err := authMsg.ExtractRequest()
		require.Error(t, err)
		assert.Nil(t, extractedReq)
	})
}

func testNilMessageExtraction(t *testing.T) {
	t.Helper()

	t.Run("Nil message extraction", func(t *testing.T) {
		authMsg := &AuthMessage{
			AuthToken: "nil-token",
			Message:   nil,
		}

		extractedReq, err := authMsg.ExtractRequest()
		// json.Marshal(nil) produces "null", which can be unmarshaled successfully
		require.NoError(t, err)
		assert.NotNil(t, extractedReq)
		assert.Empty(t, extractedReq.JSONRPC)
		assert.Empty(t, extractedReq.Method)
	})
}

func TestAuthMessage_ExtractResponse(t *testing.T) {
	testValidResponseExtraction(t)
	testResponseWithError(t)
	testRawResponseExtraction(t)
	testInvalidResponseExtraction(t)
	testNilResponseExtraction(t)
}

func testValidResponseExtraction(t *testing.T) {
	t.Helper()

	t.Run("Valid response extraction", func(t *testing.T) {
		originalResp := &mcp.Response{
			JSONRPC: "2.0",
			ID:      "extract-resp-1",
			Result: map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"result": "test data",
					"count":  5,
				},
			},
		}

		authMsg := NewAuthResponse(originalResp, "extract-resp-token")
		extractedResp, err := authMsg.ExtractResponse()

		require.NoError(t, err)
		require.NotNil(t, extractedResp)
		assert.Equal(t, originalResp.JSONRPC, extractedResp.JSONRPC)
		assert.Equal(t, originalResp.ID, extractedResp.ID)
		assert.NotNil(t, extractedResp.Result)
	})
}

func testResponseWithError(t *testing.T) {
	t.Helper()

	t.Run("Response with error", func(t *testing.T) {
		originalResp := &mcp.Response{
			JSONRPC: "2.0",
			ID:      "extract-error-resp",
			Error: &mcp.Error{
				Code:    -32603,
				Message: "Internal error",
				Data:    "Additional error data",
			},
		}

		authMsg := NewAuthResponse(originalResp, "error-token")
		extractedResp, err := authMsg.ExtractResponse()

		require.NoError(t, err)
		require.NotNil(t, extractedResp)
		assert.Equal(t, originalResp.JSONRPC, extractedResp.JSONRPC)
		assert.Equal(t, originalResp.ID, extractedResp.ID)
		assert.NotNil(t, extractedResp.Error)
		assert.Equal(t, originalResp.Error.Code, extractedResp.Error.Code)
		assert.Equal(t, originalResp.Error.Message, extractedResp.Error.Message)
	})
}

func testRawResponseExtraction(t *testing.T) {
	t.Helper()

	t.Run("Extract response from raw message data", func(t *testing.T) {
		rawMessage := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "extract-raw-resp",
			"result": map[string]interface{}{
				"value": "test result",
			},
		}

		authMsg := &AuthMessage{
			AuthToken: "raw-resp-token",
			Message:   rawMessage,
		}

		extractedResp, err := authMsg.ExtractResponse()
		require.NoError(t, err)
		require.NotNil(t, extractedResp)
		assert.Equal(t, "2.0", extractedResp.JSONRPC)
		assert.Equal(t, "extract-raw-resp", extractedResp.ID)
		assert.NotNil(t, extractedResp.Result)
	})
}

func testInvalidResponseExtraction(t *testing.T) {
	t.Helper()

	t.Run("Invalid response extraction", func(t *testing.T) {
		authMsg := &AuthMessage{
			AuthToken: "invalid-resp-token",
			Message:   "not a response object",
		}

		extractedResp, err := authMsg.ExtractResponse()
		require.Error(t, err)
		assert.Nil(t, extractedResp)
	})
}

func testNilResponseExtraction(t *testing.T) {
	t.Helper()

	t.Run("Nil message extraction", func(t *testing.T) {
		authMsg := &AuthMessage{
			AuthToken: "nil-resp-token",
			Message:   nil,
		}

		extractedResp, err := authMsg.ExtractResponse()
		// json.Marshal(nil) produces "null", which can be unmarshaled successfully
		require.NoError(t, err)
		assert.NotNil(t, extractedResp)
		assert.Empty(t, extractedResp.JSONRPC)
	})
}

func TestAuthMessage_RoundTrip(t *testing.T) {
	testRequestRoundTrip(t)
	testResponseRoundTrip(t)
}

func testRequestRoundTrip(t *testing.T) {
	t.Helper()

	t.Run("Request round trip", func(t *testing.T) {
		originalReq := &mcp.Request{
			JSONRPC: "2.0",
			ID:      "roundtrip-req",
			Method:  "tools/call",
			Params: map[string]interface{}{
				"name": "test.tool",
				"args": map[string]interface{}{
					"input":  "test input",
					"number": 123,
					"flag":   true,
				},
			},
		}
		authToken := "roundtrip-token"

		// Create auth message
		authMsg := NewAuthRequest(originalReq, authToken)

		// Marshal to JSON
		jsonData, err := json.Marshal(authMsg)
		require.NoError(t, err)

		// Unmarshal from JSON
		unmarshaled, err := UnmarshalAuthMessage(jsonData)
		require.NoError(t, err)

		// Extract request
		extractedReq, err := unmarshaled.ExtractRequest()
		require.NoError(t, err)

		// Verify all data is preserved
		assert.Equal(t, authToken, unmarshaled.AuthToken)
		assert.Equal(t, originalReq.JSONRPC, extractedReq.JSONRPC)
		assert.Equal(t, originalReq.ID, extractedReq.ID)
		assert.Equal(t, originalReq.Method, extractedReq.Method)
		assert.NotNil(t, extractedReq.Params)
	})
}

func testResponseRoundTrip(t *testing.T) {
	t.Helper()

	t.Run("Response round trip", func(t *testing.T) {
		originalResp := &mcp.Response{
			JSONRPC: "2.0",
			ID:      "roundtrip-resp",
			Result: map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{
						"name":        "test.tool",
						"description": "A test tool",
					},
				},
				"count": 1,
			},
		}
		// #nosec G101 - this is a test token for unit testing, not a real credential
		authToken := "roundtrip-resp-token"

		// Create auth message
		authMsg := NewAuthResponse(originalResp, authToken)

		// Marshal to JSON
		jsonData, err := json.Marshal(authMsg)
		require.NoError(t, err)

		// Unmarshal from JSON
		unmarshaled, err := UnmarshalAuthMessage(jsonData)
		require.NoError(t, err)

		// Extract response
		extractedResp, err := unmarshaled.ExtractResponse()
		require.NoError(t, err)

		// Verify all data is preserved
		assert.Equal(t, authToken, unmarshaled.AuthToken)
		assert.Equal(t, originalResp.JSONRPC, extractedResp.JSONRPC)
		assert.Equal(t, originalResp.ID, extractedResp.ID)
		assert.NotNil(t, extractedResp.Result)
	})
}

func TestAuthMessage_MarshalUnmarshal(t *testing.T) {
	tests := createMarshalUnmarshalTests()
	runMarshalUnmarshalTests(t, tests)
}

func createMarshalUnmarshalTests() []struct {
	name      string
	authMsg   *AuthMessage
	wantError bool
} {
	basicTests := createBasicMarshalTests()
	complexTests := createComplexMarshalTests()

	tests := make([]struct {
		name      string
		authMsg   *AuthMessage
		wantError bool
	}, 0, len(basicTests)+len(complexTests))

	tests = append(tests, basicTests...)
	tests = append(tests, complexTests...)

	return tests
}

func createBasicMarshalTests() []struct {
	name      string
	authMsg   *AuthMessage
	wantError bool
} {
	return []struct {
		name      string
		authMsg   *AuthMessage
		wantError bool
	}{
		{
			name: "Auth message with request",
			authMsg: &AuthMessage{
				AuthToken: "marshal-test-1",
				Message: &mcp.Request{
					JSONRPC: "2.0",
					ID:      "marshal-req",
					Method:  "test/method",
				},
			},
			wantError: false,
		},
		{
			name: "Auth message with response",
			authMsg: &AuthMessage{
				AuthToken: "marshal-test-2",
				Message: &mcp.Response{
					JSONRPC: "2.0",
					ID:      "marshal-resp",
					Result:  "success",
				},
			},
			wantError: false,
		},
		{
			name: "Auth message with nil message",
			authMsg: &AuthMessage{
				AuthToken: "marshal-test-4",
				Message:   nil,
			},
			wantError: false,
		},
	}
}

func createComplexMarshalTests() []struct {
	name      string
	authMsg   *AuthMessage
	wantError bool
} {
	return []struct {
		name      string
		authMsg   *AuthMessage
		wantError bool
	}{
		{
			name: "Auth message with complex data",
			authMsg: &AuthMessage{
				AuthToken: "marshal-test-3",
				Message: map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      "complex",
					"result": map[string]interface{}{
						"tools": []interface{}{
							map[string]interface{}{
								"name": "tool1",
								"parameters": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"input": map[string]interface{}{
											"type": "string",
										},
									},
								},
							},
						},
					},
				},
			},
			wantError: false,
		},
	}
}

func runMarshalUnmarshalTests(t *testing.T, tests []struct {
	name      string
	authMsg   *AuthMessage
	wantError bool
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			jsonData, err := json.Marshal(tt.authMsg)
			if tt.wantError {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, jsonData)

			// Unmarshal
			var unmarshaled AuthMessage

			err = json.Unmarshal(jsonData, &unmarshaled)
			require.NoError(t, err)

			// Verify
			assert.Equal(t, tt.authMsg.AuthToken, unmarshaled.AuthToken)

			if tt.authMsg.Message == nil {
				assert.Nil(t, unmarshaled.Message)
			} else {
				assert.NotNil(t, unmarshaled.Message)
			}
		})
	}
}

// Benchmark tests for performance.
func BenchmarkNewAuthRequest(b *testing.B) {
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "bench-req",
		Method:  "tools/call",
		Params:  map[string]interface{}{"test": "data"},
	}
	token := "benchmark-token"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewAuthRequest(req, token)
	}
}

func BenchmarkNewAuthResponse(b *testing.B) {
	resp := &mcp.Response{
		JSONRPC: "2.0",
		ID:      "bench-resp",
		Result:  map[string]interface{}{"status": "ok"},
	}
	token := "benchmark-token"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewAuthResponse(resp, token)
	}
}

func BenchmarkUnmarshalAuthMessage(b *testing.B) {
	jsonData := []byte(`{
		"auth_token": "benchmark-token",
		"message": {
			"jsonrpc": "2.0",
			"id": "bench-unmarshal",
			"method": "tools/call",
			"params": {"name": "test.tool"}
		}
	}`)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = UnmarshalAuthMessage(jsonData)
	}
}

func BenchmarkExtractRequest(b *testing.B) {
	authMsg := NewAuthRequest(&mcp.Request{
		JSONRPC: "2.0",
		ID:      "bench-extract",
		Method:  "test/method",
		Params:  map[string]interface{}{"key": "value"},
	}, "bench-token")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = authMsg.ExtractRequest()
	}
}

func BenchmarkExtractResponse(b *testing.B) {
	authMsg := NewAuthResponse(&mcp.Response{
		JSONRPC: "2.0",
		ID:      "bench-extract-resp",
		Result:  map[string]interface{}{"status": "success"},
	}, "bench-token")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = authMsg.ExtractResponse()
	}
}
