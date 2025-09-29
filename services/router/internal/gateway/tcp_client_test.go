package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	common "github.com/poiley/mcp-bridge/pkg/common/config"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// mockTCPServer simulates a gateway server for testing.
type mockTCPServer struct {
	listener   net.Listener
	t          *testing.T
	handler    func(conn net.Conn)
	startedCh  chan struct{}
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

func newMockTCPServer(t *testing.T, handler func(conn net.Conn)) *mockTCPServer {
	t.Helper()

	return &mockTCPServer{
		t:          t,
		handler:    handler,
		startedCh:  make(chan struct{}),
		shutdownCh: make(chan struct{}),
	}
}

func (s *mockTCPServer) Start() (string, error) {
	lc := &net.ListenConfig{}

	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}

	s.listener = listener

	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		close(s.startedCh)

		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-s.shutdownCh:
					return
				default:
					s.t.Logf("Accept error: %v", err)
				}

				continue
			}

			s.wg.Add(1)

			go func(c net.Conn) {
				defer s.wg.Done()
				defer func() { _ = c.Close() }()

				s.handler(c)
			}(conn)
		}
	}()

	<-s.startedCh

	return listener.Addr().String(), nil
}

func (s *mockTCPServer) Stop() {
	close(s.shutdownCh)
	_ = s.listener.Close()
	s.wg.Wait()
}

// echoHandler echoes back requests as responses.
func echoHandler(conn net.Conn) {
	reader := bufio.NewReader(conn)
	handleEchoVersionNegotiation(conn, reader)
	handleEchoMessages(conn, reader)
}

func handleEchoVersionNegotiation(conn net.Conn, reader *bufio.Reader) {
	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		sendVersionAck(conn)
	}
}

func sendVersionAck(conn net.Conn) {
	ackPayload := map[string]interface{}{"agreed_version": 1}
	ackData, _ := json.Marshal(ackPayload)
	ackFrame := &BinaryFrame{
		Version:     1,
		MessageType: MessageTypeVersionAck,
		Payload:     ackData,
	}
	_ = ackFrame.Write(conn)
}

func handleEchoMessages(conn net.Conn, reader *bufio.Reader) {
	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				_ = conn.Close()
			}
			return
		}

		if frame.MessageType == MessageTypeHealthCheck {
			sendPong(conn, frame)
			continue
		}

		processEchoRequest(conn, frame)
	}
}

func sendPong(conn net.Conn, frame *BinaryFrame) {
	pongFrame := &BinaryFrame{
		Version:     frame.Version,
		MessageType: MessageTypeHealthCheck,
		Payload:     []byte("{}"),
	}
	_ = pongFrame.Write(conn)
}

func processEchoRequest(conn net.Conn, frame *BinaryFrame) {
	var wireMsg struct {
		ID         interface{}     `json:"id"`
		MCPPayload json.RawMessage `json:"mcp_payload"`
	}

	if err := json.Unmarshal(frame.Payload, &wireMsg); err != nil {
		return
	}

	var req mcp.Request
	if err := json.Unmarshal(wireMsg.MCPPayload, &req); err != nil {
		return
	}

	sendEchoResponse(conn, frame, req)
}

func sendEchoResponse(conn net.Conn, frame *BinaryFrame, req mcp.Request) {
	resp := mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		ID:      req.ID,
		Result:  map[string]interface{}{"echo": req.Method},
	}

	respWire := struct {
		ID         interface{} `json:"id"`
		MCPPayload interface{} `json:"mcp_payload"`
	}{
		ID:         req.ID,
		MCPPayload: resp,
	}

	payload, _ := json.Marshal(respWire)
	respFrame := &BinaryFrame{
		Version:     frame.Version,
		MessageType: MessageTypeResponse,
		Payload:     payload,
	}
	_ = respFrame.Write(conn)
}

func TestTCPClient_Connect(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tests := getTCPConnectTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testTCPConnect(t, tt, logger)
		})
	}
}

type tcpConnectTest struct {
	name    string
	handler func(net.Conn)
	cfg     config.GatewayConfig
	wantErr bool
}

func getTCPConnectTests() []tcpConnectTest {
	return []tcpConnectTest{
		{
			name:    "successful connection",
			handler: echoHandler,
			cfg: config.GatewayConfig{
				URL: "tcp://localhost",
				Connection: common.ConnectionConfig{
					TimeoutMs: 5000,
				},
			},
			wantErr: false,
		},
		{
			name:    "connection refused",
			handler: nil,
			cfg: config.GatewayConfig{
				URL: "tcp://localhost:1",
				Connection: common.ConnectionConfig{
					TimeoutMs: testMaxIterations,
				},
			},
			wantErr: true,
		},
		{
			name:    "invalid URL",
			handler: nil,
			cfg: config.GatewayConfig{
				URL: "not-a-url",
			},
			wantErr: true,
		},
	}
}

