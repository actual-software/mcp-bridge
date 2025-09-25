package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

const (
	// TestTimeoutMs is the default timeout for test connections.
	TestTimeoutMs = 5000
	// ExpectedConnectionCount is the expected number of connections in pooling tests.
	// This includes 1 from TestConnectionReuse + 2 from TestReconnection = 3 total.
	ExpectedConnectionCount = 3
)

// ConnectionPoolingTestEnvironment manages connection pooling tests.
type ConnectionPoolingTestEnvironment struct {
	t               *testing.T
	logger          *zap.Logger
	connectionCount *int64
	serverAddr      string
	serverStop      func()
}

// EstablishConnectionPoolingTest creates a connection pooling test environment.
func EstablishConnectionPoolingTest(t *testing.T, logger *zap.Logger) *ConnectionPoolingTestEnvironment {
	t.Helper()

	connectionCount := int64(0)

	return &ConnectionPoolingTestEnvironment{
		t:               t,
		logger:          logger,
		connectionCount: &connectionCount,
	}
}

// CreatePoolingServerHandler creates the server handler for pooling tests.
func (env *ConnectionPoolingTestEnvironment) CreatePoolingServerHandler() func(net.Conn) {
	return func(conn net.Conn) {
		atomic.AddInt64(env.connectionCount, 1)

		reader := bufio.NewReader(conn)

		// Handle version negotiation.
		if err := env.handleVersionNegotiation(reader, conn); err != nil {
			return
		}

		// Handle echo requests.
		env.handleEchoRequests(reader, conn)
	}
}

// handleVersionNegotiation handles version negotiation protocol.
func (env *ConnectionPoolingTestEnvironment) handleVersionNegotiation(reader *bufio.Reader, conn net.Conn) error {
	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return err
	}

	if frame.MessageType != MessageTypeVersionNegotiation {
		return fmt.Errorf("expected version negotiation, got %d", frame.MessageType)
	}

	return env.sendVersionAck(conn)
}

// sendVersionAck sends version acknowledgment.
func (env *ConnectionPoolingTestEnvironment) sendVersionAck(conn net.Conn) error {
	ackPayload := map[string]interface{}{"agreed_version": 1}
	ackData, _ := json.Marshal(ackPayload)
	ackFrame := &BinaryFrame{
		Version:     1,
		MessageType: MessageTypeVersionAck,
		Payload:     ackData,
	}

	return ackFrame.Write(conn)
}

// handleEchoRequests processes echo requests.
func (env *ConnectionPoolingTestEnvironment) handleEchoRequests(reader *bufio.Reader, conn net.Conn) {
	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			return
		}

		if frame.MessageType == MessageTypeRequest {
			env.echoResponse(conn, frame)
		}
	}
}

// echoResponse echoes back the request as a response.
func (env *ConnectionPoolingTestEnvironment) echoResponse(conn net.Conn, requestFrame *BinaryFrame) {
	respFrame := &BinaryFrame{
		Version:     requestFrame.Version,
		MessageType: MessageTypeResponse,
		Payload:     requestFrame.Payload,
	}
	_ = respFrame.Write(conn)
}

// SetupServer sets server information.
func (env *ConnectionPoolingTestEnvironment) SetupServer(addr string, stopFunc func()) {
	env.serverAddr = addr
	env.serverStop = stopFunc
}

// CreateClient creates and connects a TCP client.
func (env *ConnectionPoolingTestEnvironment) CreateClient() *TCPClient {
	cfg := config.GatewayConfig{
		URL: "tcp://" + env.serverAddr,
		Connection: common.ConnectionConfig{
			TimeoutMs: TestTimeoutMs,
		},
	}

	client, err := NewTCPClient(cfg, env.logger)
	if err != nil {
		env.t.Fatalf("Failed to create client: %v", err)
	}

	return client
}

// ConnectClient connects the client to the server.
func (env *ConnectionPoolingTestEnvironment) ConnectClient(client *TCPClient) {
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		env.t.Fatalf("Failed to connect: %v", err)
	}
}

// TestConnectionReuse tests that multiple requests use the same connection.
func (env *ConnectionPoolingTestEnvironment) TestConnectionReuse(numRequests int) {
	client := env.CreateClient()

	env.ConnectClient(client)

	defer func() { _ = client.Close() }()

	// Send multiple requests.
	env.sendMultipleRequests(client, numRequests)

	// Verify only one connection was used.
	env.verifyConnectionCount(1, numRequests)
}

// sendMultipleRequests sends multiple requests through the client.
func (env *ConnectionPoolingTestEnvironment) sendMultipleRequests(client *TCPClient, count int) {
	for i := 0; i < count; i++ {
		req := env.createTestRequest(i)

		if err := client.SendRequest(req); err != nil {
			env.t.Errorf("Failed to send request %d: %v", i, err)
		}

		if _, err := client.ReceiveResponse(); err != nil {
			env.t.Errorf("Failed to receive response %d: %v", i, err)
		}
	}
}

// createTestRequest creates a test request.
func (env *ConnectionPoolingTestEnvironment) createTestRequest(index int) *mcp.Request {
	return &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  fmt.Sprintf("test_%d", index),
		ID:      fmt.Sprintf("req-%d", index),
	}
}

// verifyConnectionCount verifies the expected number of connections.
func (env *ConnectionPoolingTestEnvironment) verifyConnectionCount(expected int, requestCount int) {
	actual := atomic.LoadInt64(env.connectionCount)
	if actual != int64(expected) {
		env.t.Errorf("Expected %d connection(s) for %d requests, got %d",
			expected, requestCount, actual)
	}
}

// TestReconnection tests reconnection behavior.
func (env *ConnectionPoolingTestEnvironment) TestReconnection() {
	// First client.
	client1 := env.CreateClient()
	env.ConnectClient(client1)

	req := env.createReconnectRequest("first")
	if err := client1.SendRequest(req); err != nil {
		env.t.Errorf("Failed to send request on first connection: %v", err)
	}

	_ = client1.Close()

	// Second client (should create new connection).
	client2 := env.CreateClient()
	env.ConnectClient(client2)

	req = env.createReconnectRequest("second")
	if err := client2.SendRequest(req); err != nil {
		env.t.Errorf("Failed to send request on second connection: %v", err)
	}

	_ = client2.Close()

	// Verify two connections were established.
	env.verifyTotalConnections(ExpectedConnectionCount)
}

// createReconnectRequest creates a reconnection test request.
func (env *ConnectionPoolingTestEnvironment) createReconnectRequest(id string) *mcp.Request {
	return &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test_reconnect",
		ID:      "reconnect-" + id,
	}
}

// verifyTotalConnections verifies total connection count.
func (env *ConnectionPoolingTestEnvironment) verifyTotalConnections(expected int) {
	actual := atomic.LoadInt64(env.connectionCount)
	if actual != int64(expected) {
		env.t.Errorf("Expected %d total connections, got %d", expected, actual)
	}
}

// GetConnectionCount returns the current connection count.
func (env *ConnectionPoolingTestEnvironment) GetConnectionCount() int64 {
	return atomic.LoadInt64(env.connectionCount)
}

// Cleanup cleans up test resources.
func (env *ConnectionPoolingTestEnvironment) Cleanup() {
	if env.serverStop != nil {
		env.serverStop()
	}
}
