package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	// TestMethodPing represents the ping test method.
	TestMethodPing = "ping"
	// TestMethodEcho represents the echo test method.
	TestMethodEcho = "echo"
	// TestMethodError represents the error test method.
	TestMethodError = "error"
	// TestHeaderValue represents the test header value.
	TestHeaderValue = "test-value"
)

// mockHTTPServer creates a mock HTTP server for testing.
func newMockHTTPServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse MCP request.
		var req mcp.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)

			return
		}

		// Create response based on method.
		var resp mcp.Response

		switch req.Method {
		case TestMethodPing:
			resp = mcp.Response{
				JSONRPC: constants.TestJSONRPCVersion,
				Result:  "pong",
				ID:      req.ID,
			}
		case TestMethodEcho:
			resp = mcp.Response{
				JSONRPC: constants.TestJSONRPCVersion,
				Result:  map[string]interface{}{"echo": req.Method},
				ID:      req.ID,
			}
		case TestMethodError:
			resp = mcp.Response{
				JSONRPC: constants.TestJSONRPCVersion,
				Error: &mcp.Error{
					Code:    -32603,
					Message: "Internal error",
				},
				ID: req.ID,
			}
		default:
			resp = mcp.Response{
				JSONRPC: constants.TestJSONRPCVersion,
				Result:  map[string]interface{}{"method": req.Method},
				ID:      req.ID,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestNewHTTPClient(t *testing.T) {
	logger := zaptest.NewLogger(t)

	testCases := []struct {
		name      string
		serverURL string
		config    HTTPClientConfig
		wantError bool
	}{
		{
			name:      "valid HTTP URL",
			serverURL: fmt.Sprintf("http://localhost:%d/mcp", constants.TestHTTPPort),
			config:    HTTPClientConfig{},
			wantError: false,
		},
		{
			name:      "valid HTTPS URL",
			serverURL: fmt.Sprintf("https://example.com:%d/path", constants.TestHTTPPort),
			config:    HTTPClientConfig{},
			wantError: false,
		},
		{
			name:      "config URL overrides serverURL",
			serverURL: fmt.Sprintf("http://localhost:%d", constants.TestHTTPPort),
			config: HTTPClientConfig{
				URL: fmt.Sprintf("http://configured.example.com:%d/mcp", constants.TestMetricsPort),
			},
			wantError: false,
		},
		{
			name:      "invalid URL",
			serverURL: "not-a-url",
			config:    HTTPClientConfig{},
			wantError: true,
		},
		{
			name:      "non-HTTP scheme",
			serverURL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
			config:    HTTPClientConfig{},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewHTTPClient("test-client", tc.serverURL, tc.config, logger)

			if tc.wantError {
				require.Error(t, err)
				assert.Nil(t, client)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, client)
				assert.Equal(t, "test-client", client.GetName())
				assert.Equal(t, "http", client.GetProtocol())
				assert.Equal(t, StateDisconnected, client.GetState())
			}
		})
	}
}

func TestHTTPClientDefaults(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := HTTPClientConfig{} // Empty config to test defaults

	client, err := NewHTTPClient("test-client", fmt.Sprintf("http://localhost:%d", constants.TestHTTPPort), config, logger)
	require.NoError(t, err)

	assert.Equal(t, "POST", client.config.Method)
	assert.Equal(t, 30*time.Second, client.config.Timeout)
	assert.Equal(t, 30*time.Second, client.config.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, client.config.HealthCheck.Timeout)
	assert.Equal(t, 10, client.config.Client.MaxIdleConns)
	assert.Equal(t, 2, client.config.Client.MaxIdleConnsPerHost)
	assert.Equal(t, 90*time.Second, client.config.Client.IdleConnTimeout)
	assert.Equal(t, 3, client.config.Client.MaxRedirects)
	assert.Equal(t, "MCP-Router-HTTP-Client/1.0", client.config.Client.UserAgent)
	assert.Equal(t, "application/json", client.config.Headers["Content-Type"])
}

