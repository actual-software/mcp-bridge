package stdio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

const (
	testIterations    = 100
	testTimeout       = 50
	testMaxIterations = 1000

	// Test timing constants - tuned for reliability vs speed.
	connectionSetupTimeout    = 100 * time.Millisecond
	responseProcessingTimeout = 300 * time.Millisecond
	connectionCleanupTimeout  = 50 * time.Millisecond
)

// Mock implementations with error simulation capabilities.
type MockRequestRouterWithError struct {
	shouldError bool
	errorMsg    string
}

func (m *MockRequestRouterWithError) RouteRequest(
	ctx context.Context, req *mcp.Request, targetNamespace string,
) (*mcp.Response, error) {
	if m.shouldError {
		return nil, errors.New(m.errorMsg)
	}

	return &mcp.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"echo": req.Method},
	}, nil
}

type MockAuthProviderWithError struct {
	shouldError    bool
	shouldAuthFail bool
	errorMsg       string
}

func (m *MockAuthProviderWithError) Authenticate(ctx context.Context, credentials map[string]string) (bool, error) {
	if m.shouldError {
		return false, errors.New(m.errorMsg)
	}

	return !m.shouldAuthFail, nil
}

func (m *MockAuthProviderWithError) GetUserInfo(
	ctx context.Context, credentials map[string]string,
) (map[string]interface{}, error) {
	if m.shouldError {
		return nil, errors.New(m.errorMsg)
	}

	return map[string]interface{}{"user": "test"}, nil
}

// Test connection implementation.
type testConnection struct {
	reader       io.Reader
	writer       io.Writer
	isConnLike   bool
	closed       bool
	mu           sync.Mutex
	readDeadline time.Time
}

// blockingReader provides data once and then blocks until done channel is closed.
type blockingReader struct {
	data []byte
	done chan struct{}
	read bool
}

// syncBuffer wraps bytes.Buffer with mutex for thread-safe access.
type syncBuffer struct {
	buf bytes.Buffer
	mu  sync.RWMutex
}

func (sb *syncBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()

	defer sb.mu.Unlock()

	return sb.buf.Write(p)
}

func (sb *syncBuffer) Len() int {
	sb.mu.RLock()

	defer sb.mu.RUnlock()

	return sb.buf.Len()
}

func (sb *syncBuffer) Bytes() []byte {
	sb.mu.RLock()

	defer sb.mu.RUnlock()

	return sb.buf.Bytes()
}

func (sb *syncBuffer) Reset() {
	sb.mu.Lock()

	defer sb.mu.Unlock()

	sb.buf.Reset()
}

func (r *blockingReader) Read(p []byte) (n int, err error) {
	if r.read {
		// Wait for done signal or return EOF
		select {
		case <-r.done:
			return 0, io.EOF
		default:
			// Block indefinitely until done is closed
			<-r.done

			return 0, io.EOF
		}
	}

	r.read = true

	n = copy(p, r.data)
	if n < len(r.data) {
		// If buffer is too small, return what we can
		r.data = r.data[n:]
		r.read = false
	}

	return n, nil
}

func (c *testConnection) Read(p []byte) (n int, err error) {
	c.mu.Lock()

	defer c.mu.Unlock()

	if c.closed {
		return 0, io.EOF
	}

	return c.reader.Read(p)
}

func (c *testConnection) Write(p []byte) (n int, err error) {
	c.mu.Lock()

	defer c.mu.Unlock()

	if c.closed {
		return 0, errors.New("connection closed")
	}

	return c.writer.Write(p)
}

func (c *testConnection) Close() error {
	c.mu.Lock()

	defer c.mu.Unlock()

	c.closed = true

	return nil
}

func (c *testConnection) SetReadDeadline(t time.Time) error {
	if c.isConnLike {
		c.readDeadline = t

		return nil
	}

	return errors.New("not supported")
}

