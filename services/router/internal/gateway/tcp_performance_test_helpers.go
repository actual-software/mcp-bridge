package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

const (
	// PerformanceTestTimeoutMs is the timeout for performance test connections.
	PerformanceTestTimeoutMs = 10000
)

// TCPPerformanceTestEnvironment manages TCP performance testing.
type TCPPerformanceTestEnvironment struct {
	t               *testing.T
	logger          *zap.Logger
	serverAddr      string
	serverStop      func()
	client          *TCPClient
	totalRequests   *int64
	totalLatency    *time.Duration
	latencyMu       *sync.Mutex
	connectionCount *int64
}

// EstablishTCPPerformanceTest creates a performance test environment.
func EstablishTCPPerformanceTest(t *testing.T, logger *zap.Logger) *TCPPerformanceTestEnvironment {
	t.Helper()

	totalRequests := int64(0)
	totalLatency := time.Duration(0)
	latencyMu := &sync.Mutex{}
	connectionCount := int64(0)

	return &TCPPerformanceTestEnvironment{
		t:               t,
		logger:          logger,
		totalRequests:   &totalRequests,
		totalLatency:    &totalLatency,
		latencyMu:       latencyMu,
		connectionCount: &connectionCount,
	}
}

// SetupPerformanceServer creates a mock server for performance testing.
// This function should be called by the test to setup the server externally.
func (env *TCPPerformanceTestEnvironment) SetServerInfo(addr string, stopFunc func()) {
	env.serverAddr = addr
	env.serverStop = stopFunc
}

// CreatePerformanceHandler creates the server handler for performance testing.
func (env *TCPPerformanceTestEnvironment) CreatePerformanceHandler() func(net.Conn) {
	return func(conn net.Conn) {
		atomic.AddInt64(env.connectionCount, 1)

		reader := bufio.NewReader(conn)

		// Handle version negotiation.
		if err := env.handleVersionNegotiation(reader, conn); err != nil {
			return
		}

		// Handle requests.
		env.handlePerformanceRequests(reader, conn)
	}
}

// handleVersionNegotiation handles the initial version negotiation.
func (env *TCPPerformanceTestEnvironment) handleVersionNegotiation(reader *bufio.Reader, conn net.Conn) error {
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
func (env *TCPPerformanceTestEnvironment) sendVersionAck(conn net.Conn) error {
	ackPayload := map[string]interface{}{"agreed_version": 1}
	ackData, _ := json.Marshal(ackPayload)
	ackFrame := &BinaryFrame{
		Version:     1,
		MessageType: MessageTypeVersionAck,
		Payload:     ackData,
	}

	return ackFrame.Write(conn)
}

// handlePerformanceRequests processes requests and tracks performance.
func (env *TCPPerformanceTestEnvironment) handlePerformanceRequests(reader *bufio.Reader, conn net.Conn) {
	for {
		startTime := time.Now()

		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			return
		}

		if frame.MessageType == MessageTypeRequest {
			env.processPerformanceRequest(frame, conn, startTime)
		}
	}
}

// processPerformanceRequest handles a single performance request.
func (env *TCPPerformanceTestEnvironment) processPerformanceRequest(
	frame *BinaryFrame,
	conn net.Conn,
	startTime time.Time,
) {
	atomic.AddInt64(env.totalRequests, 1)

	// Parse and process request.
	wireMsg, req := env.parseRequest(frame)

	// Create and send response.
	env.sendPerformanceResponse(conn, frame, wireMsg, req, startTime)

	// Track latency.
	env.trackLatency(startTime)
}

// parseRequest parses the incoming request.
func (env *TCPPerformanceTestEnvironment) parseRequest(frame *BinaryFrame) (wireMsg struct {
	ID         interface{}     `json:"id"`
	MCPPayload json.RawMessage `json:"mcp_payload"`
}, req mcp.Request) {
	if err := json.Unmarshal(frame.Payload, &wireMsg); err != nil {
		// Return empty structs on error.
		return wireMsg, req
	}

	if err := json.Unmarshal(wireMsg.MCPPayload, &req); err != nil {
		// Return empty request on error.
		return wireMsg, req
	}

	return
}

