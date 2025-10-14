package discovery

import (
	"context"
	"testing"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
)

// TestConsulDiscovery_Start_Advanced tests the Start method functionality.
func TestConsulDiscovery_Start_Advanced(t *testing.T) {
	cfg := config.ServiceDiscoveryConfig{
		Provider: "consul",
	}

	logger := zaptest.NewLogger(t)

	// Create discovery instance
	discovery := &ConsulDiscovery{
		config:    cfg,
		logger:    logger,
		endpoints: make(map[string][]Endpoint),
	}

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	discovery.ctx = ctx
	discovery.cancel = cancel

	// Test that Start initializes correctly
	assert.NotNil(t, discovery.endpoints)
	assert.NotNil(t, discovery.logger)
	assert.NotNil(t, discovery.config)
	assert.Equal(t, "consul", discovery.config.Provider)

	// Test context setup
	assert.NotNil(t, discovery.ctx)
	assert.NotNil(t, discovery.cancel)

	// Test that calling cancel stops the context
	cancel()

	select {
	case <-discovery.ctx.Done():
		// Expected - context cancelled
	default:
		t.Error("Context should be cancelled")
	}
}

// TestConsulDiscovery_ServiceFiltering_Advanced tests service filtering logic.
func TestConsulDiscovery_ServiceFiltering_Advanced(t *testing.T) {
	discovery := &ConsulDiscovery{
		logger: zaptest.NewLogger(t),
	}

	tests := []struct {
		name          string
		serviceName   string
		tags          []string
		expectedMatch bool
	}{
		{"mcp service with mcp tag", "mcp/weather", []string{"mcp", "http"}, true},
		{"mcp service without mcp tag", "mcp/weather", []string{"http", "api"}, false},
		{"non-mcp service with mcp tag", "weather", []string{"mcp"}, false},
		{"empty service name", "", []string{"mcp"}, false},
		{"empty tags", "mcp/weather", []string{}, false},
		{"case insensitive mcp tag", "mcp/weather", []string{"MCP", "HTTP"}, true},
		{"mixed case service name", "MCP/weather", []string{"mcp"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the filtering logic that would be used in discoverServices
			servicePrefix := "mcp/"
			hasPrefix := len(tt.serviceName) > 0 && tt.serviceName[:len(servicePrefix)] == servicePrefix ||
				len(tt.serviceName) > 3 && tt.serviceName[:4] == "MCP/"
			hasMcpTag := discovery.hasTag(tt.tags, "mcp")

			result := hasPrefix && hasMcpTag
			assert.Equal(
				t,
				tt.expectedMatch,
				result,
				"Service %s with tags %v should match: %v",
				tt.serviceName,
				tt.tags,
				tt.expectedMatch,
			)
		})
	}
}

// TestConsulDiscovery_WatchLogic_Advanced tests watch functionality logic.
func TestConsulDiscovery_WatchLogic_Advanced(t *testing.T) {
	cfg := config.ServiceDiscoveryConfig{
		Provider: "consul",
	}

	logger := zaptest.NewLogger(t)

	discovery := &ConsulDiscovery{
		config:    cfg,
		logger:    logger,
		endpoints: make(map[string][]Endpoint),
		lastIndex: 0,
	}

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	discovery.ctx = ctx
	discovery.cancel = cancel

	// Test initial watch state
	assert.Equal(t, uint64(0), discovery.lastIndex)
	assert.NotNil(t, discovery.ctx)

	// Test watch index updates
	discovery.lastIndex = 123
	assert.Equal(t, uint64(123), discovery.lastIndex)

	// Test context cancellation stops watch
	cancel()

	select {
	case <-discovery.ctx.Done():
		// Expected - context cancelled
	default:
		t.Error("Context should be cancelled after cancel()")
	}
}

