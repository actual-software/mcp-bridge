package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestPerformanceAndScale tests performance under load.
func TestPerformanceAndScale(t *testing.T) {
	// Skip if E2E tests are disabled
	if os.Getenv("SKIP_E2E_TESTS") == skipE2ETestsValue {
		t.Skip("Skipping E2E tests (SKIP_E2E_TESTS=true)")
	}

	if testing.Short() {
		t.Skip("Skipping performance tests in short mode")
	}

	logger := e2e.NewTestLogger()
	ctx := context.Background()

	// Start Docker services
	logger.Info("Starting Docker Compose stack for performance tests")

	stack := NewDockerStack(t)
	defer stack.Cleanup()

	err := stack.Start(ctx)
	require.NoError(t, err, "Failed to start Docker stack")

	// Wait for services to be ready
	err = stack.WaitForHTTPEndpoint(ctx, stack.GetGatewayHTTPURL()+"/health")
	require.NoError(t, err, "Gateway health check failed")

	// Create router and client
	router := NewRouterController(t, stack.GetGatewayURL())

	defer func() {
		if err := router.Stop(); err != nil {
			t.Logf("Failed to stop router: %v", err)
		}
	}()

	err = router.BuildRouter()
	require.NoError(t, err, "Failed to build router binary")

	err = router.Start()
	require.NoError(t, err, "Failed to start router")

	client := NewMCPClient(router)

	// Initialize the client
	_, err = client.Initialize()
	require.NoError(t, err, "Failed to initialize client")

	t.Run("HighThroughputTesting", func(t *testing.T) {
		testHighThroughputTesting(t, client)
	})

	t.Run("ConnectionPooling", func(t *testing.T) {
		testConnectionPooling(t, client, stack)
	})

	t.Run("MemoryUsage", func(t *testing.T) {
		testMemoryUsage(t, client, stack)
	})

	t.Run("LatencyUnderLoad", func(t *testing.T) {
		testLatencyUnderLoad(t, client)
	})
}

func testHighThroughputTesting(t *testing.T, client *MCPClient) {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping high throughput test in short mode")
	}

	numRequests := 1000
	concurrency := 50

	start := time.Now()
	results := executeConcurrentRequests(t, client, numRequests, concurrency)
	duration := time.Since(start)

	successCount, errorCount := countResults(results)
	validateThroughputResults(t, successCount, errorCount, numRequests, duration)
}

func executeConcurrentRequests(t *testing.T, client *MCPClient, numRequests, concurrency int) chan error {
	t.Helper()

	var wg sync.WaitGroup

	results := make(chan error, numRequests)
	semaphore := make(chan struct{}, concurrency)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)

		go func(index int) {
			defer wg.Done()

			semaphore <- struct{}{} // Acquire

			defer func() { <-semaphore }() // Release

			resp, err := client.CallTool("echo", map[string]interface{}{
				"message": fmt.Sprintf("throughput test %d", index),
			})
			if err != nil {
				results <- err

				return
			}

			err = AssertToolCallSuccess(resp)
			results <- err
		}(i)
	}

	wg.Wait()
	close(results)

	return results
}

