package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"testing"
	"time"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
)

const (
	testIterations = 100
	httpStatusOK   = 200
	testTimeout    = 50
)

// MockConsulClient mocks the Consul API client.
type MockConsulClient struct {
	mock.Mock
}

func (m *MockConsulClient) Address() string {
	args := m.Called()

	return args.String(0)
}

func (m *MockConsulClient) Agent() *consulapi.Agent {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}

	agent, ok := args.Get(0).(*consulapi.Agent)
	if !ok {
		return nil
	}

	return agent
}

func (m *MockConsulClient) Catalog() *consulapi.Catalog {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}

	catalog, ok := args.Get(0).(*consulapi.Catalog)
	if !ok {
		return nil
	}

	return catalog
}

func (m *MockConsulClient) Health() *consulapi.Health {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}

	health, ok := args.Get(0).(*consulapi.Health)
	if !ok {
		return nil
	}
	return health
}

// MockCatalog mocks the Consul catalog interface.
type MockCatalog struct {
	mock.Mock
}

func (m *MockCatalog) Services(opts *consulapi.QueryOptions) (map[string][]string, *consulapi.QueryMeta, error) {
	args := m.Called(opts)

	var services map[string][]string
	if args.Get(0) != nil {
		services, _ = args.Get(0).(map[string][]string)
	}

	var meta *consulapi.QueryMeta
	if args.Get(1) != nil {
		meta, _ = args.Get(1).(*consulapi.QueryMeta)
	}

	return services, meta, args.Error(2)
}

// MockHealth mocks the Consul health interface.
type MockHealth struct {
	mock.Mock
}

func (m *MockHealth) Service(
	service, tag string,
	passingOnly bool,
	opts *consulapi.QueryOptions,
) ([]*consulapi.ServiceEntry, *consulapi.QueryMeta, error) {
	args := m.Called(service, tag, passingOnly, opts)

	var entries []*consulapi.ServiceEntry
	if args.Get(0) != nil {
		entries, _ = args.Get(0).([]*consulapi.ServiceEntry)
	}

	var meta *consulapi.QueryMeta
	if args.Get(1) != nil {
		meta, _ = args.Get(1).(*consulapi.QueryMeta)
	}

	return entries, meta, args.Error(2)
}

func TestInitializeConsulServiceDiscovery(t *testing.T) {
	cfg := config.ServiceDiscoveryConfig{
		Provider: "consul",
	}

	consulCfg := ConsulConfig{
		Address:       "localhost:8500",
		ServicePrefix: "mcp/",
		Datacenter:    "dc1",
		Token:         "test-token",
		TLSEnabled:    true,
		TLSSkipVerify: true,
		WatchTimeout:  "30s",
	}

	logger := zaptest.NewLogger(t)

	// This test would need to be mocked or integration test
	discovery, err := InitializeConsulServiceDiscovery(cfg, consulCfg, logger)
	if err != nil {
		// Expected since we don't have actual Consul running
		t.Skip("Skipping test that requires actual Consul instance")
	}

	require.NotNil(t, discovery)
	assert.Equal(t, cfg, discovery.config)
	assert.Equal(t, logger, discovery.logger)
}

func TestConsulDiscovery_consulServiceToEndpoint_BasicHTTP(t *testing.T) {
	cfg := config.ServiceDiscoveryConfig{
		Provider: "consul",
	}

	logger := zaptest.NewLogger(t)

	discovery := &ConsulDiscovery{
		config: cfg,
		logger: logger,
	}

	// Test basic HTTP service
	serviceEntry := &consulapi.ServiceEntry{
		Service: &consulapi.AgentService{
			ID:      "mcp-weather-1",
			Service: "mcp/weather",
			Address: "127.0.0.1",
			Port:    8080,
			Tags:    []string{"mcp", "http"},
			Meta: map[string]string{
				"mcp_namespace": "weather",
				"mcp_weight":    "testIterations",
				"mcp_path":      "/mcp",
			},
		},
		Node: &consulapi.Node{
			Node:       "node1",
			Datacenter: "dc1",
		},
		Checks: []*consulapi.HealthCheck{
			{Status: consulapi.HealthPassing},
		},
	}

	endpoint, err := discovery.consulServiceToEndpoint(serviceEntry, "mcp/")

	require.NoError(t, err)

	assert.Equal(t, "mcp/weather", endpoint.Service)
	assert.Equal(t, "weather", endpoint.Namespace)
	assert.Equal(t, "127.0.0.1", endpoint.Address)
	assert.Equal(t, 8080, endpoint.Port)
	assert.Equal(t, "http", endpoint.Scheme)
	assert.Equal(t, "/mcp", endpoint.Path)
	assert.Equal(t, testIterations, endpoint.Weight)
	assert.True(t, endpoint.Healthy)
}

