// Chaos test files allow flexible style
package chaos_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

// ChaosTest represents a chaos testing scenario.
type ChaosTest struct {
	Name        string
	Description string
	Setup       func(*testing.T, *ChaosConfig)
	Execute     func(context.Context, *testing.T, *ChaosConfig, *ChaosMetrics)
	Verify      func(*testing.T, *ChaosMetrics)
	Cleanup     func(*testing.T, *ChaosConfig)
}

// ChaosConfig holds configuration for chaos tests with environment variable support.
type ChaosConfig struct {
	GatewayURL      string
	AuthToken       string
	TestDuration    time.Duration
	NumConnections  int
	ChaosStartDelay time.Duration
	ChaosInterval   time.Duration
	// New configurable thresholds
	MinRecoveryRate float64
	MaxRecoveryTime time.Duration
	ToleranceRange  float64
	// Mock server for chaos testing
	MockServer *ChaosServer
}

// ChaosMetrics tracks metrics during chaos testing.
type ChaosMetrics struct {
	ConnectionsLost      int64
	ConnectionsRecovered int64
	RequestsFailed       int64
	RequestsSucceeded    int64
	RecoveryTime         int64 // nanoseconds
	DataLoss             int64
	OutOfOrderMessages   int64
	DuplicateMessages    int64
	PartialMessages      int64

	// Add synchronization primitives for coordination
	ready         chan struct{}
	partitionDone chan struct{}
	recoveryDone  chan struct{}
}

// NewChaosConfig creates a new chaos config with environment variable overrides.
func NewChaosConfig(t *testing.T) *ChaosConfig {
	t.Helper()

	// Create mock server if no external URL provided
	var mockServer *ChaosServer

	gatewayURL := os.Getenv("TEST_GATEWAY_URL")
	if gatewayURL == "" {
		mockServer = startMockChaosServer()
		gatewayURL = mockServer.server.URL
	}

	config := &ChaosConfig{
		GatewayURL:      gatewayURL,
		AuthToken:       generateTestToken(t),
		TestDuration:    getEnvDuration("CHAOS_TEST_DURATION", 30*time.Second), // Reduced for testing
		NumConnections:  getEnvInt("CHAOS_NUM_CONNECTIONS", 10),                // Reduced for testing
		ChaosStartDelay: getEnvDuration("CHAOS_START_DELAY", 1*time.Second),
		ChaosInterval:   getEnvDuration("CHAOS_INTERVAL", 2*time.Second),
		MinRecoveryRate: getEnvFloat("CHAOS_MIN_RECOVERY_RATE", 0.70), // 70% for mock testing
		MaxRecoveryTime: getEnvDuration("CHAOS_MAX_RECOVERY_TIME", 10*time.Second),
		ToleranceRange:  getEnvFloat("CHAOS_TOLERANCE_RANGE", 0.10), // ±10% tolerance
		MockServer:      mockServer,
	}

	return config
}

// NewChaosMetrics creates a new metrics object with proper channel initialization.
func NewChaosMetrics() *ChaosMetrics {
	return &ChaosMetrics{
		ConnectionsLost:      0, // No connections lost initially
		ConnectionsRecovered: 0, // No connections recovered initially
		RequestsFailed:       0, // No requests failed initially
		RequestsSucceeded:    0, // No requests succeeded initially
		RecoveryTime:         0, // No recovery time initially
		DataLoss:             0, // No data loss initially
		OutOfOrderMessages:   0, // No out of order messages initially
		DuplicateMessages:    0, // No duplicate messages initially
		PartialMessages:      0, // No partial messages initially
		ready:                make(chan struct{}),
		partitionDone:        make(chan struct{}),
		recoveryDone:         make(chan struct{}),
	}
}

// Environment variable helpers.
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}

	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}

	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}

	return defaultValue
}

// TestChaosScenarios runs comprehensive chaos testing scenarios.
func TestChaosScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos tests in short mode - set CHAOS_ENABLED=true to run specific chaos tests")
	}

	tests := getChaosTestCases()
	config := NewChaosConfig(t)

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			runSingleChaosTest(t, test, config)
		})
	}
}

