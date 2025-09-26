// Load test files allow flexible style
//

package load_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

// LoadTestConfig defines the parameters for load testing with environment-aware defaults.
type LoadTestConfig struct {
	GatewayURL           string
	TargetConnections    int
	ConnectionsPerSecond int
	TestDuration         time.Duration
	RequestsPerSecond    float64
	AuthToken            string
	// Performance thresholds with tolerances
	MinSuccessRate   float64
	MaxErrorRate     float64
	TolerancePercent float64
	// Environment-specific settings
	InsecureSkipVerify bool
	ConnectionTimeout  time.Duration
	RequestTimeout     time.Duration
	// Adaptive thresholds based on system resources
	SystemLoadFactor float64
	ConcurrencyLimit int
}

// LoadTestMetrics tracks performance metrics with better granularity.
type LoadTestMetrics struct {
	TotalConnections      int64
	SuccessfulConnections int64
	FailedConnections     int64
	TotalRequests         int64
	SuccessfulRequests    int64
	FailedRequests        int64
	TotalLatency          int64 // nanoseconds
	MaxLatency            int64 // nanoseconds
	MinLatency            int64 // nanoseconds
	ConnectionErrors      map[string]int64
	RequestErrors         map[string]int64
	// Additional metrics for better analysis
	P50Latency           int64
	P95Latency           int64
	P99Latency           int64
	ThroughputPerSecond  float64
	ConnectionsPerSecond float64
	// Latency histogram for percentile calculations
	LatencyHistogram []int64
	mu               sync.RWMutex
}

// SystemInfo captures system information for adaptive testing.
type SystemInfo struct {
	NumCPU       int
	MemoryGB     float64
	IsCI         bool
	Architecture string
	OS           string
}

// NewLoadTestConfig creates configuration with environment-aware defaults.
func NewLoadTestConfig(t *testing.T) *LoadTestConfig {
	t.Helper()

	sysInfo := getSystemInfo()

	// Base configuration
	config := &LoadTestConfig{
		GatewayURL: getEnvString("LOAD_TEST_GATEWAY_URL", "ws://localhost:8080"), // WebSocket gateway URL for testing
		TargetConnections: getEnvInt("LOAD_TEST_CONNECTIONS",
			calculateDefaultConnections(sysInfo)), // Number of concurrent connections to establish
		ConnectionsPerSecond: getEnvInt("LOAD_TEST_CONN_RATE",
			calculateDefaultConnRate(sysInfo)), // Rate of connection establishment
		// Duration of the load test
		TestDuration: getEnvDuration("LOAD_TEST_DURATION", calculateDefaultDuration(sysInfo)),
		// Rate of requests per second
		RequestsPerSecond: getEnvFloat("LOAD_TEST_REQ_RATE", 50.0),
		// Authentication token for requests
		AuthToken: generateLoadTestToken(t),
		// Minimum success rate threshold (set later)
		MinSuccessRate: 0.0,
		// Maximum error rate threshold (set later)
		MaxErrorRate: 0.0,
		// Tolerance percentage for thresholds (set later)
		TolerancePercent: 0.0,
		// Skip TLS verification for testing
		InsecureSkipVerify: getEnvBool("LOAD_TEST_INSECURE", false),
		ConnectionTimeout: getEnvDuration("LOAD_TEST_CONN_TIMEOUT",
			30*time.Second), // Timeout for connection establishment
		RequestTimeout:   getEnvDuration("LOAD_TEST_REQ_TIMEOUT", 5*time.Second), // Timeout for individual requests
		SystemLoadFactor: calculateSystemLoadFactor(sysInfo),                     // System load factor for adaptive testing
		ConcurrencyLimit: calculateConcurrencyLimit(sysInfo),                     // Maximum concurrent operations limit
	}

	// Set adaptive performance thresholds
	config.MinSuccessRate = calculateMinSuccessRate(sysInfo)
	config.MaxErrorRate = calculateMaxErrorRate(sysInfo)
	config.TolerancePercent = calculateTolerancePercent(sysInfo)

	t.Logf("Load test configuration:")
	t.Logf("  Target connections: %d", config.TargetConnections)
	t.Logf("  System load factor: %.2f", config.SystemLoadFactor)
	t.Logf("  Min success rate: %.2f%% (±%.1f%%)", config.MinSuccessRate*100, config.TolerancePercent*100)
	t.Logf("  Max error rate: %.2f%%", config.MaxErrorRate*100)
	t.Logf("  Test duration: %v", config.TestDuration)

	return config
}

