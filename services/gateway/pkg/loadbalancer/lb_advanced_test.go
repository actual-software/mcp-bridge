package loadbalancer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
)

const (
	testIterations          = 100
	testMaxIterations       = 1000
	testTimeout             = 50
	httpStatusOK            = 200
	httpStatusInternalError = 500
)

// TestHybridLoadBalancer_BackendHealthIntegration_Advanced tests integration with backend health.
func TestHybridLoadBalancer_BackendHealthIntegration_Advanced(t *testing.T) {
	tests := []struct {
		name      string
		endpoints []*discovery.Endpoint
		expected  int // expected number of healthy endpoints returned
	}{
		{
			name: "all healthy endpoints",
			endpoints: []*discovery.Endpoint{
				{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testIterations},
				{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: true, Weight: testIterations},
				{Service: "svc3", Address: "192.168.1.3", Port: 8080, Healthy: true, Weight: testIterations},
			},
			expected: 3,
		},
		{
			name: "mixed health endpoints",
			endpoints: []*discovery.Endpoint{
				{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testIterations},
				{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: false, Weight: testIterations},
				{Service: "svc3", Address: "192.168.1.3", Port: 8080, Healthy: true, Weight: testIterations},
			},
			expected: 2,
		},
		{
			name: "all unhealthy endpoints",
			endpoints: []*discovery.Endpoint{
				{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: false, Weight: testIterations},
				{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: false, Weight: testIterations},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := HybridConfig{
				Strategy:    "round_robin",
				HealthAware: true,
			}
			lb := NewHybridLoadBalancer(tt.endpoints, config)

			// Test round robin with health awareness
			healthyCount := 0

			for i := 0; i < len(tt.endpoints)*2; i++ {
				endpoint := lb.Next()
				if endpoint != nil {
					assert.True(t, endpoint.Healthy, "Only healthy endpoints should be returned")

					healthyCount++
				}
			}

			if tt.expected > 0 {
				assert.Positive(t, healthyCount, "Should return healthy endpoints")
			} else {
				assert.Equal(t, 0, healthyCount, "Should return no endpoints when all unhealthy")
			}
		})
	}
}

// TestHybridLoadBalancer_TrafficDistribution_Advanced tests traffic distribution patterns.
func TestHybridLoadBalancer_TrafficDistribution_Advanced(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testTimeout},
		{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: true, Weight: 30},
		{Service: "svc3", Address: "192.168.1.3", Port: 8080, Healthy: true, Weight: 20},
	}

	config := HybridConfig{
		Strategy:    "weighted",
		HealthAware: true,
	}
	lb := NewHybridLoadBalancer(endpoints, config)

	// Test weighted distribution
	distribution := make(map[string]int)
	iterations := testMaxIterations

	for i := 0; i < iterations; i++ {
		endpoint := lb.Next()

		require.NotNil(t, endpoint)

		distribution[endpoint.Service]++
	}

	// Verify approximate weight distribution
	total := float64(iterations)
	svc1Ratio := float64(distribution["svc1"]) / total
	svc2Ratio := float64(distribution["svc2"]) / total
	svc3Ratio := float64(distribution["svc3"]) / total

	// Allow for some variance (±0.1)

	assert.InDelta(t, 0.5, svc1Ratio, 0.1, "svc1 should get ~testTimeout%% of traffic")
	assert.InDelta(t, 0.3, svc2Ratio, 0.1, "svc2 should get ~30%% of traffic")
	assert.InDelta(t, 0.2, svc3Ratio, 0.1, "svc3 should get ~20%% of traffic")
}

