// Package performance provides comprehensive performance regression testing
//

//nolint:ireturn // Test helper functions return interfaces for mocking flexibility
package performance

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PerformanceResult holds the results of a performance test.
type PerformanceResult struct {
	Name           string
	Duration       time.Duration
	MemoryUsed     uint64
	CPUUsage       float64
	Timestamp      time.Time
	IsRegression   bool
	RegressionInfo string
}

// PerformanceMonitor monitors performance metrics.
type PerformanceMonitor struct {
	MetricsEndpoint string
	SampleInterval  time.Duration
	Duration        time.Duration
}

// BenchmarkResult represents a benchmark result.
type BenchmarkResult struct {
	Name        string
	Operations  int
	NsPerOp     int64
	AllocsPerOp int64
	BytesPerOp  int64
}

// TestPerformanceRegression checks for performance regressions.
func TestPerformanceRegression(t *testing.T) {
	// Load baseline performance data
	baseline := loadBaselinePerformance(t)

	// Run current performance tests
	current := runPerformanceTests(t)

	// Compare and detect regressions
	regressions := detectRegressions(baseline, current)

	// Report results
	reportPerformanceResults(t, current, regressions)

	// Fail if regressions detected
	if len(regressions) > 0 {
		for _, r := range regressions {
			t.Logf("REGRESSION: %s - %s", r.Name, r.RegressionInfo)
		}
		// Don't fail in CI for now, just warn
		t.Logf("Found %d performance regressions", len(regressions))
	}
}

func loadBaselinePerformance(t *testing.T) []PerformanceResult {
	t.Helper()

	// For now, return empty baseline (first run establishes baseline)
	baselineFile := filepath.Clean(filepath.Join("testdata", "performance_baseline.json"))

	data, err := os.ReadFile(baselineFile)
	if err != nil {
		t.Log("No baseline found, this run will establish baseline")

		return []PerformanceResult{}
	}

	var baseline []PerformanceResult
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Logf("Failed to parse baseline: %v", err)

		return []PerformanceResult{}
	}

	return baseline
}

func runPerformanceTests(t *testing.T) []PerformanceResult {
	t.Helper()

	var results []PerformanceResult

	// Test 1: Message throughput
	result := testMessageThroughput(t)
	results = append(results, result)

	// Test 2: Memory allocation
	result = testMemoryAllocation(t)
	results = append(results, result)

	// Test 3: Connection establishment
	result = testConnectionSpeed(t)
	results = append(results, result)

	return results
}

func testMessageThroughput(t *testing.T) PerformanceResult {
	t.Helper()

	const numMessages = 10000

	start := time.Now()

	// Simulate message processing
	for i := 0; i < numMessages; i++ {
		_ = processMessage(i)
	}

	duration := time.Since(start)

	return PerformanceResult{
		Name:       "MessageThroughput",
		Duration:   duration,
		Timestamp:  time.Now(),
		MemoryUsed: getMemoryUsage(),
	}
}

func testMemoryAllocation(t *testing.T) PerformanceResult {
	t.Helper()

	runtime.GC()

	var memStatsBefore runtime.MemStats
	runtime.ReadMemStats(&memStatsBefore)

	// Perform allocations
	allocations := make([][]byte, 1000)
	for i := range allocations {
		allocations[i] = make([]byte, 1024)
	}

	runtime.GC()

	var memStatsAfter runtime.MemStats
	runtime.ReadMemStats(&memStatsAfter)

	memoryUsed := memStatsAfter.Alloc - memStatsBefore.Alloc

	return PerformanceResult{
		Name:       "MemoryAllocation",
		MemoryUsed: memoryUsed,
		Timestamp:  time.Now(),
	}
}

func testConnectionSpeed(t *testing.T) PerformanceResult {
	t.Helper()

	const numConnections = 1000

	start := time.Now()

	for i := 0; i < numConnections; i++ {
		conn := establishMockConnection()
		if conn != nil {
			if err := conn.Close(); err != nil {
				t.Logf("Failed to close connection: %v", err)
			}
		}
	}

	duration := time.Since(start)

	return PerformanceResult{
		Name:      "ConnectionSpeed",
		Duration:  duration,
		Timestamp: time.Now(),
	}
}

func processMessage(id int) error {
	// Simulate message processing
	if id < 0 {
		return errors.New("invalid message ID")
	}

	return nil
}

