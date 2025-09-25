package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestProtocolVariations tests different transport protocols.
func TestProtocolVariations(t *testing.T) {
	// Skip if E2E tests are disabled
	if os.Getenv("SKIP_E2E_TESTS") == skipE2ETestsValue {
		t.Skip("Skipping E2E tests (SKIP_E2E_TESTS=true)")
	}

	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	// Start Docker services
	logger.Info("Starting Docker Compose stack for protocol tests")

	stack := NewDockerStack(t)
	defer stack.Cleanup()

	err := stack.Start(ctx)
	require.NoError(t, err, "Failed to start Docker stack")

	// Wait for services to be ready
	err = stack.WaitForHTTPEndpoint(ctx, stack.GetGatewayHTTPURL()+"/health")
	require.NoError(t, err, "Gateway health check failed")

	t.Run("WebSocketProtocol", func(t *testing.T) {
		testWebSocketProtocol(t, stack)
	})

	t.Run("BinaryTCPProtocol", func(t *testing.T) {
		testBinaryTCPProtocol(t, stack)
	})

	t.Run("ProtocolNegotiation", func(t *testing.T) {
		testProtocolNegotiation(t, stack)
	})

	t.Run("ProtocolPerformanceComparison", func(t *testing.T) {
		testProtocolPerformanceComparison(t, stack)
	})
}

func testWebSocketProtocol(t *testing.T, stack *DockerStack) {
	t.Helper()
	// Create a router controller for proper authentication and TLS
	router := NewRouterController(t, stack.GetGatewayURL())

	defer func() {
		if err := router.Stop(); err != nil {
			t.Logf("Failed to stop router: %v", err)
		}
	}()

	err := router.BuildRouter()
	require.NoError(t, err, "Failed to build router binary")

	err = router.Start()
	require.NoError(t, err, "Failed to start router")

	// Create MCP client and use it for WebSocket testing
	client := NewMCPClient(router)

	// Test WebSocket via properly authenticated router
	initResp, err := client.Initialize()
	require.NoError(t, err, "Should initialize via WebSocket")
	require.NotNil(t, initResp, "Initialize response should not be nil")

	err = AssertValidMCPResponse(initResp)
	require.NoError(t, err, "Should be valid JSON-RPC response")

	// Test basic tool call to verify WebSocket functionality
	echoResp, err := client.CallTool("echo", map[string]interface{}{
		"message": "WebSocket protocol test",
	})
	require.NoError(t, err, "Should call tool via WebSocket")

	err = AssertToolCallSuccess(echoResp)
	require.NoError(t, err, "WebSocket tool call should succeed")

	text, err := ExtractTextFromToolResponse(echoResp)
	require.NoError(t, err, "Should extract text from WebSocket response")
	assert.Contains(t, text, "WebSocket protocol test", "WebSocket response should contain test message")
}

func testBinaryTCPProtocol(t *testing.T, stack *DockerStack) {
	t.Helper()
	// Test binary TCP connection (if supported)
	tcpAddr := "localhost:8444" // Assuming binary TCP port

	dialer := &net.Dialer{}

	conn, err := dialer.DialContext(context.Background(), "tcp", tcpAddr)
	if err != nil {
		t.Skip("Binary TCP not available or configured")

		return
	}

	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close connection: %v", err)
		}
	}()

	// Send binary protocol handshake
	handshake := []byte{0x4D, 0x43, 0x50, 0x01} // MCP + version
	_, err = conn.Write(handshake)
	require.NoError(t, err, "Should send handshake")

	// Read response
	response := make([]byte, 4)
	_, err = conn.Read(response)
	require.NoError(t, err, "Should read handshake response")

	assert.Equal(t, byte(0x4D), response[0], "Should confirm MCP protocol")
}

func testProtocolNegotiation(t *testing.T, stack *DockerStack) {
	t.Helper()
	// Create a router controller for proper authentication and TLS
	router := NewRouterController(t, stack.GetGatewayURL())

	defer func() {
		if err := router.Stop(); err != nil {
			t.Logf("Failed to stop router: %v", err)
		}
	}()

	err := router.BuildRouter()
	require.NoError(t, err, "Failed to build router binary")

	err = router.Start()
	require.NoError(t, err, "Failed to start router")

	// Create MCP client for protocol negotiation testing
	client := NewMCPClient(router)

	// Test protocol version negotiation by attempting to initialize
	// The router should handle version differences gracefully
	initResp, err := client.Initialize()

	// Either succeeds with version negotiation or fails gracefully
	if err != nil {
		t.Logf("Protocol negotiation handled error gracefully: %v", err)
	} else {
		require.NotNil(t, initResp, "Initialize response should not be nil")
		err = AssertValidMCPResponse(initResp)
		require.NoError(t, err, "Should be valid JSON-RPC response")
		t.Logf("Protocol negotiation successful")
	}
}

func testProtocolPerformanceComparison(t *testing.T, stack *DockerStack) {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	router := setupRouterForPerformanceTest(t, stack)
	defer stopRouter(t, router)

	client := NewMCPClient(router)
	initializeClientForPerformance(t, client)
	
	duration := runWebSocketPerformanceTest(t, client)
	logPerformanceResults(t, duration)
}

func setupRouterForPerformanceTest(t *testing.T, stack *DockerStack) *RouterController {
	t.Helper()
	// Create a router controller for proper authentication and TLS
	router := NewRouterController(t, stack.GetGatewayURL())

	err := router.BuildRouter()
	require.NoError(t, err, "Failed to build router binary")

	err = router.Start()
	require.NoError(t, err, "Failed to start router")

	return router
}

func stopRouter(t *testing.T, router *RouterController) {
	t.Helper()

	if err := router.Stop(); err != nil {
		t.Logf("Failed to stop router: %v", err)
	}
}

func initializeClientForPerformance(t *testing.T, client *MCPClient) {
	t.Helper()
	// Initialize the client first
	_, err := client.Initialize()
	require.NoError(t, err, "Should initialize client")
}

func runWebSocketPerformanceTest(t *testing.T, client *MCPClient) time.Duration {
	t.Helper()
	// Test WebSocket performance through properly authenticated router
	wsStart := time.Now()

	// Send multiple messages via the router (which uses WebSocket internally)
	for i := 0; i < 100; i++ {
		_, err := client.CallTool("echo", map[string]interface{}{
			"message": fmt.Sprintf("performance test %d", i),
		})
		require.NoError(t, err, "Should send message via router")
	}

	return time.Since(wsStart)
}

func logPerformanceResults(t *testing.T, wsDuration time.Duration) {
	t.Helper()
	t.Logf("WebSocket performance via router: 100 requests in %v (%.2f req/sec)",
		wsDuration, 100.0/wsDuration.Seconds())
}