func testTCPConnect(t *testing.T, tt tcpConnectTest, logger *zap.Logger) {
	t.Helper()
	server := setupTCPTestServer(t, tt)
	if server != nil {
		defer server.Stop()
	}

	client := createTCPTestClient(t, tt.cfg, logger)
	testTCPConnection(t, client, tt.wantErr)
}

func setupTCPTestServer(t *testing.T, tt tcpConnectTest) *mockTCPServer {
	t.Helper()
	if tt.handler == nil {
		return nil
	}

	server := newMockTCPServer(t, tt.handler)
	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	tt.cfg.URL = "tcp://" + addr
	return server
}

func createTCPTestClient(t *testing.T, cfg config.GatewayConfig, logger *zap.Logger) *TCPClient {
	t.Helper()
	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	return client
}

func testTCPConnection(t *testing.T, client *TCPClient, wantErr bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if (err != nil) != wantErr {
		t.Errorf("Connect() error = %v, wantErr %v", err, wantErr)
	}

	if err == nil {
		defer func() {
			if err := client.Close(); err != nil {
				t.Logf("Failed to close client: %v", err)
			}
		}()

		if !client.IsConnected() {
			t.Error("Expected client to be connected")
		}
	}
}

func TestTCPClient_SendRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)
	server, client := setupSendRequestTest(t, logger)
	defer server.Stop()
	defer closeTCPClient(t, client)

	tests := getSendRequestTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSendRequest(t, client, tt)
		})
	}
}

func setupSendRequestTest(t *testing.T, logger *zap.Logger) (*mockTCPServer, *TCPClient) {
	t.Helper()
	server := newMockTCPServer(t, echoHandler)
	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	client := createConnectedTCPClient(t, addr, logger)
	return server, client
}

func createConnectedTCPClient(t *testing.T, addr string, logger *zap.Logger) *TCPClient {
	t.Helper()
	cfg := config.GatewayConfig{
		URL: "tcp://" + addr,
		Auth: common.AuthConfig{
			Type:  "bearer",
			Token: "test-token",
		},
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	return client
}

func closeTCPClient(t *testing.T, client *TCPClient) {
	t.Helper()
	if err := client.Close(); err != nil {
		t.Logf("Failed to close client: %v", err)
	}
}

type sendRequestTest struct {
	name    string
	req     *mcp.Request
	wantErr bool
}

func getSendRequestTests() []sendRequestTest {
	return []sendRequestTest{
		{
			name: "valid request",
			req: &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "test/method",
				Params:  map[string]interface{}{"key": "value"},
				ID:      "test-1",
			},
			wantErr: false,
		},
		{
			name: "request with nil params",
			req: &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "test/nil",
				ID:      123,
			},
			wantErr: false,
		},
	}
}

func testSendRequest(t *testing.T, client *TCPClient, tt sendRequestTest) {
	t.Helper()
	err := client.SendRequest(ctx, tt.req)
	if (err != nil) != tt.wantErr {
		t.Errorf("SendRequest() error = %v, wantErr %v", err, tt.wantErr)
	}
}

func TestTCPClient_ReceiveResponse(t *testing.T) {
	handler := createResponseTestServer(t)
	server := newMockTCPServer(t, handler)

	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	client, cleanup := setupTCPClientForResponse(t, addr)
	defer cleanup()

	sendTestRequestAndReceive(t, client)
}

// setupTCPClientForResponse sets up a TCP client and connects it for response testing.
func setupTCPClientForResponse(t *testing.T, addr string) (*TCPClient, func()) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	cfg := config.GatewayConfig{
		URL: "tcp://" + addr,
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	cleanup := func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client: %v", err)
		}
	}

	return client, cleanup
}

// sendTestRequestAndReceive sends a test request and validates the response.
func sendTestRequestAndReceive(t *testing.T, client *TCPClient) {
	t.Helper()

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-1",
	}

	if err := client.SendRequest(ctx, req); err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}

	validateReceivedResponse(t, client)
}

// validateReceivedResponse validates the received response format and content.
func validateReceivedResponse(t *testing.T, client *TCPClient) {
	t.Helper()

	resp, err := client.ReceiveResponse()
	if err != nil {
		t.Fatalf("ReceiveResponse() error = %v", err)
	}

	if resp.ID != "test-1" {
		t.Errorf("Response ID = %v, want %v", resp.ID, "test-1")
	}

	if resp.Error != nil {
		t.Errorf("Unexpected error in response: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result is not a map: %T", resp.Result)
	}

	if status, ok := result["status"].(string); !ok || status != "ok" {
		t.Errorf("Unexpected result: %v", result)
	}
}

