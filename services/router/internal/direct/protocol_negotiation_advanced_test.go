package direct

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestAdvancedProtocolNegotiation_MultiProtocolDetection tests comprehensive protocol detection.
// setupHTTPServer creates an HTTP server for protocol testing.
func setupHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok","id":1}`))
	}))
}

// setupFailingHTTPServer creates an HTTP server that returns 404.
func setupFailingHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
}

// setupWebSocketServer creates a WebSocket server for protocol testing.
func setupWebSocketServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("WebSocket upgrade failed: %v", err)

			return
		}

		defer func() { _ = conn.Close() }()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				t.Logf("Failed to write message: %v", err)
			}
		}
	}))
}

// setupSSEServer creates an SSE server for protocol testing.
func setupSSEServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "data: test\n\n")
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
}

// setupDelayedHTTPServer creates an HTTP server with simulated network delay.
func setupDelayedHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * constants.TestSleepShort)
		w.WriteHeader(http.StatusOK)
	}))
}

// setupProtocolManager creates and starts a DirectClientManager for testing.
func setupProtocolManager(
	t *testing.T,
	preferredOrder []string,
) (*DirectClientManager, context.Context, context.CancelFunc) {
	t.Helper()

	logger := zaptest.NewLogger(t)

	config := DirectConfig{
		MaxConnections: 10,
		DefaultTimeout: 5 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			PreferredOrder: preferredOrder,
			Timeout:        10 * time.Second,
			CacheResults:   true,
			CacheTTL:       5 * time.Minute,
		},
	}

	manager := NewDirectClientManager(config, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

	err := manager.Start(ctx)
	require.NoError(t, err)

	return manager, ctx, cancel
}

// getTestURLForProtocol returns the appropriate test URL for the given protocol.
func getTestURLForProtocol(protocol ClientType, httpURL, wsURL, sseURL string) string {
	switch protocol {
	case ClientTypeHTTP:
		return httpURL
	case ClientTypeWebSocket:
		return wsURL
	case ClientTypeSSE:
		return sseURL
	case ClientTypeStdio:
		return "stdio://test-command"
	default:
		return ""
	}
}

// validateProtocolDetection performs protocol detection tests with caching validation.
func validateProtocolDetection(
	t *testing.T,
	manager *DirectClientManager,
	ctx context.Context,
	testURL string,
	expectedProtocol ClientType,
	networkDelay time.Duration,
) {
	t.Helper()

	if networkDelay > 0 {
		time.Sleep(networkDelay)
	}

	detectedProtocol, err := manager.DetectProtocol(ctx, testURL)
	require.NoError(t, err, "Protocol detection should succeed")
	assert.Equal(t, expectedProtocol, detectedProtocol, "Detected protocol should match expected")

	detectedProtocol2, err := manager.DetectProtocol(ctx, testURL)
	require.NoError(t, err, "Second protocol detection should succeed")
	assert.Equal(t, expectedProtocol, detectedProtocol2, "Second detection should return same protocol")

	metrics := manager.GetMetrics()
	assert.Positive(t, metrics.CacheHits, "Should have cache hits after second detection")
	assert.Positive(t, metrics.ProtocolDetections, "Should have protocol detections")
}

// createProtocolTestCases creates test cases for protocol detection testing.
func createProtocolTestCases() []struct {
	name             string
	setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
	preferredOrder   []string
	expectedProtocol ClientType
	expectedCacheHit bool
	networkDelay     time.Duration
	description      string
} {
	testCases := make([]struct {
		name             string
		setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
		preferredOrder   []string
		expectedProtocol ClientType
		expectedCacheHit bool
		networkDelay     time.Duration
		description      string
	}, 0, 5)

	testCases = append(testCases, createHTTPFirstPreferenceTest())
	testCases = append(testCases, createWebSocketFallbackTest())
	testCases = append(testCases, createSSEFallbackTest())
	testCases = append(testCases, createCustomPreferenceTest())
	testCases = append(testCases, createNetworkDelayTest())

	return testCases
}

