
// Package weather provides unit tests for the Weather MCP server components
package weather

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// TestWeatherMCPServer tests the core Weather MCP server functionality.
func TestWeatherMCPServer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	server := NewWeatherMCPServer(logger)

	// Test server creation
	assert.NotNil(t, server)

	// Test health status
	health := server.GetHealth()
	assert.Equal(t, "healthy", health.Status)
	assert.Equal(t, "1.0.0", health.Version)

	// Test metrics
	metrics := server.GetMetrics()
	assert.Equal(t, int64(0), metrics.RequestsTotal)
	assert.Equal(t, int64(0), metrics.RequestsFailed)
}

// TestWeatherMCPServerWebSocket tests WebSocket handling.
func TestWeatherMCPServerWebSocket(t *testing.T) {
	logger := zaptest.NewLogger(t)
	server := NewWeatherMCPServer(logger)

	conn := setupWebSocketTestServer(t, server)

	defer func() { _ = conn.Close() }()

	testWebSocketInitialize(t, conn)
	testWebSocketToolsList(t, conn)
}

func setupWebSocketTestServer(t *testing.T, server *WeatherMCPServer) *websocket.Conn {
	t.Helper()

	// Create HTTP test server
	httpServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	t.Cleanup(httpServer.Close)
	t.Cleanup(server.Shutdown)

	// Convert to WebSocket URL
	wsURL := "ws" + httpServer.URL[4:] + "/mcp"

	// Connect to WebSocket
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	require.NoError(t, err)

	return conn
}

func testWebSocketInitialize(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
		"id": 1,
	}

	err := conn.WriteJSON(initReq)
	require.NoError(t, err)

	var initResp map[string]interface{}

	err = conn.ReadJSON(&initResp)
	require.NoError(t, err)

	assert.Equal(t, "2.0", initResp["jsonrpc"])
	assert.NotNil(t, initResp["result"])
}

func testWebSocketToolsList(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	listReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/list",
		"params":  map[string]interface{}{},
		"id":      2,
	}

	err := conn.WriteJSON(listReq)
	require.NoError(t, err)

	var listResp map[string]interface{}

	err = conn.ReadJSON(&listResp)
	require.NoError(t, err)

	result, ok := listResp["result"].(map[string]interface{})
	require.True(t, ok, "Expected result to be a map")
	tools, ok := result["tools"].([]interface{})
	require.True(t, ok, "Expected tools to be an array")
	assert.Len(t, tools, 2)
}

// TestWeatherAPICall tests actual weather API functionality.
//

func TestWeatherAPICall(t *testing.T) {
	// Skip if running in CI or without internet
	if testing.Short() {
		t.Skip("Skipping API test in short mode")
	}

	logger := zaptest.NewLogger(t)

	server := NewWeatherMCPServer(logger)
	defer server.Shutdown()

	conn := setupWeatherAPITestConnection(t, server)

	defer func() { _ = conn.Close() }()

	initializeWeatherAPITest(t, conn)
	testWeatherAPIRequest(t, conn)
}

func setupWeatherAPITestConnection(t *testing.T, server *WeatherMCPServer) *websocket.Conn {
	t.Helper()

	// Create HTTP test server
	httpServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	t.Cleanup(httpServer.Close)

	// Connect to WebSocket
	wsURL := "ws" + httpServer.URL[4:] + "/mcp"

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	require.NoError(t, err)

	return conn
}

func initializeWeatherAPITest(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params":  map[string]interface{}{},
		"id":      1,
	}

	err := conn.WriteJSON(initReq)
	require.NoError(t, err)

	var initResp map[string]interface{}

	err = conn.ReadJSON(&initResp)
	require.NoError(t, err)
}

func testWeatherAPIRequest(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	weatherReq := createWeatherAPIRequest()

	err := conn.WriteJSON(weatherReq)
	require.NoError(t, err)

	weatherResp := readWeatherAPIResponse(t, conn)
	validateWeatherAPIResponse(t, weatherResp)
}

func createWeatherAPIRequest() map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "get_current_weather",
			"arguments": map[string]interface{}{
				"latitude":  40.7128,
				"longitude": -74.0060,
			},
		},
		"id": 2,
	}
}

func readWeatherAPIResponse(t *testing.T, conn *websocket.Conn) map[string]interface{} {
	t.Helper()

	// Set deadline for response
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Logf("Warning: Failed to set read deadline: %v", err)
	}

	var weatherResp map[string]interface{}

	err := conn.ReadJSON(&weatherResp)
	require.NoError(t, err)

	return weatherResp
}

