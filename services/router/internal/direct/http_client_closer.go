package direct

import (
	"context"
	"net/http"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
)

// HTTPClientCloser handles HTTP client shutdown operations.
type HTTPClientCloser struct {
	client *HTTPClient
}

// CreateHTTPClientCloser creates a new HTTP client closer.
func CreateHTTPClientCloser(client *HTTPClient) *HTTPClientCloser {
	return &HTTPClientCloser{
		client: client,
	}
}

// CloseClient performs a graceful shutdown of the HTTP client.
func (c *HTTPClientCloser) CloseClient(ctx context.Context) error {
	if !c.beginClose() {
		return nil
	}

	c.signalShutdown()
	c.closeHTTPTransport()

	if err := c.waitForCompletion(ctx); err != nil {
		return err
	}

	c.finalizeClose()

	return nil
}

func (c *HTTPClientCloser) beginClose() bool {
	c.client.mu.Lock()
	defer c.client.mu.Unlock()

	if c.isAlreadyClosing() {
		return false
	}

	c.client.state = StateClosing
	c.client.logger.Info("closing HTTP client")

	return true
}

func (c *HTTPClientCloser) isAlreadyClosing() bool {
	return c.client.state == StateClosed || c.client.state == StateClosing
}

func (c *HTTPClientCloser) signalShutdown() {
	// Protect against double-close.
	select {
	case <-c.client.shutdownCh:
		// Channel already closed.
	default:
		close(c.client.shutdownCh)
	}
}

func (c *HTTPClientCloser) closeHTTPTransport() {
	if c.client.client == nil || c.client.client.Transport == nil {
		return
	}

	if transport, ok := c.client.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

func (c *HTTPClientCloser) waitForCompletion(ctx context.Context) error {
	finished := make(chan struct{})

	go func() {
		c.client.wg.Wait()
		close(finished)
	}()

	return c.waitWithTimeout(ctx, finished)
}

func (c *HTTPClientCloser) waitWithTimeout(ctx context.Context, finished chan struct{}) error {
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

func (c *HTTPClientCloser) setFinalState() {
	c.client.mu.Lock()
	c.client.state = StateClosed
	c.client.mu.Unlock()
}

func (c *HTTPClientCloser) finalizeClose() {
	c.setFinalState()

	c.client.updateMetrics(func(m *ClientMetrics) {
		m.IsHealthy = false
	})

	c.client.logger.Info("HTTP client closed")
}
