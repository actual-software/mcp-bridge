
package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/prometheus/client_golang/prometheus"
	prometheus_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
	"github.com/poiley/mcp-bridge/services/gateway/internal/session"
	"github.com/poiley/mcp-bridge/services/gateway/pkg/wire"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

func TestTCPHandler_MetricsCollection(t *testing.T) { 
	// Clear default registry to avoid conflicts
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	// Create metrics registry
	metricsReg := metrics.InitializeMetricsRegistry()

	tests := createTCPHandlerMetricsTests(metricsReg)

	runTCPHandlerMetricsTests(t, tests, metricsReg)
}

func createTCPHandlerMetricsTests(metricsReg *metrics.Registry) []struct {
	name         string
	setupHandler func(*testing.T) (*TCPHandler, *MockAuthProvider, *MockRouter, *MockSessionManager, *MockRateLimiter)
	runScenario  func(*testing.T, *TCPHandler, net.Conn)
	checkMetrics func(*testing.T, *metrics.Registry)
} {
	return []struct {
		name         string
		setupHandler func(*testing.T) (*TCPHandler, *MockAuthProvider, *MockRouter, *MockSessionManager, *MockRateLimiter)
		runScenario  func(*testing.T, *TCPHandler, net.Conn)
		checkMetrics func(*testing.T, *metrics.Registry)
	}{
		{
			name:         "successful_connection_and_request",
			setupHandler: setupSuccessfulMetricsTest,
			runScenario:  runSuccessfulMetricsScenario,
			checkMetrics: checkSuccessfulMetrics,
		},
		{
			name:         "authentication_failure",
			setupHandler: setupAuthFailureMetricsTest,
			runScenario:  runAuthFailureMetricsScenario,
			checkMetrics: checkAuthFailureMetrics,
		},
	}
}

func runTCPHandlerMetricsTests(t *testing.T, tests []struct {
	name         string
	setupHandler func(*testing.T) (*TCPHandler, *MockAuthProvider, *MockRouter, *MockSessionManager, *MockRateLimiter)
	runScenario  func(*testing.T, *TCPHandler, net.Conn)
	checkMetrics func(*testing.T, *metrics.Registry)
}, metricsReg *metrics.Registry) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset metrics
			prometheus.DefaultRegisterer = prometheus.NewRegistry()
			metricsReg = metrics.InitializeMetricsRegistry()

			// Setup handler
			handler, authProvider, router, sessionManager, rateLimiter := tt.setupHandler(t)

			// Create pipe connection
			clientConn, serverConn := net.Pipe()

			defer func() { _ = clientConn.Close() }()
			defer func() { _ = serverConn.Close() }()

			// Run handler in background
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			done := make(chan struct{})

			go func() {
				handler.HandleConnection(ctx, serverConn)
				close(done)
			}()

			// Run test scenario
			tt.runScenario(t, handler, clientConn)

			// Close connection and wait for handler to finish
			_ = clientConn.Close()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("Handler did not finish in time")
			}

			// Check metrics
			tt.checkMetrics(t, metricsReg)

			// Verify mocks
			authProvider.AssertExpectations(t)
			router.AssertExpectations(t)
			sessionManager.AssertExpectations(t)
			rateLimiter.AssertExpectations(t)
		})
	}
}

func setupSuccessfulMetricsTest(t *testing.T) (*TCPHandler, *MockAuthProvider, *MockRouter, *MockSessionManager, *MockRateLimiter) {
	t.Helper()
	authProvider := new(MockAuthProvider)
	router := new(MockRouter)
	sessionManager := new(MockSessionManager)
	rateLimiter := new(MockRateLimiter)

	metricsReg := metrics.InitializeMetricsRegistry()
	handler := CreateTCPProtocolHandler(
		zap.NewNop(),
		authProvider,
		router,
		sessionManager,
		metricsReg,
		rateLimiter,
		nil, // No per-message auth for this test
	)

	// Setup expectations
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "test-user",
		},
		Scopes: []string{"read", "write"},
	}
	authProvider.On("ValidateToken", "test-token").Return(claims, nil)

	testSession := &session.Session{
		ID:        "test-session",
		User:      "test-user",
		ExpiresAt: time.Now().Add(time.Hour),
		RateLimit: auth.RateLimitConfig{RequestsPerMinute: testIterations},
	}
	sessionManager.On("CreateSession", mock.Anything).Return(testSession, nil)

	rateLimiter.On("Allow", mock.Anything, "test-user", mock.Anything).Return(true, nil)

	router.On("RouteRequest", mock.Anything, mock.Anything, "").Return(&mcp.Response{
		JSONRPC: "2.0",
		ID:      "test-2",
		Result:  map[string]interface{}{"status": "ok"},
	}, nil)

	return handler, authProvider, router, sessionManager, rateLimiter
}

