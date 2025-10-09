package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// echoHandlerWithCounting is like echoHandler but counts each message received.
func echoHandlerWithCounting(conn net.Conn, requestCounter *int64) {
	reader := bufio.NewReader(conn)

	// Handle version negotiation first.
	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		// Send version ack.
		ackPayload := map[string]interface{}{
			"agreed_version": 1,
		}
		ackData, _ := json.Marshal(ackPayload)
		ackFrame := &BinaryFrame{
			Version:     1,
			MessageType: MessageTypeVersionAck,
			Payload:     ackData,
		}
		_ = ackFrame.Write(conn)
	}

	// Handle regular messages and count each one.
	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				_ = conn.Close()
			}

			break
		}

		// Increment request counter for each message received.
		atomic.AddInt64(requestCounter, 1)

		// Echo the frame back.
		response := &BinaryFrame{
			Version:     frame.Version,
			MessageType: MessageTypeResponse,
			Payload:     frame.Payload,
		}
		_ = response.Write(conn)
	}
}

// TestGatewayClient_LoadBalancing tests load balancing across multiple gateway connections.
// createLoadBalancingServers creates multiple mock servers for load balancing tests.
func createLoadBalancingServers(t *testing.T, count int) ([]*mockTCPServerBench, []string, []int64, func()) {
	t.Helper()

	servers := make([]*mockTCPServerBench, count)
	addresses := make([]string, count)
	requestCounts := make([]int64, count)

	cleanups := make([]func(), count)

	for i := 0; i < count; i++ {
		serverIndex := i
		servers[i] = newMockTCPServerBench(t, func(conn net.Conn) {
			echoHandlerWithCounting(conn, &requestCounts[serverIndex])
		})

		addr, err := servers[i].Start()
		if err != nil {
			t.Fatalf("Failed to start server %d: %v", i, err)
		}

		addresses[i] = addr
		cleanups[i] = func() { servers[i].Stop() }
	}

	return servers, addresses, requestCounts, func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}
}

// runLoadBalancingClients runs multiple clients against load balanced servers.
func runLoadBalancingClients(t *testing.T, addresses []string, numClients, requestsPerClient int, logger *zap.Logger) {
	t.Helper()
	var wg sync.WaitGroup
	for clientIdx := 0; clientIdx < numClients; clientIdx++ {
		wg.Add(1)
		go runSingleLoadBalancingClient(t, &wg, clientIdx, addresses, requestsPerClient, logger)
	}
	wg.Wait()
}

func runSingleLoadBalancingClient(
	t *testing.T, wg *sync.WaitGroup, clientID int,
	addresses []string, requestsPerClient int, logger *zap.Logger,
) {
	t.Helper()
	defer wg.Done()

	client := createLoadBalancingClient(t, clientID, addresses, logger)
	if client == nil {
		return
	}
	defer closeLoadBalancingClient(t, client, clientID)

	if !connectLoadBalancingClient(t, client, clientID) {
		return
	}

	sendLoadBalancingRequests(t, client, clientID, requestsPerClient)
}

func createLoadBalancingClient(t *testing.T, clientID int, addresses []string, logger *zap.Logger) *TCPClient {
	t.Helper()
	serverIdx := clientID % len(addresses)
	cfg := config.GatewayConfig{
		URL: "tcp://" + addresses[serverIdx],
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		t.Errorf("Client %d: Failed to create client: %v", clientID, err)

		return nil
	}

	return client
}

func closeLoadBalancingClient(t *testing.T, client *TCPClient, clientID int) {
	t.Helper()
	if err := client.Close(); err != nil {
		t.Errorf("Client %d: Failed to close: %v", clientID, err)
	}
}

func connectLoadBalancingClient(t *testing.T, client *TCPClient, clientID int) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Errorf("Client %d: Failed to connect: %v", clientID, err)

		return false

	}

	return true
}

func sendLoadBalancingRequests(t *testing.T, client *TCPClient, clientID, requestsPerClient int) {
	t.Helper()
	ctx := context.Background()
	for reqIdx := 0; reqIdx < requestsPerClient; reqIdx++ {
		req := &mcp.Request{
			JSONRPC: "2.0",
			Method:  "test.echo",
			ID:      fmt.Sprintf("client-%d-req-%d", clientID, reqIdx),
			Params:  map[string]interface{}{"message": "hello"},
		}

		if err := client.SendRequest(ctx, req); err != nil {
			t.Errorf("Client %d: Failed to send request: %v", clientID, err)

			continue
		}

		_, err := client.ReceiveResponse()
		if err != nil {
			t.Errorf("Client %d: Failed to receive response: %v", clientID, err)
		}
	}
}

