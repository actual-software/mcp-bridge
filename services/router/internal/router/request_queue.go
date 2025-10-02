// Package router implements request queueing for the MCP router.
package router

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// QueuedRequest represents a request waiting to be processed.
type QueuedRequest struct {
	Data      []byte
	Timestamp time.Time
	Context   context.Context
	Response  chan error // Channel to send processing result back
}

// RequestQueue manages requests that arrive before the router is connected.
type RequestQueue struct {
	mu      sync.RWMutex
	queue   []*QueuedRequest
	maxSize int
	logger  *zap.Logger

	// Metrics.
	totalQueued    int64
	totalDropped   int64
	totalProcessed int64
}

// NewRequestQueue creates a new request queue with the specified maximum size.
func NewRequestQueue(maxSize int, logger *zap.Logger) *RequestQueue {
	if maxSize <= 0 {
		maxSize = 100 // Default queue size
	}

	return &RequestQueue{
		queue:   make([]*QueuedRequest, 0, maxSize),
		maxSize: maxSize,
		logger:  logger,
	}
}

// Returns an error if the queue is full (backpressure).
func (rq *RequestQueue) Enqueue(data []byte, ctx context.Context) (<-chan error, error) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	// Check queue capacity.
	if len(rq.queue) >= rq.maxSize {
		rq.totalDropped++

		return nil, fmt.Errorf("request queue full (size: %d)", rq.maxSize)
	}

	// Parse request to validate and log.
	var req mcp.Request
	if err := json.Unmarshal(data, &req); err == nil {
		rq.logger.Debug("Queueing request while waiting for connection",
			zap.Any("request_id", req.ID),
			zap.String("method", req.Method),
			zap.Int("queue_depth", len(rq.queue)+1))
	}

	// Create queued request with response channel.
	qr := &QueuedRequest{
		Data:      data,
		Timestamp: time.Now(),
		Context:   ctx,
		Response:  make(chan error, 1), // Buffered to prevent blocking
	}

	rq.queue = append(rq.queue, qr)
	rq.totalQueued++

	return qr.Response, nil
}

// This is called when the connection is established.
func (rq *RequestQueue) DequeueAll() []*QueuedRequest {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if len(rq.queue) == 0 {
		return nil
	}

	rq.logger.Info("Dequeueing requests for processing",
		zap.Int("count", len(rq.queue)))

	// Move all requests out of the queue.
	requests := rq.queue
	rq.queue = make([]*QueuedRequest, 0, rq.maxSize)
	rq.totalProcessed += int64(len(requests))

	return requests
}

// Size returns the current queue size.
func (rq *RequestQueue) Size() int {
	rq.mu.RLock()
	defer rq.mu.RUnlock()

	return len(rq.queue)
}

// Clear removes all queued requests and notifies them of cancellation.
func (rq *RequestQueue) Clear(err error) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if len(rq.queue) == 0 {
		return
	}

	rq.logger.Info("Clearing request queue",
		zap.Int("count", len(rq.queue)),
		zap.Error(err))

	// Notify all queued requests of the error.
	for _, qr := range rq.queue {
		select {
		case qr.Response <- err:
		default:
			// Channel might be closed or full, ignore.
		}

		close(qr.Response)
	}

	rq.totalDropped += int64(len(rq.queue))
	rq.queue = make([]*QueuedRequest, 0, rq.maxSize)
}

// GetMetrics returns queue metrics.
func (rq *RequestQueue) GetMetrics() (queued, dropped, processed int64) {
	rq.mu.RLock()
	defer rq.mu.RUnlock()

	return rq.totalQueued, rq.totalDropped, rq.totalProcessed
}

// IsEmpty returns true if the queue is empty.
func (rq *RequestQueue) IsEmpty() bool {
	rq.mu.RLock()
	defer rq.mu.RUnlock()

	return len(rq.queue) == 0
}

// IsFull returns true if the queue is at capacity.
func (rq *RequestQueue) IsFull() bool {
	rq.mu.RLock()
	defer rq.mu.RUnlock()

	return len(rq.queue) >= rq.maxSize
}