// TestGateway_AdaptiveConcurrentConnections tests with adaptive connection limits.
func TestGateway_AdaptiveConcurrentConnections(t *testing.T) {
	// Cannot run in parallel: Load test uses real network connections, measures performance timing,
	// consumes system resources, and has environment dependencies
	if testing.Short() {
		t.Skip("Skipping load test in short mode - set LOAD_TEST_ENABLED=true to override")
	}

	// Check if load testing is explicitly enabled
	if !getEnvBool("LOAD_TEST_ENABLED", false) {
		t.Skip("Load testing disabled - set LOAD_TEST_ENABLED=true to enable")
	}

	config := NewLoadTestConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), config.TestDuration+time.Minute)
	defer cancel()

	metrics := &LoadTestMetrics{
		TotalConnections:      0,                                             // Total connection attempts
		SuccessfulConnections: 0,                                             // Number of successful connections
		FailedConnections:     0,                                             // Number of failed connections
		TotalRequests:         0,                                             // Total requests sent
		SuccessfulRequests:    0,                                             // Number of successful requests
		FailedRequests:        0,                                             // Number of failed requests
		TotalLatency:          0,                                             // Total latency in nanoseconds
		MaxLatency:            0,                                             // Maximum latency observed
		MinLatency:            math.MaxInt64,                                 // Minimum latency observed
		ConnectionErrors:      make(map[string]int64),                        // Map of connection error types to counts
		RequestErrors:         make(map[string]int64),                        // Map of request error types to counts
		P50Latency:            0,                                             // 50th percentile latency
		P95Latency:            0,                                             // 95th percentile latency
		P99Latency:            0,                                             // 99th percentile latency
		ThroughputPerSecond:   0.0,                                           // Requests processed per second
		ConnectionsPerSecond:  0.0,                                           // Connections established per second
		LatencyHistogram:      make([]int64, 0, config.TargetConnections*10), // Histogram of latency measurements
		mu:                    sync.RWMutex{},                                // Mutex for thread-safe access to metrics
	}

	t.Logf("Starting adaptive load test with %d target connections", config.TargetConnections)

	// Run the load test
	runAdaptiveLoadTest(ctx, t, config, metrics)

	// Verify results with adaptive thresholds
	verifyLoadTestResults(t, config, metrics)
}

// runAdaptiveLoadTest executes the load test with proper resource management.
func runAdaptiveLoadTest(ctx context.Context, t *testing.T, config *LoadTestConfig, metrics *LoadTestMetrics) {
	t.Helper()

	startTime := time.Now()

	// Connection establishment phase
	t.Log("Phase 1: Establishing connections...")
	connections := establishConnections(ctx, t, config, metrics)

	// Request generation phase
	t.Log("Phase 2: Generating load...")
	generateLoad(ctx, t, config, metrics, connections)

	// Cleanup phase
	t.Log("Phase 3: Cleaning up connections...")
	cleanupConnections(t, connections)

	// Calculate final metrics
	testDuration := time.Since(startTime)
	calculateFinalMetrics(metrics, testDuration)

	logDetailedMetrics(t, config, metrics, testDuration)
}

