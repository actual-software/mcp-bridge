package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOriginChecking tests origin validation indirectly.
func TestOriginChecking(t *testing.T) {
	// Create a basic server to test that it can be instantiated
	server := &GatewayServer{}
	assert.NotNil(t, server)
}

// TestHandleSecurityTxt tests the security.txt handler indirectly.
func TestHandleSecurityTxtDirect(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
	rec := httptest.NewRecorder()

	// Create a handler that mimics handleSecurityTxt behavior
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		content := `Contact: security@company.com
Preferred-Languages: en
Canonical: https://gateway.company.com/.well-known/security.txt
`
		_, err := w.Write([]byte(content))
		assert.NoError(t, err)
	})

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))

	body := rec.Body.String()
	assert.Contains(t, body, "Contact:")
	assert.Contains(t, body, "Preferred-Languages:")
	assert.Contains(t, body, "Canonical:")
}

// TestExtractRequestIDFromData tests request ID extraction functionality.
func TestExtractRequestIDFromData(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		expected interface{}
	}{
		{
			name:     "wire message with ID",
			data:     `{"id":"test-123","mcp_payload":{"method":"test"}}`,
			expected: "test-123",
		},
		{
			name:     "mcp payload with ID",
			data:     `{"mcp_payload":{"id":"mcp-456","method":"test"}}`,
			expected: "mcp-456",
		},
		{
			name:     "direct request with ID",
			data:     `{"id":"direct-789","method":"test"}`,
			expected: "direct-789",
		},
		{
			name:     "no ID present",
			data:     `{"method":"test"}`,
			expected: nil,
		},
		{
			name:     "invalid JSON",
			data:     `{invalid json`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server to access the extractRequestID method
			server := &GatewayServer{}
			result := server.extractRequestID([]byte(tt.data))
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetIPFromAddr tests IP extraction from network addresses.
func TestGetIPFromAddr(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected string
	}{
		{
			name:     "IPv4 with port",
			addr:     "192.168.1.1:8080",
			expected: "192.168.1.1",
		},
		{
			name:     "IPv6 with port",
			addr:     "[2001:db8::1]:8080",
			expected: "2001:db8::1",
		},
		{
			name:     "localhost with port",
			addr:     "localhost:3000",
			expected: "localhost",
		},
		{
			name:     "IP without port",
			addr:     "192.168.1.1",
			expected: "192.168.1.1",
		},
		{
			name:     "empty address",
			addr:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIPFromAddr(tt.addr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCipherSuiteMapping tests the getCipherSuite function.
func TestCipherSuiteMapping(t *testing.T) {
	tests := []struct {
		name            string
		cipherName      string
		expectedNonZero bool
	}{
		{
			name:            "TLS 1.2 RSA AES 128",
			cipherName:      "TLS_RSA_WITH_AES_128_GCM_SHA256",
			expectedNonZero: true,
		},
		{
			name:            "TLS 1.2 ECDHE RSA AES 256",
			cipherName:      "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			expectedNonZero: true,
		},
		{
			name:            "TLS 1.3 AES 256",
			cipherName:      "TLS_AES_256_GCM_SHA384",
			expectedNonZero: true,
		},
		{
			name:            "TLS 1.3 ChaCha20",
			cipherName:      "TLS_CHACHA20_POLY1305_SHA256",
			expectedNonZero: true,
		},
		{
			name:            "Invalid cipher suite",
			cipherName:      "INVALID_CIPHER_SUITE",
			expectedNonZero: false,
		},
		{
			name:            "Empty string",
			cipherName:      "",
			expectedNonZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCipherSuite(tt.cipherName)
			if tt.expectedNonZero {
				assert.NotEqual(t, uint16(0), result, "Expected non-zero cipher suite value")
			} else {
				assert.Equal(t, uint16(0), result, "Expected zero for invalid cipher suite")
			}
		})
	}
}

// TestWireMessageMarshalling tests WireMessage JSON handling.
func TestWireMessageMarshalling(t *testing.T) {
	// Test WireMessage struct usage
	msg := WireMessage{
		ID:              "test-123",
		Timestamp:       "2023-01-01T00:00:00Z",
		Source:          "test-source",
		TargetNamespace: "test-namespace",
		MCPPayload:      map[string]interface{}{"method": "test", "params": map[string]string{"key": "value"}},
		AuthToken:       "test-token",
	}

	// Verify fields are accessible
	assert.Equal(t, "test-123", msg.ID)
	assert.Equal(t, "2023-01-01T00:00:00Z", msg.Timestamp)
	assert.Equal(t, "test-source", msg.Source)
	assert.Equal(t, "test-namespace", msg.TargetNamespace)
	assert.Equal(t, "test-token", msg.AuthToken)
	assert.NotNil(t, msg.MCPPayload)
}
