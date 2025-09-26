package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// testingLogger interface allows using both *testing.T and *testing.B.
type testingLogger interface {
	Logf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
}

// mockTCPServerBench is a benchmark-compatible version of mockTCPServer.
type mockTCPServerBench struct {
	listener   net.Listener
	logger     testingLogger
	handler    func(conn net.Conn)
	startedCh  chan struct{}
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

func newMockTCPServerBench(logger testingLogger, handler func(conn net.Conn)) *mockTCPServerBench {
	return &mockTCPServerBench{
		logger:     logger,
		handler:    handler,
		startedCh:  make(chan struct{}),
		shutdownCh: make(chan struct{}),
	}
}

func (s *mockTCPServerBench) Start() (string, error) {
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
					s.logger.Logf("Accept error: %v", err)
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

func (s *mockTCPServerBench) Stop() {
	close(s.shutdownCh)
	_ = s.listener.Close()
	s.wg.Wait()
}

// TestTCPClient_ConnectionLifecycle tests the complete lifecycle of TCP connections.
func TestTCPClient_ConnectionLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	eventTracker := createEventTracker()
	
	server := createLifecycleTestServer(t, eventTracker)

	addr := startLifecycleServer(t, server)
	defer server.Stop()
	
	client := createLifecycleTestClient(t, addr, logger)
	runLifecycleTest(t, client)
	
	validateLifecycleEvents(t, eventTracker)
}

type eventTracker struct {
	events []string
	mu     sync.Mutex
}

func createEventTracker() *eventTracker {
	return &eventTracker{
		events: make([]string, 0),
	}
}

func (e *eventTracker) addEvent(event string) {
	e.mu.Lock()
	e.events = append(e.events, event)
	e.mu.Unlock()
}

func (e *eventTracker) getEvents() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.events...)
}

func createLifecycleTestServer(t *testing.T, tracker *eventTracker) *mockTCPServer {
	return newMockTCPServer(t, func(conn net.Conn) {
		handleLifecycleConnection(conn, tracker)
	})
}

func handleLifecycleConnection(conn net.Conn, tracker *eventTracker) {
	tracker.addEvent("connection_accepted")

	defer func() {
		tracker.addEvent("connection_closed")
	}()
	
	reader := bufio.NewReader(conn)
	handleVersionNegotiation(reader, conn, tracker)
	handleRegularMessages(reader, conn, tracker)
}

func handleVersionNegotiation(reader *bufio.Reader, conn net.Conn, tracker *eventTracker) {
	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return
	}
	
	if frame.MessageType == MessageTypeVersionNegotiation {
		tracker.addEvent("version_negotiation")
		sendVersionAck(conn)
		tracker.addEvent("version_ack_sent")
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

func handleRegularMessages(reader *bufio.Reader, conn net.Conn, tracker *eventTracker) {
	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				tracker.addEvent("read_error")
			}
			return
		}
		
		tracker.addEvent("message_received")
		handleMessageByType(*frame, conn, tracker)
	}
}

func handleMessageByType(frame BinaryFrame, conn net.Conn, tracker *eventTracker) {
	switch frame.MessageType {
	case MessageTypeHealthCheck:
		handlePing(frame, conn, tracker)
	case MessageTypeRequest:
		handleRequest(frame, conn, tracker)
	case MessageTypeResponse:
		tracker.addEvent("response_received")
	case MessageTypeControl:
		tracker.addEvent("control_received")
	case MessageTypeError:
		tracker.addEvent("error_received")
	case MessageTypeVersionNegotiation:
		tracker.addEvent("version_negotiation_received")
	case MessageTypeVersionAck:
		tracker.addEvent("version_ack_received")
	}
}

func handlePing(frame BinaryFrame, conn net.Conn, tracker *eventTracker) {
	tracker.addEvent("ping_received")

	pongFrame := &BinaryFrame{
		Version:     frame.Version,
		MessageType: MessageTypeHealthCheck,
		Payload:     []byte("{}"),
	}
	_ = pongFrame.Write(conn)

	tracker.addEvent("pong_sent")
}

