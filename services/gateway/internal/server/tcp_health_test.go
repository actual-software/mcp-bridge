package server

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
	"github.com/poiley/mcp-bridge/services/gateway/internal/health"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
	"github.com/poiley/mcp-bridge/services/gateway/pkg/wire"
	"github.com/poiley/mcp-bridge/services/gateway/test/testutil"
)

// mockDiscovery implements discovery.ServiceDiscovery for testing.
type mockDiscovery struct {
	endpoints []discovery.Endpoint
	err       error
}

func (m *mockDiscovery) Start(ctx context.Context) error {
	return m.err
}

func (m *mockDiscovery) Stop() {
	// no-op for testing
}

func (m *mockDiscovery) GetEndpoints(namespace string) []discovery.Endpoint {
	if m.err != nil {
		return nil
	}

	return m.endpoints
}

func (m *mockDiscovery) GetAllEndpoints() map[string][]discovery.Endpoint {
	if m.err != nil {
		return nil
	}

	return map[string][]discovery.Endpoint{
		"default": m.endpoints,
	}
}

func (m *mockDiscovery) ListNamespaces() []string {
	return []string{"default"}
}

func TestTCPHealthServer_Start(t *testing.T) {
	logger := zap.NewNop()
	mockDisc := &mockDiscovery{
		endpoints: []discovery.Endpoint{
			{Service: "test-server", Address: "localhost:8080", Healthy: true},
		},
	}
	checker := health.CreateHealthMonitor(mockDisc, logger)
	metrics := metrics.InitializeMetricsRegistry()

	// Test with port 0 (disabled)
	server := CreateTCPHealthCheckServer(0, checker, metrics, logger)
	err := server.Start()
	assert.NoError(t, err)
	assert.Nil(t, server.listener)

	// Test with valid port (let system assign port)
	// Use port 1 to enable, will get actual port from listener
	server = CreateTCPHealthCheckServer(1, checker, metrics, logger)
	err = server.Start()
	require.NoError(t, err)
	require.NotNil(t, server.listener)

	// Get actual address
	addr, ok := server.listener.Addr().(*net.TCPAddr)
	require.True(t, ok, "Expected *net.TCPAddr")

	// Verify server is listening
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(context.Background(), "tcp", addr.String())
	require.NoError(t, err)

	_ = conn.Close()

	// Stop server
	err = server.Stop()
	assert.NoError(t, err)

	// Verify server is no longer listening
	_, err = dialer.DialContext(context.Background(), "tcp", addr.String())
	assert.Error(t, err)
}

func TestTCPHealthServer_HandleHealthCheck(t *testing.T) {
	server, transport := setupTCPHealthServer(t)

	defer func() { _ = server.Stop() }()

	sendHealthCheckRequest(t, transport)
	response := receiveHealthCheckResponse(t, transport)
	validateHealthCheckResponse(t, response)
}

func setupTCPHealthServer(t *testing.T) (*TCPHealthServer, *wire.Transport) {
	t.Helper()

	logger := zap.NewNop()
	mockDisc := &mockDiscovery{
		endpoints: []discovery.Endpoint{
			{Service: "test-server", Address: "localhost:8080", Healthy: true},
		},
	}
	checker := health.CreateHealthMonitor(mockDisc, logger)
	checker.EnableTCP(9001)

	// Perform initial health checks to populate status
	ctx := context.Background()
	go checker.RunChecks(ctx)

	time.Sleep(testIterations * time.Millisecond) // Give time for initial check

	metrics := metrics.InitializeMetricsRegistry()

	// Start server with random port
	server := CreateTCPHealthCheckServer(1, checker, metrics, logger)
	err := server.Start()
	require.NoError(t, err)

	// Get actual port
	addr, ok := server.listener.Addr().(*net.TCPAddr)
	require.True(t, ok, "Expected *net.TCPAddr")

	// Connect to server
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(context.Background(), "tcp", addr.String())
	require.NoError(t, err)

	// Create transport
	transport := wire.NewTransport(conn)

	return server, transport
}

func sendHealthCheckRequest(t *testing.T, transport *wire.Transport) {
	t.Helper()

	// Send health check request
	healthReq := map[string]interface{}{
		"check": "detailed",
	}
	payload, err := json.Marshal(healthReq)
	require.NoError(t, err)

	frame := &wire.Frame{
		Version:     wire.CurrentVersion,
		MessageType: wire.MessageTypeHealthCheck,
		Payload:     payload,
	}

	err = frame.Encode(transport.GetWriter())
	require.NoError(t, err)
	err = transport.Flush()
	require.NoError(t, err)
}

func receiveHealthCheckResponse(t *testing.T, transport *wire.Transport) map[string]interface{} {
	t.Helper()

	// Set read timeout
	require.NoError(t, transport.SetReadDeadline(time.Now().Add(5*time.Second)))

	// Receive response
	msgType, msg, err := transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeHealthCheck, msgType)

	// Parse response
	response, ok := msg.(map[string]interface{})
	require.True(t, ok)

	return response
}

func validateHealthCheckResponse(t *testing.T, response map[string]interface{}) {
	t.Helper()

	// Verify response structure
	assert.Contains(t, response, "healthy")
	assert.Contains(t, response, "checks")
	assert.Contains(t, response, "timestamp")

	// Verify health status
	healthy, ok := response["healthy"].(bool)
	assert.True(t, ok)
	assert.True(t, healthy)

	// Verify checks
	checks, ok := response["checks"].(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, checks, "service_discovery")
	assert.Contains(t, checks, "endpoints")
	assert.Contains(t, checks, "tcp_service")
}