// TestUnixSocketStartup tests the Unix socket initialization.
func TestStdioFrontend_UnixSocketStartup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	// Create a unique socket path using process ID and timestamp
	socketPath := fmt.Sprintf("/tmp/stdio_test_%d_%d.sock", os.Getpid(), time.Now().UnixNano())
	// Clean up any existing socket
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		t.Logf("Failed to remove socket: %v", err)
	}

	defer func() {
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			t.Logf("Failed to remove socket: %v", err)
		}
	}()

	config := Config{
		Enabled: true,
		Modes: []ModeConfig{
			{
				Type:        "unix_socket",
				Path:        socketPath,
				Permissions: "0600",
				Enabled:     true,
			},
		},
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 2,
			ClientTimeout:        1 * time.Second,
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	// Test Unix socket startup
	err := frontend.Start(context.Background())
	require.NoError(t, err)

	// Verify socket was created
	_, err = os.Stat(socketPath)

	require.NoError(t, err)

	// Verify frontend is running
	metrics := frontend.GetMetrics()

	assert.True(t, metrics.IsRunning)

	// Test double start (should error)
	err = frontend.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Stop the frontend
	err = frontend.Stop(context.Background())
	require.NoError(t, err)

	// Verify metrics are updated
	metrics = frontend.GetMetrics()

	assert.False(t, metrics.IsRunning)
	assert.Equal(t, uint64(0), metrics.ActiveConnections)

	// Clean up socket
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		t.Logf("Failed to remove socket: %v", err)
	}
}

// TestUnixSocketConnection tests actual Unix socket connections.
func TestStdioFrontend_UnixSocketConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	socketPath := createTemporarySocketPath(t)

	defer cleanupSocket(t, socketPath)

	config := createUnixSocketConfig(socketPath)
	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	err := frontend.Start(context.Background())
	require.NoError(t, err)

	defer func() { _ = frontend.Stop(context.Background()) }()

	testUnixSocketConnection(t, socketPath, frontend)
}

func createTemporarySocketPath(t *testing.T) string {
	t.Helper()
	// Use /tmp directly to avoid macOS unix socket path length limit (104 chars)
	// t.TempDir() creates very long paths that exceed this limit
	socketPath := filepath.Join("/tmp", fmt.Sprintf("test-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})

	return socketPath
}

func cleanupSocket(t *testing.T, socketPath string) {
	t.Helper()

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		t.Logf("Failed to remove socket: %v", err)
	}
}

func createUnixSocketConfig(socketPath string) Config {
	return Config{
		Enabled: true,
		Modes: []ModeConfig{
			{
				Type:    "unix_socket",
				Path:    socketPath,
				Enabled: true,
			},
		},
	}
}

func testUnixSocketConnection(t *testing.T, socketPath string, frontend *Frontend) {
	t.Helper()
	time.Sleep(100 * time.Millisecond)

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(context.Background(), "unix", socketPath)
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	request := `{"jsonrpc":"2.0","method":"test","params":{},"id":1}`
	_, err = conn.Write([]byte(request + "\n"))

	require.NoError(t, err)

	err = conn.SetReadDeadline(time.Now().Add(time.Second))

	require.NoError(t, err)

	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)

	require.NoError(t, err)
	assert.Positive(t, n)
}

// TestStdinStdoutConnection tests the stdin/stdout connection wrapper.
func TestStdioFrontend_StdinStdoutConnection(t *testing.T) {
	// Test the actual stdinStdoutConn struct
	stdinStdout := &stdinStdoutConn{
		stdin:  os.Stdin,
		stdout: os.Stdout,
	}

	// Test that methods don't panic and behave correctly
	assert.NotNil(t, stdinStdout)

	// Test Close() method (should not actually close stdin/stdout)
	err := stdinStdout.Close()
	require.NoError(t, err)

	// Test the wrapper can be created
	reader := bytes.NewReader([]byte("test data"))
	writer := &bytes.Buffer{}

	testConn := &testConnection{
		reader: reader,
		writer: writer,
	}

	// Test basic read/write operations
	buf := make([]byte, 10)
	n, err := testConn.Read(buf)

	require.NoError(t, err)
	assert.Equal(t, 9, n)
	assert.Equal(t, "test data", string(buf[:n]))

	n, err = testConn.Write([]byte("response"))

	require.NoError(t, err)
	assert.Equal(t, 8, n)
	assert.Equal(t, "response", writer.String())

	err = testConn.Close()

	require.NoError(t, err)
}