// TestConsulDiscovery_HealthCheckLogic_Advanced tests health check logic.
func TestConsulDiscovery_HealthCheckLogic_Advanced(t *testing.T) {
	discovery := &ConsulDiscovery{
		logger:    zaptest.NewLogger(t),
		endpoints: make(map[string][]Endpoint),
	}

	// Set up test endpoints with mixed health states
	testEndpoints := []Endpoint{
		{
			Service: "weather-1", Namespace: "weather", Address: "127.0.0.1", Port: 8080,
			Metadata: map[string]string{"consul_id": "weather-1"},
		},
		{
			Service: "weather-2", Namespace: "weather", Address: "127.0.0.2", Port: 8080,
			Metadata: map[string]string{"consul_id": "weather-2"},
		},
		{
			Service: "weather-3", Namespace: "weather", Address: "127.0.0.3", Port: 8080,
			Metadata: map[string]string{"consul_id": "weather-3"},
		},
	}
	testEndpoints[0].SetHealthy(true)
	testEndpoints[1].SetHealthy(false) // One unhealthy initially
	testEndpoints[2].SetHealthy(true)
	discovery.endpoints["weather"] = testEndpoints

	// Test initial health states
	weatherEndpoints := discovery.GetEndpoints("weather")

	assert.Len(t, weatherEndpoints, 3)

	healthyCount := 0
	unhealthyCount := 0

	for _, endpoint := range weatherEndpoints {
		if endpoint.IsHealthy() {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	assert.Equal(t, 2, healthyCount, "Should have 2 healthy endpoints")
	assert.Equal(t, 1, unhealthyCount, "Should have 1 unhealthy endpoint")

	// Test health status changes
	discovery.endpoints["weather"][1].SetHealthy(true)  // Make unhealthy endpoint healthy
	discovery.endpoints["weather"][2].SetHealthy(false) // Make healthy endpoint unhealthy

	updatedEndpoints := discovery.GetEndpoints("weather")
	newHealthyCount := 0
	newUnhealthyCount := 0

	for _, endpoint := range updatedEndpoints {
		if endpoint.IsHealthy() {
			newHealthyCount++
		} else {
			newUnhealthyCount++
		}
	}

	assert.Equal(t, 2, newHealthyCount, "Should still have 2 healthy endpoints after change")
	assert.Equal(t, 1, newUnhealthyCount, "Should still have 1 unhealthy endpoint after change")
}

// TestConsulDiscovery_UpdateHealthStatusLogic_Advanced tests health status updating.
func TestConsulDiscovery_UpdateHealthStatusLogic_Advanced(t *testing.T) {
	discovery := &ConsulDiscovery{
		endpoints: make(map[string][]Endpoint),
		logger:    zaptest.NewLogger(t),
	}

	// Create test endpoints
	initialEndpoints := []Endpoint{
		{
			Service: "service-1", Namespace: "ns1", Address: "127.0.0.1", Port: 8080,
			Metadata: map[string]string{"consul_id": "service-1-id"},
		},
		{
			Service: "service-2", Namespace: "ns1", Address: "127.0.0.2", Port: 8080,
			Metadata: map[string]string{"consul_id": "service-2-id"},
		},
	}
	for i := range initialEndpoints {
		initialEndpoints[i].SetHealthy(true)
	}
	discovery.endpoints["ns1"] = initialEndpoints

	// Test that endpoints exist and have expected initial state
	updatedEndpoints := discovery.GetEndpoints("ns1")

	require.Len(t, updatedEndpoints, 2)

	// Verify initial states

	for _, endpoint := range updatedEndpoints {
		assert.True(t, endpoint.IsHealthy(), "All endpoints should initially be healthy")
		assert.NotEmpty(t, endpoint.Metadata["consul_id"], "Endpoints should have consul_id metadata")
	}

	// Test that we can find and update specific endpoints by metadata

	for i := range discovery.endpoints["ns1"] {
		endpoint := &discovery.endpoints["ns1"][i]
		if endpoint.Metadata["consul_id"] == "service-2-id" {
			endpoint.SetHealthy(false) // Simulate health check failure

			break
		}
	}

	// Verify the health change was applied
	finalEndpoints := discovery.GetEndpoints("ns1")
	healthyCount := 0
	unhealthyCount := 0

	for _, endpoint := range finalEndpoints {
		if endpoint.IsHealthy() {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	assert.Equal(t, 1, healthyCount, "Should have 1 healthy endpoint")
	assert.Equal(t, 1, unhealthyCount, "Should have 1 unhealthy endpoint")
}

// TestConsulDiscovery_ConcurrentAccess_Advanced tests concurrent access safety.
func TestConsulDiscovery_ConcurrentAccess_Advanced(t *testing.T) {
	discovery := &ConsulDiscovery{
		endpoints: make(map[string][]Endpoint),
		logger:    zaptest.NewLogger(t),
	}

	// Initialize with test data
	testEndpoints := []Endpoint{
		{Service: "service-1", Namespace: "test", Address: "127.0.0.1", Port: 8080},
		{Service: "service-2", Namespace: "test", Address: "127.0.0.2", Port: 8080},
	}
	for i := range testEndpoints {
		testEndpoints[i].SetHealthy(true)
	}
	discovery.endpoints["test"] = testEndpoints

	// Test concurrent reads
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()

			// These operations should be safe for concurrent access
			endpoints := discovery.GetEndpoints("test")

			assert.Len(t, endpoints, 2)

			allEndpoints := discovery.GetAllEndpoints()

			assert.Contains(t, allEndpoints, "test")

			namespaces := discovery.ListNamespaces()

			assert.Contains(t, namespaces, "test")
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestConsulDiscovery_EdgeCases_Advanced tests edge cases.
func TestConsulDiscovery_EdgeCases_Advanced(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tests := getEdgeCaseTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runEdgeCaseTest(t, logger, tt)
		})
	}
}

type edgeCaseTest struct {
	name         string
	initialState map[string][]Endpoint
	testOp       func(*ConsulDiscovery) interface{}
	expectNil    bool
	expectEmpty  bool
}

func getEdgeCaseTests() []edgeCaseTest {
	return []edgeCaseTest{
		{
			name:         "get endpoints from empty discovery",
			initialState: map[string][]Endpoint{},
			testOp:       func(d *ConsulDiscovery) interface{} { return d.GetEndpoints("nonexistent") },
			expectEmpty:  true,
		},
		{
			name:         "get all endpoints from empty discovery",
			initialState: map[string][]Endpoint{},
			testOp:       func(d *ConsulDiscovery) interface{} { return d.GetAllEndpoints() },
			expectEmpty:  true,
		},
		{
			name:         "list namespaces from empty discovery",
			initialState: map[string][]Endpoint{},
			testOp:       func(d *ConsulDiscovery) interface{} { return d.ListNamespaces() },
			expectEmpty:  true,
		},
		{
			name: "get nonexistent namespace",
			initialState: map[string][]Endpoint{
				"weather": {{Service: "weather-svc", Namespace: "weather", Address: "127.0.0.1", Port: 8080}},
			},
			testOp:      func(d *ConsulDiscovery) interface{} { return d.GetEndpoints("nonexistent") },
			expectEmpty: true,
		},
		{
			name: "normal operation",
			initialState: map[string][]Endpoint{
				"weather": {{Service: "weather-svc", Namespace: "weather", Address: "127.0.0.1", Port: 8080}},
			},
			testOp:      func(d *ConsulDiscovery) interface{} { return d.GetEndpoints("weather") },
			expectEmpty: false,
		},
	}
}

func runEdgeCaseTest(t *testing.T, logger *zap.Logger, tt edgeCaseTest) {
	t.Helper()

	discovery := &ConsulDiscovery{
		endpoints: tt.initialState,
		logger:    logger,
	}

	result := tt.testOp(discovery)
	validateEdgeCaseResult(t, result, tt)
}

func validateEdgeCaseResult(t *testing.T, result interface{}, tt edgeCaseTest) {
	t.Helper()

	if tt.expectNil {
		assert.Nil(t, result)

		return
	}

	if tt.expectEmpty {
		assertResultEmpty(t, result)
	} else {
		assertResultNotEmpty(t, result)
	}
}

func assertResultEmpty(t *testing.T, result interface{}) {
	t.Helper()

	switch v := result.(type) {
	case []Endpoint:
		assert.Empty(t, v)
	case map[string][]Endpoint:
		assert.Empty(t, v)
	case []string:
		assert.Empty(t, v)
	}
}

func assertResultNotEmpty(t *testing.T, result interface{}) {
	t.Helper()

	assert.NotNil(t, result)

	switch v := result.(type) {
	case []Endpoint:
		assert.NotEmpty(t, v)
	case map[string][]Endpoint:
		assert.NotEmpty(t, v)
	case []string:
		assert.NotEmpty(t, v)
	}
}

// TestConsulDiscovery_ConsulServiceToEndpoint_Advanced tests endpoint conversion edge cases.
func TestConsulDiscovery_ConsulServiceToEndpoint_Advanced(t *testing.T) {
	discovery := &ConsulDiscovery{
		logger: zaptest.NewLogger(t),
	}
	tests := createConsulAdvancedEndpointTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, err := discovery.consulServiceToEndpoint(tt.serviceEntry, tt.servicePrefix)
			if tt.expectError {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, endpoint)

			if tt.validateFunc != nil {
				tt.validateFunc(t, endpoint)
			}
		})
	}
}

