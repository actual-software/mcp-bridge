package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/actual-software/mcp-bridge/services/router/internal/direct"
	"github.com/actual-software/mcp-bridge/services/router/internal/ratelimit"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// TestMessageRouter_DirectToGatewayFallbackFlow tests the complete fallback flow.
func TestMessageRouter_DirectToGatewayFallbackFlow(t *testing.T) {
	handler := CreateFallbackTestHandler(t)
	handler.ExecuteTest()
}

// TestMessageRouter_FallbackPerformanceComparison compares direct vs gateway performance.
func createPerformanceTestScenarios() []struct {
	name            string
	useDirectMode   bool
	expectedLatency time.Duration
} {
	// Test configuration with smaller values for faster tests.
	directLatency := 1 * time.Millisecond
	gatewayLatency := 5 * time.Millisecond

	return []struct {
		name            string
		useDirectMode   bool
		expectedLatency time.Duration
	}{
		{
			name:            "direct_mode",
			useDirectMode:   true,
			expectedLatency: directLatency,
		},
		{
			name:            "gateway_mode",
			useDirectMode:   false,
			expectedLatency: gatewayLatency,
		},
	}
}

func runPerformanceComparisonTest(t *testing.T, scenario struct {
	name            string
	useDirectMode   bool
	expectedLatency time.Duration
}, logger *zap.Logger) {
	t.Helper()

	cfg := createPerformanceTestConfig(scenario.useDirectMode)
	mockClients := createMockClientsForPerformance()

	var requestCount int64

	mockClients.gwClient.SetSendRequestFunc(func(ctx context.Context, req *mcp.Request) error {
		atomic.AddInt64(&requestCount, 1)
		time.Sleep(5 * time.Millisecond) // Simulate gateway latency

		return nil
	})

	mockClients.directManager.SetGetClientFunc(func(ctx context.Context, serverURL string) (direct.DirectClient, error) {
		return NewTestDirectClient(
			func(ctx context.Context) error { return nil },
			func(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
				atomic.AddInt64(&requestCount, 1)
				time.Sleep(1 * time.Millisecond) // Simulate direct latency

				return &mcp.Response{
					JSONRPC: constants.TestJSONRPCVersion,
					ID:      req.ID,
					Result:  map[string]interface{}{"status": "ok"},
				}, nil
			},
			func(ctx context.Context) error { return nil },
		), nil
	})

	msgRouter := setupMessageRouterForPerformance(cfg, logger, mockClients)
	defer msgRouter.Stop()

	performanceMeasurements := measurePerformance(t, msgRouter, scenario.useDirectMode)

	validatePerformanceResults(t, scenario, performanceMeasurements, atomic.LoadInt64(&requestCount))
}

func createPerformanceTestConfig(useDirectMode bool) *config.Config {
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort)},
			},
		},
		Local: config.LocalConfig{
			RequestTimeoutMs: 5000, // 5 second timeout for performance tests
		},
	}

	if useDirectMode {
		cfg.Direct = direct.DirectConfig{
			MaxConnections: 10, // Enable direct mode
			Fallback: direct.FallbackConfig{
				Enabled: false, // Disable fallback for pure direct test
			},
		}
	}

	return cfg
}

func createMockClientsForPerformance() struct {
	gwClient      *TestGatewayClient
	directManager *TestDirectManager
} {
	mockGwClient := NewTestGatewayClient()
	mockGwClient.SetConnectFunc(func(ctx context.Context) error { return nil })
	mockGwClient.SetIsConnectedFunc(func() bool { return true })

	mockDirectManager := &TestDirectManager{}

	return struct {
		gwClient      *TestGatewayClient
		directManager *TestDirectManager
	}{
		gwClient:      mockGwClient,
		directManager: mockDirectManager,
	}
}

func setupMessageRouterForPerformance(cfg *config.Config, logger *zap.Logger, mockClients struct {
	gwClient      *TestGatewayClient
	directManager *TestDirectManager
}) *MessageRouter {
	stdinChan := make(chan []byte, 10)
	stdoutChan := make(chan []byte, 10)

	connMgr := NewConnectionManager(cfg, logger, mockClients.gwClient)
	metricsCol := NewMetricsCollector(logger)

	// Start connection manager and wait for connection.
	connMgr.Start(context.Background())

	// Wait for connection to be established.
	timeout := time.After(2 * time.Second)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	connected := false
	for !connected {
		select {
		case <-timeout:
			panic("Timeout waiting for connection")
		case <-ticker.C:
			if connMgr.GetState() == StateConnected {
				connected = true
			}
		}
	}

	msgRouter := NewMessageRouter(
		cfg,
		logger,
		mockClients.gwClient,
		mockClients.directManager,
		&ratelimit.NoOpLimiter{},
		stdinChan,
		stdoutChan,
		connMgr,
		metricsCol,
	)

	// Start the message router.
	msgRouter.Start(context.Background())

	return msgRouter
}

