package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
	"github.com/poiley/mcp-bridge/services/gateway/internal/session"
	"github.com/poiley/mcp-bridge/services/gateway/pkg/wire"
	"github.com/poiley/mcp-bridge/services/gateway/test/testutil"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

const testUserConst = "test-user"

// MockAuthProvider mocks the auth provider.
type MockAuthProvider struct {
	mock.Mock
}

func (m *MockAuthProvider) Authenticate(r *http.Request) (*auth.Claims, error) {
	args := m.Called(r)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	claims, ok := args.Get(0).(*auth.Claims)
	if !ok {
		return nil, args.Error(1)
	}

	return claims, args.Error(1)
}

func (m *MockAuthProvider) ValidateToken(token string) (*auth.Claims, error) {
	args := m.Called(token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	claims, ok := args.Get(0).(*auth.Claims)
	if !ok {
		return nil, args.Error(1)
	}

	return claims, args.Error(1)
}

// MockRouter mocks the router.
type MockRouter struct {
	mock.Mock
}

func (m *MockRouter) RouteRequest(
	ctx context.Context, req *mcp.Request, targetNamespace string,
) (*mcp.Response, error) {
	args := m.Called(ctx, req, targetNamespace)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	resp, ok := args.Get(0).(*mcp.Response)
	if !ok {
		return nil, args.Error(1)
	}

	return resp, args.Error(1)
}

// MockRateLimiter mocks the rate limiter.
type MockRateLimiter struct {
	mock.Mock
}

func (m *MockRateLimiter) Allow(ctx context.Context, key string, config auth.RateLimitConfig) (bool, error) {
	args := m.Called(ctx, key, config)

	return args.Bool(0), args.Error(1)
}

// MockSessionManager mocks the session manager.
type MockSessionManager struct {
	mock.Mock
}

func (m *MockSessionManager) CreateSession(claims *auth.Claims) (*session.Session, error) {
	args := m.Called(claims)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	sess, ok := args.Get(0).(*session.Session)
	if !ok {
		return nil, args.Error(1)
	}

	return sess, args.Error(1)
}

func (m *MockSessionManager) GetSession(id string) (*session.Session, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	sess, ok := args.Get(0).(*session.Session)
	if !ok {
		return nil, args.Error(1)
	}

	return sess, args.Error(1)
}

func (m *MockSessionManager) UpdateSession(sess *session.Session) error {
	args := m.Called(sess)

	return args.Error(0)
}

func (m *MockSessionManager) RemoveSession(id string) error {
	args := m.Called(id)

	return args.Error(0)
}

func (m *MockSessionManager) Close() error {
	args := m.Called()

	return args.Error(0)
}

func (m *MockSessionManager) RedisClient() *redis.Client {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}

	client, ok := args.Get(0).(*redis.Client)
	if !ok {
		return nil
	}

	return client
}

func TestTCPHandler_HandleConnection(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	registry := metrics.InitializeMetricsRegistry()

	t.Run("Successful authentication and request", func(t *testing.T) {
		testTCPHandlerSuccessfulAuth(t, logger, registry)
	})

	t.Run("Authentication failure", func(t *testing.T) {
		testTCPHandlerAuthFailure(t, logger, registry)
	})

	t.Run("Rate limit exceeded", func(t *testing.T) {
		testTCPHandlerRateLimitExceeded(t, logger, registry)
	})

	t.Run("Health check", func(t *testing.T) {
		testTCPHandlerHealthCheck(t, logger, registry)
	})
}

func testTCPHandlerSuccessfulAuth(t *testing.T, logger *zap.Logger, registry *metrics.Registry) {
	t.Helper()

	handler, mocks := setupSuccessfulAuthMocks(t, logger, registry)
	clientConn, serverConn := createTCPTestConnection()

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	done := runTCPHandlerInBackground(t, handler, serverConn)
	clientTransport := wire.NewTransport(clientConn)

	// Perform successful auth flow
	performVersionNegotiation(t, clientTransport)
	performSuccessfulInitialization(t, clientTransport)
	performSuccessfulRequest(t, clientTransport)

	finalizeTCPTest(t, done, clientConn)
	assertAllMockExpectations(t, mocks)
}

type tcpTestMocks struct {
	authProvider *MockAuthProvider
	mockRouter   *MockRouter
	sessions     *MockSessionManager
	rateLimiter  *MockRateLimiter
}