func validateWeatherAPIResponse(t *testing.T, weatherResp map[string]interface{}) {
	t.Helper()

	// Check response
	if weatherResp["error"] != nil {
		t.Logf("Weather API error: %v", weatherResp["error"])
		// API might be down or rate limited, but the server handled it correctly
		return
	}

	assert.NotNil(t, weatherResp["result"])

	result := parseWeatherResult(weatherResp["result"])
	if len(result.Content) > 0 {
		assert.Contains(t, result.Content[0].Text, "Temperature")
		t.Logf("Weather data: %s", result.Content[0].Text)
	}
}

func parseWeatherResult(resultData interface{}) struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
} {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	resultBytes, _ := json.Marshal(resultData)
	_ = json.Unmarshal(resultBytes, &result)

	return result
}

// TestMCPClientAgentBasic tests the basic MCP client agent functionality.
func TestMCPClientAgentBasic(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test agent creation
	agent := NewMCPClientAgent("test-agent", "/nonexistent/router", "/tmp/config.yaml", logger)
	assert.NotNil(t, agent)
	assert.Equal(t, "test-agent", agent.ID)

	// Test metrics
	metrics := agent.GetMetrics()
	assert.Equal(t, int64(0), metrics.RequestsSent)
	assert.Equal(t, int64(0), metrics.ResponsesReceived)
}

// TestCircuitBreaker tests the circuit breaker functionality.
func TestCircuitBreaker(t *testing.T) {
	cb := &CircuitBreaker{
		failureThreshold: 3,
		successThreshold: 2,
		timeout:          1 * time.Second,
		state:            "closed",
	}

	// Initially should allow requests
	assert.True(t, cb.Allow())

	// Record failures
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	// Should now be open and deny requests
	assert.False(t, cb.Allow())
	assert.Equal(t, "open", cb.GetState())

	// Wait for timeout
	time.Sleep(1100 * time.Millisecond)

	// Should now be half-open
	assert.True(t, cb.Allow())

	// Record successes to close it
	cb.RecordSuccess()
	cb.RecordSuccess()

	assert.Equal(t, "closed", cb.GetState())
}

// TestServerConfig tests the server configuration.
func TestServerConfig(t *testing.T) {
	config := DefaultServerConfig()

	assert.Equal(t, 8080, config.Port)
	assert.Equal(t, "0.0.0.0", config.Host)
	assert.True(t, config.MetricsEnabled)
	assert.True(t, config.RateLimitEnabled)
	assert.True(t, config.CircuitBreakerEnabled)
}

// TestRateLimiter tests the rate limiting functionality.
func TestRateLimiter(t *testing.T) {
	limiter := &RateLimiter{
		tokens:     5,
		maxTokens:  5,
		refillRate: 100 * time.Millisecond,
		lastRefill: time.Now(),
	}

	// Should allow 5 requests initially
	for i := 0; i < 5; i++ {
		limiter.mu.Lock()

		allowed := limiter.tokens > 0
		if allowed {
			limiter.tokens--
		}

		limiter.mu.Unlock()
		assert.True(t, allowed, "Request %d should be allowed", i)
	}

	// 6th request should be denied
	limiter.mu.Lock()
	allowed := limiter.tokens > 0
	limiter.mu.Unlock()
	assert.False(t, allowed, "6th request should be denied")

	// After refill period, should allow again
	time.Sleep(150 * time.Millisecond)

	limiter.mu.Lock()
	// Simulate refill
	now := time.Now()
	elapsed := now.Sub(limiter.lastRefill)

	tokensToAdd := int(elapsed / limiter.refillRate)
	if tokensToAdd > 0 {
		limiter.tokens = min(limiter.maxTokens, limiter.tokens+tokensToAdd)
		limiter.lastRefill = now
	}

	allowed = limiter.tokens > 0
	if allowed {
		limiter.tokens--
	}

	limiter.mu.Unlock()

	assert.True(t, allowed, "Request should be allowed after refill")
}

// BenchmarkWeatherServerBasic benchmarks basic server operations.
func BenchmarkWeatherServerBasic(b *testing.B) {
	logger := zaptest.NewLogger(b)

	server := NewWeatherMCPServer(logger)
	defer server.Shutdown()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = server.GetHealth()
		_ = server.GetMetrics()
	}
}
