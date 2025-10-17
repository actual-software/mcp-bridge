package websocket

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// Test helpers

// testServer wraps a frontend with cleanup.
type testServer struct {
	frontend *Frontend
	url      string
	t        *testing.T
}

// setupTestServer creates and starts a WebSocket frontend for testing.
func setupTestServer(
	t *testing.T,
	config Config,
	router *mockRouter,
	auth *mockAuth,
	sessions *mockSessionManager,
) *testServer {
	t.Helper()

	logger := zap.NewNop()
	frontend := CreateWebSocketFrontend("test-ws", config, router, auth, sessions, logger)

	ctx := context.Background()
	if err := frontend.Start(ctx); err != nil {
		t.Fatalf("Failed to start frontend: %v", err)
	}

	// Create listener to get actual address when using port 0
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	// Create and inject HTTP server for testing
	server := &http.Server{
		Handler: frontend.GetHandler(),
	}
	frontend.SetServer(server)

	// Start the server with the listener
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			t.Logf("Server error: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Build WebSocket URL from listener address
	url := fmt.Sprintf("ws://%s", ln.Addr().String())

	ts := &testServer{
		frontend: frontend,
		url:      url,
		t:        t,
	}

	return ts
}

// cleanup stops the test server.
func (ts *testServer) cleanup() {
	ts.t.Helper()
	ctx := context.Background()
	if err := ts.frontend.Stop(ctx); err != nil {
		ts.t.Errorf("Failed to stop frontend: %v", err)
	}
}

// Mock implementations for testing

type mockRouter struct {
	requestHandler func(ctx context.Context, req *mcp.Request, namespace string) (*mcp.Response, error)
}

func (m *mockRouter) RouteRequest(ctx context.Context, req *mcp.Request, namespace string) (*mcp.Response, error) {
	if m.requestHandler != nil {
		return m.requestHandler(ctx, req, namespace)
	}

	return &mcp.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"status": "ok"},
	}, nil
}

type mockAuth struct {
	authenticateHandler func(r *http.Request) (*auth.Claims, error)
}

func (m *mockAuth) Authenticate(r *http.Request) (*auth.Claims, error) {
	if m.authenticateHandler != nil {
		return m.authenticateHandler(r)
	}

	return &auth.Claims{}, nil
}

func (m *mockAuth) ValidateToken(token string) (*auth.Claims, error) {
	return &auth.Claims{}, nil
}

type mockSessionManager struct {
	sessions map[string]*session.Session
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{
		sessions: make(map[string]*session.Session),
	}
}

func (m *mockSessionManager) CreateSession(claims *auth.Claims) (*session.Session, error) {
	sess := &session.Session{
		ID:        "test-session-id",
		User:      "test-user",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	m.sessions[sess.ID] = sess

	return sess, nil
}

func (m *mockSessionManager) GetSession(id string) (*session.Session, error) {
	if sess, ok := m.sessions[id]; ok {
		return sess, nil
	}

	return nil, fmt.Errorf("session not found")
}

func (m *mockSessionManager) UpdateSession(sess *session.Session) error {
	m.sessions[sess.ID] = sess

	return nil
}

func (m *mockSessionManager) RemoveSession(id string) error {
	delete(m.sessions, id)

	return nil
}

func (m *mockSessionManager) Close() error {
	return nil
}

func TestCreateWebSocketFrontend(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "localhost",
		Port:           8080,
		MaxConnections: 100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowedOrigins: []string{"*"},
	}

	frontend := CreateWebSocketFrontend("test-ws", config, router, auth, sessions, logger)

	if frontend == nil {
		t.Fatal("CreateWebSocketFrontend returned nil")
	}

	if frontend.GetName() != "test-ws" {
		t.Errorf("Expected name 'test-ws', got '%s'", frontend.GetName())
	}

	if frontend.GetProtocol() != "websocket" {
		t.Errorf("Expected protocol 'websocket', got '%s'", frontend.GetProtocol())
	}
}

