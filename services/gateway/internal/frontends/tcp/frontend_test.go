package tcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/wire"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// Test helpers

type testServer struct {
	frontend *Frontend
	addr     string
	t        *testing.T
}

func setupTestServer(
	t *testing.T,
	config Config,
	router *mockRouter,
	auth *mockAuth,
	sessions *mockSessionManager,
) *testServer {
	t.Helper()

	logger := zap.NewNop()
	frontend := CreateTCPFrontend("test-tcp", config, router, auth, sessions, logger)

	ctx := context.Background()
	if err := frontend.Start(ctx); err != nil {
		t.Fatalf("Failed to start frontend: %v", err)
	}

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	addr := frontend.listener.Addr().String()

	return &testServer{
		frontend: frontend,
		addr:     addr,
		t:        t,
	}
}

func (ts *testServer) cleanup() {
	ts.t.Helper()
	ctx := context.Background()
	if err := ts.frontend.Stop(ctx); err != nil {
		ts.t.Errorf("Failed to stop frontend: %v", err)
	}
}

// Mock implementations

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
	return &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "test-user"},
	}, nil
}

func (m *mockAuth) ValidateToken(token string) (*auth.Claims, error) {
	return &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "test-user"},
	}, nil
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

// Tests

func TestCreateTCPFrontend(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "localhost",
		Port:           9090,
		MaxConnections: 100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
	}

	frontend := CreateTCPFrontend("test-tcp", config, router, auth, sessions, logger)

	if frontend == nil {
		t.Fatal("CreateTCPFrontend returned nil")
	}

	if frontend.GetName() != "test-tcp" {
		t.Errorf("Expected name 'test-tcp', got '%s'", frontend.GetName())
	}

	if frontend.GetProtocol() != "tcp_binary" {
		t.Errorf("Expected protocol 'tcp_binary', got '%s'", frontend.GetProtocol())
	}
}

func TestTCPFrontendStartStop(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "127.0.0.1",
		Port:           0, // Use random port
		MaxConnections: 100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
	}

	frontend := CreateTCPFrontend("test-tcp", config, router, auth, sessions, logger)

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

func TestTCPConnection(t *testing.T) {
	var requestReceived bool
	ts := setupTCPTestWithMocks(t, &requestReceived)
	defer ts.cleanup()

	conn := connectToTCPServer(t, ts.addr)
	defer func() { _ = conn.Close() }()

	sendTCPRequest(t, conn)
	resp := receiveTCPResponse(t, conn, &requestReceived)
	verifyTCPResponse(t, resp)
	verifyTCPMetrics(t, ts.frontend)
}

func setupTCPTestWithMocks(t *testing.T, requestReceived *bool) *testServer {
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

	config := Config{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
	}

	return setupTestServer(t, config, router, &mockAuth{}, newMockSessionManager())
}

func connectToTCPServer(t *testing.T, addr string) net.Conn {
	t.Helper()

	t.Logf("Connecting to %s", addr)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to TCP server: %v", err)
	}

	t.Log("Connected successfully")
	return conn
}

func sendTCPRequest(t *testing.T, conn net.Conn) {
	t.Helper()

	transport := wire.NewTransport(conn)

	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test/echo",
		Params:  map[string]interface{}{},
	}

	t.Logf("Sending request: %+v", req)
	if err := transport.SendRequest(req); err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
}

func receiveTCPResponse(t *testing.T, conn net.Conn, requestReceived *bool) *mcp.Response {
	t.Helper()

	transport := wire.NewTransport(conn)

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	msgType, payload, err := transport.ReceiveMessage()
	if err != nil {
		t.Logf("Request was received by router: %v", *requestReceived)
		t.Fatalf("Failed to read response: %v", err)
	}

	t.Logf("Received message type: %d", msgType)

	if msgType != wire.MessageTypeResponse {
		t.Errorf("Expected response message type, got %d", msgType)
	}

	resp, ok := payload.(*mcp.Response)
	if !ok {
		t.Fatalf("Payload is not a response: %T", payload)
	}

	t.Logf("Received response: %+v", resp)
	return resp
}

func verifyTCPResponse(t *testing.T, resp *mcp.Response) {
	t.Helper()

	if resp.JSONRPC != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %v", resp.JSONRPC)
	}

	if result, ok := resp.Result.(map[string]interface{}); ok {
		if result["echo"] != "test/echo" {
			t.Errorf("Expected echo 'test/echo', got %v", result["echo"])
		}
	} else {
		t.Error("Response missing result field")
	}
}

func verifyTCPMetrics(t *testing.T, frontend *Frontend) {
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

func TestTCPConnectionLimit(t *testing.T) {
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 1, // Only allow 1 connection
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	// First connection should succeed
	conn1, err := net.Dial("tcp", ts.addr)
	if err != nil {
		t.Fatalf("First connection should succeed: %v", err)
	}
	defer func() { _ = conn1.Close() }()

	// Give it time to register
	time.Sleep(200 * time.Millisecond)

	// Verify we have 1 connection
	metrics := ts.frontend.GetMetrics()
	if metrics.ActiveConnections != 1 {
		t.Errorf("Expected 1 active connection, got %d", metrics.ActiveConnections)
	}

	// Second connection should be accepted but handled with limit logic
	conn2, err := net.Dial("tcp", ts.addr)
	if err != nil {
		t.Fatalf("Failed to dial second connection: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	// The connection is accepted but should be closed by the server due to limit
	time.Sleep(200 * time.Millisecond)
}

func TestTCPMetrics(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "127.0.0.1",
		Port:           0,
		MaxConnections: 100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
	}

	frontend := CreateTCPFrontend("test-tcp", config, router, auth, sessions, logger)

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
	if config.Port != 8444 {
		t.Errorf("Expected default port 8444, got %d", config.Port)
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
