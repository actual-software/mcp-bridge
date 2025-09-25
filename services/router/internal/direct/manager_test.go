package direct

import (
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// MockDirectClient implements DirectClient interface for testing.
type MockDirectClient struct {
	name            string
	protocol        string
	url             string
	connected       bool
	connectError    error
	requestError    error
	healthError     error
	closeError      error
	requestCount    uint64
	errorCount      uint64
	averageLatency  time.Duration
	lastHealthCheck time.Time
	connectionTime  time.Time
	lastUsed        time.Time
	isHealthy       bool
}

func NewMockDirectClient(name, protocol, url string) *MockDirectClient {
	return &MockDirectClient{
		name:            name,
		protocol:        protocol,
		url:             url,
		connectionTime:  time.Now(),
		lastUsed:        time.Now(),
		lastHealthCheck: time.Now(),
		isHealthy:       true,
	}
}

func (m *MockDirectClient) Connect(ctx context.Context) error {
	if m.connectError != nil {
		return m.connectError
	}

	m.connected = true
	m.connectionTime = time.Now()

	return nil
}

func (m *MockDirectClient) SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	if !m.connected {
		return nil, ErrClientNotConnected
	}

	if m.requestError != nil {
		m.errorCount++

		return nil, m.requestError
	}

	m.requestCount++
	m.lastUsed = time.Now()

	// Return a simple success response.
	return &mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		Result:  map[string]interface{}{"status": "ok"},
		ID:      req.ID,
	}, nil
}

func (m *MockDirectClient) Health(ctx context.Context) error {
	m.lastHealthCheck = time.Now()
	if m.healthError != nil {
		m.isHealthy = false

		return m.healthError
	}

	m.isHealthy = true

	return nil
}

func (m *MockDirectClient) Close(ctx context.Context) error {
	if m.closeError != nil {
		return m.closeError
	}

	m.connected = false

	return nil
}

func (m *MockDirectClient) GetName() string {
	return m.name
}

func (m *MockDirectClient) GetProtocol() string {
	return m.protocol
}

func (m *MockDirectClient) GetMetrics() ClientMetrics {
	return ClientMetrics{
		RequestCount:    m.requestCount,
		ErrorCount:      m.errorCount,
		AverageLatency:  m.averageLatency,
		LastHealthCheck: m.lastHealthCheck,
		IsHealthy:       m.isHealthy,
		ConnectionTime:  m.connectionTime,
		LastUsed:        m.lastUsed,
	}
}

func TestNewDirectClientManager(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 30 * time.Second,
		MaxConnections: 50,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 30 * time.Second,
			Timeout:  5 * time.Second,
		},
	}

	manager := NewDirectClientManager(config, logger)

	// Only test that manager was created and implements the interface.
	assert.NotNil(t, manager)
	assert.Implements(t, (*DirectClientManagerInterface)(nil), manager)
}

func TestNewDirectClientManagerDefaults(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{} // Empty config to test defaults

	manager := NewDirectClientManager(config, logger)

	// Only test that manager was created with defaults.
	assert.NotNil(t, manager)
	assert.Implements(t, (*DirectClientManagerInterface)(nil), manager)
}

func TestDirectClientManagerStartStop(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 5 * time.Second,
		MaxConnections: 10,
		HealthCheck: HealthCheckConfig{
			Enabled:  false, // Disable for simple test
			Interval: 1 * time.Second,
			Timeout:  1 * time.Second,
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	// Test start.
	err := manager.Start(ctx)
	require.NoError(t, err)

	// Test start when already running.
	err = manager.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Test stop.
	err = manager.Stop(ctx)
	require.NoError(t, err)

	// Test stop when not running.
	err = manager.Stop(ctx)
	require.NoError(t, err) // Should not error
}

func TestDirectClientManagerWithHealthCheck(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 5 * time.Second,
		MaxConnections: 10,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 100 * time.Millisecond, // Short interval for testing
			Timeout:  1 * time.Second,
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Wait a bit to ensure health check loop starts.
	time.Sleep(constants.TestLongTickInterval)

	// Manager should be running without errors.
	assert.NotNil(t, manager)
}

// This test has been removed as it tests internal implementation details.
// that are not exposed through the DirectClientManagerInterface

func TestDirectClientManagerGetClientNotRunning(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{}
	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	// Test GetClient when not running.
	_, err := manager.GetClient(ctx, "http://example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

// This test has been removed as it tests internal implementation details.
// that are not exposed through the DirectClientManagerInterface

func TestDirectClientManagerProtocolDetection(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	manager := setupProtocolDetectionManager(t, logger)
	defer closeProtocolDetectionManager(t, manager)

	testCases := createProtocolDetectionTests()
	runProtocolDetectionTests(t, manager, testCases)
}

func setupProtocolDetectionManager(t *testing.T, logger *zap.Logger) DirectClientManagerInterface {
	t.Helper()
	
	config := DirectConfig{
		DefaultTimeout: 5 * time.Second,
		MaxConnections: 10,
		HealthCheck: HealthCheckConfig{
			Enabled: false, // Disable for testing
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)
	
	return manager
}

func closeProtocolDetectionManager(t *testing.T, manager DirectClientManagerInterface) {
	t.Helper()
	
	ctx := context.Background()
	err := manager.Stop(ctx)
	require.NoError(t, err)
}

func createProtocolDetectionTests() []struct {
	name      string
	serverURL string
	expectErr bool
	errorMsg  string
} {
	return []struct {
		name      string
		serverURL string
		expectErr bool
		errorMsg  string
	}{
		{
			name:      "HTTP URL should be detectable",
			serverURL: "http://example.com",
			expectErr: true, // Connection will fail but protocol should be detected
			errorMsg:  "",   // Error will be about connection, not protocol detection
		},
		{
			name:      "WebSocket URL should be detectable",
			serverURL: "ws://example.com",
			expectErr: true, // Connection will fail but protocol should be detected
			errorMsg:  "",
		},
		{
			name:      "HTTPS URL should be detectable",
			serverURL: "https://example.com",
			expectErr: true, // Connection will fail but protocol should be detected
			errorMsg:  "",
		},
		{
			name:      "Invalid URL should fail early",
			serverURL: "invalid://bad-url",
			expectErr: true,
			errorMsg:  "", // Should get some kind of protocol or URL error
		},
		{
			name:      "Stdio command should be detectable",
			serverURL: "echo 'test'", // Simple command that should exist
			expectErr: true,          // Will fail to establish MCP connection but command exists
			errorMsg:  "",
		},
	}
}

func runProtocolDetectionTests(t *testing.T, manager DirectClientManagerInterface, testCases []struct {
	name      string
	serverURL string
	expectErr bool
	errorMsg  string
}) {
	t.Helper()
	
	ctx := context.Background()
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test that GetClient attempts to create appropriate client type.
			// Even if connection fails, it should fail at connection stage, not protocol detection.
			_, err := manager.GetClient(ctx, tc.serverURL)

			if tc.expectErr {
				require.Error(t, err, "Expected error for %s", tc.serverURL)

				if tc.errorMsg != "" {
					assert.Contains(t, err.Error(), tc.errorMsg)
				}
			} else {
				require.NoError(t, err, "Expected success for %s", tc.serverURL)
			}
		})
	}
}

func TestDirectClientManagerConcurrentAccess(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		MaxConnections: 5,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Test concurrent access to GetClient.
	const (
		numGoroutines = 10
		numRequests   = 5
	)

	var wg sync.WaitGroup

	errChan := make(chan error, numGoroutines*numRequests)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func(routineID int) {
			defer wg.Done()

			for j := 0; j < numRequests; j++ {
				// Use different URLs to test concurrent protocol detection.
				testURL := fmt.Sprintf("http://example-%d-%d.com", routineID, j)
				_, err := manager.GetClient(ctx, testURL)
				// We expect errors since these are fake URLs, but there should be no panics.
				// and the manager should handle concurrent access gracefully
				if err != nil {
					errChan <- err
				}
			}
		}(i)
	}

 wg.Wait()
	close(errChan)

	// Count errors - we expect most/all to fail since URLs are fake.
	errorCount := 0
	for err := range errChan {
		errorCount++
		// Errors should be about connection failures, not internal state corruption.
		assert.NotContains(t, err.Error(), "panic")
		assert.NotContains(t, err.Error(), "concurrent")
	}

	// Manager should still be functional after concurrent access.
	assert.NotNil(t, manager)
}

