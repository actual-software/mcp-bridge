package router

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/circuit"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/loadbalancer"
	"github.com/actual-software/mcp-bridge/services/gateway/test/testutil"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

const (
	// Test constants.
	testIterations    = 100
	testMaxIterations = 1000
	testTimeout       = 50
)

func TestInitializeRequestRouter(t *testing.T) {
	config := config.RoutingConfig{
		Strategy:            "round_robin",
		HealthCheckInterval: "30s",
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			TimeoutSeconds:   30,
		},
	}

	testEndpoint := discovery.Endpoint{Address: "localhost", Port: 8080, Scheme: "http"}
	testEndpoint.SetHealthy(true)
	mockDiscovery := &mockServiceDiscovery{
		endpoints: map[string][]discovery.Endpoint{
			"test": {testEndpoint},
		},
	}

	registry := testutil.CreateTestMetricsRegistry()
	logger := testutil.NewTestLogger(t)

	router := InitializeRequestRouter(context.Background(), config, mockDiscovery, registry, logger)

	if router == nil {
		t.Fatal("Expected router to be created")

		return
	}

	if router.config.Strategy != "round_robin" {
		t.Errorf("Expected strategy 'round_robin', got '%s'", router.config.Strategy)
	}

	if router.discovery == nil {
		t.Error("Discovery not set")
	}

	if router.metrics != registry {
		t.Error("Metrics not set correctly")
	}

	if router.logger != logger {
		t.Error("Logger not set correctly")
	}

	if router.endpointClients == nil {
		t.Error("Endpoint clients map not initialized")
	}

	if router.balancers == nil {
		t.Error("Balancers map not initialized")
	}

	if router.breakers == nil {
		t.Error("Breakers map not initialized")
	}
}

func TestRouter_extractNamespace(t *testing.T) {
	router := &Router{}

	tests := createExtractNamespaceTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := router.extractNamespace(tt.request)
			if result != tt.expected {
				t.Errorf("Expected namespace '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func createExtractNamespaceTests() []struct {
	name     string
	request  *mcp.Request
	expected string
} {
	var tests []struct {
		name     string
		request  *mcp.Request
		expected string
	}

	tests = append(tests, createSystemMethodTests()...)
	tests = append(tests, createToolsMethodTests()...)
	tests = append(tests, createEdgeCaseMethodTests()...)

	return tests
}

func createSystemMethodTests() []struct {
	name     string
	request  *mcp.Request
	expected string
} {
	return []struct {
		name     string
		request  *mcp.Request
		expected string
	}{
		{
			name: "Initialize method",
			request: &mcp.Request{
				Method: "initialize",
			},
			expected: "system",
		},
		{
			name: "Shutdown method",
			request: &mcp.Request{
				Method: "shutdown",
			},
			expected: "system",
		},
		{
			name: "Tools list without namespace",
			request: &mcp.Request{
				Method: "tools/list",
			},
			expected: "system",
		},
	}
}

func createToolsMethodTests() []struct {
	name     string
	request  *mcp.Request
	expected string
} {
	return []struct {
		name     string
		request  *mcp.Request
		expected string
	}{
		{
			name: "Tools call with namespace",
			request: &mcp.Request{
				Method: "tools/call",
				Params: map[string]interface{}{
					"name": "k8s.getPods",
				},
			},
			expected: "k8s",
		},
		{
			name: "Tools call without dot",
			request: &mcp.Request{
				Method: "tools/call",
				Params: map[string]interface{}{
					"name": "simpleToolName",
				},
			},
			expected: "simpleToolName",
		},
		{
			name: "Tools list with namespace",
			request: &mcp.Request{
				Method: "tools/list",
				Params: map[string]interface{}{
					"namespace": "docker",
				},
			},
			expected: "docker",
		},
	}
}

func createEdgeCaseMethodTests() []struct {
	name     string
	request  *mcp.Request
	expected string
} {
	return []struct {
		name     string
		request  *mcp.Request
		expected string
	}{
		{
			name: "Unknown method",
			request: &mcp.Request{
				Method: "custom/method",
			},
			expected: "default",
		},
		{
			name: "Tools call with invalid params",
			request: &mcp.Request{
				Method: "tools/call",
				Params: "invalid",
			},
			expected: "default",
		},
	}
}

func TestRouter_getLoadBalancer(t *testing.T) {
	mockDiscovery := createMockDiscoveryForLoadBalancer()
	tests := createLoadBalancerTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := createTestRouter(tt.strategy, mockDiscovery)
			lb := router.getLoadBalancer(tt.namespace)

			validateLoadBalancerResult(t, lb, tt.expectNil, router, tt.namespace)
		})
	}
}