// establishConnections creates connections with proper rate limiting and error handling.
func establishConnections(ctx context.Context, t *testing.T, config *LoadTestConfig,
	metrics *LoadTestMetrics, // Complex load test connection setup
) []*websocket.Conn {
	t.Helper()

	connections := make([]*websocket.Conn, 0, config.TargetConnections)
	connectionsMu := sync.Mutex{}

	// Rate limiter for connection establishment
	ticker := time.NewTicker(time.Second / time.Duration(config.ConnectionsPerSecond))
	defer ticker.Stop()

	var wg sync.WaitGroup

	semaphore := make(chan struct{}, config.ConcurrencyLimit)
	establishmentStart := time.Now()

	for i := range config.TargetConnections {
		select {
		case <-ctx.Done():
			t.Log("Context canceled during connection establishment")

			return connections
		case <-ticker.C:
		}

		wg.Add(1)

		go establishConnectionAsync(ctx, t, i, config, metrics, &connectionsMu, &connections, semaphore, &wg)
	}

	wg.Wait()

	establishmentDuration := time.Since(establishmentStart)
	successfulConns := atomic.LoadInt64(&metrics.SuccessfulConnections)
	metrics.ConnectionsPerSecond = float64(successfulConns) / establishmentDuration.Seconds()

	t.Logf("Established %d/%d connections in %v (%.2f conn/sec)",
		successfulConns, config.TargetConnections, establishmentDuration, metrics.ConnectionsPerSecond)

	return connections
}

func establishConnectionAsync(ctx context.Context, t *testing.T, connIndex int, config *LoadTestConfig,
	metrics *LoadTestMetrics, connectionsMu *sync.Mutex, connections *[]*websocket.Conn,
	semaphore chan struct{}, wg *sync.WaitGroup) {
	t.Helper()

	defer wg.Done()

	// Acquire semaphore
	semaphore <- struct{}{}

	defer func() { <-semaphore }()

	conn, err := establishSingleConnection(ctx, config)
	if err != nil {
		atomic.AddInt64(&metrics.FailedConnections, 1)
		metrics.mu.Lock()
		metrics.ConnectionErrors[err.Error()]++
		metrics.mu.Unlock()
		t.Logf("Connection %d failed: %v", connIndex, err)

		return
	}

	connectionsMu.Lock()

	*connections = append(*connections, conn)

	connectionsMu.Unlock()

	atomic.AddInt64(&metrics.SuccessfulConnections, 1)
}

// establishSingleConnection creates a single WebSocket connection with proper timeout.
func establishSingleConnection(ctx context.Context, config *LoadTestConfig) (*websocket.Conn, error) {
	dialer := &websocket.Dialer{
		NetDial:           nil,                      // Use default network dialer
		NetDialContext:    nil,                      // Use default network dialer with context
		NetDialTLSContext: nil,                      // Use default TLS dialer with context
		Proxy:             nil,                      // Use default proxy settings
		HandshakeTimeout:  config.ConnectionTimeout, // Custom handshake timeout from config
		ReadBufferSize:    0,                        // Use default read buffer size
		WriteBufferSize:   0,                        // Use default write buffer size
		WriteBufferPool:   nil,                      // Use default write buffer pool
		Subprotocols:      nil,                      // No specific subprotocols required
		EnableCompression: false,                    // Disable compression for load testing
		Jar:               nil,                      // No cookie jar needed
		TLSClientConfig: &tls.Config{
			Rand:                                nil,                       // Use default random source
			Time:                                nil,                       // Use default time source
			Certificates:                        nil,                       // No client certificates
			NameToCertificate:                   nil,                       // Deprecated field, set to nil
			GetCertificate:                      nil,                       // No dynamic certificate selection
			GetClientCertificate:                nil,                       // No client certificate callback
			GetConfigForClient:                  nil,                       // No per-client config callback
			VerifyPeerCertificate:               nil,                       // Use default peer verification
			VerifyConnection:                    nil,                       // Use default connection verification
			RootCAs:                             nil,                       // Use system root CAs
			NextProtos:                          nil,                       // No specific protocols
			ServerName:                          "",                        // No specific server name
			ClientAuth:                          0,                         // No client auth required
			ClientCAs:                           nil,                       // No client CAs
			InsecureSkipVerify:                  config.InsecureSkipVerify, //nolint:gosec // Test environment only
			CipherSuites:                        nil,                       // Use default cipher suites
			PreferServerCipherSuites:            false,                     // Use default cipher suite preference
			SessionTicketsDisabled:              false,                     // Enable session tickets
			SessionTicketKey:                    [32]byte{},                // Use default session ticket key
			ClientSessionCache:                  nil,                       // No client session cache
			UnwrapSession:                       nil,                       // No session unwrapping
			WrapSession:                         nil,                       // No session wrapping
			MinVersion:                          0,                         // Use default minimum TLS version
			MaxVersion:                          0,                         // Use default maximum TLS version
			CurvePreferences:                    nil,                       // Use default curve preferences
			DynamicRecordSizingDisabled:         false,                     // Enable dynamic record sizing
			Renegotiation:                       0,                         // Use default renegotiation setting
			KeyLogWriter:                        nil,                       // No key logging
			EncryptedClientHelloConfigList:      nil,                       // No ECH configuration
			EncryptedClientHelloRejectionVerify: nil,                       // No ECH rejection verification
		},
	}

	headers := http.Header{}
	if config.AuthToken != "" {
		headers.Set("Authorization", "Bearer "+config.AuthToken)
	}

	conn, resp, err := dialer.DialContext(ctx, config.GatewayURL, headers)
	if resp != nil && resp.Body != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	return conn, nil
}