func handleRequest(frame BinaryFrame, conn net.Conn, tracker *eventTracker) {
	tracker.addEvent("request_received")

	respFrame := &BinaryFrame{
		Version:     frame.Version,
		MessageType: MessageTypeResponse,
		Payload:     frame.Payload,
	}
	_ = respFrame.Write(conn)

	tracker.addEvent("response_sent")
}

func startLifecycleServer(t *testing.T, server *mockTCPServer) string {
	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	return addr
}

func createLifecycleTestClient(t *testing.T, addr string, logger *zap.Logger) *TCPClient {
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
	return client
}

func runLifecycleTest(t *testing.T, client *TCPClient) {
	ctx := context.Background()
	
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	
	if !client.IsConnected() {
		t.Error("Expected client to be connected")
	}
	
	if err := client.SendPing(); err != nil {
		t.Errorf("Failed to send ping: %v", err)
	}
	
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-1",
	}
	if err := client.SendRequest(req); err != nil {
		t.Errorf("Failed to send request: %v", err)
	}
	
	_ = client.Close()
	
	if client.IsConnected() {
		t.Error("Expected client to be disconnected")
	}
}

func validateLifecycleEvents(t *testing.T, tracker *eventTracker) {
	time.Sleep(100 * time.Millisecond)

	events := tracker.getEvents()
	
	expectedEvents := []string{"connection_accepted", "version_negotiation", "version_ack_sent"}
	for _, expected := range expectedEvents {
		found := false

		for _, event := range events {
			if event == expected {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Expected event %s not found in events: %v", expected, events)
		}
	}
}

// TestTCPClient_ConcurrentOperations tests thread safety of TCP client.
func TestTCPClient_ConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var requestCount int64

	server := setupConcurrentOperationsServer(t, &requestCount)

	addr := startServerOrFail(t, server)
	defer server.Stop()

	client := createAndConnectClient(t, addr, logger)
	defer closeClient(t, client)

	const numRequests = 50

	results := runConcurrentRequests(t, client, numRequests)
	validateConcurrentResults(t, results, numRequests, requestCount)
}

func setupConcurrentOperationsServer(t *testing.T, requestCount *int64) *mockTCPServer {
	t.Helper()
	
	return newMockTCPServer(t, func(conn net.Conn) {
		handleConcurrentConnection(conn, requestCount)
	})
}

func handleConcurrentConnection(conn net.Conn, requestCount *int64) {
	reader := bufio.NewReader(conn)

	if !handleConcurrentVersionNegotiation(reader, conn) {
		return
	}

	// Handle multiple concurrent requests
	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			return
		}

		if frame.MessageType == MessageTypeRequest {
			if !processConcurrentRequest(*frame, conn, requestCount) {
				return
			}
		}
	}
}

func handleConcurrentVersionNegotiation(reader *bufio.Reader, conn net.Conn) bool {
	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return false
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		ackPayload := map[string]interface{}{"agreed_version": 1}
		ackData, _ := json.Marshal(ackPayload)
		ackFrame := &BinaryFrame{
			Version:     1,
			MessageType: MessageTypeVersionAck,
			Payload:     ackData,
		}
		_ = ackFrame.Write(conn)
	}
	
	return true
}

func processConcurrentRequest(frame BinaryFrame, conn net.Conn, requestCount *int64) bool {
	count := atomic.AddInt64(requestCount, 1)

	// Parse request to get ID
	var wireMsg struct {
		ID         interface{}     `json:"id"`
		MCPPayload json.RawMessage `json:"mcp_payload"`
	}
	if err := json.Unmarshal(frame.Payload, &wireMsg); err != nil {
		return false
	}

	// Send response with count
	resp := mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		ID:      wireMsg.ID,
		Result:  map[string]interface{}{"count": count},
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

	return respFrame.Write(conn) == nil
}

func startServerOrFail(t *testing.T, server *mockTCPServer) string {
	t.Helper()
	
	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	return addr
}

func createAndConnectClient(t *testing.T, addr string, logger *zap.Logger) *TCPClient {
	t.Helper()
	
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

	return client
}

func closeClient(t *testing.T, client *TCPClient) {
	t.Helper()
	
	if err := client.Close(); err != nil {
		t.Logf("Failed to close client: %v", err)
	}
}

