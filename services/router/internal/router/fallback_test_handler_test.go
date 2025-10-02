package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/actual-software/mcp-bridge/services/router/internal/direct"
	"github.com/actual-software/mcp-bridge/services/router/internal/ratelimit"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// FallbackTestHandler handles direct to gateway fallback testing.
type FallbackTestHandler struct {
	t             *testing.T
	logger        *zap.Logger
	config        *config.Config
	mockGwClient  *TestGatewayClient
	mockDirectMgr *TestDirectManager
	stdinChan     chan []byte
	stdoutChan    chan []byte
	connMgr       *ConnectionManager
	metricsCol    *MetricsCollector
	msgRouter     *MessageRouter
}

// CreateFallbackTestHandler creates a new fallback test handler.
func CreateFallbackTestHandler(t *testing.T) *FallbackTestHandler {
	t.Helper()

	return &FallbackTestHandler{
		t:          t,
		logger:     zaptest.NewLogger(t),
		stdinChan:  make(chan []byte, 10),
		stdoutChan: make(chan []byte, 10),
	}
}

// ExecuteTest runs the complete fallback test scenario.
func (h *FallbackTestHandler) ExecuteTest() {
	h.initializeConfig()
	h.setupMockComponents()
	h.initializeRouter()

	h.startServices()
	defer h.stopServices()

	h.waitForConnection()
	h.testFallbackRequest()
	h.validateResults()
}

// initializeConfig sets up the test configuration.
func (h *FallbackTestHandler) initializeConfig() {
	h.config = &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort)},
			},
		},
		Direct: direct.DirectConfig{
			MaxConnections: 10,
			Fallback: direct.FallbackConfig{
				Enabled:       true,
				MaxRetries:    2,
				RetryDelay:    10 * time.Millisecond,
				DirectTimeout: 50 * time.Millisecond,
			},
		},
		Local: config.LocalConfig{
			RequestTimeoutMs: 1000,
		},
	}
}

// setupMockComponents configures mock gateway and direct clients.
func (h *FallbackTestHandler) setupMockComponents() {
	h.mockGwClient = h.createMockGatewayClient()
	h.mockDirectMgr = h.createMockDirectManager()
}

// createMockGatewayClient creates a mock gateway client that always succeeds.
func (h *FallbackTestHandler) createMockGatewayClient() *TestGatewayClient {
	client := NewTestGatewayClient()
	client.connectFunc = func(ctx context.Context) error { return nil }
	client.sendRequestFunc = func(ctx context.Context, req *mcp.Request) error { return nil }
	client.isConnectedFunc = func() bool { return true }

	return client
}

// createMockDirectManager creates a mock direct manager that always fails.
func (h *FallbackTestHandler) createMockDirectManager() *TestDirectManager {
	return &TestDirectManager{
		getClientFunc: func(ctx context.Context, serverURL string) (direct.DirectClient, error) {
			return &TestDirectClient{
				connectFunc: func(ctx context.Context) error { return nil },
				sendRequestFunc: func(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
					return nil, errors.New("direct connection failed")
				},
				closeFunc: func(ctx context.Context) error { return nil },
			}, nil
		},
	}
}

// initializeRouter creates the router components.
func (h *FallbackTestHandler) initializeRouter() {
	h.connMgr = NewConnectionManager(h.config, h.logger, h.mockGwClient)
	h.metricsCol = NewMetricsCollector(h.logger)

	h.msgRouter = NewMessageRouter(
		h.config,
		h.logger,
		h.mockGwClient,
		h.mockDirectMgr,
		&ratelimit.NoOpLimiter{},
		h.stdinChan,
		h.stdoutChan,
		h.connMgr,
		h.metricsCol,
	)
}

// startServices starts the connection manager and message router.
func (h *FallbackTestHandler) startServices() {
	ctx := context.Background()
	h.connMgr.Start(ctx)
	h.msgRouter.Start(ctx)
}

// stopServices stops the connection manager and message router.
func (h *FallbackTestHandler) stopServices() {
	h.msgRouter.Stop()
	h.connMgr.Stop()
}