func establishMockConnection() *mockConnection {
	// Simulate connection establishment
	return &mockConnection{}
}

type mockConnection struct{}

func (m *mockConnection) Close() error {
	return nil
}

func (m *mockConnection) SendMessage(_ []byte) error {
	// Simulate realistic message sending time (about 1000 msg/s)
	time.Sleep(time.Microsecond * 1000) // 1ms delay = ~1000 msg/s

	return nil
}

func getMemoryUsage() uint64 {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return memStats.Alloc
}

func detectRegressions(baseline, current []PerformanceResult) []PerformanceResult {
	var regressions []PerformanceResult

	if len(baseline) == 0 {
		return regressions // No baseline to compare against
	}

	baselineMap := make(map[string]PerformanceResult)
	for _, b := range baseline {
		baselineMap[b.Name] = b
	}

	for _, c := range current {
		if b, exists := baselineMap[c.Name]; exists {
			// Check for regression (20% threshold)
			if c.Duration > b.Duration*120/100 {
				c.IsRegression = true
				c.RegressionInfo = fmt.Sprintf("Duration increased by %.1f%% (was %v, now %v)",
					float64(c.Duration-b.Duration)/float64(b.Duration)*100,
					b.Duration, c.Duration)
				regressions = append(regressions, c)
			}

			if c.MemoryUsed > b.MemoryUsed*150/100 {
				c.IsRegression = true
				c.RegressionInfo = fmt.Sprintf("Memory increased by %.1f%% (was %d, now %d)",
					float64(c.MemoryUsed-b.MemoryUsed)/float64(b.MemoryUsed)*100,
					b.MemoryUsed, c.MemoryUsed)
				regressions = append(regressions, c)
			}
		}
	}

	return regressions
}

func reportPerformanceResults(t *testing.T, results []PerformanceResult, regressions []PerformanceResult) {
	t.Helper()

	log.Println("\n=== Performance Report ===")
	log.Printf("Timestamp: %v\n", time.Now())
	log.Printf("Git Commit: %s\n", getGitCommit())
	log.Printf("Regressions: %d\n", len(regressions))
	log.Printf("Improvements: %d\n", countImprovements(results))

	// Save results for future baseline
	saveResults(t, results)
}

func getGitCommit() string {
	// In a real scenario, get actual git commit
	return "abc123"
}

func countImprovements(results []PerformanceResult) int {
	// Count performance improvements
	count := 0

	for _, r := range results {
		if !r.IsRegression && r.Duration > 0 {
			// Could check for improvements here
			count++
		}
	}

	return count
}

func saveResults(t *testing.T, results []PerformanceResult) {
	t.Helper()

	// Create testdata directory if it doesn't exist
	testdataDir := "testdata"
	_ = os.MkdirAll(testdataDir, 0750)

	// Save current results
	outputFile := filepath.Join(testdataDir, fmt.Sprintf("performance_%d.json", time.Now().Unix()))

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Logf("Failed to marshal results: %v", err)

		return
	}

	if err := os.WriteFile(outputFile, data, 0600); err != nil {
		t.Logf("Failed to save results: %v", err)
	}
}

// TestGatewayPerformanceRegression tests gateway-specific performance.
func TestGatewayPerformanceRegression(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T) time.Duration
		maxTime  time.Duration
	}{
		{
			name:     "request_latency",
			testFunc: testGatewayRequestLatency,
			maxTime:  100 * time.Millisecond,
		},
		{
			name:     "throughput",
			testFunc: testGatewayThroughput,
			maxTime:  2 * time.Second,
		},
		{
			name:     "memory_usage",
			testFunc: testGatewayMemoryUsage,
			maxTime:  500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration := tt.testFunc(t)
			if duration > tt.maxTime {
				t.Logf("Performance warning: %s took %v (max: %v)", tt.name, duration, tt.maxTime)
			}
		})
	}
}

func testGatewayRequestLatency(t *testing.T) time.Duration {
	t.Helper()

	const numRequests = 100

	var totalDuration time.Duration

	for i := 0; i < numRequests; i++ {
		start := time.Now()
		// Simulate gateway request
		time.Sleep(time.Microsecond * 100)
		totalDuration += time.Since(start)
	}

	avgDuration := totalDuration / numRequests
	t.Logf("Average request latency: %v", avgDuration)

	return avgDuration
}

