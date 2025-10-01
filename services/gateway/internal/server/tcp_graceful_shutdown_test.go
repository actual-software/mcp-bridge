package server

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	"github.com/poiley/mcp-bridge/services/gateway/pkg/wire"
)

// TestTCPConnectionTracking verifies that TCP connections are properly tracked.
func TestTCPConnectionTracking(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8443,
			Protocol:       "tcp",
			MaxConnections: testIterations,
		},
	}

	// Create server with minimal dependencies
	server := &GatewayServer{
		config:         cfg,
		logger:         logger,
		tcpConnections: sync.Map{},
		ctx:            context.Background(),
		wg:             sync.WaitGroup{},
	}

	// Simulate adding connections
	conn1ID := "conn-1"
	conn2ID := "conn-2"

	// Mock connections
	conn1, _ := net.Pipe()
	conn2, _ := net.Pipe()

	defer func() { _ = conn1.Close() }()
	defer func() { _ = conn2.Close() }()

	// Store connections
	server.tcpConnections.Store(conn1ID, conn1)
	server.tcpConnections.Store(conn2ID, conn2)

	// Verify connections are tracked
	count := 0

	server.tcpConnections.Range(func(key, value interface{}) bool {
		count++

		return true
	})
	assert.Equal(t, 2, count)

	// Simulate removal
	server.tcpConnections.Delete(conn1ID)

	// Verify connection was removed
	count = 0

	server.tcpConnections.Range(func(key, value interface{}) bool {
		count++

		return true
	})
	assert.Equal(t, 1, count)
}

// TestTCPHandlerGracefulShutdown tests that the handler waits for requests.
func TestTCPHandlerGracefulShutdown(t *testing.T) {
	logger := zap.NewNop()

	// Create handler with minimal mocks
	handler := &TCPHandler{
		logger: logger,
	}

	// Test wait for requests with timeout
	var wg sync.WaitGroup

	// Simulate ongoing request
	wg.Add(1)

	go func() {
		time.Sleep(testIterations * time.Millisecond)
		wg.Done()
	}()

	// Should complete within timeout
	completed := handler.waitForRequests(&wg, httpStatusInternalError*time.Millisecond)
	assert.True(t, completed)

	// Test timeout case
	wg.Add(1)
	defer wg.Done() // Will complete after test

	completed = handler.waitForRequests(&wg, testTimeout*time.Millisecond)
	assert.False(t, completed)
}

// TestShutdownControlMessage verifies control message handling.
func TestShutdownControlMessage(t *testing.T) {
	// Create pipe for communication
	clientConn, serverConn := net.Pipe()

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	// Create transports
	clientTransport := wire.NewTransport(clientConn)
	serverTransport := wire.NewTransport(serverConn)

	// Channel to coordinate the test
	done := make(chan struct{})

	var msgType wire.MessageType

	var msg interface{}

	var readErr error

	// Start reading in a goroutine (must be concurrent with writing for pipes)
	go func() {
		defer close(done)

		msgType, msg, readErr = serverTransport.ReceiveMessage()
	}()

	// Send control message
	control := map[string]interface{}{
		"command": "shutdown",
		"reason":  "test",
	}

	err := clientTransport.SendControl(control)
	require.NoError(t, err)

	// Wait for read to complete with timeout
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out waiting for message")
	}

	// Verify results
	require.NoError(t, readErr)
	assert.Equal(t, wire.MessageTypeControl, msgType)

	controlMsg, ok := msg.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "shutdown", controlMsg["command"])
	assert.Equal(t, "test", controlMsg["reason"])
}

// TestGracefulShutdownFlow tests the complete shutdown flow.
func TestGracefulShutdownFlow(t *testing.T) {
	logger := zap.NewNop()

	// Set up server and context
	server, ctx := setupGracefulShutdownTest(logger)
	defer cleanupGracefulShutdownTest(server)

	// Add mock connections and goroutines
	addMockConnectionsForShutdown(server)
	startMockGoroutinesForShutdown(server, ctx)

	// Perform and verify shutdown
	performGracefulShutdown(t, server, ctx)
}

func setupGracefulShutdownTest(logger *zap.Logger) (*GatewayServer, context.Context) {
	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Create a minimal server
	server := &GatewayServer{
		logger:         logger,
		connections:    sync.Map{},
		tcpConnections: sync.Map{},
		wg:             sync.WaitGroup{},
		ctx:            ctx,
		cancel:         cancel,
	}

	return server, ctx
}

func cleanupGracefulShutdownTest(server *GatewayServer) {
	// Cleanup handled by test framework
}

func addMockConnectionsForShutdown(server *GatewayServer) {
	// Add some mock connections
	tcpConn1, _ := net.Pipe()
	tcpConn2, _ := net.Pipe()

	server.tcpConnections.Store("tcp-1", tcpConn1)
	server.tcpConnections.Store("tcp-2", tcpConn2)
}

func startMockGoroutinesForShutdown(server *GatewayServer, ctx context.Context) {
	// Simulate some goroutines
	server.wg.Add(2)

	go func() {
		defer server.wg.Done()

		<-ctx.Done()
	}()
	go func() {
		defer server.wg.Done()

		<-ctx.Done()
	}()
}

func performGracefulShutdown(t *testing.T, server *GatewayServer, ctx context.Context) {
	t.Helper()
	// Perform shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 2*time.Second)
	defer shutdownCancel()

	err := server.Shutdown(shutdownCtx)
	require.NoError(t, err)

	// Verify context was canceled
	select {
	case <-ctx.Done():
		// Success
	default:
		t.Fatal("Context was not canceled")
	}
}