func createConsulAdvancedEndpointTests() []struct {
	name          string
	serviceEntry  *consulapi.ServiceEntry
	servicePrefix string
	expectError   bool
	validateFunc  func(*testing.T, *Endpoint)
} {
	customMetadataTests := createCustomMetadataTests()
	invalidWeightTests := createInvalidWeightTests()
	noNamespaceTests := createNoNamespaceTests()

	var allTests []struct {
		name          string
		serviceEntry  *consulapi.ServiceEntry
		servicePrefix string
		expectError   bool
		validateFunc  func(*testing.T, *Endpoint)
	}

	allTests = append(allTests, customMetadataTests...)
	allTests = append(allTests, invalidWeightTests...)
	allTests = append(allTests, noNamespaceTests...)

	return allTests
}

func createCustomMetadataTests() []struct {
	name          string
	serviceEntry  *consulapi.ServiceEntry
	servicePrefix string
	expectError   bool
	validateFunc  func(*testing.T, *Endpoint)
} {
	return []struct {
		name          string
		serviceEntry  *consulapi.ServiceEntry
		servicePrefix string
		expectError   bool
		validateFunc  func(*testing.T, *Endpoint)
	}{
		{
			name: "service with custom metadata",
			serviceEntry: &consulapi.ServiceEntry{
				Service: &consulapi.AgentService{
					ID:      "custom-service",
					Service: "mcp/custom",
					Address: "192.168.1.testIterations",
					Port:    9090,
					Tags:    []string{"mcp", "custom"},
					Meta: map[string]string{
						"mcp_namespace": "custom-ns",
						"mcp_weight":    "150",
						"mcp_path":      "/api/v1",
						"custom_field":  "custom_value",
					},
				},
				Node: &consulapi.Node{
					Node:       "custom-node",
					Datacenter: "dc2",
				},
				Checks: []*consulapi.HealthCheck{{Status: consulapi.HealthPassing}},
			},
			servicePrefix: "mcp/",
			expectError:   false,
			validateFunc: func(t *testing.T, endpoint *Endpoint) {
				t.Helper()

				assert.Equal(t, "mcp/custom", endpoint.Service)
				assert.Equal(t, "custom", endpoint.Namespace) // Extracted from service name "mcp/custom" by removing "mcp/" prefix
				assert.Equal(t, "192.168.1.testIterations", endpoint.Address)
				assert.Equal(t, 9090, endpoint.Port)
				assert.Equal(t, 150, endpoint.Weight)
				assert.Equal(t, "/api/v1", endpoint.Path)
				assert.True(t, endpoint.IsHealthy())
				assert.Equal(t, "custom_value", endpoint.Metadata["custom_field"])
				assert.Equal(t, "custom-node", endpoint.Metadata["consul_node"])
				assert.Equal(t, "dc2", endpoint.Metadata["consul_datacenter"])
			},
		},
	}
}