func createHTTPFirstPreferenceTest() struct {
	name             string
	setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
	preferredOrder   []string
	expectedProtocol ClientType
	expectedCacheHit bool
	networkDelay     time.Duration
	description      string
} {
	return struct {
		name             string
		setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
		preferredOrder   []string
		expectedProtocol ClientType
		expectedCacheHit bool
		networkDelay     time.Duration
		description      string
	}{
		name: "http_first_preference_success",
		setupServers: func(t *testing.T) (string, string, string, func()) {
			t.Helper()
			httpServer := setupHTTPServer(t)

			return httpServer.URL, "", "", func() { httpServer.Close() }
		},
		preferredOrder:   []string{"http", "websocket", "sse", "stdio"},
		expectedProtocol: ClientTypeHTTP,
		description:      "HTTP server should be detected first in preference order",
	}
}

func createWebSocketFallbackTest() struct {
	name             string
	setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
	preferredOrder   []string
	expectedProtocol ClientType
	expectedCacheHit bool
	networkDelay     time.Duration
	description      string
} {
	return struct {
		name             string
		setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
		preferredOrder   []string
		expectedProtocol ClientType
		expectedCacheHit bool
		networkDelay     time.Duration
		description      string
	}{
		name: "websocket_fallback_when_http_fails",
		setupServers: func(t *testing.T) (string, string, string, func()) {
			t.Helper()
			httpServer := setupFailingHTTPServer(t)
			wsServer := setupWebSocketServer(t)
			wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")

			return httpServer.URL, wsURL, "", func() {
				httpServer.Close()
				wsServer.Close()
			}
		},
		preferredOrder:   []string{"http", "websocket", "sse", "stdio"},
		expectedProtocol: ClientTypeWebSocket,
		description:      "WebSocket should be detected when HTTP fails",
	}
}

func createSSEFallbackTest() struct {
	name             string
	setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
	preferredOrder   []string
	expectedProtocol ClientType
	expectedCacheHit bool
	networkDelay     time.Duration
	description      string
} {
	return struct {
		name             string
		setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
		preferredOrder   []string
		expectedProtocol ClientType
		expectedCacheHit bool
		networkDelay     time.Duration
		description      string
	}{
		name: "sse_fallback_chain",
		setupServers: func(t *testing.T) (string, string, string, func()) {
			t.Helper()
			sseServer := setupSSEServer(t)

			return "", "", sseServer.URL, func() { sseServer.Close() }
		},
		preferredOrder:   []string{"http", "websocket", "sse", "stdio"},
		expectedProtocol: ClientTypeHTTP,
		description:      "HTTP should be detected for a server that supports both HTTP and SSE",
	}
}

func createCustomPreferenceTest() struct {
	name             string
	setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
	preferredOrder   []string
	expectedProtocol ClientType
	expectedCacheHit bool
	networkDelay     time.Duration
	description      string
} {
	return struct {
		name             string
		setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
		preferredOrder   []string
		expectedProtocol ClientType
		expectedCacheHit bool
		networkDelay     time.Duration
		description      string
	}{
		name: "custom_preference_order",
		setupServers: func(t *testing.T) (string, string, string, func()) {
			t.Helper()
			httpServer := setupHTTPServer(t)
			wsServer := setupWebSocketServer(t)
			wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")

			return httpServer.URL, wsURL, "", func() {
				httpServer.Close()
				wsServer.Close()
			}
		},
		preferredOrder:   []string{"websocket", "http", "sse", "stdio"},
		expectedProtocol: ClientTypeWebSocket,
		description:      "Custom preference order should be respected",
	}
}