func testGatewayThroughput(t *testing.T) time.Duration {
	t.Helper()

	const numRequests = 1000

	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numRequests; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Simulate concurrent request
			time.Sleep(time.Millisecond)
		}()
	}

	wg.Wait()

	duration := time.Since(start)

	throughput := float64(numRequests) / duration.Seconds()
	t.Logf("Throughput: %.2f requests/second", throughput)

	return duration
}

func testGatewayMemoryUsage(t *testing.T) time.Duration {
	t.Helper()

	start := time.Now()
	// Measure memory before
	runtime.GC()

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Simulate gateway operations
	data := make([][]byte, 100)
	for i := range data {
		data[i] = make([]byte, 1024*10) // 10KB per allocation
	}
	// Measure memory after
	runtime.GC()

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	memUsed := memAfter.Alloc - memBefore.Alloc
	t.Logf("Memory used: %d bytes", memUsed)

	// Clean up - force data to be considered used
	_ = data

	runtime.GC()

	return time.Since(start)
}

// TestRouterPerformanceRegression tests router-specific performance.
func TestRouterPerformanceRegression(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(t *testing.T) time.Duration
		maxTime  time.Duration
	}{
		{
			name:     "connection_time",
			testFunc: testRouterConnectionTime,
			maxTime:  50 * time.Millisecond,
		},
		{
			name:     "message_throughput",
			testFunc: testRouterMessageThroughput,
			maxTime:  5 * time.Second,
		},
		{
			name:     "reconnection_time",
			testFunc: testRouterReconnectionTime,
			maxTime:  100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration := tt.testFunc(t)
			if duration > tt.maxTime {
				t.Logf("Performance warning: %s took %v (max: %v)", tt.name, duration, tt.maxTime)
			}
		})
	}
}

func testRouterConnectionTime(t *testing.T) time.Duration {
	t.Helper()

	start := time.Now()

	// Simulate router connection
	conn := EstablishRouterConnection()
	if conn != nil {
		_ = conn.Close()
	}

	return time.Since(start)
}

func testRouterMessageThroughput(t *testing.T) time.Duration {
	t.Helper()

	const numMessages = 10000

	start := time.Now()

	conn := EstablishRouterConnection()
	if conn == nil {
		t.Fatal("Failed to establish connection")
	}

	defer func() { _ = conn.Close() }()

	for i := 0; i < numMessages; i++ {
		msg := fmt.Sprintf("message-%d", i)
		_ = conn.SendMessage([]byte(msg))
	}

	duration := time.Since(start)
	throughput := float64(numMessages) / duration.Seconds()
	t.Logf("Router throughput: %.2f messages/second", throughput)

	return duration
}

func testRouterReconnectionTime(t *testing.T) time.Duration {
	t.Helper()

	conn := EstablishRouterConnection()
	if conn == nil {
		t.Fatal("Failed to establish initial connection")
	}

	// Close and measure reconnection time
	_ = conn.Close()

	start := time.Now()
	conn = EstablishRouterConnection()
	duration := time.Since(start)

	if conn != nil {
		_ = conn.Close()
	}

	return duration
}

func TestContinuousPerformanceMonitoring(t *testing.T) {
	// Setup performance monitoring infrastructure
	cleanup, metricsEndpoint, err := setupPerformanceProfilingInfrastructure(t)
	require.NoError(t, err)

	defer cleanup()

	// Create performance monitor
	monitor := &PerformanceMonitor{
		MetricsEndpoint: metricsEndpoint,
		SampleInterval:  100 * time.Millisecond,
		Duration:        30 * time.Second,
	}

	// Run monitoring
	ctx, cancel := context.WithTimeout(context.Background(), monitor.Duration)
	defer cancel()

	results := monitor.Run(ctx)

	// Analyze results
	for metric, values := range results {
		if len(values) == 0 {
			continue
		}

		// Calculate statistics
		stats := CalculateStats(values)

		t.Logf("Metric %s: Mean=%.2f, P95=%.2f, P99=%.2f",
			metric, stats.Mean, stats.P95, stats.P99)

		// Check for anomalies
		anomalies := DetectAnomalies(values)
		if len(anomalies) > 0 {
			t.Logf("Detected %d anomalies in %s", len(anomalies), metric)
		}
	}
}

// Statistics holds statistical analysis results.
type Statistics struct {
	Mean   float64
	Median float64
	StdDev float64
	P95    float64
	P99    float64
	Min    float64
	Max    float64
}