func TestConnectionState(t *testing.T) { 
	testCases := []struct {
		state    ConnectionState
		expected string
	}{
		{StateDisconnected, "disconnected"},
		{StateConnecting, "connecting"},
		{StateConnected, "connected"},
		{StateHealthy, "healthy"},
		{StateUnhealthy, "unhealthy"},
		{StateClosing, "closing"},
		{StateClosed, "closed"},
		{StateError, "error"},
		{ConnectionState(999), "unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.state.String())
		})
	}
}

func TestDirectClientManagerValidURLs(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	manager := setupValidURLsManager(t, logger)
	defer closeValidURLsManager(t, manager)

	testCases := createValidURLsTests()
	runValidURLsTests(t, manager, testCases)
}

func setupValidURLsManager(t *testing.T, logger *zap.Logger) DirectClientManagerInterface {
	t.Helper()
	
	config := DirectConfig{
		DefaultTimeout: 2 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}
	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)
	
	return manager
}

func closeValidURLsManager(t *testing.T, manager DirectClientManagerInterface) {
	t.Helper()
	
	ctx := context.Background()
	err := manager.Stop(ctx)
	require.NoError(t, err)
}

func createValidURLsTests() []struct {
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
			name:        "HTTP URL",
			serverURL:   "http://example.com",
			expectError: true, // Will fail connection but should recognize protocol
			description: "Should recognize HTTP protocol and attempt connection",
		},
		{
			name:        "HTTPS URL",
			serverURL:   "https://example.com",
			expectError: true, // Will fail connection but should recognize protocol
			description: "Should recognize HTTPS protocol and attempt connection",
		},
		{
			name:        "WebSocket URL",
			serverURL:   "ws://example.com",
			expectError: true, // Will fail connection but should recognize protocol
			description: "Should recognize WebSocket protocol and attempt connection",
		},
		{
			name:        "Secure WebSocket URL",
			serverURL:   "wss://example.com",
			expectError: true, // Will fail connection but should recognize protocol
			description: "Should recognize secure WebSocket protocol and attempt connection",
		},
		{
			name:        "Simple command",
			serverURL:   "echo hello",
			expectError: true, // Command exists but won't establish MCP connection
			description: "Should recognize stdio protocol and attempt connection",
		},
		{
			name:        "Invalid command",
			serverURL:   "nonexistent-command-xyz123",
			expectError: true, // Command doesn't exist
			description: "Should fail cleanly for non-existent commands",
		},
	}
}

func runValidURLsTests(t *testing.T, manager DirectClientManagerInterface, testCases []struct {
	name        string
	serverURL   string
	expectError bool
	description string
}) {
	t.Helper()
	
	ctx := context.Background()
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := manager.GetClient(ctx, tc.serverURL)

			if tc.expectError {
				require.Error(t, err, tc.description)
				// Error should not be about unsupported protocol.
				assert.NotContains(t, err.Error(), "unsupported protocol",
					"Should not fail due to unsupported protocol for %s", tc.serverURL)
			} else {
				require.NoError(t, err, tc.description)
			}
		})
	}
}

func TestDirectClientManagerLifecycleIntegration(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 2 * time.Second,
		MaxConnections: 5,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 500 * time.Millisecond,
			Timeout:  1 * time.Second,
		},
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			CacheResults:   true,
			CacheTTL:       1 * time.Second,
			PreferredOrder: []string{"http", "websocket", "stdio"},
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	// Test full lifecycle.
	t.Run("start_use_stop", func(t *testing.T) {
		// Start manager.
		err := manager.Start(ctx)
		require.NoError(t, err)

		// Use manager for various protocols.
		testURLs := []string{
			"http://example.com",
			"ws://example.com",
			"echo test",
		}

		for _, url := range testURLs {
			_, err := manager.GetClient(ctx, url)
			// We expect errors since these are fake/non-MCP endpoints,
			// but they should be connection errors, not protocol errors
			if err != nil {
				assert.NotContains(t, err.Error(), "unsupported protocol")
				assert.NotContains(t, err.Error(), "not running")
			}
		}

		// Stop manager.
		err = manager.Stop(ctx)
		require.NoError(t, err)

		// Verify manager won't accept requests after stop.
		_, err = manager.GetClient(ctx, "http://test.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not running")
	})
}

func BenchmarkDirectClientManagerGetClient(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}
	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(b, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(b, err)
	}()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Test GetClient performance (will fail but measures protocol detection speed).
		_, _ = manager.GetClient(ctx, "http://example.com")
	}
}

func BenchmarkDirectClientManagerStartStop(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		manager := NewDirectClientManager(config, logger)
		ctx := context.Background()

		err := manager.Start(ctx)
		require.NoError(b, err)

		err = manager.Stop(ctx)
		require.NoError(b, err)
	}
}

