
package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
	"github.com/poiley/mcp-bridge/services/gateway/test/testutil"
)

const testIterations = 100

// MockDiscovery implements discovery.ServiceDiscovery for testing.
type MockDiscovery struct {
	namespaces []string
	endpoints  map[string][]discovery.Endpoint
	mu         sync.RWMutex
}

func NewMockDiscovery() *MockDiscovery {
	return &MockDiscovery{
		endpoints: make(map[string][]discovery.Endpoint),
	}
}

func (m *MockDiscovery) Start(_ context.Context) error {
	return nil
}

func (m *MockDiscovery) Stop() {}

func (m *MockDiscovery) GetEndpoints(namespace string) []discovery.Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.endpoints[namespace]
}

func (m *MockDiscovery) GetAllEndpoints() map[string][]discovery.Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]discovery.Endpoint)
	for k, v := range m.endpoints {
		result[k] = v
	}

	return result
}

func (m *MockDiscovery) ListNamespaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.namespaces
}

func (m *MockDiscovery) SetNamespaces(namespaces []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.namespaces = namespaces
}

func (m *MockDiscovery) SetEndpoints(namespace string, endpoints []discovery.Endpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.endpoints[namespace] = endpoints
}

func TestCreateHealthMonitor(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	mockDiscovery := NewMockDiscovery()

	checker := CreateHealthMonitor(mockDiscovery, logger)

	if checker == nil {
		t.Fatal("Expected checker to be created")
	}

	// Verify initial state
	status := checker.GetStatus()
	if !status.Healthy {
		t.Error("Expected initial status to be healthy")
	}

	if status.Message != "Starting up" {
		t.Errorf("Expected initial message 'Starting up', got '%s'", status.Message)
	}

	if status.Checks == nil {
		t.Error("Expected checks map to be initialized")
	}
}

func TestChecker_GetStatus(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	mockDiscovery := NewMockDiscovery()
	checker := CreateHealthMonitor(mockDiscovery, logger)

	// Set some internal state
	checker.status.Healthy = false
	checker.status.Message = "Test message"
	checker.status.Endpoints = 5
	checker.status.HealthyEndpoints = 3
	checker.status.Namespaces = []string{"ns1", "ns2"}
	checker.status.Checks["test"] = CheckResult{
		Healthy:   true,
		Message:   "Test check",
		LastCheck: time.Now(),
	}

	// Get status
	status := checker.GetStatus()

	// Verify it returns a copy
	if status.Healthy != false {
		t.Error("Expected status to be unhealthy")
	}

	if status.Message != "Test message" {
		t.Errorf("Expected message 'Test message', got '%s'", status.Message)
	}

	if status.Endpoints != 5 {
		t.Errorf("Expected 5 endpoints, got %d", status.Endpoints)
	}

	if status.HealthyEndpoints != 3 {
		t.Errorf("Expected 3 healthy endpoints, got %d", status.HealthyEndpoints)
	}

	if len(status.Namespaces) != 2 {
		t.Errorf("Expected 2 namespaces, got %d", len(status.Namespaces))
	}

	// Modify returned status and verify original is unchanged
	status.Healthy = true
	if checker.status.Healthy != false {
		t.Error("Original status should not be modified")
	}
}