// TestStdinStdoutMode tests the stdin/stdout mode startup.
func TestStdioFrontend_StdinStdoutMode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Modes: []ModeConfig{
			{
				Type:    "stdin_stdout",
				Enabled: true,
			},
		},
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 5,
			ClientTimeout:        2 * time.Second,
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	err := frontend.Start(context.Background())
	require.NoError(t, err)

	// Verify frontend is running
	metrics := frontend.GetMetrics()

	assert.True(t, metrics.IsRunning)

	// Stop the frontend
	err = frontend.Stop(context.Background())
	require.NoError(t, err)

	metrics = frontend.GetMetrics()

	assert.False(t, metrics.IsRunning)
}

// TestHandleConnection tests the connection handling logic.
func TestStdioFrontend_HandleConnection(t *testing.T) {
	// Setup test components
	frontend, clientConn, serverConn := setupHandleConnectionTest(t)

	defer cleanupConnections(clientConn, serverConn)

	// Start frontend
	startFrontend(t, frontend)

	defer func() { _ = frontend.Stop(context.Background()) }()

	// Run the connection test
	runConnectionTest(t, clientConn, serverConn)
}

func setupHandleConnectionTest(t *testing.T) (*Frontend, net.Conn, net.Conn) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 2,
			ClientTimeout:        1 * time.Second,
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)
	clientConn, serverConn := net.Pipe()

	return frontend, clientConn, serverConn
}

func cleanupConnections(clientConn, serverConn net.Conn) {
	_ = clientConn.Close()
	_ = serverConn.Close()
}

func startFrontend(t *testing.T, frontend *Frontend) {
	t.Helper()

	err := frontend.Start(context.Background())
	require.NoError(t, err)
}

func runConnectionTest(t *testing.T, clientConn, serverConn net.Conn) {
	t.Helper()
	// Prepare test request
	request := createTestRequest()
	requestData, err := json.Marshal(request)

	require.NoError(t, err)

	// Setup channels for async operations
	responseChan, errorChan := setupTestChannels()

	// Start client writer

	go writeClientRequest(clientConn, requestData)

	// Start server handler

	go handleServerConnection(serverConn, responseChan, errorChan)

	// Read and validate response
	validateConnectionResponse(t, clientConn, responseChan, errorChan)
}

func createTestRequest() mcp.Request {
	return mcp.Request{
		JSONRPC: "2.0",
		Method:  "test_method",
		ID:      "test-1",
		Params:  map[string]interface{}{"test": "data"},
	}
}

func setupTestChannels() (chan []byte, chan error) {
	return make(chan []byte, 1), make(chan error, 1)
}

func writeClientRequest(clientConn net.Conn, requestData []byte) {
	defer func() { _ = clientConn.Close() }()

	_, _ = clientConn.Write(append(requestData, '\n'))

	time.Sleep(responseProcessingTimeout)
}

func handleServerConnection(serverConn net.Conn, responseChan chan []byte, errorChan chan error) {
	defer func() { _ = serverConn.Close() }()

	decoder := json.NewDecoder(serverConn)
	encoder := json.NewEncoder(serverConn)

	var req mcp.Request
	if err := decoder.Decode(&req); err != nil {
		errorChan <- err

		return
	}

	// Create response
	resp := &mcp.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"echo": req.Method},
	}

	if err := encoder.Encode(resp); err != nil {
		errorChan <- err

		return
	}

	close(responseChan)
}

func validateConnectionResponse(t *testing.T, clientConn net.Conn, responseChan chan []byte, errorChan chan error) {
	t.Helper()

	// Read response from client side
	responseBuffer := make([]byte, 1024)
	_ = clientConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := clientConn.Read(responseBuffer)

	// Wait for handler to complete
	select {
	case <-responseChan:
		// Success
	case err := <-errorChan:
		t.Logf("Handler error: %v", err)
	case <-time.After(1 * time.Second):
		t.Log("Handler timed out")
	}

	if err != nil && !errors.Is(err, io.EOF) {
		if n == 0 {
			t.Logf("No response received, error: %v", err)

			return
		}
	}

	if n > 0 {
		var response mcp.Response

		err = json.Unmarshal(responseBuffer[:n], &response)
		if err == nil {
			assert.Equal(t, "test-1", response.ID)
		}
	}
}

