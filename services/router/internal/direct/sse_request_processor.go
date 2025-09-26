package direct

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// Request processor constants.
const (
	ProcessorDivider = 2
)

// SSERequestProcessor handles SSE client request processing with proper separation of concerns.
type SSERequestProcessor struct {
	client *SSEClient
}

// InitializeSSERequestProcessor creates a new SSE request processor.
func InitializeSSERequestProcessor(client *SSEClient) *SSERequestProcessor {
	return &SSERequestProcessor{
		client: client,
	}
}

// ProcessSSERequest processes an SSE request through the complete pipeline.
func (p *SSERequestProcessor) ProcessSSERequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	// Validate connection state.
	if err := p.validateConnectionState(); err != nil {
		return nil, err
	}

	// Prepare request with ID.
	requestID := p.prepareRequestID(req)

	// Setup response channel.
	respCh := p.setupResponseChannel(requestID)
	defer p.cleanupResponseChannel(requestID, respCh)

	// Send the request.
	startTime := time.Now()

	if err := p.sendRequest(ctx, req); err != nil {
		p.recordError()

		return nil, err
	}

	// Wait for response.
	return p.waitForResponse(ctx, respCh, startTime)
}

// validateConnectionState checks if the client is connected.
func (p *SSERequestProcessor) validateConnectionState() error {
	p.client.mu.RLock()
	defer p.client.mu.RUnlock()

	if p.client.state != StateConnected && p.client.state != StateHealthy {
		return NewConnectionError(p.client.url, "sse", "client not connected", ErrClientNotConnected)
	}

	return nil
}

// prepareRequestID ensures the request has a unique ID.
func (p *SSERequestProcessor) prepareRequestID(req *mcp.Request) string {
	if req.ID == nil || req.ID == "" {
		requestID := fmt.Sprintf("%s-%d", p.client.name, atomic.AddUint64(&p.client.requestID, 1))
		req.ID = requestID

		return requestID
	}

	return p.extractRequestIDString(req.ID)
}

// extractRequestIDString converts request ID to string.
func (p *SSERequestProcessor) extractRequestIDString(id interface{}) string {
	if strID, ok := id.(string); ok {
		return strID
	}

	return fmt.Sprintf("%v", id)
}

const responseChannelBufferSize = 10 // Buffer size for response channels

// setupResponseChannel creates and registers a response channel.
func (p *SSERequestProcessor) setupResponseChannel(requestID string) chan *mcp.Response {
	respCh := make(chan *mcp.Response, responseChannelBufferSize) // Buffered for better concurrency

	p.client.requestMapMu.Lock()
	p.client.pendingRequests[requestID] = respCh
	p.client.requestMapMu.Unlock()

	return respCh
}

// cleanupResponseChannel removes and closes the response channel.
func (p *SSERequestProcessor) cleanupResponseChannel(requestID string, respCh chan *mcp.Response) {
	p.client.requestMapMu.Lock()
	delete(p.client.pendingRequests, requestID)
	close(respCh)
	p.client.requestMapMu.Unlock()
}

// sendRequest sends the HTTP request.
func (p *SSERequestProcessor) sendRequest(ctx context.Context, req *mcp.Request) error {
	return p.client.sendHTTPRequest(ctx, req)
}

// waitForResponse waits for a response with timeout handling.
func (p *SSERequestProcessor) waitForResponse(
	ctx context.Context,
	respCh chan *mcp.Response,
	startTime time.Time,
) (*mcp.Response, error) {
	timeout := p.calculateTimeout(ctx)

	select {
	case resp := <-respCh:
		p.recordSuccess(startTime)

		return resp, nil

	case <-time.After(timeout):
		p.recordError()

		return nil, NewTimeoutError(p.client.url, "sse", timeout, nil)

	case <-ctx.Done():
		p.recordError()

		return nil, ctx.Err()

	case <-p.client.shutdownCh:
		return nil, NewShutdownError(p.client.url, "sse", "client is shutting down", nil)
	}
}

// calculateTimeout determines the appropriate timeout to use.
func (p *SSERequestProcessor) calculateTimeout(ctx context.Context) time.Duration {
	timeout := p.client.config.RequestTimeout

	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			return remaining
		}
	}

	return timeout
}

// recordSuccess updates metrics for successful request.
func (p *SSERequestProcessor) recordSuccess(startTime time.Time) {
	duration := time.Since(startTime)

	p.client.updateMetrics(func(m *ClientMetrics) {
		m.RequestCount++
		m.LastUsed = time.Now()

		// Simple moving average for latency.
		if m.RequestCount == 1 {
			m.AverageLatency = duration
		} else {
			m.AverageLatency = (m.AverageLatency + duration) / ProcessorDivider
		}
	})
}

// recordError updates error metrics.
func (p *SSERequestProcessor) recordError() {
	p.client.updateMetrics(func(m *ClientMetrics) {
		m.ErrorCount++
	})
}