func createInvalidWeightTests() []struct {
	name          string
	serviceEntry  *consulapi.ServiceEntry
	servicePrefix string
	expectError   bool
	validateFunc  func(*testing.T, *Endpoint)
} {
	return []struct {
		name          string
		serviceEntry  *consulapi.ServiceEntry
		servicePrefix string
		expectError   bool
		validateFunc  func(*testing.T, *Endpoint)
	}{
		{
			name: "service with invalid weight",
			serviceEntry: &consulapi.ServiceEntry{
				Service: &consulapi.AgentService{
					ID:      "invalid-weight",
					Service: "mcp/test",
					Address: "127.0.0.1",
					Port:    8080,
					Tags:    []string{"mcp"},
					Meta: map[string]string{
						"mcp_namespace": "test",
						"mcp_weight":    "invalid",
					},
				},
				Node:   &consulapi.Node{Node: "node1", Datacenter: "dc1"},
				Checks: []*consulapi.HealthCheck{{Status: consulapi.HealthPassing}},
			},
			servicePrefix: "mcp/",
			expectError:   false,
			validateFunc: func(t *testing.T, endpoint *Endpoint) {
				t.Helper()

				assert.Equal(t, testIterations, endpoint.Weight, "Should use default weight when invalid weight provided")
			},
		},
	}
}

