package router

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/gateway"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"github.com/poiley/mcp-bridge/test/testutil"
)

const (
	httpStatusInternalError = 500
)

// TestRouterIntegration_ConnectionEstablishment tests the router's ability to establish.
// WebSocket connections to the gateway.
func TestRouterIntegration_ConnectionEstablishment(t *testing.T) { 
	// Create mock gateway.
	gateway := newMockGateway(t, func(conn *websocket.Conn) {
		// Simply accept connection and close.
	})
	defer gateway.Close()

	// Create router.
	router := createTestRouter(t, gateway.URL)

	// Test connection establishment.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	errChan := make(chan error, 1)

	go func() {
		errChan <- router.Run(ctx)
	}()

	// Wait for connection.
	assert.Eventually(t, func() bool {
		return router.GetState() == StateConnected
	}, httpStatusInternalError*time.Millisecond, 10*time.Millisecond, "Router should connect to gateway")

	// Cleanup.
	cancel()
	<-errChan
}

// TestRouterIntegration_MessageFlow tests the complete request/response flow.
// through the router.
func TestRouterIntegration_MessageFlow(t *testing.T) { 
	responseReceived := make(chan mcp.Response, 1)
	done := make(chan struct{})

	router, ctx, cancel := setupIntegrationTest(t)
	defer cancel()

	go func() { _ = router.Run(ctx) }()
	waitForConnection(t, router)
	sendInitializationRequest(t, router)

	testRequest := createTestRequest()
	monitorForResponse(t, router, responseReceived, done)

	// Send request.
	requestData, _ := json.Marshal(testRequest)
	router.GetStdinChan() <- requestData

	verifyIntegrationResponse(t, testRequest, responseReceived)

	// Wait for goroutine to complete to avoid race condition.
	<-done
}

func setupIntegrationTest(t *testing.T) (*LocalRouter, context.Context, context.CancelFunc) {
	t.Helper()
	
	gateway := createMockGatewayWithHandler(t)
	t.Cleanup(func() { gateway.Close() })

	// Setup router with longer-lived context
	router := createTestRouter(t, gateway.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	
	return router, ctx, cancel
}

func createTestRequest() mcp.Request {
	return mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test/echo",
		Params:  map[string]string{"data": "hello"},
		ID:      "test-1",
	}
}

func monitorForResponse(t *testing.T, router *LocalRouter, responseReceived chan mcp.Response, done chan struct{}) {
	t.Helper()
	
	// Monitor for response with proper synchronization.
	responseCtx, responseCancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(responseCancel)

	go func() {
		defer close(done)

		select {
		case responseData := <-router.GetStdoutChan():
			var response mcp.Response

			if err := json.Unmarshal(responseData, &response); err != nil {
				return
			}

			select {
			case responseReceived <- response:
			case <-responseCtx.Done():
				return
			}
		case <-responseCtx.Done():
			return
		}
	}()
}

func verifyIntegrationResponse(t *testing.T, testRequest mcp.Request, responseReceived chan mcp.Response) {
	t.Helper()
	
	// Verify response.
	select {
	case response := <-responseReceived:
		assert.Equal(t, testRequest.ID, response.ID)
		assert.Equal(t, "2.0", response.JSONRPC)
		assert.Nil(t, response.Error)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for response")
	}
}

// createMockGatewayWithHandler creates a mock gateway that processes MCP messages.
func createMockGatewayWithHandler(t *testing.T) *httptest.Server {
	t.Helper()

	return newMockGateway(t, func(conn *websocket.Conn) {
		handleMockGatewayMessages(t, conn)
	})
}

// handleMockGatewayMessages processes incoming messages from the router.
func handleMockGatewayMessages(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	for {
		var wireMsg gateway.WireMessage
		if err := conn.ReadJSON(&wireMsg); err != nil {
			return
		}

		response := processMockGatewayMessage(t, wireMsg)
		if response != nil {
			sendMockGatewayResponse(t, conn, wireMsg, *response)
		}
	}
}

// processMockGatewayMessage processes a single message and returns appropriate response.
func processMockGatewayMessage(t *testing.T, wireMsg gateway.WireMessage) *mcp.Response {
	t.Helper()

	payloadBytes, _ := json.Marshal(wireMsg.MCPPayload)

	var req mcp.Request

	if err := json.Unmarshal(payloadBytes, &req); err != nil {
		return nil
	}

	if req.Method == "initialize" {
		response := createInitializeResponse(req.ID)

		return &response
	} else {
		response := createEchoResponse(req)

		return &response
	}
}