// TestHybridLoadBalancer_ConcurrentTrafficDistribution_Advanced tests concurrent load balancing.
func TestHybridLoadBalancer_ConcurrentTrafficDistribution_Advanced(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testIterations},
		{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: true, Weight: testIterations},
		{Service: "svc3", Address: "192.168.1.3", Port: 8080, Healthy: true, Weight: testIterations},
	}

	config := HybridConfig{
		Strategy:    "round_robin",
		HealthAware: true,
	}
	lb := NewHybridLoadBalancer(endpoints, config)

	// Concurrent access test
	const numGoroutines = testTimeout

	const requestsPerGoroutine = testIterations

	results := make(chan string, numGoroutines*requestsPerGoroutine)

	var wg sync.WaitGroup

	// Launch concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				endpoint := lb.Next()
				if endpoint != nil {
					results <- endpoint.Service
				}
			}
		}()
	}

	wg.Wait()
	close(results)

	// Count distribution
	distribution := make(map[string]int)
	totalRequests := 0

	for service := range results {
		distribution[service]++
		totalRequests++
	}

	// Verify that all requests were handled
	assert.Equal(t, numGoroutines*requestsPerGoroutine, totalRequests)

	// Verify reasonably even distribution (should be roughly 1/3 each)

	for service, count := range distribution {
		ratio := float64(count) / float64(totalRequests)

		assert.InDelta(t, 0.33, ratio, 0.1, "Service %s should get roughly 1/3 of traffic", service)
	}
}

// TestHybridLoadBalancer_ProtocolGrouping_Advanced tests protocol-based grouping.
func TestHybridLoadBalancer_ProtocolGrouping_Advanced(t *testing.T) {
	lb, endpoints := setupProtocolGroupingTest(t)

	testHTTPProtocolPreference(t, lb)
	testWebSocketFallback(t, endpoints)
	testSSELastResort(t, endpoints)
}

func setupProtocolGroupingTest(t *testing.T) (*HybridLoadBalancer, []*discovery.Endpoint) {
	t.Helper()

	endpoints := []*discovery.Endpoint{
		{
			Service: "http-svc1", Address: "192.168.1.1", Port: 8080, Healthy: true,
			Weight: testIterations, Scheme: "http",
			Metadata: map[string]string{"protocol": "http"},
		},
		{
			Service: "http-svc2", Address: "192.168.1.2", Port: 8080, Healthy: true,
			Weight: testIterations, Scheme: "http",
			Metadata: map[string]string{"protocol": "http"},
		},
		{
			Service: "ws-svc1", Address: "192.168.1.3", Port: 8080, Healthy: true,
			Weight: testIterations, Scheme: "ws",
			Metadata: map[string]string{"protocol": "websocket"},
		},
		{
			Service: "ws-svc2", Address: "192.168.1.4", Port: 8080, Healthy: true,
			Weight: testIterations, Scheme: "wss",
			Metadata: map[string]string{"protocol": "websocket"},
		},
		{
			Service: "sse-svc1", Address: "192.168.1.5", Port: 8080, Healthy: true,
			Weight: testIterations, Scheme: "http", Path: "/events",
			Metadata: map[string]string{"protocol": "sse"},
		},
	}

	config := HybridConfig{
		Strategy:           "round_robin",
		HealthAware:        true,
		ProtocolPreference: []string{"http", "websocket", "sse"},
	}
	lb := NewHybridLoadBalancer(endpoints, config)

	// Test protocol statistics
	stats := lb.GetProtocolStats()

	assert.NotNil(t, stats)

	return lb, endpoints
}

func testHTTPProtocolPreference(t *testing.T, lb *HybridLoadBalancer) {
	t.Helper()

	protocolCounts := make(map[string]int)

	for i := 0; i < 10; i++ {
		endpoint := lb.Next()

		require.NotNil(t, endpoint)

		if endpoint.Metadata != nil {
			protocol := endpoint.Metadata["protocol"]
			protocolCounts[protocol]++
		}
	}

	// Should only get HTTP endpoints due to protocol preference
	assert.Positive(t, protocolCounts["http"], "Should route to HTTP endpoints when available")
	assert.Equal(t, 0, protocolCounts["websocket"], "Should not route to WebSocket when HTTP is available")
	assert.Equal(t, 0, protocolCounts["sse"], "Should not route to SSE when HTTP is available")
}