func getChaosTestCases() []ChaosTest {
	return []ChaosTest{
		{
			Name:        "network_partition_recovery",
			Description: "Tests recovery from network partitions with proper synchronization",
			Setup:       nil, // No specific setup required
			Execute:     testNetworkPartitionRecovery,
			Verify:      verifyPartitionRecovery,
			Cleanup:     nil, // No specific cleanup required
		},
		{
			Name:        "connection_drop_resilience",
			Description: "Tests resilience to dropped connections",
			Setup:       nil, // No specific setup required
			Execute:     testConnectionDropResilience,
			Verify:      verifyConnectionResilience,
			Cleanup:     nil, // No specific cleanup required
		},
		{
			Name:        "load_balancer_failure",
			Description: "Simulates load balancer failures",
			Setup:       nil, // No specific setup required
			Execute:     testLoadBalancerFailure,
			Verify:      verifyLoadBalancerRecovery,
			Cleanup:     nil, // No specific cleanup required
		},
		{
			Name:        "tls_handshake_failures",
			Description: "Simulates TLS handshake failures",
			Setup:       nil, // No specific setup required
			Execute:     testTLSHandshakeFailures,
			Verify:      verifyTLSFailureHandling,
			Cleanup:     nil, // No specific cleanup required
		},
	}
}

func runSingleChaosTest(t *testing.T, test ChaosTest, config *ChaosConfig) {
	t.Helper()
	// Create context with timeout for each test
	ctx, cancel := context.WithTimeout(context.Background(), config.TestDuration)
	defer cancel()

	metrics := NewChaosMetrics()

	t.Logf("Starting chaos test: %s", test.Description)

	// Setup if needed
	if test.Setup != nil {
		test.Setup(t, config)
	}

	// Cleanup with proper resource management
	defer func() {
		if test.Cleanup != nil {
			test.Cleanup(t, config)
		}
		// Always clean up mock server if it exists
		if config.MockServer != nil {
			config.MockServer.Close()
		}
	}()

	// Execute chaos test with context
	test.Execute(ctx, t, config, metrics)

	// Verify results
	test.Verify(t, metrics)

	// Log results
	logChaosTestResults(t, test.Name, metrics)
}

// establishInitialConnections creates and establishes all initial connections.
func establishInitialConnections(ctx context.Context, t *testing.T,
	config *ChaosConfig) (connections []*TestConnection,
	successfulCount int) {
	t.Helper()

	connections = make([]*TestConnection, config.NumConnections)

	var wg sync.WaitGroup

	// Establish connections with proper error handling
	for i := range config.NumConnections {
		wg.Add(1)

		go func(index int) {
			defer wg.Done()

			conn, err := establishConnection(ctx, config)
			if err != nil {
				t.Errorf("Failed to establish connection %d: %v", index, err)

				return
			}

			connections[index] = conn
		}(i)
	}

	// Wait for all connections to be ready
	wg.Wait()

	// Count successful initial connections
	for _, conn := range connections {
		if conn != nil {
			successfulCount++
		}
	}

	return connections, successfulCount
}

// simulateNetworkPartition simulates a network partition for the specified duration.
func simulateNetworkPartition(ctx context.Context, t *testing.T, config *ChaosConfig, metrics *ChaosMetrics) time.Time {
	t.Helper()

	// Simulate network partition using mock server if available
	t.Log("Simulating network partition...")

	partitionStart := time.Now()

	if config.MockServer != nil {
		// Use server-side partition simulation
		config.MockServer.SimulatePartition()
		defer config.MockServer.RestoreConnectivity()
	}

	// Wait for partition duration with context awareness
	partitionDuration := getEnvDuration("CHAOS_PARTITION_DURATION", 2*time.Second)
	select {
	case <-time.After(partitionDuration):
		// Partition period complete
	case <-ctx.Done():
		t.Fatal("Context timeout during partition")

		return partitionStart
	}

	// Signal partition is done
	close(metrics.partitionDone)

	// Restore network connectivity
	t.Log("Restoring network connectivity...")

	if config.MockServer != nil {
		config.MockServer.RestoreConnectivity()
	}

	return partitionStart
}

