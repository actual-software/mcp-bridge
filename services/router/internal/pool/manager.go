package pool

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// PooledGatewayClient wraps a connection pool and implements the GatewayClient interface.
type PooledGatewayClient struct {
	wsPool      *Pool
	tcpPool     *Pool
	isWebSocket bool
	logger      *zap.Logger

	// Current connection context.
	mu      sync.Mutex
	current Connection // Store the connection wrapper, not just the client
}

// NewPooledGatewayClient creates a new pooled gateway client.
func NewPooledGatewayClient(
	poolConfig Config,
	gwConfig config.GatewayConfig,
	logger *zap.Logger,
) (*PooledGatewayClient, error) {
	u, err := url.Parse(gwConfig.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway URL: %w", err)
	}

	pgc := &PooledGatewayClient{
		logger: logger,
	}

	switch u.Scheme {
	case "ws", "wss":
		pgc.isWebSocket = true

		pool, err := NewWebSocketPool(poolConfig, gwConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create WebSocket pool: %w", err)
		}

		pgc.wsPool = pool

	case "tcp", "tcps", "tcp+tls":
		pgc.isWebSocket = false

		pool, err := NewTCPPool(poolConfig, gwConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create TCP pool: %w", err)
		}

		pgc.tcpPool = pool

	default:
		return nil, fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	return pgc, nil
}

// acquireConnection gets a connection from the appropriate pool.
func (p *PooledGatewayClient) acquireConnection(ctx context.Context) (Connection, error) {
	if p.isWebSocket {
		p.logger.Debug("Acquiring WebSocket connection from pool")

		conn, err := p.wsPool.Acquire(ctx)
		if err != nil {
			p.logger.Error("Failed to acquire WebSocket connection from pool",
				zap.Error(err),
			)

			return nil, err
		}

		p.logger.Debug("Successfully acquired WebSocket connection",
			zap.String("conn_id", conn.GetID()),
		)

		return conn, nil
	}

	p.logger.Debug("Acquiring TCP connection from pool")

	conn, err := p.tcpPool.Acquire(ctx)
	if err != nil {
		p.logger.Error("Failed to acquire TCP connection from pool",
			zap.Error(err),
		)

		return nil, err
	}

	p.logger.Debug("Successfully acquired TCP connection",
		zap.String("conn_id", conn.GetID()),
	)

	return conn, nil
}

// releaseConnection returns a connection to the pool.
func (p *PooledGatewayClient) releaseConnection() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.current == nil {
		return
	}

	p.logger.Debug("Releasing connection to pool",
		zap.String("conn_id", p.current.GetID()),
		zap.Bool("websocket", p.isWebSocket),
	)

	var err error
	if p.isWebSocket {
		err = p.wsPool.Release(p.current)
	} else {
		err = p.tcpPool.Release(p.current)
	}

	if err != nil {
		p.logger.Warn("Failed to release connection",
			zap.String("conn_id", p.current.GetID()),
			zap.Error(err),
		)
	}

	p.current = nil
}

// Connect establishes a connection (gets one from the pool).
func (p *PooledGatewayClient) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Release existing connection if any.
	if p.current != nil {
		p.releaseConnection()
	}

	conn, err := p.acquireConnection(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}

	p.current = conn

	p.logger.Debug("Acquired connection from pool",
		zap.String("conn_id", conn.GetID()),
		zap.Bool("websocket", p.isWebSocket),
	)

	return nil
}

// SendRequest sends a request using the current connection.
func (p *PooledGatewayClient) SendRequest(ctx context.Context, req *mcp.Request) error {
	p.mu.Lock()
	conn := p.current
	p.mu.Unlock()

	if conn == nil {
		// Auto-connect if not connected.
		if err := p.Connect(ctx); err != nil {
			return fmt.Errorf("not connected and failed to auto-connect: %w", err)
		}

		p.mu.Lock()
		conn = p.current
		p.mu.Unlock()
	}

	// Get the underlying client from the connection wrapper.
	genericConn, ok := conn.(*GenericConnection)
	if !ok {
		return errors.New("type assertion failed")
	}

	client := genericConn.GetClient()

	return client.SendRequest(ctx, req)
}

// ReceiveResponse receives a response from the current connection.
func (p *PooledGatewayClient) ReceiveResponse() (*mcp.Response, error) {
	p.mu.Lock()
	conn := p.current
	p.mu.Unlock()

	if conn == nil {
		return nil, errors.New("not connected")
	}

	// Get the underlying client from the connection wrapper.
	genericConn, ok := conn.(*GenericConnection)
	if !ok {
		return nil, errors.New("type assertion failed")
	}

	client := genericConn.GetClient()

	return client.ReceiveResponse()
}

// SendPing sends a ping on the current connection.
func (p *PooledGatewayClient) SendPing() error {
	p.mu.Lock()
	conn := p.current
	p.mu.Unlock()

	if conn == nil {
		return errors.New("not connected")
	}

	// Get the underlying client from the connection wrapper.
	genericConn, ok := conn.(*GenericConnection)
	if !ok {
		return errors.New("type assertion failed")
	}

	client := genericConn.GetClient()

	return client.SendPing()
}

// Close releases the current connection and closes the pool.
func (p *PooledGatewayClient) Close() error {
	p.releaseConnection()

	if p.wsPool != nil {
		return p.wsPool.Close()
	}

	if p.tcpPool != nil {
		return p.tcpPool.Close()
	}

	return nil
}

// IsConnected checks if there's an active connection.
func (p *PooledGatewayClient) IsConnected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.current != nil && p.current.IsAlive()
}

// Stats returns pool statistics.
func (p *PooledGatewayClient) Stats() Stats {
	if p.wsPool != nil {
		return p.wsPool.Stats()
	}

	if p.tcpPool != nil {
		return p.tcpPool.Stats()
	}

	return Stats{}
}