// createResponseTestServer creates a server that handles version negotiation and sends a test response.
func createResponseTestServer(t *testing.T) func(net.Conn) {
	t.Helper()

	return func(conn net.Conn) {
		reader := bufio.NewReader(conn)

		frame := handleVersionNegotiationForResponse(t, conn, reader)
		if frame == nil {
			return
		}

		sendTestResponse(t, conn, frame)
	}
}

// handleVersionNegotiationForResponse handles version negotiation and returns the request frame.
func handleVersionNegotiationForResponse(t *testing.T, conn net.Conn, reader *bufio.Reader) *BinaryFrame {
	t.Helper()

	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return nil
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		sendVersionAckResponse(t, conn)

		// Wait for actual request.
		frame, err = ReadBinaryFrame(reader)
		if err != nil {
			return nil
		}
	}

	return frame
}

// sendVersionAckResponse sends version acknowledgment for response tests.
func sendVersionAckResponse(t *testing.T, conn net.Conn) {
	t.Helper()

	ackPayload := map[string]interface{}{"agreed_version": 1}
	ackData, _ := json.Marshal(ackPayload)
	ackFrame := &BinaryFrame{
		Version:     1,
		MessageType: MessageTypeVersionAck,
		Payload:     ackData,
	}
	_ = ackFrame.Write(conn)
}

// sendTestResponse creates and sends a test response frame.
func sendTestResponse(t *testing.T, conn net.Conn, requestFrame *BinaryFrame) {
	t.Helper()

	resp := mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		ID:      "test-1",
		Result:  map[string]interface{}{"status": "ok"},
	}

	respWire := struct {
		ID         interface{} `json:"id"`
		MCPPayload interface{} `json:"mcp_payload"`
	}{
		ID:         "test-1",
		MCPPayload: resp,
	}

	payload, _ := json.Marshal(respWire)
	respFrame := &BinaryFrame{
		Version:     requestFrame.Version,
		MessageType: MessageTypeResponse,
		Payload:     payload,
	}

	_ = respFrame.Write(conn)
}

func TestTCPClient_SendPing(t *testing.T) {
	logger := zaptest.NewLogger(t)

	pingReceived := make(chan struct{})
	handler := createPingTestServer(t, pingReceived)
	server := newMockTCPServer(t, handler)

	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	defer server.Stop()

	cfg := config.GatewayConfig{
		URL: "tcp://" + addr,
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client: %v", err)
		}
	}()

	// Send ping.
	if err := client.SendPing(); err != nil {
		t.Errorf("SendPing() error = %v", err)
	}

	// Wait for ping to be received.
	select {
	case <-pingReceived:
		// Success.
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for ping")
	}
}

// createPingTestServer creates a server that handles version negotiation and ping responses.
func createPingTestServer(t *testing.T, pingReceived chan struct{}) func(net.Conn) {
	t.Helper()

	return func(conn net.Conn) {
		reader := bufio.NewReader(conn)

		handleVersionNegotiationForPing(t, conn, reader)
		processPingRequests(t, conn, reader, pingReceived)
	}
}

// handleVersionNegotiationForPing handles version negotiation for ping tests.
func handleVersionNegotiationForPing(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	t.Helper()

	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		sendPingVersionAck(t, conn)
	}
}

// sendPingVersionAck sends version acknowledgment for ping tests.
func sendPingVersionAck(t *testing.T, conn net.Conn) {
	t.Helper()

	ackPayload := map[string]interface{}{"agreed_version": 1}
	ackData, _ := json.Marshal(ackPayload)
	ackFrame := &BinaryFrame{
		Version:     1,
		MessageType: MessageTypeVersionAck,
		Payload:     ackData,
	}
	_ = ackFrame.Write(conn)
}

// processPingRequests processes ping requests and sends pong responses.
func processPingRequests(t *testing.T, conn net.Conn, reader *bufio.Reader, pingReceived chan struct{}) {
	t.Helper()

	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			return
		}

		if frame.MessageType == MessageTypeHealthCheck {
			close(pingReceived)
			sendPingPong(t, conn, frame)
		}
	}
}

// sendPingPong sends a pong response to a ping request.
func sendPingPong(t *testing.T, conn net.Conn, requestFrame *BinaryFrame) {
	t.Helper()

	pongFrame := &BinaryFrame{
		Version:     requestFrame.Version,
		MessageType: MessageTypeHealthCheck,
		Payload:     []byte("{}"),
	}
	_ = pongFrame.Write(conn)
}

func TestTCPClient_Reconnect(t *testing.T) {
	logger := zaptest.NewLogger(t)

	server := newMockTCPServer(t, echoHandler)

	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	defer server.Stop()

	cfg := config.GatewayConfig{
		URL: "tcp://" + addr,
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	// Connect.
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	if !client.IsConnected() {
		t.Error("Expected client to be connected")
	}

	// Close connection.
	_ = client.Close()

	if client.IsConnected() {
		t.Error("Expected client to be disconnected")
	}

	// Reconnect.
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to reconnect: %v", err)
	}

	if !client.IsConnected() {
		t.Error("Expected client to be connected after reconnect")
	}

	_ = client.Close()
}

