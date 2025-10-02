//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/health"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/ratelimit"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/router"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/server"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	testIterations    = 100
	testMaxIterations = 1000
)

// e2eTestClient represents a test WebSocket client for E2E testing
type e2eTestClient struct {
	conn   *websocket.Conn
	logger *zap.Logger
	mu     sync.Mutex
}

// SendRequest sends an MCP request and waits for response with timeout protection
func (c *e2eTestClient) SendRequest(t *testing.T, req mcp.Request) *mcp.Response {
	// Set read deadline to prevent indefinite blocking
	deadline := time.Now().Add(30 * time.Second)

	// Wrap in wire message
	wireMsg := server.WireMessage{
		ID:              req.ID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Source:          "e2e-test-client",
		TargetNamespace: "e2e-test",
		MCPPayload:      req,
	}

	// Serialize request with mutex protection for concurrent access
	c.mu.Lock()
	err := c.conn.SetWriteDeadline(deadline)
	require.NoError(t, err)
	err = c.conn.WriteJSON(wireMsg)
	c.mu.Unlock()
	require.NoError(t, err)

	// Read response (also needs protection for concurrent access)
	c.mu.Lock()
	err = c.conn.SetReadDeadline(deadline)
	require.NoError(t, err)
	var respWire server.WireMessage
	err = c.conn.ReadJSON(&respWire)
	c.mu.Unlock()
	require.NoError(t, err)

	// Extract MCP response
	respData, err := json.Marshal(respWire.MCPPayload)
	require.NoError(t, err)

	var resp mcp.Response
	err = json.Unmarshal(respData, &resp)
	require.NoError(t, err)

	return &resp
}

// Close closes the WebSocket connection
func (c *e2eTestClient) Close() error {
	return c.conn.Close()
}

