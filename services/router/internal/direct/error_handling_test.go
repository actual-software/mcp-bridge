package direct

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

func createErrorRecoveryTests(t *testing.T) []struct {
	name           string
	clientFunc     func() DirectClient
	testScenarios  []string
	expectedErrors int
	description    string
} {
	t.Helper()

	stdioTest := createStdioErrorRecoveryTest(t)
	httpTest := createHTTPErrorRecoveryTest(t)

	return []struct {
		name           string
		clientFunc     func() DirectClient
		testScenarios  []string
		expectedErrors int
		description    string
	}{
		stdioTest,
		httpTest,
	}
}

func runErrorRecoveryTest(t *testing.T, tt struct {
	name           string
	clientFunc     func() DirectClient
	testScenarios  []string
	expectedErrors int
	description    string
}) {
	t.Helper()

	client := tt.clientFunc()
	require.NotNil(t, client)

	ctx := context.Background()

	// Try to connect (may fail for some clients, which is acceptable).
	err := client.Connect(ctx)
	if err != nil && !strings.Contains(tt.name, "http") {
		t.Logf("Connect failed (expected for some tests): %v", err)
	}

	defer func() {
		if err := client.Close(ctx); err != nil {
			t.Logf("Failed to close client: %v", err)
		}
	}()

	errorCount := 0

	for i, scenario := range tt.testScenarios {
		reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)

		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "test/method",
			Params:  map[string]interface{}{"scenario": scenario},
			ID:      fmt.Sprintf("error-recovery-%d", i),
		}

		_, err := client.SendRequest(reqCtx, req)

		cancel()

		if err != nil {
			t.Logf("Request %d (%s) failed: %v", i, scenario, err)

			errorCount++
		}
	}

	t.Logf("Error recovery test: %d errors out of %d requests", errorCount, len(tt.testScenarios))
}

func TestDirectClient_ErrorRecovery(t *testing.T) {
	tests := createErrorRecoveryTests(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runErrorRecoveryTest(t, tt)
		})
	}
}

func createTimeoutHandlingTests(t *testing.T) []struct {
	name        string
	clientFunc  func() DirectClient
	timeout     time.Duration
	expectError bool
	description string
} {
	t.Helper()

	return []struct {
		name        string
		clientFunc  func() DirectClient
		timeout     time.Duration
		expectError bool
		description string
	}{
		{
			name: "stdio_timeout",
			clientFunc: func() DirectClient {
				config := StdioClientConfig{
					Timeout:       5 * time.Second,
					MaxBufferSize: 1024,
				}
				// Command that sleeps longer than our test timeout.
				client, err := NewStdioClient("test-sleep", "sleep 10", config, zaptest.NewLogger(t))
				if err != nil {
					t.Fatalf("Failed to create stdio client: %v", err)
				}

				return client
			},
			timeout:     httpStatusInternalError * time.Millisecond,
			expectError: true,
			description: "Stdio client should timeout on slow commands",
		},
		{
			name: "http_timeout",
			clientFunc: func() DirectClient {
				config := HTTPClientConfig{
					URL:     "http://httpbin.org/delay/5",
					Timeout: 5 * time.Second,
					Client: HTTPTransportConfig{
						MaxIdleConns: 1,
					},
				}
				client, err := NewHTTPClient("test-delay", "http://httpbin.org/delay/5", config, zaptest.NewLogger(t))
				require.NoError(t, err)

				return client
			},
			timeout:     httpStatusInternalError * time.Millisecond,
			expectError: true,
			description: "HTTP client should timeout on slow responses",
		},
	}
}

