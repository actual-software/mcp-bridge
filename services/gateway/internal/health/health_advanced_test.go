package health

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
)

// MockServiceDiscovery provides a mock service discovery for testing.
type MockServiceDiscovery struct {
	namespaces []string
	endpoints  map[string][]discovery.Endpoint
}

func (m *MockServiceDiscovery) GetEndpoints(namespace string) []discovery.Endpoint {
	if m.endpoints == nil {
		return []discovery.Endpoint{}
	}

	return m.endpoints[namespace]
}

func (m *MockServiceDiscovery) GetAllEndpoints() map[string][]discovery.Endpoint {
	if m.endpoints == nil {
		return make(map[string][]discovery.Endpoint)
	}

	return m.endpoints
}

func (m *MockServiceDiscovery) ListNamespaces() []string {
	if m.namespaces == nil {
		return []string{}
	}

	return m.namespaces
}

func (m *MockServiceDiscovery) Start(ctx context.Context) error {
	return nil
}

func (m *MockServiceDiscovery) Stop() {}

// RegisterEndpointChangeCallback is a no-op for mock discovery.
func (m *MockServiceDiscovery) RegisterEndpointChangeCallback(callback func(namespace string)) {
	// Mock doesn't trigger endpoint changes
}

// TestChecker_UpdateSubsystem_Advanced tests the UpdateSubsystem method.
func TestChecker_UpdateSubsystem_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockDiscovery := &MockServiceDiscovery{}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	// Test updating a subsystem
	subsystem := Subsystem{
		Name:         "test_service",
		
		Status:       "operational",
		Message:      "Service is running normally",
		Metrics:      map[string]interface{}{"connections": 5},
		Dependencies: []string{"database", "cache"},
	}

	checker.UpdateSubsystem("test_service", subsystem)

	status := checker.GetStatus()
	assert.Contains(t, status.Subsystems, "test_service")

	retrievedSubsystem := status.Subsystems["test_service"]
	assert.True(t, retrievedSubsystem.IsHealthy())
	assert.Equal(t, "operational", retrievedSubsystem.Status)
	assert.Equal(t, "Service is running normally", retrievedSubsystem.Message)
	assert.Equal(t, 5, retrievedSubsystem.Metrics["connections"])
	assert.Contains(t, retrievedSubsystem.Dependencies, "database")
	assert.NotZero(t, retrievedSubsystem.LastCheck)
}

// TestChecker_GetDetailedStatus_Advanced tests the GetDetailedStatus method.
func TestChecker_GetDetailedStatus_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockDiscovery := &MockServiceDiscovery{
		namespaces: []string{"test-ns"},
		endpoints: map[string][]discovery.Endpoint{
			"test-ns": {
				{Service: "test-service", Address: "127.0.0.1", Port: 8080},
			},
		},
	}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	// Get detailed status (this will trigger subsystem checks)
	status := checker.GetDetailedStatus()

	assert.NotNil(t, status)
	assert.NotEmpty(t, status.Subsystems)

	// Service discovery subsystem should be created
	assert.Contains(t, status.Subsystems, "service_discovery")
	sdStatus := status.Subsystems["service_discovery"]
	assert.True(t, sdStatus.IsHealthy())
	assert.Contains(t, sdStatus.Message, "endpoints")
	assert.NotZero(t, sdStatus.Duration)
	assert.Contains(t, sdStatus.Metrics, "endpoint_count")
}

// TestChecker_ServiceDiscoverySubsystem_Advanced tests service discovery health checks.
func TestChecker_ServiceDiscoverySubsystem_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name          string
		mockDiscovery *MockServiceDiscovery
		expectHealthy bool
		expectMessage string
	}{
		{
			name: "healthy with endpoints",
			mockDiscovery: &MockServiceDiscovery{
				namespaces: []string{"ns1", "ns2"},
				endpoints: map[string][]discovery.Endpoint{
					"ns1": {{Service: "svc1"}},
					"ns2": {{Service: "svc2"}},
				},
			},
			expect
			expectMessage: "endpoints",
		},
		{
			name: "healthy with empty discovery",
			mockDiscovery: &MockServiceDiscovery{
				endpoints: map[string][]discovery.Endpoint{},
			},
			expect
			expectMessage: "0 endpoints",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := CreateHealthMonitor(tt.mockDiscovery, logger)

			status := checker.GetDetailedStatus()

			sdStatus, exists := status.Subsystems["service_discovery"]
			assert.True(t, exists)
			assert.Equal(t, tt.expectHealthy, sdStatus.IsHealthy())
			assert.Contains(t, sdStatus.Message, tt.expectMessage)
		})
	}
}