func TestChecker_IsHealthy(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	mockDiscovery := NewMockDiscovery()
	checker := CreateHealthMonitor(mockDiscovery, logger)

	tests := []struct {
		name     string
		healthy  bool
		expected bool
	}{
		{
			name:     "Healthy",
			healthy:  true,
			expected: true,
		},
		{
			name:     "Unhealthy",
			healthy:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker.status.Healthy = tt.healthy
			if result := checker.IsHealthy(); result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestChecker_IsReady(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	mockDiscovery := NewMockDiscovery()
	checker := CreateHealthMonitor(mockDiscovery, logger)

	tests := []struct {
		name             string
		healthyEndpoints int
		expected         bool
	}{
		{
			name:             "Ready with healthy endpoints",
			healthyEndpoints: 5,
			expected:         true,
		},
		{
			name:             "Not ready with no healthy endpoints",
			healthyEndpoints: 0,
			expected:         false,
		},
		{
			name:             "Ready with one healthy endpoint",
			healthyEndpoints: 1,
			expected:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker.status.HealthyEndpoints = tt.healthyEndpoints
			if result := checker.IsReady(); result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestChecker_checkServiceDiscovery(t *testing.T) {
	logger := testutil.NewTestLogger(t)

	tests := []struct {
		name       string
		namespaces []string
		expected   CheckResult
	}{
		{
			name:       "Healthy with namespaces",
			namespaces: []string{"ns1", "ns2", "ns3"},
			expected: CheckResult{
				Healthy: true,
				Message: "Discovered 3 namespaces",
			},
		},
		{
			name:       "Unhealthy with no namespaces",
			namespaces: []string{},
			expected: CheckResult{
				Healthy: false,
				Message: "No namespaces discovered",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDiscovery := NewMockDiscovery()
			mockDiscovery.SetNamespaces(tt.namespaces)

			checker := CreateHealthMonitor(mockDiscovery, logger)
			result := checker.checkServiceDiscovery()

			if result.Healthy != tt.expected.Healthy {
				t.Errorf("Expected healthy=%v, got %v", tt.expected.Healthy, result.Healthy)
			}

			if result.Message != tt.expected.Message {
				t.Errorf("Expected message '%s', got '%s'", tt.expected.Message, result.Message)
			}

			if result.LastCheck.IsZero() {
				t.Error("Expected LastCheck to be set")
			}

			// Verify namespaces are updated in status
			if tt.expected.Healthy {
				status := checker.GetStatus()
				if len(status.Namespaces) != len(tt.namespaces) {
					t.Errorf("Expected %d namespaces in status, got %d",
						len(tt.namespaces), len(status.Namespaces))
				}
			}
		})
	}
}

func TestChecker_checkEndpoints(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	tests := createEndpointCheckTests()
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runEndpointCheckTest(t, tt, logger)
		})
	}
}

func createEndpointCheckTests() []struct {
	name                  string
	endpoints             map[string][]discovery.Endpoint
	expectedTotal         int
	expectedHealthy       int
	expectedHealthyStatus bool
} {
	var tests []struct {
		name                  string
		endpoints             map[string][]discovery.Endpoint
		expectedTotal         int
		expectedHealthy       int
		expectedHealthyStatus bool
	}

	tests = append(tests, createHealthyEndpointTests()...)
	tests = append(tests, createUnhealthyEndpointTests()...)
	tests = append(tests, createEmptyEndpointTests()...)

	return tests
}

func createHealthyEndpointTests() []struct {
	name                  string
	endpoints             map[string][]discovery.Endpoint
	expectedTotal         int
	expectedHealthy       int
	expectedHealthyStatus bool
} {
	return []struct {
		name                  string
		endpoints             map[string][]discovery.Endpoint
		expectedTotal         int
		expectedHealthy       int
		expectedHealthyStatus bool
	}{
		{
			name: "All endpoints healthy",
			endpoints: map[string][]discovery.Endpoint{
				"ns1": {
					{Service: "svc1", Healthy: true},
					{Service: "svc2", Healthy: true},
				},
				"ns2": {
					{Service: "svc3", Healthy: true},
				},
			},
			expectedTotal:         3,
			expectedHealthy:       3,
			expectedHealthyStatus: true,
		},
	}
}

func createUnhealthyEndpointTests() []struct {
	name                  string
	endpoints             map[string][]discovery.Endpoint
	expectedTotal         int
	expectedHealthy       int
	expectedHealthyStatus bool
} {
	return []struct {
		name                  string
		endpoints             map[string][]discovery.Endpoint
		expectedTotal         int
		expectedHealthy       int
		expectedHealthyStatus bool
	}{
		{
			name: "Some endpoints unhealthy",
			endpoints: map[string][]discovery.Endpoint{
				"ns1": {
					{Service: "svc1", Healthy: true},
					{Service: "svc2", Healthy: false},
				},
				"ns2": {
					{Service: "svc3", Healthy: true},
				},
			},
			expectedTotal:         3,
			expectedHealthy:       2,
			expectedHealthyStatus: true,
		},
		{
			name: "All endpoints unhealthy",
			endpoints: map[string][]discovery.Endpoint{
				"ns1": {
					{Service: "svc1", Healthy: false},
					{Service: "svc2", Healthy: false},
				},
			},
			expectedTotal:         2,
			expectedHealthy:       0,
			expectedHealthyStatus: false,
		},
	}
}

func createEmptyEndpointTests() []struct {
	name                  string
	endpoints             map[string][]discovery.Endpoint
	expectedTotal         int
	expectedHealthy       int
	expectedHealthyStatus bool
} {
	return []struct {
		name                  string
		endpoints             map[string][]discovery.Endpoint
		expectedTotal         int
		expectedHealthy       int
		expectedHealthyStatus bool
	}{
		{
			name:                  "No endpoints",
			endpoints:             map[string][]discovery.Endpoint{},
			expectedTotal:         0,
			expectedHealthy:       0,
			expectedHealthyStatus: false,
		},
	}
}

func runEndpointCheckTest(t *testing.T, tt struct {
	name                  string
	endpoints             map[string][]discovery.Endpoint
	expectedTotal         int
	expectedHealthy       int
	expectedHealthyStatus bool
}, logger *zap.Logger) {
	t.Helper()

	mockDiscovery := NewMockDiscovery()
	for ns, eps := range tt.endpoints {
		mockDiscovery.SetEndpoints(ns, eps)
	}

	checker := CreateHealthMonitor(mockDiscovery, logger)
	result := checker.checkEndpoints()

	if result.Healthy != tt.expectedHealthyStatus {
		t.Errorf("Expected healthy=%v, got %v", tt.expectedHealthyStatus, result.Healthy)
	}

	expectedMsg := fmt.Sprintf("%d/%d endpoints healthy", tt.expectedHealthy, tt.expectedTotal)
	if result.Message != expectedMsg {
		t.Errorf("Expected message '%s', got '%s'", expectedMsg, result.Message)
	}

	// Verify status is updated
	status := checker.GetStatus()
	if status.Endpoints != tt.expectedTotal {
		t.Errorf("Expected %d total endpoints, got %d", tt.expectedTotal, status.Endpoints)
	}

	if status.HealthyEndpoints != tt.expectedHealthy {
		t.Errorf("Expected %d healthy endpoints, got %d",
			tt.expectedHealthy, status.HealthyEndpoints)
	}
}

func TestChecker_performChecks(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	mockDiscovery := NewMockDiscovery()

	// Set up healthy state
	mockDiscovery.SetNamespaces([]string{"ns1", "ns2"})
	mockDiscovery.SetEndpoints("ns1", []discovery.Endpoint{
		{Service: "svc1", Healthy: true},
		{Service: "svc2", Healthy: true},
	})

	checker := CreateHealthMonitor(mockDiscovery, logger)
	checker.performChecks()

	// Verify overall health
	status := checker.GetStatus()
	if !status.Healthy {
		t.Error("Expected overall status to be healthy")
	}

	if status.Message != "All systems operational" {
		t.Errorf("Expected message 'All systems operational', got '%s'", status.Message)
	}

	// Verify individual checks
	if len(status.Checks) != 2 {
		t.Errorf("Expected 2 checks, got %d", len(status.Checks))
	}

	sdCheck, ok := status.Checks["service_discovery"]
	if !ok {
		t.Error("Expected service_discovery check")
	} else if !sdCheck.Healthy {
		t.Error("Expected service_discovery to be healthy")
	}

	epCheck, ok := status.Checks["endpoints"]
	if !ok {
		t.Error("Expected endpoints check")
	} else if !epCheck.Healthy {
		t.Error("Expected endpoints to be healthy")
	}

	// Test unhealthy state
	mockDiscovery.SetNamespaces([]string{})
	checker.performChecks()

	status = checker.GetStatus()
	if status.Healthy {
		t.Error("Expected overall status to be unhealthy")
	}

	if status.Message != "service_discovery is unhealthy" {
		t.Errorf("Expected unhealthy message, got '%s'", status.Message)
	}
}

func TestChecker_RunChecks(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	mockDiscovery := NewMockDiscovery()
	mockDiscovery.SetNamespaces([]string{"ns1"})

	checker := CreateHealthMonitor(mockDiscovery, logger)

	ctx, cancel := context.WithCancel(context.Background())

	// Start RunChecks in goroutine
	done := make(chan bool)

	go func() {
		checker.RunChecks(ctx)

		done <- true
	}()

	// Wait a bit to ensure initial check runs
	time.Sleep(testIterations * time.Millisecond)

	// Verify checks were performed
	status := checker.GetStatus()
	if len(status.Checks) == 0 {
		t.Error("Expected checks to be performed")
	}

	// Cancel and wait for goroutine to finish
	cancel()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("RunChecks did not stop within timeout")
	}
}

func TestServer_Handlers(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	mockDiscovery := NewMockDiscovery()
	checker := CreateHealthMonitor(mockDiscovery, logger)
	server := CreateHealthCheckServer(checker, 8080)

	tests := getServerHandlerTests(mockDiscovery, checker)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runServerHandlerTest(t, server, tt)
		})
	}
}