func TestConsulDiscovery_consulServiceToEndpoint_WebSocketTLS(t *testing.T) {
	cfg := config.ServiceDiscoveryConfig{
		Provider: "consul",
	}

	logger := zaptest.NewLogger(t)

	discovery := &ConsulDiscovery{
		config: cfg,
		logger: logger,
	}

	// Test WebSocket service with TLS
	serviceEntry := &consulapi.ServiceEntry{
		Service: &consulapi.AgentService{
			ID:      "mcp-chat-1",
			Service: "mcp/chat",
			Address: "127.0.0.1",
			Port:    8443,
			Tags:    []string{"mcp", "websocket", "tls"},
			Meta: map[string]string{
				"mcp_protocol": "websocket",
				"mcp_tools":    `[{"name":"send_message","description":"Send a chat message"}]`,
			},
		},
		Node: &consulapi.Node{
			Node:       "node2",
			Datacenter: "dc1",
		},
		Checks: []*consulapi.HealthCheck{
			{Status: consulapi.HealthPassing},
		},
	}

	endpoint, err := discovery.consulServiceToEndpoint(serviceEntry, "mcp/")

	require.NoError(t, err)

	assert.Equal(t, "mcp/chat", endpoint.Service)
	assert.Equal(t, "chat", endpoint.Namespace)
	assert.Equal(t, "wss", endpoint.Scheme)
	assert.Len(t, endpoint.Tools, 1)
	assert.Equal(t, "send_message", endpoint.Tools[0].Name)
}

func TestConsulDiscovery_inferProtocol(t *testing.T) {
	discovery := &ConsulDiscovery{}

	tests := []struct {
		name     string
		service  *consulapi.AgentService
		expected string
	}{
		{
			name: "Explicit protocol in metadata",
			service: &consulapi.AgentService{
				Meta: map[string]string{"mcp_protocol": "custom"},
				Tags: []string{"http"},
			},
			expected: "custom",
		},
		{
			name: "WebSocket from tags",
			service: &consulapi.AgentService{
				Tags: []string{"mcp", "websocket"},
			},
			expected: "websocket",
		},
		{
			name: "SSE from tags",
			service: &consulapi.AgentService{
				Tags: []string{"mcp", "sse"},
			},
			expected: "sse",
		},
		{
			name: "Stdio from tags",
			service: &consulapi.AgentService{
				Tags: []string{"mcp", "stdio"},
			},
			expected: "stdio",
		},
		{
			name: "HTTP from tags",
			service: &consulapi.AgentService{
				Tags: []string{"mcp", "http"},
			},
			expected: "http",
		},
		{
			name: "Default to HTTP",
			service: &consulapi.AgentService{
				Tags: []string{"mcp"},
			},
			expected: "http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			protocol := discovery.inferProtocol(tt.service)
			assert.Equal(t, tt.expected, protocol)
		})
	}
}

