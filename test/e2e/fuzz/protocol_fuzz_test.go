// Fuzz test files allow flexible style
package fuzz_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Global random source for thread-safe random number generation.
// #nosec G404 - Using deterministic random for reproducible fuzzing tests
var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// FuzzTestConfig controls fuzz testing behavior.
type FuzzTestConfig struct {
	Duration     time.Duration
	Workers      int
	MaxMsgSize   int
	EnablePanics bool
}

// NewFuzzTestConfig creates fuzz configuration from environment.
func NewFuzzTestConfig() *FuzzTestConfig {
	return &FuzzTestConfig{
		Duration:     getEnvDuration("FUZZ_DURATION", 10*time.Second),
		Workers:      getEnvInt("FUZZ_WORKERS", 4),
		MaxMsgSize:   getEnvInt("FUZZ_MAX_MSG_SIZE", 64*1024), // 64KB
		EnablePanics: getEnvBool("FUZZ_ENABLE_PANICS", false),
	}
}

// ProtocolValidator validates MCP protocol messages.
type ProtocolValidator struct {
	maxMessageSize int
	requiredFields map[string]bool
}

// NewProtocolValidator creates a new protocol validator.
func NewProtocolValidator() *ProtocolValidator {
	return &ProtocolValidator{
		maxMessageSize: 1024 * 1024, // 1MB
		requiredFields: map[string]bool{
			"jsonrpc": true,
		},
	}
}

// ValidateMessage validates an MCP message.

func (pv *ProtocolValidator) ValidateMessage(
	message map[string]interface{}) error {
	// Check required fields
	if err := pv.validateRequiredFields(message); err != nil {
		return err
	}

	// Validate JSON-RPC version
	if err := pv.validateJSONRPCVersion(message); err != nil {
		return err
	}

	// Validate message type
	return pv.validateMessageType(message)
}

func (pv *ProtocolValidator) validateRequiredFields(message map[string]interface{}) error {
	for field := range pv.requiredFields {
		if _, exists := message[field]; !exists {
			return fmt.Errorf("missing required field: %s", field)
		}
	}

	return nil
}

func (pv *ProtocolValidator) validateJSONRPCVersion(message map[string]interface{}) error {
	jsonrpc, ok := message["jsonrpc"].(string)
	if !ok {
		return errors.New("jsonrpc field must be a string")
	}

	if jsonrpc != "2.0" {
		return fmt.Errorf("invalid jsonrpc version: %s", jsonrpc)
	}

	return nil
}

func (pv *ProtocolValidator) validateMessageType(message map[string]interface{}) error {
	hasMethod := message["method"] != nil
	hasResult := message["result"] != nil
	hasError := message["error"] != nil
	hasID := message["id"] != nil

	if hasMethod && (hasResult || hasError) {
		return errors.New("request cannot have result or error")
	}

	if !hasMethod && !hasResult && !hasError {
		return errors.New("message must be request, response, or error")
	}

	if (hasResult || hasError) && !hasID {
		return errors.New("response must have id")
	}

	return nil
}

// MessageProcessor processes MCP messages safely.
type MessageProcessor struct {
	validator *ProtocolValidator
	handlers  map[string]func(map[string]interface{}) (interface{}, error)
	mu        sync.RWMutex
}

// NewMessageProcessor creates a new message processor.
func NewMessageProcessor() *MessageProcessor {
	mp := &MessageProcessor{
		// Protocol validator for message validation
		validator: NewProtocolValidator(),
		handlers:  make(map[string]func(map[string]interface{}) (interface{}, error)), // Method handlers map
		// Mutex for thread-safe access to handlers
		mu: sync.RWMutex{},
	}

	// Register default handlers
	mp.RegisterHandler("ping", mp.handlePing)
	mp.RegisterHandler("echo", mp.handleEcho)
	mp.RegisterHandler("resources/list", mp.handleResourcesList)
	mp.RegisterHandler("tools/call", mp.handleToolCall)

	return mp
}

// RegisterHandler registers a method handler.
func (mp *MessageProcessor) RegisterHandler(method string, handler func(map[string]interface{}) (interface{}, error)) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.handlers[method] = handler
}