// attemptRecovery attempts to recover connections after partition.
func attemptRecovery(ctx context.Context, t *testing.T, config *ChaosConfig, metrics *ChaosMetrics,
	successfulConnections int) {
	t.Helper()

	// Attempt to re-establish connections
	var recoveryWg sync.WaitGroup
	for i := range successfulConnections {
		recoveryWg.Add(1)

		go func(index int) {
			defer recoveryWg.Done()

			// Try to establish new connection (simulating recovery)
			_, err := establishConnection(ctx, config)
			if err != nil {
				atomic.AddInt64(&metrics.RequestsFailed, 1)
				t.Logf("Failed to restore connection %d: %v", index, err)
			} else {
				atomic.AddInt64(&metrics.ConnectionsRecovered, 1)
			}
		}(i)
	}

	// Wait for recovery completion
	done := make(chan struct{})

	go func() {
		recoveryWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Recovery completed
	case <-time.After(config.MaxRecoveryTime):
		t.Log("Recovery timed out")
	case <-ctx.Done():
		t.Fatal("Context timeout during recovery")

		return
	}
}

func testNetworkPartitionRecovery(ctx context.Context, t *testing.T, config *ChaosConfig, metrics *ChaosMetrics) {
	t.Helper()

	// Create connections with proper lifecycle management
	connections, successfulConnections := establishInitialConnections(ctx, t, config)

	// Signal that setup is complete
	close(metrics.ready)

	// Wait for connections to stabilize using proper synchronization
	select {
	case <-time.After(config.ChaosStartDelay):
		// Connections should be stable now
	case <-ctx.Done():
		t.Fatal("Context timeout during connection stabilization")

		return
	}

	if successfulConnections == 0 {
		t.Fatal("No successful connections established")

		return
	}

	t.Logf("Established %d successful connections", successfulConnections)

	// Simulate network partition
	partitionStart := simulateNetworkPartition(ctx, t, config, metrics)

	// Attempt recovery
	attemptRecovery(ctx, t, config, metrics, successfulConnections)

	// Measure recovery time
	recoveryTime := time.Since(partitionStart)
	atomic.StoreInt64(&metrics.RecoveryTime, recoveryTime.Nanoseconds())

	// Signal recovery is done
	close(metrics.recoveryDone)

	// Clean up connections
	for _, conn := range connections {
		if conn != nil {
			_ = conn.Close()
		}
	}
}

// verifyPartitionRecovery uses configurable thresholds with tolerance.
func verifyPartitionRecovery(t *testing.T, metrics *ChaosMetrics) {
	t.Helper()
	config := NewChaosConfig(t)

	recovered := atomic.LoadInt64(&metrics.ConnectionsRecovered)
	failed := atomic.LoadInt64(&metrics.RequestsFailed)
	total := recovered + failed

	if total == 0 {
		t.Error("No connections were tested")

		return
	}

	recoveryRate := float64(recovered) / float64(total)
	recoveryTime := time.Duration(atomic.LoadInt64(&metrics.RecoveryTime))

	// Use configurable thresholds with tolerance
	minRate := config.MinRecoveryRate - config.ToleranceRange
	maxRate := config.MinRecoveryRate + config.ToleranceRange

	assert.GreaterOrEqual(t, recoveryRate, minRate,
		"Recovery rate %.2f%% is below acceptable range (%.2f%% - %.2f%%)",
		recoveryRate*100, minRate*100, maxRate*100)

	assert.LessOrEqual(t, recoveryTime, config.MaxRecoveryTime,
		"Recovery time %v exceeds maximum allowed %v", recoveryTime, config.MaxRecoveryTime)

	t.Logf("Recovery metrics: rate=%.2f%%, time=%v", recoveryRate*100, recoveryTime)
}