func countResults(results chan error) (successCount, errorCount int) {
	for err := range results {
		if err != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	return successCount, errorCount
}

func validateThroughputResults(t *testing.T, successCount, errorCount, numRequests int, duration time.Duration) {
	t.Helper()

	throughput := float64(successCount) / duration.Seconds()
	t.Logf("High throughput test: %d/%d successful in %v (%.2f req/sec)",
		successCount, numRequests, duration, throughput)

	// Expect at least 80% success rate and reasonable throughput
	successRate := float64(successCount) / float64(numRequests)
	assert.GreaterOrEqual(t, successRate, 0.8, "Should have at least 80%% success rate")
	assert.GreaterOrEqual(t, throughput, 10.0, "Should achieve at least 10 req/sec")
}

func testConnectionPooling(t *testing.T, client *MCPClient, stack *DockerStack) {
	t.Helper()
	// Test connection reuse and pooling

	connectionMetrics := executePoolingRequests(client)
	analyzePoolingMetrics(t, connectionMetrics)
}

func executePoolingRequests(client *MCPClient) chan map[string]interface{} {
	var wg sync.WaitGroup

	connectionMetrics := make(chan map[string]interface{}, 20)

	// Make concurrent requests to test pooling
	for i := 0; i < 20; i++ {
		wg.Add(1)

		go func(index int) {
			defer wg.Done()

			start := time.Now()
			resp, err := client.CallTool("echo", map[string]interface{}{
				"message": fmt.Sprintf("pooling test %d", index),
			})
			duration := time.Since(start)

			metrics := map[string]interface{}{
				"index":    index,
				"duration": duration,
				"error":    err,
				"success":  err == nil && resp != nil,
			}
			connectionMetrics <- metrics
		}(i)
	}

	wg.Wait()
	close(connectionMetrics)

	return connectionMetrics
}

func analyzePoolingMetrics(t *testing.T, connectionMetrics chan map[string]interface{}) {
	t.Helper()

	var (
		totalDuration time.Duration
		successCount  int
	)

	for metrics := range connectionMetrics {
		if success, ok := metrics["success"].(bool); ok && success {
			successCount++

			if duration, ok := metrics["duration"].(time.Duration); ok {
				totalDuration += duration
			}
		}
	}

	if successCount > 0 {
		avgDuration := totalDuration / time.Duration(successCount)
		t.Logf("Connection pooling: %d successful requests, avg duration: %v",
			successCount, avgDuration)

		// Pooled connections should be reasonably fast
		assert.Less(t, avgDuration, 5*time.Second, "Pooled connections should be fast")
	}
}

func testMemoryUsage(t *testing.T, client *MCPClient, stack *DockerStack) {
	t.Helper()
	// Get baseline memory usage
	baselineMemory := getContainerMemoryUsage(t, stack, "gateway")

	// Perform many operations
	for i := 0; i < 100; i++ {
		_, _ = client.CallTool("echo", map[string]interface{}{
			"message": fmt.Sprintf("memory test %d", i),
		})
	}

	// Check memory after operations
	afterMemory := getContainerMemoryUsage(t, stack, "gateway")

	memoryIncrease := afterMemory - baselineMemory
	t.Logf("Memory usage: baseline=%dMB, after=%dMB, increase=%dMB",
		baselineMemory/1024/1024, afterMemory/1024/1024, memoryIncrease/1024/1024)

	// Memory increase should be reasonable (less than 100MB for 100 requests)
	assert.Less(t, memoryIncrease, int64(100*1024*1024),
		"Memory increase should be reasonable")
}

func testLatencyUnderLoad(t *testing.T, client *MCPClient) {
	t.Helper()

	numRequests := 50
	latencies := make([]time.Duration, 0, numRequests)

	for i := 0; i < numRequests; i++ {
		start := time.Now()
		resp, err := client.CallTool("echo", map[string]interface{}{
			"message": fmt.Sprintf("latency test %d", i),
		})
		latency := time.Since(start)

		if err == nil && resp != nil {
			latencies = append(latencies, latency)
		}
	}

	if len(latencies) > 0 {
		// Calculate statistics
		sort.Slice(latencies, func(i, j int) bool {
			return latencies[i] < latencies[j]
		})

		p50 := latencies[len(latencies)/2]
		p95 := latencies[int(float64(len(latencies))*0.95)]
		p99 := latencies[int(float64(len(latencies))*0.99)]

		t.Logf("Latency under load: P50=%v, P95=%v, P99=%v", p50, p95, p99)

		// Latency should be reasonable
		assert.Less(t, p95, 2*time.Second, "P95 latency should be under 2s")
		assert.Less(t, p99, 5*time.Second, "P99 latency should be under 5s")
	}
}