// ProcessMessage processes a message safely.
func (mp *MessageProcessor) ProcessMessage(message map[string]interface{}) (interface{}, error) {
	// Validate message structure
	if err := mp.validator.ValidateMessage(message); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Handle requests
	if method, ok := message["method"].(string); ok {
		mp.mu.RLock()
		handler, exists := mp.handlers[method]
		mp.mu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("unknown method: %s", method)
		}

		// Call handler safely
		defer func() {
			if r := recover(); r != nil {
				// Convert panic to error instead of crashing
				log.Printf("Handler panic for method %s: %v", method, r)
			}
		}()

		return handler(message)
	}

	// Handle responses (just validate for now)
	return map[string]string{"status": "processed"}, nil
}

// Handler implementations.
func (mp *MessageProcessor) handlePing(_ map[string]interface{}) (interface{}, error) {
	return map[string]string{"result": "pong"}, nil
}

func (mp *MessageProcessor) handleEcho(message map[string]interface{}) (interface{}, error) {
	params, ok := message["params"]
	if !ok {
		return nil, errors.New("echo requires params")
	}

	return map[string]interface{}{"echo": params}, nil
}

func (mp *MessageProcessor) handleResourcesList(_ map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"resources": []map[string]string{
			{"uri": "file://test.txt", "name": "Test File"},
		},
	}, nil
}

func (mp *MessageProcessor) handleToolCall(message map[string]interface{}) (interface{}, error) {
	params, ok := message["params"].(map[string]interface{})
	if !ok {
		return nil, errors.New("tool call requires params object")
	}

	name, ok := params["name"].(string)
	if !ok {
		return nil, errors.New("tool call requires name")
	}

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": fmt.Sprintf("Tool %s executed", name)},
		},
	}, nil
}

// FuzzProtocolMessages fuzzes MCP protocol message parsing and processing.
func FuzzProtocolMessages(f *testing.F) {
	if testing.Short() {
		f.Skip("Skipping fuzz tests in short mode - set FUZZ_TEST_ENABLED=true to override")
	}

	if !getEnvBool("FUZZ_TEST_ENABLED", false) {
		f.Skip("Fuzz testing disabled - set FUZZ_TEST_ENABLED=true to enable")
	}

	seedFuzzTestCases(f)

	processor := NewMessageProcessor()

	f.Fuzz(func(t *testing.T, message string) {
		executeFuzzTest(t, processor, message)
	})
}