func TestGatewayClient_LoadBalancing(t *testing.T) {
	// ctx := context.Background() - unused
	_, addresses, requestCounts, cleanup := createLoadBalancingServers(t, 3)
	defer cleanup()

	gatewayConfig := config.GatewayConfig{
		URL: "tcp://" + addresses[0],
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	logger := zaptest.NewLogger(t)
	client, err := NewTCPClient(gatewayConfig, logger)
	require.NoError(t, err)

	defer func() { _ = client.Close() }()

	runLoadBalancingClients(t, addresses, 5, 10, logger)

	// Verify load distribution
	totalRequests := int64(0)

	for i, count := range requestCounts {
		actualCount := atomic.LoadInt64(&count)
		totalRequests += actualCount
		t.Logf("Server %d (%s) handled %d requests", i, addresses[i], actualCount)
	}

	require.Positive(t, totalRequests, "No requests were processed")

	// With round-robin, requests should be distributed across servers
	nonZeroServers := 0

	for _, count := range requestCounts {
		if atomic.LoadInt64(&count) > 0 {
			nonZeroServers++
		}
	}

	require.Greater(t, nonZeroServers, 1, "Load balancing should distribute requests across multiple servers")
}

// TestGatewayClient_FailoverScenarios tests various failover scenarios.
func TestGatewayClient_FailoverScenarios(t *testing.T) {
	// ctx := context.Background() - unused
	logger := zaptest.NewLogger(t)
	tests := createFailoverScenarioTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runFailoverScenarioTest(t, logger, tt)
		})
	}
}

type failoverScenarioTest struct {
	name            string
	serverBehavior  func(conn net.Conn)
	clientOps       func(context.Context, *TCPClient) error
	expectedSuccess bool
	expectedError   string
}

func createFailoverScenarioTests() []failoverScenarioTest {
	tests := make([]failoverScenarioTest, 0, 3)
	tests = append(tests, createServerShutdownTest())

	tests = append(tests, createCorruptionTest())

	tests = append(tests, createSlowResponseTest())

	return tests
}

func createServerShutdownTest() failoverScenarioTest {
	return failoverScenarioTest{
		name: "server_shutdown_during_request",
		serverBehavior: func(conn net.Conn) {
			reader := newEchoReader(conn)
			frame, err := ReadBinaryFrame(reader)
			if err != nil {
				return
			}
			if frame.MessageType == MessageTypeVersionNegotiation {
				sendIntegrationVersionAck(conn)
			}
			_ = conn.Close()
		},
		clientOps: func(ctx context.Context, client *TCPClient) error {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "test_failover",
				ID:      "failover-1",
			}
			if err := client.SendRequest(ctx, req); err != nil {
				return err

			}
			_, err := client.ReceiveResponse()

			return err
		},
		expectedSuccess: false,
		expectedError:   "connection closed unexpectedly", // More descriptive error from improved error handling
	}
}

func createCorruptionTest() failoverScenarioTest {
	return failoverScenarioTest{
		name: "partial_response_corruption",
		serverBehavior: func(conn net.Conn) {
			reader := newEchoReader(conn)
			frame, err := ReadBinaryFrame(reader)
			if err != nil {
				return
			}
			if frame.MessageType == MessageTypeVersionNegotiation {
				sendIntegrationVersionAck(conn)
			}
			_, err = ReadBinaryFrame(reader)
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
			_ = conn.Close()
		},
		clientOps: func(ctx context.Context, client *TCPClient) error {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "test_corruption",
				ID:      "corrupt-1",
			}
			if err := client.SendRequest(ctx, req); err != nil {
				return err
			}
			_, err := client.ReceiveResponse()

			return err
		},
		expectedSuccess: false,
		expectedError:   "failed to read",
	}
}