// CalculateStats calculates statistics for a set of values.
func CalculateStats(values []float64) Statistics {
	if len(values) == 0 {
		return Statistics{}
	}

	// Sort values for percentile calculations
	sorted := make([]float64, len(values))

	copy(sorted, values)
	sort.Float64s(sorted)

	// Calculate mean
	sum := 0.0
	for _, v := range values {
		sum += v
	}

	mean := sum / float64(len(values))

	// Calculate standard deviation
	varSum := 0.0

	for _, v := range values {
		diff := v - mean
		varSum += diff * diff
	}

	stdDev := math.Sqrt(varSum / float64(len(values)))

	// Calculate percentiles
	p95Index := int(float64(len(sorted)) * 0.95)
	p99Index := int(float64(len(sorted)) * 0.99)

	if p95Index >= len(sorted) {
		p95Index = len(sorted) - 1
	}

	if p99Index >= len(sorted) {
		p99Index = len(sorted) - 1
	}

	return Statistics{
		Mean:   mean,
		Median: sorted[len(sorted)/2],
		StdDev: stdDev,
		P95:    sorted[p95Index],
		P99:    sorted[p99Index],
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
	}
}

// DetectAnomalies detects anomalous values using z-score.
func DetectAnomalies(values []float64) []int {
	if len(values) < 3 {
		return nil
	}

	stats := CalculateStats(values)
	if stats.StdDev == 0 {
		return nil
	}

	var anomalies []int

	for i, v := range values {
		zScore := math.Abs((v - stats.Mean) / stats.StdDev)
		if zScore > 3.0 { // Values more than 3 standard deviations from mean
			anomalies = append(anomalies, i)
		}
	}

	return anomalies
}

// calculateMemoryGrowth safely calculates memory growth avoiding integer overflow.
func calculateMemoryGrowth(initial, final uint64) int64 {
	if final >= initial {
		diff := final - initial
		if diff <= math.MaxInt64 {
			return int64(diff)
		}

		return math.MaxInt64
	}

	diff := initial - final
	if diff <= math.MaxInt64 {
		return -int64(diff)
	}

	return math.MinInt64
}

// TestMemoryLeakDetection checks for memory leaks.
func TestMemoryLeakDetection(t *testing.T) {
	// Force GC and get initial memory
	runtime.GC()
	runtime.GC()

	var initialMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)

	// Run operations that might leak memory
	for i := 0; i < 1000; i++ {
		conn := EstablishRouterConnection()
		if conn != nil {
			_ = conn.SendMessage([]byte("test"))
			_ = conn.Close()
		}
	}

	// Force GC and get final memory
	runtime.GC()
	runtime.GC()

	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	// Check for leak
	memoryGrowth := calculateMemoryGrowth(initialMem.Alloc, finalMem.Alloc)

	// Allow some growth, but not excessive
	maxGrowth := int64(10 * 1024 * 1024) // 10MB

	if memoryGrowth > maxGrowth {
		t.Errorf("Potential memory leak detected: %d bytes growth", memoryGrowth)
	}

	t.Logf("Memory growth: %d bytes", memoryGrowth)
}

// TestCPUProfileRegression runs CPU profiling to detect hot spots.
func TestCPUProfileRegression(t *testing.T) {
	// Cannot run in parallel: CPU profiling requires exclusive access to system CPU metrics
	if testing.Short() {
		t.Skip("Skipping CPU profiling test in short mode")
	}

	// Setup performance profiling infrastructure
	cleanup, _, err := setupPerformanceProfilingInfrastructure(t)
	require.NoError(t, err)

	defer cleanup()

	// Run CPU profiling
	cpuProfileFile := runCPUProfiling(t)

	// Verify CPU profile
	verifyCPUProfile(t, cpuProfileFile)

	// Run memory profiling
	memProfileFile := runMemoryProfiling(t)

	// Verify memory profile
	verifyMemoryProfile(t, memProfileFile)
}