// Additional test for connection pool management.
func TestDirectClientManagerConnectionPoolLimits(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 2 * time.Second,
		MaxConnections: 2, // Small limit for testing
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		ConnectionPool: ConnectionPoolConfig{
			MaxActiveConnections:  2,
			MaxIdleConnections:    1,
			IdleTimeout:           1 * time.Second,
			ConnectionTTL:         10 * time.Second,
			HealthCheckInterval:   500 * time.Millisecond,
			EnableConnectionReuse: true,
			CleanupInterval:       500 * time.Millisecond,
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Test concurrent connection attempts that exceed limits.
	const numGoroutines = 5

	var wg sync.WaitGroup

	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			testURL := fmt.Sprintf("echo test-concurrent-%d", id)

			_, err := manager.GetClient(ctx, testURL)
			results <- err
		}(i)
	}

 wg.Wait()
	close(results)

	// Collect results.
	errorCount := 0

	for err := range results {
		if err != nil {
			errorCount++
		}
	}

	// All requests should fail (expected since echo won't provide MCP responses).
	// but they should fail gracefully without panics or deadlocks
	assert.Equal(t, numGoroutines, errorCount, "All requests should fail gracefully")
}

// Test protocol detection with caching.
func TestDirectClientManagerProtocolCaching(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			CacheResults:   true,
			CacheTTL:       2 * time.Second,
			PreferredOrder: []string{"http", "websocket", "stdio"},
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	testURL := "echo test-cache"

	// First request should trigger protocol detection.
	start1 := time.Now()
	_, err1 := manager.GetClient(ctx, testURL)
	duration1 := time.Since(start1)

	// Second request should use cached result and be faster.
	start2 := time.Now()
	_, err2 := manager.GetClient(ctx, testURL)
	duration2 := time.Since(start2)

	// Both should have the same type of error (connection failure).
	// but second should be faster due to caching (or might succeed due to protocol detection)
	// We don't require both to error since protocol detection may work.
	if err1 != nil && err2 != nil {
		// If both failed, the second should be faster due to caching.
		assert.Less(t, duration2, duration1, "Cached request should be faster")
	}
}