func runTimeoutHandlingTest(t *testing.T, tt struct {
	name        string
	clientFunc  func() DirectClient
	timeout     time.Duration
	expectError bool
	description string
}) {
	t.Helper()

	client := tt.clientFunc()
	require.NotNil(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to connect.
	err := client.Connect(ctx)
	if err != nil {
		t.Logf("Connect failed (may be expected): %v", err)
	}

	defer func() {
		// Use a short timeout for cleanup to avoid hanging tests.
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()

		_ = client.Close(closeCtx)
	}()

	// Test timeout on request.
	reqCtx, reqCancel := context.WithTimeout(ctx, tt.timeout)
	defer reqCancel()

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test/timeout",
		ID:      "timeout-test",
	}

	start := time.Now()
	_, err = client.SendRequest(reqCtx, req)
	duration := time.Since(start)

	if tt.expectError {
		require.Error(t, err, tt.description)
		assert.LessOrEqual(t, duration, tt.timeout+testIterations*time.Millisecond,
			"Request should timeout within expected duration")
	} else {
		require.NoError(t, err, tt.description)
	}

	t.Logf("Timeout test completed in %v", duration)
}

func TestDirectClient_TimeoutHandling(t *testing.T) {
	tests := createTimeoutHandlingTests(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runTimeoutHandlingTest(t, tt)
		})
	}
}

func createConnectionFailureTests(t *testing.T) []struct {
	name        string
	clientFunc  func() DirectClient
	description string
} {
	t.Helper()

	stdioTest := createStdioFailureTest(t)
	httpTest := createHTTPFailureTest(t)
	webSocketTest := createWebSocketFailureTest(t)

	return []struct {
		name        string
		clientFunc  func() DirectClient
		description string
	}{
		stdioTest,
		httpTest,
		webSocketTest,
	}
}

func runConnectionFailureTest(t *testing.T, tt struct {
	name        string
	clientFunc  func() DirectClient
	description string
}) {
	t.Helper()

	client := tt.clientFunc()
	require.NotNil(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connection should fail.
	err := client.Connect(ctx)
	require.Error(t, err, "Connection should fail for invalid targets")

	// Requests should also fail when not connected.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test/method",
		ID:      "connection-failure-test",
	}

	_, err = client.SendRequest(ctx, req)
	require.Error(t, err, "Requests should fail when not connected")

	// Health check should also fail.
	err = client.Health(ctx)
	require.Error(t, err, "Health check should fail when not connected")

	// Close should not panic even if not connected.
	assert.NotPanics(t, func() {
		_ = client.Close(context.Background())
	}, "Close should not panic even if not connected")
}

func TestDirectClient_ConnectionFailure(t *testing.T) {
	tests := createConnectionFailureTests(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runConnectionFailureTest(t, tt)
		})
	}
}

func runConcurrentRequestsTest(t *testing.T) {
	t.Helper()

	// Test concurrent access to DirectClientManager rather than individual client requests.
	// This focuses on the concurrency safety of the manager itself.
	manager := setupConcurrentTestManager(t)

	defer func() { _ = manager.Stop(context.Background()) }()

	results := runConcurrentClientOperations(t, manager)
	verifyConcurrentResults(t, results)
}

func TestDirectClient_ConcurrentRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	runConcurrentRequestsTest(t)
}

func createErrorPropagationConfig() DirectConfig {
	return DirectConfig{
		MaxConnections: 5,
		DefaultTimeout: 2 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			Timeout:        httpStatusInternalError * time.Millisecond,
			CacheResults:   false,
			PreferredOrder: []string{"http", "websocket", "stdio", "sse"},
		},
	}
}

func createErrorPropagationTests() []struct {
	name        string
	serverURL   string
	expectError bool
	description string
} {
	return []struct {
		name        string
		serverURL   string
		expectError bool
		description string
	}{
		{
			name:        "invalid_url",
			serverURL:   "://invalid-url",
			expectError: true,
			description: "Invalid URL should propagate parsing error",
		},
		{
			name:        "nonexistent_host",
			serverURL:   "http://nonexistent-host-12345.invalid",
			expectError: true,
			description: "Nonexistent host should propagate connection error",
		},
		{
			name:        "valid_stdio",
			serverURL:   "echo success",
			expectError: false,
			description: "Valid stdio command should succeed",
		},
		{
			name:        "invalid_stdio",
			serverURL:   "nonexistent-command-12345",
			expectError: true,
			description: "Invalid stdio command should propagate execution error",
		},
	}
}