func setupSuccessfulAuthMocks(
	t *testing.T, logger *zap.Logger, registry *metrics.Registry,
) (*TCPHandler, *tcpTestMocks) {
	t.Helper()

	// Create mocks
	authProvider := new(MockAuthProvider)
	mockRouter := new(MockRouter)
	sessions := &MockSessionManager{}
	rateLimiter := new(MockRateLimiter)

	handler := CreateTCPProtocolHandler(logger, authProvider, mockRouter, sessions, registry, rateLimiter, nil)

	// Setup mocks
	claims := &auth.Claims{
		RateLimit: auth.RateLimitConfig{
			RequestsPerMinute: 60,
			Burst:             10,
		},
	}
	claims.Subject = testUserConst

	authProvider.On("ValidateToken", "test-token").Return(claims, nil)
	rateLimiter.On("Allow", mock.Anything, testUserConst, claims.RateLimit).Return(true, nil)

	// Setup session manager mock
	testSession := &session.Session{
		ID:        "session-123",
		User:      testUserConst,
		ExpiresAt: time.Now().Add(time.Hour),
		RateLimit: claims.RateLimit,
	}
	sessions.On("CreateSession", claims).Return(testSession, nil)

	mockRouter.On("RouteRequest", mock.Anything, mock.MatchedBy(func(req *mcp.Request) bool {
		return req.Method == "test-method"
	}), "").Return(&mcp.Response{
		JSONRPC: "2.0",
		ID:      "test-2",
		Result:  "test-result",
	}, nil)

	mocks := &tcpTestMocks{
		authProvider: authProvider,
		mockRouter:   mockRouter,
		sessions:     sessions,
		rateLimiter:  rateLimiter,
	}

	return handler, mocks
}

func createTCPTestConnection() (net.Conn, net.Conn) {
	return net.Pipe()
}

func runTCPHandlerInBackground(t *testing.T, handler *TCPHandler, serverConn net.Conn) chan error {
	t.Helper()

	// Handle connection in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error)

	go func() {
		handler.HandleConnection(ctx, serverConn)
		close(done)
	}()

	return done
}

func finalizeTCPTest(t *testing.T, done chan error, clientConn net.Conn) {
	t.Helper()

	// Close connection
	_ = clientConn.Close()

	// Wait for handler to finish
	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Handler timeout")
	}
}

func assertAllMockExpectations(t *testing.T, mocks *tcpTestMocks) {
	t.Helper()

	mocks.authProvider.AssertExpectations(t)
	mocks.mockRouter.AssertExpectations(t)
	mocks.rateLimiter.AssertExpectations(t)
	mocks.sessions.AssertExpectations(t)
}

func testTCPHandlerAuthFailure(t *testing.T, logger *zap.Logger, registry *metrics.Registry) {
	t.Helper()

	// Create mocks
	authProvider := new(MockAuthProvider)
	mockRouter := new(MockRouter)
	sessions := &MockSessionManager{}
	rateLimiter := new(MockRateLimiter)

	handler := CreateTCPProtocolHandler(logger, authProvider, mockRouter, sessions, registry, rateLimiter, nil)

	// Create pipe
	clientConn, serverConn := net.Pipe()

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	// Setup mocks
	authProvider.On("ValidateToken", "bad-token").Return(nil, assert.AnError)

	// Handle connection in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error)

	go func() {
		handler.HandleConnection(ctx, serverConn)
		close(done)
	}()

	// Create client transport
	clientTransport := wire.NewTransport(clientConn)

	// Perform version negotiation and failed auth
	performVersionNegotiation(t, clientTransport)
	performFailedInitialization(t, clientTransport)

	// Connection should be closed
	<-done

	authProvider.AssertExpectations(t)
}

func testTCPHandlerRateLimitExceeded(t *testing.T, logger *zap.Logger, registry *metrics.Registry) {
	t.Helper()

	// Create mocks
	authProvider := new(MockAuthProvider)
	mockRouter := new(MockRouter)
	sessions := &MockSessionManager{}
	rateLimiter := new(MockRateLimiter)

	handler := CreateTCPProtocolHandler(logger, authProvider, mockRouter, sessions, registry, rateLimiter, nil)

	// Create pipe
	clientConn, serverConn := net.Pipe()

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	// Setup mocks
	claims := &auth.Claims{
		RateLimit: auth.RateLimitConfig{
			RequestsPerMinute: 60,
			Burst:             10,
		},
	}
	claims.Subject = testUserConst

	authProvider.On("ValidateToken", "test-token").Return(claims, nil)
	rateLimiter.On("Allow", mock.Anything, testUserConst, claims.RateLimit).Return(false, nil)

	// Setup session manager mock
	testSession := &session.Session{
		ID:        "session-123",
		User:      testUserConst,
		ExpiresAt: time.Now().Add(time.Hour),
		RateLimit: claims.RateLimit,
	}
	sessions.On("CreateSession", claims).Return(testSession, nil)

	// Handle connection in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error)

	go func() {
		handler.HandleConnection(ctx, serverConn)
		close(done)
	}()

	// Create client transport
	clientTransport := wire.NewTransport(clientConn)

	// Perform test flow
	performVersionNegotiation(t, clientTransport)
	performSuccessfulInitialization(t, clientTransport)
	performRateLimitedRequest(t, clientTransport)

	// Close connection
	cancel()

	_ = clientConn.Close()

	<-done

	authProvider.AssertExpectations(t)
	rateLimiter.AssertExpectations(t)
	sessions.AssertExpectations(t)
}

