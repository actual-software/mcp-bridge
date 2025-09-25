// Package discovery provides service discovery implementations for locating and managing MCP server endpoints.
package discovery

import (
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

// CreateServiceDiscoveryProvider creates a service discovery instance based on the provider configuration.
//

func CreateServiceDiscoveryProvider(cfg config.ServiceDiscoveryConfig, logger *zap.Logger) (ServiceDiscovery, error) {
	// Get the effective provider (prefer Provider over Mode for consistency)
	provider := cfg.Provider
	if provider == "" && cfg.Mode != "" {
		provider = cfg.Mode
	}

	// Default to kubernetes if no provider specified
	if provider == "" {
		provider = "kubernetes"
	}

	logger.Info("Initializing service discovery", zap.String("provider", provider))

	switch provider {
	case "kubernetes":
		return InitializeKubernetesServiceDiscovery(cfg, logger)
	case "static":
		return CreateStaticServiceDiscovery(cfg, logger)
	case "stdio":
		return CreateStdioServiceDiscovery(cfg.Stdio, logger)
	case "websocket", "ws":
		return CreateWebSocketServiceDiscovery(cfg.WebSocket, logger)
	case "sse", "server-sent-events":
		return CreateSSEServiceDiscovery(cfg.SSE, logger)
	default:
		return nil, customerrors.NewInvalidServiceConfigError("unsupported provider", provider)
	}
}