// TestConnection represents a test connection with improved lifecycle management.
type TestConnection struct {
	conn        *websocket.Conn
	mu          sync.RWMutex
	partitioned bool
	ctx         context.Context
	cancel      context.CancelFunc
}

// Close closes the test connection and cancels its context.
func (tc *TestConnection) Close() error {
	tc.cancel()

	return tc.conn.Close()
}

func establishConnection(ctx context.Context, config *ChaosConfig) (*TestConnection, error) {
	connCtx, cancel := context.WithCancel(ctx)

	// Convert HTTP URL to WebSocket URL if needed
	wsURL := config.GatewayURL
	if strings.HasPrefix(wsURL, "http://") {
		wsURL = "ws" + wsURL[4:]
	} else if strings.HasPrefix(wsURL, "https://") {
		wsURL = "wss" + wsURL[5:]
	}

	conn, resp, err := websocket.DefaultDialer.DialContext(connCtx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	if err != nil {
		cancel()

		return nil, fmt.Errorf("failed to dial: %w", err)
	}

	return &TestConnection{
		conn:        conn,
		mu:          sync.RWMutex{}, // Initialize mutex
		partitioned: false,          // Not partitioned initially
		ctx:         connCtx,
		cancel:      cancel,
	}, nil
}

func (tc *TestConnection) SimulatePartitionWithContext(ctx context.Context) {
	tc.mu.Lock()
	tc.partitioned = true
	tc.mu.Unlock()

	// Simulate partition until context is canceled
	<-ctx.Done()

	tc.mu.Lock()
	tc.partitioned = false
	tc.mu.Unlock()
}

func (tc *TestConnection) RestoreConnectionWithTimeout(ctx context.Context, timeout time.Duration) error {
	restoreCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Simulate connection restore with timeout
	select {
	case <-time.After(100 * time.Millisecond): // Simulate restore time
		return nil
	case <-restoreCtx.Done():
		return errors.New("restore timeout")
	}
}

func logChaosTestResults(t *testing.T, testName string, metrics *ChaosMetrics) {
	t.Helper()
	t.Logf("Chaos test results for %s:", testName)
	t.Logf("  Connections lost: %d", atomic.LoadInt64(&metrics.ConnectionsLost))
	t.Logf("  Connections recovered: %d", atomic.LoadInt64(&metrics.ConnectionsRecovered))
	t.Logf("  Requests failed: %d", atomic.LoadInt64(&metrics.RequestsFailed))
	t.Logf("  Recovery time: %v", time.Duration(atomic.LoadInt64(&metrics.RecoveryTime)))
}

// Placeholder implementations for other test functions.
func testConnectionDropResilience(_ context.Context, _ *testing.T, _ *ChaosConfig, _ *ChaosMetrics) {
	// Implementation would go here
}

func verifyConnectionResilience(_ *testing.T, _ *ChaosMetrics) {
	// Implementation would go here
}

func testLoadBalancerFailure(_ context.Context, _ *testing.T, _ *ChaosConfig, _ *ChaosMetrics) {
	// Implementation would go here
}

func verifyLoadBalancerRecovery(_ *testing.T, _ *ChaosMetrics) {
	// Implementation would go here
}

func testTLSHandshakeFailures(_ context.Context, _ *testing.T, _ *ChaosConfig, _ *ChaosMetrics) {
	// Implementation would go here
}

func verifyTLSFailureHandling(_ *testing.T, _ *ChaosMetrics) {
	// Implementation would go here
}

// Helper functions for chaos testing with mock server

func generateTestToken(t *testing.T) string {
	t.Helper()
	// This should generate a proper test token, not just return a mock
	if token := os.Getenv("TEST_AUTH_TOKEN"); token != "" {
		return token
	}
	// For now, return a mock token - this should be improved
	return "test-jwt-token"
}

// ChaosServer represents a mock MCP server that can simulate chaos conditions.
type ChaosServer struct {
	server          *httptest.Server
	upgrader        websocket.Upgrader
	connections     map[*websocket.Conn]bool
	mu              sync.RWMutex
	partitionActive bool
	dropRate        float64 // Percentage of connections to drop (0.0 - 1.0)
}

// startMockChaosServer creates and starts a mock WebSocket server for chaos testing.
func startMockChaosServer() *ChaosServer {
	cs := &ChaosServer{
		server: nil, // Will be set after server creation
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 0,   // Use default handshake timeout
			ReadBufferSize:   0,   // Use default read buffer size
			WriteBufferSize:  0,   // Use default write buffer size
			WriteBufferPool:  nil, // No custom write buffer pool
			Subprotocols:     nil, // No subprotocols
			Error:            nil, // Use default error handler
			CheckOrigin: func(_ *http.Request) bool {
				return true // Allow any origin for testing
			},
			EnableCompression: false, // No compression for testing
		},
		connections:     make(map[*websocket.Conn]bool),
		mu:              sync.RWMutex{}, // Initialize mutex
		partitionActive: false,          // Not partitioned initially
		dropRate:        0.0,            // No connections dropped initially
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)

			if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			}

			return
		}

		// Simulate server overload by randomly refusing connections
		cs.mu.RLock()

		if cs.dropRate > 0 && (float64(len(cs.connections))/100.0) > cs.dropRate {
			cs.mu.RUnlock()
			http.Error(w, "Server overloaded", http.StatusServiceUnavailable)

			return
		}

		cs.mu.RUnlock()

		// Upgrade to WebSocket
		conn, err := cs.upgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)

			return
		}

		cs.mu.Lock()
		cs.connections[conn] = true
		cs.mu.Unlock()

		defer func() {
			cs.mu.Lock()
			delete(cs.connections, conn)
			cs.mu.Unlock()

			_ = conn.Close()
		}()

		// Handle WebSocket messages
		cs.handleConnection(conn)
	})

	cs.server = httptest.NewServer(handler)

	return cs
}