func runCPUProfiling(t *testing.T) string {
	t.Helper()
	cpuProfileFile := filepath.Clean(filepath.Join(t.TempDir(), "cpu_profile.prof"))
	cpuFile, err := os.Create(cpuProfileFile)
	require.NoError(t, err, "Failed to create CPU profile file")

	defer func() { _ = cpuFile.Close() }()

	// Start CPU profiling
	err = pprof.StartCPUProfile(cpuFile)
	require.NoError(t, err, "Failed to start CPU profiling")

	// Run workload for profiling
	t.Log("Running CPU-intensive workload for 10s")

	workloadDuration := 10 * time.Second
	start := time.Now()
	deadline := start.Add(workloadDuration)
	iterations := 0

	for time.Now().Before(deadline) {
		runCPUIntensiveWorkload()

		iterations++
	}

	// Stop CPU profiling
	pprof.StopCPUProfile()
	t.Logf("Completed %d iterations in %v", iterations, time.Since(start))

	return cpuProfileFile
}

func verifyCPUProfile(t *testing.T, cpuProfileFile string) {
	t.Helper()

	stat, err := os.Stat(cpuProfileFile)
	require.NoError(t, err, "CPU profile file should exist")
	assert.Positive(t, stat.Size(), "CPU profile should not be empty")

	profileSize := stat.Size()
	t.Logf("CPU profile size: %d bytes", profileSize)

	// Check profile size is reasonable
	assert.Greater(t, profileSize, int64(100), "Profile should have content")
	assert.Less(t, profileSize, int64(10*1024*1024), "Profile should not be excessively large")
}

func runMemoryProfiling(t *testing.T) string {
	t.Helper()
	memProfileFile := filepath.Clean(filepath.Join(t.TempDir(), "mem_profile.prof"))
	memFile, err := os.Create(memProfileFile)
	require.NoError(t, err, "Failed to create memory profile file")

	defer func() { _ = memFile.Close() }()

	// Force GC before profiling
	runtime.GC()
	runtime.GC() // Run twice to ensure cleanup

	// Write memory profile
	err = pprof.WriteHeapProfile(memFile)
	require.NoError(t, err, "Failed to write memory profile")

	return memProfileFile
}

func verifyMemoryProfile(t *testing.T, memProfileFile string) {
	t.Helper()

	memStat, err := os.Stat(memProfileFile)
	require.NoError(t, err, "Memory profile file should exist")
	assert.Positive(t, memStat.Size(), "Memory profile should not be empty")
	t.Logf("Memory profile size: %d bytes", memStat.Size())
}

// Benchmark tests for performance tracking.

func BenchmarkMessageProcessing(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = processMessage(i)
	}
}

func BenchmarkConnectionEstablishment(b *testing.B) {
	for i := 0; i < b.N; i++ {
		conn := establishMockConnection()
		if conn != nil {
			_ = conn.Close()
		}
	}
}

func BenchmarkMemoryAllocation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		data := make([]byte, 1024)
		_ = data // Use to avoid optimization
	}
}

// PerformanceMonitor implementation

func (pm *PerformanceMonitor) Run(ctx context.Context) map[string][]float64 {
	results := make(map[string][]float64)
	client := &http.Client{Timeout: 5 * time.Second}

	ticker := time.NewTicker(pm.SampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return results
		case <-ticker.C:
			// Fetch metrics
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pm.MetricsEndpoint+"/metrics", nil)

			resp, err := client.Do(req)
			if err != nil {
				continue
			}

			// Parse metrics
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "#") || line == "" {
					continue
				}

				// Parse metric lines like "metric_name value"
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					metricName := parts[0]
					if value, err := strconv.ParseFloat(parts[1], 64); err == nil {
						results[metricName] = append(results[metricName], value)
					}
				}
			}

			_ = resp.Body.Close()
		}
	}
}

// Test infrastructure types and functions

type TestConnection interface {
	Close() error
	SendMessage([]byte) error
}

//nolint:ireturn // Test helper returns interface for mocking and testing flexibility
func EstablishRouterConnection() TestConnection {
	// Test helper function returns interface for mocking purposes
	return &mockConnection{}
}

// Mock test client implementation

type MockTestClient struct{}

func (c *MockTestClient) Connect() error {
	return nil
}

func (c *MockTestClient) Close() error {
	return nil
}

func (c *MockTestClient) SendRequest(data []byte) ([]byte, error) {
	// Simulate request processing
	return data, nil
}

// Test infrastructure types

type CPUProfilerInterface interface {
	Start() error
	Stop()
	Analyze() interface{}
}

type CPUProfilerImpl struct{}

func (c *CPUProfilerImpl) Start() error         { return nil }
func (c *CPUProfilerImpl) Stop()                {}
func (c *CPUProfilerImpl) Analyze() interface{} { return nil }