func testWebSocketFallback(t *testing.T, originalEndpoints []*discovery.Endpoint) {
	t.Helper()

	// Create unhealthy HTTP endpoints
	endpointsNoHTTP := make([]*discovery.Endpoint, len(originalEndpoints))
	copy(endpointsNoHTTP, originalEndpoints)

	for _, ep := range endpointsNoHTTP {
		if ep.Metadata["protocol"] == "http" {
			ep.Healthy = false
		}
	}

	config := HybridConfig{
		Strategy:           "round_robin",
		HealthAware:        true,
		ProtocolPreference: []string{"http", "websocket", "sse"},
	}
	lb2 := NewHybridLoadBalancer(endpointsNoHTTP, config)
	protocolCounts2 := make(map[string]int)

	for i := 0; i < 10; i++ {
		endpoint := lb2.Next()

		require.NotNil(t, endpoint)

		if endpoint.Metadata != nil {
			protocol := endpoint.Metadata["protocol"]
			protocolCounts2[protocol]++
		}
	}

	// Should fall back to WebSocket when HTTP is unhealthy
	assert.Equal(t, 0, protocolCounts2["http"], "Should not route to unhealthy HTTP endpoints")
	assert.Positive(t, protocolCounts2["websocket"], "Should route to WebSocket when HTTP is unhealthy")
	assert.Equal(t, 0, protocolCounts2["sse"], "Should not route to SSE when WebSocket is available")
}

func testSSELastResort(t *testing.T, originalEndpoints []*discovery.Endpoint) {
	t.Helper()

	// Create endpoints with only SSE healthy
	endpointsOnlySSE := []*discovery.Endpoint{
		{
			Service: "http-svc1", Address: "192.168.1.1", Port: 8080, Healthy: false,
			Weight: testIterations, Scheme: "http",
			Metadata: map[string]string{"protocol": "http"},
		},
		{
			Service: "ws-svc1", Address: "192.168.1.3", Port: 8080, Healthy: false,
			Weight: testIterations, Scheme: "ws",
			Metadata: map[string]string{"protocol": "websocket"},
		},
		{
			Service: "sse-svc1", Address: "192.168.1.5", Port: 8080, Healthy: true,
			Weight: testIterations, Scheme: "http", Path: "/events",
			Metadata: map[string]string{"protocol": "sse"},
		},
	}

	config := HybridConfig{
		Strategy:           "round_robin",
		HealthAware:        true,
		ProtocolPreference: []string{"http", "websocket", "sse"},
	}
	lb3 := NewHybridLoadBalancer(endpointsOnlySSE, config)
	protocolCounts3 := make(map[string]int)

	for i := 0; i < 10; i++ {
		endpoint := lb3.Next()

		require.NotNil(t, endpoint)

		if endpoint.Metadata != nil {
			protocol := endpoint.Metadata["protocol"]
			protocolCounts3[protocol]++
		}
	}

	// Should route to SSE when it's the only healthy protocol
	assert.Equal(t, 0, protocolCounts3["http"], "Should not route to unhealthy HTTP endpoints")
	assert.Equal(t, 0, protocolCounts3["websocket"], "Should not route to unhealthy WebSocket endpoints")
	assert.Positive(t, protocolCounts3["sse"], "Should route to SSE when it's the only healthy option")
}

// TestLoadBalancer_HealthRecoveryPatterns_Advanced tests health recovery patterns.
func TestLoadBalancer_HealthRecoveryPatterns_Advanced(t *testing.T) {
	lb := setupHealthRecoveryTest(t)

	testInitialHealthyState(t, lb)
	testServiceDegradation(t, lb)
	testServiceRecovery(t, lb)
}

func setupHealthRecoveryTest(t *testing.T) *HybridLoadBalancer {
	t.Helper()

	initialEndpoints := []*discovery.Endpoint{
		{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testIterations},
		{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: true, Weight: testIterations},
		{Service: "svc3", Address: "192.168.1.3", Port: 8080, Healthy: true, Weight: testIterations},
	}

	config := HybridConfig{
		Strategy:    "round_robin",
		HealthAware: true,
	}

	return NewHybridLoadBalancer(initialEndpoints, config)
}

func testInitialHealthyState(t *testing.T, lb *HybridLoadBalancer) {
	t.Helper()

	healthyCount := 0

	for i := 0; i < 30; i++ {
		endpoint := lb.Next()
		if endpoint != nil && endpoint.Healthy {
			healthyCount++
		}
	}

	assert.Equal(t, 30, healthyCount, "All requests should go to healthy endpoints")
}