func testTCPHandlerHealthCheck(t *testing.T, logger *zap.Logger, registry *metrics.Registry) {
	t.Helper()

	// Create mocks
	authProvider := new(MockAuthProvider)
	mockRouter := new(MockRouter)
	sessions := &MockSessionManager{}
	rateLimiter := new(MockRateLimiter)

	handler := CreateTCPProtocolHandler(logger, authProvider, mockRouter, sessions, registry, rateLimiter, nil)

	// Create pipe
	clientConn, serverConn := net.Pipe()

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	// Handle connection in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error)

	go func() {
		handler.HandleConnection(ctx, serverConn)
		close(done)
	}()

	// Create client transport
	clientTransport := wire.NewTransport(clientConn)

	// Perform health check flow
	performVersionNegotiation(t, clientTransport)
	performHealthCheck(t, clientTransport)

	// Close connection
	cancel()

	_ = clientConn.Close()

	<-done
}

func performVersionNegotiation(t *testing.T, clientTransport *wire.Transport) {
	t.Helper()

	// First, perform version negotiation
	versionNeg := &wire.VersionNegotiationPayload{
		MinVersion: wire.MinVersion,
		MaxVersion: wire.MaxVersion,
		Preferred:  wire.CurrentVersion,
		Supported:  []uint16{wire.CurrentVersion},
	}
	err := clientTransport.SendVersionNegotiation(versionNeg)
	require.NoError(t, err)

	// Receive version ack
	msgType, msg, err := clientTransport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeVersionAck, msgType)

	if ack, ok := msg.(*wire.VersionAckPayload); ok {
		assert.Equal(t, wire.CurrentVersion, ack.AgreedVersion)
	}
}

func performSuccessfulInitialization(t *testing.T, clientTransport *wire.Transport) {
	t.Helper()

	// Send initialization request
	initReq := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-1",
		Method:  "initialize",
		Params: map[string]interface{}{
			"token": "test-token",
		},
	}

	err := clientTransport.SendRequest(initReq)
	require.NoError(t, err)

	// Receive initialization response
	msgType, msg, err := clientTransport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeResponse, msgType)

	resp, ok := msg.(*mcp.Response)
	require.True(t, ok, "Expected *mcp.Response, got %T", msg)
	assert.Equal(t, "test-1", resp.ID)
	assert.NotNil(t, resp.Result)
}

func performFailedInitialization(t *testing.T, clientTransport *wire.Transport) {
	t.Helper()

	// Send initialization request with bad token
	initReq := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-1",
		Method:  "initialize",
		Params: map[string]interface{}{
			"token": "bad-token",
		},
	}

	err := clientTransport.SendRequest(initReq)
	require.NoError(t, err)

	// Receive error response
	msgType, msg, err := clientTransport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeResponse, msgType)

	resp, ok := msg.(*mcp.Response)
	require.True(t, ok, "Expected *mcp.Response, got %T", msg)
	assert.Equal(t, "test-1", resp.ID)
	assert.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "Authentication failed")
}

func performSuccessfulRequest(t *testing.T, clientTransport *wire.Transport) {
	t.Helper()

	// Send actual request
	testReq := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-2",
		Method:  "test-method",
	}

	err := clientTransport.SendRequest(testReq)
	require.NoError(t, err)

	// Receive response
	msgType, msg, err := clientTransport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeResponse, msgType)

	resp, ok := msg.(*mcp.Response)
	require.True(t, ok, "Expected *mcp.Response, got %T", msg)
	assert.Equal(t, "test-2", resp.ID)
	assert.Equal(t, "test-result", resp.Result)
}

func performRateLimitedRequest(t *testing.T, clientTransport *wire.Transport) {
	t.Helper()

	// Send actual request
	testReq := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-2",
		Method:  "test-method",
	}

	err := clientTransport.SendRequest(testReq)
	require.NoError(t, err)

	// Receive rate limit error
	msgType, msg, err := clientTransport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeResponse, msgType)

	resp, ok := msg.(*mcp.Response)
	require.True(t, ok, "Expected *mcp.Response, got %T", msg)
	require.NotNil(t, resp)
	assert.Equal(t, "test-2", resp.ID)
	require.NotNil(t, resp.Error, "Expected error response but got result: %v", resp.Result)
	assert.Contains(t, resp.Error.Message, "Rate limit exceeded")
}

func performHealthCheck(t *testing.T, clientTransport *wire.Transport) {
	t.Helper()

	// Send health check
	err := clientTransport.SendHealthCheck()
	require.NoError(t, err)

	// Receive health check response
	msgType, _, err := clientTransport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeHealthCheck, msgType)
}