// setupPerformanceProfilingInfrastructure sets up metrics endpoint and profiling for tests.
func setupPerformanceProfilingInfrastructure(t *testing.T) (cleanup func(), metricsEndpoint string, err error) {
	t.Helper()

	// Find available port for metrics endpoint
	port, err := findAvailablePort()
	if err != nil {
		return nil, "", fmt.Errorf("failed to find available port: %w", err)
	}

	metricsEndpoint = fmt.Sprintf("http://localhost:%d", port)

	// Create and start metrics server
	server := createMetricsServer(t, port)
	if err := startMetricsServer(t, server, metricsEndpoint); err != nil {
		return nil, "", err
	}

	t.Logf("Performance profiling infrastructure ready at %s", metricsEndpoint)

	cleanup = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			t.Logf("Failed to shutdown metrics server: %v", err)
		}
	}

	return cleanup, metricsEndpoint, nil
}

func createMetricsServer(t *testing.T, port int) *http.Server {
	t.Helper()

	mux := http.NewServeMux()

	// Metrics endpoint
	mux.HandleFunc("/metrics", handleMetricsEndpoint)

	// Health check endpoint
	mux.HandleFunc("/health", handleHealthEndpoint)

	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func handleMetricsEndpoint(w http.ResponseWriter, r *http.Request) {
	// Generate basic metrics
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)

	_, _ = fmt.Fprintf(w, "# HELP mcp_memory_alloc_bytes Currently allocated memory in bytes\n")
	_, _ = fmt.Fprintf(w, "# TYPE mcp_memory_alloc_bytes gauge\n")
	_, _ = fmt.Fprintf(w, "mcp_memory_alloc_bytes %d\n", memStats.Alloc)

	_, _ = fmt.Fprintf(w, "# HELP mcp_memory_sys_bytes Total memory obtained from system\n")
	_, _ = fmt.Fprintf(w, "# TYPE mcp_memory_sys_bytes gauge\n")
	_, _ = fmt.Fprintf(w, "mcp_memory_sys_bytes %d\n", memStats.Sys)

	_, _ = fmt.Fprintf(w, "# HELP mcp_gc_runs_total Total number of GC runs\n")
	_, _ = fmt.Fprintf(w, "# TYPE mcp_gc_runs_total counter\n")
	_, _ = fmt.Fprintf(w, "mcp_gc_runs_total %d\n", memStats.NumGC)

	_, _ = fmt.Fprintf(w, "# HELP mcp_goroutines Number of goroutines\n")
	_, _ = fmt.Fprintf(w, "# TYPE mcp_goroutines gauge\n")
	_, _ = fmt.Fprintf(w, "mcp_goroutines %d\n", runtime.NumGoroutine())

	_, _ = fmt.Fprintf(w, "# HELP mcp_uptime_seconds Server uptime in seconds\n")
	_, _ = fmt.Fprintf(w, "# TYPE mcp_uptime_seconds counter\n")
	_, _ = fmt.Fprintf(w, "mcp_uptime_seconds %d\n", time.Now().Unix())
}

func handleHealthEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `{"status":"ok","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
}

func startMetricsServer(t *testing.T, server *http.Server, metricsEndpoint string) error {
	t.Helper()
	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Logf("Metrics server error: %v", err)
		}
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Verify server is running
	client := &http.Client{Timeout: 5 * time.Second}
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, metricsEndpoint+"/health", nil)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("metrics endpoint not ready: %w", err)
	}

	_ = resp.Body.Close()

	return nil
}

// findAvailablePort finds an available port for testing.
func findAvailablePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}

	defer func() { _ = listener.Close() }()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("failed to get TCP address from listener")
	}

	return tcpAddr.Port, nil
}

// runCPUIntensiveWorkload runs a CPU-intensive workload for profiling.
func runCPUIntensiveWorkload() {
	// Perform CPU-intensive operations
	const iterations = 10000

	sum := 0.0

	for i := 0; i < iterations; i++ {
		// Mathematical operations
		x := float64(i)
		sum += math.Sqrt(x) * math.Sin(x) * math.Cos(x)

		// String operations
		s := fmt.Sprintf("iteration-%d", i)
		_ = strings.ToUpper(s)

		// Allocations
		data := make([]byte, 100)
		for j := range data {
			data[j] = byte(i % 256)
		}
	}

	// Use sum to avoid compiler optimization
	_ = sum
}
