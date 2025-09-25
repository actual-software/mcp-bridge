package router

import (
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
)

// Testing utilities for LocalRouter - separated from production code.

// GetStateConnected returns the StateConnected constant for testing.
func GetStateConnected() ConnectionState {
	return StateConnected
}

// GetStateError returns the StateError constant for testing.
func GetStateError() ConnectionState {
	return StateError
}

// GetStateConnecting returns the StateConnecting constant for testing.
func GetStateConnecting() ConnectionState {
	return StateConnecting
}

// GetStdinChan returns the stdin channel for testing.
func (r *LocalRouter) GetStdinChan() chan<- []byte {
	return r.stdinChan
}

// GetStdoutChan returns the stdout channel for testing.
func (r *LocalRouter) GetStdoutChan() <-chan []byte {
	return r.stdoutChan
}

// NewForTesting creates a new LocalRouter instance for testing (no real stdio).
// This function disables real stdio handling and provides direct channel access for testing.
func NewForTesting(cfg *config.Config, logger *zap.Logger) (*LocalRouter, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	if logger == nil {
		return nil, errors.New("logger is required")
	}

	r := &LocalRouter{
		config:     cfg,
		logger:     logger,
		stdinChan:  make(chan []byte, DefaultChannelBufferSize),
		stdoutChan: make(chan []byte, DefaultChannelBufferSize),
		skipStdio:  true, // Skip real stdio for testing
	}

	// Initialize components using the same method as production.
	if err := r.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	return r, nil
}
