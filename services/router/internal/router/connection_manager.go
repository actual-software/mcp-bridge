package router

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/gateway"
)

// ConnectionManager handles WebSocket connection lifecycle with automatic reconnection.
type ConnectionManager struct {
	config   *config.Config
	logger   *zap.Logger
	gwClient gateway.GatewayClient

	state   ConnectionState
	stateMu sync.RWMutex

	// State change notification channel.
	stateChangeChan chan ConnectionState
	channelClosed   bool // Track if channel is closed

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics.
	retries *uint64
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager(cfg *config.Config, logger *zap.Logger, gwClient gateway.GatewayClient) *ConnectionManager {
	ctx, cancel := context.WithCancel(context.Background())

	var retries uint64

	return &ConnectionManager{
		config:          cfg,
		logger:          logger,
		gwClient:        gwClient,
		state:           StateInit,
		stateChangeChan: make(chan ConnectionState, StateChangeChannelBuffer), // Buffered to prevent blocking
		ctx:             ctx,
		cancel:          cancel,
		retries:         &retries,
	}
}

// Start begins connection management with automatic reconnection.
func (cm *ConnectionManager) Start() {
	cm.wg.Add(1)

	go cm.maintainConnection()
}

// Stop gracefully stops the connection manager.
func (cm *ConnectionManager) Stop() {
	cm.logger.Info("Stopping connection manager")

	// First cancel context to stop goroutines.
	cm.cancel()

	// Then safely close the channel and set state.
	cm.stateMu.Lock()

	if !cm.channelClosed {
		cm.state = StateShutdown
		close(cm.stateChangeChan)
		cm.channelClosed = true
	}

	cm.stateMu.Unlock()

	cm.wg.Wait()
}

// GetState returns the current connection state (thread-safe).
func (cm *ConnectionManager) GetState() ConnectionState {
	cm.stateMu.RLock()
	defer cm.stateMu.RUnlock()

	return cm.state
}

// setState updates the connection state (thread-safe) - internal method.
func (cm *ConnectionManager) setState(state ConnectionState) {
	cm.stateMu.Lock()
	defer cm.stateMu.Unlock()

	oldState := cm.state
	cm.state = state

	// Only notify if state actually changed and channel is still open.
	if oldState != state && !cm.channelClosed {
		cm.logger.Debug("Connection state changed", zap.String("state", state.String()))

		// Non-blocking send to state change channel.
		select {
		case cm.stateChangeChan <- state:
			// State change notification sent.
		default:
			// Channel full, log warning but don't block.
			cm.logger.Warn("State change channel full, dropping notification",
				zap.String("state", state.String()))
		}
	}
}

// GetStateChangeChan returns a read-only channel for state change notifications.
func (cm *ConnectionManager) GetStateChangeChan() <-chan ConnectionState {
	return cm.stateChangeChan
}

// GetRetryCount returns the number of connection retries.
func (cm *ConnectionManager) GetRetryCount() uint64 {
	return atomic.LoadUint64(cm.retries)
}

// maintainConnection manages the WebSocket connection with automatic reconnection.
func (cm *ConnectionManager) maintainConnection() {
	defer cm.wg.Done()

	backoff := 1 * time.Second
	maxBackoff := defaultMaxTimeoutSeconds * time.Second
	multiplier := 2.0
	attempts := 0

	for {
		select {
		case <-cm.ctx.Done():
			cm.setState(StateShutdown)

			return
		default:
			cm.logger.Info("Attempting to connect to gateway",
				zap.Int("attempt", attempts+1),
			)

			cm.setState(StateConnecting)

			if err := cm.connect(); err != nil {
				cm.logger.Error("Connection failed",
					zap.Error(err),
					zap.Duration("next_retry", backoff),
				)
				cm.setState(StateError)
				atomic.AddUint64(cm.retries, 1)

				// Check max attempts (use default of 10).
				maxAttempts := 10
				if maxAttempts > 0 && attempts >= maxAttempts {
					cm.logger.Error("Max reconnection attempts reached")

					return
				}

				// Exponential backoff.
				time.Sleep(backoff)

				backoff = time.Duration(float64(backoff) * multiplier)
				if backoff > maxBackoff {
					backoff = maxBackoff
				}

				attempts++

				continue
			}

			// Reset backoff on successful connection.
			backoff = 1 * time.Second
			attempts = 0

			cm.setState(StateConnected)
			cm.logger.Info("Successfully connected to gateway")

			// Handle connection until it fails.
			cm.handleConnection()
		}
	}
}

// connect establishes the WebSocket connection.
func (cm *ConnectionManager) connect() error {
	ctx, cancel := context.WithTimeout(cm.ctx, defaultTimeoutSeconds * time.Second)
	defer cancel()

	return cm.gwClient.Connect(ctx)
}

// handleConnection manages an active connection with keepalive.
func (cm *ConnectionManager) handleConnection() {
	keepaliveInterval := defaultTimeoutSeconds * time.Second
	if keepaliveInterval <= 0 {
		cm.handleConnectionWithoutKeepalive()

		return
	}

	cm.handleConnectionWithKeepalive(keepaliveInterval)
}

// handleConnectionWithoutKeepalive manages connection without keepalive pings.
func (cm *ConnectionManager) handleConnectionWithoutKeepalive() {
	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-time.After(ConnectionStateCheckInterval):
			// Check if state changed to reconnecting (connection lost).
			if cm.GetState() == StateReconnecting {
				return
			}
		}
	}
}

// handleConnectionWithKeepalive manages connection with periodic keepalive pings.
func (cm *ConnectionManager) handleConnectionWithKeepalive(keepaliveInterval time.Duration) {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			if cm.shouldStopKeepalive() {
				return
			}
		case <-time.After(ConnectionStateCheckInterval):
			// Periodically check if state changed to reconnecting.
			if cm.GetState() == StateReconnecting {
				return
			}
		}
	}
}

// shouldStopKeepalive checks state and sends ping, returns true if keepalive should stop.
func (cm *ConnectionManager) shouldStopKeepalive() bool {
	// Check state before sending ping.
	if cm.GetState() != StateConnected {
		return true
	}

	if err := cm.gwClient.SendPing(); err != nil {
		cm.logger.Error("Keepalive failed", zap.Error(err))
		cm.setState(StateReconnecting)

		return true
	}

	return false
}