func TestTCPHealthServer_SimpleHealthCheck(t *testing.T) {
	logger := zap.NewNop()
	mockDisc := &mockDiscovery{
		endpoints: []discovery.Endpoint{
			{Service: "test-server", Address: "localhost:8080", Healthy: true},
		},
	}
	checker := health.CreateHealthMonitor(mockDisc, logger)
	metrics := metrics.InitializeMetricsRegistry()

	// Start server
	server := CreateTCPHealthCheckServer(1, checker, metrics, logger)
	err := server.Start()
	require.NoError(t, err)

	defer func() { _ = server.Stop() }()

	// Get actual port
	addr, ok := server.listener.Addr().(*net.TCPAddr)
	require.True(t, ok, "Expected *net.TCPAddr")

	// Connect to server
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(context.Background(), "tcp", addr.String())
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	// Create transport
	transport := wire.NewTransport(conn)

	// Send simple health check request
	healthReq := map[string]interface{}{
		"check": "simple",
	}
	payload, err := json.Marshal(healthReq)
	require.NoError(t, err)

	frame := &wire.Frame{
		Version:     wire.CurrentVersion,
		MessageType: wire.MessageTypeHealthCheck,
		Payload:     payload,
	}

	err = frame.Encode(transport.GetWriter())
	require.NoError(t, err)
	err = transport.Flush()
	require.NoError(t, err)

	// Receive response
	require.NoError(t, transport.SetReadDeadline(time.Now().Add(5*time.Second)))
	msgType, msg, err := transport.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, wire.MessageTypeHealthCheck, msgType)

	// Parse response
	response, ok := msg.(map[string]interface{})
	require.True(t, ok)

	// Simple response should only have healthy field
	assert.Contains(t, response, "healthy")
	assert.Len(t, response, 1)
}

func TestTCPHealthServer_InvalidMessage(t *testing.T) {
	logger := zap.NewNop()
	mockDisc := &mockDiscovery{
		endpoints: []discovery.Endpoint{},
	}
	checker := health.CreateHealthMonitor(mockDisc, logger)
	metrics := metrics.InitializeMetricsRegistry()

	// Start server
	server := CreateTCPHealthCheckServer(1, checker, metrics, logger)
	err := server.Start()
	require.NoError(t, err)

	defer func() { _ = server.Stop() }()

	// Get actual port
	addr, ok := server.listener.Addr().(*net.TCPAddr)
	require.True(t, ok, "Expected *net.TCPAddr")

	// Connect to server
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(context.Background(), "tcp", addr.String())
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	// Create transport
	transport := wire.NewTransport(conn)

	// Send non-health-check message
	frame := &wire.Frame{
		Version:     wire.CurrentVersion,
		MessageType: wire.MessageTypeRequest, // Wrong type
		Payload:     []byte("{}"),
	}

	err = frame.Encode(transport.GetWriter())
	require.NoError(t, err)
	err = transport.Flush()
	require.NoError(t, err)

	// Connection should be closed by server
	require.NoError(t, transport.SetReadDeadline(time.Now().Add(1*time.Second)))
	_, _, err = transport.ReceiveMessage()
	assert.Error(t, err)
}

func TestTCPHealthServer_Metrics(t *testing.T) {
	logger := zap.NewNop()
	mockDisc := &mockDiscovery{
		endpoints: []discovery.Endpoint{
			{Service: "test-server", Address: "localhost:8080", Healthy: true},
		},
	}
	checker := health.CreateHealthMonitor(mockDisc, logger)
	metricsReg := metrics.InitializeMetricsRegistry()

	// Start server
	server := CreateTCPHealthCheckServer(1, checker, metricsReg, logger)
	err := server.Start()
	require.NoError(t, err)

	defer func() { _ = server.Stop() }()

	// Get actual port
	addr, ok := server.listener.Addr().(*net.TCPAddr)
	require.True(t, ok, "Expected *net.TCPAddr")

	// Perform health check
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(context.Background(), "tcp", addr.String())
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	transport := wire.NewTransport(conn)

	// Send health check
	frame := &wire.Frame{
		Version:     wire.CurrentVersion,
		MessageType: wire.MessageTypeHealthCheck,
		Payload:     []byte(`{"check": "simple"}`),
	}

	err = frame.Encode(transport.GetWriter())
	require.NoError(t, err)
	err = transport.Flush()
	require.NoError(t, err)

	// Receive response
	require.NoError(t, transport.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err = transport.ReceiveMessage()
	require.NoError(t, err)

	// Verify metrics were recorded correctly
	testMetrics := &testutil.TestMetricsRegistry{Registry: metricsReg}

	// Check inbound message metric
	inboundCount := testMetrics.GetTCPMessageCount("inbound", "health_check")
	require.InEpsilon(t, float64(1), inboundCount, 0.01, "Expected 1 inbound health check message")

	// Check outbound message metric
	outboundCount := testMetrics.GetTCPMessageCount("outbound", "health_check")
	require.InEpsilon(t, float64(1), outboundCount, 0.01, "Expected 1 outbound health check response")

	// Verify no protocol errors
	errorCount := testMetrics.GetTCPProtocolErrorCount("health_receive_error")
	require.InEpsilon(t, float64(0), errorCount, 0.01, "Expected no health receive errors")

	errorCount = testMetrics.GetTCPProtocolErrorCount("health_send_error")
	require.InEpsilon(t, float64(0), errorCount, 0.01, "Expected no health send errors")

	errorCount = testMetrics.GetTCPProtocolErrorCount("health_flush_error")
	require.InEpsilon(t, float64(0), errorCount, 0.01, "Expected no health flush errors")
}