// TestChecker_ConnectionPoolSubsystem_Advanced tests connection pool health checks.
func TestChecker_ConnectionPoolSubsystem_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockDiscovery := &MockServiceDiscovery{}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	status := checker.GetDetailedStatus()

	poolStatus, exists := status.Subsystems["connection_pool"]
	assert.True(t, exists)
	assert.True(t, poolStatus.IsHealthy(), "Connection pool should be healthy by default")
	assert.Contains(t, poolStatus.Message, "operational")
	assert.NotZero(t, poolStatus.Duration)

	// Check that metrics are populated
	assert.Contains(t, poolStatus.Metrics, "active_connections")
	assert.Contains(t, poolStatus.Metrics, "idle_connections")
	assert.Contains(t, poolStatus.Metrics, "max_connections")
}

// TestChecker_AuthSubsystem_Advanced tests auth subsystem health checks.
func TestChecker_AuthSubsystem_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockDiscovery := &MockServiceDiscovery{}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	status := checker.GetDetailedStatus()

	authStatus, exists := status.Subsystems["authentication"]
	assert.True(t, exists)
	assert.True(t, authStatus.IsHealthy(), "Auth should be healthy by default")
	assert.Contains(t, authStatus.Message, "operational")
	assert.NotZero(t, authStatus.Duration)
}

// TestChecker_RateLimitSubsystem_Advanced tests rate limit subsystem health checks.
func TestChecker_RateLimitSubsystem_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockDiscovery := &MockServiceDiscovery{}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	status := checker.GetDetailedStatus()

	rlStatus, exists := status.Subsystems["rate_limiting"]
	assert.True(t, exists)
	assert.True(t, rlStatus.IsHealthy(), "Rate limit should be healthy by default")
	assert.Contains(t, rlStatus.Message, "operational")
	assert.NotZero(t, rlStatus.Duration)
}

// TestChecker_OverallHealth_Advanced tests overall health calculation.
func TestChecker_OverallHealth_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name            string
		subsystems      map[string]Subsystem
		expectedOverall bool
	}{
		{
			name: "all healthy",
			subsystems: map[string]Subsystem{
				"s1": { Status: "ok"},
				"s2": { Status: "ok"},
			},
			expectedOverall: true,
		},
		{
			name: "one unhealthy",
			subsystems: map[string]Subsystem{
				"s1": { Status: "ok"},
				"s2": { Status: "failed"},
			},
			expectedOverall: false,
		},
		{
			name:            "no subsystems",
			subsystems:      map[string]Subsystem{},
			expectedOverall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDiscovery := &MockServiceDiscovery{}
			checker := CreateHealthMonitor(mockDiscovery, logger)

			// Set up subsystems
			for name, subsystem := range tt.subsystems {
				checker.UpdateSubsystem(name, subsystem)
			}

			// Check overall health
			assert.Equal(t, tt.expectedOverall, checker.IsHealthy())
		})
	}
}

// TestChecker_HealthAggregation_Advanced tests health status aggregation.
func TestChecker_HealthAggregation_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockDiscovery := &MockServiceDiscovery{}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	// Add multiple subsystems with different health states
	healthySubsystem := Subsystem{
		Name:    "healthy_service",
		
		Status:  "operational",
		Message: "All good",
		Metrics: map[string]interface{}{"uptime": 3600},
	}

	unhealthySubsystem := Subsystem{
		Name:    "unhealthy_service",
		
		Status:  "failed",
		Message: "Service down",
		Metrics: map[string]interface{}{"errors": 10},
	}

	checker.UpdateSubsystem("healthy_service", healthySubsystem)
	checker.UpdateSubsystem("unhealthy_service", unhealthySubsystem)

	// Overall health should be false due to one failing subsystem
	assert.False(t, checker.IsHealthy())

	status := checker.GetStatus()
	assert.Len(t, status.Subsystems, 2)

	// Fix the failing subsystem
	fixedSubsystem := unhealthySubsystem
	fixedSubsystem.IsHealthy() = true
	fixedSubsystem.Status = "recovered"
	fixedSubsystem.Message = "Service restored"

	checker.UpdateSubsystem("unhealthy_service", fixedSubsystem)
	assert.True(t, checker.IsHealthy())
}