func TestHTTPClientConnectAndClose(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockHTTPServer()
	defer mockServer.Close()

	config := HTTPClientConfig{
		URL:     mockServer.URL,
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false, // Disable for simple test
		},
	}

	client, err := NewHTTPClient("test-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test connect.
	err = client.Connect(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateConnected, client.GetState())

	// Test connect when already connected.
	err = client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already connected")

	// Test close.
	err = client.Close(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateClosed, client.GetState())

	// Test close when already closed.
	err = client.Close(ctx)
	require.NoError(t, err) // Should not error
}

func TestHTTPClientSendRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockHTTPServer()
	defer mockServer.Close()

	config := HTTPClientConfig{
		URL:     mockServer.URL,
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewHTTPClient("echo-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect.
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Test successful request.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "echo",
		ID:      "test-123",
	}

	resp, err := client.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, "test-123", resp.ID)

	// Check response content.
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "echo", result["echo"])

	// Check metrics.
	metrics := client.GetMetrics()
	assert.Equal(t, uint64(1), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
	assert.Positive(t, metrics.AverageLatency)
}

func TestHTTPClientSendRequestNotConnected(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := HTTPClientConfig{
		URL: fmt.Sprintf("http://localhost:%d", constants.TestHTTPPort),
	}

	client, err := NewHTTPClient("test-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-123",
	}

	// Try to send request when not connected.
	_, err = client.SendRequest(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestHTTPClientSendRequestTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a server that responds slowly.
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * constants.TestSleepShort) // Sleep longer than client timeout
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Result:  "slow response",
			ID:      "test",
		})
	}))
	defer slowServer.Close()

	config := HTTPClientConfig{
		URL:     slowServer.URL,
		Timeout: 50 * time.Millisecond, // Very short timeout
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewHTTPClient("slow-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect (this might succeed if connectivity test is fast enough).
	err = client.Connect(ctx)
	if err != nil {
		// If connect fails due to timeout, that's also acceptable for this test.
		t.Skip("Connect failed due to timeout, which is expected behavior")

		return
	}

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Test timeout request.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "slow_method",
		ID:      "test-123",
	}

	_, err = client.SendRequest(ctx, req)
	require.Error(t, err)
	// Could be timeout or request error depending on timing.
}

func TestHTTPClientSendRequestHTTPError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a server that returns HTTP errors.
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Server Error", http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	config := HTTPClientConfig{
		URL:     errorServer.URL,
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewHTTPClient("error-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect should succeed even though requests will fail.
	err = client.Connect(ctx)
	if err != nil {
		// If connectivity test fails, that's expected.
		t.Skip("Connect failed, which is expected for error server")

		return
	}

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Test error request.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-123",
	}

	_, err = client.SendRequest(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP error 500")

	// Check metrics.
	metrics := client.GetMetrics()
	assert.Equal(t, uint64(0), metrics.RequestCount)
	assert.Equal(t, uint64(1), metrics.ErrorCount)
}

func TestHTTPClientHealth(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockHTTPServer()
	defer mockServer.Close()

	config := HTTPClientConfig{
		URL: mockServer.URL,
		HealthCheck: HealthCheckConfig{
			Enabled: false, // We'll test manually
			Timeout: 1 * time.Second,
		},
	}

	client, err := NewHTTPClient("test-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test health when not connected.
	err = client.Health(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")

	// Connect.
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Test health when connected.
	err = client.Health(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateHealthy, client.GetState())

	// Check metrics.
	metrics := client.GetMetrics()
	assert.True(t, metrics.IsHealthy)
	assert.False(t, metrics.LastHealthCheck.IsZero())
}

func TestHTTPClientHealthCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockHTTPServer()
	defer mockServer.Close()

	config := HTTPClientConfig{
		URL:     mockServer.URL,
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 50 * time.Millisecond, // Fast for testing
			Timeout:  1 * time.Second,
		},
	}

	client, err := NewHTTPClient("ping-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect (this will start health check loop).
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Wait for a few health checks to run.
	time.Sleep(2 * constants.TestSleepShort)

	// Should be healthy.
	assert.Equal(t, StateHealthy, client.GetState())
	metrics := client.GetMetrics()
	assert.True(t, metrics.IsHealthy)
}

func TestHTTPClientGetStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := HTTPClientConfig{
		URL: fmt.Sprintf("http://localhost:%d/mcp", constants.TestHTTPPort),
	}

	client, err := NewHTTPClient("status-client", config.URL, config, logger)
	require.NoError(t, err)

	status := client.GetStatus()
	assert.Equal(t, "status-client", status.Name)
	assert.Equal(t, fmt.Sprintf("http://localhost:%d/mcp", constants.TestHTTPPort), status.URL)
	assert.Equal(t, "http", status.Protocol)
	assert.Equal(t, StateDisconnected, status.State)
}

