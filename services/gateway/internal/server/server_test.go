
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
	"github.com/poiley/mcp-bridge/services/gateway/internal/health"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
	"github.com/poiley/mcp-bridge/services/gateway/internal/ratelimit"
	"github.com/poiley/mcp-bridge/services/gateway/internal/router"
	"github.com/poiley/mcp-bridge/services/gateway/internal/session"
	"github.com/poiley/mcp-bridge/services/gateway/test/testutil"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

func TestBootstrapGatewayServer(t *testing.T) {
	cfg, mockAuth, mockSessions, testRouter, mockHealth, registry, mockRateLimiter, logger :=
		setupBootstrapTestDependencies(t)
	
	server := BootstrapGatewayServer(
		cfg, mockAuth, mockSessions, testRouter, mockHealth, registry, mockRateLimiter, logger)
	
	validateBootstrappedServer(t, server, cfg, mockHealth, registry, logger)
}

func setupBootstrapTestDependencies(t *testing.T) (
	*config.Config,
	*mockAuthProvider,
	*mockSessionManager,
	*router.Router,
	*health.Checker,
	*metrics.Registry,
	ratelimit.RateLimiter,
	*zap.Logger,
) {
	t.Helper()
	
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:                 8443,
			MaxConnections:       testMaxIterations,
			ConnectionBufferSize: 65536,
		},
	}

	mockAuth := &mockAuthProvider{}
	mockSessions := &mockSessionManager{}

	// Create a minimal router for testing
	routerCfg := config.RoutingConfig{
		Strategy: "round_robin",
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			TimeoutSeconds:   30,
		},
	}
	mockDiscovery := &mockServiceDiscovery{}
	testRouter := router.InitializeRequestRouter(
		context.Background(), routerCfg, mockDiscovery,
		testutil.CreateTestMetricsRegistry(),
		testutil.NewTestLogger(t))

	mockHealth := health.CreateHealthMonitor(nil, zap.NewNop())
	registry := testutil.CreateTestMetricsRegistry()
	mockRateLimiter := ratelimit.CreateLocalMemoryRateLimiter(zap.NewNop())
	logger := testutil.NewTestLogger(t)

	return cfg, mockAuth, mockSessions, testRouter, mockHealth, registry, mockRateLimiter, logger
}

func validateBootstrappedServer(
	t *testing.T,
	server *GatewayServer,
	cfg *config.Config,
	mockHealth *health.Checker,
	registry *metrics.Registry,
	logger *zap.Logger,
) {
	t.Helper()
	
	if server == nil {
		t.Fatal("Expected server to be created")
	}

	if server.config != cfg {
		t.Error("Config not set correctly")
	}

	if server.auth == nil {
		t.Error("Auth provider not set")
	}

	if server.sessions == nil {
		t.Error("Session manager not set")
	}

	if server.router == nil {
		t.Error("Router not set")
	}

	if server.health != mockHealth {
		t.Error("Health checker not set correctly")
	}

	if server.metrics != registry {
		t.Error("Metrics registry not set correctly")
	}

	if server.logger != logger {
		t.Error("Logger not set correctly")
	}

	if server.upgrader.ReadBufferSize != cfg.Server.ConnectionBufferSize {
		t.Error("Upgrader buffer size not set correctly")
	}
}