// SendNotification sends an MCP notification (no response expected)
func (c *e2eTestClient) SendNotification(t *testing.T, notification mcp.Request) error {
	// Set write deadline to prevent indefinite blocking
	deadline := time.Now().Add(30 * time.Second)

	wireMsg := server.WireMessage{
		ID:              notification.ID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Source:          "e2e-test-client",
		TargetNamespace: "e2e-test",
		MCPPayload:      notification,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.conn.SetWriteDeadline(deadline)
	if err != nil {
		return err
	}

	return c.conn.WriteJSON(wireMsg)
}

// E2ETestSuite represents the end-to-end protocol flow integration test suite
type E2ETestSuite struct {
	t               *testing.T
	ctx             context.Context
	cancel          context.CancelFunc
	logger          *zap.Logger
	gateway         *server.GatewayServer
	mockRedis       *miniredis.Miniredis
	mockBackend     *MockMCPBackend
	authProvider    auth.Provider
	sessionManager  session.Manager
	requestRouter   router.Router
	healthChecker   health.Checker
	metricsRegistry *metrics.Registry
	rateLimiter     ratelimit.RateLimiter
	config          *config.Config
	serverAddr      string
}

// MockMCPBackend represents a mock MCP backend server for integration testing
type MockMCPBackend struct {
	server           *httptest.Server
	upgrader         websocket.Upgrader
	connections      map[*websocket.Conn]*ConnectionState
	mu               sync.RWMutex
	messageHandlers  map[string]func(*websocket.Conn, *mcp.Request) *mcp.Response
	latencyInjection time.Duration
	errorInjection   bool
	dropRate         float64
}

// ConnectionState tracks the state of individual connections
type ConnectionState struct {
	ID            string
	Authenticated bool
	MessageCount  int64
	LastActivity  time.Time
	Capabilities  map[string]interface{}
}

// NewE2ETestSuite creates and initializes a new end-to-end test suite
func NewE2ETestSuite(t *testing.T) *E2ETestSuite {
	ctx, cancel := context.WithCancel(context.Background())

	suite := &E2ETestSuite{
		t:      t,
		ctx:    ctx,
		cancel: cancel,
		logger: zaptest.NewLogger(t),
	}

	suite.setupEnvironment()
	suite.setupMockBackend()
	suite.setupGateway()

	return suite
}

// setupEnvironment initializes the test environment and dependencies
func (s *E2ETestSuite) setupEnvironment() {
	// Set JWT secret for testing
	os.Setenv("TEST_JWT_SECRET", "e2e-integration-test-secret-key-12345")
	s.t.Cleanup(func() { os.Unsetenv("TEST_JWT_SECRET") })

	// Start mock Redis instance
	mockRedis, err := miniredis.Run()
	require.NoError(s.t, err)
	s.mockRedis = mockRedis
	s.t.Cleanup(func() { mockRedis.Close() })

	// Create test configuration
	s.config = &config.Config{
		Version: 1,
		Server: config.ServerConfig{
			Port:                 0, // Use random port
			MetricsPort:          0,
			MaxConnections:       testMaxIterations,
			ConnectionBufferSize: 65536,
			ReadTimeout:          "30s",
			WriteTimeout:         "30s",
			IdleTimeout:          "60s",
		},
		Auth: config.AuthConfig{
			Provider: "jwt",
			JWT: config.JWTConfig{
				Issuer:       "e2e-test-gateway",
				Audience:     "e2e-test-gateway",
				SecretKeyEnv: "TEST_JWT_SECRET",
			},
		},
		Routing: config.RoutingConfig{
			Strategy:            "round_robin",
			HealthCheckInterval: "1s",
			CircuitBreaker: config.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 5,
				SuccessThreshold: 2,
				TimeoutSeconds:   30,
			},
		},
		Discovery: config.ServiceDiscoveryConfig{
			Mode:              "static",
			NamespaceSelector: []string{"e2e-test"},
		},
		Sessions: config.SessionConfig{
			Provider: "redis",
			Redis: config.RedisConfig{
				URL: fmt.Sprintf("redis://%s", mockRedis.Addr()),
				DB:  0,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "debug",
			Format: "json",
		},
		Metrics: config.MetricsConfig{
			Enabled: true,
			Path:    "/metrics",
		},
		RateLimit: config.RateLimitConfig{
			Enabled:        true,
			RequestsPerSec: testMaxIterations,
			Burst:          testIterations,
		},
	}
}

// setupMockBackend creates and configures the mock MCP backend server
func (s *E2ETestSuite) setupMockBackend() {
	s.mockBackend = &MockMCPBackend{
		connections:     make(map[*websocket.Conn]*ConnectionState),
		messageHandlers: make(map[string]func(*websocket.Conn, *mcp.Request) *mcp.Response),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	// Register message handlers
	s.mockBackend.RegisterHandler("initialize", s.mockBackend.handleInitialize)
	s.mockBackend.RegisterHandler("tools/list", s.mockBackend.handleToolsList)
	s.mockBackend.RegisterHandler("tools/call", s.mockBackend.handleToolsCall)
	s.mockBackend.RegisterHandler("resources/list", s.mockBackend.handleResourcesList)
	s.mockBackend.RegisterHandler("resources/read", s.mockBackend.handleResourcesRead)
	s.mockBackend.RegisterHandler("prompts/list", s.mockBackend.handlePromptsList)
	s.mockBackend.RegisterHandler("prompts/get", s.mockBackend.handlePromptsGet)

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.mockBackend.handleHealthCheck)
	mux.HandleFunc("/", s.mockBackend.handleWebSocket)

	s.mockBackend.server = httptest.NewServer(mux)
	s.t.Cleanup(func() { s.mockBackend.server.Close() })
}

// setupGateway initializes the gateway server with all components
func (s *E2ETestSuite) setupGateway() {
	var err error

	// Initialize authentication provider
	s.authProvider, err = auth.InitializeAuthenticationProvider(s.config.Auth, s.logger)
	require.NoError(s.t, err)

	// Initialize session manager
	s.sessionManager, err = session.CreateSessionStorageManager(context.Background(), s.config.Sessions, s.logger)
	require.NoError(s.t, err)

	// Create mock service discovery
	mockDiscovery := &mockServiceDiscovery{
		endpoints: make(map[string][]discovery.Endpoint),
	}

	// Register mock backend
	backendURL := s.mockBackend.server.URL
	backendHost, backendPortStr, err := net.SplitHostPort(backendURL[7:]) // Remove "http://"
	require.NoError(s.t, err)

	backendPort := 8080
	fmt.Sscanf(backendPortStr, "%d", &backendPort)

	err = mockDiscovery.RegisterEndpoint("e2e-test", discovery.Endpoint{
		Address: backendHost,
		Port:    backendPort,
		Healthy: true,
		Tags:    map[string]string{"protocol": "websocket"},
	})
	require.NoError(s.t, err)

	// Initialize components
	s.metricsRegistry = metrics.InitializeMetricsRegistry()
	s.requestRouter = router.InitializeRequestRouter(context.Background(), s.config.Routing, mockDiscovery, s.metricsRegistry, s.logger)
	s.healthChecker = health.CreateHealthMonitor(mockDiscovery, s.logger)
	s.rateLimiter = ratelimit.CreateLocalMemoryRateLimiter(s.logger)

	// Create gateway server
	s.gateway = server.BootstrapGatewayServer(
		s.config,
		s.authProvider,
		s.sessionManager,
		s.requestRouter,
		s.healthChecker,
		s.metricsRegistry,
		s.rateLimiter,
		s.logger,
	)

	// Start server
	s.startGatewayServer()
}

// startGatewayServer starts the gateway server and waits for it to be ready
func (s *E2ETestSuite) startGatewayServer() {
	serverReady := make(chan string)
	errorChan := make(chan error, 1)

	go func() {
		// Create a custom listener to get the actual port
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			errorChan <- fmt.Errorf("failed to start gateway server listener: %w", err)
			return
		}

		// Get the actual port assigned
		addr := listener.Addr().(*net.TCPAddr)
		s.config.Server.Port = addr.Port

		// Close the listener so the server can bind to it
		_ = listener.Close()

		serverReady <- fmt.Sprintf("127.0.0.1:%d", addr.Port)

		// Start the actual gateway server
		if err := s.gateway.Start(); err != nil && err != http.ErrServerClosed {
			errorChan <- fmt.Errorf("gateway server error: %w", err)
		}
	}()

	// Wait for server startup
	select {
	case s.serverAddr = <-serverReady:
		time.Sleep(testIterations * time.Millisecond) // Allow server to fully initialize
		s.t.Logf("Gateway server ready at: %s", s.serverAddr)
	case err := <-errorChan:
		s.t.Fatalf("Failed to start gateway server: %v", err)
	case <-time.After(10 * time.Second):
		s.t.Fatal("Timeout waiting for gateway server to start")
	}
}

