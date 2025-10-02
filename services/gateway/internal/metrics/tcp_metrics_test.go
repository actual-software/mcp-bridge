package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestTCPConnectionMetrics(t *testing.T) {
	t.Parallel()

	reg := InitializeMetricsRegistry()

	// Test TCP connection increment
	reg.IncrementTCPConnections()

	totalConns := testutil.ToFloat64(reg.TCPConnectionsTotal)
	activeConns := testutil.ToFloat64(reg.TCPConnectionsActive)

	assert.InEpsilon(t, float64(1), totalConns, 0.01)
	assert.InEpsilon(t, float64(1), activeConns, 0.01)

	// Test TCP connection decrement
	reg.DecrementTCPConnections()

	activeConns = testutil.ToFloat64(reg.TCPConnectionsActive)
	assert.InDelta(t, float64(0), activeConns, 0.01)

	// Total should remain unchanged
	totalConns = testutil.ToFloat64(reg.TCPConnectionsTotal)
	assert.InEpsilon(t, float64(1), totalConns, 0.01)
}

func TestTCPMessageMetrics(t *testing.T) {
	t.Parallel()

	reg := InitializeMetricsRegistry()

	// Test message counting
	reg.IncrementTCPMessages("inbound", "request")
	reg.IncrementTCPMessages("outbound", "response")
	reg.IncrementTCPMessages("inbound", "health_check")

	inboundRequests := testutil.ToFloat64(reg.TCPMessagesTotal.WithLabelValues("inbound", "request"))
	outboundResponses := testutil.ToFloat64(reg.TCPMessagesTotal.WithLabelValues("outbound", "response"))
	inboundHealthChecks := testutil.ToFloat64(reg.TCPMessagesTotal.WithLabelValues("inbound", "health_check"))

	assert.InEpsilon(t, float64(1), inboundRequests, 0.01)
	assert.InEpsilon(t, float64(1), outboundResponses, 0.01)
	assert.InEpsilon(t, float64(1), inboundHealthChecks, 0.01)
}

func TestTCPBytesMetrics(t *testing.T) {
	t.Parallel()

	reg := InitializeMetricsRegistry()

	// Test byte counting
	reg.AddTCPBytes("inbound", 1024)
	reg.AddTCPBytes("inbound", 512)
	reg.AddTCPBytes("outbound", 2048)

	inboundBytes := testutil.ToFloat64(reg.TCPBytesTotal.WithLabelValues("inbound"))
	outboundBytes := testutil.ToFloat64(reg.TCPBytesTotal.WithLabelValues("outbound"))

	assert.InEpsilon(t, float64(1536), inboundBytes, 0.01) // 1024 + 512
	assert.InEpsilon(t, float64(2048), outboundBytes, 0.01)
}

func TestTCPProtocolErrors(t *testing.T) {
	t.Parallel()

	reg := InitializeMetricsRegistry()

	// Test error counting
	reg.IncrementTCPProtocolErrors("receive_error")
	reg.IncrementTCPProtocolErrors("send_error")
	reg.IncrementTCPProtocolErrors("send_error")
	reg.IncrementTCPProtocolErrors("routing_error")

	receiveErrors := testutil.ToFloat64(reg.TCPProtocolErrors.WithLabelValues("receive_error"))
	sendErrors := testutil.ToFloat64(reg.TCPProtocolErrors.WithLabelValues("send_error"))
	routingErrors := testutil.ToFloat64(reg.TCPProtocolErrors.WithLabelValues("routing_error"))

	assert.InEpsilon(t, float64(1), receiveErrors, 0.01)
	assert.InEpsilon(t, float64(2), sendErrors, 0.01)
	assert.InEpsilon(t, float64(1), routingErrors, 0.01)
}

func TestTCPMessageDuration(t *testing.T) {
	t.Parallel()

	reg := InitializeMetricsRegistry()

	// Test duration recording - just ensure no panic
	reg.RecordTCPMessageDuration("initialize", testIterations*time.Millisecond)
	reg.RecordTCPMessageDuration("tools/call", testTimeout*time.Millisecond)
	reg.RecordTCPMessageDuration("tools/call", 75*time.Millisecond)

	// For histograms, we can't easily test the values,
	// but we've verified the method doesn't panic
	assert.NotNil(t, reg.TCPMessageDuration)
}