func runConcurrentRequests(t *testing.T, client *TCPClient, numRequests int) chan int64 {
	t.Helper()
	
	var wg sync.WaitGroup

	results := make(chan int64, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)

		go sendConcurrentRequest(t, &wg, client, i, results)
	}

	wg.Wait()
	close(results)
	return results
}

func sendConcurrentRequest(t *testing.T, wg *sync.WaitGroup, client *TCPClient, id int, results chan<- int64) {
	defer wg.Done()

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      fmt.Sprintf("req-%d", id),
	}

	if err := client.SendRequest(req); err != nil {
		t.Errorf("Failed to send request %d: %v", id, err)
		return
	}

	resp, err := client.ReceiveResponse()
	if err != nil {
		t.Errorf("Failed to receive response %d: %v", id, err)
		return
	}

	count := extractCountFromResponse(t, resp, id)
	if count >= 0 {
		results <- count
	}
}

func extractCountFromResponse(t *testing.T, resp *mcp.Response, id int) int64 {
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Errorf("Invalid result type for request %d", id)
		return -1
	}

	count, ok := result["count"].(float64)
	if !ok {
		t.Errorf("Invalid count type for request %d", id)
		return -1
	}

	return int64(count)
}

func validateConcurrentResults(t *testing.T, results chan int64, expectedNumRequests int, actualRequestCount int64) {
	t.Helper()
	
	// Verify all requests were processed
	receivedCounts := make(map[int64]bool)
	for count := range results {
		receivedCounts[count] = true
	}

	if len(receivedCounts) != expectedNumRequests {
		t.Errorf("Expected %d unique responses, got %d", expectedNumRequests, len(receivedCounts))
	}

	if actualRequestCount != int64(expectedNumRequests) {
		t.Errorf("Expected %d requests processed, got %d", expectedNumRequests, actualRequestCount)
	}
}

// TestTCPClient_ErrorRecovery tests error handling and recovery scenarios.
func TestTCPClient_ErrorRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tests := createErrorRecoveryTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runErrorRecoveryTest(t, tt, logger)
		})
	}
}

type errorRecoveryTest struct {
	name          string
	handler       func(net.Conn)
	expectedError string
	shouldRecover bool
}

func createErrorRecoveryTests() []errorRecoveryTest {
	return []errorRecoveryTest{
		{
			name:          "malformed version negotiation",
			handler:       handleMalformedVersionNegotiation,
			expectedError: "failed to read",
			shouldRecover: false,
		},
		{
			name:          "connection drop after handshake",
			handler:       handleConnectionDropAfterHandshake,
			expectedError: "connection",
			shouldRecover: true,
		},
		{
			name:          "partial message transmission",
			handler:       handlePartialMessageTransmission,
			expectedError: "connection",
			shouldRecover: true,
		},
	}
}

func handleMalformedVersionNegotiation(conn net.Conn) {
	// Send invalid version negotiation response
	_, _ = conn.Write([]byte("invalid data"))
	_ = conn.Close()
}

func handleConnectionDropAfterHandshake(conn net.Conn) {
	reader := bufio.NewReader(conn)

	// Handle version negotiation properly
	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		sendVersionAck(conn)
	}

	// Close connection immediately after handshake
	_ = conn.Close()
}

func handlePartialMessageTransmission(conn net.Conn) {
	reader := bufio.NewReader(conn)

	// Handle version negotiation
	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		sendVersionAck(conn)
	}

	// Read request and send partial response
	_, err = ReadBinaryFrame(reader)
	if err != nil {
		return
	}

	// Send only partial response header then close
	_, _ = conn.Write([]byte{0x00, 0x01}) // Partial header
	_ = conn.Close()
}

func runErrorRecoveryTest(t *testing.T, tt errorRecoveryTest, logger *zap.Logger) {
	t.Helper()
	
	server := newMockTCPServer(t, tt.handler)

	addr := startServerOrFail(t, server)
	defer server.Stop()

	client := createErrorRecoveryClient(t, addr, logger)

	defer func() { _ = client.Close() }()

	ctx := context.Background()
	err := attemptConnectionAndOperation(client, ctx)
	
	validateErrorResult(t, err, tt.expectedError)
}