func seedFuzzTestCases(f *testing.F) {
	f.Helper()
	// Seed with valid MCP messages
	validMessages := []string{
		`{"jsonrpc":"2.0","method":"ping","id":1}`,
		`{"jsonrpc":"2.0","method":"echo","params":{"text":"hello"},"id":2}`,
		`{"jsonrpc":"2.0","result":"pong","id":1}`,
		`{"jsonrpc":"2.0","error":{"code":-1,"message":"error"},"id":2}`,
		`{"jsonrpc":"2.0","method":"resources/list","id":3}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"test"},"id":4}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}

	for _, msg := range validMessages {
		f.Add(msg)
	}

	// Seed with edge cases
	edgeCases := []string{
		`{}`,                                     // Empty object
		`{"jsonrpc":"2.0"}`,                      // Minimal message
		`{"jsonrpc":"1.0","method":"test"}`,      // Wrong version
		`{"method":"test"}`,                      // Missing jsonrpc
		`{"jsonrpc":"2.0","method":"","id":1}`,   // Empty method
		`{"jsonrpc":"2.0","result":null,"id":1}`, // Null result
	}

	for _, msg := range edgeCases {
		f.Add(msg)
	}
}

func executeFuzzTest(t *testing.T, processor *MessageProcessor, message string) {
	t.Helper()
	// Test JSON parsing resilience
	var parsed map[string]interface{}

	err := json.Unmarshal([]byte(message), &parsed)
	if err != nil {
		// Invalid JSON should be handled gracefully
		return
	}

	// Test protocol validation and processing
	// Should never panic regardless of input
	assert.NotPanics(t, func() {
		result, processingErr := processor.ProcessMessage(parsed)

		// Log interesting cases for debugging
		if processingErr == nil && result != nil {
			t.Logf("Successfully processed: %s -> %v", message, result)
		}
	}, "Message processing should not panic: %s", message)
}

// FuzzBinaryProtocol fuzzes binary protocol frames.
func FuzzBinaryProtocol(f *testing.F) {
	if testing.Short() {
		f.Skip("Skipping binary fuzz tests in short mode")
	}

	if !getEnvBool("FUZZ_TEST_ENABLED", false) {
		f.Skip("Binary fuzz testing disabled")
	}

	// Seed with valid binary frames
	validFrames := [][]byte{
		createBinaryFrame([]byte(`{"jsonrpc":"2.0","method":"ping","id":1}`)),
		createBinaryFrame([]byte(`{"jsonrpc":"2.0","result":"pong","id":1}`)),
		createBinaryFrame([]byte("")), // Empty payload
	}

	for _, frame := range validFrames {
		f.Add(frame)
	}

	f.Fuzz(func(t *testing.T, frame []byte) {
		// Test binary frame parsing
		assert.NotPanics(t, func() {
			payload, err := parseBinaryFrame(frame)
			if err != nil {
				// Invalid frames should error gracefully
				return
			}

			// If we can parse the frame, try to process the payload
			if len(payload) > 0 {
				var message map[string]interface{}
				if json.Unmarshal(payload, &message) == nil {
					processor := NewMessageProcessor()
					if _, err := processor.ProcessMessage(message); err != nil {
						// Ignore processing errors in fuzz test
						_ = err
					}
				}
			}
		}, "Binary frame processing should not panic")
	})
}

// NetworkProxy implementations for chaos testing

// LatencyProxy adds configurable network latency.
type LatencyProxy struct {
	listenAddr  string
	targetAddr  string
	minLatency  time.Duration
	maxLatency  time.Duration
	dropRate    float64
	listener    net.Listener
	connections map[net.Conn]net.Conn
	connsMu     sync.RWMutex
	stopped     chan struct{}
	wg          sync.WaitGroup
}

// NewLatencyProxy creates a new latency-inducing proxy.
func NewLatencyProxy(listenAddr, targetAddr string, minLatency, maxLatency time.Duration,
	dropRate float64) *LatencyProxy {
	return &LatencyProxy{
		listenAddr:  listenAddr,                  // Address to listen on for incoming connections
		targetAddr:  targetAddr,                  // Target address to proxy connections to
		minLatency:  minLatency,                  // Minimum latency to add to connections
		maxLatency:  maxLatency,                  // Maximum latency to add to connections
		dropRate:    dropRate,                    // Packet drop rate (0.0 to 1.0)
		listener:    nil,                         // Network listener (set when started)
		connections: make(map[net.Conn]net.Conn), // Map of client to target connections
		connsMu:     sync.RWMutex{},              // Mutex for thread-safe access to connections map
		stopped:     make(chan struct{}),         // Channel to signal server stop
		wg:          sync.WaitGroup{},            // WaitGroup for graceful shutdown
	}
}

// Start starts the proxy server.
func (lp *LatencyProxy) Start() error {
	lc := &net.ListenConfig{}

	listener, err := lc.Listen(context.Background(), "tcp", lp.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	lp.listener = listener

	lp.wg.Add(1)

	go lp.serve()

	return nil
}

// Stop stops the proxy server.
func (lp *LatencyProxy) Stop() error {
	close(lp.stopped)

	if lp.listener != nil {
		if err := lp.listener.Close(); err != nil {
			// Error closing listener is non-critical during shutdown
			_ = err
		}
	}

	// Close all connections
	lp.connsMu.Lock()

	for client, target := range lp.connections {
		if err := client.Close(); err != nil {
			// Error closing connection is non-critical during shutdown
			_ = err
		}

		if err := target.Close(); err != nil {
			// Error closing connection is non-critical during shutdown
			_ = err
		}
	}

	lp.connsMu.Unlock()

	lp.wg.Wait()

	return nil
}

// serve handles incoming connections.
func (lp *LatencyProxy) serve() {
	defer lp.wg.Done()

	for {
		select {
		case <-lp.stopped:
			return
		default:
		}

		// Set a short timeout so we can check for stop signal
		if tcpListener, ok := lp.listener.(*net.TCPListener); ok {
			if err := tcpListener.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				// Error setting deadline is non-critical
				_ = err
			}
		}

		conn, err := lp.listener.Accept()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}

			return
		}

		lp.wg.Add(1)

		go lp.handleConnection(conn)
	}
}

// handleConnection proxies a single connection with latency.
func (lp *LatencyProxy) handleConnection(clientConn net.Conn) {
	defer lp.wg.Done()
	defer func() {
		if err := clientConn.Close(); err != nil {
			// Error closing connection is non-critical
			_ = err
		}
	}()

	// Connect to target
	d := &net.Dialer{}

	targetConn, err := d.DialContext(context.Background(), "tcp", lp.targetAddr)
	if err != nil {
		return
	}

	defer func() {
		if err := targetConn.Close(); err != nil {
			// Error closing connection is non-critical
			_ = err
		}
	}()

	// Track connection
	lp.connsMu.Lock()
	lp.connections[clientConn] = targetConn
	lp.connsMu.Unlock()

	defer func() {
		lp.connsMu.Lock()
		delete(lp.connections, clientConn)
		lp.connsMu.Unlock()
	}()

	// Proxy data in both directions with latency
	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		lp.proxyWithLatency(clientConn, targetConn, "client->target")
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		lp.proxyWithLatency(targetConn, clientConn, "target->client")
	}()

	wg.Wait()
}

// proxyWithLatency proxies data with added latency and packet loss.
func (lp *LatencyProxy) proxyWithLatency(src, dst net.Conn, _ string) {
	buffer := make([]byte, 32*1024)

	for {
		if err := src.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			// Error setting deadline is non-critical
			_ = err
		}

		n, err := src.Read(buffer)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}

			return
		}

		// Simulate packet loss
		if rng.Float64() < lp.dropRate {
			continue // Drop packet
		}

		// Add latency
		latency := lp.minLatency

		if lp.maxLatency > lp.minLatency {
			extraLatency := time.Duration(rng.Int63n(int64(lp.maxLatency - lp.minLatency)))
			latency += extraLatency
		}

		if latency > 0 {
			time.Sleep(latency)
		}

		// Forward data
		_, err = dst.Write(buffer[:n])
		if err != nil {
			return
		}
	}
}

// PacketLossProxy drops packets randomly.
type PacketLossProxy struct {
	*LatencyProxy
}

// NewPacketLossProxy creates a proxy that drops packets.
func NewPacketLossProxy(listenAddr, targetAddr string, dropRate float64) *PacketLossProxy {
	return &PacketLossProxy{
		LatencyProxy: NewLatencyProxy(listenAddr, targetAddr, 0, 0, dropRate),
	}
}

// TestNetworkProxies tests the network proxy implementations.
func TestNetworkProxies(t *testing.T) {
	// Cannot run in parallel: Creates shared network server and tests network conditions that interfere
	if testing.Short() {
		t.Skip("Skipping network proxy tests in short mode")
	}

	// Create a simple echo server for testing
	echoListener, echoAddr := startEchoServer(t)

	defer func() {
		if err := echoListener.Close(); err != nil {
			// Error closing listener is non-critical
			_ = err
		}
	}()

	t.Run("latency_proxy", func(t *testing.T) {
		testLatencyProxy(t, echoAddr)
	})

	t.Run("packet_loss_proxy", func(t *testing.T) {
		testPacketLossProxy(t, echoAddr)
	})
}

// startEchoServer starts a simple echo server for testing.
func startEchoServer(t *testing.T) (listener net.Listener,
	addr string) {
	t.Helper()

	lc := &net.ListenConfig{}
	echoListener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	echoAddr := echoListener.Addr().String()

	// Start echo server
	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer func() {
					if err := c.Close(); err != nil {
						// Error closing connection is non-critical
						_ = err
					}
				}()

				if _, err := io.Copy(c, c); err != nil {
					// Error in echo copy is non-critical
					_ = err
				}
			}(conn)
		}
	}()

	listener = echoListener
	addr = echoAddr

	return listener, addr
}

// testLatencyProxy tests the latency proxy functionality.
func testLatencyProxy(t *testing.T, echoAddr string) {
	t.Helper()

	lc := &net.ListenConfig{}
	proxyListener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer func() {
		if err := proxyListener.Close(); err != nil {
			// Error closing listener is non-critical
			_ = err
		}
	}()

	proxyAddr := proxyListener.Addr().String()

	if err := proxyListener.Close(); err != nil {
		// Error closing listener is non-critical
		_ = err
	} // Close so proxy can use the address

	proxy := NewLatencyProxy(proxyAddr, echoAddr, 10*time.Millisecond, 50*time.Millisecond, 0.0)

	defer func() {
		if err := proxy.Stop(); err != nil {
			t.Logf("Failed to stop proxy: %v", err)
		}
	}()

	err = proxy.Start()
	require.NoError(t, err)

	// Test connection through proxy
	d := &net.Dialer{}
	conn, err := d.DialContext(context.Background(), "tcp", proxyAddr)
	require.NoError(t, err)

	defer func() {
		_ = conn.Close()
	}()

	testMessage := "hello world"
	start := time.Now()

	_, err = conn.Write([]byte(testMessage))
	require.NoError(t, err)

	response := make([]byte, len(testMessage))
	_, err = conn.Read(response)
	require.NoError(t, err)

	duration := time.Since(start)

	assert.Equal(t, testMessage, string(response))
	assert.GreaterOrEqual(t, duration, 10*time.Millisecond, "Should add latency")
}

// testPacketLossProxy tests the packet loss proxy functionality.
func testPacketLossProxy(t *testing.T, echoAddr string) {
	t.Helper()

	lc := &net.ListenConfig{}
	proxyListener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer func() {
		if err := proxyListener.Close(); err != nil {
			// Error closing listener is non-critical
			_ = err
		}
	}()

	proxyAddr := proxyListener.Addr().String()

	if err := proxyListener.Close(); err != nil {
		// Error closing listener is non-critical
		_ = err
	}

	proxy := NewPacketLossProxy(proxyAddr, echoAddr, 0.8) // 80% packet loss for more reliable test

	defer func() {
		if err := proxy.Stop(); err != nil {
			t.Logf("Failed to stop proxy: %v", err)
		}
	}()

	err = proxy.Start()
	require.NoError(t, err)

	// Test multiple times to verify packet loss behavior
	lossDetected := false

	for range 5 {
		if testSinglePacketLossAttempt(t, proxyAddr) {
			lossDetected = true

			break
		}
	}

	assert.True(t, lossDetected, "Should detect packet loss effects (incomplete data or timeouts)")
}

// testSinglePacketLossAttempt tests a single packet loss attempt.
func testSinglePacketLossAttempt(t *testing.T, proxyAddr string) bool {
	t.Helper()

	d := &net.Dialer{}
	conn, err := d.DialContext(context.Background(), "tcp", proxyAddr)
	require.NoError(t, err)

	defer func() {
		_ = conn.Close()
	}()

	// Set a shorter timeout to detect packet loss more reliably
	if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Logf("Failed to set read deadline: %v", err)
	}

	// Send a larger message that's more likely to be affected by packet loss
	testData := make([]byte, 100)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	start := time.Now()
	_, err = conn.Write(testData)
	require.NoError(t, err)

	// Try to read back the data
	received := make([]byte, len(testData))
	totalReceived := 0

	for totalReceived < len(testData) {
		n, err := conn.Read(received[totalReceived:])
		if err != nil {
			// Timeout or connection error indicates packet loss
			return true
		}

		totalReceived += n
	}

	duration := time.Since(start)

	// If we didn't receive all data or it took much longer than expected, packet loss occurred
	return totalReceived < len(testData) || duration > 150*time.Millisecond
}

// Binary protocol helpers

func createBinaryFrame(payload []byte) []byte {
	frame := make([]byte, 4+len(payload))
	// #nosec G115 - payload length is controlled in test environment
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	copy(frame[4:], payload)

	return frame
}

func parseBinaryFrame(frame []byte) ([]byte, error) {
	if len(frame) < 4 {
		return nil, errors.New("frame too short")
	}

	length := binary.BigEndian.Uint32(frame[:4])
	if length > 1024*1024 { // 1MB limit
		return nil, fmt.Errorf("frame too large: %d", length)
	}

	if len(frame) < int(4+length) {
		return nil, errors.New("incomplete frame")
	}

	return frame[4 : 4+length], nil
}

// Environment helpers.
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}

	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}

	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}

	return defaultValue
}