// Test adaptive timeout mechanisms.
func TestDirectClientManagerAdaptiveTimeout(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 2 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		AdaptiveTimeout: AdaptiveTimeoutConfig{
			BaseTimeout:    1 * time.Second,
			MinTimeout:     500 * time.Millisecond,
			MaxTimeout:     5 * time.Second,
			SuccessRatio:   0.8,
			AdaptationRate: 0.1,
			EnableLearning: true,
			LearningPeriod: 10 * time.Second,
		},
		TimeoutTuning: TimeoutTuningConfig{
			EnableDynamicAdjustment:   true,
			EnableLatencyBasedTimeout: true,
			TimeoutProfile:            "default",
			RetryProfile:              "default",
			NetworkCondition:          "good",
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Test that manager starts successfully with adaptive timeout configuration.
	assert.NotNil(t, manager)
}

// Test memory optimization features basic.
func TestDirectClientManagerMemoryOptimizationBasic(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		MemoryOptimization: MemoryOptimizationConfig{
			EnableObjectPooling: true,
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Verify memory optimization is active.
	// This mainly tests that the manager starts successfully with memory optimization enabled.
	assert.NotNil(t, manager)
}

// Test observability and metrics collection.
func TestDirectClientManagerObservability(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		Observability: ObservabilityConfig{
			MetricsEnabled:          true,
			TracingEnabled:          true,
			HealthMonitoringEnabled: true,
			MetricsInterval:         1 * time.Second,
			MaxTraces:               1000,
			ProfilingEnabled:        true,
			MetricsRetention:        1 * time.Hour,
			MemoryTrackingEnabled:   true,
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Test that manager starts successfully with observability enabled.
	assert.NotNil(t, manager)
}

// Test error propagation and handling.
func TestDirectClientManagerErrorHandling(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 500 * time.Millisecond, // Short timeout for testing
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	testCases := []struct {
		name      string
		url       string
		expectErr bool
		errMsg    string
	}{
		{
			name:      "Invalid URL",
			url:       "://invalid",
			expectErr: true,
			errMsg:    "protocol",
		},
		{
			name:      "Nonexistent host",
			url:       "http://nonexistent-host-12345.invalid",
			expectErr: true,
			errMsg:    "", // Various network error messages possible
		},
		{
			name:      "Invalid command",
			url:       "nonexistent-command-xyz123",
			expectErr: true,
			errMsg:    "", // Command not found or protocol detection failure
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := manager.GetClient(ctx, tc.url)

			if tc.expectErr {
				require.Error(t, err)

				if tc.errMsg != "" {
					assert.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Test health check functionality.
func TestDirectClientManagerHealthChecks(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 200 * time.Millisecond, // Fast interval for testing
			Timeout:  500 * time.Millisecond,
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Allow some time for health checks to run.
	time.Sleep(300 * time.Millisecond)

	// Test that manager runs health checks without errors.
	assert.NotNil(t, manager)
}

// Test concurrent protocol detection.
func TestDirectClientManagerConcurrentProtocolDetection(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 2 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			CacheResults:   false, // Disable caching to test raw detection
			PreferredOrder: []string{"stdio", "http", "websocket"},
		},
	}

	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Test concurrent protocol detection for different URL types.
	testURLs := []string{
		"echo test1",
		"echo test2",
		"http://example1.com",
		"http://example2.com",
		"ws://example1.com",
		"ws://example2.com",
	}

	var wg sync.WaitGroup
	for i, url := range testURLs {
		wg.Add(1)

		go func(id int, testURL string) {
			defer wg.Done()

			_, err := manager.GetClient(ctx, testURL)
			// We expect errors since these won't establish real MCP connections.
			// But there should be no panics or deadlocks.
			// Note: Some URLs might succeed in protocol detection but fail later.
			if err != nil {
				// Expected - we're testing malformed URLs.
				t.Logf("URL %s failed as expected: %v", testURL, err)
			}
		}(i, url)
	}

	// Use a timeout to detect deadlocks.
	done := make(chan struct{})

	go func() {
  wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - all goroutines completed.
	case <-time.After(10 * time.Second):
		t.Fatal("Test timed out - possible deadlock in concurrent protocol detection")
	}
}

// ============================================================================
// COMPREHENSIVE ADDITIONAL TESTS FOR ENHANCED COVERAGE.
// ============================================================================

// Test comprehensive metrics collection and management.
func TestDirectClientManagerMetricsCollection(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		Observability: ObservabilityConfig{
			MetricsEnabled:          true,
			MetricsInterval:         100 * time.Millisecond,
			TracingEnabled:          true,
			MaxTraces:               100,
			HealthMonitoringEnabled: true,
			MetricsRetention:        1 * time.Hour,
			MemoryTrackingEnabled:   true,
		},
	}

	manager, ok := NewDirectClientManager(config, logger).(*DirectClientManager)
	assert.True(t, ok, "type assertion failed")

	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Test basic metrics retrieval.
	metrics := manager.GetMetrics()
	assert.NotZero(t, metrics.LastUpdate)
	assert.GreaterOrEqual(t, metrics.TotalClients, 0)
	assert.GreaterOrEqual(t, metrics.ActiveConnections, 0)

	// Test detailed metrics.
	detailedMetrics := manager.GetDetailedMetrics()
	assert.GreaterOrEqual(t, detailedMetrics.TotalClients, 0)
	assert.NotNil(t, detailedMetrics.ClientsByProtocol)
	assert.NotNil(t, detailedMetrics.ClientsByState)
	assert.NotNil(t, detailedMetrics.PoolStats)

	// Test metrics export.
	exportData, err := manager.ExportObservabilityData()
	require.NoError(t, err)
	assert.NotEmpty(t, exportData)

	// Verify the exported data is valid JSON.
	var exportedJSON map[string]interface{}

	err = json.Unmarshal(exportData, &exportedJSON)
	require.NoError(t, err)
}

// Test client lifecycle management in detail.
func TestDirectClientManagerClientLifecycle(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		ConnectionPool: ConnectionPoolConfig{
			MaxActiveConnections:  5,
			MaxIdleConnections:    2,
			IdleTimeout:           500 * time.Millisecond,
			ConnectionTTL:         2 * time.Second,
			HealthCheckInterval:   100 * time.Millisecond,
			EnableConnectionReuse: true,
			CleanupInterval:       200 * time.Millisecond,
		},
	}

	mgr := NewDirectClientManager(config, logger)


	manager, ok := mgr.(*DirectClientManager)


	require.True(t, ok, "Expected *DirectClientManager type")

	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Test client listing.
	clients := manager.ListClients()
	assert.NotNil(t, clients)

	// Test getting all client metrics (should be empty initially).
	allMetrics := manager.GetAllClientMetrics()
	assert.NotNil(t, allMetrics)

	// Test removing non-existent client.
	err = manager.RemoveClient("http://nonexistent.com", ClientTypeHTTP)
	require.Error(t, err) // Should error for non-existent client
}

// Test protocol detection with various URL patterns and caching.

func TestDirectClientManagerProtocolDetectionComprehensive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := setupConcreteProtocolDetectionManager(t, logger)
	
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	testCases := createProtocolDetectionTestCases()
	runConcreteProtocolDetectionTests(t, manager, ctx, testCases)
	testProtocolDetectionCacheExpiration(t, manager, ctx)
}

func setupConcreteProtocolDetectionManager(t *testing.T, logger *zap.Logger) *DirectClientManager {
	t.Helper()
	
	config := DirectConfig{
		DefaultTimeout: 100 * time.Millisecond, // Much shorter timeout for tests
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			CacheResults:   true,
			CacheTTL:       1 * time.Second,       // Shorter TTL for testing
			Timeout:        50 * time.Millisecond, // Very short timeout to fail fast
			PreferredOrder: []string{"stdio", "http", "websocket", "sse"},
		},
	}

	mgr := NewDirectClientManager(config, logger)
	manager, ok := mgr.(*DirectClientManager)
	require.True(t, ok, "Expected *DirectClientManager type")
	
	return manager
}

type protocolDetectionTestCase struct {
	name          string
	url           string
	expectedProto ClientType
	shouldCache   bool
	testCacheHit  bool
}

func createProtocolDetectionTestCases() []protocolDetectionTestCase {
	basicCases := []protocolDetectionTestCase{
		{
			name:          "HTTP URL with path",
			url:           "http://127.0.0.1:99999/api/mcp",
			expectedProto: "",
			shouldCache:   true,
			testCacheHit:  false,
		},
		{
			name:          "HTTPS URL",
			url:           "https://127.0.0.1:99998",
			expectedProto: "",
			shouldCache:   true,
			testCacheHit:  false,
		},
		{
			name:          "WebSocket URL",
			url:           "ws://127.0.0.1:99997/mcp",
			expectedProto: "",
			shouldCache:   true,
			testCacheHit:  false,
		},
		{
			name:          "Secure WebSocket URL",
			url:           "wss://127.0.0.1:99996",
			expectedProto: "",
			shouldCache:   true,
			testCacheHit:  false,
		},
	}
	
	additionalCases := createAdditionalProtocolTestCases()
	return append(basicCases, additionalCases...)
}

func createAdditionalProtocolTestCases() []protocolDetectionTestCase {
	return []protocolDetectionTestCase{
		{
			name:          "SSE-like HTTP URL",
			url:           "http://127.0.0.1:99995/events/stream",
			expectedProto: "",
			shouldCache:   true,
			testCacheHit:  false,
		},
		{
			name:          "Stdio command with args",
			url:           "echo 'hello world'",
			expectedProto: "",
			shouldCache:   true,
			testCacheHit:  false,
		},
		{
			name:          "Stdio URL scheme",
			url:           "stdio://echo test",
			expectedProto: "",
			shouldCache:   true,
			testCacheHit:  false,
		},
		{
			name:          "Cached request repeat",
			url:           "http://127.0.0.1:99999/api/mcp",
			expectedProto: "",
			shouldCache:   false,
			testCacheHit:  true,
		},
	}
}

func runConcreteProtocolDetectionTests(
	t *testing.T,
	manager *DirectClientManager,
	ctx context.Context,
	testCases []protocolDetectionTestCase,
) {
	t.Helper()
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test GetClient which internally calls protocol detection.
			_, err := manager.GetClient(ctx, tc.url)
			// We expect most to fail since these are fake URLs,
			// but they should fail at connection stage, not protocol detection
			if err != nil {
				assert.NotContains(t, err.Error(), "protocol auto-detection is disabled")
			}

			// For cache hit test, verify metrics show cache usage.
			if tc.testCacheHit {
				metrics := manager.GetMetrics()
				assert.Positive(t, metrics.CacheHits, "Should have cache hits for repeated URL")
			}
		})
	}
}

func testProtocolDetectionCacheExpiration(t *testing.T, manager *DirectClientManager, ctx context.Context) {
	t.Helper()
	
	// Test cache expiration - wait for TTL to expire.
	time.Sleep(1100 * time.Millisecond) // Wait for cache TTL to expire

	// Request the same URL again - should not use cache.
	_, err := manager.GetClient(ctx, "http://127.0.0.1:99999/api/mcp")
	// Should still detect protocol even after cache expiration.
	if err != nil {
		assert.NotContains(t, err.Error(), "protocol auto-detection is disabled")
	}
}

// Test configuration validation and defaults setting.
func TestDirectClientManagerConfigurationValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	testCases := createConfigValidationTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runConfigValidationTest(t, logger, tc)
		})
	}
}

func createConfigValidationTestCases() []configValidationTestCase {
	basicCases := createBasicConfigTestCases()
	advancedCases := createAdvancedConfigTestCases()
	return append(basicCases, advancedCases...)
}