func TestGatewayServer_handleWebSocket_ConnectionLimit(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:                 8443,
			MaxConnections:       2,
			ConnectionBufferSize: 65536,
		},
	}

	server := &GatewayServer{
		config:      cfg,
		logger:      zap.NewNop(),
		metrics:     testutil.CreateTestMetricsRegistry(),
		rateLimiter: ratelimit.CreateLocalMemoryRateLimiter(zap.NewNop()),
		connCount:   2, // Already at limit
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	server.handleWebSocket(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	responseBody := w.Body.String()
	if !strings.Contains(responseBody, "maximum connections reached") &&
		!strings.Contains(responseBody, "Connection limit reached") {
		t.Errorf("Expected connection limit message, got: %s", responseBody)
	}
}

func TestGatewayServer_handleWebSocket_AuthFailure(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:                 8443,
			MaxConnections:       testMaxIterations,
			ConnectionBufferSize: 65536,
		},
	}

	mockAuth := &mockAuthProvider{
		authenticateErr: errors.New("invalid token"),
	}

	server := &GatewayServer{
		config:      cfg,
		logger:      zap.NewNop(),
		auth:        mockAuth,
		metrics:     testutil.CreateTestMetricsRegistry(),
		rateLimiter: ratelimit.CreateLocalMemoryRateLimiter(zap.NewNop()),
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")

	w := httptest.NewRecorder()

	server.handleWebSocket(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestGatewayServer_handleWebSocket_Success(t *testing.T) {
	server := setupWebSocketTestServer(t)
	defer server.cancel()

	testServer := createWebSocketHTTPServer(server)
	defer testServer.Close()

	conn, resp := connectWebSocketClient(t, testServer)
	defer closeWebSocketConnection(conn, resp)

	validateWebSocketConnection(t, server, resp)
}

func setupWebSocketTestServer(t *testing.T) *GatewayServer {
	t.Helper()
	
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:                 8443,
			MaxConnections:       testMaxIterations,
			ConnectionBufferSize: 65536,
		},
	}

	mockAuth := &mockAuthProvider{
		claims: &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "test-user",
			},
			Scopes: []string{"tools:read"},
		},
	}

	mockSessions := &mockSessionManager{
		session: &session.Session{
			ID:   "test-session",
			User: "test-user",
		},
	}

	// Create a minimal router for testing
	routerCfg := config.RoutingConfig{
		Strategy: "round_robin",
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			TimeoutSeconds:   30,
		},
	}
	mockDiscovery := &mockServiceDiscovery{}
	testRouter := router.InitializeRequestRouter(
		context.Background(), routerCfg, mockDiscovery,
		testutil.CreateTestMetricsRegistry(),
		testutil.NewTestLogger(t))

	mockHealth := health.CreateHealthMonitor(nil, zap.NewNop())
	registry := testutil.CreateTestMetricsRegistry()
	mockRateLimiter := ratelimit.CreateLocalMemoryRateLimiter(zap.NewNop())
	logger := testutil.NewTestLogger(t)

	server := BootstrapGatewayServer(
		cfg, mockAuth, mockSessions, testRouter, mockHealth, registry, mockRateLimiter, logger,
	)
	
	// Override the upgrader to allow all origins for testing
	server.upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	return server
}

func createWebSocketHTTPServer(server *GatewayServer) *httptest.Server {
	// Create test WebSocket server
	return httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
}

func connectWebSocketClient(t *testing.T, testServer *httptest.Server) (*websocket.Conn, *http.Response) {
	t.Helper()
	
	// Connect as client
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")
	headers := http.Header{"Authorization": []string{"Bearer test-token"}}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	return conn, resp
}

func closeWebSocketConnection(conn *websocket.Conn, resp *http.Response) {
	if conn != nil {
		_ = conn.Close()
	}

	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func validateWebSocketConnection(t *testing.T, server *GatewayServer, resp *http.Response) {
	t.Helper()
	
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("Expected status %d, got %d", http.StatusSwitchingProtocols, resp.StatusCode)
	}

	// Verify connection registered
	time.Sleep(100 * time.Millisecond) // Give more time for connection to be fully registered

	connCount := atomic.LoadInt64(&server.connCount)
	if connCount != 1 {
		t.Errorf("Expected connection count to be 1, got %d", connCount)
	}

	_, exists := server.connections.Load("test-session")
	if !exists {
		// Debug: check what connections exist
		var connectionIDs []string

		server.connections.Range(func(key, value interface{}) bool {
			if keyStr, ok := key.(string); ok {
				connectionIDs = append(connectionIDs, keyStr)
			}
			return true
		})
		t.Errorf("Expected connection to be registered. Found connections: %v", connectionIDs)
	}
}

func TestGatewayServer_processClientMessage(t *testing.T) {
	tests := getProcessClientMessageTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runProcessClientMessageTest(t, tt)
		})
	}
}

func getProcessClientMessageTests() []processClientMessageTest {
	return []processClientMessageTest{
		{
			name: "Valid request but no backend",
			wireMessage: WireMessage{
				ID:              "test-123",
				TargetNamespace: "test",
				MCPPayload: map[string]interface{}{
					"jsonrpc": "2.0",
					"method":  "test.method",
					"id":      "test-123",
				},
			},
			routerResp: &mcp.Response{
				JSONRPC: "2.0",
				Result:  "ok",
				ID:      "test-123",
			},
			wantError:     true,
			errorContains: "no endpoints available",
			hasBackend:    false,
		},
		{
			name: "Missing MCP payload",
			wireMessage: WireMessage{
				ID: "test-456",
			},
			wantError:     true,
			errorContains: "missing MCP payload",
			hasBackend:    true,
		},
		{
			name: "Invalid MCP request",
			wireMessage: WireMessage{
				ID:         "test-789",
				MCPPayload: "not an object",
			},
			wantError:     true,
			errorContains: "invalid request", // Updated to match typed error
			hasBackend:    true,
		},
		{
			name: "Router error",
			wireMessage: WireMessage{
				ID: "test-error",
				MCPPayload: map[string]interface{}{
					"jsonrpc": "2.0",
					"method":  "test.method",
					"id":      "test-error",
				},
			},
			routerErr:     errors.New("no endpoints available"),
			wantError:     true,
			errorContains: "no endpoints available",
			hasBackend:    false,
		},
	}
}

