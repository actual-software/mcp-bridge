package pool

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/gateway"
)

// GenericConnection wraps a GatewayClient as a pooled connection.
// This eliminates code duplication between TCP and WebSocket connection wrappers.
// by using the existing GatewayClient interface that both client types implement.
type GenericConnection struct {
	client gateway.GatewayClient
	id     string
	alive  int32
}

// NewGenericConnection creates a new generic connection wrapper.
func NewGenericConnection(client gateway.GatewayClient) *GenericConnection {
	return &GenericConnection{
		client: client,
		id:     uuid.New().String(),
		alive:  1,
	}
}

// IsAlive checks if the connection is still alive.
// This combines both the wrapper's alive flag and the client's connection status.
func (c *GenericConnection) IsAlive() bool {
	// Check wrapper alive status first (atomic read).
	if atomic.LoadInt32(&c.alive) != 1 {
		return false
	}

	// Check if client is nil (should not happen, but defensive programming).
	if c.client == nil {
		return false
	}

	// Check client connection status.
	return c.client.IsConnected()
}

// Close closes the connection and marks it as no longer alive.
func (c *GenericConnection) Close() error {
	atomic.StoreInt32(&c.alive, 0)

	if c.client == nil {
		return nil
	}

	return c.client.Close()
}

// GetID returns the connection ID.
func (c *GenericConnection) GetID() string {
	return c.id
}

// GetClient returns the underlying client.
// This allows callers to access client-specific methods.

func (c *GenericConnection) GetClient() gateway.GatewayClient {
	return c.client
}

// GenericFactory creates connections using a generic approach.
// This eliminates code duplication between TCP and WebSocket factories.
// by working with the GatewayClient interface.
type GenericFactory struct {
	config        config.GatewayConfig
	logger        *zap.Logger
	clientCreator func(config.GatewayConfig, *zap.Logger) (gateway.GatewayClient, error)
	clientName    string // For logging (e.g., "WebSocket", "TCP")
}

// NewGenericFactory creates a new generic connection factory.
//
// Parameters:
//   - cfg: Gateway configuration
//   - logger: Logger instance
//   - clientCreator: Function that creates and connects a GatewayClient
//   - clientName: Human-readable name for logging (e.g., "WebSocket", "TCP")
func NewGenericFactory(
	cfg config.GatewayConfig,
	logger *zap.Logger,
	clientCreator func(config.GatewayConfig, *zap.Logger) (gateway.GatewayClient, error),
	clientName string,
) *GenericFactory {
	return &GenericFactory{
		config:        cfg,
		logger:        logger,
		clientCreator: clientCreator,
		clientName:    clientName,
	}
}

// Create creates a new connection using the configured client creator.

func (f *GenericFactory) Create(ctx context.Context) (Connection, error) {
	client, err := f.clientCreator(f.config, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s client: %w", f.clientName, err)
	}

	// Connect the client.
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect %s: %w", f.clientName, err)
	}

	conn := NewGenericConnection(client)

	f.logger.Debug(fmt.Sprintf("Created %s connection", f.clientName),
		zap.String("conn_id", conn.GetID()),
		zap.String("url", f.config.URL),
	)

	return conn, nil
}

// Validate validates a connection by checking its health.
func (f *GenericFactory) Validate(conn Connection) error {
	// Type assertion to ensure we have the right connection type.
	genericConn, ok := conn.(*GenericConnection)
	if !ok {
		return fmt.Errorf("invalid connection type: expected *GenericConnection, got %T", conn)
	}

	// Check if connection is alive.
	if !genericConn.IsAlive() {
		return errors.New("connection is not alive")
	}

	// Send a ping to verify connection health.
	client := genericConn.GetClient()
	if client == nil {
		return errors.New("client is nil")
	}

	if err := client.SendPing(); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	return nil
}