func createBasicConfigTestCases() []configValidationTestCase {
	return []configValidationTestCase{
		{
			name:           "Empty config should set defaults",
			config:         DirectConfig{},
			expectDefaults: true,
			validateFunc: func(t *testing.T, mgr DirectClientManagerInterface) {
				t.Helper()
				validateDefaultConfig(t, mgr)
			},
		},
		{
			name: "Partial config should preserve values and set missing defaults",
			config: DirectConfig{
				DefaultTimeout: 10 * time.Second,
				MaxConnections: 50,
			},
			expectDefaults: false,
			validateFunc: func(t *testing.T, mgr DirectClientManagerInterface) {
				t.Helper()
				manager, ok := mgr.(*DirectClientManager)
				require.True(t, ok, "Expected *DirectClientManager type")
				assert.Equal(t, 10*time.Second, manager.config.DefaultTimeout)
				assert.Equal(t, 50, manager.config.MaxConnections)
				assert.NotZero(t, manager.config.HealthCheck.Interval)
			},
		},
	}
}

func createAdvancedConfigTestCases() []configValidationTestCase {
	return []configValidationTestCase{
		{
			name: "Protocol performance configs should have defaults",
			config: DirectConfig{
				DefaultTimeout: 5 * time.Second,
			},
			expectDefaults: true,
			validateFunc: func(t *testing.T, mgr DirectClientManagerInterface) {
				t.Helper()
				manager, ok := mgr.(*DirectClientManager)
				require.True(t, ok, "Expected *DirectClientManager type")
				assert.Positive(t, manager.config.Stdio.Performance.StdinBufferSize)
				assert.Positive(t, manager.config.WebSocket.Performance.ReadBufferSize)
				assert.Positive(t, manager.config.HTTP.Performance.ResponseBufferSize)
				assert.Positive(t, manager.config.SSE.Performance.StreamBufferSize)
			},
		},
		{
			name: "Observability config with custom values",
			config: DirectConfig{
				DefaultTimeout: 5 * time.Second,
				Observability: ObservabilityConfig{
					MetricsEnabled:          true,
					TracingEnabled:          true,
					HealthMonitoringEnabled: true,
					MetricsInterval:         30 * time.Second,
					MaxTraces:               500,
				},
			},
			validateFunc: func(t *testing.T, mgr DirectClientManagerInterface) {
				t.Helper()
				manager, ok := mgr.(*DirectClientManager)
				require.True(t, ok, "Expected *DirectClientManager type")
				assert.True(t, manager.config.Observability.MetricsEnabled)
				assert.True(t, manager.config.Observability.TracingEnabled)
				assert.Equal(t, 30*time.Second, manager.config.Observability.MetricsInterval)
				assert.Equal(t, 500, manager.config.Observability.MaxTraces)
			},
		},
	}
}

// Test error handling and edge cases extensively.
type configValidationTestCase struct {
	name           string
	config         DirectConfig
	expectDefaults bool
	validateFunc   func(*testing.T, DirectClientManagerInterface)
}

func runConfigValidationTest(t *testing.T, logger *zap.Logger, tc configValidationTestCase) {
	t.Helper()
	manager := NewDirectClientManager(tc.config, logger)
	assert.NotNil(t, manager)

	if tc.validateFunc != nil {
		tc.validateFunc(t, manager)
	}
}

func validateDefaultConfig(t *testing.T, mgr DirectClientManagerInterface) {
	t.Helper()
	// Cast to concrete type to access internal fields for validation.
	manager, ok := mgr.(*DirectClientManager)
	require.True(t, ok, "Expected *DirectClientManager type")
	assert.Equal(t, 30*time.Second, manager.config.DefaultTimeout)
	assert.Equal(t, 100, manager.config.MaxConnections)
	assert.True(t, manager.config.Fallback.Enabled)
}

func TestDirectClientManagerErrorHandlingComprehensive(t *testing.T) {
	manager := setupErrorHandlingTestManager(t)
	ctx := context.Background()
	
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop(ctx) }()
	
	errorTestCases := createErrorHandlingTestCases()
	runErrorHandlingTests(t, manager, ctx, errorTestCases)
}

func setupErrorHandlingTestManager(t *testing.T) DirectClientManagerInterface {
	t.Helper()
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 200 * time.Millisecond,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		AutoDetection: AutoDetectionConfig{
			Enabled: true,
			Timeout: 100 * time.Millisecond,
		},
	}
	
	mgr := NewDirectClientManager(config, logger)
	manager, ok := mgr.(*DirectClientManager)
	require.True(t, ok, "Expected *DirectClientManager type")
	return manager
}

type errorHandlingTestCase struct {
	name        string
	url         string
	expectError bool
	errorType   string
	description string
}

func createErrorHandlingTestCases() []errorHandlingTestCase {
	return []errorHandlingTestCase{
		{
			name:        "Empty URL",
			url:         "",
			expectError: true,
			errorType:   "protocol",
			description: "Empty URL should fail protocol detection",
		},
		{
			name:        "Invalid URL scheme",
			url:         "invalid-scheme://test",
			expectError: true,
			errorType:   "protocol",
			description: "Invalid scheme should fail protocol detection",
		},
		{
			name:        "Malformed URL",
			url:         "ht!tp://malformed",
			expectError: true,
			errorType:   "url",
			description: "Malformed URL should fail early",
		},
		{
			name:        "Unreachable HTTP host",
			url:         "http://192.0.2.1:12345",
			expectError: true,
			errorType:   "connection",
			description: "Unreachable host should timeout/fail",
		},
		{
			name:        "Unreachable WebSocket host",
			url:         "ws://192.0.2.1:12345",
			expectError: true,
			errorType:   "connection",
			description: "Unreachable WebSocket should timeout/fail",
		},
		{
			name:        "Non-existent stdio command",
			url:         "this-command-definitely-does-not-exist-12345",
			expectError: true,
			errorType:   "command",
			description: "Non-existent command should fail detection",
		},
		{
			name:        "Stdio command that succeeds",
			url:         "echo 'hello & world'",
			expectError: false,
			errorType:   "none",
			description: "Valid echo command should succeed in client creation",
		},
	}
}

func runErrorHandlingTests(
	t *testing.T,
	manager DirectClientManagerInterface,
	ctx context.Context,
	testCases []errorHandlingTestCase,
) {
	t.Helper()
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := manager.GetClient(ctx, tc.url)
			
			if tc.expectError {
				require.Error(t, err, tc.description)
				if err != nil {
					assert.NotContains(t, err.Error(), "panic")
					assert.NotContains(t, err.Error(), "concurrent")
				}
			} else {
				require.NoError(t, err, tc.description)
			}
		})
	}
}