type processClientMessageTest struct {
	name          string
	wireMessage   WireMessage
	routerResp    *mcp.Response
	routerErr     error
	wantError     bool
	errorContains string
	hasBackend    bool
}

func runProcessClientMessageTest(t *testing.T, tt processClientMessageTest) {
	t.Helper()

	server, client := setupProcessClientMessageTest(t, tt.hasBackend)
	defer client.cancel()

	// Marshal wire message
	data, _ := json.Marshal(tt.wireMessage)
	err := server.processClientMessage(client.ctx, client, data)

	validateProcessClientMessageResult(t, err, client, tt)
}

func setupProcessClientMessageTest(t *testing.T, hasBackend bool) (*GatewayServer, *ClientConnection) {
	t.Helper()

	routerCfg := config.RoutingConfig{
		Strategy: "round_robin",
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			TimeoutSeconds:   30,
		},
	}

	var mockDiscovery *mockServiceDiscovery
	if hasBackend {
		mockDiscovery = &mockServiceDiscovery{
			endpoints: map[string][]discovery.Endpoint{
				"test": {{Address: "localhost", Port: 8080, Scheme: "http", Healthy: true}},
			},
		}
	} else {
		mockDiscovery = &mockServiceDiscovery{
			endpoints: map[string][]discovery.Endpoint{},
		}
	}

	testRouter := router.InitializeRequestRouter(
		context.Background(), routerCfg, mockDiscovery, testutil.CreateTestMetricsRegistry(), zap.NewNop(),
	)

	server := &GatewayServer{
		router:      testRouter,
		metrics:     testutil.CreateTestMetricsRegistry(),
		logger:      zap.NewNop(),
		rateLimiter: ratelimit.CreateLocalMemoryRateLimiter(zap.NewNop()),
	}

	client := &ClientConnection{
		ID: "test-client",
		Session: &session.Session{
			ID:   "test-session",
			User: "test-user",
			RateLimit: auth.RateLimitConfig{
				RequestsPerMinute: testMaxIterations,
				Burst:             testTimeout,
			},
		},
		Logger: zap.NewNop(),
		send:   make(chan []byte, 10),
	}

	client.ctx, client.cancel = context.WithCancel(context.Background())

	return server, client
}

func validateProcessClientMessageResult(
	t *testing.T, err error, client *ClientConnection, tt processClientMessageTest,
) {
	t.Helper()

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")
		} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
		}

		return
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Check if response was sent
	select {
	case msg := <-client.send:
		var wireResp WireMessage

		require.NoError(t, json.Unmarshal(msg, &wireResp))

		if wireResp.Source != "gateway" {
			t.Error("Expected source to be 'gateway'")
		}
	case <-time.After(testIterations * time.Millisecond):
		t.Error("Expected response to be sent")
	}
}

func TestGatewayServer_sendResponse(t *testing.T) {
	server := &GatewayServer{}

	client := &ClientConnection{
		send: make(chan []byte, 1),
	}
	client.ctx, client.cancel = context.WithCancel(context.Background())

	resp := &mcp.Response{
		JSONRPC: "2.0",
		Result:  "test result",
		ID:      "test-123",
	}

	// Test successful send
	err := server.sendResponse(client, resp)
	if err != nil {
		t.Errorf("Failed to send response: %v", err)
	}

	// Verify message sent
	select {
	case msg := <-client.send:
		var wireMsg WireMessage

		require.NoError(t, json.Unmarshal(msg, &wireMsg))

		if wireMsg.ID != "test-123" {
			t.Error("Response ID mismatch")
		}

		if wireMsg.Source != "gateway" {
			t.Error("Expected source to be 'gateway'")
		}
	default:
		t.Error("No message in send channel")
	}

	// Verify response count incremented
	if atomic.LoadUint64(&client.responseCount) != 1 {
		t.Error("Expected response count to be 1")
	}

	// Test with canceled context
	client.cancel()
	// Fill the channel to ensure it blocks
	client.send <- []byte("dummy")

	err = server.sendResponse(client, resp)
	if err == nil {
		t.Error("Expected error with canceled context")
	}
}