func createMockDiscoveryForLoadBalancer() *mockServiceDiscovery {
	ep1 := discovery.Endpoint{Address: "host1", Port: 8080, Scheme: "http", Weight: 1}
	ep1.SetHealthy(true)
	ep2 := discovery.Endpoint{Address: "host2", Port: 8080, Scheme: "http", Weight: 2}
	ep2.SetHealthy(true)

	return &mockServiceDiscovery{
		endpoints: map[string][]discovery.Endpoint{
			"test":  {ep1, ep2},
			"empty": {},
		},
	}
}

func createLoadBalancerTests() []struct {
	name      string
	namespace string
	strategy  string
	expectNil bool
} {
	return []struct {
		name      string
		namespace string
		strategy  string
		expectNil bool
	}{
		{
			name:      "Round robin strategy",
			namespace: "test",
			strategy:  "round_robin",
			expectNil: false,
		},
		{
			name:      "Least connections strategy",
			namespace: "test",
			strategy:  "least_connections",
			expectNil: false,
		},
		{
			name:      "Weighted strategy",
			namespace: "test",
			strategy:  "weighted",
			expectNil: false,
		},
		{
			name:      "Default strategy",
			namespace: "test",
			strategy:  "unknown",
			expectNil: false,
		},
		{
			name:      "No endpoints",
			namespace: "empty",
			strategy:  "round_robin",
			expectNil: true,
		},
		{
			name:      "Unknown namespace",
			namespace: "unknown",
			strategy:  "round_robin",
			expectNil: true,
		},
	}
}

func createTestRouter(strategy string, mockDiscovery *mockServiceDiscovery) *Router {
	return &Router{
		config: config.RoutingConfig{
			Strategy: strategy,
		},
		discovery: mockDiscovery,
		balancers: make(map[string]loadbalancer.LoadBalancer),
	}
}

func validateLoadBalancerResult(
	t *testing.T, lb loadbalancer.LoadBalancer, expectNil bool,
	router *Router, namespace string,
) {
	t.Helper()

	if expectNil {
		if lb != nil {
			t.Error("Expected nil load balancer")
		}
	} else {
		if lb == nil {
			t.Error("Expected load balancer to be created")
		}

		lb2 := router.getLoadBalancer(namespace)
		if lb != lb2 {
			t.Error("Expected cached load balancer")
		}
	}
}

func TestRouter_getCircuitBreaker(t *testing.T) {
	router := &Router{
		config: config.RoutingConfig{
			CircuitBreaker: config.CircuitBreakerConfig{
				FailureThreshold: 3,
				SuccessThreshold: 1,
				TimeoutSeconds:   10,
			},
		},
		breakers: make(map[string]*circuit.CircuitBreaker),
	}

	// Get new breaker
	breaker1 := router.getCircuitBreaker("host1:8080")
	if breaker1 == nil {
		t.Fatal("Expected circuit breaker to be created")
	}

	// Get cached breaker
	breaker2 := router.getCircuitBreaker("host1:8080")
	if breaker1 != breaker2 {
		t.Error("Expected cached circuit breaker")
	}

	// Get different breaker
	breaker3 := router.getCircuitBreaker("host2:8080")
	if breaker3 == nil {
		t.Fatal("Expected circuit breaker to be created")
	}

	if breaker3 == breaker1 {
		t.Error("Expected different circuit breaker for different host")
	}

	// Test with zero timeout
	router.config.CircuitBreaker.TimeoutSeconds = 0

	breaker4 := router.getCircuitBreaker("host3:8080")
	if breaker4 == nil {
		t.Error("Expected circuit breaker with default timeout")
	}
}