// sendMockGatewayResponse sends a response back to the router.
func sendMockGatewayResponse(t *testing.T, conn *websocket.Conn, wireMsg gateway.WireMessage, response mcp.Response) {
	t.Helper()

	wireResponse := gateway.WireMessage{
		ID:         wireMsg.ID,
		Timestamp:  wireMsg.Timestamp,
		Source:     "mock-gateway",
		MCPPayload: response,
	}

	if err := conn.WriteJSON(wireResponse); err != nil {
		t.Logf("Failed to write JSON response: %v", err)
	}
}


// sendInitializationRequest sends initialization request and waits for response.
func sendInitializationRequest(t *testing.T, router *LocalRouter) {
	t.Helper()

	initRequest := mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": MCPProtocolVersion,
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
		ID: "init-1",
	}

	initData, _ := json.Marshal(initRequest)
	router.GetStdinChan() <- initData

	waitForInitializationResponse(t, router)
}

// TestRouterIntegration_GracefulShutdown tests that the router shuts down cleanly.
// when the context is canceled.
func TestRouterIntegration_GracefulShutdown(t *testing.T) { 
	// Create mock gateway.
	gateway := newMockGateway(t, func(conn *websocket.Conn) {
		// Keep connection alive until closed.
		for {
			var msg json.RawMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
		}
	})
	defer gateway.Close()

	// Create router.
	router := createTestRouter(t, gateway.URL)

	// Start router.
	ctx, cancel := context.WithCancel(context.Background())

	errChan := make(chan error, 1)

	go func() {
		errChan <- router.Run(ctx)
	}()

	// Wait for connection.
	waitForConnection(t, router)

	// Trigger shutdown.
	cancel()

	// Verify clean shutdown.
	select {
	case err := <-errChan:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Unexpected error during shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for shutdown")
	}
}

// Helper functions.

func newMockGateway(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Failed to upgrade WebSocket: %v", err)

			return
		}

		defer func() { _ = conn.Close() }()

		handler(conn)
	}))
}

func createTestRouter(t *testing.T, gatewayURL string) *LocalRouter {
	t.Helper()

	wsURL := "ws" + gatewayURL[4:] // Convert http:// to ws://

	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL:  wsURL,
					Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
				},
			},
		},
		Local: config.LocalConfig{
			RequestTimeoutMs: 5000,
			RateLimit: common.RateLimitConfig{
				Enabled: false,
			},
		},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	return router
}

func waitForConnection(t *testing.T, router *LocalRouter) {
	t.Helper()

	assert.Eventually(t, func() bool {
		return router.GetState() == StateConnected
	}, httpStatusInternalError*time.Millisecond, 10*time.Millisecond, "Router should connect")
}

func waitForInitializationResponse(t *testing.T, router *LocalRouter) {
	t.Helper()

	// Wait for initialization response.
	select {
	case responseData := <-router.GetStdoutChan():
		var response mcp.Response
		if err := json.Unmarshal(responseData, &response); err != nil {
			t.Fatalf("Failed to unmarshal initialization response: %v", err)
		}

		if response.ID != "init-1" {
			t.Fatalf("Unexpected response ID: got %v, want init-1", response.ID)
		}

		if response.Error != nil {
			t.Fatalf("Initialization failed: %v", response.Error)
		}

		return // Success
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for initialization response")
	}
}

func createInitializeResponse(id interface{}) mcp.Response {
	return mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		Result: mcp.InitializeResult{
			ProtocolVersion: MCPProtocolVersion,
			Capabilities: mcp.Capabilities{
				Tools:     &mcp.ToolsCapability{},
				Resources: true,
				Prompts:   true,
			},
			ServerInfo: mcp.ServerInfo{
				Name:    "mock-gateway",
				Version: "1.0.0",
			},
		},
		ID: id,
	}
}

func createEchoResponse(req mcp.Request) mcp.Response {
	return mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		Result: map[string]interface{}{
			"echo":   req.Params,
			"method": req.Method,
		},
		ID: req.ID,
	}
}