type serverHandlerTest struct {
	name              string
	endpoint          string
	setupChecker      func()
	expectedStatus    int
	expectedBody      string
	checkResponseJSON bool
}

func getServerHandlerTests(mockDiscovery *MockDiscovery, checker *Checker) []serverHandlerTest {
	var tests []serverHandlerTest
	
	tests = append(tests, getHealthEndpointTests(mockDiscovery, checker)...)
	tests = append(tests, getHealthzEndpointTests(checker)...)
	tests = append(tests, getReadyEndpointTests(checker)...)
	
	return tests
}

func getHealthEndpointTests(mockDiscovery *MockDiscovery, checker *Checker) []serverHandlerTest {
	return []serverHandlerTest{
		{
			name:     "Health endpoint - healthy",
			endpoint: "/health",
			setupChecker: func() {
				mockDiscovery.SetNamespaces([]string{"ns1"})
				mockDiscovery.SetEndpoints("ns1", []discovery.Endpoint{
					{Service: "svc1", Healthy: true},
				})
				checker.performChecks()
			},
			expectedStatus:    http.StatusOK,
			checkResponseJSON: true,
		},
		{
			name:     "Health endpoint - unhealthy",
			endpoint: "/health",
			setupChecker: func() {
				mockDiscovery.SetNamespaces([]string{})
				checker.performChecks()
			},
			expectedStatus:    http.StatusServiceUnavailable,
			checkResponseJSON: true,
		},
	}
}

