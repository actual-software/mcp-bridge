package pool

import (
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/gateway"
)

// createWebSocketClient is the client creator function for WebSocket connections.
//
//nolint:ireturn // Factory pattern requires interface return
func createWebSocketClient(cfg config.GatewayConfig, logger *zap.Logger) (gateway.GatewayClient, error) {
	return gateway.NewClient(cfg, logger)
}

// NewWebSocketPool creates a new WebSocket connection pool.
func NewWebSocketPool(poolConfig Config, gwConfig config.GatewayConfig, logger *zap.Logger) (*Pool, error) {
	factory := NewGenericFactory(gwConfig, logger, createWebSocketClient, "WebSocket")

	return NewPool(poolConfig, factory, logger)
}
