package gateway

import (
	"context"
	"fmt"
	"net/url"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

const (
	// SchemeTCPTLS represents the TCP with TLS scheme.
	SchemeTCPTLS = "tcp+tls"
	// SchemeTCPS represents the secure TCP scheme.
	SchemeTCPS = "tcps"
)

// GatewayClient defines the interface for gateway clients.
type GatewayClient interface {
	Connect(ctx context.Context) error
	SendRequest(ctx context.Context, req *mcp.Request) error
	ReceiveResponse() (*mcp.Response, error)
	SendPing() error
	Close() error
	IsConnected() bool
}

// NewGatewayClient creates a new gateway client based on the URL scheme.
//
//nolint:ireturn // Factory pattern requires interface return
func NewGatewayClient(cfg config.GatewayConfig, defaultNamespace string, logger *zap.Logger) (GatewayClient, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway URL: %w", err)
	}

	switch u.Scheme {
	case "ws", "wss":
		// WebSocket client.
		return NewClient(cfg, defaultNamespace, logger)
	case "tcp", SchemeTCPS, SchemeTCPTLS:
		// TCP client with binary protocol.
		return NewTCPClient(cfg, defaultNamespace, logger)
	default:
		return nil, fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}
}

// for high availability and efficient resource utilization across multiple gateway endpoints.
//
//nolint:ireturn // Factory pattern requires interface return
func NewGatewayClientWithPool(ctx context.Context, cfg *config.Config, logger *zap.Logger) (GatewayClient, error) {
	logger.Info("Creating pool-based gateway client",
		zap.Int("endpoints", len(cfg.GetGatewayEndpoints())),
		zap.String("strategy", cfg.GetLoadBalancerConfig().Strategy))

	return NewPoolClient(ctx, cfg, logger)
}