func TestGatewayServer_sendErrorResponse(t *testing.T) {
	server := &GatewayServer{
		logger: zap.NewNop(),
	}

	client := &ClientConnection{
		send:   make(chan []byte, 1),
		Logger: zap.NewNop(),
	}

	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	testErr := errors.New("test error")
	server.sendErrorResponse(client, "test-123", testErr)

	// Verify error response sent
	select {
	case msg := <-client.send:
		var wireMsg WireMessage

		require.NoError(t, json.Unmarshal(msg, &wireMsg))

		// Extract MCP response
		mcpData, _ := json.Marshal(wireMsg.MCPPayload)

		var resp mcp.Response

		require.NoError(t, json.Unmarshal(mcpData, &resp))

		if resp.Error == nil {
			t.Error("Expected error in response")
		}

		if resp.Error.Code != mcp.ErrorCodeInternalError {
			t.Errorf("Expected error code %d, got %d", mcp.ErrorCodeInternalError, resp.Error.Code)
		}

		if resp.Error.Message != "test error" {
			t.Errorf("Expected error message 'test error', got '%s'", resp.Error.Message)
		}
	default:
		t.Error("No message in send channel")
	}

	// Verify error count incremented
	if atomic.LoadUint64(&client.errorCount) != 1 {
		t.Error("Expected error count to be 1")
	}
}

func TestGatewayServer_removeConnection(t *testing.T) {
	mockSessions := &mockSessionManager{}

	server := &GatewayServer{
		sessions:    mockSessions,
		metrics:     testutil.CreateTestMetricsRegistry(),
		logger:      zap.NewNop(),
		connections: sync.Map{},
		connCount:   1,
	}

	// Add connection
	client := &ClientConnection{ID: "test-client"}
	server.connections.Store("test-client", client)

	// Remove connection
	server.removeConnection("test-client")

	// Verify connection removed
	_, exists := server.connections.Load("test-client")
	if exists {
		t.Error("Expected connection to be removed")
	}

	// Verify count decremented
	if atomic.LoadInt64(&server.connCount) != 0 {
		t.Error("Expected connection count to be 0")
	}

	// Verify session removed
	if !mockSessions.removeSessionCalled {
		t.Error("Expected RemoveSession to be called")
	}
}

func TestClientConnection_Close(t *testing.T) {
	// Create mock connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, _ := upgrader.Upgrade(w, r, nil)

		defer func() { _ = conn.Close() }() 

		time.Sleep(testIterations * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, resp, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	client := &ClientConnection{
		Conn: conn,
		send: make(chan []byte, 1),
	}
	client.ctx, client.cancel = context.WithCancel(context.Background())

	// Send a message
	client.send <- []byte("test")

	// Close connection
	client.Close()

	// Verify context canceled
	select {
	case <-client.ctx.Done():
		// Success
	default:
		t.Error("Expected context to be canceled")
	}

	// Verify we can't send more messages (would block or panic)
	select {
	case client.send <- []byte("should not send"):
		// This might succeed if the channel has buffer space
	case <-time.After(testIterations * time.Millisecond):
	}
}

func TestGatewayServer_Shutdown(t *testing.T) {
	server := &GatewayServer{
		logger:      zap.NewNop(),
		connections: sync.Map{},
		server: &http.Server{
			Addr:              ":0",
			ReadHeaderTimeout: 30 * time.Second, // G112: Prevent Slowloris attacks
		},
	}
	server.ctx, server.cancel = context.WithCancel(context.Background())

	// Add some connections
	for i := 0; i < 3; i++ {
		client := &ClientConnection{
			ID:   fmt.Sprintf("client-%d", i),
			send: make(chan []byte),
		}
		client.ctx, client.cancel = context.WithCancel(context.Background())
		server.connections.Store(client.ID, client)
	}

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := server.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown error: %v", err)
	}

	// Verify all connections closed
	count := 0

	server.connections.Range(func(_, _ interface{}) bool {
		count++

		return true
	})

	if count > 0 {
		t.Error("Expected all connections to be closed")
	}
}