func testServiceDegradation(t *testing.T, lb *HybridLoadBalancer) {
	t.Helper()

	// Simulate one service becoming unhealthy
	degradedEndpoints := []*discovery.Endpoint{
		{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: false, Weight: testIterations}, // unhealthy
		{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: true, Weight: testIterations},
		{Service: "svc3", Address: "192.168.1.3", Port: 8080, Healthy: true, Weight: testIterations},
	}

	lb.UpdateEndpoints(degradedEndpoints)

	// After degradation - should only route to healthy services
	serviceCounts := make(map[string]int)

	for i := 0; i < testIterations; i++ {
		endpoint := lb.Next()

		require.NotNil(t, endpoint)
		assert.True(t, endpoint.Healthy, "Should only return healthy endpoints")

		serviceCounts[endpoint.Service]++
	}

	// Should not route to svc1 (unhealthy)

	assert.Equal(t, 0, serviceCounts["svc1"], "Should not route to unhealthy service")
	assert.Positive(t, serviceCounts["svc2"], "Should route to healthy svc2")
	assert.Positive(t, serviceCounts["svc3"], "Should route to healthy svc3")
}

func testServiceRecovery(t *testing.T, lb *HybridLoadBalancer) {
	t.Helper()

	// Simulate recovery
	recoveredEndpoints := []*discovery.Endpoint{
		{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testIterations}, // recovered
		{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: true, Weight: testIterations},
		{Service: "svc3", Address: "192.168.1.3", Port: 8080, Healthy: true, Weight: testIterations},
	}

	lb.UpdateEndpoints(recoveredEndpoints)

	// After recovery - should route to all services again
	recoveredServiceCounts := make(map[string]int)

	for i := 0; i < 300; i++ {
		endpoint := lb.Next()

		require.NotNil(t, endpoint)
		assert.True(t, endpoint.Healthy, "All endpoints should be healthy")

		recoveredServiceCounts[endpoint.Service]++
	}

	// All services should receive traffic after recovery
	assert.Positive(t, recoveredServiceCounts["svc1"], "svc1 should receive traffic after recovery")
	assert.Positive(t, recoveredServiceCounts["svc2"], "svc2 should continue receiving traffic")
	assert.Positive(t, recoveredServiceCounts["svc3"], "svc3 should continue receiving traffic")
}

// TestLoadBalancer_WeightDistributionAccuracy_Advanced tests accuracy of weight-based distribution.
func TestLoadBalancer_WeightDistributionAccuracy_Advanced(t *testing.T) {
	testCases := []struct {
		name      string
		weights   []int
		tolerance float64
	}{
		{"equal_weights", []int{testIterations, testIterations, testIterations}, 0.1},
		{"proportional_weights", []int{300, httpStatusOK, testIterations}, 0.1},
		{"extreme_weights", []int{testMaxIterations, 10, 1}, 0.15}, // Higher tolerance for extreme ratios
		{"single_weight", []int{httpStatusInternalError}, 0.0},     // Should get testIterations% of traffic
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			endpoints := make([]*discovery.Endpoint, len(tc.weights))
			totalWeight := 0

			for i, weight := range tc.weights {
				endpoints[i] = &discovery.Endpoint{
					Service: "svc" + string(rune('1'+i)),
					Address: "192.168.1." + string(rune('1'+i)),
					Port:    8080,
					Healthy: true,
					Weight:  weight,
				}
				totalWeight += weight
			}

			config := HybridConfig{
				Strategy:    "weighted",
				HealthAware: true,
			}
			lb := NewHybridLoadBalancer(endpoints, config)

			distribution := make(map[string]int)
			iterations := 10000

			for i := 0; i < iterations; i++ {
				endpoint := lb.Next()

				require.NotNil(t, endpoint)

				distribution[endpoint.Service]++
			}

			// Verify distribution accuracy

			for i, weight := range tc.weights {
				serviceName := "svc" + string(rune('1'+i))
				expectedRatio := float64(weight) / float64(totalWeight)
				actualRatio := float64(distribution[serviceName]) / float64(iterations)

				assert.InDelta(t, expectedRatio, actualRatio, tc.tolerance,
					"Service %s: expected %.3f, got %.3f", serviceName, expectedRatio, actualRatio)
			}
		})
	}
}

// TestHybridLoadBalancer_EdgeCases_Advanced tests various edge cases.
func TestHybridLoadBalancer_EdgeCases_Advanced(t *testing.T) {
	tests := getHybridLoadBalancerEdgeCaseTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := tt.setup()
			tt.testFunc(t, lb)
		})
	}
}