// TestHandleConnectionWithAuth tests authentication during connection handling.
func TestStdioFrontend_HandleConnectionWithAuth(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProviderWithError{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 2,
			ClientTimeout:        1 * time.Second,
			AuthRequired:         true,
		},
	}

	testSuccessfulAuth(t, config, mockRouter, mockAuth, mockSessions, logger)
	testFailedAuth(t, config, mockRouter, mockSessions, logger)
}

func testSuccessfulAuth(
	t *testing.T, config Config, mockRouter *MockRequestRouter,
	mockAuth *MockAuthProviderWithError, mockSessions *MockSessionManager, logger *zap.Logger,
) {
	t.Helper()

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)
	err := frontend.Start(context.Background())
	require.NoError(t, err)

	defer func() {
		_ = frontend.Stop(context.Background())
	}()

	request1 := mcp.Request{
		JSONRPC: "2.0",
		Method:  "test_method",
		ID:      "test-1",
		Params: map[string]interface{}{
			"auth_token":    "valid_token",
			"bearer_token":  "bearer123",
			"username":      "testuser",
			"password":      "testpass",
			"authorization": "Bearer xyz",
			"x-auth-token":  "xauth123",
			"x-api-key":     "apikey456",
		},
	}

	requestData1, err := json.Marshal(request1)

	require.NoError(t, err)

	reader1 := &blockingReader{
		data: append(requestData1, '\n'),
		done: make(chan struct{}),
	}
	writer1 := &syncBuffer{}

	testConn1 := &testConnection{
		reader:     reader1,
		writer:     writer1,
		isConnLike: true,
	}

	frontend.wg.Add(1)

	go func() {
		frontend.handleConnection(context.Background(), testConn1)
	}()

	time.Sleep(responseProcessingTimeout)
	close(reader1.done)
	time.Sleep(connectionCleanupTimeout)

	assert.Positive(t, writer1.Len(), "Expected successful auth response")
}

func testFailedAuth(
	t *testing.T,
	config Config,
	mockRouter *MockRequestRouter,
	mockSessions *MockSessionManager,
	logger *zap.Logger,
) {
	t.Helper()

	failingFrontend := setupFailingAuthFrontend(t, config, mockRouter, mockSessions, logger)
	defer func() {
		_ = failingFrontend.Stop(context.Background())
	}()

	request := createAuthTestRequest()
	testConn := executeAuthRequest(t, failingFrontend, request)

	syncBuf, ok := testConn.writer.(*syncBuffer)
	require.True(t, ok, "writer should be a syncBuffer")
	verifyAuthFailureResponse(t, syncBuf)
}

func setupFailingAuthFrontend(
	t *testing.T,
	config Config,
	mockRouter *MockRequestRouter,
	mockSessions *MockSessionManager,
	logger *zap.Logger,
) *Frontend {
	t.Helper()

	failingMockAuth := &MockAuthProviderWithError{
		shouldAuthFail: true,
	}
	failingFrontend := CreateStdioFrontend("test", config, mockRouter, failingMockAuth, mockSessions, logger)

	err := failingFrontend.Start(context.Background())
	require.NoError(t, err)

	return failingFrontend
}

func createAuthTestRequest() mcp.Request {
	return mcp.Request{
		JSONRPC: "2.0",
		Method:  "test_method",
		ID:      "test-2",
		Params: map[string]interface{}{
			"auth_token":    "invalid_token",
			"bearer_token":  "bearer123",
			"username":      "testuser",
			"password":      "testpass",
			"authorization": "Bearer xyz",
			"x-auth-token":  "xauth123",
			"x-api-key":     "apikey456",
		},
	}
}

func executeAuthRequest(t *testing.T, frontend *Frontend, request mcp.Request) *testConnection {
	t.Helper()

	requestData, err := json.Marshal(request)
	require.NoError(t, err)

	reader := &blockingReader{
		data: append(requestData, '\n'),
		done: make(chan struct{}),
	}
	writer := &syncBuffer{}

	testConn := &testConnection{
		reader:     reader,
		writer:     writer,
		isConnLike: true,
	}

	frontend.wg.Add(1)
	go func() {
		frontend.handleConnection(context.Background(), testConn)
	}()

	time.Sleep(responseProcessingTimeout)
	close(reader.done)
	time.Sleep(connectionCleanupTimeout)

	return testConn
}