func TestRouter_RouteRequest(t *testing.T) {
	// Setup test environment
	backend, requestCount := setupTestBackend(t)
	defer backend.Close()

	router := setupTestRouter(t, backend.URL)

	// Run test cases
	runRouteRequestTests(t, router)

	// Verify backend was called
	if atomic.LoadInt32(requestCount) == 0 {
		t.Error("Expected at least one request to backend")
	}
}

func setupTestBackend(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()

	requestCount := int32(0)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		handleTestBackendRequest(t, w, r)
	}))

	return backend, &requestCount
}

func handleTestBackendRequest(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	// Handle health check requests
	if r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)

		return
	}

	// Verify request
	if r.Method != http.MethodPost {
		t.Errorf("Expected POST, got %s", r.Method)
	}

	if r.Header.Get("Content-Type") != "application/json" {
		t.Error("Expected Content-Type: application/json")
	}

	// Read and parse request
	body, _ := io.ReadAll(r.Body)

	var req mcp.Request

	_ = json.Unmarshal(body, &req)

	// Send response
	resp := mcp.Response{
		JSONRPC: "2.0",
		Result: map[string]string{
			"status": "ok",
			"method": req.Method,
		},
		ID: req.ID,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func setupTestRouter(t *testing.T, backendURL string) *Router {
	t.Helper()
	logger := zaptest.NewLogger(t)

	cfg := config.RoutingConfig{
		Strategy:            "round_robin",
		HealthCheckInterval: "30s",
		CircuitBreaker: config.CircuitBreakerConfig{
			Enabled:          false,
			FailureThreshold: 5,
			SuccessThreshold: 2,
			TimeoutSeconds:   30,
		},
	}

	registry := testutil.CreateTestMetricsRegistry()

	// Create a mock discovery that returns the backend endpoint
	parts := strings.Split(strings.TrimPrefix(backendURL, "http://"), ":")
	port := 80
	_, _ = fmt.Sscanf(parts[1], "%d", &port)

	ep := discovery.Endpoint{Address: parts[0], Port: port, Scheme: "http"}
	ep.SetHealthy(true)
	mockDiscovery := &mockServiceDiscovery{
		endpoints: map[string][]discovery.Endpoint{
			"default": {ep},
		},
	}

	return InitializeRequestRouter(context.Background(), cfg, mockDiscovery, registry, logger)
}

func runRouteRequestTests(t *testing.T, router *Router) {
	t.Helper()

	tests := []struct {
		name            string
		request         *mcp.Request
		targetNamespace string
		wantError       bool
	}{
		{
			name: "valid request",
			request: &mcp.Request{
				JSONRPC: "2.0",
				Method:  "test/method",
				ID:      "test-1",
			},
			targetNamespace: "default",
			wantError:       false,
		},
		{
			name: "invalid namespace",
			request: &mcp.Request{
				JSONRPC: "2.0",
				Method:  "test/method",
				ID:      "test-2",
			},
			targetNamespace: "nonexistent",
			wantError:       true,
		},
	}

	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := router.RouteRequest(ctx, tt.request, tt.targetNamespace)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRouter_RouteRequest_WithSession(t *testing.T) {
	sessionReceived := false

	backend := setupSessionTestBackend(&sessionReceived)
	defer backend.Close()

	router := setupSessionTestRouter(backend)
	testSessionRouting(t, router, &sessionReceived)
}

func setupSessionTestBackend(sessionReceived *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle health check requests
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)

			return
		}

		sessionID := r.Header.Get("X-MCP-Session-ID")
		user := r.Header.Get("X-MCP-User")

		if sessionID == "test-session-123" && user == "test-user" {
			*sessionReceived = true
		}

		resp := mcp.Response{
			JSONRPC: "2.0",
			Result:  "ok",
			ID:      "test-123",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func setupSessionTestRouter(backend *httptest.Server) *Router {
	parts := strings.Split(strings.TrimPrefix(backend.URL, "http://"), ":")
	port := 80
	_, _ = fmt.Sscanf(parts[1], "%d", &port)

	ep := discovery.Endpoint{Address: parts[0], Port: port, Scheme: "http"}
	ep.SetHealthy(true)
	mockDiscovery := &mockServiceDiscovery{
		endpoints: map[string][]discovery.Endpoint{
			"test": {ep},
		},
	}

	return &Router{
		config: config.RoutingConfig{
			Strategy: "round_robin",
			CircuitBreaker: config.CircuitBreakerConfig{
				FailureThreshold: 5,
				SuccessThreshold: 2,
				TimeoutSeconds:   30,
			},
		},
		discovery:       mockDiscovery,
		metrics:         testutil.CreateTestMetricsRegistry(),
		logger:          zap.NewNop(),
		balancers:       make(map[string]loadbalancer.LoadBalancer),
		breakers:        make(map[string]*circuit.CircuitBreaker),
		endpointClients: make(map[string]*http.Client),
	}
}

func testSessionRouting(t *testing.T, router *Router, sessionReceived *bool) {
	t.Helper()

	sess := &session.Session{
		ID:   "test-session-123",
		User: "test-user",
	}

	ctx := context.WithValue(context.Background(), sessionContextKey, sess)

	req := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test.method",
		ID:      "test-123",
	}

	_, err := router.RouteRequest(ctx, req, "test")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !*sessionReceived {
		t.Error("Session headers not received by backend")
	}
}

func TestRouter_forwardRequest_Errors(t *testing.T) {
	tests := createForwardRequestErrorTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testForwardRequestError(t, tt)
		})
	}
}

