package sse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

func setupTestServer(
	t *testing.T,
	config Config,
	router *mockRouter,
	auth *mockAuth,
	sessions *mockSessionManager,
) *testServer {
	t.Helper()

	logger := zap.NewNop()
	frontend := CreateSSEFrontend("test-sse", config, router, auth, sessions, logger)

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

	url := fmt.Sprintf("http://%s", ln.Addr().String())

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
	ts := setupSSEStreamTest(t)
	defer ts.cleanup()

	resp := connectToSSEStream(t, ts.url+"/events")
	defer func() { _ = resp.Body.Close() }()

	verifySSEStreamHeaders(t, resp)
	verifySSEStreamData(t, resp)
	verifySSEMetrics(t, ts.frontend)
}

func setupSSEStreamTest(t *testing.T) *testServer {
	t.Helper()

	auth := &mockAuth{
		authenticateHandler: func(r *http.Request) (*auth.Claims, error) {
			t.Logf("Auth called for %s", r.RemoteAddr)

			return &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: "test-user"},
			}, nil
		},
	}

	config := Config{
		Host:            "127.0.0.1",
		Port:            0,
		StreamEndpoint:  "/events",
		RequestEndpoint: "/api/v1/request",
		KeepAlive:       1 * time.Second,
	}

	return setupTestServer(t, config, &mockRouter{}, auth, newMockSessionManager())
}

func connectToSSEStream(t *testing.T, url string) *http.Response {
	t.Helper()

	t.Logf("Connecting to SSE stream at %s", url)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to SSE stream: %v", err)
	}

	return resp
}

func verifySSEStreamHeaders(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", resp.Header.Get("Content-Type"))
	}
}

func verifySSEStreamData(t *testing.T, resp *http.Response) {
	t.Helper()

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
}

func verifySSEMetrics(t *testing.T, frontend *Frontend) {
	t.Helper()

	time.Sleep(100 * time.Millisecond)

	metrics := frontend.GetMetrics()
	if metrics.TotalConnections == 0 {
		t.Error("TotalConnections should be > 0")
	}
}

func TestSSERequestEndpoint(t *testing.T) {
	requestChan := make(chan bool, 1)
	responseChan := make(chan bool, 1)

	ts := setupSSERequestTest(t, requestChan)
	defer ts.cleanup()

	streamResp := establishSSEStream(t, ts.url+"/events", responseChan)
	defer func() { _ = streamResp.Body.Close() }()

	sendSSERequest(t, ts.url+"/api/v1/request")
	verifySSERequestReceived(t, requestChan, responseChan)
	verifySSERequestMetrics(t, ts.frontend)
}

func setupSSERequestTest(t *testing.T, requestChan chan bool) *testServer {
	t.Helper()

	router := &mockRouter{
		requestHandler: func(ctx context.Context, req *mcp.Request, namespace string) (*mcp.Response, error) {
			t.Logf("Router received request: method=%s, id=%v", req.Method, req.ID)
			select {
			case requestChan <- true:
			default:
			}

			return &mcp.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]interface{}{"echo": req.Method},
			}, nil
		},
	}

	config := Config{
		Host:            "127.0.0.1",
		Port:            0,
		StreamEndpoint:  "/events",
		RequestEndpoint: "/api/v1/request",
	}

	return setupTestServer(t, config, router, &mockAuth{}, newMockSessionManager())
}

func establishSSEStream(t *testing.T, url string, responseChan chan bool) *http.Response {
	t.Helper()

	streamReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	streamClient := &http.Client{Timeout: 30 * time.Second}

	streamResp, err := streamClient.Do(streamReq)
	if err != nil {
		t.Fatalf("Failed to establish stream: %v", err)
	}

	go readSSEStream(t, streamResp, responseChan)
	time.Sleep(200 * time.Millisecond)

	return streamResp
}

func readSSEStream(t *testing.T, resp *http.Response, responseChan chan bool) {
	t.Helper()

	reader := bufio.NewReader(resp.Body)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		t.Logf("Stream received: %s", strings.TrimSpace(line))

		if strings.Contains(line, "echo") {
			select {
			case responseChan <- true:
			default:
			}
		}
	}
}

func sendSSERequest(t *testing.T, url string) {
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

	body, err := json.Marshal(wireMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	t.Logf("Sending request to %s", url)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected status 202, got %d, body: %s", resp.StatusCode, string(body))
	}
}

func verifySSERequestReceived(t *testing.T, requestChan, responseChan chan bool) {
	t.Helper()

	select {
	case <-requestChan:
	case <-time.After(1 * time.Second):
		t.Error("Router did not receive the request")
	}

	select {
	case <-responseChan:
	case <-time.After(1 * time.Second):
		t.Error("Response not received on stream")
	}
}

func verifySSERequestMetrics(t *testing.T, frontend *Frontend) {
	t.Helper()

	metrics := frontend.GetMetrics()
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

	client := &http.Client{Timeout: 5 * time.Second}
	req1, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req1.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req1)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}

	// Try GET on request endpoint (should be POST only)
	url = ts.url + config.RequestEndpoint

	req2, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err = client.Do(req2)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