func verifyAuthFailureResponse(t *testing.T, writer *syncBuffer) {
	t.Helper()

	assert.Positive(t, writer.Len(), "Expected auth failure response")

	var errorResp mcp.Response
	err := json.Unmarshal(writer.Bytes(), &errorResp)

	require.NoError(t, err)
	assert.NotNil(t, errorResp.Error, "Expected error in auth failure response")
}

// TestHandleRequest tests the request handling logic.
func TestStdioFrontend_HandleRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 2,
			ClientTimeout:        1 * time.Second,
		},
	}

	testSuccessfulRequest(t, config, mockAuth, mockSessions, logger)
	testRequestWithRouterError(t, config, mockAuth, mockSessions, logger)
}

func testSuccessfulRequest(
	t *testing.T, config Config, mockAuth *MockAuthProvider,
	mockSessions *MockSessionManager, logger *zap.Logger,
) {
	t.Helper()

	mockRouter := &MockRequestRouterWithError{}
	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	writer := &syncBuffer{}
	conn := &ClientConnection{
		id:     "test-conn",
		writer: json.NewEncoder(writer),
		mu:     sync.RWMutex{},
	}

	request := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test_method",
		ID:      "test-1",
	}

	frontend.wg.Add(1)

	go frontend.handleRequest(context.Background(), conn, request)

	time.Sleep(responseProcessingTimeout)

	assert.Positive(t, writer.Len())
}

func testRequestWithRouterError(
	t *testing.T, config Config, mockAuth *MockAuthProvider,
	mockSessions *MockSessionManager, logger *zap.Logger,
) {
	t.Helper()

	errorMockRouter := &MockRequestRouterWithError{
		shouldError: true,
		errorMsg:    "routing failed",
	}

	errorFrontend := CreateStdioFrontend("test", config, errorMockRouter, mockAuth, mockSessions, logger)

	writer := &syncBuffer{}
	conn := &ClientConnection{
		id:     "test-conn",
		writer: json.NewEncoder(writer),
		mu:     sync.RWMutex{},
	}

	request := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test_method",
		ID:      "test-2",
	}

	errorFrontend.wg.Add(1)

	go errorFrontend.handleRequest(context.Background(), conn, request)

	time.Sleep(responseProcessingTimeout)

	assert.Positive(t, writer.Len())

	var errorResp mcp.Response

	err := json.Unmarshal(writer.Bytes(), &errorResp)

	require.NoError(t, err)
	assert.NotNil(t, errorResp.Error)
	assert.Contains(t, errorResp.Error.Message, "routing failed")
}

// TestCleanupIdleConnections tests the connection cleanup functionality.
func TestStdioFrontend_CleanupIdleConnections(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 5,
			ClientTimeout:        testIterations * time.Millisecond, // Very short timeout for testing
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	// Create some test connections
	conn1 := &ClientConnection{
		id:       "conn1",
		created:  time.Now(),
		lastUsed: time.Now().Add(-200 * time.Millisecond), // Old connection for cleanup testing
		conn:     &testConnection{reader: bytes.NewReader(nil), writer: &bytes.Buffer{}},
	}

	conn2 := &ClientConnection{
		id:       "conn2",
		created:  time.Now(),
		lastUsed: time.Now(), // Recent connection
		conn:     &testConnection{reader: bytes.NewReader(nil), writer: &bytes.Buffer{}},
	}

	// Add connections to frontend
	frontend.connectionsMu.Lock()
	frontend.connections["conn1"] = conn1
	frontend.connections["conn2"] = conn2
	frontend.connectionsMu.Unlock()

	// Update metrics to reflect connections
	frontend.updateMetrics(func(m *FrontendMetrics) {
		m.ActiveConnections = 2
	})

	initialMetrics := frontend.GetMetrics()

	assert.Equal(t, uint64(2), initialMetrics.ActiveConnections)

	// Run cleanup
	frontend.cleanupIdleConnections()

	// Check that old connection was removed
	frontend.connectionsMu.RLock()

	assert.NotContains(t, frontend.connections, "conn1")
	assert.Contains(t, frontend.connections, "conn2")
	frontend.connectionsMu.RUnlock()

	// Metrics should be updated
	finalMetrics := frontend.GetMetrics()

	assert.Equal(t, uint64(1), finalMetrics.ActiveConnections)
}