func createForwardRequestErrorTests() []struct {
	name          string
	handler       http.HandlerFunc
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		handler       http.HandlerFunc
		wantError     bool
		errorContains string
	}{
		{
			name: "Backend returns error status",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}),
			wantError:     true,
			errorContains: "backend returned status 500",
		},
		{
			name: "Backend returns invalid JSON",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not json"))
			}),
			wantError:     true,
			errorContains: "failed to parse response",
		},
		{
			name: "Backend closes connection",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}),
			wantError:     true,
			errorContains: "request failed",
		},
	}
}

func testForwardRequestError(t *testing.T, tt struct {
	name          string
	handler       http.HandlerFunc
	wantError     bool
	errorContains string
}) {
	t.Helper()

	backend := httptest.NewServer(tt.handler)
	defer backend.Close()

	parts := strings.Split(strings.TrimPrefix(backend.URL, "http://"), ":")
	port := 80
	_, _ = fmt.Sscanf(parts[1], "%d", &port)

	logger := testutil.NewTestLogger(t)

	router := &Router{
		endpointClients: make(map[string]*http.Client),
		logger:          logger,
	}

	endpoint := &discovery.Endpoint{
		Address: parts[0],
		Port:    port,
		Scheme:  "http",
	}

	req := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test",
		ID:      "test-123",
	}

	ctx := context.Background()
	_, err := router.forwardRequest(ctx, endpoint, req)

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")
		} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
		}
	} else {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	}
}

func TestRouter_checkEndpoint(t *testing.T) {
	tests := createCheckEndpointTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testEndpointCheck(t, tt)
		})
	}
}

func createCheckEndpointTests() []struct {
	name           string
	handler        http.HandlerFunc
	expectedHealth bool
} {
	return []struct {
		name           string
		handler        http.HandlerFunc
		expectedHealth bool
	}{
		{
			name: "Healthy endpoint",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			expectedHealth: true,
		},
		{
			name: "Unhealthy endpoint",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			}),
			expectedHealth: false,
		},
		{
			name: "Timeout",
			handler: http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				time.Sleep(3 * time.Second) // Must be > defaultHealthCheckTimeout (2s)
			}),
			expectedHealth: false,
		},
	}
}

