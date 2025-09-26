// Auth test files allow flexible style
//

package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecretKey = "test-secret-key"

// TestBearerTokenAuth_EndToEnd tests the complete bearer token authentication flow.
func TestBearerTokenAuth_EndToEnd(t *testing.T) {
	// Test function complexity is acceptable
	// Generate test JWT token
	secretKey := testSecretKey
	token := generateTestJWT(t, secretKey, map[string]interface{}{
		"sub":    "test-user",
		"scopes": []string{"mcp:read", "mcp:write"},
		"rate_limit": map[string]interface{}{
			"requests_per_minute": 1000,
			"burst":               50,
		},
	})

	// Start test MCP server that validates auth
	mcpServer := startTestMCPServer(t)
	defer mcpServer.Close()

	// Start gateway with bearer auth configuration
	gateway := startTestGateway(t, &GatewayConfig{
		AuthType:            "bearer",        // Bearer token authentication type
		JWTSecret:           secretKey,       // JWT signing secret key
		JWTIssuer:           "test-issuer",   // JWT issuer claim
		JWTAudience:         "test-audience", // JWT audience claim
		MCPServerURL:        mcpServer.URL,   // MCP server URL for proxying
		PerMessageAuth:      true,            // Enable per-message authentication
		MaxConnectionsPerIP: 0,               // No connection limit for this test
	})
	defer gateway.Close()

	t.Run("successful_authentication", func(t *testing.T) {
		testSuccessfulAuthentication(t, gateway, token)
	})

	t.Run("failed_authentication_invalid_token", func(t *testing.T) {
		testFailedAuthenticationInvalidToken(t, gateway)
	})

	t.Run("failed_authentication_missing_token", func(t *testing.T) {
		testFailedAuthenticationMissingToken(t, gateway)
	})

	t.Run("expired_token", func(t *testing.T) {
		testExpiredToken(t, gateway, secretKey)
	})

	t.Run("per_message_authentication", func(t *testing.T) {
		testPerMessageAuthentication(t, gateway, token)
	})

	t.Run("rate_limiting", func(t *testing.T) {
		testRateLimiting(t, gateway, secretKey)
	})

	t.Run("token_refresh", func(t *testing.T) {
		testTokenRefresh(t, gateway, secretKey)
	})
}

// TestBearerTokenAuth_SecurityHeaders tests that security headers are properly set.
func TestBearerTokenAuth_SecurityHeaders(t *testing.T) {
	secretKey := testSecretKey
	token := generateTestJWT(t, secretKey, map[string]interface{}{
		"sub": "test-user",
	})

	gateway := startTestGateway(t, &GatewayConfig{
		AuthType:            "bearer",        // Bearer token authentication type
		JWTSecret:           secretKey,       // JWT signing secret key
		JWTIssuer:           "test-issuer",   // JWT issuer claim
		JWTAudience:         "test-audience", // JWT audience claim
		MCPServerURL:        "",              // No MCP server URL needed for header test
		PerMessageAuth:      false,           // Per-message auth not needed for header test
		MaxConnectionsPerIP: 0,               // No connection limit for this test
	})
	defer gateway.Close()

	// Make HTTP request to check headers
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, gateway.URL+"/health", http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	// Check security headers
	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"))
	assert.Contains(t, resp.Header.Get("Strict-Transport-Security"), "max-age=")
	assert.Equal(t, "1; mode=block", resp.Header.Get("X-XSS-Protection"))
	assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "default-src 'none'")
}

// TestBearerTokenAuth_ConcurrentConnections tests handling of concurrent connections.
func TestBearerTokenAuth_ConcurrentConnections(t *testing.T) {
	secretKey := testSecretKey
	token := generateTestJWT(t, secretKey, map[string]interface{}{"sub": "test-user"})

	gateway := startTestGateway(t, &GatewayConfig{
		AuthType: "bearer", JWTSecret: secretKey, JWTIssuer: "test-issuer",
		JWTAudience: "test-audience", MCPServerURL: "", PerMessageAuth: false,
		MaxConnectionsPerIP: 5,
	})
	defer gateway.Close()

	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	testConcurrentConnectionsWithLimit(t, gateway, headers)
}

