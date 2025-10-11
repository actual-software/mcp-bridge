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

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/actual-software/mcp-bridge/services/router/internal/direct"
	"github.com/actual-software/mcp-bridge/services/router/internal/ratelimit"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap/zaptest"
)

// StressTestHandler handles concurrent fallback stress testing.
type StressTestHandler struct {
	t                    *testing.T
	config               *config.Config
	mockGwClient         *TestGatewayClient
	mockDirectManager    *TestDirectManager
	msgRouter            *MessageRouter
	stdinChan            chan []byte
	stdoutChan           chan []byte
	numGoroutines        int
	requestsPerGoroutine int
	metrics              *StressTestMetrics
}

// StressTestMetrics tracks stress test metrics.
type StressTestMetrics struct {
	directAttempts  int64
	gatewayAttempts int64
	directFailures  int64
	successCount    int64
	errorCount      int64
}

// CreateStressTestHandler creates a new stress test handler.
func CreateStressTestHandler(t *testing.T) *StressTestHandler {
	t.Helper()

	return &StressTestHandler{
		t:                    t,
		numGoroutines:        10,
		requestsPerGoroutine: 5,
		stdinChan:            make(chan []byte, 1000),
		stdoutChan:           make(chan []byte, 1000),
		metrics:              &StressTestMetrics{},
	}
}

// ExecuteTest runs the complete stress test scenario.
func (h *StressTestHandler) ExecuteTest() {
	h.setupConfiguration()
	h.setupMockComponents()
	h.initializeRouter()

	h.startServices()
	defer h.stopServices()

	h.waitForConnection()

	startTime := h.runConcurrentRequests()
	duration := time.Since(startTime)

	h.waitForProcessing()
	h.validateResults(duration)
}

// setupConfiguration creates the test configuration.
func (h *StressTestHandler) setupConfiguration() {
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
				MaxRetries:    1,
				RetryDelay:    5 * time.Millisecond,
				DirectTimeout: 10 * time.Millisecond,
			},
		},
		Local: config.LocalConfig{
			RequestTimeoutMs: 5000,
		},
	}
}

// setupMockComponents creates mock clients.
func (h *StressTestHandler) setupMockComponents() {
	h.mockGwClient = h.createMockGatewayClient()
	h.mockDirectManager = h.createMockDirectManager()
}

// createMockGatewayClient creates a gateway client that always succeeds.
func (h *StressTestHandler) createMockGatewayClient() *TestGatewayClient {
	client := NewTestGatewayClient()
	client.connectFunc = func(ctx context.Context) error { return nil }
	client.sendRequestFunc = func(ctx context.Context, req *mcp.Request) error {
		atomic.AddInt64(&h.metrics.gatewayAttempts, 1)

		return nil
	}
	client.isConnectedFunc = func() bool { return true }

	return client
}

// createMockDirectManager creates a direct manager that fails 50% of requests.
func (h *StressTestHandler) createMockDirectManager() *TestDirectManager {
	return &TestDirectManager{
		getClientFunc: func(ctx context.Context, serverURL string) (direct.DirectClient, error) {
			return &TestDirectClient{
				connectFunc:     func(ctx context.Context) error { return nil },
				sendRequestFunc: h.createDirectRequestHandler(),
				closeFunc:       func(ctx context.Context) error { return nil },
			}, nil
		},
	}
}

// createDirectRequestHandler creates the request handler for direct client.
func (h *StressTestHandler) createDirectRequestHandler() func(context.Context, *mcp.Request) (*mcp.Response, error) {
	return func(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
		attempts := atomic.AddInt64(&h.metrics.directAttempts, 1)

		// Fail 50% of direct attempts.
		if attempts%2 == 0 {
			atomic.AddInt64(&h.metrics.directFailures, 1)

			return nil, errors.New("simulated direct failure")
		}

		return &mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			ID:      req.ID,
			Result:  map[string]interface{}{"source": "direct"},
		}, nil
	}
}

// initializeRouter creates the router and related components.
func (h *StressTestHandler) initializeRouter() {
	logger := zaptest.NewLogger(h.t)
	connMgr := NewConnectionManager(h.config, logger, h.mockGwClient)
	metricsCol := NewMetricsCollector(logger)

	h.msgRouter = NewMessageRouter(
		h.config,
		logger,
		h.mockGwClient,
		h.mockDirectManager,
		&ratelimit.NoOpLimiter{},
		h.stdinChan,
		h.stdoutChan,
		connMgr,
		metricsCol,
	)
}

// startServices starts the router services.
func (h *StressTestHandler) startServices() {
	ctx := context.Background()
	connMgr := h.msgRouter.connMgr
	connMgr.Start(ctx)
	h.msgRouter.Start(ctx)
}

// stopServices stops the router services.
func (h *StressTestHandler) stopServices() {
	h.msgRouter.Stop()
	h.msgRouter.connMgr.Stop()
}

// waitForConnection waits for connection to be established.
func (h *StressTestHandler) waitForConnection() {
	waiter := &ConnectionWaiter{
		connMgr: h.msgRouter.connMgr,
		timeout: 2 * time.Second,
	}

	if err := waiter.WaitForState(StateConnected); err != nil {
		h.t.Fatal("Timeout waiting for connection")
	}
}