func TestTCPClient_TLS(t *testing.T) {
	// Skip TLS test in short mode.
	if testing.Short() {
		t.Skip("Skipping TLS test in short mode")
	}

	logger := zaptest.NewLogger(t)

	// For this test, we would need proper TLS certificates.
	// This is a placeholder showing the structure.

	cfg := config.GatewayConfig{
		URL: fmt.Sprintf("tcps://localhost:%d", constants.TestHTTPSPort),
		TLS: common.TLSConfig{
			Verify:     false, // Skip verification for test
			MinVersion: "1.2",
		},
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Verify TLS config was set.
	if client.tlsConfig == nil {
		t.Error("Expected TLS config to be set")
	}

	if client.tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("Expected TLS 1.2, got %v", client.tlsConfig.MinVersion)
	}
}

func TestTCPClient_OAuth2(t *testing.T) {
	logger := zaptest.NewLogger(t)

	handler := createOAuth2TestServer(t)
	server := newMockTCPServer(t, handler)

	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	cfg := config.GatewayConfig{
		URL: "tcp://" + addr,
		Auth: common.AuthConfig{
			Type:  "bearer",
			Token: "test-oauth-token",
		},
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client: %v", err)
		}
	}()

	// Send request - should include auth token.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-1",
	}
	if err := client.SendRequest(ctx, req); err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}

	// Receive response.
	resp, err := client.ReceiveResponse()
	if err != nil {
		t.Fatalf("ReceiveResponse() error = %v", err)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("Result is not a map: %T", resp.Result)
	}

	if auth, ok := result["authenticated"].(bool); !ok || !auth {
		t.Errorf("Expected authenticated=true, got %v", result)
	}
}

// createOAuth2TestServer creates a server that validates OAuth2 tokens and responds with authentication status.
func createOAuth2TestServer(t *testing.T) func(net.Conn) {
	t.Helper()

	return func(conn net.Conn) {
		reader := bufio.NewReader(conn)

		frame := handleVersionNegotiationForOAuth2(t, conn, reader)
		if frame == nil {
			return
		}

		validateOAuth2TokenAndRespond(t, conn, frame)
	}
}

// handleVersionNegotiationForOAuth2 handles version negotiation for OAuth2 tests.
func handleVersionNegotiationForOAuth2(t *testing.T, conn net.Conn, reader *bufio.Reader) *BinaryFrame {
	t.Helper()

	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return nil
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		sendVersionAckForOAuth2(t, conn)

		// Wait for actual request.
		frame, err = ReadBinaryFrame(reader)
		if err != nil {
			return nil
		}
	}

	return frame
}

// sendVersionAckForOAuth2 sends version acknowledgment for OAuth2 tests.
func sendVersionAckForOAuth2(t *testing.T, conn net.Conn) {
	t.Helper()

	ackPayload := map[string]interface{}{"agreed_version": 1}
	ackData, _ := json.Marshal(ackPayload)
	ackFrame := &BinaryFrame{
		Version:     1,
		MessageType: MessageTypeVersionAck,
		Payload:     ackData,
	}
	_ = ackFrame.Write(conn)
}

// validateOAuth2TokenAndRespond validates the OAuth2 token and sends an authentication response.
func validateOAuth2TokenAndRespond(t *testing.T, conn net.Conn, frame *BinaryFrame) {
	t.Helper()

	var wireMsg struct {
		AuthToken  string          `json:"auth_token"`
		MCPPayload json.RawMessage `json:"mcp_payload"`
	}

	if err := json.Unmarshal(frame.Payload, &wireMsg); err != nil {
		t.Errorf("Failed to unmarshal wire message: %v", err)

		return
	}

	if wireMsg.AuthToken == "" {
		t.Error("Expected auth token in request")
	}

	sendOAuth2AuthResponse(t, conn, frame)
}

// sendOAuth2AuthResponse sends an OAuth2 authentication response.
func sendOAuth2AuthResponse(t *testing.T, conn net.Conn, requestFrame *BinaryFrame) {
	t.Helper()

	resp := mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		ID:      "test-1",
		Result:  map[string]interface{}{"authenticated": true},
	}

	respWire := struct {
		ID         interface{} `json:"id"`
		MCPPayload interface{} `json:"mcp_payload"`
	}{
		ID:         "test-1",
		MCPPayload: resp,
	}

	payload, _ := json.Marshal(respWire)
	respFrame := &BinaryFrame{
		Version:     requestFrame.Version,
		MessageType: MessageTypeResponse,
		Payload:     payload,
	}

	_ = respFrame.Write(conn)
}