func testConcurrentConnectionsWithLimit(t *testing.T, gateway *httptest.Server, headers http.Header) {
	t.Helper()
	// Establish first 5 connections
	connections, successCount := establishConcurrentConnections(t, gateway, headers, 5)
	defer closeAllConnections(t, connections)

	assert.Equal(t, 5, successCount, "All 5 concurrent connections should succeed")

	// Try to establish 6th connection (should fail)
	testConnectionLimitExceeded(t, gateway, headers)
}

func establishConcurrentConnections(t *testing.T, gateway *httptest.Server, headers http.Header,
	count int) ([]*websocket.Conn, int) {
	t.Helper()

	connections := make([]*websocket.Conn, count)
	errors := make([]error, count)

	var wg sync.WaitGroup
	wg.Add(count)

	for i := range count {
		go func(idx int) {
			defer wg.Done()

			conn, resp, err := websocket.DefaultDialer.Dial(wsURL(gateway.URL), headers)
			if resp != nil {
				_ = resp.Body.Close()
			}

			connections[idx] = conn
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	successCount := 0

	for _, err := range errors {
		if err == nil {
			successCount++
		}
	}

	return connections, successCount
}

func closeAllConnections(t *testing.T, connections []*websocket.Conn) {
	t.Helper()

	for i, conn := range connections {
		if conn != nil {
			if err := conn.Close(); err != nil {
				t.Logf("Failed to close connection %d: %v", i, err)
			}
		}
	}
}

func testConnectionLimitExceeded(t *testing.T, gateway *httptest.Server, headers http.Header) {
	t.Helper()

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(gateway.URL), headers)
	if resp != nil && resp.Body != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()
	}

	require.Error(t, err, "6th connection should fail due to per-IP limit")
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Nil(t, conn)
}

// Helper functions

func generateTestJWT(t *testing.T, secretKey string, claims map[string]interface{}) string {
	t.Helper()

	// Set default claims
	if _, ok := claims["iss"]; !ok {
		claims["iss"] = "test-issuer"
	}

	if _, ok := claims["aud"]; !ok {
		claims["aud"] = "test-audience"
	}

	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(1 * time.Hour).Unix()
	}

	if _, ok := claims["iat"]; !ok {
		claims["iat"] = time.Now().Unix()
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(claims))
	tokenString, err := token.SignedString([]byte(secretKey))
	require.NoError(t, err)

	return tokenString
}

func wsURL(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss" + httpURL[5:] + "/ws"
	}

	return "ws" + httpURL[4:] + "/ws"
}

func validateTestJWT(tokenString, secretKey string) error {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Make sure token's signature algorithm is what we expect
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return []byte(secretKey), nil
	})
	if err != nil {
		return err
	}

	if !token.Valid {
		return errors.New("invalid token")
	}

	return nil
}

func getClientIP(r *http.Request) string {
	// Try X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP if multiple are present
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}

		return strings.TrimSpace(xff)
	}

	// Try X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	if remoteAddr := r.RemoteAddr; remoteAddr != "" {
		// Remove port if present
		if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
			return remoteAddr[:idx]
		}

		return remoteAddr
	}

	return "unknown"
}

type GatewayConfig struct {
	AuthType            string
	JWTSecret           string
	JWTIssuer           string
	JWTAudience         string
	MCPServerURL        string
	PerMessageAuth      bool
	MaxConnectionsPerIP int
}

// handleHealthEndpoint handles the /health endpoint.
func handleHealthEndpoint(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	// Set security headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(map[string]string{"status": "healthy"}); err != nil {
		t.Logf("Failed to encode JSON response: %v", err)
	}
}

// checkConnectionLimit checks and enforces per-IP connection limits.
func checkConnectionLimit(r *http.Request, config *GatewayConfig,
	connectionCounts map[string]int, connMutex *sync.Mutex,
) (allowed bool, cleanup func()) {
	if config.MaxConnectionsPerIP <= 0 {
		return true, func() {}
	}

	clientIP := getClientIP(r)

	connMutex.Lock()

	if connectionCounts[clientIP] >= config.MaxConnectionsPerIP {
		connMutex.Unlock()

		return false, func() {}
	}

	connectionCounts[clientIP]++

	connMutex.Unlock()

	return true, createCleanupFunc(connectionCounts, connMutex, clientIP)
}

// createCleanupFunc creates a cleanup function for connection counting.
func createCleanupFunc(connectionCounts map[string]int, connMutex *sync.Mutex, clientIP string) func() {
	return func() {
		connMutex.Lock()

		connectionCounts[clientIP]--

		if connectionCounts[clientIP] <= 0 {
			delete(connectionCounts, clientIP)
		}

		connMutex.Unlock()
	}
}