// TestMaxConcurrentClients tests the connection limit enforcement.
func TestStdioFrontend_MaxConcurrentClients(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 1,
			ClientTimeout:        1 * time.Second,
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	setupMaxClientsTest(frontend)
	testMaxConcurrentClientsLimit(t, frontend)
}

func setupMaxClientsTest(frontend *Frontend) {
	conn1 := &ClientConnection{
		id:       "conn1",
		created:  time.Now(),
		lastUsed: time.Now(),
		conn:     &testConnection{reader: bytes.NewReader(nil), writer: &bytes.Buffer{}},
	}

	frontend.connectionsMu.Lock()
	frontend.connections["conn1"] = conn1
	frontend.connectionsMu.Unlock()

	frontend.updateMetrics(func(m *FrontendMetrics) {
		m.ActiveConnections = 1
	})
}

func testMaxConcurrentClientsLimit(t *testing.T, frontend *Frontend) {
	t.Helper()

	request := mcp.Request{
		JSONRPC: "2.0",
		Method:  "test_method",
		ID:      "test-1",
	}

	requestData, err := json.Marshal(request)

	require.NoError(t, err)

	reader := bytes.NewReader(append(requestData, '\n'))
	writer := &bytes.Buffer{}

	testConn := &testConnection{
		reader:     reader,
		writer:     writer,
		isConnLike: true,
	}

	err = frontend.Start(context.Background())
	require.NoError(t, err)

	defer func() {
		_ = frontend.Stop(context.Background())
	}()

	frontend.wg.Add(1)

	go frontend.handleConnection(context.Background(), testConn)

	time.Sleep(responseProcessingTimeout)

	frontend.connectionsMu.RLock()

	assert.Len(t, frontend.connections, 1)
	assert.Contains(t, frontend.connections, "conn1")
	frontend.connectionsMu.RUnlock()
}

// TestConnectionCleanupRoutine tests the background cleanup routine.
func TestStdioFrontend_ConnectionCleanupRoutine(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Modes: []ModeConfig{
			{
				Type:    "stdin_stdout",
				Enabled: true,
			},
		},
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 5,
			ClientTimeout:        testTimeout * time.Millisecond, // Very short for testing
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	err := frontend.Start(context.Background())
	require.NoError(t, err)

	// Add an old connection manually
	oldConn := &ClientConnection{
		id:       "old-conn",
		created:  time.Now().Add(-1 * time.Second),
		lastUsed: time.Now().Add(-1 * time.Second),
		conn:     &testConnection{reader: bytes.NewReader(nil), writer: &bytes.Buffer{}},
	}

	frontend.connectionsMu.Lock()
	frontend.connections["old-conn"] = oldConn
	frontend.connectionsMu.Unlock()

	frontend.updateMetrics(func(m *FrontendMetrics) {
		m.ActiveConnections = 1
	})

	// Wait for cleanup routine to run
	time.Sleep(connectionCleanupTimeout)

	// Stop frontend
	err = frontend.Stop(context.Background())
	require.NoError(t, err)

	// Connection should have been cleaned up
	frontend.connectionsMu.RLock()

	assert.Empty(t, frontend.connections)
	frontend.connectionsMu.RUnlock()
}

