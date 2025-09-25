
package health

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
)

func TestChecker_TCPHealthCheck(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	mockDisc := NewMockDiscovery()
	mockDisc.SetEndpoints("default", []discovery.Endpoint{
		{Service: "test-server", Address: "localhost:8080", Healthy: true},
	})

	checker := CreateHealthMonitor(mockDisc, logger)

	// Initially TCP should not be enabled
	assert.False(t, checker.tcpEnabled)
	assert.Equal(t, 0, checker.tcpPort)

	// Perform checks without TCP enabled
	checker.performChecks()
	status := checker.GetStatus()
	assert.NotContains(t, status.Checks, "tcp_service")

	// Enable TCP
	checker.EnableTCP(9001)
	assert.True(t, checker.tcpEnabled)
	assert.Equal(t, 9001, checker.tcpPort)

	// Perform checks with TCP enabled
	checker.performChecks()
	status = checker.GetStatus()
	assert.Contains(t, status.Checks, "tcp_service")

	// Verify TCP check result
	tcpCheck, ok := status.Checks["tcp_service"]
	assert.True(t, ok)
	assert.True(t, tcpCheck.Healthy)
	assert.Contains(t, tcpCheck.Message, "TCP service running on port 9001")
}

func TestChecker_TCPHealthCheck_NotConfigured(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	mockDisc := NewMockDiscovery()

	checker := CreateHealthMonitor(mockDisc, logger)

	// Enable TCP with port 0
	checker.EnableTCP(0)

	// Check TCP service
	result := checker.checkTCPService()
	assert.False(t, result.Healthy)
	assert.Equal(t, "TCP service not configured", result.Message)
}