func TestWebSocketFrontendStartStop(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "localhost",
		Port:           0, // Use random port
		MaxConnections: 100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowedOrigins: []string{"*"},
	}

	frontend := CreateWebSocketFrontend("test-ws", config, router, auth, sessions, logger)

	ctx := context.Background()

	// Start the frontend
	if err := frontend.Start(ctx); err != nil {
		t.Fatalf("Failed to start frontend: %v", err)
	}

	// Verify it's running
	metrics := frontend.GetMetrics()
	if !metrics.IsRunning {
		t.Error("Frontend should be running")
	}

	// Stop the frontend
	if err := frontend.Stop(ctx); err != nil {
		t.Fatalf("Failed to stop frontend: %v", err)
	}

	// Verify it's stopped
	metrics = frontend.GetMetrics()
	if metrics.IsRunning {
		t.Error("Frontend should not be running")
	}
}

func TestWebSocketConnection(t *testing.T) {
	var requestReceived bool
	ts := setupWebSocketTestWithMocks(t, &requestReceived)
	defer ts.cleanup()

	ws := connectToWebSocket(t, ts.url)
	defer func() { _ = ws.Close() }()

	sendWebSocketRequest(t, ws)
	response := receiveWebSocketResponse(t, ws, &requestReceived)
	verifyWebSocketResponse(t, response)
	verifyWebSocketMetrics(t, ts.frontend)
}

func setupWebSocketTestWithMocks(t *testing.T, requestReceived *bool) *testServer {
	t.Helper()

	router := &mockRouter{
		requestHandler: func(ctx context.Context, req *mcp.Request, namespace string) (*mcp.Response, error) {
			t.Logf("Router received request: method=%s, id=%v", req.Method, req.ID)
			*requestReceived = true

			return &mcp.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]interface{}{"echo": req.Method},
			}, nil
		},
	}

	auth := &mockAuth{
		authenticateHandler: func(r *http.Request) (*auth.Claims, error) {
			t.Logf("Auth called for %s", r.RemoteAddr)

			return &auth.Claims{}, nil
		},
	}

	config := Config{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowedOrigins: []string{"*"},
		PingInterval:   10 * time.Second,
	}

	return setupTestServer(t, config, router, auth, newMockSessionManager())
}

func connectToWebSocket(t *testing.T, url string) *websocket.Conn {
	t.Helper()

	t.Logf("Connecting to %s", url)
	ws, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		if resp != nil {
			t.Logf("HTTP response: %d %s", resp.StatusCode, resp.Status)
		}
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	t.Log("Connected successfully")

	return ws
}

func sendWebSocketRequest(t *testing.T, ws *websocket.Conn) {
	t.Helper()

	wireMsg := map[string]interface{}{
		"id":        "test-1",
		"timestamp": time.Now().Format(time.RFC3339),
		"source":    "test-client",
		"mcp_payload": map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "test/echo",
			"params":  map[string]interface{}{},
		},
	}

	t.Logf("Sending wire message: %+v", wireMsg)
	if err := ws.WriteJSON(wireMsg); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
}

func receiveWebSocketResponse(t *testing.T, ws *websocket.Conn, requestReceived *bool) map[string]interface{} {
	t.Helper()

	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var response map[string]interface{}

	t.Log("Waiting for response...")
	if err := ws.ReadJSON(&response); err != nil {
		t.Logf("Request was received by router: %v", *requestReceived)
		t.Fatalf("Failed to read response: %v", err)
	}

	t.Logf("Received response: %+v", response)

	return response
}

func verifyWebSocketResponse(t *testing.T, response map[string]interface{}) {
	t.Helper()

	// Extract MCP payload from wire message
	mcpPayload, ok := response["mcp_payload"].(map[string]interface{})
	if !ok {
		t.Fatalf("Response missing mcp_payload: %+v", response)
	}

	if mcpPayload["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %v", mcpPayload["jsonrpc"])
	}

	if result, ok := mcpPayload["result"].(map[string]interface{}); ok {
		if result["echo"] != "test/echo" {
			t.Errorf("Expected echo 'test/echo', got %v", result["echo"])
		}
	} else {
		t.Error("Response missing result field")
	}
}

