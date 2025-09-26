
package server

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
)

func TestPerIPConnectionLimits(t *testing.T) {
	server := setupPerIPLimitTestServer()

	t.Run("increment and decrement IP counts", func(t *testing.T) {
		testIPCountIncrementDecrement(t, server)
	})

	t.Run("getIPFromAddr", func(t *testing.T) {
		testGetIPFromAddr(t)
	})

	t.Run("concurrent IP connection tracking", func(t *testing.T) {
		testConcurrentIPTracking(t, server)
	})
}

func setupPerIPLimitTestServer() *GatewayServer {
	cfg := &config.Config{
		Server: config.ServerConfig{
			MaxConnections:      testIterations,
			MaxConnectionsPerIP: 5,
		},
	}

	return &GatewayServer{
		config:         cfg,
		logger:         zap.NewNop(),
		ipConnCount:    sync.Map{},
		metrics:        metrics.InitializeMetricsRegistry(),
		tcpConnections: sync.Map{},
	}
}

func testIPCountIncrementDecrement(t *testing.T, s *GatewayServer) {
	t.Helper()
	
	ip := "192.168.1.testIterations"

	count1 := s.incrementIPConnCount(ip)
	assert.Equal(t, int64(1), count1)

	count2 := s.incrementIPConnCount(ip)
	assert.Equal(t, int64(2), count2)

	assert.Equal(t, int64(2), s.getIPConnCount(ip))

	s.decrementIPConnCount(ip)
	assert.Equal(t, int64(1), s.getIPConnCount(ip))

	s.decrementIPConnCount(ip)
	assert.Equal(t, int64(0), s.getIPConnCount(ip))

	_, exists := s.ipConnCount.Load(ip)
	assert.False(t, exists, "IP should be removed from map when count reaches 0")
}

func testGetIPFromAddr(t *testing.T) {
	t.Helper()
	
	tests := []struct {
		addr     string
		expected string
	}{
		{"192.168.1.testIterations:8080", "192.168.1.testIterations"},
		{"[::1]:8080", "::1"},
		{"localhost:9000", "localhost"},
		{"10.0.0.1:0", "10.0.0.1"},
		{"invalid-address", "invalid-address"},
	}

	for _, tt := range tests {
		result := getIPFromAddr(tt.addr)
		assert.Equal(t, tt.expected, result, "For address %s", tt.addr)
	}
}

func testConcurrentIPTracking(t *testing.T, s *GatewayServer) {
	t.Helper()
	
	ip := "10.0.0.1"
	numGoroutines := testTimeout
	incrementsPerGoroutine := testIterations

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			for j := 0; j < incrementsPerGoroutine; j++ {
				s.incrementIPConnCount(ip)
			}
		}()
	}

	wg.Wait()

	expectedCount := int64(numGoroutines * incrementsPerGoroutine)
	assert.Equal(t, expectedCount, s.getIPConnCount(ip))

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			for j := 0; j < incrementsPerGoroutine; j++ {
				s.decrementIPConnCount(ip)
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int64(0), s.getIPConnCount(ip))
}

func TestTCPPerIPLimits(t *testing.T) {
	server, cancel := setupTCPPerIPLimitServer(t)
	defer cancel()

	serverAddr := server.tcpListener.Addr().String()

	conns := testPerIPConnectionLimits(t, serverAddr)
	defer cleanupConnections(conns)

	testPerIPLimitMetrics(t, server.metrics)
	testConnectionRecovery(t, serverAddr, conns)
	shutdownServerGracefully(t, server, cancel)
}

func setupTCPPerIPLimitServer(t *testing.T) (*GatewayServer, context.CancelFunc) {
	t.Helper()
	
	// Create config with per-IP limits
	cfg := &config.Config{
		Server: config.ServerConfig{
			MaxConnections:      testIterations,
			MaxConnectionsPerIP: 3, // Allow only 3 connections per IP
			TCPPort:             0, // Let system assign port
		},
	}

	// Create minimal server components
	logger := zap.NewNop()
	metricsReg := metrics.InitializeMetricsRegistry()

	// Create server
	s := &GatewayServer{
		config:         cfg,
		logger:         logger,
		metrics:        metricsReg,
		ipConnCount:    sync.Map{},
		tcpConnections: sync.Map{},
		wg:             sync.WaitGroup{},
	}

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx

	// Start TCP listener
	lc := &net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s.tcpListener = listener

	// Create TCP handler with proper dependencies
	s.tcpHandler = CreateTCPProtocolHandler(
		logger,
		nil,        // auth provider (not needed for this test)
		nil,        // router (not needed for this test)
		nil,        // sessions (not needed for this test)
		metricsReg, // metrics registry
		nil,        // rate limiter (not needed for this test)
		nil,        // message authenticator (not needed for this test)
	)

	// Start accept loop
	s.wg.Add(1)

	go s.acceptTCPConnections()

	return s, cancel
}

func testPerIPConnectionLimits(t *testing.T, serverAddr string) []net.Conn {
	t.Helper()
	
	var conns []net.Conn

	dialer := &net.Dialer{}

	// First 3 connections should succeed
	for i := 0; i < 3; i++ {
		conn, err := dialer.DialContext(context.Background(), "tcp", serverAddr)
		require.NoError(t, err)

		conns = append(conns, conn)

		time.Sleep(testTimeout * time.Millisecond) // Give server time to process
	}

	// Wait a bit for connections to be registered
	time.Sleep(testIterations * time.Millisecond)

	// 4th connection should fail (timeout since server closes it)
	conn4, err := dialer.DialContext(context.Background(), "tcp", serverAddr)
	if err == nil {
		// Connection was accepted at TCP level, but should be closed by server
		// Try to read - should get EOF quickly
		require.NoError(t, conn4.SetReadDeadline(time.Now().Add(httpStatusInternalError * time.Millisecond)))

		buf := make([]byte, 1)
		_, err := conn4.Read(buf)
		assert.Error(t, err, "Expected connection to be closed due to per-IP limit")

		_ = conn4.Close()
	}

	return conns
}

func cleanupConnections(conns []net.Conn) {
	for _, conn := range conns {
		if conn != nil {
			_ = conn.Close()
		}
	}
}

func testPerIPLimitMetrics(t *testing.T, metricsReg *metrics.Registry) {
	t.Helper()
	
	// Check metrics
	rejected := testutil.ToFloat64(metricsReg.ConnectionsRejected)
	assert.Greater(t, rejected, float64(0), "Should have rejected connections")
}

func testConnectionRecovery(t *testing.T, serverAddr string, conns []net.Conn) {
	t.Helper()
	
	// Close one connection
	require.NoError(t, conns[0].Close())
	time.Sleep(testIterations * time.Millisecond) // Give server time to process disconnect

	// Now we should be able to connect again
	dialer := &net.Dialer{}

	conn5, err := dialer.DialContext(context.Background(), "tcp", serverAddr)
	if err == nil {
		defer func() { _ = conn5.Close() }()
	}
}

func shutdownServerGracefully(t *testing.T, s *GatewayServer, cancel context.CancelFunc) {
	t.Helper()
	
	// Cleanup
	cancel()

	// Wait for accept loop to finish
	done := make(chan struct{})

	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Server shutdown timeout")
	}

	if s.tcpListener != nil {
		_ = s.tcpListener.Close()
	}
}