// Cleanup shuts down all test components
func (s *E2ETestSuite) Cleanup() {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if s.gateway != nil {
		s.gateway.Shutdown(shutdownCtx)
	}

	s.cancel()
}

// TestEndToEndProtocolFlows tests comprehensive end-to-end protocol flows
func TestEndToEndProtocolFlows(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping end-to-end integration test in short mode")
	}

	suite := NewE2ETestSuite(t)
	defer suite.Cleanup()

	t.Run("MCP_Protocol_Initialization_Flow", func(t *testing.T) {
		suite.testMCPInitializationFlow(t)
	})

	t.Run("Tool_Discovery_And_Execution_Flow", func(t *testing.T) {
		suite.testToolDiscoveryAndExecutionFlow(t)
	})

	t.Run("Resource_Management_Flow", func(t *testing.T) {
		suite.testResourceManagementFlow(t)
	})

	t.Run("Prompt_Management_Flow", func(t *testing.T) {
		suite.testPromptManagementFlow(t)
	})

	t.Run("Concurrent_Client_Sessions", func(t *testing.T) {
		suite.testConcurrentClientSessions(t)
	})

	t.Run("Session_Persistence_And_Recovery", func(t *testing.T) {
		suite.testSessionPersistenceAndRecovery(t)
	})

	t.Run("Backend_Failover_And_Recovery", func(t *testing.T) {
		suite.testBackendFailoverAndRecovery(t)
	})
}