func runErrorPropagationTest(t *testing.T, manager DirectClientManagerInterface, tt struct {
	name        string
	serverURL   string
	expectError bool
	description string
}) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := manager.GetClient(ctx, tt.serverURL)

	if tt.expectError {
		require.Error(t, err, tt.description)
		assert.Nil(t, client, "Client should be nil on error")
	} else {
		require.NoError(t, err, tt.description)
		assert.NotNil(t, client, "Client should not be nil on success")
	}
}

func TestDirectClientManager_ErrorPropagation(t *testing.T) {
	config := createErrorPropagationConfig()
	logger := zaptest.NewLogger(t)
	manager := NewDirectClientManager(config, logger)
	require.NotNil(t, manager)

	err := manager.Start(context.Background())
	require.NoError(t, err)

	defer func() { _ = manager.Stop(context.Background()) }()

	tests := createErrorPropagationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runErrorPropagationTest(t, manager, tt)
		})
	}
}

// Helper functions for createConnectionFailureTests.
func createStdioFailureTest(t *testing.T) struct {
	name        string
	clientFunc  func() DirectClient
	description string
} {
	t.Helper()

	return struct {
		name        string
		clientFunc  func() DirectClient
		description string
	}{
		name: "stdio_invalid_command",
		clientFunc: func() DirectClient {
			config := StdioClientConfig{
				Timeout:       2 * time.Second,
				MaxBufferSize: 1024,
			}
			client, err := NewStdioClient("test-invalid", "nonexistent-command-12345", config, zaptest.NewLogger(t))
			if err != nil {
				t.Fatalf("Failed to create stdio client: %v", err)
			}

			return client
		},
		description: "Stdio client should handle invalid commands gracefully",
	}
}

func createHTTPFailureTest(t *testing.T) struct {
	name        string
	clientFunc  func() DirectClient
	description string
} {
	t.Helper()

	return struct {
		name        string
		clientFunc  func() DirectClient
		description string
	}{
		name: "http_invalid_url",
		clientFunc: func() DirectClient {
			config := HTTPClientConfig{
				URL:     "http://nonexistent-host-12345.invalid",
				Timeout: 2 * time.Second,
				Client: HTTPTransportConfig{
					MaxIdleConns: 1,
				},
			}
			client, err := NewHTTPClient(
				"test-invalid-http",
				"http://nonexistent-host-12345.invalid",
				config,
				zaptest.NewLogger(t),
			)
			require.NoError(t, err)

			return client
		},
		description: "HTTP client should handle connection failures gracefully",
	}
}

func createWebSocketFailureTest(t *testing.T) struct {
	name        string
	clientFunc  func() DirectClient
	description string
} {
	t.Helper()

	return struct {
		name        string
		clientFunc  func() DirectClient
		description string
	}{
		name: "websocket_invalid_url",
		clientFunc: func() DirectClient {
			config := WebSocketClientConfig{
				URL:              "ws://nonexistent-host-12345.invalid",
				HandshakeTimeout: 2 * time.Second,
				PingInterval:     30 * time.Second,
			}
			client, err := NewWebSocketClient(
				"test-invalid-ws",
				"ws://nonexistent-host-12345.invalid",
				config,
				zaptest.NewLogger(t),
			)
			require.NoError(t, err)

			return client
		},
		description: "WebSocket client should handle connection failures gracefully",
	}
}

// Helper functions for runConcurrentRequestsTest.
func setupConcurrentTestManager(t *testing.T) DirectClientManagerInterface {
	t.Helper()

	config := DirectConfig{
		MaxConnections: 10,
		DefaultTimeout: 3 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			Timeout:        1 * time.Second,
			CacheResults:   true,
			PreferredOrder: []string{"stdio"},
		},
	}
	logger := zaptest.NewLogger(t)
	manager := NewDirectClientManager(config, logger)
	require.NotNil(t, manager)

	err := manager.Start(context.Background())
	require.NoError(t, err)

	return manager
}

