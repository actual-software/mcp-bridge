package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

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
	frontend := CreateHTTPFrontend("test-http", config, router, auth, sessions, logger)

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

// Tests

func TestCreateHTTPFrontend(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:           "localhost",
		Port:           8080,
		RequestPath:    "/api/v1/mcp",
		MaxRequestSize: 1024 * 1024,
	}

	frontend := CreateHTTPFrontend("test-http", config, router, auth, sessions, logger)

	if frontend == nil {
		t.Fatal("CreateHTTPFrontend returned nil")
	}

	if frontend.GetName() != "test-http" {
		t.Errorf("Expected name 'test-http', got '%s'", frontend.GetName())
	}

	if frontend.GetProtocol() != "http" {
		t.Errorf("Expected protocol 'http', got '%s'", frontend.GetProtocol())
	}
}

func TestHTTPFrontendStartStop(t *testing.T) {
	logger := zap.NewNop()
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:        "127.0.0.1",
		Port:        0,
		RequestPath: "/api/v1/mcp",
	}

	frontend := CreateHTTPFrontend("test-http", config, router, auth, sessions, logger)

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

func TestHTTPRequest(t *testing.T) {
	requestReceived := false
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
	auth := &mockAuth{
		authenticateHandler: func(r *http.Request) (*auth.Claims, error) {
			t.Logf("Auth called for %s", r.RemoteAddr)
			return &auth.Claims{}, nil
		},
	}
	sessions := newMockSessionManager()

	config := Config{
		Host:        "127.0.0.1",
		Port:        0,
		RequestPath: "/api/v1/mcp",
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

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

	url := ts.url + config.RequestPath
	t.Logf("Sending request to %s", url)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(respBody, &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	t.Logf("Received response: %+v", response)

	if !requestReceived {
		t.Error("Router did not receive the request")
	}

	if response["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
	}

	if result, ok := response["result"].(map[string]interface{}); ok {
		if result["echo"] != "test/echo" {
			t.Errorf("Expected echo 'test/echo', got %v", result["echo"])
		}
	} else {
		t.Error("Response missing result field")
	}

	// Verify metrics
	time.Sleep(100 * time.Millisecond)
	metrics := ts.frontend.GetMetrics()
	if metrics.TotalConnections == 0 {
		t.Error("TotalConnections should be > 0")
	}
	if metrics.RequestCount == 0 {
		t.Error("RequestCount should be > 0")
	}
}

func TestHTTPMethodNotAllowed(t *testing.T) {
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:        "127.0.0.1",
		Port:        0,
		RequestPath: "/api/v1/mcp",
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	url := ts.url + config.RequestPath
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestHTTPAuthenticationFailure(t *testing.T) {
	router := &mockRouter{}
	auth := &mockAuth{
		authenticateHandler: func(r *http.Request) (*auth.Claims, error) {
			return nil, fmt.Errorf("invalid token")
		},
	}
	sessions := newMockSessionManager()

	config := Config{
		Host:        "127.0.0.1",
		Port:        0,
		RequestPath: "/api/v1/mcp",
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	wireMsg := map[string]interface{}{
		"id":        "test-1",
		"timestamp": time.Now().Format(time.RFC3339),
		"source":    "test-client",
		"mcp_payload": map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "test/echo",
		},
	}

	body, _ := json.Marshal(wireMsg)
	url := ts.url + config.RequestPath
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

func TestHTTPInvalidJSON(t *testing.T) {
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:        "127.0.0.1",
		Port:        0,
		RequestPath: "/api/v1/mcp",
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	url := ts.url + config.RequestPath
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte("invalid json")))
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Should still return 200 with error in response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var response map[string]interface{}
	if err := json.Unmarshal(respBody, &response); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	if response["error"] == nil {
		t.Error("Expected error in response")
	}
}

func TestHTTPHealthEndpoint(t *testing.T) {
	router := &mockRouter{}
	auth := &mockAuth{}
	sessions := newMockSessionManager()

	config := Config{
		Host:        "127.0.0.1",
		Port:        0,
		RequestPath: "/api/v1/mcp",
	}

	ts := setupTestServer(t, config, router, auth, sessions)
	defer ts.cleanup()

	url := ts.url + "/health"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Failed to send health check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK" {
		t.Errorf("Expected 'OK', got '%s'", string(body))
	}
}

func TestConfigApplyDefaults(t *testing.T) {
	config := Config{}
	config.ApplyDefaults()

	if config.Host != "0.0.0.0" {
		t.Errorf("Expected default host '0.0.0.0', got '%s'", config.Host)
	}
	if config.Port != 8080 {
		t.Errorf("Expected default port 8080, got %d", config.Port)
	}
	if config.RequestPath != "/api/v1/mcp" {
		t.Errorf("Expected default path '/api/v1/mcp', got '%s'", config.RequestPath)
	}
	if config.MaxRequestSize != 1024*1024 {
		t.Errorf("Expected default MaxRequestSize 1MB, got %d", config.MaxRequestSize)
	}
}