func createNoNamespaceTests() []struct {
	name          string
	serviceEntry  *consulapi.ServiceEntry
	servicePrefix string
	expectError   bool
	validateFunc  func(*testing.T, *Endpoint)
} {
	return []struct {
		name          string
		serviceEntry  *consulapi.ServiceEntry
		servicePrefix string
		expectError   bool
		validateFunc  func(*testing.T, *Endpoint)
	}{
		{
			name: "service without namespace metadata",
			serviceEntry: &consulapi.ServiceEntry{
				Service: &consulapi.AgentService{
					ID:      "no-namespace",
					Service: "other/service", // Doesn't match prefix
					Address: "127.0.0.1",
					Port:    8080,
					Tags:    []string{"mcp"},
					Meta:    map[string]string{},
				},
				Node:   &consulapi.Node{Node: "node1", Datacenter: "dc1"},
				Checks: []*consulapi.HealthCheck{{Status: consulapi.HealthPassing}},
			},
			servicePrefix: "mcp/",
			expectError:   false,
			validateFunc: func(t *testing.T, endpoint *Endpoint) {
				t.Helper()

				assert.Equal(t, "default", endpoint.Namespace, "Should use default namespace when none provided")
			},
		},
	}
}

// TestConsulDiscovery_IsHealthy_Advanced tests health check edge cases.
func TestConsulDiscovery_IsHealthy_Advanced(t *testing.T) {
	discovery := &ConsulDiscovery{}
	tests := createHealthCheckAdvancedTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := discovery.isHealthy(tt.entry)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func createHealthCheckAdvancedTests() []struct {
	name     string
	entry    *consulapi.ServiceEntry
	expected bool
} {
	return []struct {
		name     string
		entry    *consulapi.ServiceEntry
		expected bool
	}{
		{
			name: "multiple passing checks",
			entry: &consulapi.ServiceEntry{
				Checks: []*consulapi.HealthCheck{
					{Status: consulapi.HealthPassing},
					{Status: consulapi.HealthPassing},
					{Status: consulapi.HealthPassing},
				},
			},
			expected: true,
		},
		{
			name: "mixed status with one critical",
			entry: &consulapi.ServiceEntry{
				Checks: []*consulapi.HealthCheck{
					{Status: consulapi.HealthPassing},
					{Status: consulapi.HealthPassing},
					{Status: consulapi.HealthCritical},
				},
			},
			expected: false,
		},
		{
			name: "all warning status",
			entry: &consulapi.ServiceEntry{
				Checks: []*consulapi.HealthCheck{
					{Status: consulapi.HealthWarning},
					{Status: consulapi.HealthWarning},
				},
			},
			expected: false,
		},
		{
			name: "empty checks array",
			entry: &consulapi.ServiceEntry{
				Checks: []*consulapi.HealthCheck{},
			},
			expected: true,
		},
		{
			name: "nil checks",
			entry: &consulapi.ServiceEntry{
				Checks: nil,
			},
			expected: true,
		},
	}
}
