package sse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// Test helpers

type testServer struct {
	frontend *Frontend
	url      string
	t        *testing.T
}

func setupTestServer(t *testing.T, config Config, router *mockRouter, auth *mockAuth, sessions *mockSessionManager) *testServer {
	t.Helper()

	logger := zap.NewNop()
	frontend := CreateSSEFrontend("test-sse", config, router, auth, sessions, logger)

	ctx := context.Background()
	if err := frontend.Start(ctx); err != nil {
		t.Fatalf("Failed to start frontend: %v", err)
	}

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	url := fmt.Sprintf("http://%s", frontend.server.Addr)

	return &testServer{
		frontend: frontend,
		url:      url,
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

// Tests

func TestCreateSSEFrontend(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:            "localhost",
		Port:            8081,
		StreamEndpoint:  "/events",
		RequestEndpoint: "/api/v1/request",
	}

	frontend := CreateSSEFrontend("test-sse", config, router, auth, sessions, logger)

	if frontend == nil {
		t.Fatal("CreateSSEFrontend returned nil")
	}

	if frontend.GetName() != "test-sse" {
		t.Errorf("Expected name 'test-sse', got '%s'", frontend.GetName())
	}

	if frontend.GetProtocol() != "sse" {
		t.Errorf("Expected protocol 'sse', got '%s'", frontend.GetProtocol())
	}
}

func TestSSEFrontendStartStop(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:            "127.0.0.1",
		Port:            0,
		StreamEndpoint:  "/events",
		RequestEndpoint: "/api/v1/request",
	}

	frontend := CreateSSEFrontend("test-sse", config, router, auth, sessions, logger)

	ctx := context.Background()

	if err := frontend.Start(ctx); err != nil {
		t.Fatalf("Failed to start frontend: %v", err)
	}

	metrics := frontend.GetMetrics()
	if !metrics.IsRunning {
		t.Error("Frontend should be running")
	}

	if err := frontend.Stop(ctx); err != nil {
		t.Fatalf("Failed to stop frontend: %v", err)
	}

	metrics = frontend.GetMetrics()
	if metrics.IsRunning {
		t.Error("Frontend should not be running")
	}
}

func TestSSEStreamConnection(t *testing.T) {
	router := &mockRouter{}
	auth := &mockAuth{
		authenticateHandler: func(r *http.Request) (*auth.Claims, error) {
			t.Logf("Auth called for %s", r.RemoteAddr)
			return &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: "test-user"},
			}, nil
		},
	}
	sessions := newMockSessionManager()

	config := Config{
		Host:            "127.0.0.1",
		Port:            0,
		StreamEndpoint:  "/events",
		RequestEndpoint: "/api/v1/request",
		KeepAlive:       1 * time.Second, // Short keepalive for testing
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	url := ts.url + config.StreamEndpoint
	t.Logf("Connecting to SSE stream at %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to SSE stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", resp.Header.Get("Content-Type"))
	}

	// Read a few lines to verify stream is working
	reader := bufio.NewReader(resp.Body)
	linesRead := 0
	for linesRead < 3 {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Error reading from stream: %v", err)
		}
		t.Logf("Received: %s", strings.TrimSpace(line))
		linesRead++
	}

	if linesRead == 0 {
		t.Error("No data received from SSE stream")
	}

	// Verify metrics
	time.Sleep(100 * time.Millisecond)
	metrics := ts.frontend.GetMetrics()
	if metrics.TotalConnections == 0 {
		t.Error("TotalConnections should be > 0")
	}
}

func TestSSERequestEndpoint(t *testing.T) {
	requestReceived := false
	responseReceived := false
	router := &mockRouter{
		requestHandler: func(ctx context.Context, req *mcp.Request, namespace string) (*mcp.Response, error) {
			t.Logf("Router received request: method=%s, id=%v", req.Method, req.ID)
			requestReceived = true
			return &mcp.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]interface{}{"echo": req.Method},
			}, nil
		},
	}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:            "127.0.0.1",
		Port:            0,
		StreamEndpoint:  "/events",
		RequestEndpoint: "/api/v1/request",
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	// First establish SSE stream
	streamURL := ts.url + config.StreamEndpoint
	streamReq, _ := http.NewRequest("GET", streamURL, nil)
	streamClient := &http.Client{Timeout: 30 * time.Second}
	streamResp, err := streamClient.Do(streamReq)
	if err != nil {
		t.Fatalf("Failed to establish stream: %v", err)
	}
	if streamResp != nil && streamResp.Body != nil {
		defer func() { _ = streamResp.Body.Close() }()
	}

	// Start reading from stream in background
	go func() {
		reader := bufio.NewReader(streamResp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			t.Logf("Stream received: %s", strings.TrimSpace(line))
			if strings.Contains(line, "echo") {
				responseReceived = true
			}
		}
	}()

	// Give stream time to establish
	time.Sleep(200 * time.Millisecond)

	// Now send request
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

	body, err := json.Marshal(wireMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	url := ts.url + config.RequestEndpoint
	t.Logf("Sending request to %s", url)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 202, got %d, body: %s", resp.StatusCode, string(body))
	}

	if !requestReceived {
		t.Error("Router did not receive the request")
	}

	// Wait for response on stream
	time.Sleep(500 * time.Millisecond)
	if !responseReceived {
		t.Error("Response not received on stream")
	}

	// Verify metrics
	metrics := ts.frontend.GetMetrics()
	if metrics.RequestCount == 0 {
		t.Error("RequestCount should be > 0")
	}
}

func TestSSEMethodNotAllowed(t *testing.T) {
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:            "127.0.0.1",
		Port:            0,
		StreamEndpoint:  "/events",
		RequestEndpoint: "/api/v1/request",
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	// Try POST on stream endpoint (should be GET only)
	url := ts.url + config.StreamEndpoint
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}

	// Try GET on request endpoint (should be POST only)
	url = ts.url + config.RequestEndpoint
	resp, err = http.Get(url)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestConfigApplyDefaults(t *testing.T) {
	config := Config{}
	config.ApplyDefaults()

	if config.Host != "0.0.0.0" {
		t.Errorf("Expected default host '0.0.0.0', got '%s'", config.Host)
	}
	if config.Port != 8081 {
		t.Errorf("Expected default port 8081, got %d", config.Port)
	}
	if config.StreamEndpoint != "/events" {
		t.Errorf("Expected default stream endpoint '/events', got '%s'", config.StreamEndpoint)
	}
	if config.RequestEndpoint != "/api/v1/request" {
		t.Errorf("Expected default request endpoint '/api/v1/request', got '%s'", config.RequestEndpoint)
	}
}