// generateLoad sends requests through established connections.
func generateLoad(ctx context.Context, t *testing.T, config *LoadTestConfig,
	metrics *LoadTestMetrics, connections []*websocket.Conn,
) {
	t.Helper()

	if len(connections) == 0 {
		t.Error("No connections available for load generation")

		return
	}

	loadCtx, loadCancel := context.WithTimeout(ctx, config.TestDuration)
	defer loadCancel()

	runLoadGenerationLoop(t, loadCtx, config, metrics, connections)
}

func runLoadGenerationLoop(t *testing.T, loadCtx context.Context, config *LoadTestConfig,
	metrics *LoadTestMetrics, connections []*websocket.Conn) {
	t.Helper()

	requestTicker := time.NewTicker(time.Duration(float64(time.Second) / config.RequestsPerSecond))
	defer requestTicker.Stop()

	var wg sync.WaitGroup

	semaphore := make(chan struct{}, config.ConcurrencyLimit)
	requestCount := 0

	for {
		select {
		case <-loadCtx.Done():
			t.Log("Load generation phase completed")
			wg.Wait()

			return
		case <-requestTicker.C:
			// Select a random connection
			conn := connections[requestCount%len(connections)]
			requestCount++

			wg.Add(1)

			go func(c *websocket.Conn, reqID int) {
				defer wg.Done()
				// Acquire semaphore
				semaphore <- struct{}{}

				defer func() { <-semaphore }()

				sendSingleRequest(loadCtx, c, config, metrics, reqID)
			}(conn, requestCount)
		}
	}
}

// sendSingleRequest sends a single request and measures latency.
func sendSingleRequest(ctx context.Context, conn *websocket.Conn, config *LoadTestConfig,
	metrics *LoadTestMetrics, reqID int, // Load test function handles complex request/response logic
) {
	requestStart := time.Now()
	message := createTestMessage(reqID, requestStart)

	// Send request with timeout
	requestCtx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()

	done := make(chan error, 1)
	go performRequestResponse(conn, message, done)

	select {
	case err := <-done:
		handleRequestCompletion(err, requestStart, metrics)
	case <-requestCtx.Done():
		handleRequestTimeout(metrics)
	}
}

func createTestMessage(reqID int, timestamp time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id":        reqID,
		"method":    "test/ping",
		"timestamp": timestamp.UnixNano(),
	}
}

func performRequestResponse(conn *websocket.Conn, message map[string]interface{}, done chan<- error) {
	err := conn.WriteJSON(message)
	if err != nil {
		done <- fmt.Errorf("write failed: %w", err)

		return
	}

	var response map[string]interface{}

	err = conn.ReadJSON(&response)
	done <- err
}

func handleRequestCompletion(err error, requestStart time.Time, metrics *LoadTestMetrics) {
	latency := time.Since(requestStart).Nanoseconds()

	if err != nil {
		recordFailedRequest(err, metrics)
	} else {
		recordSuccessfulRequest(latency, metrics)
	}

	atomic.AddInt64(&metrics.TotalRequests, 1)
}

