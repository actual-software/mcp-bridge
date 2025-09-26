package health

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

// MockHealthChecker implements HealthChecker for testing.
type MockHealthChecker struct {
	componentName string
	result        HealthCheckResult
	callCount     int
}

func NewMockHealthChecker(name string, status HealthStatus) *MockHealthChecker {
	return &MockHealthChecker{
		componentName: name,
		result: HealthCheckResult{
			ComponentName: name,
			Status:        status,
			Message:       string(status),
			Timestamp:     time.Now(),
			Duration:      10 * time.Millisecond,
		},
	}
}

func (m *MockHealthChecker) CheckHealth(ctx context.Context) HealthCheckResult {
	m.callCount++
	m.result.Timestamp = time.Now()

	return m.result
}

func (m *MockHealthChecker) GetComponentName() string {
	return m.componentName
}

func TestNewCompositeHealthChecker(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()

	checker := NewCompositeHealthChecker(logger)

	if checker == nil {
		t.Fatal("Expected non-nil composite health checker")
	}

	if len(checker.checkers) != 0 {
		t.Errorf("Expected 0 initial checkers, got %d", len(checker.checkers))
	}
}

func TestCompositeHealthChecker_AddChecker(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	checker := NewCompositeHealthChecker(logger)

	mockChecker := NewMockHealthChecker("test-component", HealthStatusHealthy)

	checker.AddChecker(mockChecker)

	if len(checker.checkers) != 1 {
		t.Errorf("Expected 1 checker after adding, got %d", len(checker.checkers))
	}

	// Add another checker.
	mockChecker2 := NewMockHealthChecker("test-component-2", HealthStatusUnhealthy)
	checker.AddChecker(mockChecker2)

	if len(checker.checkers) != 2 {
		t.Errorf("Expected 2 checkers after adding second, got %d", len(checker.checkers))
	}
}

func TestCompositeHealthChecker_RemoveChecker(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	checker := NewCompositeHealthChecker(logger)

	mockChecker := NewMockHealthChecker("test-component", HealthStatusHealthy)
	checker.AddChecker(mockChecker)

	// Remove existing checker.
	removed := checker.RemoveChecker("test-component")
	if !removed {
		t.Error("Expected RemoveChecker to return true for existing component")
	}

	if len(checker.checkers) != 0 {
		t.Errorf("Expected 0 checkers after removal, got %d", len(checker.checkers))
	}

	// Try to remove non-existent checker.
	removed = checker.RemoveChecker("non-existent")
	if removed {
		t.Error("Expected RemoveChecker to return false for non-existent component")
	}
}

func TestCompositeHealthChecker_CheckHealth(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	checker := NewCompositeHealthChecker(logger)

	mockChecker1 := NewMockHealthChecker("component-1", HealthStatusHealthy)
	mockChecker2 := NewMockHealthChecker("component-2", HealthStatusDegraded)

	checker.AddChecker(mockChecker1)
	checker.AddChecker(mockChecker2)

	ctx := context.Background()
	results := checker.CheckHealth(ctx)

	if len(results) != 2 {
		t.Errorf("Expected 2 health check results, got %d", len(results))
	}

	// Verify both checkers were called.
	if mockChecker1.callCount != 1 {
		t.Errorf("Expected checker 1 to be called once, got %d", mockChecker1.callCount)
	}

	if mockChecker2.callCount != 1 {
		t.Errorf("Expected checker 2 to be called once, got %d", mockChecker2.callCount)
	}

	// Verify results contain expected components.
	componentNames := make(map[string]bool)
	for _, result := range results {
		componentNames[result.ComponentName] = true
	}

	if !componentNames["component-1"] {
		t.Error("Expected component-1 in results")
	}

	if !componentNames["component-2"] {
		t.Error("Expected component-2 in results")
	}
}

func TestCompositeHealthChecker_GetOverallHealth(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	tests := createCompositeHealthTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checker := setupCompositeHealthChecker(logger, tt.checkerStates)

			ctx := context.Background()
			result := checker.GetOverallHealth(ctx)

			validateCompositeHealthResult(t, tt, &result)
		})
	}
}