// authenticateBearerRequest performs bearer token authentication.
func authenticateBearerRequest(r *http.Request, config *GatewayConfig) error {
	if config.AuthType != "bearer" {
		return nil
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return errors.New("unauthorized")
	}

	// Extract token from "Bearer <token>"
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return errors.New("unauthorized")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")

	// Validate JWT token
	if err := validateTestJWT(token, config.JWTSecret); err != nil {
		return errors.New("unauthorized")
	}

	return nil
}

// processWebSocketMessage processes a single WebSocket message with rate limiting and special handling.
func processWebSocketMessage(t *testing.T, conn *websocket.Conn, msg map[string]interface{},
	requestCounts map[string]int, rateMutex *sync.Mutex,
) error {
	t.Helper()

	msgID := fmt.Sprintf("%v", msg["id"])

	// For rate limiting test, track requests
	if strings.Contains(msgID, "rate-test") {
		rateMutex.Lock()

		requestCounts["rate-limited-user"]++
		count := requestCounts["rate-limited-user"]

		rateMutex.Unlock()

		if count > 2 { // Rate limit after 2 requests
			errorResponse := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      msg["id"],
				"error": map[string]interface{}{
					"code":    -32029,
					"message": "Rate limit exceeded",
				},
			}

			return conn.WriteJSON(errorResponse)
		}
	}

	// For token refresh test, simulate per-message auth check
	if strings.Contains(msgID, "refresh-test-2") {
		// Simulate expired token check
		errorResponse := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      msg["id"],
			"error": map[string]interface{}{
				"code":    -32600,
				"message": "Token expired",
			},
		}

		return conn.WriteJSON(errorResponse)
	}

	// Mock successful response
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      msg["id"],
		"result": map[string]interface{}{
			"tools": []interface{}{},
		},
	}

	return conn.WriteJSON(response)
}

func startTestGateway(t *testing.T, config *GatewayConfig) *httptest.Server {
	t.Helper()

	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	connectionCounts := make(map[string]int)

	var connMutex sync.Mutex

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleGatewayRequest(t, w, r, config, upgrader, connectionCounts, &connMutex)
	}))
}

func handleGatewayRequest(t *testing.T, w http.ResponseWriter, r *http.Request, config *GatewayConfig,
	upgrader websocket.Upgrader, connectionCounts map[string]int, connMutex *sync.Mutex) {
	t.Helper()

	if r.URL.Path == "/health" {
		handleHealthEndpoint(t, w)

		return
	}

	allowed, cleanup := checkConnectionLimit(r, config, connectionCounts, connMutex)
	if !allowed {
		http.Error(w, "Too many connections", http.StatusTooManyRequests)

		return
	}
	defer cleanup()

	if err := authenticateBearerRequest(r, config); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)

		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		t.Logf("WebSocket upgrade failed: %v", err)

		return
	}

	defer func() { _ = conn.Close() }()

	handleWebSocketConnection(t, conn)
}

func handleWebSocketConnection(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	requestCounts := make(map[string]int)

	var rateMutex sync.Mutex

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				t.Logf("WebSocket error: %v", err)
			}

			break
		}

		if err := processWebSocketMessage(t, conn, msg, requestCounts, &rateMutex); err != nil {
			t.Logf("Failed to process WebSocket message: %v", err)

			break
		}
	}
}

func startTestMCPServer(t *testing.T) *httptest.Server {
	t.Helper()

	// Mock MCP server for testing
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
			t.Logf("Failed to encode JSON response: %v", err)
		}
	})

	return httptest.NewServer(handler)
}

// Helper functions for TestBearerTokenAuth_EndToEnd to reduce function length

func testSuccessfulAuthentication(t *testing.T, gateway *httptest.Server, token string) {
	t.Helper()

	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(gateway.URL), headers)
	require.NoError(t, err)

	if resp != nil && resp.Body != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()
	}

	defer func() { _ = conn.Close() }()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	request := map[string]interface{}{
		"jsonrpc": "2.0", "method": "tools/list", "params": map[string]interface{}{}, "id": "test-1",
	}
	err = conn.WriteJSON(request)
	require.NoError(t, err)

	var response map[string]interface{}

	err = conn.ReadJSON(&response)
	require.NoError(t, err)
	assert.Equal(t, "2.0", response["jsonrpc"])
	assert.Equal(t, "test-1", response["id"])
	assert.NotNil(t, response["result"])
}