func getHybridLoadBalancerEdgeCaseTests() []struct {
	name     string
	setup    func() *HybridLoadBalancer
	testFunc func(*testing.T, *HybridLoadBalancer)
} {
	return []struct {
		name     string
		setup    func() *HybridLoadBalancer
		testFunc func(*testing.T, *HybridLoadBalancer)
	}{
		{
			name:     "zero weight endpoint",
			setup:    setupZeroWeightEndpoint,
			testFunc: testZeroWeightEndpoint,
		},
		{
			name:     "empty endpoints list",
			setup:    setupEmptyEndpointsList,
			testFunc: testEmptyEndpointsList,
		},
		{
			name:     "single healthy endpoint",
			setup:    setupSingleHealthyEndpoint,
			testFunc: testSingleHealthyEndpoint,
		},
	}
}

func setupZeroWeightEndpoint() *HybridLoadBalancer {
	endpoints := []*discovery.Endpoint{
		{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: 0},
	}
	config := HybridConfig{Strategy: "weighted", HealthAware: true}

	return NewHybridLoadBalancer(endpoints, config)
}

func testZeroWeightEndpoint(t *testing.T, lb *HybridLoadBalancer) {
	t.Helper()
	endpoint := lb.Next()
	assert.NotNil(t, endpoint, "Should return endpoint even with zero weight")
}

func setupEmptyEndpointsList() *HybridLoadBalancer {
	config := HybridConfig{Strategy: "round_robin", HealthAware: true}

	return NewHybridLoadBalancer([]*discovery.Endpoint{}, config)
}

func testEmptyEndpointsList(t *testing.T, lb *HybridLoadBalancer) {
	t.Helper()
	endpoint := lb.Next()
	assert.Nil(t, endpoint, "Should return nil when no endpoints available")
}

func setupSingleHealthyEndpoint() *HybridLoadBalancer {
	endpoints := []*discovery.Endpoint{
		{Service: "only-svc", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testIterations},
	}
	config := HybridConfig{Strategy: "round_robin", HealthAware: true}

	return NewHybridLoadBalancer(endpoints, config)
}

func testSingleHealthyEndpoint(t *testing.T, lb *HybridLoadBalancer) {
	t.Helper()
	// Should consistently return the same endpoint
	for i := 0; i < 10; i++ {
		endpoint := lb.Next()
		require.NotNil(t, endpoint)
		assert.Equal(t, "only-svc", endpoint.Service)
	}
}

// TestLoadBalancer_PerformanceUnderLoad_Advanced tests performance under load.
func TestLoadBalancer_PerformanceUnderLoad_Advanced(t *testing.T) {
	// Set up performance test with many endpoints
	endpoints := createManyTestEndpoints()
	algorithms := []string{"round_robin", "least_connections", "weighted"}
	requests := 10000

	// Test performance for each algorithm

	for _, algorithm := range algorithms {
		t.Run(algorithm, func(t *testing.T) {
			runPerformanceTestForAlgorithm(t, algorithm, endpoints, requests)
		})
	}
}

func createManyTestEndpoints() []*discovery.Endpoint {
	const numEndpoints = testTimeout

	endpoints := make([]*discovery.Endpoint, numEndpoints)

	for i := 0; i < numEndpoints; i++ {
		endpoints[i] = &discovery.Endpoint{
			Service: "svc-" + string(rune('a'+i%26)),
			Address: "192.168.1." + string(rune('1'+i%254)),
			Port:    8080 + i,
			Healthy: i%10 != 0, // 90% healthy
			Weight:  testTimeout + i%testIterations,
		}
	}

	return endpoints
}

func runPerformanceTestForAlgorithm(t *testing.T, algorithm string, endpoints []*discovery.Endpoint, requests int) {
	t.Helper()
	config := HybridConfig{
		Strategy:    algorithm,
		HealthAware: true,
	}
	lb := NewHybridLoadBalancer(endpoints, config)

	start := time.Now()
	successCount := executePerformanceRequests(lb, requests)
	duration := time.Since(start)

	validatePerformanceResults(t, algorithm, successCount, requests, duration)
}