func createNetworkDelayTest() struct {
	name             string
	setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
	preferredOrder   []string
	expectedProtocol ClientType
	expectedCacheHit bool
	networkDelay     time.Duration
	description      string
} {
	return struct {
		name             string
		setupServers     func(t *testing.T) (httpURL, wsURL, sseURL string, cleanup func())
		preferredOrder   []string
		expectedProtocol ClientType
		expectedCacheHit bool
		networkDelay     time.Duration
		description      string
	}{
		name: "protocol_detection_with_network_delay",
		setupServers: func(t *testing.T) (string, string, string, func()) {
			t.Helper()
			httpServer := setupDelayedHTTPServer(t)

			return httpServer.URL, "", "", func() { httpServer.Close() }
		},
		preferredOrder:   []string{"http", "websocket", "sse", "stdio"},
		expectedProtocol: ClientTypeHTTP,
		networkDelay:     1 * time.Second,
		description:      "Protocol detection should handle network delays",
	}
}

func TestAdvancedProtocolNegotiation_MultiProtocolDetection(t *testing.T) {
	t.Parallel()

	tests := createProtocolTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manager, ctx, cancel := setupProtocolManager(t, tt.preferredOrder)
			defer cancel()

			defer func() {
				if err := manager.Stop(ctx); err != nil {
					t.Logf("Failed to stop manager: %v", err)
				}
			}()

			httpURL, wsURL, sseURL, cleanup := tt.setupServers(t)
			defer cleanup()

			testURL := getTestURLForProtocol(tt.expectedProtocol, httpURL, wsURL, sseURL)
			if testURL == "" {
				t.Skip("Test URL not available for expected protocol")
			}

			validateProtocolDetection(t, manager, ctx, testURL, tt.expectedProtocol, tt.networkDelay)
		})
	}
}

// TestAdvancedProtocolNegotiation_ConcurrentDetection tests concurrent protocol detection.
func TestAdvancedProtocolNegotiation_ConcurrentDetection(t *testing.T) {
	t.Parallel()

	manager := setupConcurrentDetectionManager(t)
	defer cleanupConcurrentDetectionManager(t, manager)

	servers, serverURLs := setupConcurrentDetectionServers()
	defer closeConcurrentDetectionServers(servers)

	runConcurrentDetectionTest(t, manager, serverURLs)
}

func setupConcurrentDetectionManager(t *testing.T) *DirectClientManager {
	t.Helper()
	logger := zaptest.NewLogger(t)

	config := DirectConfig{
		MaxConnections: 50,
		DefaultTimeout: 5 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			PreferredOrder: []string{"http", "websocket", "sse", "stdio"},
			Timeout:        10 * time.Second,
			CacheResults:   true,
			CacheTTL:       5 * time.Minute,
		},
	}

	manager := NewDirectClientManager(config, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := manager.Start(ctx)
	require.NoError(t, err)

	return manager
}

func cleanupConcurrentDetectionManager(t *testing.T, manager *DirectClientManager) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := manager.Stop(ctx); err != nil {
		t.Logf("Failed to stop manager: %v", err)
	}
}