func createCompositeHealthTests() []struct {
	name           string
	checkerStates  []HealthStatus
	expectedStatus HealthStatus
	expectedMsg    string
} {
	return []struct {
		name           string
		checkerStates  []HealthStatus
		expectedStatus HealthStatus
		expectedMsg    string
	}{
		{
			name:           "all healthy",
			checkerStates:  []HealthStatus{HealthStatusHealthy, HealthStatusHealthy},
			expectedStatus: HealthStatusHealthy,
			expectedMsg:    "All components healthy",
		},
		{
			name:           "some degraded",
			checkerStates:  []HealthStatus{HealthStatusHealthy, HealthStatusDegraded},
			expectedStatus: HealthStatusDegraded,
			expectedMsg:    "1 components degraded, 1 healthy",
		},
		{
			name:           "some unhealthy",
			checkerStates:  []HealthStatus{HealthStatusHealthy, HealthStatusUnhealthy},
			expectedStatus: HealthStatusUnhealthy,
			expectedMsg:    "1 components unhealthy, 0 degraded, 1 healthy",
		},
		{
			name:           "mixed states",
			checkerStates:  []HealthStatus{HealthStatusHealthy, HealthStatusDegraded, HealthStatusUnhealthy},
			expectedStatus: HealthStatusUnhealthy,
			expectedMsg:    "1 components unhealthy, 1 degraded, 1 healthy",
		},
		{
			name:           "no components",
			checkerStates:  []HealthStatus{},
			expectedStatus: HealthStatusUnknown,
			expectedMsg:    "No components registered",
		},
	}
}

func setupCompositeHealthChecker(logger *zap.Logger, checkerStates []HealthStatus) *CompositeHealthChecker {
	checker := NewCompositeHealthChecker(logger)

	// Add mock checkers with specified states.
	for i, status := range checkerStates {
		mockChecker := NewMockHealthChecker(
			"component-"+string(rune('1'+i)),
			status,
		)
		checker.AddChecker(mockChecker)
	}

	return checker
}

func validateCompositeHealthResult(t *testing.T, tt struct {
	name           string
	checkerStates  []HealthStatus
	expectedStatus HealthStatus
	expectedMsg    string
}, result *HealthCheckResult) {
	t.Helper()

	if result.Status != tt.expectedStatus {
		t.Errorf("Expected status %v, got %v", tt.expectedStatus, result.Status)
	}

	if result.Message != tt.expectedMsg {
		t.Errorf("Expected message '%s', got '%s'", tt.expectedMsg, result.Message)
	}

	if result.ComponentName != "system" {
		t.Errorf("Expected component name 'system', got '%s'", result.ComponentName)
	}

	// Verify details structure.
	if result.Details == nil {
		t.Error("Expected non-nil details")

		return
	}

	componentCount, ok := result.Details["component_count"].(int)
	if !ok || componentCount != len(tt.checkerStates) {
		t.Errorf("Expected component_count %d, got %v", len(tt.checkerStates), result.Details["component_count"])
	}
}

// MockGatewayPool for testing GatewayPoolHealthChecker.
type MockGatewayPool struct {
	stats map[string]interface{}
}

func (m *MockGatewayPool) GetStats() map[string]interface{} {
	return m.stats
}

func TestGatewayPoolHealthChecker(t *testing.T) {
	t.Parallel()

	tests := createGatewayPoolHealthTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := &MockGatewayPool{stats: tt.stats}
			checker := NewGatewayPoolHealthChecker(pool, "gateway-pool")

			ctx := context.Background()
			result := checker.CheckHealth(ctx)

			validateGatewayPoolHealthResult(t, tt, &result)
		})
	}
}

func createGatewayPoolHealthTests() []struct {
	name           string
	stats          map[string]interface{}
	expectedStatus HealthStatus
	expectedMsg    string
} {
	return []struct {
		name           string
		stats          map[string]interface{}
		expectedStatus HealthStatus
		expectedMsg    string
	}{
		{
			name: "all endpoints healthy",
			stats: map[string]interface{}{
				"total_endpoints":   2,
				"healthy_endpoints": 2,
				"strategy":          "round_robin",
			},
			expectedStatus: HealthStatusHealthy,
			expectedMsg:    "All 2 endpoints healthy",
		},
		{
			name: "some endpoints unhealthy",
			stats: map[string]interface{}{
				"total_endpoints":   3,
				"healthy_endpoints": 2,
				"strategy":          "weighted",
			},
			expectedStatus: HealthStatusDegraded,
			expectedMsg:    "2 of 3 endpoints healthy",
		},
		{
			name: "no healthy endpoints",
			stats: map[string]interface{}{
				"total_endpoints":   2,
				"healthy_endpoints": 0,
				"strategy":          "round_robin",
			},
			expectedStatus: HealthStatusUnhealthy,
			expectedMsg:    "No healthy endpoints available",
		},
		{
			name: "no endpoints configured",
			stats: map[string]interface{}{
				"total_endpoints":   0,
				"healthy_endpoints": 0,
				"strategy":          "round_robin",
			},
			expectedStatus: HealthStatusUnhealthy,
			expectedMsg:    "No endpoints configured",
		},
	}
}