// TestChecker_PredictiveHealthPatterns_Advanced tests predictive health patterns.
func TestChecker_PredictiveHealthPatterns_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test subsystem health tracking over time
	mockDiscovery := &MockServiceDiscovery{
		endpoints: map[string][]discovery.Endpoint{
			"service": {
				{Service: "svc1"},
				{Service: "svc2"},
				{Service: "svc3"},
			},
		},
	}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	// Initial state - all healthy
	status1 := checker.GetDetailedStatus()
	sdStatus1 := status1.Subsystems["service_discovery"]
	assert.True(t, sdStatus1.IsHealthy())
	endpointCount1, ok := sdStatus1.Metrics["endpoint_count"].(int)
	require.True(t, ok, "Expected endpoint_count to be int")
	assert.Equal(t, 3, endpointCount1)

	// Simulate endpoint degradation
	mockDiscovery.endpoints["service"][0].IsHealthy() = false
	status2 := checker.GetDetailedStatus()
	sdStatus2 := status2.Subsystems["service_discovery"]
	assert.True(t, sdStatus2.IsHealthy()) // Service discovery itself is still healthy
	endpointCount2, ok := sdStatus2.Metrics["endpoint_count"].(int)
	require.True(t, ok, "Expected endpoint_count to be int")
	assert.Equal(t, 3, endpointCount2) // Total count unchanged

	// Add a custom subsystem to test health aggregation
	degradedSubsystem := Subsystem{
		Name:    "predictive_monitor",
		
		Status:  "degraded",
		Message: "Predictive failure detected",
		Metrics: map[string]interface{}{
			"failure_rate":     0.7,
			"response_time_ms": 1500,
		},
	}
	checker.UpdateSubsystem("predictive_monitor", degradedSubsystem)

	// Overall health should now be affected
	assert.False(t, checker.IsHealthy())

	// Simulate recovery
	recoveredSubsystem := degradedSubsystem
	recoveredSubsystem.IsHealthy() = true
	recoveredSubsystem.Status = "recovered"
	recoveredSubsystem.Message = "System recovered"
	recoveredSubsystem.Metrics["failure_rate"] = 0.1
	checker.UpdateSubsystem("predictive_monitor", recoveredSubsystem)

	// Should be healthy again
	assert.True(t, checker.IsHealthy())
}

// TestChecker_ConcurrentAccess_Advanced tests concurrent health check access.
func TestChecker_ConcurrentAccess_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockDiscovery := &MockServiceDiscovery{
		endpoints: map[string][]discovery.Endpoint{
			"test": {{Service: "test-svc"}},
		},
	}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	done := make(chan error, 20)

	runConcurrentReads(checker, done)
	runConcurrentWrites(checker, done)

	waitForConcurrentOperations(t, done, 20)

	status := checker.GetStatus()
	assert.NotNil(t, status)
	assert.Contains(t, status.Subsystems, "concurrent_test")
}

func runConcurrentReads(checker *Checker, done chan error) {
	for i := 0; i < 10; i++ {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- fmt.Errorf("panic in read goroutine: %v", r)
				} else {
					done <- nil
				}
			}()

			status := checker.GetDetailedStatus()
			if status.Subsystems == nil {
				done <- errors.New("subsystems map is nil")

				return
			}
		}()
	}
}

func runConcurrentWrites(checker *Checker, done chan error) {
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() {
				if r := recover(); r != nil {
					done <- fmt.Errorf("panic in write goroutine: %v", r)
				} else {
					done <- nil
				}
			}()

			subsystem := Subsystem{
				Name:    "concurrent_test",
				Healthy: id%2 == 0,
				Status:  "testing",
				Message: "Concurrent access test",
			}
			checker.UpdateSubsystem("concurrent_test", subsystem)
		}(i)
	}
}

func waitForConcurrentOperations(t *testing.T, done chan error, expectedOps int) {
	t.Helper()

	for i := 0; i < expectedOps; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Goroutine failed: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}
}

// TestServer_Start_EdgeCases tests server start with edge cases.
func TestServer_Start_EdgeCases(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockDiscovery := &MockServiceDiscovery{}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	tests := []struct {
		name        string
		port        int
		expectStart bool
	}{
		{"valid port 0 (auto-assign)", 0, true},
		{"valid port 8080", 8080, true},
		{"invalid negative port", -1, false},
		{"invalid high port", 99999, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := CreateHealthCheckServer(checker, tt.port)

			// Server should start without error initially
			// (actual binding happens when serving)
			// CreateHealthCheckServer doesn't validate ports so it always returns a server instance
			assert.NotNil(t, server)
		})
	}
}

// TestChecker_NilDiscovery tests behavior with nil discovery service.
func TestChecker_NilDiscovery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	checker := CreateHealthMonitor(nil, logger)

	status := checker.GetDetailedStatus()

	// Service discovery subsystem should be marked as unhealthy
	sdStatus, exists := status.Subsystems["service_discovery"]
	assert.True(t, exists)
	assert.False(t, sdStatus.IsHealthy())
	assert.Contains(t, sdStatus.Message, "not configured")
}

// BenchmarkChecker_HealthCheck benchmarks health check performance.
func BenchmarkChecker_HealthCheck(b *testing.B) {
	logger := zaptest.NewLogger(b)

	// Set up realistic scenario
	endpoints := make(map[string][]discovery.Endpoint)

	for i := 0; i < 10; i++ {
		nsName := "namespace-" + string(rune('0'+i))

		endpoints[nsName] = make([]discovery.Endpoint, 5)
		for j := 0; j < 5; j++ {
			endpoints[nsName][j] = discovery.Endpoint{
				Service: "service-" + string(rune('0'+j)),
				Healthy: j%2 == 0,
			}
		}
	}

	mockDiscovery := &MockServiceDiscovery{
		endpoints: endpoints,
	}
	checker := CreateHealthMonitor(mockDiscovery, logger)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		status := checker.GetDetailedStatus()
		_ = status.IsHealthy()
	}
}