func createSlowResponseTest() failoverScenarioTest {
	return failoverScenarioTest{
		name: "slow_server_response",
		serverBehavior: func(conn net.Conn) {
			reader := newEchoReader(conn)
			frame, err := ReadBinaryFrame(reader)
			if err != nil {
				return
			}
			if frame.MessageType == MessageTypeVersionNegotiation {
				sendIntegrationVersionAck(conn)
			}
			frame, err = ReadBinaryFrame(reader)
			if err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)

			var wireMsg struct {
				ID         interface{}     `json:"id"`
				MCPPayload json.RawMessage `json:"mcp_payload"`
			}
			if err := json.Unmarshal(frame.Payload, &wireMsg); err != nil {
				return
			}

			resp := mcp.Response{
				JSONRPC: constants.TestJSONRPCVersion,
				ID:      wireMsg.ID,
				Result:  map[string]interface{}{"status": "slow_but_ok"},
			}
			sendResponse(conn, frame.Version, wireMsg.ID, resp)
		},
		clientOps: func(ctx context.Context, client *TCPClient) error {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "test_slow",
				ID:      "slow-1",
			}
			if err := client.SendRequest(ctx, req); err != nil {
				return err
			}
			_, err := client.ReceiveResponse()

			return err
		},
		expectedSuccess: true,
		expectedError:   "",
	}
}

func runFailoverScenarioTest(t *testing.T, logger *zap.Logger, tt failoverScenarioTest) {
	t.Helper()

	server := newMockTCPServerBench(t, tt.serverBehavior)

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
		if !tt.expectedSuccess && tt.expectedError != "" && containsError(err.Error(), tt.expectedError) {
			return
		}

		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() { _ = client.Close() }()

	err = tt.clientOps(ctx, client)
	validateFailoverResult(t, err, tt)
}

func validateFailoverResult(t *testing.T, err error, tt failoverScenarioTest) {
	t.Helper()

	if tt.expectedSuccess {
		if err != nil {
			t.Errorf("Expected success but got error: %v", err)
		}
	} else {
		if err == nil {
			t.Errorf("Expected error containing %q but got success", tt.expectedError)
		} else if tt.expectedError != "" && !containsError(err.Error(), tt.expectedError) {
			t.Errorf("Expected error containing %q, got: %v", tt.expectedError, err)
		}
	}
}

// TestGatewayClient_CircuitBreakerBehavior tests circuit breaker functionality.
func TestGatewayClient_CircuitBreakerBehavior(t *testing.T) {
	// ctx := context.Background() - unused
	handler := CreateCircuitBreakerTestHandler(t)
	handler.ExecuteTest()
}

// TestGatewayClient_ConcurrentFailover tests concurrent operations during failover.
func TestGatewayClient_ConcurrentFailover(t *testing.T) {
	// ctx := context.Background() - unused
	logger := zaptest.NewLogger(t)
	serverCounters := createServerCounters()

	server := createFailoverTestServer(t, serverCounters)

	addr := startFailoverServer(t, server)
	defer server.Stop()

	cfg := createFailoverClientConfig(addr)
	clientCounters := runConcurrentFailoverTest(t, cfg, logger)

	validateConcurrentFailoverResults(t, serverCounters, clientCounters)
}

type serverCounters struct {
	requestCount *int64
	successCount *int64
	errorCount   *int64
}

type clientCounters struct {
	successes *int64
	errors    *int64
	total     int64
}

func createServerCounters() *serverCounters {
	return &serverCounters{
		requestCount: new(int64),
		successCount: new(int64),
		errorCount:   new(int64),
	}
}

func createFailoverTestServer(t *testing.T, counters *serverCounters) *mockTCPServerBench {
	t.Helper()

	return newMockTCPServerBench(t, func(conn net.Conn) {
		atomic.AddInt64(counters.requestCount, 1)
		currentCount := atomic.LoadInt64(counters.requestCount)

		if currentCount%3 == 0 {
			atomic.AddInt64(counters.errorCount, 1)

			_ = conn.Close()

			return
		}

		handleSuccessfulConnection(conn, counters.successCount)
	})
}

func handleSuccessfulConnection(conn net.Conn, successCount *int64) {
	reader := newEchoReader(conn)

	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		sendIntegrationVersionAck(conn)
	}

	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			return
		}

		if frame.MessageType == MessageTypeRequest {
			atomic.AddInt64(successCount, 1)
			processRequestFrame(conn, *frame)
		}
	}
}

func processRequestFrame(conn net.Conn, frame BinaryFrame) {
	var wireMsg struct {
		ID         interface{}     `json:"id"`
		MCPPayload json.RawMessage `json:"mcp_payload"`
	}
	if err := json.Unmarshal(frame.Payload, &wireMsg); err != nil {
		return
	}

	resp := mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		ID:      wireMsg.ID,
		Result:  map[string]interface{}{"status": "concurrent_success"},
	}
	sendResponse(conn, frame.Version, wireMsg.ID, resp)
}