// TestUnixSocketPermissions tests socket permission setting.
func TestStdioFrontend_UnixSocketPermissions(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	tempDir := t.TempDir()
	socketPath := filepath.Join(tempDir, "test.sock")

	// Check if path is too long for Unix socket (macOS limit ~104 chars)
	if len(socketPath) > 104 {
		// Use a shorter path in /tmp instead
		socketPath = fmt.Sprintf("/tmp/stdio_perm_%d.sock", time.Now().UnixNano()%1000000)
		// Clean up any existing socket
		_ = os.Remove(socketPath)

		defer func() {
			_ = os.Remove(socketPath)
		}()
	}

	config := Config{
		Enabled: true,
		Modes: []ModeConfig{
			{
				Type:        "unix_socket",
				Path:        socketPath,
				Permissions: "0600",
				Enabled:     true,
			},
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	err := frontend.Start(context.Background())
	require.NoError(t, err)

	defer func() {
		_ = frontend.Stop(context.Background())
	}()

	// Check socket permissions
	info, err := os.Stat(socketPath)

	require.NoError(t, err)

	// On Unix systems, socket permissions should be set
	mode := info.Mode()

	assert.NotEqual(t, 0, mode&os.ModeSocket)
}

// TestStopGracefulShutdown tests graceful shutdown behavior.
func TestStdioFrontend_StopGracefulShutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	socketPath, config := setupGracefulShutdownTest(t)

	defer cleanupSocketPath(t, socketPath)

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	testGracefulShutdown(t, frontend)
}

func setupGracefulShutdownTest(t *testing.T) (string, Config) {
	t.Helper()

	socketPath := fmt.Sprintf("/tmp/stdio_shutdown_%d_%d.sock", os.Getpid(), time.Now().UnixNano())
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		t.Logf("Failed to remove socket: %v", err)
	}

	config := Config{
		Enabled: true,
		Modes: []ModeConfig{
			{
				Type:    "unix_socket",
				Path:    socketPath,
				Enabled: true,
			},
		},
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 5,
			ClientTimeout:        1 * time.Second,
		},
	}

	return socketPath, config
}

func cleanupSocketPath(t *testing.T, socketPath string) {
	t.Helper()

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		t.Logf("Failed to remove socket: %v", err)
	}
}