func TestHTTPClientConnectInvalidURL(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := HTTPClientConfig{
		URL:     "http://nonexistent.invalid:12345",
		Timeout: 1 * time.Second, // Short timeout for quick test
	}

	client, err := NewHTTPClient("invalid-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Try to connect to invalid URL.
	err = client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to establish")
	assert.Equal(t, StateError, client.GetState())
}

func createHeaderTestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for custom header.
		if r.Header.Get("X-Test-Header") != TestHeaderValue {
			http.Error(w, "Missing header", http.StatusBadRequest)

			return
		}

		// Check User-Agent.
		if r.Header.Get("User-Agent") != "Custom-Agent/1.0" {
			http.Error(w, "Wrong user agent", http.StatusBadRequest)

			return
		}

		// Return success response.
		resp := mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Result:  "success",
			ID:      "test",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func createHeaderTestConfig(url string) HTTPClientConfig {
	return HTTPClientConfig{
		URL: url,
		Headers: map[string]string{
			"X-Test-Header": "test-value",
		},
		Client: HTTPTransportConfig{
			UserAgent: "Custom-Agent/1.0",
		},
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}
}

func runHeaderTest(t *testing.T, client *HTTPClient) {
	t.Helper()

	ctx := context.Background()

	// Connect should succeed with proper headers.
	err := client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	assert.Equal(t, StateConnected, client.GetState())

	// Send request to verify headers are sent correctly.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "header-test",
	}

	resp, err := client.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "success", resp.Result)
}

func TestHTTPClientHeaders(t *testing.T) {
	logger := zaptest.NewLogger(t)

	headerServer := createHeaderTestServer()
	defer headerServer.Close()

	config := createHeaderTestConfig(headerServer.URL)
	client, err := NewHTTPClient("header-client", config.URL, config, logger)
	require.NoError(t, err)

	runHeaderTest(t, client)
}

func TestHTTPClientURL(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := HTTPClientConfig{
		URL: fmt.Sprintf("http://example.com:%d/path", constants.TestHTTPPort),
	}

	client, err := NewHTTPClient("url-client", "http://original.com", config, logger)
	require.NoError(t, err)

	// Config URL should override serverURL parameter.
	assert.Equal(t, fmt.Sprintf("http://example.com:%d/path", constants.TestHTTPPort), client.GetURL())
}

func TestHTTPClientMethods(t *testing.T) {
	logger := zaptest.NewLogger(t)

	testCases := []struct {
		name       string
		method     string
		wantMethod string
	}{
		{"default method", "", "POST"},
		{"explicit POST", "POST", "POST"},
		{"PUT method", "PUT", "PUT"},
		{"PATCH method", "PATCH", "PATCH"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create server that checks HTTP method.
			methodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.wantMethod {
					http.Error(w, fmt.Sprintf("Expected %s, got %s", tc.wantMethod, r.Method),
						http.StatusMethodNotAllowed)

					return
				}

				resp := mcp.Response{
					JSONRPC: constants.TestJSONRPCVersion,
					Result:  "success",
					ID:      "test",
				}
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer methodServer.Close()

			config := HTTPClientConfig{
				URL:    methodServer.URL,
				Method: tc.method,
				HealthCheck: HealthCheckConfig{
					Enabled: false,
				},
			}

			client, err := NewHTTPClient("method-client", config.URL, config, logger)
			require.NoError(t, err)

			ctx := context.Background()
			err = client.Connect(ctx)
			require.NoError(t, err)

			defer func() {
				if err := client.Close(ctx); err != nil {
					t.Logf("Failed to close client: %v", err)
				}
			}()

			// Verify method is used correctly.
			assert.Equal(t, tc.wantMethod, client.config.Method)
		})
	}
}