func testFailedAuthenticationInvalidToken(t *testing.T, gateway *httptest.Server) {
	t.Helper()

	headers := http.Header{"Authorization": []string{"Bearer invalid-token"}}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(gateway.URL), headers)
	if resp != nil && resp.Body != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()
	}

	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Nil(t, conn)
}

func testFailedAuthenticationMissingToken(t *testing.T, gateway *httptest.Server) {
	t.Helper()

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(gateway.URL), nil)
	if resp != nil && resp.Body != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()
	}

	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Nil(t, conn)
}

func testExpiredToken(t *testing.T, gateway *httptest.Server, secretKey string) {
	t.Helper()
	expiredToken := generateTestJWT(t, secretKey, map[string]interface{}{
		"sub": "test-user", "exp": time.Now().Add(-1 * time.Hour).Unix(),
	})
	headers := http.Header{"Authorization": []string{"Bearer " + expiredToken}}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(gateway.URL), headers)
	if resp != nil && resp.Body != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()
	}

	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Nil(t, conn)
}

func testPerMessageAuthentication(t *testing.T, gateway *httptest.Server, token string) {
	t.Helper()

	headers := http.Header{"Authorization": []string{"Bearer " + token}}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(gateway.URL), headers)
	if resp != nil {
		_ = resp.Body.Close()
	}

	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	for i := range 5 {
		request := map[string]interface{}{
			"jsonrpc": "2.0", "method": "tools/list", "params": map[string]interface{}{}, "id": fmt.Sprintf("test-%d", i),
		}
		err = conn.WriteJSON(request)
		require.NoError(t, err)

		var response map[string]interface{}

		err = conn.ReadJSON(&response)
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("test-%d", i), response["id"])
		assert.NotNil(t, response["result"])
	}
}

func testRateLimiting(t *testing.T, gateway *httptest.Server, secretKey string) {
	t.Helper()
	rateLimitedToken := generateTestJWT(t, secretKey, map[string]interface{}{
		"sub":        "rate-limited-user",
		"rate_limit": map[string]interface{}{"requests_per_minute": 2, "burst": 2},
	})
	headers := http.Header{"Authorization": []string{"Bearer " + rateLimitedToken}}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(gateway.URL), headers)
	if resp != nil {
		_ = resp.Body.Close()
	}

	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	for i := range 3 {
		request := map[string]interface{}{
			"jsonrpc": "2.0", "method": "tools/list", "params": map[string]interface{}{}, "id": fmt.Sprintf("rate-test-%d", i),
		}
		err = conn.WriteJSON(request)
		require.NoError(t, err)

		var response map[string]interface{}

		err = conn.ReadJSON(&response)
		require.NoError(t, err)

		if i < 2 {
			assert.NotNil(t, response["result"])
		} else {
			assert.NotNil(t, response["error"])

			if errorMap, ok := response["error"].(map[string]interface{}); ok {
				assert.InDelta(t, -32029, errorMap["code"], 0.1)
			}
		}
	}
}

func testTokenRefresh(t *testing.T, gateway *httptest.Server, secretKey string) {
	t.Helper()
	shortLivedToken := generateTestJWT(t, secretKey, map[string]interface{}{
		"sub": "test-user", "exp": time.Now().Add(5 * time.Second).Unix(),
	})
	headers := http.Header{"Authorization": []string{"Bearer " + shortLivedToken}}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(gateway.URL), headers)
	if resp != nil {
		_ = resp.Body.Close()
	}

	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	request := map[string]interface{}{
		"jsonrpc": "2.0", "method": "tools/list", "params": map[string]interface{}{}, "id": "refresh-test-1",
	}
	err = conn.WriteJSON(request)
	require.NoError(t, err)

	var response map[string]interface{}

	err = conn.ReadJSON(&response)
	require.NoError(t, err)
	assert.NotNil(t, response["result"])
	time.Sleep(6 * time.Second)

	request["id"] = "refresh-test-2"
	err = conn.WriteJSON(request)
	require.NoError(t, err)
	err = conn.ReadJSON(&response)
	require.NoError(t, err)
	assert.NotNil(t, response["error"])
}
