package direct

import (
	"context"
	"net/http"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
)

// SSEClientCloser handles SSE client shutdown operations.
type SSEClientCloser struct {
	client *SSEClient
}

// CreateSSEClientCloser creates a new SSE client closer.
func CreateSSEClientCloser(client *SSEClient) *SSEClientCloser {
	return &SSEClientCloser{
		client: client,
	}
}

// CloseClient performs a graceful shutdown of the SSE client.
func (c *SSEClientCloser) CloseClient(ctx context.Context) error {
	if !c.beginClose() {
		return nil
	}

	c.signalShutdown()
	c.closeResources()

	if err := c.waitForCompletion(ctx); err != nil {
		return err
	}

	c.finalizeClose()

	return nil
}

func (c *SSEClientCloser) beginClose() bool {
	c.client.mu.Lock()
	defer c.client.mu.Unlock()

	if c.isAlreadyClosing() {
		return false
	}

	c.client.state = StateClosing
	c.client.logger.Info("closing SSE client")

	return true
}

func (c *SSEClientCloser) isAlreadyClosing() bool {
	return c.client.state == StateClosed || c.client.state == StateClosing
}

func (c *SSEClientCloser) signalShutdown() {
	// Protect against double-close.
	select {
	case <-c.client.shutdownCh:
		// Channel already closed.
	default:
		close(c.client.shutdownCh)
	}
}

func (c *SSEClientCloser) closeResources() {
	c.closeSSEStream()
	c.closeHTTPTransport()
}

func (c *SSEClientCloser) closeSSEStream() {
	c.client.streamMu.Lock()
	defer c.client.streamMu.Unlock()

	if c.client.streamConn != nil {
		_ = c.client.streamConn.Body.Close()
		c.client.streamConn = nil
		c.client.streamReader = nil
	}
}

func (c *SSEClientCloser) closeHTTPTransport() {
	if c.client.client == nil || c.client.client.Transport == nil {
		return
	}

	if transport, ok := c.client.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

func (c *SSEClientCloser) waitForCompletion(ctx context.Context) error {
	finished := make(chan struct{})

	go func() {
		c.client.wg.Wait()
		close(finished)
	}()

	return c.waitWithTimeout(ctx, finished)
}

func (c *SSEClientCloser) waitWithTimeout(ctx context.Context, finished chan struct{}) error {
	select {
	case <-finished:
		c.client.logger.Debug("all background routines finished")

		return nil
	case <-time.After(constants.GracefulShutdownTimeout):
		c.client.logger.Debug("background routines did not finish quickly, continuing with close")

		return nil
	case <-ctx.Done():
		c.setFinalState()

		return ctx.Err()
	}
}

func (c *SSEClientCloser) setFinalState() {
	c.client.mu.Lock()
	c.client.state = StateClosed
	c.client.mu.Unlock()
}

func (c *SSEClientCloser) finalizeClose() {
	c.setFinalState()

	c.client.updateMetrics(func(m *ClientMetrics) {
		m.IsHealthy = false
	})

	c.client.logger.Info("SSE client closed")
}