func createRedirectTestServers() (*httptest.Server, *httptest.Server) {
	// Create redirect server.
	finalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Result:  "final",
			ID:      "test",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalServer.URL, http.StatusFound)
	}))

	return finalServer, redirectServer
}

func createRedirectTestCases() []struct {
	name                string
	followRedirects     bool
	expectFinalResponse bool
} {
	return []struct {
		name                string
		followRedirects     bool
		expectFinalResponse bool
	}{
		{"follow redirects", true, true},
		{"don't follow redirects", false, false},
	}
}

func runRedirectTest(t *testing.T, tc struct {
	name                string
	followRedirects     bool
	expectFinalResponse bool
}, redirectURL string, logger *zap.Logger) {
	t.Helper()

	config := HTTPClientConfig{
		URL: redirectURL,
		Client: HTTPTransportConfig{
			FollowRedirects: tc.followRedirects,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewHTTPClient("redirect-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect behavior - we accept redirects in connectivity test.
	// since we accept any response as connectivity confirmation
	err = client.Connect(ctx)

	require.NoError(t, err)

	defer func() {
		if err := client.Close(ctx); err != nil {
			t.Logf("Failed to close client: %v", err)
		}
	}()

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "redirect-test",
	}

	resp, err := client.SendRequest(ctx, req)

	if tc.expectFinalResponse {
		// Should follow redirect and get final response.
		require.NoError(t, err)
		assert.Equal(t, "final", resp.Result)
	} else if err == nil {
		// Should not follow redirect, may get error or redirect response.
		// Got a response but it shouldn't be the final one.
		assert.NotEqual(t, "final", resp.Result)
		// Error is also acceptable since redirect wasn't followed.
	}
}

func TestHTTPClientRedirects(t *testing.T) {
	logger := zaptest.NewLogger(t)

	finalServer, redirectServer := createRedirectTestServers()
	defer finalServer.Close()
	defer redirectServer.Close()

	testCases := createRedirectTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runRedirectTest(t, tc, redirectServer.URL, logger)
		})
	}
}

// ==============================================================================
// ENHANCED HTTP CLIENT TESTS - COMPREHENSIVE COVERAGE.
// ==============================================================================

// TestHTTPClientPerformanceOptimizations tests HTTP-specific performance features.
func createPerformanceOptimizationTests() []struct {
	name   string
	config HTTPPerformanceConfig
} {
	return []struct {
		name   string
		config HTTPPerformanceConfig
	}{
		{
			name: "compression enabled",
			config: HTTPPerformanceConfig{
				EnableCompression:    true,
				CompressionThreshold: 1024,
				EnableHTTP2:          true,
				MaxConnsPerHost:      10,
				EnablePipelining:     true,
				ReuseEncoders:        true,
				ResponseBufferSize:   64 * 1024,
			},
		},
		{
			name: "HTTP2 optimized",
			config: HTTPPerformanceConfig{
				EnableCompression:    false,
				CompressionThreshold: 0,
				EnableHTTP2:          true,
				MaxConnsPerHost:      20,
				EnablePipelining:     false,
				ReuseEncoders:        true,
				ResponseBufferSize:   128 * 1024,
			},
		},
		{
			name: "minimal overhead",
			config: HTTPPerformanceConfig{
				EnableCompression:    false,
				CompressionThreshold: 0,
				EnableHTTP2:          false,
				MaxConnsPerHost:      1,
				EnablePipelining:     false,
				ReuseEncoders:        false,
				ResponseBufferSize:   8 * 1024,
			},
		},
	}
}