func runConcurrentClientOperations(t *testing.T, manager DirectClientManagerInterface) chan error {
	t.Helper()

	const numConcurrent = 5

	var wg sync.WaitGroup

	results := make(chan error, numConcurrent)

	// Test concurrent access to manager.GetClient
	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			// Use unique server URLs to test concurrent client creation.
			serverURL := fmt.Sprintf("echo concurrent test %d", workerID)

			client, err := manager.GetClient(ctx, serverURL)
			if err != nil {
				results <- fmt.Errorf("worker %d GetClient failed: %w", workerID, err)

				return
			}

			// Test basic client operations under concurrent load.
			_ = client.GetName()
			_ = client.GetProtocol()
			metrics := client.GetMetrics()
			_ = metrics.RequestCount

			results <- nil
		}(i)
	}

	wg.Wait()
	close(results)

	return results
}

func verifyConcurrentResults(t *testing.T, results chan error) {
	t.Helper()

	const numConcurrent = 5

	// Check results.
	successCount := 0
	errorCount := 0

	for err := range results {
		if err != nil {
			t.Logf("Concurrent access error: %v", err)

			errorCount++
		} else {
			successCount++
		}
	}

	t.Logf("Concurrent test: %d successes, %d errors out of %d workers",
		successCount, errorCount, numConcurrent)

	// All concurrent manager access should succeed.
	assert.Equal(t, numConcurrent, successCount, "All concurrent client creations should succeed")
}

// Helper functions for createErrorRecoveryTests.
func createStdioErrorRecoveryTest(t *testing.T) struct {
	name           string
	clientFunc     func() DirectClient
	testScenarios  []string
	expectedErrors int
	description    string
} {
	t.Helper()

	return struct {
		name           string
		clientFunc     func() DirectClient
		testScenarios  []string
		expectedErrors int
		description    string
	}{
		name: "stdio_error_recovery",
		clientFunc: func() DirectClient {
			config := StdioClientConfig{
				Timeout:       2 * time.Second,
				MaxBufferSize: 1024,
			}
			// Use a command that will fail after some attempts.
			client, err := NewStdioClient(
				"test-stdio",
				`bash -c 'if [ $RANDOM -lt 16384 ]; then echo error >&2; exit 1; else echo "{\"jsonrpc\":\"2.0\",
				\"result\":{\"success\":true},
				\"id\":\"test\"}"; fi'`,
				config,
				zaptest.NewLogger(t),
			)
			if err != nil {
				t.Fatalf("Failed to create stdio client: %v", err)
			}

			return client
		},
		testScenarios:  []string{"normal", "error", "recovery"},
		expectedErrors: 1, // Expect some errors but not all
		description:    "Stdio client should handle intermittent command failures",
	}
}

func createHTTPErrorRecoveryTest(t *testing.T) struct {
	name           string
	clientFunc     func() DirectClient
	testScenarios  []string
	expectedErrors int
	description    string
} {
	t.Helper()

	return struct {
		name           string
		clientFunc     func() DirectClient
		testScenarios  []string
		expectedErrors int
		description    string
	}{
		name: "http_error_recovery",
		clientFunc: func() DirectClient {
			config := HTTPClientConfig{
				URL:     "http://httpbin.org/status/500",
				Timeout: 1 * time.Second,
				Client: HTTPTransportConfig{
					MaxIdleConns: 1,
				},
			}
			client, err := NewHTTPClient(
				"test-http",
				"http://httpbin.org/status/500",
				config,
				zaptest.NewLogger(t),
			)
			require.NoError(t, err)

			return client
		},
		testScenarios:  []string{"500_error", "500_error", "500_error"},
		expectedErrors: 3, // All should fail with 500 errors
		description:    "HTTP client should handle server errors gracefully",
	}
}