func TestWireMessage_JSONMarshaling(t *testing.T) {
	msg := WireMessage{
		ID:              "test-123",
		Timestamp:       "2024-01-01T00:00:00Z",
		Source:          "test-client",
		TargetNamespace: "test-ns",
		MCPPayload: map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "test",
			"id":      "test-123",
		},
	}

	// Marshal
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var decoded WireMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify fields
	if decoded.ID != msg.ID {
		t.Error("ID mismatch")
	}

	if decoded.Timestamp != msg.Timestamp {
		t.Error("Timestamp mismatch")
	}

	if decoded.Source != msg.Source {
		t.Error("Source mismatch")
	}

	if decoded.TargetNamespace != msg.TargetNamespace {
		t.Error("TargetNamespace mismatch")
	}
}

// Mock implementations

type mockAuthProvider struct {
	claims          *auth.Claims
	authenticateErr error
}

func (m *mockAuthProvider) Authenticate(r *http.Request) (*auth.Claims, error) {
	if m.authenticateErr != nil {
		return nil, m.authenticateErr
	}

	return m.claims, nil
}

func (m *mockAuthProvider) ValidateScopes(claims *auth.Claims, requiredScopes []string) error {
	return nil
}

func (m *mockAuthProvider) ValidateToken(tokenString string) (*auth.Claims, error) {
	if m.authenticateErr != nil {
		return nil, m.authenticateErr
	}

	return m.claims, nil
}

type mockSessionManager struct {
	session             *session.Session
	createErr           error
	removeSessionCalled bool
}

func (m *mockSessionManager) CreateSession(claims *auth.Claims) (*session.Session, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}

	return m.session, nil
}

func (m *mockSessionManager) GetSession(id string) (*session.Session, error) {
	return m.session, nil
}

func (m *mockSessionManager) UpdateSession(sess *session.Session) error {
	return nil
}

func (m *mockSessionManager) RemoveSession(id string) error {
	m.removeSessionCalled = true

	return nil
}

func (m *mockSessionManager) SetData(sessionID, key string, value interface{}) error {
	return nil
}

func (m *mockSessionManager) GetData(sessionID, key string) (interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *mockSessionManager) Close() error {
	return nil
}

func (m *mockSessionManager) RedisClient() *redis.Client {
	return nil
}

type mockRouter struct {
	response *mcp.Response
	err      error
}

func (m *mockRouter) RouteRequest(
	ctx context.Context, req *mcp.Request, targetNamespace string,
) (*mcp.Response, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.response, nil
}

func (m *mockRouter) GetRequestCount() uint64 {
	return 0
}

// mockServiceDiscovery for testing.
type mockServiceDiscovery struct {
	endpoints map[string][]discovery.Endpoint
	mu        sync.RWMutex
}

func (m *mockServiceDiscovery) Start(ctx context.Context) error {
	return nil
}

func (m *mockServiceDiscovery) Stop() {
	// No-op
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

func (m *mockServiceDiscovery) ListNamespaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	namespaces := make([]string, 0, len(m.endpoints))
	for ns := range m.endpoints {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

func BenchmarkGatewayServer_processClientMessage(b *testing.B) {
	// Create a minimal router for testing
	routerCfg := config.RoutingConfig{
		Strategy: "round_robin",
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			TimeoutSeconds:   30,
		},
	}
	mockDiscovery := &mockServiceDiscovery{}
	testRouter := router.InitializeRequestRouter(
		context.Background(), routerCfg, mockDiscovery, testutil.CreateTestMetricsRegistry(), zap.NewNop(),
	)

	server := &GatewayServer{
		router:  testRouter,
		metrics: testutil.CreateTestMetricsRegistry(),
		logger:  zap.NewNop(),
	}

	client := &ClientConnection{
		ID: "bench-client",
		Session: &session.Session{
			ID:   "bench-session",
			User: "bench-user",
		},
		Logger: zap.NewNop(),
		send:   make(chan []byte, testIterations),
	}

	client.ctx, client.cancel = context.WithCancel(context.Background())
	defer client.cancel()

	// Drain send channel
	go func() {
		for v := range client.send {
			// Drain channel
			_ = v // Explicitly ignore value
		}
	}()

	wireMsg := WireMessage{
		ID:              "bench-1",
		TargetNamespace: "bench",
		MCPPayload: map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "bench.method",
			"id":      "bench-1",
		},
	}
	data, _ := json.Marshal(wireMsg)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := server.processClientMessage(client.ctx, client, data); err != nil {
			b.Fatal(err)
		}
	}
}