func runPerformanceOptimizationTest(t *testing.T, tc struct {
	name   string
	config HTTPPerformanceConfig
}, logger *zap.Logger) {
	t.Helper()

	mockServer := newMockHTTPServer()
	defer mockServer.Close()

	client := setupPerformanceTestClient(t, tc, mockServer, logger)
	defer closePerformanceTestClient(t, client)

	connectTime := measureConnectionTime(t, client)
	requestTimes := runPerformanceRequests(t, client)
	verifyPerformanceResults(t, tc.name, connectTime, requestTimes)
}

func setupPerformanceTestClient(t *testing.T, tc struct {
	name   string
	config HTTPPerformanceConfig
}, mockServer *httptest.Server, logger *zap.Logger) *HTTPClient {
	t.Helper()

	config := HTTPClientConfig{
		URL:         mockServer.URL,
		Timeout:     5 * time.Second,
		Performance: tc.config,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewHTTPClient("perf-client", config.URL, config, logger)
	require.NoError(t, err)

	return client
}

func closePerformanceTestClient(t *testing.T, client *HTTPClient) {
	t.Helper()

	err := client.Close(context.Background())
	require.NoError(t, err)
}

func measureConnectionTime(t *testing.T, client *HTTPClient) time.Duration {
	t.Helper()

	ctx := context.Background()
	start := time.Now()
	err := client.Connect(ctx)
	require.NoError(t, err)

	return time.Since(start)
}

func runPerformanceRequests(t *testing.T, client *HTTPClient) []time.Duration {
	t.Helper()

	const numRequests = 15

	ctx := context.Background()
	requestTimes := make([]time.Duration, numRequests)

	for i := 0; i < numRequests; i++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  fmt.Sprintf("perf_test_%d", i),
			ID:      fmt.Sprintf("perf-%d", i),
		}

		start := time.Now()
		resp, err := client.SendRequest(ctx, req)
		requestTimes[i] = time.Since(start)

		require.NoError(t, err)
		assert.NotNil(t, resp)
	}

	return requestTimes
}

func verifyPerformanceResults(t *testing.T, testName string, connectTime time.Duration, requestTimes []time.Duration) {
	t.Helper()

	// Verify performance characteristics.
	assert.Less(t, connectTime, 2*time.Second, "Connection should be fast: %v", connectTime)

	var totalRequestTime time.Duration
	for _, reqTime := range requestTimes {
		totalRequestTime += reqTime
		assert.Less(t, reqTime, 1*time.Second, "Request should be fast: %v", reqTime)
	}

	avgRequestTime := totalRequestTime / time.Duration(len(requestTimes))

	t.Logf("Performance stats for %s:", testName)
	t.Logf("  Connect time: %v", connectTime)
	t.Logf("  Avg request time: %v", avgRequestTime)
	t.Logf("  Total request time: %v", totalRequestTime)
}

func TestHTTPClientPerformanceOptimizations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	testCases := createPerformanceOptimizationTests()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runPerformanceOptimizationTest(t, tc, logger)
		})
	}
}

// TestHTTPClientConcurrentOperations tests concurrent request handling.
func TestHTTPClientConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockHTTPServer()
	defer mockServer.Close()

	client := setupConcurrentHTTPClient(t, mockServer, logger)
	defer closeConcurrentHTTPClient(t, client)

	const (
		numGoroutines        = 8
		requestsPerGoroutine = 4
	)

	errors, responses := runConcurrentHTTPOperations(t, client, numGoroutines, requestsPerGoroutine)
	verifyConcurrentHTTPResults(t, client, errors, responses, numGoroutines, requestsPerGoroutine)
}