func testEndpointCheck(t *testing.T, tt struct {
	name           string
	handler        http.HandlerFunc
	expectedHealth bool
}) {
	t.Helper()

	backend := httptest.NewServer(tt.handler)
	defer backend.Close()

	parts := strings.Split(strings.TrimPrefix(backend.URL, "http://"), ":")
	port := 80
	_, _ = fmt.Sscanf(parts[1], "%d", &port)

	router := &Router{
		endpointClients: make(map[string]*http.Client),
		logger:          zap.NewNop(),
		breakers:        make(map[string]*circuit.CircuitBreaker),
		config: config.RoutingConfig{
			CircuitBreaker: config.CircuitBreakerConfig{
				TimeoutSeconds: 10,
			},
		},
	}

	endpoint := &discovery.Endpoint{
		Address: parts[0],
		Port:    port,
		Scheme:  "http",
	}
	endpoint.SetHealthy(!tt.expectedHealth)

	router.checkEndpoint(context.Background(), endpoint)

	if endpoint.IsHealthy() != tt.expectedHealth {
		t.Errorf("Expected health %v, got %v", tt.expectedHealth, endpoint.IsHealthy())
	}
}

func TestRouter_updateLoadBalancers(t *testing.T) {
	router := &Router{
		balancers: map[string]loadbalancer.LoadBalancer{
			"test1": loadbalancer.NewRoundRobin(nil),
			"test2": loadbalancer.NewRoundRobin(nil),
		},
	}

	// Verify balancers exist
	if len(router.balancers) != 2 {
		t.Errorf("Expected 2 balancers, got %d", len(router.balancers))
	}

	router.updateLoadBalancers()

	// Verify balancers cleared
	if len(router.balancers) != 0 {
		t.Errorf("Expected 0 balancers after update, got %d", len(router.balancers))
	}
}

func TestRouter_ConcurrentAccess(t *testing.T) {
	backend := setupConcurrentTestBackend()
	defer backend.Close()

	router := setupConcurrentTestRouter(backend)
	runConcurrentRequests(t, router)
}

func setupConcurrentTestBackend() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mcp.Response{
			JSONRPC: "2.0",
			Result:  "ok",
			ID:      r.URL.Query().Get("id"),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func setupConcurrentTestRouter(backend *httptest.Server) *Router {
	parts := strings.Split(strings.TrimPrefix(backend.URL, "http://"), ":")
	port := 80
	_, _ = fmt.Sscanf(parts[1], "%d", &port)

	ep := discovery.Endpoint{Address: parts[0], Port: port, Scheme: "http"}
	ep.SetHealthy(true)
	mockDiscovery := &mockServiceDiscovery{
		endpoints: map[string][]discovery.Endpoint{
			"test": {ep},
		},
	}

	return &Router{
		config: config.RoutingConfig{
			Strategy: "round_robin",
			CircuitBreaker: config.CircuitBreakerConfig{
				FailureThreshold: testTimeout,
				SuccessThreshold: 2,
				TimeoutSeconds:   30,
			},
		},
		discovery:       mockDiscovery,
		metrics:         testutil.CreateTestMetricsRegistry(),
		logger:          zap.NewNop(),
		balancers:       make(map[string]loadbalancer.LoadBalancer),
		breakers:        make(map[string]*circuit.CircuitBreaker),
		endpointClients: make(map[string]*http.Client),
	}
}

func runConcurrentRequests(t *testing.T, router *Router) {
	t.Helper()

	var wg sync.WaitGroup

	errors := make(chan error, testIterations)

	for i := 0; i < testIterations; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			req := &mcp.Request{
				JSONRPC: "2.0",
				Method:  "test.method",
				ID:      fmt.Sprintf("test-%d", id),
			}

			ctx := context.Background()

			_, err := router.RouteRequest(ctx, req, "test")
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errorCount := 0

	for err := range errors {
		t.Logf("Concurrent request error: %v", err)

		errorCount++
	}

	if errorCount > 10 {
		t.Errorf("Too many errors: %d", errorCount)
	}
}

func TestRouter_GetRequestCount(t *testing.T) {
	router := &Router{
		requestCounter: 42,
	}

	count := router.GetRequestCount()
	if count != 42 {
		t.Errorf("Expected count 42, got %d", count)
	}

	// Test concurrent increment
	var wg sync.WaitGroup
	for i := 0; i < testIterations; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			atomic.AddUint64(&router.requestCounter, 1)
		}()
	}

	wg.Wait()

	count = router.GetRequestCount()
	if count != 142 {
		t.Errorf("Expected count 142, got %d", count)
	}
}