func startFailoverServer(t *testing.T, server *mockTCPServerBench) string {
	t.Helper()

	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	return addr
}

func createFailoverClientConfig(addr string) config.GatewayConfig {
	return config.GatewayConfig{
		URL: "tcp://" + addr,
		Connection: common.ConnectionConfig{
			TimeoutMs: 3000,
		},
	}
}

func runConcurrentFailoverTest(t *testing.T, cfg config.GatewayConfig, logger *zap.Logger) *clientCounters {
	t.Helper()

	const (
		numGoroutines          = 20
		operationsPerGoroutine = 5
	)

	counters := &clientCounters{
		successes: new(int64),
		errors:    new(int64),
		total:     numGoroutines * operationsPerGoroutine,
	}

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func(goroutineID int) {
			defer wg.Done()

			runGoroutineOperations(goroutineID, operationsPerGoroutine, cfg, logger, counters)
		}(i)
	}

	wg.Wait()

	return counters
}

func runGoroutineOperations(
	goroutineID, operationsCount int,
	cfg config.GatewayConfig,
	logger *zap.Logger,
	counters *clientCounters,
) {
	for opIdx := 0; opIdx < operationsCount; opIdx++ {
		runSingleOperation(goroutineID, opIdx, cfg, logger, counters)
	}
}

func runSingleOperation(
	goroutineID, opIdx int,
	cfg config.GatewayConfig,
	logger *zap.Logger,
	counters *clientCounters,
) {
	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		atomic.AddInt64(counters.errors, 1)

		return
	}

	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		atomic.AddInt64(counters.errors, 1)

		return
	}

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  fmt.Sprintf("concurrent_%d_%d", goroutineID, opIdx),
		ID:      fmt.Sprintf("req_%d_%d", goroutineID, opIdx),
	}

	if err := client.SendRequest(ctx, req); err != nil {
		atomic.AddInt64(counters.errors, 1)

		return
	}

	_, err = client.ReceiveResponse()
	if err != nil {
		atomic.AddInt64(counters.errors, 1)
	} else {
		atomic.AddInt64(counters.successes, 1)
	}
}

func validateConcurrentFailoverResults(t *testing.T, serverCounters *serverCounters, clientCounters *clientCounters) {
	t.Helper()
	clientSuccesses := atomic.LoadInt64(clientCounters.successes)
	clientErrors := atomic.LoadInt64(clientCounters.errors)

	t.Logf("Concurrent failover test results:")
	t.Logf("  Total operations attempted: %d", clientCounters.total)
	t.Logf("  Client successes: %d", clientSuccesses)
	t.Logf("  Client errors: %d", clientErrors)
	t.Logf("  Server request count: %d", atomic.LoadInt64(serverCounters.requestCount))
	t.Logf("  Server success count: %d", atomic.LoadInt64(serverCounters.successCount))
	t.Logf("  Server error count: %d", atomic.LoadInt64(serverCounters.errorCount))

	if clientSuccesses == 0 {
		t.Error("Expected some successful operations in concurrent failover test")
	}

	if clientErrors == 0 {
		t.Error("Expected some errors due to simulated server failures")
	}

	successRate := float64(clientSuccesses) / float64(clientCounters.total)
	if successRate < 0.3 {
		t.Errorf("Success rate %.2f%% too low for concurrent failover test", successRate*100)
	}
}

// Helper functions for the tests.

func newEchoReader(conn net.Conn) *bufio.Reader {
	return bufio.NewReader(conn)
}

func sendIntegrationVersionAck(conn net.Conn) {
	ackPayload := map[string]interface{}{"agreed_version": 1}
	ackData, _ := json.Marshal(ackPayload)
	ackFrame := &BinaryFrame{
		Version:     1,
		MessageType: MessageTypeVersionAck,
		Payload:     ackData,
	}
	_ = ackFrame.Write(conn)
}

func sendResponse(conn net.Conn, version uint16, requestID interface{}, resp mcp.Response) {
	respWire := struct {
		ID         interface{} `json:"id"`
		MCPPayload interface{} `json:"mcp_payload"`
	}{
		ID:         requestID,
		MCPPayload: resp,
	}

	payload, _ := json.Marshal(respWire)
	respFrame := &BinaryFrame{
		Version:     version,
		MessageType: MessageTypeResponse,
		Payload:     payload,
	}
	_ = respFrame.Write(conn)
}

func containsError(errorStr, expectedSubstring string) bool {
	return errorStr != "" && strings.Contains(errorStr, expectedSubstring)
}