func validateGatewayPoolHealthResult(t *testing.T, tt struct {
	name           string
	stats          map[string]interface{}
	expectedStatus HealthStatus
	expectedMsg    string
}, result *HealthCheckResult) {
	t.Helper()

	if result.Status != tt.expectedStatus {
		t.Errorf("Expected status %v, got %v", tt.expectedStatus, result.Status)
	}

	if result.Message != tt.expectedMsg {
		t.Errorf("Expected message '%s', got '%s'", tt.expectedMsg, result.Message)
	}

	if result.ComponentName != "gateway-pool" {
		t.Errorf("Expected component name 'gateway-pool', got '%s'", result.ComponentName)
	}

	// Verify details.
	if result.Details == nil {
		t.Error("Expected non-nil details")

		return
	}

	if result.Details["total_endpoints"] != tt.stats["total_endpoints"] {
		t.Errorf("Expected total_endpoints %v, got %v", tt.stats["total_endpoints"], result.Details["total_endpoints"])
	}
}

// MockConnectionManager for testing ConnectionManagerHealthChecker.
type MockConnectionManager struct {
	state      interface{}
	retryCount uint64
}

func (m *MockConnectionManager) GetState() interface{} {
	return m.state
}

func (m *MockConnectionManager) GetRetryCount() uint64 {
	return m.retryCount
}

func TestConnectionManagerHealthChecker(t *testing.T) {
	t.Parallel()

	tests := createConnectionManagerHealthTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			connMgr := &MockConnectionManager{
				state:      tt.state,
				retryCount: tt.retryCount,
			}
			checker := NewConnectionManagerHealthChecker(connMgr, "connection-manager")

			ctx := context.Background()
			result := checker.CheckHealth(ctx)

			validateConnectionManagerHealthResult(t, tt, &result)
		})
	}
}

func createConnectionManagerHealthTests() []struct {
	name           string
	state          string
	retryCount     uint64
	expectedStatus HealthStatus
	expectedMsg    string
} {
	return []struct {
		name           string
		state          string
		retryCount     uint64
		expectedStatus HealthStatus
		expectedMsg    string
	}{
		{
			name:           "connected",
			state:          "CONNECTED",
			retryCount:     0,
			expectedStatus: HealthStatusHealthy,
			expectedMsg:    "Connection established",
		},
		{
			name:           "connecting",
			state:          "CONNECTING",
			retryCount:     2,
			expectedStatus: HealthStatusDegraded,
			expectedMsg:    "Connection in CONNECTING state",
		},
		{
			name:           "error state",
			state:          "ERROR",
			retryCount:     5,
			expectedStatus: HealthStatusUnhealthy,
			expectedMsg:    "Connection in error state",
		},
		{
			name:           "shutdown",
			state:          "SHUTDOWN",
			retryCount:     0,
			expectedStatus: HealthStatusUnhealthy,
			expectedMsg:    "Connection manager shutdown",
		},
		{
			name:           "high retry count",
			state:          "CONNECTED",
			retryCount:     15,
			expectedStatus: HealthStatusDegraded,
			expectedMsg:    "Connection established (high retry count: 15)",
		},
	}
}

func validateConnectionManagerHealthResult(t *testing.T, tt struct {
	name           string
	state          string
	retryCount     uint64
	expectedStatus HealthStatus
	expectedMsg    string
}, result *HealthCheckResult) {
	t.Helper()

	if result.Status != tt.expectedStatus {
		t.Errorf("Expected status %v, got %v", tt.expectedStatus, result.Status)
	}

	if result.Message != tt.expectedMsg {
		t.Errorf("Expected message '%s', got '%s'", tt.expectedMsg, result.Message)
	}

	if result.ComponentName != "connection-manager" {
		t.Errorf("Expected component name 'connection-manager', got '%s'", result.ComponentName)
	}
}

func TestCustomHealthChecker(t *testing.T) {
	t.Parallel()

	customCheck := func(ctx context.Context) HealthCheckResult {
		return HealthCheckResult{
			ComponentName: "custom",
			Status:        HealthStatusHealthy,
			Message:       "Custom check passed",
			Timestamp:     time.Now(),
			Duration:      5 * time.Millisecond,
		}
	}

	checker := NewCustomHealthChecker("custom-component", customCheck)

	if checker.GetComponentName() != "custom-component" {
		t.Errorf("Expected component name 'custom-component', got '%s'", checker.GetComponentName())
	}

	ctx := context.Background()
	result := checker.CheckHealth(ctx)

	if result.ComponentName != "custom" {
		t.Errorf("Expected result component name 'custom', got '%s'", result.ComponentName)
	}

	if result.Status != HealthStatusHealthy {
		t.Errorf("Expected status healthy, got %v", result.Status)
	}

	if result.Message != "Custom check passed" {
		t.Errorf("Expected message 'Custom check passed', got '%s'", result.Message)
	}
}