// runConcurrentRequests executes concurrent request goroutines.
func (h *StressTestHandler) runConcurrentRequests() time.Time {
	runner := &ConcurrentRequestRunner{
		handler:              h,
		numGoroutines:        h.numGoroutines,
		requestsPerGoroutine: h.requestsPerGoroutine,
	}

	return runner.Run()
}

// waitForProcessing allows async processing to complete.
func (h *StressTestHandler) waitForProcessing() {
	time.Sleep(100 * time.Millisecond)
}

// validateResults validates the stress test results.
func (h *StressTestHandler) validateResults(duration time.Duration) {
	validator := &StressTestValidator{
		t:             h.t,
		metrics:       h.metrics,
		totalRequests: int64(h.numGoroutines * h.requestsPerGoroutine),
		duration:      duration,
		metricsCol:    h.msgRouter.metricsCol,
	}
	validator.Validate()
}

// ConcurrentRequestRunner handles concurrent request execution.
type ConcurrentRequestRunner struct {
	handler              *StressTestHandler
	numGoroutines        int
	requestsPerGoroutine int
}

// Run executes all concurrent requests.
func (r *ConcurrentRequestRunner) Run() time.Time {
	var wg sync.WaitGroup

	startTime := time.Now()

	for i := 0; i < r.numGoroutines; i++ {
		wg.Add(1)

		go r.runGoroutine(&wg, i)
	}

	wg.Wait()

	return startTime
}

// runGoroutine executes requests for a single goroutine.
func (r *ConcurrentRequestRunner) runGoroutine(wg *sync.WaitGroup, goroutineID int) {
	defer wg.Done()

	for j := 0; j < r.requestsPerGoroutine; j++ {
		r.sendRequest(goroutineID, j)
	}
}

// sendRequest sends a single request.
func (r *ConcurrentRequestRunner) sendRequest(goroutineID, requestID int) {
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "tools/call",
		ID:      fmt.Sprintf("stress-%d-%d", goroutineID, requestID),
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		atomic.AddInt64(&r.handler.metrics.errorCount, 1)

		return
	}

	err = r.handler.msgRouter.processStdinMessage(reqData)
	if err != nil {
		atomic.AddInt64(&r.handler.metrics.errorCount, 1)
	} else {
		atomic.AddInt64(&r.handler.metrics.successCount, 1)
	}
}

// StressTestValidator validates stress test results.
type StressTestValidator struct {
	t             *testing.T
	metrics       *StressTestMetrics
	totalRequests int64
	duration      time.Duration
	metricsCol    *MetricsCollector
}

// Validate performs all validations.
func (v *StressTestValidator) Validate() {
	v.logResults()
	v.validateSuccessRate()
	v.validateGatewayFallback()
	v.validateThroughput()
}

// logResults logs the test results.
func (v *StressTestValidator) logResults() {
	throughput := float64(v.totalRequests) / v.duration.Seconds()

	v.t.Logf("Concurrent fallback stress test results:")
	v.t.Logf("  Total requests: %d", v.totalRequests)
	v.t.Logf("  Successful requests: %d", atomic.LoadInt64(&v.metrics.successCount))
	v.t.Logf("  Failed requests: %d", atomic.LoadInt64(&v.metrics.errorCount))
	v.t.Logf("  Duration: %v", v.duration)
	v.t.Logf("  Throughput: %.2f req/s", throughput)
	v.t.Logf("  Direct attempts: %d", atomic.LoadInt64(&v.metrics.directAttempts))
	v.t.Logf("  Direct failures: %d", atomic.LoadInt64(&v.metrics.directFailures))
	v.t.Logf("  Gateway attempts: %d", atomic.LoadInt64(&v.metrics.gatewayAttempts))
	v.t.Logf("  Fallback successes: %d", v.metricsCol.GetMetrics().FallbackSuccesses)
}

// validateSuccessRate checks if success rate is acceptable.
func (v *StressTestValidator) validateSuccessRate() {
	successCount := atomic.LoadInt64(&v.metrics.successCount)
	if successCount == 0 {
		v.t.Error("Expected some successful requests")
	}

	successRate := float64(successCount) / float64(v.totalRequests)
	if successRate < 0.8 {
		v.t.Errorf("Success rate %.2f%% too low for stress test", successRate*100)
	}
}

// validateGatewayFallback checks if gateway fallback occurred.
func (v *StressTestValidator) validateGatewayFallback() {
	if atomic.LoadInt64(&v.metrics.gatewayAttempts) == 0 {
		v.t.Error("Expected some gateway attempts due to fallback")
	}
}

// validateThroughput checks if throughput meets minimum requirements.
func (v *StressTestValidator) validateThroughput() {
	throughput := float64(v.totalRequests) / v.duration.Seconds()
	minThroughput := 50.0

	if throughput < minThroughput {
		v.t.Errorf("Throughput %.2f req/s below minimum %.2f req/s", throughput, minThroughput)
	}
}