func verifyWebSocketMetrics(t *testing.T, frontend *Frontend) {
	t.Helper()

	time.Sleep(100 * time.Millisecond)

	metrics := frontend.GetMetrics()
	if metrics.TotalConnections == 0 {
		t.Error("TotalConnections should be > 0")
	}
	if metrics.RequestCount == 0 {
		t.Error("RequestCount should be > 0")
	}
}

func TestWebSocketConnectionLimit(t *testing.T) {
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 1, // Only allow 1 connection
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowedOrigins: []string{"*"},
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	// First connection should succeed
	ws1, resp1, err := websocket.DefaultDialer.Dial(ts.url, nil)
	if err != nil {
		t.Fatalf("First connection should succeed: %v", err)
	}
	if resp1 != nil && resp1.Body != nil {
		defer func() { _ = resp1.Body.Close() }()
	}
	defer func() { _ = ws1.Close() }()

	// Give it time to register
	time.Sleep(200 * time.Millisecond)

	// Second connection should be rejected
	ws2, resp2, err := websocket.DefaultDialer.Dial(ts.url, nil)
	if err == nil {
		_ = ws2.Close()
		t.Fatal("Second connection should have been rejected")
	}
	if resp2 != nil && resp2.Body != nil {
		_ = resp2.Body.Close()
	}

	if resp2 != nil && resp2.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", resp2.StatusCode)
	}
}

func TestWebSocketInvalidMessage(t *testing.T) {
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowedOrigins: []string{"*"},
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	ws, resp, err := websocket.DefaultDialer.Dial(ts.url, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = ws.Close() }()

	// Send invalid JSON
	if err := ws.WriteMessage(websocket.TextMessage, []byte("invalid json")); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Should receive error response
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var response map[string]interface{}
	if err := ws.ReadJSON(&response); err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Extract MCP payload from wire message
	mcpPayload, ok := response["mcp_payload"].(map[string]interface{})
	if !ok {
		t.Fatalf("Response missing mcp_payload: %+v", response)
	}

	if mcpPayload["error"] == nil {
		t.Error("Expected error in response")
	}
}

func TestWebSocketMetrics(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "localhost",
		Port:           0,
		MaxConnections: 100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		AllowedOrigins: []string{"*"},
	}

	frontend := CreateWebSocketFrontend("test-ws", config, router, auth, sessions, logger)

	// Initial metrics
	metrics := frontend.GetMetrics()
	if metrics.IsRunning {
		t.Error("Frontend should not be running initially")
	}
	if metrics.ActiveConnections != 0 {
		t.Error("ActiveConnections should be 0 initially")
	}

	ctx := context.Background()
	if err := frontend.Start(ctx); err != nil {
		t.Fatalf("Failed to start frontend: %v", err)
	}
	defer func() { _ = frontend.Stop(ctx) }()

	// After start
	metrics = frontend.GetMetrics()
	if !metrics.IsRunning {
		t.Error("Frontend should be running after Start()")
	}
}

func TestConfigApplyDefaults(t *testing.T) {
	config := Config{}
	config.ApplyDefaults()

	if config.Host != "0.0.0.0" {
		t.Errorf("Expected default host '0.0.0.0', got '%s'", config.Host)
	}
	if config.Port != 8443 {
		t.Errorf("Expected default port 8443, got %d", config.Port)
	}
	if config.MaxConnections != 10000 {
		t.Errorf("Expected default MaxConnections 10000, got %d", config.MaxConnections)
	}
	if config.ReadTimeout != 60*time.Second {
		t.Errorf("Expected default ReadTimeout 60s, got %v", config.ReadTimeout)
	}
	if config.WriteTimeout != 60*time.Second {
		t.Errorf("Expected default WriteTimeout 60s, got %v", config.WriteTimeout)
	}
}