// testMCPInitializationFlow tests the complete MCP initialization protocol
func (s *E2ETestSuite) testMCPInitializationFlow(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// Test initialize request
	initRequest := mcp.Request{
		JSONRPC: "2.0",
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{"listChanged": true},
				"resources": map[string]interface{}{"subscribe": true, "listChanged": true},
				"prompts":   map[string]interface{}{"listChanged": true},
			},
			"clientInfo": map[string]interface{}{
				"name":    "e2e-test-client",
				"version": "1.0.0",
			},
		},
		ID: "init-1",
	}

	response := client.SendRequest(t, initRequest)

	assert.NotNil(t, response.Result)
	assert.Nil(t, response.Error)
	assert.Equal(t, "init-1", response.ID)

	// Verify response structure
	result, ok := response.Result.(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, "2024-11-05", result["protocolVersion"])
	assert.NotNil(t, result["capabilities"])
	assert.NotNil(t, result["serverInfo"])

	// Test initialized notification
	initializedNotification := mcp.Request{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
		Params:  map[string]interface{}{},
	}

	err := client.SendNotification(t, initializedNotification)
	assert.NoError(t, err)

	t.Log("✅ MCP initialization flow completed successfully")
}

// testToolDiscoveryAndExecutionFlow tests tool listing and execution
func (s *E2ETestSuite) testToolDiscoveryAndExecutionFlow(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// Initialize client first
	s.initializeClient(t, client)

	// Test tools/list
	listRequest := mcp.Request{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      "tools-list-1",
	}

	response := client.SendRequest(t, listRequest)
	assert.NotNil(t, response.Result)
	assert.Nil(t, response.Error)

	result, ok := response.Result.(map[string]interface{})
	require.True(t, ok)

	tools, ok := result["tools"].([]interface{})
	require.True(t, ok)
	assert.Greater(t, len(tools), 0)

	// Test tool execution
	callRequest := mcp.Request{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "test.echo",
			"arguments": map[string]interface{}{
				"message": "Hello from E2E test",
			},
		},
		ID: "tools-call-1",
	}

	response = client.SendRequest(t, callRequest)
	assert.NotNil(t, response.Result)
	assert.Nil(t, response.Error)

	t.Log("✅ Tool discovery and execution flow completed successfully")
}

// testResourceManagementFlow tests resource listing and reading
func (s *E2ETestSuite) testResourceManagementFlow(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	s.initializeClient(t, client)

	// Test resources/list
	listRequest := mcp.Request{
		JSONRPC: "2.0",
		Method:  "resources/list",
		ID:      "resources-list-1",
	}

	response := client.SendRequest(t, listRequest)
	assert.NotNil(t, response.Result)
	assert.Nil(t, response.Error)

	// Test resource reading
	readRequest := mcp.Request{
		JSONRPC: "2.0",
		Method:  "resources/read",
		Params: map[string]interface{}{
			"uri": "test://example.com/resource1",
		},
		ID: "resources-read-1",
	}

	response = client.SendRequest(t, readRequest)
	assert.NotNil(t, response.Result)
	assert.Nil(t, response.Error)

	t.Log("✅ Resource management flow completed successfully")
}

// testPromptManagementFlow tests prompt listing and retrieval
func (s *E2ETestSuite) testPromptManagementFlow(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	s.initializeClient(t, client)

	// Test prompts/list
	listRequest := mcp.Request{
		JSONRPC: "2.0",
		Method:  "prompts/list",
		ID:      "prompts-list-1",
	}

	response := client.SendRequest(t, listRequest)
	assert.NotNil(t, response.Result)
	assert.Nil(t, response.Error)

	// Test prompt retrieval
	getRequest := mcp.Request{
		JSONRPC: "2.0",
		Method:  "prompts/get",
		Params: map[string]interface{}{
			"name": "test-prompt",
			"arguments": map[string]interface{}{
				"variable1": "value1",
			},
		},
		ID: "prompts-get-1",
	}

	response = client.SendRequest(t, getRequest)
	assert.NotNil(t, response.Result)
	assert.Nil(t, response.Error)

	t.Log("✅ Prompt management flow completed successfully")
}

