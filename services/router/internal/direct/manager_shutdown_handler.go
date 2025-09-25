package direct

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// ManagerShutdownHandler handles DirectClientManager shutdown operations.
type ManagerShutdownHandler struct {
	manager *DirectClientManager
}

// CreateManagerShutdownHandler creates a new manager shutdown handler.
func CreateManagerShutdownHandler(manager *DirectClientManager) *ManagerShutdownHandler {
	return &ManagerShutdownHandler{
		manager: manager,
	}
}

// ExecuteShutdown performs a graceful shutdown of the DirectClientManager.
func (h *ManagerShutdownHandler) ExecuteShutdown(ctx context.Context) error {
	if !h.beginShutdown() {
		return nil
	}

	h.signalShutdown()
	h.cleanupResources()
	h.stopObservabilityComponents(ctx)
	h.closeConnectionPool()

	if err := h.waitForCompletion(ctx); err != nil {
		return err
	}

	h.finalizeShutdown()

	return nil
}

func (h *ManagerShutdownHandler) beginShutdown() bool {
	h.manager.mu.Lock()
	defer h.manager.mu.Unlock()

	if !h.manager.running {
		return false
	}

	h.manager.logger.Info("stopping DirectClientManager")

	return true
}

func (h *ManagerShutdownHandler) signalShutdown() {
	// Protect against double-close.
	select {
	case <-h.manager.shutdown:
		// Channel already closed.
	default:
		close(h.manager.shutdown)
	}
}

func (h *ManagerShutdownHandler) cleanupResources() {
	// Clear all cache timers to prevent goroutine leaks.
	h.manager.clearAllCacheTimers()

	// Clear protocol cache.
	h.manager.cacheMu.Lock()
	h.manager.protocolCache = make(map[string]ClientType)
	h.manager.cacheMu.Unlock()
}

func (h *ManagerShutdownHandler) stopObservabilityComponents(ctx context.Context) {
	// Stop status monitor.
	if err := h.manager.statusMonitor.Stop(ctx); err != nil {
		h.manager.logger.Warn("error stopping status monitor", zap.Error(err))
	}

	// Stop observability manager.
	if err := h.manager.observability.Stop(ctx); err != nil {
		h.manager.logger.Warn("error stopping observability manager", zap.Error(err))
	}

	// Stop memory optimizer.
	if err := h.manager.memoryOptimizer.Stop(); err != nil {
		h.manager.logger.Warn("error stopping memory optimizer", zap.Error(err))
	}
}

func (h *ManagerShutdownHandler) closeConnectionPool() {
	// Close the connection pool (this will close all connections).
	if err := h.manager.connectionPool.Close(); err != nil {
		h.manager.logger.Warn("error closing connection pool", zap.Error(err))
	}
}

func (h *ManagerShutdownHandler) waitForCompletion(ctx context.Context) error {
	done := make(chan struct{})

	go func() {
		h.manager.wg.Wait()
		close(done)
	}()

	return h.waitWithTimeout(ctx, done)
}

func (h *ManagerShutdownHandler) waitWithTimeout(ctx context.Context, done chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-time.After(DefaultDirectTimeoutSeconds * time.Second):
		h.manager.logger.Warn("background routines did not finish in time")

		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *ManagerShutdownHandler) finalizeShutdown() {
	h.manager.mu.Lock()
	h.manager.running = false
	h.manager.mu.Unlock()

	h.manager.logger.Info("DirectClientManager stopped")
}