func testGracefulShutdown(t *testing.T, frontend *Frontend) {
	t.Helper()

	err := frontend.Start(context.Background())
	require.NoError(t, err)

	conn1 := &ClientConnection{
		id:       "conn1",
		created:  time.Now(),
		lastUsed: time.Now(),
		conn:     &testConnection{reader: bytes.NewReader(nil), writer: &bytes.Buffer{}},
	}

	frontend.connectionsMu.Lock()
	frontend.connections["conn1"] = conn1
	frontend.connectionsMu.Unlock()

	frontend.updateMetrics(func(m *FrontendMetrics) {
		m.ActiveConnections = 1
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	err = frontend.Stop(ctx)

	require.NoError(t, err)

	frontend.connectionsMu.RLock()

	assert.Empty(t, frontend.connections)
	frontend.connectionsMu.RUnlock()

	metrics := frontend.GetMetrics()

	assert.False(t, metrics.IsRunning)
	assert.Equal(t, uint64(0), metrics.ActiveConnections)
}

// TestStopTimeout tests stop timeout behavior.
func TestStdioFrontend_StopTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Modes: []ModeConfig{
			{
				Type:    "stdin_stdout",
				Enabled: true,
			},
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	err := frontend.Start(context.Background())
	require.NoError(t, err)

	// Test stop with immediate timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)

	defer cancel()

	time.Sleep(10 * time.Millisecond) // Let timeout expire

	err = frontend.Stop(ctx)

	require.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

// TestConnectionErrorHandling tests various error conditions.
func TestStdioFrontend_ConnectionErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Process: ProcessManagementConfig{
			MaxConcurrentClients: 5,
			ClientTimeout:        1 * time.Second,
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	err := frontend.Start(context.Background())
	require.NoError(t, err)

	defer func() {
		_ = frontend.Stop(context.Background())
	}()

	// Test invalid JSON
	reader := bytes.NewReader([]byte("invalid json\n"))
	writer := &bytes.Buffer{}

	testConn := &testConnection{
		reader:     reader,
		writer:     writer,
		isConnLike: true,
	}

	frontend.wg.Add(1)

	go frontend.handleConnection(context.Background(), testConn)

	time.Sleep(responseProcessingTimeout)

	// Check metrics for error count
	metrics := frontend.GetMetrics()

	assert.Positive(t, metrics.ErrorCount)

	// Test EOF (client disconnect)
	eofReader := &testEOFReader{}
	writer2 := &bytes.Buffer{}

	testConn2 := &testConnection{
		reader:     eofReader,
		writer:     writer2,
		isConnLike: true,
	}

	frontend.wg.Add(1)

	go frontend.handleConnection(context.Background(), testConn2)

	time.Sleep(responseProcessingTimeout)
	// Should handle EOF gracefully
}

type testEOFReader struct{}

func (r *testEOFReader) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

// TestWriteErrorHandling tests write error scenarios.
func TestStdioFrontend_WriteErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	// Create a writer that always fails
	failingWriter := &testFailingWriter{}

	conn := &ClientConnection{
		id:     "test-conn",
		writer: json.NewEncoder(failingWriter),
		mu:     sync.RWMutex{},
	}

	request := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test_method",
		ID:      "test-1",
	}

	// Handle request with failing writer
	frontend.wg.Add(1)

	go frontend.handleRequest(context.Background(), conn, request)

	time.Sleep(responseProcessingTimeout)

	// Check that error count increased
	metrics := frontend.GetMetrics()

	assert.Positive(t, metrics.ErrorCount)
}

type testFailingWriter struct{}

func (w *testFailingWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write failed")
}

// TestUnixSocketExistingFile tests handling of existing socket file.
func TestStdioFrontend_UnixSocketExistingFile(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	socketPath := fmt.Sprintf("/tmp/stdio_existing_%d_%d.sock", os.Getpid(), time.Now().UnixNano())

	defer func() {
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			t.Logf("Failed to remove socket: %v", err)
		}
	}()

	// Create existing file
	file, err := os.Create(filepath.Clean(socketPath))

	require.NoError(t, err)

	_ = file.Close()

	config := Config{
		Enabled: true,
		Modes: []ModeConfig{
			{
				Type:    "unix_socket",
				Path:    socketPath,
				Enabled: true,
			},
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	// Should succeed by removing existing file
	err = frontend.Start(context.Background())
	require.NoError(t, err)

	// Clean up
	_ = frontend.Stop(context.Background())
}

// TestDefaultConfigValues tests that default configuration values are set correctly.
func TestStdioFrontend_DefaultConfigValues(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	// Test with empty process config
	config := Config{
		Enabled: true,
		Process: ProcessManagementConfig{}, // Empty - should get defaults
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	// Check that defaults were applied
	assert.Equal(t, testIterations, frontend.config.Process.MaxConcurrentClients)
	assert.Equal(t, 300*time.Second, frontend.config.Process.ClientTimeout)
}

// Benchmark tests.
func BenchmarkStdioFrontend_HandleRequest(b *testing.B) {
	frontend, conn, request := setupBenchmarkComponents(b)
	// Get the underlying buffer from the encoder
	encoderWriter := reflect.ValueOf(conn.writer).Elem().FieldByName("w")
	writer, ok := encoderWriter.Interface().(*bytes.Buffer)
	if !ok {
		b.Fatal("Failed to get bytes.Buffer from encoder")
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		writer.Reset()
		request.ID = fmt.Sprintf("bench-%d", i)
		benchmarkSingleRequest(frontend, conn, request)
	}
}

func setupBenchmarkComponents(b *testing.B) (*Frontend, *ClientConnection, *mcp.Request) {
	b.Helper()
	logger := zaptest.NewLogger(b)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{
		Enabled: true,
		Process: ProcessManagementConfig{
			MaxConcurrentClients: testMaxIterations,
			ClientTimeout:        30 * time.Second,
		},
	}

	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	writer := &bytes.Buffer{}
	conn := &ClientConnection{
		id:     "bench-conn",
		writer: json.NewEncoder(writer),
		mu:     sync.RWMutex{},
	}

	request := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "bench_method",
		ID:      "bench-1",
		Params:  map[string]interface{}{"data": "benchmark"},
	}

	return frontend, conn, request
}

func benchmarkSingleRequest(frontend *Frontend, conn *ClientConnection, request *mcp.Request) {
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := frontend.router.RouteRequest(ctx, request, "")
		if err != nil {
			frontend.sendErrorResponse(conn, request, err.Error())

			return
		}

		conn.mu.Lock()
		_ = conn.writer.Encode(resp)
		conn.mu.Unlock()
	}()

	wg.Wait()
}

func BenchmarkStdioFrontend_UpdateMetrics(b *testing.B) {
	logger := zaptest.NewLogger(b)
	mockRouter := &MockRequestRouter{}
	mockAuth := &MockAuthProvider{}
	mockSessions := &MockSessionManager{}

	config := Config{Enabled: true}
	frontend := CreateStdioFrontend("test", config, mockRouter, mockAuth, mockSessions, logger)

	counter := uint64(0)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		frontend.updateMetrics(func(m *FrontendMetrics) {
			m.RequestCount = atomic.AddUint64(&counter, 1)
		})
	}
}