// sendPerformanceResponse sends a response with timing information.
func (env *TCPPerformanceTestEnvironment) sendPerformanceResponse(conn net.Conn, frame *BinaryFrame, wireMsg struct {
	ID         interface{}     `json:"id"`
	MCPPayload json.RawMessage `json:"mcp_payload"`
}, req mcp.Request, startTime time.Time) {
	processingTime := time.Since(startTime)

	resp := mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		ID:      req.ID,
		Result: map[string]interface{}{
			"method":        req.Method,
			"processing_ns": processingTime.Nanoseconds(),
		},
	}

	respWire := struct {
		ID         interface{} `json:"id"`
		MCPPayload interface{} `json:"mcp_payload"`
	}{
		ID:         wireMsg.ID,
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

// trackLatency records request latency.
func (env *TCPPerformanceTestEnvironment) trackLatency(startTime time.Time) {
	totalTime := time.Since(startTime)

	env.latencyMu.Lock()
	*env.totalLatency += totalTime
	env.latencyMu.Unlock()
}

// CreateClient creates and connects a TCP client.
func (env *TCPPerformanceTestEnvironment) CreateClient(addr string) {
	cfg := config.GatewayConfig{
		URL: "tcp://" + addr,
		Connection: common.ConnectionConfig{
			TimeoutMs: PerformanceTestTimeoutMs,
		},
	}

	client, err := NewTCPClient(cfg, "", env.logger)
	if err != nil {
		env.t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		env.t.Fatalf("Failed to connect: %v", err)
	}

	env.client = client
}

// PerformanceTestParameters holds test parameters.
type PerformanceTestParameters struct {
	NumRequests   int
	MaxLatency    time.Duration
	MinThroughput float64
}

// ExecutePerformanceTest runs the performance test.
func (env *TCPPerformanceTestEnvironment) ExecutePerformanceTest(params PerformanceTestParameters) {
	start := time.Now()

	// Send requests.
	for i := 0; i < params.NumRequests; i++ {
		env.sendAndReceiveRequest(i, params.MaxLatency)
	}

	totalTestTime := time.Since(start)

	// Validate performance metrics.
	env.validatePerformanceMetrics(params, totalTestTime)

	// Report metrics.
	env.reportPerformanceMetrics(params.NumRequests, totalTestTime)
}

// sendAndReceiveRequest sends a request and validates response.
func (env *TCPPerformanceTestEnvironment) sendAndReceiveRequest(index int, maxLatency time.Duration) {
	ctx := context.Background()
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  fmt.Sprintf("perf_test_%d", index),
		ID:      fmt.Sprintf("perf-%d", index),
	}

	requestStart := time.Now()

	if err := env.client.SendRequest(ctx, req); err != nil {
		env.t.Fatalf("Failed to send request %d: %v", index, err)
	}

	resp, err := env.client.ReceiveResponse()
	if err != nil {
		env.t.Fatalf("Failed to receive response %d: %v", index, err)
	}

	env.validateRequestLatency(index, requestStart, maxLatency)
	env.validateResponseContent(index, req, resp)
}

// validateRequestLatency checks if request latency is acceptable.
func (env *TCPPerformanceTestEnvironment) validateRequestLatency(index int, start time.Time, maxLatency time.Duration) {
	latency := time.Since(start)
	if latency > maxLatency {
		env.t.Errorf("Request %d latency %v exceeds maximum %v", index, latency, maxLatency)
	}
}

// validateResponseContent validates the response content.
func (env *TCPPerformanceTestEnvironment) validateResponseContent(index int, req *mcp.Request, resp *mcp.Response) {
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		env.t.Errorf("Invalid result type for request %d", index)

		return
	}

	if method, ok := result["method"].(string); !ok || method != req.Method {
		env.t.Errorf("Method mismatch for request %d: expected %s, got %v", index, req.Method, method)
	}
}

// validatePerformanceMetrics validates overall performance metrics.
func (env *TCPPerformanceTestEnvironment) validatePerformanceMetrics(
	params PerformanceTestParameters,
	totalTime time.Duration,
) {
	// Check request count.
	if int(*env.totalRequests) != params.NumRequests {
		env.t.Errorf("Expected %d requests, server processed %d", params.NumRequests, *env.totalRequests)
	}

	// Check average latency.
	avgLatency := totalTime / time.Duration(params.NumRequests)
	if avgLatency > params.MaxLatency {
		env.t.Errorf("Average latency %v exceeds maximum %v", avgLatency, params.MaxLatency)
	}

	// Check throughput.
	throughput := float64(params.NumRequests) / totalTime.Seconds()
	if throughput < params.MinThroughput {
		env.t.Errorf("Throughput %.2f req/s below minimum %.2f req/s", throughput, params.MinThroughput)
	}
}

// reportPerformanceMetrics logs performance results.
func (env *TCPPerformanceTestEnvironment) reportPerformanceMetrics(numRequests int, totalTime time.Duration) {
	avgLatency := totalTime / time.Duration(numRequests)
	throughput := float64(numRequests) / totalTime.Seconds()

	env.t.Logf("Performance metrics:")
	env.t.Logf("  Total requests: %d", *env.totalRequests)
	env.t.Logf("  Total test time: %v", totalTime)
	env.t.Logf("  Average latency: %v", avgLatency)
	env.t.Logf("  Throughput: %.2f req/s", throughput)
}

// Cleanup closes resources.
func (env *TCPPerformanceTestEnvironment) Cleanup() {
	if env.client != nil {
		_ = env.client.Close()
	}

	if env.serverStop != nil {
		env.serverStop()
	}
}