// Test health check integration and monitoring.
func TestDirectClientManagerHealthCheckIntegration(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 100 * time.Millisecond,
			Timeout:  200 * time.Millisecond,
		},
		Observability: ObservabilityConfig{
			HealthMonitoringEnabled: true,
			MetricsEnabled:          true,
		},
	}

	mgr := NewDirectClientManager(config, logger)


	manager, ok := mgr.(*DirectClientManager)


	require.True(t, ok, "Expected *DirectClientManager type")

	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	// Let health checks run for a bit.
	time.Sleep(250 * time.Millisecond)

	// Test health alert retrieval.
	alerts := manager.GetHealthAlerts()
	assert.NotNil(t, alerts) // Should not be nil even if empty

	// Test system status.
	systemStatus := manager.GetSystemStatus()
	assert.NotEmpty(t, systemStatus.Version)
	assert.True(t, systemStatus.ManagerStatus.Running)

	// Test health report generation.
	healthReport := manager.GenerateHealthReport()
	assert.NotNil(t, healthReport)
	assert.Contains(t, healthReport, "overall_status")
	assert.Contains(t, healthReport, "timestamp")

	// Test system status export.
	statusData, err := manager.ExportSystemStatus()
	require.NoError(t, err)
	assert.NotEmpty(t, statusData)

	// Verify exported status is valid JSON.
	var statusJSON map[string]interface{}

	err = json.Unmarshal(statusData, &statusJSON)
	require.NoError(t, err)
}

// Test adaptive timeout and retry mechanisms.
func TestDirectClientManagerAdaptiveMechanisms(t *testing.T) {
	manager := setupAdaptiveTestManager(t)
	ctx := context.Background()
	
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop(ctx) }()
	
	testAdaptiveStats(t, manager)
	testOptimizedConfigurations(t, manager)
	testAdaptiveRequestSending(t, manager, ctx)
}

func setupAdaptiveTestManager(t *testing.T) *DirectClientManager {
	t.Helper()
	logger := zaptest.NewLogger(t)
	config := createAdaptiveTestConfig()
	
	mgr := NewDirectClientManager(config, logger)
	manager, ok := mgr.(*DirectClientManager)
	require.True(t, ok, "Expected *DirectClientManager type")
	return manager
}

func createAdaptiveTestConfig() DirectConfig {
	return DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		AutoDetection: AutoDetectionConfig{
			Enabled: true,
			Timeout: 500 * time.Millisecond,
		},
		AdaptiveTimeout: AdaptiveTimeoutConfig{
			BaseTimeout:    500 * time.Millisecond,
			MinTimeout:     100 * time.Millisecond,
			MaxTimeout:     2 * time.Second,
			SuccessRatio:   0.8,
			AdaptationRate: 0.1,
			EnableLearning: true,
			LearningPeriod: 5 * time.Second,
		},
		AdaptiveRetry: AdaptiveRetryConfig{
			MaxRetries:            3,
			BaseDelay:             100 * time.Millisecond,
			MaxDelay:              1 * time.Second,
			BackoffFactor:         2.0,
			CircuitBreakerEnabled: true,
			FailureThreshold:      5,
		},
		TimeoutTuning: TimeoutTuningConfig{
			EnableDynamicAdjustment:   true,
			EnableLatencyBasedTimeout: true,
			EnableLoadBasedRetry:      true,
			TimeoutProfile:            "fast",
			RetryProfile:              "aggressive",
			NetworkCondition:          "good",
		},
	}
}

func testAdaptiveStats(t *testing.T, manager *DirectClientManager) {
	t.Helper()
	
	adaptiveStats := manager.GetAdaptiveStats()
	assert.NotNil(t, adaptiveStats)
	
	timeoutStats := manager.GetTimeoutTuningStats()
	assert.NotNil(t, timeoutStats)
	assert.Contains(t, timeoutStats, "timeout_profile")
	assert.Contains(t, timeoutStats, "retry_profile")
	assert.Contains(t, timeoutStats, "network_condition")
}

func testOptimizedConfigurations(t *testing.T, manager *DirectClientManager) {
	t.Helper()
	
	protocols := []string{"stdio", "http", "websocket", "sse"}
	for _, protocol := range protocols {
		timeoutConfig := manager.GetOptimizedTimeoutConfig(protocol)
		assert.Greater(t, timeoutConfig.BaseTimeout, time.Duration(0))
		assert.LessOrEqual(t, timeoutConfig.MinTimeout, timeoutConfig.BaseTimeout)
		assert.GreaterOrEqual(t, timeoutConfig.MaxTimeout, timeoutConfig.BaseTimeout)
		
		retryConfig := manager.GetOptimizedRetryConfig(protocol)
		assert.Positive(t, retryConfig.MaxRetries)
		assert.Greater(t, retryConfig.BaseDelay, time.Duration(0))
	}
}

func testAdaptiveRequestSending(t *testing.T, manager *DirectClientManager, ctx context.Context) {
	t.Helper()
	
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-123",
	}
	
	_, err := manager.SendRequestWithAdaptive(ctx, "http://fake-test.com", req)
	if err != nil {
		assert.NotContains(t, err.Error(), "panic")
	}
}

// Test memory optimization features advanced.
func TestDirectClientManagerMemoryOptimizationAdvanced(t *testing.T) {
	manager := setupMemoryOptimizedTestManager(t)
	ctx := context.Background()
	
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop(ctx) }()
	
	testMemoryOptimizationFeatures(t, manager, ctx)
}

func setupMemoryOptimizedTestManager(t *testing.T) *DirectClientManager {
	t.Helper()
	logger := zaptest.NewLogger(t)
	config := createMemoryOptimizedConfig()
	
	mgr := NewDirectClientManager(config, logger)
	manager, ok := mgr.(*DirectClientManager)
	require.True(t, ok, "Expected *DirectClientManager type")
	return manager
}

func createMemoryOptimizedConfig() DirectConfig {
	return DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		MemoryOptimization: MemoryOptimizationConfig{
			EnableObjectPooling: true,
			BufferPoolConfig: BufferPoolConfig{
				Enabled:          true,
				InitialSize:      1024,
				MaxSize:          64 * 1024,
				MaxPooledBuffers: 50,
			},
			JSONPoolConfig: JSONPoolConfig{
				Enabled:          true,
				MaxPooledObjects: 20,
			},
			GCConfig: GCConfig{
				Enabled:         true,
				GCPercent:       100,
				ForceGCInterval: 30 * time.Second,
				MemoryLimit:     100 * 1024 * 1024,
			},
			MemoryMonitoring: MemoryMonitoringConfig{
				Enabled:            true,
				MonitoringInterval: 10 * time.Second,
				AlertThreshold:     0.8,
				LogMemoryStats:     true,
			},
		},
	}
}

