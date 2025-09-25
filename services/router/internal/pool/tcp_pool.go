package pool

import (
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/gateway"
)

// createTCPClient is the client creator function for TCP connections.
func createTCPClient(cfg config.GatewayConfig, logger *zap.Logger) (gateway.GatewayClient, error) {
	return gateway.NewTCPClient(cfg, logger)
}

// NewTCPPool creates a new TCP connection pool.
func NewTCPPool(poolConfig Config, gwConfig config.GatewayConfig, logger *zap.Logger) (*Pool, error) {
	factory := NewGenericFactory(gwConfig, logger, createTCPClient, "TCP")

	return NewPool(poolConfig, factory, logger)
}