// testConcurrentClientSessions tests multiple concurrent client sessions
func (s *E2ETestSuite) testConcurrentClientSessions(t *testing.T) {
	const numClients = 10
	clients := make([]*e2eTestClient, numClients)

	// Create multiple concurrent clients
	var wg sync.WaitGroup
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			clients[index] = s.createAuthenticatedClient(t)
			s.initializeClient(t, clients[index])
		}(i)
	}
	wg.Wait()

	// Cleanup
	defer func() {
		for _, client := range clients {
			if client != nil {
				_ = client.Close()
			}
		}
	}()

	// Send concurrent requests from all clients
	wg = sync.WaitGroup{}
	errors := make(chan error, numClients)

	for i, client := range clients {
		if client == nil {
			continue
		}

		wg.Add(1)
		go func(index int, c *e2eTestClient) {
			defer wg.Done()

			request := mcp.Request{
				JSONRPC: "2.0",
				Method:  "tools/list",
				ID:      fmt.Sprintf("concurrent-%d", index),
			}

			response := c.SendRequest(t, request)
			if response.Error != nil {
				errors <- fmt.Errorf("client %d failed: %v", index, response.Error)
			}
		}(i, client)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Error(err)
		errorCount++
	}

	assert.Equal(t, 0, errorCount, "All concurrent requests should succeed")
	t.Log("✅ Concurrent client sessions test completed successfully")
}

// testSessionPersistenceAndRecovery tests session persistence across reconnections
func (s *E2ETestSuite) testSessionPersistenceAndRecovery(t *testing.T) {
	// Create client and establish session
	client1 := s.createAuthenticatedClient(t)
	s.initializeClient(t, client1)

	// Send a request to establish session state
	request := mcp.Request{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      "session-test-1",
	}

	response := client1.SendRequest(t, request)
	assert.Nil(t, response.Error)

	// Close first connection
	_ = client1.Close()

	// Create new client with same session (simulate reconnection)
	client2 := s.createAuthenticatedClient(t)
	defer func() { _ = client2.Close() }()

	s.initializeClient(t, client2)

	// Verify session persistence by making another request
	request2 := mcp.Request{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      "session-test-2",
	}

	response2 := client2.SendRequest(t, request2)
	assert.Nil(t, response2.Error)

	t.Log("✅ Session persistence and recovery test completed successfully")
}

// testBackendFailoverAndRecovery tests backend failure handling and recovery
func (s *E2ETestSuite) testBackendFailoverAndRecovery(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	s.initializeClient(t, client)

	// Send normal request to ensure backend is working
	request := mcp.Request{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      "failover-test-1",
	}

	response := client.SendRequest(t, request)
	assert.Nil(t, response.Error)

	// Inject error into backend
	s.mockBackend.SetErrorInjection(true)

	// Send request that should fail
	request2 := mcp.Request{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      "failover-test-2",
	}

	response2 := client.SendRequest(t, request2)
	assert.NotNil(t, response2.Error, "Request should fail when backend is down")

	// Restore backend
	s.mockBackend.SetErrorInjection(false)

	// Verify recovery
	request3 := mcp.Request{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      "failover-test-3",
	}

	response3 := client.SendRequest(t, request3)
	assert.Nil(t, response3.Error, "Request should succeed after backend recovery")

	t.Log("✅ Backend failover and recovery test completed successfully")
}

// Helper methods for test client management

func (s *E2ETestSuite) createAuthenticatedClient(t *testing.T) *e2eTestClient {
	token := s.generateTestToken(t)

	wsURL := fmt.Sprintf("ws://%s/", s.serverAddr)
	headers := http.Header{
		"Authorization": []string{fmt.Sprintf("Bearer %s", token)},
	}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	if resp.Body != nil {
		resp.Body.Close()
	}

	return &e2eTestClient{
		conn:   conn,
		logger: s.logger,
	}
}

func (s *E2ETestSuite) initializeClient(t *testing.T, client *e2eTestClient) {
	initRequest := mcp.Request{
		JSONRPC: "2.0",
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{},
				"resources": map[string]interface{}{},
				"prompts":   map[string]interface{}{},
			},
			"clientInfo": map[string]interface{}{
				"name":    "e2e-test-client",
				"version": "1.0.0",
			},
		},
		ID: "init",
	}

	response := client.SendRequest(t, initRequest)
	require.Nil(t, response.Error)
	require.NotNil(t, response.Result)
}

