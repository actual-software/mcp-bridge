package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// StdioRequestHandler manages request sending for StdioClient.
type StdioRequestHandler struct {
	client *StdioClient
	logger *zap.Logger
}

// CreateStdioRequestHandler creates a new request handler.
func CreateStdioRequestHandler(client *StdioClient) *StdioRequestHandler {
	return &StdioRequestHandler{
		client: client,
		logger: client.logger,
	}
}

// ProcessRequest handles sending a request and receiving a response.
func (h *StdioRequestHandler) ProcessRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	if err := h.validateClientState(); err != nil {
		return nil, err
	}

	requestID := h.ensureRequestID(req)
	respCh := h.registerRequest(requestID)

	defer h.cleanupRequest(requestID, respCh)

	if err := h.sendRequest(req); err != nil {
		return nil, err
	}

	return h.waitForResponse(ctx, requestID, respCh)
}

// validateClientState checks if the client is ready to send requests.
func (h *StdioRequestHandler) validateClientState() error {
	h.client.mu.RLock()
	defer h.client.mu.RUnlock()

	if h.client.state != StateConnected && h.client.state != StateHealthy {
		return NewConnectionError(h.client.url, "stdio", "client not connected", ErrClientNotConnected)
	}

	return nil
}

// ensureRequestID ensures the request has a unique ID.
func (h *StdioRequestHandler) ensureRequestID(req *mcp.Request) string {
	if req.ID == nil || req.ID == "" {
		requestID := fmt.Sprintf("%s-%d", h.client.name, atomic.AddUint64(&h.client.requestID, 1))
		req.ID = requestID

		return requestID
	}

	requestID, ok := req.ID.(string)
	if !ok {
		requestID = fmt.Sprintf("%v", req.ID)
	}

	return requestID
}

// registerRequest registers a request and creates a response channel.
func (h *StdioRequestHandler) registerRequest(requestID string) chan *mcp.Response {
	respCh := make(chan *mcp.Response, 1)

	h.client.requestMapMu.Lock()
	h.client.requestMap[requestID] = respCh
	h.client.requestMapMu.Unlock()

	return respCh
}

// cleanupRequest removes the request from the map and closes the channel.
func (h *StdioRequestHandler) cleanupRequest(requestID string, respCh chan *mcp.Response) {
	h.client.requestMapMu.Lock()
	delete(h.client.requestMap, requestID)
	close(respCh)
	h.client.requestMapMu.Unlock()
}

// sendRequest sends the request through the appropriate method.
func (h *StdioRequestHandler) sendRequest(req *mcp.Request) error {
	h.client.writeMu.Lock()
	defer h.client.writeMu.Unlock()

	if h.shouldUseMemoryOptimizer() {
		return h.sendWithMemoryOptimizer(req)
	}

	return h.sendStandard(req)
}

// shouldUseMemoryOptimizer determines if memory optimizer should be used.
func (h *StdioRequestHandler) shouldUseMemoryOptimizer() bool {
	return h.client.memoryOptimizer != nil && h.client.config.Performance.EnableBufferedIO
}

// sendWithMemoryOptimizer sends the request using memory optimization.
func (h *StdioRequestHandler) sendWithMemoryOptimizer(req *mcp.Request) error {
	// Get buffer from pool.
	reqBuffer := h.client.memoryOptimizer.GetBuffer()
	defer h.client.memoryOptimizer.PutBuffer(reqBuffer)

	// Encode to buffer.
	tempEncoder := json.NewEncoder(reqBuffer)
	if err := tempEncoder.Encode(req); err != nil {
		h.recordError()

		return NewRequestError(h.client.url, "stdio", "failed to encode request", err)
	}

	// Write to stdin.
	if _, err := h.client.stdinBuf.Write(reqBuffer.Bytes()); err != nil {
		h.recordError()

		return NewRequestError(h.client.url, "stdio", "failed to send request", err)
	}

	return h.flushBuffer()
}

// sendStandard sends the request using standard encoding.
func (h *StdioRequestHandler) sendStandard(req *mcp.Request) error {
	if err := h.client.stdinEncoder.Encode(req); err != nil {
		h.recordError()

		return NewRequestError(h.client.url, "stdio", "failed to encode and send request", err)
	}

	if h.client.config.Performance.EnableBufferedIO && h.client.stdinBuf != nil {
		return h.flushBuffer()
	}

	return nil
}

// flushBuffer flushes the buffered writer.
func (h *StdioRequestHandler) flushBuffer() error {
	if err := h.client.stdinBuf.Flush(); err != nil {
		h.recordError()

		return NewRequestError(h.client.url, "stdio", "failed to flush request buffer", err)
	}

	return nil
}

// waitForResponse waits for the response with timeout.
func (h *StdioRequestHandler) waitForResponse(
	ctx context.Context,
	requestID string,
	respCh chan *mcp.Response,
) (*mcp.Response, error) {
	timeout := h.calculateTimeout(ctx)
	startTime := time.Now()

	select {
	case resp := <-respCh:
		h.recordSuccess(time.Since(startTime))

		return h.validateResponse(resp, requestID)

	case <-time.After(timeout):
		h.recordError()

		return nil, NewRequestError(h.client.url, "stdio",
			fmt.Sprintf("request timeout after %v", timeout), ErrRequestTimeout)

	case <-ctx.Done():
		h.recordError()

		return nil, NewRequestError(h.client.url, "stdio", "request cancelled", ctx.Err())

	case <-h.client.shutdownCh:
		h.recordError()

		return nil, NewRequestError(h.client.url, "stdio", "client is shutting down", ErrClientNotConnected)
	}
}

// calculateTimeout determines the appropriate timeout for the request.
func (h *StdioRequestHandler) calculateTimeout(ctx context.Context) time.Duration {
	timeout := h.client.config.Timeout

	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}

	return timeout
}

// validateResponse validates the received response.
func (h *StdioRequestHandler) validateResponse(resp *mcp.Response, requestID string) (*mcp.Response, error) {
	if resp == nil {
		return nil, NewRequestError(h.client.url, "stdio",
			"received nil response", nil)
	}

	// Check if response has an error.
	if resp.Error != nil {
		h.logger.Warn("request returned error",
			zap.String("request_id", requestID),
			zap.Int("error_code", resp.Error.Code),
			zap.String("error_message", resp.Error.Message))

		return resp, nil // Return the response with error
	}

	return resp, nil
}

// recordError increments the error counter.
func (h *StdioRequestHandler) recordError() {
	h.client.updateMetrics(func(m *ClientMetrics) {
		m.ErrorCount++
	})
}

// recordSuccess updates success metrics.
func (h *StdioRequestHandler) recordSuccess(duration time.Duration) {
	h.client.updateMetrics(func(m *ClientMetrics) {
		m.RequestCount++

		if m.RequestCount == 1 {
			m.AverageLatency = duration
		} else {
			// Simple moving average.
			m.AverageLatency = (m.AverageLatency + duration) / DivisionFactorTwo
		}
	})
}