func recordFailedRequest(err error, metrics *LoadTestMetrics) {
	atomic.AddInt64(&metrics.FailedRequests, 1)
	metrics.mu.Lock()
	metrics.RequestErrors[err.Error()]++
	metrics.mu.Unlock()
}

func recordSuccessfulRequest(latency int64, metrics *LoadTestMetrics) {
	atomic.AddInt64(&metrics.SuccessfulRequests, 1)
	atomic.AddInt64(&metrics.TotalLatency, latency)

	updateMinLatency(latency, metrics)
	updateMaxLatency(latency, metrics)

	metrics.mu.Lock()
	metrics.LatencyHistogram = append(metrics.LatencyHistogram, latency)
	metrics.mu.Unlock()
}

func updateMinLatency(latency int64, metrics *LoadTestMetrics) {
	for {
		current := atomic.LoadInt64(&metrics.MinLatency)
		if latency >= current || atomic.CompareAndSwapInt64(&metrics.MinLatency, current, latency) {
			break
		}
	}
}

func updateMaxLatency(latency int64, metrics *LoadTestMetrics) {
	for {
		current := atomic.LoadInt64(&metrics.MaxLatency)
		if latency <= current || atomic.CompareAndSwapInt64(&metrics.MaxLatency, current, latency) {
			break
		}
	}
}

func handleRequestTimeout(metrics *LoadTestMetrics) {
	atomic.AddInt64(&metrics.FailedRequests, 1)
	atomic.AddInt64(&metrics.TotalRequests, 1)

	metrics.mu.Lock()
	metrics.RequestErrors["timeout"]++
	metrics.mu.Unlock()
}

// cleanupConnections properly closes all connections.
func cleanupConnections(t *testing.T, connections []*websocket.Conn) {
	t.Helper()

	var wg sync.WaitGroup

	for i, conn := range connections {
		if conn == nil {
			continue
		}

		wg.Add(1)

		go func(index int, c *websocket.Conn) {
			defer wg.Done()

			// Send close message
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				t.Logf("Error sending close message to connection %d: %v", index, err)
			}

			// Close connection
			err = c.Close()
			if err != nil {
				t.Logf("Error closing connection %d: %v", index, err)
			}
		}(i, conn)
	}

	wg.Wait()
}

// calculateFinalMetrics computes final performance metrics.
func calculateFinalMetrics(metrics *LoadTestMetrics, testDuration time.Duration) {
	totalRequests := atomic.LoadInt64(&metrics.TotalRequests)
	if totalRequests > 0 {
		metrics.ThroughputPerSecond = float64(totalRequests) / testDuration.Seconds()
	}

	// Calculate percentiles
	metrics.mu.Lock()

	if len(metrics.LatencyHistogram) > 0 {
		sortedLatencies := make([]int64, len(metrics.LatencyHistogram))
		copy(sortedLatencies, metrics.LatencyHistogram)

		// Simple sort for percentile calculation
		for i := 0; i < len(sortedLatencies); i++ {
			for j := i + 1; j < len(sortedLatencies); j++ {
				if sortedLatencies[i] > sortedLatencies[j] {
					sortedLatencies[i], sortedLatencies[j] = sortedLatencies[j], sortedLatencies[i]
				}
			}
		}

		metrics.P50Latency = sortedLatencies[len(sortedLatencies)*50/100]
		metrics.P95Latency = sortedLatencies[len(sortedLatencies)*95/100]
		metrics.P99Latency = sortedLatencies[len(sortedLatencies)*99/100]
	}

	metrics.mu.Unlock()
}