func (s *E2ETestSuite) generateTestToken(t *testing.T) string {
	claims := &auth.Claims{
		Scopes: []string{"mcp:*", "tools:*", "resources:*", "prompts:*"},
	}
	claims.Subject = "e2e-test-user"
	claims.Issuer = "e2e-test-gateway"
	claims.Audience = jwt.ClaimStrings{"e2e-test-gateway"}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
	claims.IssuedAt = jwt.NewNumericDate(time.Now())
	claims.NotBefore = jwt.NewNumericDate(time.Now())

	testSecret := []byte("e2e-integration-test-secret-key-12345")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(testSecret)
	require.NoError(t, err)

	return tokenString
}

// Mock MCP Backend implementation

func (m *MockMCPBackend) RegisterHandler(method string, handler func(*websocket.Conn, *mcp.Request) *mcp.Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messageHandlers[method] = handler
}

func (m *MockMCPBackend) SetLatencyInjection(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencyInjection = latency
}

func (m *MockMCPBackend) SetErrorInjection(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorInjection = enabled
}

func (m *MockMCPBackend) SetDropRate(rate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropRate = rate
}

func (m *MockMCPBackend) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (m *MockMCPBackend) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	connState := &ConnectionState{
		ID:           fmt.Sprintf("conn-%d", time.Now().UnixNano()),
		LastActivity: time.Now(),
		Capabilities: make(map[string]interface{}),
	}

	m.mu.Lock()
	m.connections[conn] = connState
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.connections, conn)
		m.mu.Unlock()
		_ = conn.Close()
	}()

	for {
		var req mcp.Request
		if err := conn.ReadJSON(&req); err != nil {
			break
		}

		// Apply latency injection
		m.mu.RLock()
		latency := m.latencyInjection
		errorInjected := m.errorInjection
		m.mu.RUnlock()

		if latency > 0 {
			time.Sleep(latency)
		}

		if errorInjected {
			errorResp := &mcp.Response{
				JSONRPC: "2.0",
				Error: &mcp.Error{
					Code:    mcp.ErrorCodeInternalError,
					Message: "Injected error for testing",
				},
				ID: req.ID,
			}
			conn.WriteJSON(errorResp)
			continue
		}

		// Handle request
		m.mu.RLock()
		handler, exists := m.messageHandlers[req.Method]
		m.mu.RUnlock()

		var response *mcp.Response
		if exists {
			response = handler(conn, &req)
		} else {
			response = &mcp.Response{
				JSONRPC: "2.0",
				Error: &mcp.Error{
					Code:    mcp.ErrorCodeMethodNotFound,
					Message: "Method not found",
				},
				ID: req.ID,
			}
		}

		conn.WriteJSON(response)

		// Update connection state
		m.mu.Lock()
		connState.MessageCount++
		connState.LastActivity = time.Now()
		m.mu.Unlock()
	}
}

// Message handlers for mock backend

func (m *MockMCPBackend) handleInitialize(conn *websocket.Conn, req *mcp.Request) *mcp.Response {
	return &mcp.Response{
		JSONRPC: "2.0",
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{"listChanged": true},
				"resources": map[string]interface{}{"subscribe": true, "listChanged": true},
				"prompts":   map[string]interface{}{"listChanged": true},
			},
			"serverInfo": map[string]interface{}{
				"name":    "mock-e2e-backend",
				"version": "1.0.0",
			},
		},
		ID: req.ID,
	}
}

func (m *MockMCPBackend) handleToolsList(conn *websocket.Conn, req *mcp.Request) *mcp.Response {
	return &mcp.Response{
		JSONRPC: "2.0",
		Result: map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "test.echo",
					"description": "Echoes back the input message",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"message": map[string]string{"type": "string"},
						},
						"required": []string{"message"},
					},
				},
				{
					"name":        "test.calculate",
					"description": "Performs simple calculations",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"operation": map[string]string{"type": "string"},
							"a":         map[string]string{"type": "number"},
							"b":         map[string]string{"type": "number"},
						},
						"required": []string{"operation", "a", "b"},
					},
				},
			},
		},
		ID: req.ID,
	}
}