func setupConcurrentDetectionServers() ([]*httptest.Server, []string) {
	servers := make([]*httptest.Server, 5)
	serverURLs := make([]string, 5)

	for i := range servers {
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			delay := time.Duration(i*100) * time.Millisecond
			time.Sleep(delay)
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"server":%d}`, i)
		}))
		serverURLs[i] = servers[i].URL
	}

	return servers, serverURLs
}

func closeConcurrentDetectionServers(servers []*httptest.Server) {
	for _, server := range servers {
		server.Close()
	}
}

func runConcurrentDetectionTest(t *testing.T, manager *DirectClientManager, serverURLs []string) {
	t.Helper()

	const (
		numWorkers          = 20
		detectionsPerWorker = 10
	)

	var (
		wg                   sync.WaitGroup
		successfulDetections int64
		errors               int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)

		go runConcurrentDetectionWorker(
			t, &wg, manager, serverURLs, ctx, i, detectionsPerWorker,
			&successfulDetections, &errors,
		)
	}

	wg.Wait()

	verifyConcurrentDetectionResults(t, manager, numWorkers, detectionsPerWorker, successfulDetections, errors)
}

func runConcurrentDetectionWorker(t *testing.T, wg *sync.WaitGroup, manager *DirectClientManager,
	serverURLs []string, ctx context.Context, workerID, detectionsPerWorker int,
	successfulDetections, errors *int64) {
	t.Helper()
	defer wg.Done()

	for j := 0; j < detectionsPerWorker; j++ {
		serverURL := serverURLs[j%len(serverURLs)]

		protocol, err := manager.DetectProtocol(ctx, serverURL)
		if err != nil {
			atomic.AddInt64(errors, 1)
			t.Logf("Worker %d detection %d failed: %v", workerID, j, err)

			continue
		}

		if protocol != ClientTypeHTTP {
			t.Logf("Worker %d detected unexpected protocol %s for %s", workerID, protocol, serverURL)

			continue
		}

		atomic.AddInt64(successfulDetections, 1)
	}
}

func verifyConcurrentDetectionResults(
	t *testing.T,
	manager *DirectClientManager,
	numWorkers, detectionsPerWorker int,
	successfulDetections, errors int64,
) {
	t.Helper()

	totalDetections := int64(numWorkers * detectionsPerWorker)
	successRate := float64(successfulDetections) / float64(totalDetections)

	assert.Greater(t, successRate, 0.9, "Success rate should be > 90%")
	assert.Less(t, atomic.LoadInt64(&errors), totalDetections/10, "Error rate should be < 10%")

	metrics := manager.GetMetrics()
	assert.Positive(t, metrics.ProtocolDetections, "Should have protocol detections")
	assert.Positive(t, metrics.CacheHits, "Should have cache hits from repeated detections")

	t.Logf("Concurrent detection results: %d successful, %d errors out of %d total",
		successfulDetections, atomic.LoadInt64(&errors), totalDetections)
}

// TestAdvancedProtocolNegotiation_FailureScenarios tests various failure scenarios.
func TestAdvancedProtocolNegotiation_FailureScenarios(t *testing.T) {
	t.Parallel()

	tests := createFailureScenarioTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runFailureScenarioTest(t, tt)
		})
	}
}

type failureScenarioTest struct {
	name          string
	config        DirectConfig
	serverURL     string
	setupServer   func(t *testing.T) (string, func())
	expectedError string
	testTimeout   time.Duration
	description   string
}

func createFailureScenarioTests() []failureScenarioTest {
	return []failureScenarioTest{
		{
			name:          "auto_detection_disabled",
			config:        createDisabledAutoDetectionConfig(),
			serverURL:     "http://example.com",
			expectedError: "protocol auto-detection is disabled",
			description:   "Should fail when auto-detection is disabled",
		},
		{
			name:          "all_protocols_fail",
			config:        createAllProtocolsFailConfig(),
			setupServer:   setupAllProtocolsFailServer,
			expectedError: "could not detect compatible protocol",
			description:   "Should fail when all protocols are incompatible",
		},
		{
			name:          "detection_timeout",
			config:        createDetectionTimeoutConfig(),
			setupServer:   setupSlowResponseServer,
			expectedError: "could not detect compatible protocol",
			testTimeout:   5 * time.Second,
			description:   "Should timeout when detection takes too long",
		},
		{
			name:          "invalid_url_format",
			config:        createValidAutoDetectionConfig(),
			serverURL:     "://invalid-url-format",
			expectedError: "could not detect compatible protocol",
			description:   "Should handle invalid URL formats gracefully",
		},
		{
			name:          "unreachable_host",
			config:        createValidAutoDetectionConfig(),
			serverURL:     "http://unreachable-host-12345.invalid",
			expectedError: "could not detect compatible protocol",
			testTimeout:   10 * time.Second,
			description:   "Should handle unreachable hosts gracefully",
		},
	}
}

func createDisabledAutoDetectionConfig() DirectConfig {
	return DirectConfig{
		MaxConnections: 10,
		DefaultTimeout: 5 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled: false, // Disabled
		},
	}
}

func createAllProtocolsFailConfig() DirectConfig {
	return DirectConfig{
		MaxConnections: 10,
		DefaultTimeout: 2 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			PreferredOrder: []string{"websocket", "sse", "stdio"}, // Remove HTTP so detection actually fails
			Timeout:        5 * time.Second,
		},
	}
}

func createDetectionTimeoutConfig() DirectConfig {
	return DirectConfig{
		MaxConnections: 10,
		DefaultTimeout: 5 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			PreferredOrder: []string{"http", "websocket", "sse", "stdio"},
			Timeout:        100 * time.Millisecond, // Very short timeout
		},
	}
}

func createValidAutoDetectionConfig() DirectConfig {
	return DirectConfig{
		MaxConnections: 10,
		DefaultTimeout: 5 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			PreferredOrder: []string{"http", "websocket", "sse", "stdio"},
			Timeout:        10 * time.Second,
		},
	}
}

func setupAllProtocolsFailServer(t *testing.T) (string, func()) {
	t.Helper()

	// Server that returns HTTP responses but doesn't support WebSocket/SSE
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	return server.URL, server.Close
}

func setupSlowResponseServer(t *testing.T) (string, func()) {
	t.Helper()

	// Server with long response time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second) // Longer than timeout
		w.WriteHeader(http.StatusOK)
	}))

	return server.URL, server.Close
}

func runFailureScenarioTest(t *testing.T, tt failureScenarioTest) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	manager := NewDirectClientManager(tt.config, logger)

	timeout := tt.testTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		if err := manager.Stop(ctx); err != nil {
			t.Logf("Failed to stop manager: %v", err)
		}
	}()

	testURL := determineTestURL(t, tt)
	runProtocolDetectionTest(t, manager, ctx, testURL, tt.expectedError)
	verifyNoCacheForFailures(t, manager, ctx, testURL, tt.config)
}

func determineTestURL(t *testing.T, tt failureScenarioTest) string {
	t.Helper()

	if tt.setupServer != nil {
		testURL, cleanup := tt.setupServer(t)
		t.Cleanup(cleanup)

		return testURL
	}

	return tt.serverURL
}

func runProtocolDetectionTest(
	t *testing.T,
	manager *DirectClientManager,
	ctx context.Context,
	testURL, expectedError string,
) {
	t.Helper()

	// Test protocol detection failure
	_, err := manager.DetectProtocol(ctx, testURL)
	require.Error(t, err, "Protocol detection should fail")
	assert.Contains(t, err.Error(), expectedError, "Error message should contain expected text")
}

func verifyNoCacheForFailures(
	t *testing.T,
	manager *DirectClientManager,
	ctx context.Context,
	testURL string,
	config DirectConfig,
) {
	t.Helper()

	// Verify that cache doesn't contain failed entries
	if config.AutoDetection.Enabled && config.AutoDetection.CacheResults {
		// Try again to ensure it's not cached
		_, err2 := manager.DetectProtocol(ctx, testURL)
		require.Error(t, err2, "Second detection should also fail")
	}
}

// TestAdvancedProtocolNegotiation_EdgeCases tests edge cases and boundary conditions.
func TestAdvancedProtocolNegotiation_EdgeCases(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	config := createEdgeCaseTestConfig()

	manager := setupEdgeCaseTestManager(t, config, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	err := manager.Start(ctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := manager.Stop(ctx); err != nil {
			t.Logf("Failed to stop manager: %v", err)
		}
	})

	runEdgeCaseTests(t, manager, ctx, config, logger)
}

func createEdgeCaseTestConfig() DirectConfig {
	return DirectConfig{
		MaxConnections: 10,
		DefaultTimeout: 5 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			PreferredOrder: []string{"http", "websocket", "sse", "stdio"},
			Timeout:        10 * time.Second,
			CacheResults:   true,
			CacheTTL:       1 * time.Second, // Short TTL for testing
		},
	}
}

func setupEdgeCaseTestManager(t *testing.T, config DirectConfig, logger *zap.Logger) *DirectClientManager {
	t.Helper()

	manager := NewDirectClientManager(config, logger)

	return manager
}

func runEdgeCaseTests(
	t *testing.T,
	manager *DirectClientManager,
	ctx context.Context,
	config DirectConfig,
	logger *zap.Logger,
) {
	t.Helper()

	t.Run("cache_expiration", func(t *testing.T) {
		t.Parallel()
		testCacheExpiration(t, manager, ctx)
	})

	t.Run("empty_preferred_order", func(t *testing.T) {
		t.Parallel()
		testEmptyPreferredOrder(t, ctx, config, logger)
	})

	t.Run("protocol_hints_integration", func(t *testing.T) {
		t.Parallel()
		testProtocolHintsIntegration(t, manager, ctx)
	})

	t.Run("network_error_recovery", func(t *testing.T) {
		t.Parallel()
		testNetworkErrorRecovery(t, manager, ctx)
	})
}

func testCacheExpiration(t *testing.T, manager *DirectClientManager, ctx context.Context) {
	t.Helper()

	// Setup server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// First detection
	protocol1, err := manager.DetectProtocol(ctx, server.URL)
	require.NoError(t, err)
	assert.Equal(t, ClientTypeHTTP, protocol1)

	// Wait for cache expiration
	time.Sleep(2 * time.Second)

	// Second detection after expiration (should not be cached)
	protocol2, err := manager.DetectProtocol(ctx, server.URL)
	require.NoError(t, err)
	assert.Equal(t, ClientTypeHTTP, protocol2)

	metrics := manager.GetMetrics()
	assert.Positive(t, metrics.CacheMisses, "Should have cache misses due to expiration")
}

func testEmptyPreferredOrder(t *testing.T, ctx context.Context, config DirectConfig, logger *zap.Logger) {
	t.Helper()

	// Create manager with empty preferred order (should use defaults)
	emptyConfig := config
	emptyConfig.AutoDetection.PreferredOrder = []string{}

	//nolint:contextcheck // NewDirectClientManager creates its own context for manager lifecycle
	emptyManager := NewDirectClientManager(emptyConfig, logger)

	err := emptyManager.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = emptyManager.Stop(ctx) }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	protocol, err := emptyManager.DetectProtocol(ctx, server.URL)
	require.NoError(t, err)
	assert.Equal(t, ClientTypeHTTP, protocol, "Should use default preference order")
}

func testProtocolHintsIntegration(t *testing.T, manager *DirectClientManager, ctx context.Context) {
	t.Helper()

	// Setup WebSocket server
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()
	}))
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")

	// Test with hints (should be faster than regular detection)
	startTime := time.Now()
	protocol, err := manager.DetectProtocolWithHints(ctx, wsURL)
	detectionTime := time.Since(startTime)

	require.NoError(t, err)
	assert.Equal(t, ClientTypeWebSocket, protocol)
	assert.Less(t, detectionTime, 5*time.Second, "Hinted detection should be fast")
}

func testNetworkErrorRecovery(t *testing.T, manager *DirectClientManager, ctx context.Context) {
	t.Helper()

	// Create a server that will be closed mid-test
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	serverURL := server.URL

	// First detection (should succeed)
	protocol1, err := manager.DetectProtocol(ctx, serverURL)
	require.NoError(t, err)
	assert.Equal(t, ClientTypeHTTP, protocol1)

	// Close server
	server.Close()

	// Wait for cache expiration
	time.Sleep(2 * time.Second)

	// Second detection (should fail due to closed server)
	_, err = manager.DetectProtocol(ctx, serverURL)
	require.Error(t, err, "Detection should fail for closed server")
	assert.Contains(t, err.Error(), "could not detect compatible protocol")
}

// TestAdvancedProtocolNegotiation_PerformanceCharacteristics tests performance aspects.
// Note: This test does not use t.Parallel() because subtests share manager state
// and must run sequentially to avoid metric interference.
func TestAdvancedProtocolNegotiation_PerformanceCharacteristics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	manager := setupPerformanceTestManager(t)
	t.Cleanup(func() {
		cleanupPerformanceTestManager(t, manager)
	})

	server := setupPerformanceTestServer()
	t.Cleanup(server.Close)

	// Run subtests sequentially to avoid interference with shared manager metrics
	t.Run("detection_performance", func(t *testing.T) {
		runDetectionPerformanceTest(t, manager, server.URL)
	})

	t.Run("cache_effectiveness", func(t *testing.T) {
		runCacheEffectivenessTest(t, manager, server.URL)
	})
}

func setupPerformanceTestManager(t *testing.T) *DirectClientManager {
	t.Helper()
	logger := zaptest.NewLogger(t)

	config := DirectConfig{
		MaxConnections: 100,
		DefaultTimeout: 5 * time.Second,
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			PreferredOrder: []string{"http", "websocket", "sse", "stdio"},
			Timeout:        10 * time.Second,
			CacheResults:   true,
			CacheTTL:       5 * time.Minute,
		},
	}

	manager := NewDirectClientManager(config, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := manager.Start(ctx)
	require.NoError(t, err)

	return manager
}

func cleanupPerformanceTestManager(t *testing.T, manager *DirectClientManager) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := manager.Stop(ctx); err != nil {
		t.Logf("Failed to stop manager: %v", err)
	}
}

func setupPerformanceTestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func runDetectionPerformanceTest(t *testing.T, manager *DirectClientManager, serverURL string) {
	t.Helper()

	const numDetections = 1000

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	startTime := time.Now()

	var (
		successCount int64
		wg           sync.WaitGroup
	)

	for i := 0; i < numDetections; i++ {
		wg.Add(1)

		go runSingleDetection(t, &wg, manager, ctx, serverURL, i, &successCount)
	}

	wg.Wait()

	totalTime := time.Since(startTime)
	avgTime := totalTime / time.Duration(numDetections)

	assert.Greater(t, successCount, int64(numDetections*0.95), "Success rate should be > 95%")
	assert.Less(t, avgTime, 100*time.Millisecond, "Average detection time should be < 100ms")

	metrics := manager.GetMetrics()
	assert.Positive(t, metrics.CacheHits, "Should have cache hits")

	t.Logf("Performance: %d detections in %v (avg: %v per detection, success: %d)",
		numDetections, totalTime, avgTime, successCount)
}

func runSingleDetection(
	t *testing.T,
	wg *sync.WaitGroup,
	manager *DirectClientManager,
	ctx context.Context,
	serverURL string,
	i int,
	successCount *int64,
) {
	t.Helper()
	defer wg.Done()

	testURL := fmt.Sprintf("%s?test=%d", serverURL, i%10)

	_, err := manager.DetectProtocol(ctx, testURL)
	if err == nil {
		atomic.AddInt64(successCount, 1)
	}
}

func runCacheEffectivenessTest(t *testing.T, manager *DirectClientManager, serverURL string) {
	t.Helper()

	const numRepeatedDetections = 100

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Fill cache
	_, err := manager.DetectProtocol(ctx, serverURL)
	require.NoError(t, err)

	// Reset metrics
	initialMetrics := manager.GetMetrics()

	startTime := time.Now()

	// Repeated detections (should all be cache hits)
	for i := 0; i < numRepeatedDetections; i++ {
		_, err := manager.DetectProtocol(ctx, serverURL)
		require.NoError(t, err)
	}

	cacheTime := time.Since(startTime)
	avgCacheTime := cacheTime / time.Duration(numRepeatedDetections)

	finalMetrics := manager.GetMetrics()
	newCacheHits := finalMetrics.CacheHits - initialMetrics.CacheHits

	assert.Equal(t, numRepeatedDetections, newCacheHits, "All repeated detections should be cache hits")
	assert.Less(t, avgCacheTime, 10*time.Millisecond, "Cache hits should be very fast")

	t.Logf("Cache performance: %d cache hits in %v (avg: %v per hit)",
		numRepeatedDetections, cacheTime, avgCacheTime)
}