// Mock implementations

type mockServiceDiscovery struct {
	endpoints map[string][]discovery.Endpoint
	mu        sync.RWMutex
}

func (m *mockServiceDiscovery) GetEndpoints(namespace string) []discovery.Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.endpoints[namespace]
}

func (m *mockServiceDiscovery) GetAllEndpoints() map[string][]discovery.Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]discovery.Endpoint)
	for k, v := range m.endpoints {
		result[k] = v
	}

	return result
}

func (m *mockServiceDiscovery) RegisterEndpoint(namespace string, endpoint discovery.Endpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.endpoints[namespace] = append(m.endpoints[namespace], endpoint)

	return nil
}

func (m *mockServiceDiscovery) DeregisterEndpoint(namespace, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	endpoints := m.endpoints[namespace]
	for i, ep := range endpoints {
		if ep.Address == address {
			m.endpoints[namespace] = append(endpoints[:i], endpoints[i+1:]...)

			break
		}
	}

	return nil
}

func (m *mockServiceDiscovery) Close() error {
	return nil
}

func (m *mockServiceDiscovery) ListNamespaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	namespaces := make([]string, 0, len(m.endpoints))
	for ns := range m.endpoints {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

func (m *mockServiceDiscovery) Start(_ context.Context) error {
	return nil
}

func (m *mockServiceDiscovery) Stop() {
	// No-op
}

// RegisterEndpointChangeCallback is a no-op for mock discovery.
func (m *mockServiceDiscovery) RegisterEndpointChangeCallback(callback func(namespace string)) {
	// Mock doesn't trigger endpoint changes
}

func BenchmarkRouter_RouteRequest(b *testing.B) {
	// Create fast backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","result":"ok","id":"test"}`)
	}))
	defer backend.Close()

	parts := strings.Split(strings.TrimPrefix(backend.URL, "http://"), ":")
	port := 80
	_, _ = fmt.Sscanf(parts[1], "%d", &port) // Best effort parse, default to 80

	ep := discovery.Endpoint{Address: parts[0], Port: port, Scheme: "http"}
	ep.SetHealthy(true)
	mockDiscovery := &mockServiceDiscovery{
		endpoints: map[string][]discovery.Endpoint{
			"test": {ep},
		},
	}

	router := &Router{
		config: config.RoutingConfig{
			Strategy: "round_robin",
			CircuitBreaker: config.CircuitBreakerConfig{
				FailureThreshold: testMaxIterations,
				SuccessThreshold: 2,
				TimeoutSeconds:   30,
			},
		},
		discovery:       mockDiscovery,
		metrics:         testutil.CreateTestMetricsRegistry(),
		logger:          zap.NewNop(),
		balancers:       make(map[string]loadbalancer.LoadBalancer),
		breakers:        make(map[string]*circuit.CircuitBreaker),
		endpointClients: make(map[string]*http.Client),
	}

	req := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "benchmark.test",
		ID:      "bench-1",
	}

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := router.RouteRequest(ctx, req, "test")
		if err != nil {
			b.Fatal(err)
		}
	}
}