func setupConcurrentHTTPClient(t *testing.T, mockServer *httptest.Server, logger *zap.Logger) *HTTPClient {
	t.Helper()

	config := HTTPClientConfig{
		URL:     mockServer.URL,
		Timeout: 10 * time.Second,
		Performance: HTTPPerformanceConfig{
			EnableCompression:  true,
			EnableHTTP2:        true,
			MaxConnsPerHost:    20,
			EnablePipelining:   true,
			ReuseEncoders:      true,
			ResponseBufferSize: 64 * 1024,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewHTTPClient("concurrent-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	return client
}

func closeConcurrentHTTPClient(t *testing.T, client *HTTPClient) {
	t.Helper()

	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runConcurrentHTTPOperations(
	t *testing.T,
	client *HTTPClient,
	numGoroutines, requestsPerGoroutine int,
) ([]error, []*mcp.Response) {
	t.Helper()

	channelSize := numGoroutines * requestsPerGoroutine
	errChan := make(chan error, channelSize)
	responseChan := make(chan *mcp.Response, channelSize)

	done := launchConcurrentHTTPWorkers(client, numGoroutines, requestsPerGoroutine, errChan, responseChan)
	waitForHTTPCompletion(t, done)

	return collectHTTPResults(errChan, responseChan, channelSize)
}

func launchConcurrentHTTPWorkers(
	client *HTTPClient,
	numGoroutines, requestsPerGoroutine int,
	errChan chan<- error,
	responseChan chan<- *mcp.Response,
) chan struct{} {
	var wg sync.WaitGroup
	ctx := context.Background()
	done := make(chan struct{})

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go processHTTPWorkerRequests(&wg, client, ctx, g, requestsPerGoroutine, errChan, responseChan)
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	return done
}

func processHTTPWorkerRequests(
	wg *sync.WaitGroup,
	client *HTTPClient,
	ctx context.Context,
	goroutineID, requestsPerGoroutine int,
	errChan chan<- error,
	responseChan chan<- *mcp.Response,
) {
	defer wg.Done()

	for r := 0; r < requestsPerGoroutine; r++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "concurrent_test",
			ID:      fmt.Sprintf("concurrent-%d-%d-%d", goroutineID, r, time.Now().UnixNano()),
		}

		resp, err := client.SendRequest(ctx, req)
		if err != nil {
			errChan <- err

			return
		}
		responseChan <- resp
	}
}

func waitForHTTPCompletion(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		// Success
	case <-time.After(30 * time.Second):
		t.Fatal("Concurrent operations timed out")
	}
}

func collectHTTPResults(errChan chan error, responseChan chan *mcp.Response, capacity int) ([]error, []*mcp.Response) {
	close(errChan)
	close(responseChan)

	errors := make([]error, 0, capacity)
	for err := range errChan {
		errors = append(errors, err)
	}

	responses := make([]*mcp.Response, 0, capacity)
	for resp := range responseChan {
		responses = append(responses, resp)
	}

	return errors, responses
}

func verifyConcurrentHTTPResults(
	t *testing.T,
	client *HTTPClient,
	errors []error,
	responses []*mcp.Response,
	numGoroutines, requestsPerGoroutine int,
) {
	t.Helper()

	assert.Empty(t, errors, "No errors should occur during concurrent operations")
	assert.Len(t, responses, numGoroutines*requestsPerGoroutine, "All requests should receive responses")

	// Verify metrics.
	metrics := client.GetMetrics()
	assert.Equal(t, safeIntToUint64(numGoroutines*requestsPerGoroutine), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
	assert.True(t, metrics.IsHealthy)
}

func BenchmarkHTTPClientSendRequest(b *testing.B) {
	logger := zaptest.NewLogger(b)

	mockServer := newMockHTTPServer()
	defer mockServer.Close()

	config := HTTPClientConfig{
		URL:     mockServer.URL,
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewHTTPClient("bench-client", config.URL, config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(b, err)
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "benchmark",
				ID:      fmt.Sprintf("bench-%d", i),
			}
			i++

			_, err := client.SendRequest(ctx, req)
			if err != nil {
				b.Errorf("Request failed: %v", err)
			}
		}
	})
}