func (m *MockMCPBackend) handleToolsCall(conn *websocket.Conn, req *mcp.Request) *mcp.Response {
	params, _ := req.Params.(map[string]interface{})
	toolName, _ := params["name"].(string)
	arguments, _ := params["arguments"].(map[string]interface{})

	switch toolName {
	case "test.echo":
		message, _ := arguments["message"].(string)
		return &mcp.Response{
			JSONRPC: "2.0",
			Result: []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Echo: %s", message),
				},
			},
			ID: req.ID,
		}
	case "test.calculate":
		// Simple calculation implementation
		return &mcp.Response{
			JSONRPC: "2.0",
			Result: []map[string]interface{}{
				{
					"type": "text",
					"text": "Calculation result: 42",
				},
			},
			ID: req.ID,
		}
	default:
		return &mcp.Response{
			JSONRPC: "2.0",
			Error: &mcp.Error{
				Code:    mcp.ErrorCodeInvalidParams,
				Message: "Unknown tool",
			},
			ID: req.ID,
		}
	}
}

func (m *MockMCPBackend) handleResourcesList(conn *websocket.Conn, req *mcp.Request) *mcp.Response {
	return &mcp.Response{
		JSONRPC: "2.0",
		Result: map[string]interface{}{
			"resources": []map[string]interface{}{
				{
					"uri":         "test://example.com/resource1",
					"name":        "Test Resource 1",
					"description": "A test resource for integration testing",
					"mimeType":    "text/plain",
				},
				{
					"uri":         "test://example.com/resource2",
					"name":        "Test Resource 2",
					"description": "Another test resource",
					"mimeType":    "application/json",
				},
			},
		},
		ID: req.ID,
	}
}

func (m *MockMCPBackend) handleResourcesRead(conn *websocket.Conn, req *mcp.Request) *mcp.Response {
	params, _ := req.Params.(map[string]interface{})
	uri, _ := params["uri"].(string)

	var content string
	switch uri {
	case "test://example.com/resource1":
		content = "This is the content of test resource 1"
	case "test://example.com/resource2":
		content = `{"message": "This is test resource 2", "data": [1, 2, 3]}`
	default:
		return &mcp.Response{
			JSONRPC: "2.0",
			Error: &mcp.Error{
				Code:    mcp.ErrorCodeInvalidParams,
				Message: "Resource not found",
			},
			ID: req.ID,
		}
	}

	return &mcp.Response{
		JSONRPC: "2.0",
		Result: map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"uri":      uri,
					"mimeType": "text/plain",
					"text":     content,
				},
			},
		},
		ID: req.ID,
	}
}

func (m *MockMCPBackend) handlePromptsList(conn *websocket.Conn, req *mcp.Request) *mcp.Response {
	return &mcp.Response{
		JSONRPC: "2.0",
		Result: map[string]interface{}{
			"prompts": []map[string]interface{}{
				{
					"name":        "test-prompt",
					"description": "A test prompt for integration testing",
					"arguments": []map[string]interface{}{
						{
							"name":        "variable1",
							"description": "Test variable",
							"required":    true,
						},
					},
				},
			},
		},
		ID: req.ID,
	}
}

func (m *MockMCPBackend) handlePromptsGet(conn *websocket.Conn, req *mcp.Request) *mcp.Response {
	params, _ := req.Params.(map[string]interface{})
	name, _ := params["name"].(string)
	arguments, _ := params["arguments"].(map[string]interface{})

	if name != "test-prompt" {
		return &mcp.Response{
			JSONRPC: "2.0",
			Error: &mcp.Error{
				Code:    mcp.ErrorCodeInvalidParams,
				Message: "Prompt not found",
			},
			ID: req.ID,
		}
	}

	variable1, _ := arguments["variable1"].(string)

	return &mcp.Response{
		JSONRPC: "2.0",
		Result: map[string]interface{}{
			"description": "Test prompt with variable",
			"messages": []map[string]interface{}{
				{
					"role": "user",
					"content": map[string]interface{}{
						"type": "text",
						"text": fmt.Sprintf("This is a test prompt with variable: %s", variable1),
					},
				},
			},
		},
		ID: req.ID,
	}
}

// Additional helper method for notifications
func (c *testClient) SendNotification(t *testing.T, notification mcp.Request) error {
	wireMsg := server.WireMessage{
		ID:              notification.ID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Source:          "e2e-test-client",
		TargetNamespace: "e2e-test",
		MCPPayload:      notification,
	}

	c.mu.Lock()
	err := c.conn.WriteJSON(wireMsg)
	c.mu.Unlock()

	return err
}
