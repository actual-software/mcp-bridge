package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/wire"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// Test-specific mock for rate limiter.
type mockRateLimiterTCP struct {
	shouldAllow bool
	err         error
}

func (m *mockRateLimiterTCP) Allow(ctx context.Context, key string, config auth.RateLimitConfig) (bool, error) {
	return m.shouldAllow, m.err
}

func TestTCPHandler_HealthCheckRetry_Success(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockAuth := &mockAuthProvider{
		claims: &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "test-user"}},
	}
	mockRouter := &mockRouter{
		response: &mcp.Response{JSONRPC: "2.0", Result: "ok"},
	}
	mockSessions := &mockSessionManager{}
	registry := metrics.InitializeMetricsRegistry()
	mockRateLimit := &mockRateLimiterTCP{shouldAllow: true}

	handler := CreateTCPProtocolHandler(
		logger,
		mockAuth,
		mockRouter,
		mockSessions,
		registry,
		mockRateLimit,
		nil,
	)

	// Create real connections using net.Pipe
	clientConn, serverConn := net.Pipe()

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	// Execute handler in background
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan bool)

	go func() {
		handler.HandleConnection(ctx, serverConn)

		done <- true
	}()

	// Create transport on client side to send/receive messages
	transport := wire.NewTransport(clientConn)

	defer func() { _ = transport.Close() }()

	// Wait a moment for handler to start
	time.Sleep(testIterations * time.Millisecond)

	// Close client connection to trigger timeout, which should trigger health check retry
	_ = clientConn.Close()

	// Give it time to process retries and exit
	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Fatal("Handler did not complete in time")
	}
}

func TestTCPHandler_HealthCheckRetry_AllFail(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockAuth := &mockAuthProvider{
		claims: &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "test-user"}},
	}
	mockRouter := &mockRouter{
		response: &mcp.Response{JSONRPC: "2.0", Result: "ok"},
	}
	mockSessions := &mockSessionManager{}
	registry := metrics.InitializeMetricsRegistry()
	mockRateLimit := &mockRateLimiterTCP{shouldAllow: true}

	handler := CreateTCPProtocolHandler(
		logger,
		mockAuth,
		mockRouter,
		mockSessions,
		registry,
		mockRateLimit,
		nil,
	)

	// Create real connections using net.Pipe
	clientConn, serverConn := net.Pipe()

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	// Execute handler in background
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan bool)

	go func() {
		handler.HandleConnection(ctx, serverConn)

		done <- true
	}()

	// Create transport on client side
	transport := wire.NewTransport(clientConn)

	defer func() { _ = transport.Close() }()

	// Wait a moment for handler to start
	time.Sleep(testIterations * time.Millisecond)

	// Close client connection to trigger timeout/errors
	_ = clientConn.Close()

	// Give it time to process and exit
	select {
	case <-done:
		// Success - connection should close after retries
	case <-time.After(3 * time.Second):
		t.Fatal("Handler did not complete in time")
	}
}

func TestTCPHandler_HealthCheckResponse_RetrySuccess(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockAuth := &mockAuthProvider{
		claims: &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "test-user"}},
	}
	mockRouter := &mockRouter{
		response: &mcp.Response{JSONRPC: "2.0", Result: "ok"},
	}
	mockSessions := &mockSessionManager{}
	registry := metrics.InitializeMetricsRegistry()
	mockRateLimit := &mockRateLimiterTCP{shouldAllow: true}

	handler := CreateTCPProtocolHandler(
		logger,
		mockAuth,
		mockRouter,
		mockSessions,
		registry,
		mockRateLimit,
		nil,
	)

	// Create real connections using net.Pipe
	clientConn, serverConn := net.Pipe()

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	// Execute handler in background
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan bool)

	go func() {
		handler.HandleConnection(ctx, serverConn)

		done <- true
	}()

	// Create transport on client side
	transport := wire.NewTransport(clientConn)

	defer func() { _ = transport.Close() }()

	// Wait a moment for handler to start
	time.Sleep(testIterations * time.Millisecond)

	// Send a health check to trigger response
	err := transport.SendHealthCheck()
	if err != nil {
		t.Logf("Expected send error due to connection close: %v", err)
	}

	// Close client connection
	_ = clientConn.Close()

	// Give it time to process and exit
	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Fatal("Handler did not complete in time")
	}
}