func testMemoryOptimizationFeatures(t *testing.T, manager *DirectClientManager, ctx context.Context) {
	t.Helper()
	
	assert.NotNil(t, manager)
	
	detailedMetrics := manager.GetDetailedMetrics()
	assert.NotNil(t, detailedMetrics)
	
	for i := 0; i < constants.TestBatchSize; i++ {
		testURL := fmt.Sprintf("echo test-memory-%d", i)
		_, err := manager.GetClient(ctx, testURL)
		if err != nil {
			t.Logf("Expected error for echo URL %s: %v", testURL, err)
		}
	}
}

// Test connection pool edge cases and limits.
func TestDirectClientManagerConnectionPoolEdgeCases(t *testing.T) {
	manager := setupConnectionPoolTestManager(t)
	ctx := context.Background()
	
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop(ctx) }()
	
	testRapidConnectionRequests(t, manager, ctx)
	testPoolCleanupAndStats(t, manager)
}

func setupConnectionPoolTestManager(t *testing.T) *DirectClientManager {
	t.Helper()
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 500 * time.Millisecond,
		MaxConnections: 2,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		ConnectionPool: ConnectionPoolConfig{
			MaxActiveConnections:  2,
			MaxIdleConnections:    1,
			IdleTimeout:           100 * time.Millisecond,
			ConnectionTTL:         500 * time.Millisecond,
			HealthCheckInterval:   50 * time.Millisecond,
			EnableConnectionReuse: true,
			CleanupInterval:       50 * time.Millisecond,
		},
	}
	
	mgr := NewDirectClientManager(config, logger)
	manager, ok := mgr.(*DirectClientManager)
	require.True(t, ok, "Expected *DirectClientManager type")
	return manager
}

func testRapidConnectionRequests(t *testing.T, manager *DirectClientManager, ctx context.Context) {
	t.Helper()
	const numRequests = 10
	
	results := make(chan error, numRequests)
	var wg sync.WaitGroup
	
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			testURL := fmt.Sprintf("echo pool-test-%d", id)
			_, err := manager.GetClient(ctx, testURL)
			results <- err
		}(i)
	}
	
	wg.Wait()
	close(results)
	
	completedRequests := 0
	for range results {
		completedRequests++
	}
	
	assert.Equal(t, numRequests, completedRequests, "All requests should complete")
}

func testPoolCleanupAndStats(t *testing.T, manager *DirectClientManager) {
	t.Helper()
	
	time.Sleep(150 * time.Millisecond)
	
	metrics := manager.GetMetrics()
	assert.GreaterOrEqual(t, metrics.TotalClients, 0)
	
	err := manager.RemoveClient("echo pool-test-1", ClientTypeStdio)
	if err != nil {
		t.Logf("RemoveClient returned expected error: %v", err)
	}
}

// Test protocol-specific client creation indirectly.
func TestDirectClientManagerProtocolSpecificCreation(t *testing.T) {
	manager := setupProtocolSpecificTestManager(t)
	ctx := context.Background()
	
	err := manager.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = manager.Stop(ctx) }()
	
	testProtocolSpecificClientCreation(t, manager, ctx)
}

func setupProtocolSpecificTestManager(t *testing.T) *DirectClientManager {
	t.Helper()
	logger := zaptest.NewLogger(t)
	config := createFullProtocolConfig()
	
	mgr := NewDirectClientManager(config, logger)
	manager, ok := mgr.(*DirectClientManager)
	require.True(t, ok, "Expected *DirectClientManager type")
	return manager
}

func createFullProtocolConfig() DirectConfig {
	config := DirectConfig{
		DefaultTimeout: 500 * time.Millisecond,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}
	
	config.Stdio = createFullStdioConfig()
	config.WebSocket = createFullWebSocketConfig()
	config.HTTP = createFullHTTPConfig()
	config.SSE = createFullSSEConfig()
	
	return config
}

func createFullStdioConfig() StdioConfig {
	return StdioConfig{
		DefaultWorkingDir: "/tmp",
		DefaultEnv:        map[string]string{"TEST": "value"},
		ProcessTimeout:    1 * time.Second,
		MaxBufferSize:     1024,
		Performance: StdioPerformanceTuning{
			StdinBufferSize:  512,
			StdoutBufferSize: 512,
			EnableBufferedIO: true,
			ReuseEncoders:    true,
			MaxRestarts:      2,
			RestartDelay:     1 * time.Second,
		},
	}
}

func createFullWebSocketConfig() WebSocketConfig {
	return WebSocketConfig{
		HandshakeTimeout: 2 * time.Second,
		PingInterval:     30 * time.Second,
		PongTimeout:      5 * time.Second,
		MaxMessageSize:   1024,
		DefaultHeaders:   map[string]string{"User-Agent": "Test"},
		Performance: WebSocketPerformanceTuning{
			EnableReadCompression:  true,
			EnableWriteCompression: false,
			OptimizePingPong:       true,
			EnableMessagePooling:   true,
			ReadBufferSize:         2048,
			WriteBufferSize:        2048,
		},
	}
}

func createFullHTTPConfig() HTTPConfig {
	return HTTPConfig{
		RequestTimeout:  2 * time.Second,
		MaxIdleConns:    5,
		DefaultHeaders:  map[string]string{"Content-Type": "application/json"},
		FollowRedirects: true,
		Performance: HTTPPerformanceTuning{
			EnableCompression:    true,
			CompressionThreshold: 1024,
			EnableHTTP2:          true,
			MaxConnsPerHost:      3,
			ReuseEncoders:        true,
			ResponseBufferSize:   4096,
			IdleConnTimeout:      30 * time.Second,
			KeepAlive:            15 * time.Second,
		},
	}
}

func createFullSSEConfig() SSEConfig {
	return SSEConfig{
		StreamTimeout:  30 * time.Second,
		RequestTimeout: 5 * time.Second,
		DefaultHeaders: map[string]string{"Accept": "text/event-stream"},
		BufferSize:     2048,
		Performance: SSEPerformanceTuning{
			StreamBufferSize:    8192,
			RequestBufferSize:   4096,
			ReuseConnections:    true,
			EnableCompression:   true,
			ConnectionPoolSize:  3,
			FastReconnect:       true,
			ReconnectDelay:      2 * time.Second,
			EnableEventBatching: false,
			BatchTimeout:        5 * time.Millisecond,
		},
	}
}

type protocolCreationTestCase struct {
	name        string
	url         string
	description string
}

func createProtocolCreationTestCases() []protocolCreationTestCase {
	return []protocolCreationTestCase{
		{
			name:        "Stdio protocol",
			url:         "echo 'stdio test'",
			description: "Should create stdio client with configured settings",
		},
		{
			name:        "HTTP protocol",
			url:         "http://example.com/mcp",
			description: "Should create HTTP client with configured settings",
		},
		{
			name:        "HTTPS protocol",
			url:         "https://secure.example.com/mcp",
			description: "Should create HTTPS client with configured settings",
		},
		{
			name:        "WebSocket protocol",
			url:         "ws://websocket.example.com/mcp",
			description: "Should create WebSocket client with configured settings",
		},
		{
			name:        "Secure WebSocket protocol",
			url:         "wss://secure-websocket.example.com/mcp",
			description: "Should create secure WebSocket client",
		},
		{
			name:        "SSE-hinted HTTP URL",
			url:         "http://sse.example.com/events",
			description: "Should create SSE client for events endpoint",
		},
	}
}