// verifyLoadTestResults validates results using adaptive thresholds.
func verifyLoadTestResults(t *testing.T, config *LoadTestConfig, metrics *LoadTestMetrics) {
	t.Helper()

	totalConnections := atomic.LoadInt64(&metrics.SuccessfulConnections) + atomic.LoadInt64(&metrics.FailedConnections)
	totalRequests := atomic.LoadInt64(&metrics.TotalRequests)

	if totalConnections == 0 {
		t.Fatal("No connection attempts were made")
	}

	if totalRequests == 0 {
		t.Fatal("No requests were made")
	}

	// Calculate rates
	connectionSuccessRate := float64(atomic.LoadInt64(&metrics.SuccessfulConnections)) / float64(totalConnections)
	requestSuccessRate := float64(atomic.LoadInt64(&metrics.SuccessfulRequests)) / float64(totalRequests)
	requestErrorRate := float64(atomic.LoadInt64(&metrics.FailedRequests)) / float64(totalRequests)

	// Apply tolerance ranges
	minSuccessRateWithTolerance := config.MinSuccessRate - config.TolerancePercent
	maxErrorRateWithTolerance := config.MaxErrorRate + config.TolerancePercent

	// Validate connection success rate
	assert.GreaterOrEqual(t, connectionSuccessRate, minSuccessRateWithTolerance,
		"Connection success rate %.2f%% is below acceptable threshold %.2f%% (±%.1f%% tolerance)",
		connectionSuccessRate*100, config.MinSuccessRate*100, config.TolerancePercent*100)

	// Validate request success rate with system-aware thresholds
	assert.GreaterOrEqual(t, requestSuccessRate, minSuccessRateWithTolerance,
		"Request success rate %.2f%% is below acceptable threshold %.2f%% (±%.1f%% tolerance)",
		requestSuccessRate*100, config.MinSuccessRate*100, config.TolerancePercent*100)

	// Validate error rate
	assert.LessOrEqual(t, requestErrorRate, maxErrorRateWithTolerance,
		"Request error rate %.2f%% exceeds maximum allowed %.2f%% (+%.1f%% tolerance)",
		requestErrorRate*100, config.MaxErrorRate*100, config.TolerancePercent*100)

	// Performance assertions with system awareness
	if metrics.ThroughputPerSecond > 0 {
		expectedMinThroughput := config.RequestsPerSecond * config.SystemLoadFactor
		assert.GreaterOrEqual(t, metrics.ThroughputPerSecond, expectedMinThroughput,
			"Throughput %.2f req/sec is below expected minimum %.2f req/sec (adjusted for system load factor %.2f)",
			metrics.ThroughputPerSecond, expectedMinThroughput, config.SystemLoadFactor)
	}

	// Latency assertions with percentile-based validation
	if len(metrics.LatencyHistogram) > 0 {
		p95Latency := time.Duration(metrics.P95Latency)
		maxAcceptableLatency := calculateMaxAcceptableLatency(config)

		assert.LessOrEqual(t, p95Latency, maxAcceptableLatency,
			"P95 latency %v exceeds maximum acceptable %v", p95Latency, maxAcceptableLatency)
	}
}

// System information and adaptive threshold functions

func getSystemInfo() SystemInfo {
	return SystemInfo{
		NumCPU:       runtime.NumCPU(),
		MemoryGB:     getAvailableMemoryGB(),
		IsCI:         isRunningInCI(),
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
	}
}

func getAvailableMemoryGB() float64 {
	// This is a simplified implementation
	// In a real scenario, you'd want to check actual available memory
	return 8.0 // Default assumption
}

func isRunningInCI() bool {
	ciEnvVars := []string{"CI", "CONTINUOUS_INTEGRATION", "GITHUB_ACTIONS", "GITLAB_CI", "CIRCLECI"}
	for _, env := range ciEnvVars {
		if os.Getenv(env) != "" {
			return true
		}
	}

	return false
}

func calculateDefaultConnections(sysInfo SystemInfo) int {
	baseConnections := 1000

	if sysInfo.IsCI {
		return baseConnections / 2 // Reduce for CI environments
	}

	// Scale based on CPU count
	return baseConnections * sysInfo.NumCPU / 4
}

func calculateDefaultConnRate(sysInfo SystemInfo) int {
	baseRate := 100

	if sysInfo.IsCI {
		return baseRate / 2
	}

	return baseRate
}

func calculateDefaultDuration(sysInfo SystemInfo) time.Duration {
	if sysInfo.IsCI {
		return 2 * time.Minute // Shorter for CI
	}

	return 5 * time.Minute
}