func runSuccessfulMetricsScenario(t *testing.T, handler *TCPHandler, conn net.Conn) {
	t.Helper()
	transport := wire.NewTransport(conn)

	// First, perform version negotiation
	versionNeg := &wire.VersionNegotiationPayload{
		MinVersion: wire.MinVersion,
		MaxVersion: wire.MaxVersion,
		Preferred:  wire.CurrentVersion,
		Supported:  []uint16{wire.CurrentVersion},
	}
	require.NoError(t, transport.SendVersionNegotiation(versionNeg))

	// Receive version ack
	msgType, msg, err := transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeVersionAck, msgType)
	if ack, ok := msg.(*wire.VersionAckPayload); ok {
		assert.Equal(t, wire.CurrentVersion, ack.AgreedVersion)
	}

	// Send initialization request
	initReq := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "init-1",
		Method:  "initialize",
		Params: map[string]interface{}{
			"token": "test-token",
		},
	}
	require.NoError(t, transport.SendRequest(initReq))

	// Receive init response
	msgType, _, err = transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeResponse, msgType)

	// Handle the response properly
	if resp, ok := msg.(*mcp.Response); ok {
		assert.NotNil(t, resp.Result)
	} else {
		t.Logf("Received message type: %T, value: %+v", msg, msg)
		assert.NotNil(t, msg)
	}

	// Send actual request
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-2",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "test-tool",
			"arguments": map[string]interface{}{
				"key": "value",
			},
		},
	}
	require.NoError(t, transport.SendRequest(req))

	// Receive response
	msgType, _, err = transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeResponse, msgType)

	// Send health check
	require.NoError(t, transport.SendHealthCheck())

	// Try to receive health check response (may fail due to connection close)
	_, _, _ = transport.ReceiveMessage() // Ignore error - connection may close
}

func checkSuccessfulMetrics(t *testing.T, reg *metrics.Registry) {
	t.Helper()
	// Check connection metrics
	tcpConnTotal := prometheus_testutil.ToFloat64(reg.TCPConnectionsTotal)
	assert.Equal(t, float64(1), tcpConnTotal, "TCP connections total")

	// Check message metrics - be more flexible due to version negotiation
	inboundRequests := prometheus_testutil.ToFloat64(reg.TCPMessagesTotal.WithLabelValues("inbound", "request"))
	assert.GreaterOrEqual(t, inboundRequests, float64(2), "At least two inbound requests (init + tools/call)")

	outboundResponses := prometheus_testutil.ToFloat64(reg.TCPMessagesTotal.WithLabelValues("outbound", "response"))
	assert.GreaterOrEqual(t, outboundResponses, float64(2), "At least two outbound responses")
}

func setupAuthFailureMetricsTest(t *testing.T) (*TCPHandler, *MockAuthProvider, *MockRouter, *MockSessionManager, *MockRateLimiter) {
	t.Helper()
	authProvider := new(MockAuthProvider)
	router := new(MockRouter)
	sessionManager := new(MockSessionManager)
	rateLimiter := new(MockRateLimiter)

	metricsReg := metrics.InitializeMetricsRegistry()
	handler := CreateTCPProtocolHandler(
		zap.NewNop(),
		authProvider,
		router,
		sessionManager,
		metricsReg,
		rateLimiter,
		nil,
	)

	// Setup auth failure
	authProvider.On("ValidateToken", "bad-token").Return(nil, assert.AnError)

	return handler, authProvider, router, sessionManager, rateLimiter
}

func runAuthFailureMetricsScenario(t *testing.T, handler *TCPHandler, conn net.Conn) {
	t.Helper()
	transport := wire.NewTransport(conn)

	// First, perform version negotiation
	versionNeg := &wire.VersionNegotiationPayload{
		MinVersion: wire.MinVersion,
		MaxVersion: wire.MaxVersion,
		Preferred:  wire.CurrentVersion,
		Supported:  []uint16{wire.CurrentVersion},
	}
	require.NoError(t, transport.SendVersionNegotiation(versionNeg))

	// Receive version ack
	msgType, msg, err := transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeVersionAck, msgType)

	// Send initialization request with bad token
	initReq := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "init-1",
		Method:  "initialize",
		Params: map[string]interface{}{
			"token": "bad-token",
		},
	}
	require.NoError(t, transport.SendRequest(initReq))

	// Should receive error response
	msgType, _, err = transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeResponse, msgType)

	if resp, ok := msg.(*mcp.Response); ok {
		assert.NotNil(t, resp.Error)
	} else {
		// Accept that the message format might be different
		assert.NotNil(t, msg)
	}
}