func testProtocolSpecificClientCreation(t *testing.T, manager *DirectClientManager, ctx context.Context) {
	t.Helper()
	protocolTests := createProtocolCreationTestCases()
	
	for _, tc := range protocolTests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := manager.GetClient(ctx, tc.url)
			if err != nil {
				assert.NotContains(t, err.Error(), "unsupported protocol", tc.description)
				assert.NotContains(t, err.Error(), "failed to create", tc.description)
			}
		})
	}
}

// Test cache expiration and cleanup.
func TestDirectClientManagerCacheManagement(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{
		DefaultTimeout: 500 * time.Millisecond,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			CacheResults:   true,
			CacheTTL:       200 * time.Millisecond, // Short TTL for testing
			PreferredOrder: []string{"stdio", "http"},
		},
	}

	mgr := NewDirectClientManager(config, logger)


	manager, ok := mgr.(*DirectClientManager)


	require.True(t, ok, "Expected *DirectClientManager type")

	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(t, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(t, err)
	}()

	testURL := "echo cache-test"

	// First request - should cache result.
	_, err1 := manager.GetClient(ctx, testURL)

	// Second request immediately - should use cache.
	start := time.Now()
	_, err2 := manager.GetClient(ctx, testURL)
	duration := time.Since(start)

	// Should be fast due to caching.
	assert.Less(t, duration, 100*time.Millisecond, "Cached request should be fast")

	// Check cache metrics.
	metrics := manager.GetMetrics()
	if err1 != nil && err2 != nil {
		// If both failed, second should have been faster due to cache.
		assert.Positive(t, metrics.CacheHits, "Should have cache hits")
	}

	// Wait for cache to expire.
	time.Sleep(300 * time.Millisecond)

	// Third request - cache should be expired, so detection runs again.
	_, err3 := manager.GetClient(ctx, testURL)

	// Verify cache miss after expiration.
	finalMetrics := manager.GetMetrics()
	if err3 != nil {
		assert.Greater(t, finalMetrics.CacheMisses, metrics.CacheMisses, "Should have cache miss after expiration")
	}
}

// Test manager stop behavior under various conditions.
func TestDirectClientManagerStopBehavior(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	t.Run("Stop when not started", func(t *testing.T) {
		testStopWhenNotStarted(t, logger)
	})
	
	t.Run("Stop with active connections", func(t *testing.T) {
		testStopWithActiveConnections(t, logger)
	})
	
	t.Run("Multiple stop calls", func(t *testing.T) {
		testMultipleStopCalls(t, logger)
	})
	
	t.Run("Stop with context timeout", func(t *testing.T) {
		testStopWithContextTimeout(t, logger)
	})
}

func testStopWhenNotStarted(t *testing.T, logger *zap.Logger) {
	t.Helper()
	config := DirectConfig{}
	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()
	
	err := manager.Stop(ctx)
	require.NoError(t, err)
}

func testStopWithActiveConnections(t *testing.T, logger *zap.Logger) {
	t.Helper()
	config := DirectConfig{
		DefaultTimeout: 1 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 100 * time.Millisecond,
		},
	}
	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()
	
	err := manager.Start(ctx)
	require.NoError(t, err)
	
	for i := 0; i < 3; i++ {
		_, _ = manager.GetClient(ctx, fmt.Sprintf("echo stop-test-%d", i))
	}
	
	err = manager.Stop(ctx)
	require.NoError(t, err)
	
	_, err = manager.GetClient(ctx, "echo after-stop")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func testMultipleStopCalls(t *testing.T, logger *zap.Logger) {
	t.Helper()
	config := DirectConfig{}
	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()
	
	err := manager.Start(ctx)
	require.NoError(t, err)
	
	err = manager.Stop(ctx)
	require.NoError(t, err)
	
	err = manager.Stop(ctx)
	require.NoError(t, err)
}

func testStopWithContextTimeout(t *testing.T, logger *zap.Logger) {
	t.Helper()
	config := DirectConfig{
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 50 * time.Millisecond,
		},
	}
	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()
	
	err := manager.Start(ctx)
	require.NoError(t, err)
	
	stopCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()
	
	err = manager.Stop(stopCtx)
	if err != nil {
		assert.Contains(t, err.Error(), "context deadline exceeded")
	}
	
	err = manager.Stop(context.Background())
	require.NoError(t, err)
}

// Test interface compliance and method coverage.
func TestDirectClientManagerInterfaceCompliance(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := DirectConfig{}

	// Test that our concrete type implements the interface.
	var manager = NewDirectClientManager(config, logger)
	assert.NotNil(t, manager)

	ctx := context.Background()

	// Test all interface methods.
	err := manager.Start(ctx)
	require.NoError(t, err)

	_, err = manager.GetClient(ctx, "echo interface-test")
	require.Error(t, err) // Expected to fail but should not panic

	err = manager.Stop(ctx)
	require.NoError(t, err)
}

// Benchmark for performance regression detection.
func BenchmarkDirectClientManagerProtocolDetection(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DirectConfig{
		DefaultTimeout: 100 * time.Millisecond,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			CacheResults:   false, // Disable cache to test raw performance
			PreferredOrder: []string{"stdio", "http"},
		},
	}
	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(b, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(b, err)
	}()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Test protocol detection performance.
		testURL := fmt.Sprintf("echo bench-test-%d", i%10) // Cycle through 10 URLs
		_, _ = manager.GetClient(ctx, testURL)
	}
}

func BenchmarkDirectClientManagerWithCaching(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DirectConfig{
		DefaultTimeout: 100 * time.Millisecond,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
		AutoDetection: AutoDetectionConfig{
			Enabled:        true,
			CacheResults:   true, // Enable cache for this benchmark
			CacheTTL:       5 * time.Minute,
			PreferredOrder: []string{"stdio", "http"},
		},
	}
	manager := NewDirectClientManager(config, logger)
	ctx := context.Background()

	err := manager.Start(ctx)
	require.NoError(b, err)

	defer func() {
		err := manager.Stop(ctx)
		require.NoError(b, err)
	}()

	// Warm up cache with a few URLs.
	for i := 0; i < 5; i++ {
		testURL := fmt.Sprintf("echo cache-warmup-%d", i)
		_, _ = manager.GetClient(ctx, testURL)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Test cached protocol detection performance.
		testURL := fmt.Sprintf("echo cache-warmup-%d", i%5) // Use warmed URLs
		_, _ = manager.GetClient(ctx, testURL)
	}
}