func measurePerformance(t *testing.T, msgRouter *MessageRouter, useDirectMode bool) struct {
	totalLatency time.Duration
	duration     time.Duration
	avgLatency   time.Duration
	numRequests  int
} {
	t.Helper()

	numRequests := 10

	var totalLatency time.Duration

	start := time.Now()

	for i := 0; i < numRequests; i++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "tools/call",
			ID:      fmt.Sprintf("perf-test-%d", i),
		}

		reqData, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		requestStart := time.Now()

		err = msgRouter.processStdinMessage(reqData)
		if err != nil {
			t.Errorf("Request %d failed: %v", i, err)

			continue
		}

		totalLatency += time.Since(requestStart)

		// For gateway mode, wait for the response to be processed.
		if !useDirectMode {
			time.Sleep(constants.TestTickInterval) // Give time for async response processing
		}
	}

	duration := time.Since(start)
	avgLatency := totalLatency / time.Duration(numRequests)

	return struct {
		totalLatency time.Duration
		duration     time.Duration
		avgLatency   time.Duration
		numRequests  int
	}{
		totalLatency: totalLatency,
		duration:     duration,
		avgLatency:   avgLatency,
		numRequests:  numRequests,
	}
}

func validatePerformanceResults(t *testing.T, scenario struct {
	name            string
	useDirectMode   bool
	expectedLatency time.Duration
}, measurements struct {
	totalLatency time.Duration
	duration     time.Duration
	avgLatency   time.Duration
	numRequests  int
}, requestCount int64) {
	t.Helper()

	t.Logf("%s performance results:", scenario.name)
	t.Logf("  Total time: %v", measurements.duration)
	t.Logf("  Average latency: %v", measurements.avgLatency)
	t.Logf("  Requests processed: %d", requestCount)
	t.Logf("  Throughput: %.2f req/s", float64(measurements.numRequests)/measurements.duration.Seconds())

	// Basic validation that requests were processed.
	if requestCount == 0 {
		t.Error("No requests were processed")
	}

	// Verify performance characteristics with more lenient bounds.
	if scenario.useDirectMode {
		// Direct mode should be faster than 100ms per request.
		if measurements.avgLatency > 100*time.Millisecond {
			t.Errorf("Direct mode latency %v too high", measurements.avgLatency)
		}
	} else {
		// Gateway mode should be slower but still reasonable.
		if measurements.avgLatency > 200*time.Millisecond {
			t.Errorf("Gateway mode latency %v too high", measurements.avgLatency)
		}
	}
}

func TestMessageRouter_FallbackPerformanceComparison(t *testing.T) {
	logger := zaptest.NewLogger(t)

	scenarios := createPerformanceTestScenarios()

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			runPerformanceComparisonTest(t, scenario, logger)
		})
	}
}

// TestMessageRouter_ConcurrentFallbackStressTest stress tests concurrent fallback operations.
func TestMessageRouter_ConcurrentFallbackStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}
	handler := CreateStressTestHandler(t)
	handler.ExecuteTest()
}

// Mock implementations for testing.

type TestGatewayClient struct {
	mu                  sync.RWMutex
	connectFunc         func(context.Context) error
	sendRequestFunc     func(context.Context, *mcp.Request) error
	receiveResponseFunc func() (*mcp.Response, error)
	isConnectedFunc     func() bool
	sendRequestCalled   int64
	pendingRequests     chan *mcp.Request
	pendingResponses    chan *mcp.Response
}

func NewTestGatewayClient() *TestGatewayClient {
	return &TestGatewayClient{
		pendingRequests:  make(chan *mcp.Request, 100),
		pendingResponses: make(chan *mcp.Response, 100),
	}
}

func (t *TestGatewayClient) Connect(ctx context.Context) error {
	t.mu.RLock()
	fn := t.connectFunc
	t.mu.RUnlock()

	if fn != nil {
		return fn(ctx)
	}

	return nil
}

func (t *TestGatewayClient) SendRequest(ctx context.Context, req *mcp.Request) error {
	atomic.AddInt64(&t.sendRequestCalled, 1)

	t.mu.RLock()
	fn := t.sendRequestFunc
	t.mu.RUnlock()

	if fn != nil {
		if err := fn(ctx, req); err != nil {
			return err
		}
	}

	// Store the request for response correlation.
	select {
	case t.pendingRequests <- req:
		// Create and queue a response.
		resp := &mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			ID:      req.ID,
			Result:  map[string]interface{}{"source": "gateway", "method": req.Method},
		}

		go func() {
			// Small delay to simulate network latency.
			time.Sleep(5 * time.Millisecond)

			select {
			case t.pendingResponses <- resp:
			default:
				// Channel full, drop response.
			}
		}()
	default:
		return errors.New("request queue full")
	}

	return nil
}