func calculateSystemLoadFactor(sysInfo SystemInfo) float64 {
	factor := 1.0

	if sysInfo.IsCI {
		factor *= 0.7 // CI systems are typically more constrained
	}

	if sysInfo.NumCPU < 4 {
		factor *= 0.8 // Adjust for low-CPU systems
	}

	return factor
}

func calculateConcurrencyLimit(sysInfo SystemInfo) int {
	return sysInfo.NumCPU * 50 // 50 concurrent operations per CPU
}

func calculateMinSuccessRate(sysInfo SystemInfo) float64 {
	baseRate := 0.95 // 95%

	if sysInfo.IsCI {
		baseRate = 0.90 // Lower expectations for CI
	}

	return baseRate
}

func calculateMaxErrorRate(sysInfo SystemInfo) float64 {
	baseRate := 0.05 // 5%

	if sysInfo.IsCI {
		baseRate = 0.10 // Higher tolerance for CI
	}

	return baseRate
}

func calculateTolerancePercent(sysInfo SystemInfo) float64 {
	tolerance := 0.02 // ±2%

	if sysInfo.IsCI {
		tolerance = 0.05 // ±5% for CI
	}

	return tolerance
}

func calculateMaxAcceptableLatency(config *LoadTestConfig) time.Duration {
	baseLatency := 1 * time.Second

	if config.SystemLoadFactor < 0.8 {
		baseLatency = time.Duration(float64(baseLatency) * 1.5) // Allow higher latency on constrained systems
	}

	return baseLatency
}

// Environment variable helpers.
func getEnvString(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
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

func getEnvBool(key string, defaultValue bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}

	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}

	return defaultValue
}

func logDetailedMetrics(t *testing.T, config *LoadTestConfig, metrics *LoadTestMetrics, testDuration time.Duration) {
	t.Helper()
	t.Logf("=== Load Test Results ===")
	t.Logf("Test duration: %v", testDuration)
	t.Logf("Target connections: %d", config.TargetConnections)
	t.Logf("Successful connections: %d", atomic.LoadInt64(&metrics.SuccessfulConnections))
	t.Logf("Failed connections: %d", atomic.LoadInt64(&metrics.FailedConnections))
	t.Logf("Connection rate: %.2f conn/sec", metrics.ConnectionsPerSecond)
	t.Logf("Total requests: %d", atomic.LoadInt64(&metrics.TotalRequests))
	t.Logf("Successful requests: %d", atomic.LoadInt64(&metrics.SuccessfulRequests))
	t.Logf("Failed requests: %d", atomic.LoadInt64(&metrics.FailedRequests))
	t.Logf("Throughput: %.2f req/sec", metrics.ThroughputPerSecond)

	if metrics.P50Latency > 0 {
		t.Logf("Latency P50: %v", time.Duration(metrics.P50Latency))
		t.Logf("Latency P95: %v", time.Duration(metrics.P95Latency))
		t.Logf("Latency P99: %v", time.Duration(metrics.P99Latency))
		t.Logf("Min latency: %v", time.Duration(atomic.LoadInt64(&metrics.MinLatency)))
		t.Logf("Max latency: %v", time.Duration(atomic.LoadInt64(&metrics.MaxLatency)))
	}

	// Log error breakdown
	metrics.mu.RLock()

	if len(metrics.ConnectionErrors) > 0 {
		t.Logf("Connection errors:")

		for err, count := range metrics.ConnectionErrors {
			t.Logf("  %s: %d", err, count)
		}
	}

	if len(metrics.RequestErrors) > 0 {
		t.Logf("Request errors:")

		for err, count := range metrics.RequestErrors {
			t.Logf("  %s: %d", err, count)
		}
	}

	metrics.mu.RUnlock()
}

// generateLoadTestToken creates a proper test token (this should be implemented properly).
func generateLoadTestToken(_ *testing.T) string {
	if token := os.Getenv("LOAD_TEST_AUTH_TOKEN"); token != "" {
		return token
	}

	// Generate a proper JWT token for testing
	// This is a placeholder - implement actual token generation
	return "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJ0ZXN0IiwiYXVkIjoibG9hZC10ZXN0IixcImp4cFwiO" +
		"joxNzIyNTI2ODAwfQ.test-signature"
}