// waitForConnection waits for the connection to be established.
func (h *FallbackTestHandler) waitForConnection() {
	waiter := &ConnectionWaiter{
		connMgr: h.connMgr,
		timeout: 2 * time.Second,
	}

	if err := waiter.WaitForState(StateConnected); err != nil {
		h.t.Fatal("Timeout waiting for connection")
	}
}

// testFallbackRequest sends a request that should trigger fallback.
func (h *FallbackTestHandler) testFallbackRequest() {
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "tools/call",
		Params:  map[string]interface{}{"test": "fallback"},
		ID:      "fallback-test-1",
	}

	h.sendRequest(req)
	h.verifyResponse(req)
}

// sendRequest marshals and sends a request.
func (h *FallbackTestHandler) sendRequest(req *mcp.Request) {
	reqData, err := json.Marshal(req)
	if err != nil {
		h.t.Fatalf("Failed to marshal request: %v", err)
	}

	err = h.msgRouter.processStdinMessage(reqData)
	if err != nil {
		h.t.Errorf("Expected success with fallback, got error: %v", err)
	}

	// Allow async processing.
	time.Sleep(100 * time.Millisecond)
}

// verifyResponse checks that the correct response was received.
func (h *FallbackTestHandler) verifyResponse(req *mcp.Request) {
	select {
	case respData := <-h.stdoutChan:
		h.validateResponseData(respData, req)
	case <-time.After(500 * time.Millisecond):
		h.t.Error("No response received within timeout")
	}
}

// validateResponseData validates the response content.
func (h *FallbackTestHandler) validateResponseData(respData []byte, req *mcp.Request) {
	var resp mcp.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		h.t.Errorf("Failed to unmarshal response: %v", err)

		return
	}

	if resp.ID != req.ID {
		h.t.Errorf("Response ID mismatch: expected %v, got %v", req.ID, resp.ID)
	}

	if resp.Result == nil {
		h.t.Error("Expected response result, got nil")
	}
}

// validateResults checks that fallback was properly executed.
func (h *FallbackTestHandler) validateResults() {
	validator := &FallbackValidator{
		t:             h.t,
		directManager: h.mockDirectMgr,
		gatewayClient: h.mockGwClient,
		metricsCol:    h.metricsCol,
	}
	validator.Validate()
}

// ConnectionWaiter helps wait for connection states.
type ConnectionWaiter struct {
	connMgr *ConnectionManager
	timeout time.Duration
}

// WaitForState waits for a specific connection state.
func (w *ConnectionWaiter) WaitForState(targetState ConnectionState) error {
	timeout := time.After(w.timeout)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return errors.New("timeout waiting for connection state")
		case <-ticker.C:
			if w.connMgr.GetState() == targetState {
				return nil
			}
		}
	}
}

// FallbackValidator validates fallback test results.
type FallbackValidator struct {
	t             *testing.T
	directManager *TestDirectManager
	gatewayClient *TestGatewayClient
	metricsCol    *MetricsCollector
}

// Validate checks all fallback expectations.
func (v *FallbackValidator) Validate() {
	v.validateDirectAttempt()
	v.validateGatewayFallback()
	v.validateMetrics()
}

// validateDirectAttempt verifies direct manager was called.
func (v *FallbackValidator) validateDirectAttempt() {
	if v.directManager.getClientCalled == 0 {
		v.t.Error("Expected direct manager to be called")
	}
}

// validateGatewayFallback verifies gateway was used as fallback.
func (v *FallbackValidator) validateGatewayFallback() {
	if v.gatewayClient.sendRequestCalled == 0 {
		v.t.Error("Expected gateway client to be called for fallback")
	}
}

// validateMetrics verifies fallback metrics were recorded.
func (v *FallbackValidator) validateMetrics() {
	if v.metricsCol.GetMetrics().FallbackSuccesses == 0 {
		v.t.Error("Expected fallback success metric to be incremented")
	}
}