func (t *TestGatewayClient) ReceiveResponse() (*mcp.Response, error) {
	t.mu.RLock()
	fn := t.receiveResponseFunc
	t.mu.RUnlock()

	if fn != nil {
		return fn()
	}

	// Wait for a response with a short timeout to not block the router.
	select {
	case resp := <-t.pendingResponses:
		return resp, nil
	case <-time.After(50 * time.Millisecond):
		// Return a timeout error that the router expects.
		return nil, errors.New("timeout waiting for response")
	}
}

func (t *TestGatewayClient) SendPing() error { return nil }
func (t *TestGatewayClient) IsConnected() bool {
	t.mu.RLock()
	fn := t.isConnectedFunc
	t.mu.RUnlock()

	if fn != nil {
		return fn()
	}

	return true
}
func (t *TestGatewayClient) Close() error { return nil }

// SetConnectFunc safely sets the connect function for testing.
func (t *TestGatewayClient) SetConnectFunc(fn func(context.Context) error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.connectFunc = fn
}

// SetSendRequestFunc safely sets the send request function for testing.
func (t *TestGatewayClient) SetSendRequestFunc(fn func(context.Context, *mcp.Request) error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sendRequestFunc = fn
}

// SetReceiveResponseFunc safely sets the receive response function for testing.
func (t *TestGatewayClient) SetReceiveResponseFunc(fn func() (*mcp.Response, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.receiveResponseFunc = fn
}

// SetIsConnectedFunc safely sets the is connected function for testing.
func (t *TestGatewayClient) SetIsConnectedFunc(fn func() bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.isConnectedFunc = fn
}

type TestDirectManager struct {
	mu              sync.RWMutex
	getClientFunc   func(context.Context, string) (direct.DirectClient, error)
	getClientCalled int64
}

func (t *TestDirectManager) Start(ctx context.Context) error { return nil }
func (t *TestDirectManager) Stop(ctx context.Context) error  { return nil }

//nolint:ireturn // Test helper requires interface return
func (t *TestDirectManager) GetClient(ctx context.Context, serverURL string) (direct.DirectClient, error) {
	atomic.AddInt64(&t.getClientCalled, 1)

	t.mu.RLock()
	fn := t.getClientFunc
	t.mu.RUnlock()

	if fn != nil {
		return fn(ctx, serverURL)
	}

	return nil, errors.New("no client available")
}

// SetGetClientFunc safely sets the get client function for testing.
func (t *TestDirectManager) SetGetClientFunc(fn func(context.Context, string) (direct.DirectClient, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.getClientFunc = fn
}

type TestDirectClient struct {
	mu              sync.RWMutex
	connectFunc     func(context.Context) error
	sendRequestFunc func(context.Context, *mcp.Request) (*mcp.Response, error)
	closeFunc       func(context.Context) error
}

// NewTestDirectClient creates a new TestDirectClient with the given functions.
func NewTestDirectClient(
	connectFunc func(context.Context) error,
	sendRequestFunc func(context.Context, *mcp.Request) (*mcp.Response, error),
	closeFunc func(context.Context) error,
) *TestDirectClient {
	return &TestDirectClient{
		connectFunc:     connectFunc,
		sendRequestFunc: sendRequestFunc,
		closeFunc:       closeFunc,
	}
}

func (t *TestDirectClient) Connect(ctx context.Context) error {
	t.mu.RLock()
	fn := t.connectFunc
	t.mu.RUnlock()

	if fn != nil {
		return fn(ctx)
	}

	return nil
}

func (t *TestDirectClient) SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	t.mu.RLock()
	fn := t.sendRequestFunc
	t.mu.RUnlock()

	if fn != nil {
		return fn(ctx, req)
	}

	return &mcp.Response{JSONRPC: constants.TestJSONRPCVersion, ID: req.ID}, nil
}

func (t *TestDirectClient) Health(ctx context.Context) error { return nil }

func (t *TestDirectClient) Close(ctx context.Context) error {
	t.mu.RLock()
	fn := t.closeFunc
	t.mu.RUnlock()

	if fn != nil {
		return fn(ctx)
	}

	return nil
}

func (t *TestDirectClient) GetName() string                  { return "test-direct-client" }
func (t *TestDirectClient) GetProtocol() string              { return "test" }
func (t *TestDirectClient) GetMetrics() direct.ClientMetrics { return direct.ClientMetrics{} }
