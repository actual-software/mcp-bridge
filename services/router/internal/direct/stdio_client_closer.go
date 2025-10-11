package direct

import (
	"context"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
)

// StdioClientCloser manages the graceful shutdown of StdioClient.
type StdioClientCloser struct {
	client *StdioClient
	logger *zap.Logger
}

// CreateStdioCloser creates a new closer for the StdioClient.
func CreateStdioCloser(client *StdioClient) *StdioClientCloser {
	return &StdioClientCloser{
		client: client,
		logger: client.logger,
	}
}

// GracefulShutdown performs a graceful shutdown of the StdioClient.
func (c *StdioClientCloser) GracefulShutdown(ctx context.Context) error {
	if !c.shouldClose() {
		return nil
	}

	c.initializeShutdown()

	if err := c.shutdownProcess(ctx); err != nil {
		return err
	}

	return c.waitForCleanup(ctx)
}

// shouldClose checks if the client should be closed.
func (c *StdioClientCloser) shouldClose() bool {
	c.client.mu.Lock()
	defer c.client.mu.Unlock()

	if c.client.state == StateClosed || c.client.state == StateClosing {
		return false
	}

	c.client.state = StateClosing

	return true
}

// initializeShutdown starts the shutdown process.
func (c *StdioClientCloser) initializeShutdown() {
	c.logger.Info("closing stdio client")

	// Signal shutdown (protect against double-close).
	c.signalShutdown()

	// Close stdin to signal the process to shutdown.
	c.closeStdin()
}

// signalShutdown safely closes the shutdown channel.
func (c *StdioClientCloser) signalShutdown() {
	select {
	case <-c.client.shutdownCh:
		// Channel already closed.
	default:
		close(c.client.shutdownCh)
	}
}

// closeStdin closes the stdin writer.
func (c *StdioClientCloser) closeStdin() {
	c.client.mu.RLock()
	stdin := c.client.stdin
	c.client.mu.RUnlock()

	if stdin != nil {
		_ = stdin.Close()
	}
}

// shutdownProcess handles process termination.
func (c *StdioClientCloser) shutdownProcess(ctx context.Context) error {
	if c.client.cmd == nil {
		return nil
	}

	done := make(chan error, 1)

	go func() {
		done <- c.client.cmd.Wait()
	}()

	return c.waitForProcessExit(ctx, done)
}

// waitForProcessExit waits for the process to exit or forces termination.
func (c *StdioClientCloser) waitForProcessExit(ctx context.Context, done chan error) error {
	select {
	case err := <-done:
		if err != nil {
			c.logger.Warn("process exited with error", zap.Error(err))
		}

		return nil

	case <-time.After(c.client.config.Process.KillTimeout):
		c.forceKillProcess()
		<-done // Wait for the kill to complete

		return nil

	case <-ctx.Done():
		c.forceKillProcess()
		c.setStateClosed()

		return ctx.Err()
	}
}

// forceKillProcess forcefully terminates the process.
func (c *StdioClientCloser) forceKillProcess() {
	c.logger.Warn("force killing process")

	if c.client.cmd != nil && c.client.cmd.Process != nil {
		_ = c.client.cmd.Process.Kill()
	}
}

// setStateClosed sets the client state to closed.
func (c *StdioClientCloser) setStateClosed() {
	c.client.mu.Lock()
	c.client.state = StateClosed
	c.client.mu.Unlock()
}

// waitForCleanup waits for background routines to finish.
func (c *StdioClientCloser) waitForCleanup(ctx context.Context) error {
	finished := make(chan struct{})

	go func() {
		c.client.wg.Wait()
		close(finished)
	}()

	waitTimeout := c.calculateWaitTimeout()

	select {
	case <-finished:
		c.logger.Debug("all background routines finished")
		// Small delay to ensure any final log statements complete
		time.Sleep(10 * time.Millisecond)
	case <-time.After(waitTimeout):
		c.logger.Warn("timeout waiting for background routines", zap.Duration("timeout", waitTimeout))
	case <-ctx.Done():
		return ctx.Err()
	}

	// Clean up resources.
	c.cleanupResources()

	c.setStateClosed()
	c.logger.Info("stdio client closed successfully")

	return nil
}

// calculateWaitTimeout determines the appropriate wait timeout.
func (c *StdioClientCloser) calculateWaitTimeout() time.Duration {
	// Use a shorter timeout for tests but longer for production.
	if c.client.config.Process.KillTimeout > 0 &&
		c.client.config.Process.KillTimeout < defaultRetryCount*time.Second {
		// Likely in test environment, use shorter timeout.
		return constants.ProcessQuickWaitTimeout
	}

	return constants.ProcessWaitTimeout
}

// cleanupResources performs final resource cleanup.
func (c *StdioClientCloser) cleanupResources() {
	// Ensure channels are closed.
	c.safeCloseChannel(c.client.doneCh)

	// Clear pending requests.
	c.clearPendingRequests()

	// Clear memory optimizer if present.
	c.clearMemoryOptimizer()
}

// safeCloseChannel safely closes a channel.
func (c *StdioClientCloser) safeCloseChannel(ch chan struct{}) {
	select {
	case <-ch:
		// Already closed.
	default:
		close(ch)
	}
}

// clearPendingRequests clears all pending requests.
func (c *StdioClientCloser) clearPendingRequests() {
	c.client.requestMapMu.Lock()
	defer c.client.requestMapMu.Unlock()

	for id, ch := range c.client.requestMap {
		close(ch)
		delete(c.client.requestMap, id)
	}
}

// clearMemoryOptimizer clears memory optimizer resources.
func (c *StdioClientCloser) clearMemoryOptimizer() {
	if c.client.memoryOptimizer != nil {
		_ = c.client.memoryOptimizer.Stop()
	}
}