func executePerformanceRequests(lb *HybridLoadBalancer, requests int) int {
	successCount := 0

	for i := 0; i < requests; i++ {
		endpoint := lb.Next()
		if endpoint != nil && endpoint.Healthy {
			successCount++
		}
	}

	return successCount
}

func validatePerformanceResults(t *testing.T, algorithm string, successCount, requests int, duration time.Duration) {
	t.Helper()
	// Performance assertions
	assert.Greater(t, successCount, requests*8/10, "Should succeed at least 80%% of requests")
	assert.Less(t, duration, 5*time.Second, "Should complete %d requests within 5 seconds", requests)

	// Calculate and verify throughput
	rps := float64(requests) / duration.Seconds()

	assert.Greater(t, rps, 1000.0, "Should handle at least 1000 requests per second")

	t.Logf("Algorithm: %s, RPS: %.2f, Success Rate: %.2f%%",
		algorithm, rps, float64(successCount)/float64(requests)*testIterations)
}

// TestHybridLoadBalancer_UpdateEndpoints_Advanced tests dynamic endpoint updates.
func TestHybridLoadBalancer_UpdateEndpoints_Advanced(t *testing.T) {
	// Start with initial endpoints
	initialEndpoints := []*discovery.Endpoint{
		{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testIterations},
		{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: true, Weight: testIterations},
	}

	config := HybridConfig{
		Strategy:    "round_robin",
		HealthAware: true,
	}
	lb := NewHybridLoadBalancer(initialEndpoints, config)

	// Verify initial state
	initialDistribution := make(map[string]int)

	for i := 0; i < testIterations; i++ {
		endpoint := lb.Next()

		require.NotNil(t, endpoint)

		initialDistribution[endpoint.Service]++
	}

	assert.Positive(t, initialDistribution["svc1"])
	assert.Positive(t, initialDistribution["svc2"])

	// Update with new endpoints
	updatedEndpoints := []*discovery.Endpoint{
		{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testIterations},
		{
			Service: "svc3", Address: "192.168.1.3", Port: 8080, Healthy: true, Weight: testIterations,
		}, // svc2 removed, svc3 added
		{Service: "svc4", Address: "192.168.1.4", Port: 8080, Healthy: true, Weight: testIterations}, // svc4 added
	}

	lb.UpdateEndpoints(updatedEndpoints)

	// Verify updated state
	updatedDistribution := make(map[string]int)

	for i := 0; i < 300; i++ {
		endpoint := lb.Next()

		require.NotNil(t, endpoint)

		updatedDistribution[endpoint.Service]++
	}

	// Should not route to svc2 (removed)

	assert.Equal(t, 0, updatedDistribution["svc2"], "Should not route to removed service")
	// Should route to remaining and new services
	assert.Positive(t, updatedDistribution["svc1"], "Should continue routing to svc1")
	assert.Positive(t, updatedDistribution["svc3"], "Should route to new svc3")
	assert.Positive(t, updatedDistribution["svc4"], "Should route to new svc4")
}

// BenchmarkHybridLoadBalancer_Advanced benchmarks the hybrid load balancer.
func BenchmarkHybridLoadBalancer_Advanced(b *testing.B) {
	endpoints := []*discovery.Endpoint{
		{Service: "svc1", Address: "192.168.1.1", Port: 8080, Healthy: true, Weight: testIterations},
		{Service: "svc2", Address: "192.168.1.2", Port: 8080, Healthy: true, Weight: 150},
		{Service: "svc3", Address: "192.168.1.3", Port: 8080, Healthy: true, Weight: httpStatusOK},
		{Service: "svc4", Address: "192.168.1.4", Port: 8080, Healthy: true, Weight: testTimeout},
		{Service: "svc5", Address: "192.168.1.5", Port: 8080, Healthy: true, Weight: 75},
	}

	algorithms := map[string]HybridConfig{
		"RoundRobin":       {Strategy: "round_robin", HealthAware: true},
		"LeastConnections": {Strategy: "least_connections", HealthAware: true},
		"Weighted":         {Strategy: "weighted", HealthAware: true},
	}

	for name, config := range algorithms {
		b.Run(name, func(b *testing.B) {
			lb := NewHybridLoadBalancer(endpoints, config)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				endpoint := lb.Next()
				if endpoint == nil {
					b.Error("Got nil endpoint")
				}
			}
		})
	}
}