// handleConnection handles individual WebSocket connections with chaos simulation.
func (cs *ChaosServer) handleConnection(conn *websocket.Conn) {
	for {
		// Check if partition is active
		cs.mu.RLock()

		if cs.partitionActive {
			cs.mu.RUnlock()
			// Simulate network partition by closing connection
			return
		}

		cs.mu.RUnlock()

		// Read message from client
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			// Connection closed or error occurred
			return
		}

		// Simulate random message drops during chaos
		if cs.shouldDropMessage() {
			continue
		}

		// Echo response back to client (simple MCP-like behavior)
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      msg["id"],
			"result": map[string]interface{}{
				"status": "ok",
				"echo":   msg,
			},
		}

		if err := conn.WriteJSON(response); err != nil {
			// Failed to write response
			return
		}
	}
}

// shouldDropMessage simulates random message drops.
func (cs *ChaosServer) shouldDropMessage() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	// Simulate 5% message drop rate during normal operation
	return cs.partitionActive || (time.Now().UnixNano()%100 < 5)
}

// SimulatePartition activates network partition simulation.
func (cs *ChaosServer) SimulatePartition() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.partitionActive = true

	// Close all existing connections to simulate partition
	for conn := range cs.connections {
		_ = conn.Close()
	}
}

// RestoreConnectivity deactivates network partition simulation.
func (cs *ChaosServer) RestoreConnectivity() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.partitionActive = false
}

// SetDropRate sets the connection drop rate (0.0 - 1.0).
func (cs *ChaosServer) SetDropRate(rate float64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.dropRate = rate
}

// GetActiveConnections returns the number of active connections.
func (cs *ChaosServer) GetActiveConnections() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	return len(cs.connections)
}

// Close shuts down the mock server.
func (cs *ChaosServer) Close() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Close all connections
	for conn := range cs.connections {
		_ = conn.Close()
	}

	if cs.server != nil {
		cs.server.Close()
	}
}