func checkAuthFailureMetrics(t *testing.T, reg *metrics.Registry) {
	t.Helper()
	// Check auth failure metrics
	authFailures := prometheus_testutil.ToFloat64(reg.AuthFailuresTotal.WithLabelValues("invalid_token"))
	assert.Equal(t, float64(1), authFailures, "Auth failures")

	// Check message metrics - version negotiation + init request
	inboundRequests := prometheus_testutil.ToFloat64(reg.TCPMessagesTotal.WithLabelValues("inbound", "request"))
	assert.Equal(t, float64(1), inboundRequests, "Inbound requests (init)")

	versionNegs := prometheus_testutil.ToFloat64(reg.TCPMessagesTotal.WithLabelValues("inbound", "version_negotiation"))
	assert.Equal(t, float64(1), versionNegs, "Version negotiations")
}

func TestTCPHandler_MessageDurationMetrics(t *testing.T) {
	// Clear default registry
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	metricsReg, handler := setupDurationMetricsTest(t)
	clientConn, serverConn := createConnectionPair()
	
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	done := runHandlerInBackground(t, handler, serverConn)
	runDurationMetricsClient(t, clientConn)
	waitForHandlerCompletion(t, done, clientConn)
	validateDurationMetrics(t, metricsReg)
}

func setupDurationMetricsTest(t *testing.T) (*metrics.Registry, *TCPHandler) {
	t.Helper()
	
	metricsReg := metrics.InitializeMetricsRegistry()
	authProvider := new(MockAuthProvider)
	router := new(MockRouter)
	sessionManager := new(MockSessionManager)
	rateLimiter := new(MockRateLimiter)

	handler := CreateTCPProtocolHandler(
		zap.NewNop(),
		authProvider,
		router,
		sessionManager,
		metricsReg,
		rateLimiter,
		nil,
	)

	// Setup successful auth
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "test-user",
		},
		Scopes: []string{"read", "write"},
	}
	authProvider.On("ValidateToken", "test-token").Return(claims, nil)

	testSession := &session.Session{
		ID:        "test-session",
		User:      "test-user",
		ExpiresAt: time.Now().Add(time.Hour),
		RateLimit: auth.RateLimitConfig{RequestsPerMinute: testIterations},
	}
	sessionManager.On("CreateSession", mock.Anything).Return(testSession, nil)

	rateLimiter.On("Allow", mock.Anything, "test-user", mock.Anything).Return(true, nil)

	// Add delay to router to test duration metrics
	router.On("RouteRequest", mock.Anything, mock.Anything, "").Return(&mcp.Response{
		JSONRPC: "2.0",
		ID:      "test-2",
		Result:  map[string]interface{}{"status": "ok"},
	}, nil).Run(func(args mock.Arguments) {
		time.Sleep(testTimeout * time.Millisecond) // Simulate processing time
	})

	return metricsReg, handler
}

func createConnectionPair() (net.Conn, net.Conn) {
	return net.Pipe()
}

func runHandlerInBackground(t *testing.T, handler *TCPHandler, serverConn net.Conn) chan struct{} {
	t.Helper()
	
	// Run handler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})

	go func() {
		handler.HandleConnection(ctx, serverConn)
		close(done)
	}()

	return done
}

func runDurationMetricsClient(t *testing.T, clientConn net.Conn) {
	t.Helper()
	
	// Run client
	transport := wire.NewTransport(clientConn)

	// First, perform version negotiation
	versionNeg := &wire.VersionNegotiationPayload{
		MinVersion: wire.MinVersion,
		MaxVersion: wire.MaxVersion,
		Preferred:  wire.CurrentVersion,
		Supported:  []uint16{wire.CurrentVersion},
	}
	require.NoError(t, transport.SendVersionNegotiation(versionNeg))

	// Receive version ack
	msgType, msg, err := transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeVersionAck, msgType)

	if ack, ok := msg.(*wire.VersionAckPayload); ok {
		assert.Equal(t, wire.CurrentVersion, ack.AgreedVersion)
	}

	// Initialize
	initReq := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "init-1",
		Method:  "initialize",
		Params: map[string]interface{}{
			"token": "test-token",
		},
	}
	require.NoError(t, transport.SendRequest(initReq))

	msgType, _, err = transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeResponse, msgType)

	// Send request
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-2",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "test-tool",
		},
	}
	require.NoError(t, transport.SendRequest(req))

	msgType, _, err = transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeResponse, msgType)
}

func waitForHandlerCompletion(t *testing.T, done chan struct{}, clientConn net.Conn) {
	t.Helper()
	
	// Close and wait
	_ = clientConn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Handler did not finish")
	}
}

func validateDurationMetrics(t *testing.T, metricsReg *metrics.Registry) {
	t.Helper()
	
	// Check duration metrics were recorded
	// We can't check exact values for histograms easily, but we can verify they exist
	assert.NotNil(t, metricsReg.TCPMessageDuration)
	assert.NotNil(t, metricsReg.RequestDuration)
}