func createErrorRecoveryClient(t *testing.T, addr string, logger *zap.Logger) *TCPClient {
	t.Helper()
	
	cfg := config.GatewayConfig{
		URL: "tcp://" + addr,
		Connection: common.ConnectionConfig{
			TimeoutMs: 2000, // Shorter timeout for error tests
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	return client
}

func attemptConnectionAndOperation(client *TCPClient, ctx context.Context) error {
	// First connection attempt should fail or succeed depending on test
	err := client.Connect(ctx)
	if err != nil {
		return err
	}

	// If connect succeeded, try to send a request which should fail
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-1",
	}

	err = client.SendRequest(req)
	if err != nil {
		return err
	}

	// Try to receive response which should fail
	_, err = client.ReceiveResponse()
	return err
}

func validateErrorResult(t *testing.T, err error, expectedError string) {
	t.Helper()
	
	if expectedError != "" {
		if err == nil || !strings.Contains(err.Error(), expectedError) {
			t.Errorf("Expected error containing %q, got: %v", expectedError, err)
		}
	} else if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

// TestTCPClient_PerformanceMetrics tests performance characteristics.
func TestTCPClient_PerformanceMetrics(t *testing.T) { 
	logger := zaptest.NewLogger(t)

	// Use descriptive performance test environment instead of complex inline logic.
	env := EstablishTCPPerformanceTest(t, logger)
	defer env.Cleanup()

	// Setup server with performance handler.
	server := newMockTCPServer(t, env.CreatePerformanceHandler())

	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	env.SetServerInfo(addr, server.Stop)
	env.CreateClient(addr)

	// Define test parameters.
	params := PerformanceTestParameters{
		NumRequests:   100,
		MaxLatency:    100 * time.Millisecond, // Expected max latency for local TCP
		MinThroughput: 100.0,                  // requests per second
	}

	// Execute performance test.
	env.ExecutePerformanceTest(params)
}

// TestTCPClient_ConnectionPooling tests connection reuse and pooling behavior.
func TestTCPClient_ConnectionPooling(t *testing.T) { 
	logger := zaptest.NewLogger(t)

	// Use descriptive connection pooling test environment.
	env := EstablishConnectionPoolingTest(t, logger)
	defer env.Cleanup()

	// Setup server with pooling handler.
	server := newMockTCPServer(t, env.CreatePoolingServerHandler())

	addr, err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	env.SetupServer(addr, server.Stop)

	// Test connection reuse with multiple requests.
	const numRequests = 10
	env.TestConnectionReuse(numRequests)

	// Test reconnection behavior.
	env.TestReconnection()
}

// BenchmarkTCPClient_SendRequest benchmarks TCP client request sending.
func BenchmarkTCPClient_SendRequest(b *testing.B) {
	logger := zaptest.NewLogger(b)

	server := createBenchmarkEchoServer(b)

	addr, err := server.Start()
	if err != nil {
		b.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	cfg := config.GatewayConfig{
		URL: "tcp://" + addr,
		Connection: common.ConnectionConfig{
			TimeoutMs: 10000,
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			b.Logf("Failed to close client: %v", err)
		}
	}()

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "benchmark",
		Params:  map[string]interface{}{"key": "value"},
		ID:      "bench-1",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := client.SendRequest(req); err != nil {
			b.Fatalf("Failed to send request: %v", err)
		}

		_, err := client.ReceiveResponse()
		if err != nil {
			b.Fatalf("Failed to receive response: %v", err)
		}
	}
}

// createBenchmarkEchoServer creates a mock server that handles version negotiation and echoes requests.
func createBenchmarkEchoServer(b *testing.B) *mockTCPServerBench {
	b.Helper()

	return newMockTCPServerBench(b, func(conn net.Conn) {
		reader := bufio.NewReader(conn)

		handleVersionNegotiationBench(conn, reader)
		echoRequestsBench(conn, reader)
	})
}

// handleVersionNegotiationBench handles the initial version negotiation for benchmark tests.
func handleVersionNegotiationBench(conn net.Conn, reader *bufio.Reader) {
	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		ackPayload := map[string]interface{}{"agreed_version": 1}
		ackData, _ := json.Marshal(ackPayload)
		ackFrame := &BinaryFrame{
			Version:     1,
			MessageType: MessageTypeVersionAck,
			Payload:     ackData,
		}
		_ = ackFrame.Write(conn)
	}
}

// echoRequestsBench continuously echoes back all incoming requests for benchmark tests.
func echoRequestsBench(conn net.Conn, reader *bufio.Reader) {
	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			return
		}

		if frame.MessageType == MessageTypeRequest {
			respFrame := &BinaryFrame{
				Version:     frame.Version,
				MessageType: MessageTypeResponse,
				Payload:     frame.Payload,
			}
			_ = respFrame.Write(conn)
		}
	}
}