func TestConsulDiscovery_hasTag(t *testing.T) {
	discovery := &ConsulDiscovery{}

	tests := []struct {
		name     string
		tags     []string
		tag      string
		expected bool
	}{
		{
			name:     "Tag exists",
			tags:     []string{"mcp", "http", "v1"},
			tag:      "http",
			expected: true,
		},
		{
			name:     "Tag exists case insensitive",
			tags:     []string{"MCP", "HTTP"},
			tag:      "mcp",
			expected: true,
		},
		{
			name:     "Tag does not exist",
			tags:     []string{"mcp", "http"},
			tag:      "websocket",
			expected: false,
		},
		{
			name:     "Empty tags",
			tags:     []string{},
			tag:      "mcp",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := discovery.hasTag(tt.tags, tt.tag)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConsulDiscovery_isHealthy(t *testing.T) {
	discovery := &ConsulDiscovery{}

	tests := []struct {
		name     string
		entry    *consulapi.ServiceEntry
		expected bool
	}{
		{
			name: "All checks passing",
			entry: &consulapi.ServiceEntry{
				Checks: []*consulapi.HealthCheck{
					{Status: consulapi.HealthPassing},
					{Status: consulapi.HealthPassing},
				},
			},
			expected: true,
		},
		{
			name: "One check failing",
			entry: &consulapi.ServiceEntry{
				Checks: []*consulapi.HealthCheck{
					{Status: consulapi.HealthPassing},
					{Status: consulapi.HealthCritical},
				},
			},
			expected: false,
		},
		{
			name: "Warning status",
			entry: &consulapi.ServiceEntry{
				Checks: []*consulapi.HealthCheck{
					{Status: consulapi.HealthWarning},
				},
			},
			expected: false,
		},
		{
			name: "No checks",
			entry: &consulapi.ServiceEntry{
				Checks: []*consulapi.HealthCheck{},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := discovery.isHealthy(tt.entry)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConsulDiscovery_GetEndpoints(t *testing.T) {
	discovery := &ConsulDiscovery{
		endpoints: map[string][]Endpoint{
			"weather": {
				{Service: "weather-1", Namespace: "weather", Address: "127.0.0.1", Port: 8080},
				{Service: "weather-2", Namespace: "weather", Address: "127.0.0.2", Port: 8080},
			},
			"chat": {
				{Service: "chat-1", Namespace: "chat", Address: "127.0.0.3", Port: 8443},
			},
		},
	}

	// Test getting endpoints for existing namespace
	weatherEndpoints := discovery.GetEndpoints("weather")

	assert.Len(t, weatherEndpoints, 2)
	assert.Equal(t, "weather-1", weatherEndpoints[0].Service)
	assert.Equal(t, "weather-2", weatherEndpoints[1].Service)

	// Test getting endpoints for non-existing namespace
	nonExistentEndpoints := discovery.GetEndpoints("non-existent")

	assert.Empty(t, nonExistentEndpoints)
}

func TestConsulDiscovery_GetAllEndpoints(t *testing.T) {
	discovery := &ConsulDiscovery{
		endpoints: map[string][]Endpoint{
			"weather": {
				{Service: "weather-1", Namespace: "weather"},
			},
			"chat": {
				{Service: "chat-1", Namespace: "chat"},
			},
		},
	}

	allEndpoints := discovery.GetAllEndpoints()

	assert.Len(t, allEndpoints, 2)
	assert.Contains(t, allEndpoints, "weather")
	assert.Contains(t, allEndpoints, "chat")
	assert.Len(t, allEndpoints["weather"], 1)
	assert.Len(t, allEndpoints["chat"], 1)
}

func TestConsulDiscovery_ListNamespaces(t *testing.T) {
	discovery := &ConsulDiscovery{
		endpoints: map[string][]Endpoint{
			"weather":   {{Service: "weather-1"}},
			"chat":      {{Service: "chat-1"}},
			"analytics": {{Service: "analytics-1"}},
		},
	}

	namespaces := discovery.ListNamespaces()

	assert.Len(t, namespaces, 3)
	assert.Contains(t, namespaces, "weather")
	assert.Contains(t, namespaces, "chat")
	assert.Contains(t, namespaces, "analytics")
}

// Integration test with actual Consul instance setup/teardown.
func TestConsulDiscovery_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cleanup, consulAddr, err := setupConsulInfrastructure(t)
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	defer cleanup()

	client := createConsulClient(t, consulAddr)
	testConsulConnectivity(t, client)
	serviceInfo := registerTestService(t, client)
	testServiceQuery(t, client, serviceInfo)
	testConsulDiscoveryIntegration(t, consulAddr, serviceInfo)
	cleanupTestService(t, client, serviceInfo.ID)
}

type testServiceInfo struct {
	ID   string
	Name string
	Port int
}

func createConsulClient(t *testing.T, consulAddr string) *consulapi.Client {
	t.Helper()

	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = consulAddr
	client, err := consulapi.NewClient(consulConfig)

	require.NoError(t, err, "Failed to create Consul client")

	return client
}

func testConsulConnectivity(t *testing.T, client *consulapi.Client) {
	t.Helper()

	leader, err := client.Status().Leader()
	require.NoError(t, err, "Failed to get Consul leader")
	assert.NotEmpty(t, leader, "Consul leader should be set")
}

func registerTestService(t *testing.T, client *consulapi.Client) testServiceInfo {
	t.Helper()

	serviceInfo := testServiceInfo{
		ID:   "test-service-1",
		Name: "test-mcp-service",
		Port: 8080,
	}
	registration := &consulapi.AgentServiceRegistration{
		ID:      serviceInfo.ID,
		Name:    serviceInfo.Name,
		Port:    serviceInfo.Port,
		Address: "localhost",
		Check: &consulapi.AgentServiceCheck{
			HTTP:     fmt.Sprintf("http://localhost:%d/health", serviceInfo.Port),
			Interval: "10s",
			Timeout:  "5s",
		},
	}
	err := client.Agent().ServiceRegister(registration)
	require.NoError(t, err, "Failed to register service")
	time.Sleep(2 * time.Second) // Wait for registration to propagate

	return serviceInfo
}

func testServiceQuery(t *testing.T, client *consulapi.Client, serviceInfo testServiceInfo) {
	t.Helper()

	services, _, err := client.Health().Service(serviceInfo.Name, "", false, nil)
	require.NoError(t, err, "Failed to query service")
	assert.Len(t, services, 1, "Should find exactly one service")
	assert.Equal(t, serviceInfo.ID, services[0].Service.ID)
	assert.Equal(t, serviceInfo.Name, services[0].Service.Service)
	assert.Equal(t, serviceInfo.Port, services[0].Service.Port)
}

func testConsulDiscoveryIntegration(t *testing.T, consulAddr string, serviceInfo testServiceInfo) {
	t.Helper()

	cfg := config.ServiceDiscoveryConfig{
		Provider: "consul",
	}
	consulCfg := ConsulConfig{
		Address: consulAddr,
	}
	logger := zaptest.NewLogger(t)
	discovery, err := InitializeConsulServiceDiscovery(cfg, consulCfg, logger)

	require.NoError(t, err, "Failed to create ConsulDiscovery")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	defer cancel()

	err = discovery.Start(ctx)

	require.NoError(t, err, "Failed to start discovery")

	endpoints := discovery.GetEndpoints("default")

	t.Logf("Found %d endpoints", len(endpoints))

	discovery.Stop()
}

func cleanupTestService(t *testing.T, client *consulapi.Client, serviceID string) {
	t.Helper()

	err := client.Agent().ServiceDeregister(serviceID)
	require.NoError(t, err, "Failed to deregister service")
}

func TestConsulDiscovery_StartStop(t *testing.T) {
	cfg := config.ServiceDiscoveryConfig{
		Provider: "consul",
	}

	logger := zaptest.NewLogger(t)

	// Mock discovery for testing start/stop lifecycle
	discovery := &ConsulDiscovery{
		config:    cfg,
		logger:    logger,
		endpoints: make(map[string][]Endpoint),
	}

	ctx, cancel := context.WithCancel(context.Background())
	discovery.ctx = ctx
	discovery.cancel = cancel

	// Test stop (should not panic)
	discovery.Stop()

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context should be cancelled after Stop()")
	}
}

// setupConsulInfrastructure starts a Consul container for testing.
func setupConsulInfrastructure(t *testing.T) (cleanup func(), consulAddr string, err error) {
	t.Helper()

	ctx := context.Background() // Test infrastructure context

	// Check if Docker is available
	if !isDockerAvailable() {
		return nil, "", errors.New("Docker is not available for Consul setup")
	}

	// Find an available port for Consul
	port, err := findAvailablePort()
	if err != nil {
		return nil, "", fmt.Errorf("failed to find available port: %w", err)
	}

	// Start Consul container with sanitized name
	containerName := fmt.Sprintf("consul-test-%d", time.Now().Unix())
	// Validate container name contains only safe characters
	if !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(containerName) {
		return nil, "", fmt.Errorf("invalid container name: %s", containerName)
	}

	consulAddr = fmt.Sprintf("localhost:%d", port)

	// Start Consul in dev mode for testing
	// containerName is validated with regex above (line 605), port comes from findAvailablePort

	cmd := exec.CommandContext(ctx, "docker", "run", "-d", "--rm", //nolint:gosec // Legitimate test Docker command
		"--name", containerName,
		"-p", fmt.Sprintf("%d:8500", port),
		"consul:1.16",
		"agent", "-dev", "-client", "0.0.0.0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("failed to start Consul container: %w, output: %s", err, string(output))
	}

	t.Logf("Started Consul container %s on port %d", containerName, port)

	// Wait for Consul to be ready

	if err := waitForConsul(consulAddr, 30*time.Second); err != nil {
		// Cleanup on failure
		_ = exec.CommandContext(ctx, "docker", "stop", containerName).Run() //nolint:gosec // Test cleanup command
		return nil, "", fmt.Errorf("Consul failed to become ready: %w", err)
	}

	t.Logf("Consul is ready at %s", consulAddr)

	cleanup = func() {
		t.Logf("Stopping Consul container %s", containerName)

		cmd := exec.CommandContext(ctx, "docker", "stop", containerName) //nolint:gosec // Test cleanup command
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Logf("Failed to stop Consul container: %v, output: %s", err, string(output))
		}
	}

	return cleanup, consulAddr, nil
}

// isDockerAvailable checks if Docker daemon is running.

func isDockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "ps")
	return cmd.Run() == nil
}

// findAvailablePort finds an available port on localhost.
func findAvailablePort() (int, error) {
	lc := &net.ListenConfig{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	listener, err := lc.Listen(ctx, "tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	defer func() {
		if err := listener.Close(); err != nil {
			// Best effort close in test helper
			_ = err
		}
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("expected TCP address, got %T", listener.Addr())
	}
	return addr.Port, nil
}

// waitForConsul waits for Consul to become ready.
func waitForConsul(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Try to create a client and check if Consul is ready
		config := consulapi.DefaultConfig()
		config.Address = addr

		client, err := consulapi.NewClient(config)
		if err != nil {
			time.Sleep(1 * time.Second)

			continue
		}

		// Try to get the leader to verify Consul is working
		_, err = client.Status().Leader()
		if err == nil {
			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("Consul at %s did not become ready within %v", addr, timeout)
}