func getHealthzEndpointTests(checker *Checker) []serverHandlerTest {
	return []serverHandlerTest{
		{
			name:     "Healthz endpoint - healthy",
			endpoint: "/healthz",
			setupChecker: func() {
				checker.status.Healthy = true
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		{
			name:     "Healthz endpoint - unhealthy",
			endpoint: "/healthz",
			setupChecker: func() {
				checker.status.Healthy = false
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   "Unhealthy",
		},
	}
}

func getReadyEndpointTests(checker *Checker) []serverHandlerTest {
	return []serverHandlerTest{
		{
			name:     "Ready endpoint - ready",
			endpoint: "/ready",
			setupChecker: func() {
				checker.status.HealthyEndpoints = 1
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "Ready",
		},
		{
			name:     "Ready endpoint - not ready",
			endpoint: "/ready",
			setupChecker: func() {
				checker.status.HealthyEndpoints = 0
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   "Not ready",
		},
	}
}

func runServerHandlerTest(t *testing.T, server *Server, tt serverHandlerTest) {
	t.Helper()

	// Setup checker state
	tt.setupChecker()

	// Create request
	req := httptest.NewRequest(http.MethodGet, tt.endpoint, nil)
	w := httptest.NewRecorder()

	// Call handler
	callServerHandler(server, w, req, tt.endpoint)

	// Validate response
	validateServerResponse(t, w, tt)
}

func callServerHandler(server *Server, w http.ResponseWriter, req *http.Request, endpoint string) {
	switch endpoint {
	case "/health":
		server.handleHealth(w, req)
	case "/healthz":
		server.handleHealthz(w, req)
	case "/ready":
		server.handleReady(w, req)
	}
}

func validateServerResponse(t *testing.T, w *httptest.ResponseRecorder, tt serverHandlerTest) {
	t.Helper()

	// Check status code
	if w.Code != tt.expectedStatus {
		t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
	}

	// Check response body
	if tt.checkResponseJSON {
		validateJSONResponse(t, w)
	} else if tt.expectedBody != "" {
		body := w.Body.String()
		if body != tt.expectedBody {
			t.Errorf("Expected body '%s', got '%s'", tt.expectedBody, body)
		}
	}
}

func validateJSONResponse(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	var status Status

	err := json.NewDecoder(w.Body).Decode(&status)
	if err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	// Verify it's a valid status object
	if status.Message == "" {
		t.Error("Expected message in status response")
	}
}

func TestServer_StartStop(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	mockDiscovery := NewMockDiscovery()
	checker := CreateHealthMonitor(mockDiscovery, logger)

	// Use a random port to avoid conflicts
	server := CreateHealthCheckServer(checker, 0)

	// Start server in goroutine
	errChan := make(chan error)

	go func() {
		err := server.Start()
		if err != nil {
			errChan <- err
		}
	}()

	// Give server time to start
	time.Sleep(testIterations * time.Millisecond)

	// Stop server
	server.Stop()

	// Check for errors
	select {
	case err := <-errChan:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Unexpected error: %v", err)
		}
	case <-time.After(1 * time.Second):
	}
}

func TestStatus_JSON(t *testing.T) {
	status := Status{
		Healthy:          true,
		Message:          "Test status",
		Endpoints:        10,
		HealthyEndpoints: 8,
		Namespaces:       []string{"ns1", "ns2"},
		Checks: map[string]CheckResult{
			"test_check": {
				Healthy:   true,
				Message:   "Check passed",
				LastCheck: time.Now(),
			},
		},
	}

	// Test JSON marshaling
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Failed to marshal status: %v", err)
	}

	// Test JSON unmarshaling
	var decoded Status

	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal status: %v", err)
	}

	// Verify fields
	if decoded.Healthy != status.Healthy {
		t.Error("Healthy field mismatch")
	}

	if decoded.Message != status.Message {
		t.Error("Message field mismatch")
	}

	if decoded.Endpoints != status.Endpoints {
		t.Error("Endpoints field mismatch")
	}

	if decoded.HealthyEndpoints != status.HealthyEndpoints {
		t.Error("HealthyEndpoints field mismatch")
	}

	if len(decoded.Namespaces) != len(status.Namespaces) {
		t.Error("Namespaces length mismatch")
	}

	if len(decoded.Checks) != len(status.Checks) {
		t.Error("Checks length mismatch")
	}
}

func TestChecker_ConcurrentAccess(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	mockDiscovery := NewMockDiscovery()
	checker := CreateHealthMonitor(mockDiscovery, logger)

	// Run concurrent operations
	var wg sync.WaitGroup

	iterations := testIterations

	// Concurrent reads
	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < iterations; i++ {
			_ = checker.GetStatus()
			_ = checker.IsHealthy()
			_ = checker.IsReady()
		}
	}()

	// Concurrent writes via performChecks
	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < iterations; i++ {
			checker.performChecks()
		}
	}()

	// Concurrent modifications to mock discovery
	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < iterations; i++ {
			if i%2 == 0 {
				mockDiscovery.SetNamespaces([]string{"ns1", "ns2"})
				mockDiscovery.SetEndpoints("ns1", []discovery.Endpoint{
					{Service: "svc", Healthy: true},
				})
			} else {
				mockDiscovery.SetNamespaces([]string{})
				mockDiscovery.SetEndpoints("ns1", []discovery.Endpoint{})
			}
		}
	}()

	// Wait for all goroutines
	wg.Wait()
}