// BenchmarkTCPClient_ConcurrentRequests benchmarks concurrent TCP operations.
func BenchmarkTCPClient_ConcurrentRequests(b *testing.B) {
	logger := zaptest.NewLogger(b)

	server := newMockTCPServerBench(b, echoHandler)

	addr, err := server.Start()
	if err != nil {
		b.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	cfg := config.GatewayConfig{
		URL: "tcp://" + addr,
		Connection: common.ConnectionConfig{
			TimeoutMs: 10000,
		},
	}

	client, err := NewTCPClient(cfg, logger)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			b.Logf("Failed to close client: %v", err)
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "concurrent_benchmark",
				ID:      fmt.Sprintf("concurrent-%d", time.Now().UnixNano()),
			}

			if err := client.SendRequest(req); err != nil {
				b.Fatalf("Failed to send request: %v", err)
			}

			_, err := client.ReceiveResponse()
			if err != nil {
				b.Fatalf("Failed to receive response: %v", err)
			}
		}
	})
}

// TestTCPClient_HealthMonitoring tests health check and monitoring features.
func TestTCPClient_HealthMonitoring(t *testing.T) { 
	logger := zaptest.NewLogger(t)

	var pingCount int64

	server := createHealthMonitoringServer(t, &pingCount)

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

	// Test multiple health checks.
	const numPings = 5
	for i := 0; i < numPings; i++ {
		if err := client.SendPing(); err != nil {
			t.Errorf("Failed to send ping %d: %v", i, err)
		}

		time.Sleep(constants.TestTickInterval) // Small delay between pings
	}

	// Allow time for all pings to be processed.
	time.Sleep(100 * time.Millisecond)

	if pingCount != numPings {
		t.Errorf("Expected %d pings received, got %d", numPings, pingCount)
	}
}

// createHealthMonitoringServer creates a server that handles version negotiation and health checks.
func createHealthMonitoringServer(t *testing.T, pingCount *int64) *mockTCPServer {
	t.Helper()

	return newMockTCPServer(t, func(conn net.Conn) {
		reader := bufio.NewReader(conn)

		handleVersionNegotiationForHealth(t, conn, reader)
		processHealthCheckRequests(t, conn, reader, pingCount)
	})
}

// handleVersionNegotiationForHealth handles version negotiation for health monitoring tests.
func handleVersionNegotiationForHealth(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	t.Helper()

	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		sendHealthVersionAck(t, conn)
	}
}

// sendHealthVersionAck sends version acknowledgment for health monitoring tests.
func sendHealthVersionAck(t *testing.T, conn net.Conn) {
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

// processHealthCheckRequests continuously processes health check requests and responds with pongs.
func processHealthCheckRequests(t *testing.T, conn net.Conn, reader *bufio.Reader, pingCount *int64) {
	t.Helper()

	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			return
		}

		if frame.MessageType == MessageTypeHealthCheck {
			atomic.AddInt64(pingCount, 1)
			sendHealthCheckPong(t, conn, frame)
		}
	}
}

// sendHealthCheckPong sends a pong response to a health check request.
func sendHealthCheckPong(t *testing.T, conn net.Conn, requestFrame *BinaryFrame) {
	t.Helper()

	pongFrame := &BinaryFrame{
		Version:     requestFrame.Version,
		MessageType: MessageTypeHealthCheck,
		Payload:     []byte(`{"status":"healthy"}`),
	}
	_ = pongFrame.Write(conn)
}
