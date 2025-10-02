package direct

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// WebSocketRequestHandler handles WebSocket request processing.
type WebSocketRequestHandler struct {
	client *WebSocketClient
}

// CreateWebSocketRequestHandler creates a new request handler.
func CreateWebSocketRequestHandler(client *WebSocketClient) *WebSocketRequestHandler {
	return &WebSocketRequestHandler{
		client: client,
	}
}

// ProcessRequest processes a WebSocket request.
func (h *WebSocketRequestHandler) ProcessRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	if err := h.validateConnection(); err != nil {
		return nil, err
	}

	requestID := h.ensureRequestID(req)

	respCh := h.setupResponseChannel(requestID)
	defer h.cleanupResponseChannel(requestID, respCh)

	if err := h.sendRequest(req); err != nil {
		return nil, err
	}

	return h.waitForResponse(ctx, respCh)
}

func (h *WebSocketRequestHandler) validateConnection() error {
	h.client.mu.RLock()
	defer h.client.mu.RUnlock()

	if h.client.state != StateConnected && h.client.state != StateHealthy {
		return NewConnectionError(h.client.url, "websocket", "client not connected", ErrClientNotConnected)
	}

	return nil
}

func (h *WebSocketRequestHandler) ensureRequestID(req *mcp.Request) string {
	if req.ID != nil && req.ID != "" {
		if requestID, ok := req.ID.(string); ok {
			return requestID
		}

		return fmt.Sprintf("%v", req.ID)
	}

	requestID := fmt.Sprintf("%s-%d", h.client.name, atomic.AddUint64(&h.client.requestID, 1))
	req.ID = requestID

	return requestID
}

func (h *WebSocketRequestHandler) setupResponseChannel(requestID string) chan *mcp.Response {
	respCh := make(chan *mcp.Response, 1)

	h.client.requestMapMu.Lock()
	h.client.requestMap[requestID] = respCh
	h.client.requestMapMu.Unlock()

	return respCh
}

func (h *WebSocketRequestHandler) cleanupResponseChannel(requestID string, respCh chan *mcp.Response) {
	h.client.requestMapMu.Lock()
	delete(h.client.requestMap, requestID)
	close(respCh)
	h.client.requestMapMu.Unlock()
}

func (h *WebSocketRequestHandler) sendRequest(req *mcp.Request) error {
	conn := h.getConnection()
	if conn == nil {
		h.recordError()

		return NewConnectionError(h.client.url, "websocket", "connection is nil", ErrClientNotConnected)
	}

	if err := h.writeJSON(conn, req); err != nil {
		h.recordError()
		h.markUnhealthy()

		return NewRequestError(h.client.url, "websocket", "failed to send request", err)
	}

	return nil
}

func (h *WebSocketRequestHandler) getConnection() *websocket.Conn {
	h.client.connMu.RLock()
	defer h.client.connMu.RUnlock()

	return h.client.conn
}

func (h *WebSocketRequestHandler) writeJSON(conn *websocket.Conn, req *mcp.Request) error {
	h.client.writeMu.Lock()
	defer h.client.writeMu.Unlock()

	return conn.WriteJSON(req)
}

func (h *WebSocketRequestHandler) markUnhealthy() {
	h.client.mu.Lock()
	h.client.state = StateUnhealthy
	h.client.mu.Unlock()
}

func (h *WebSocketRequestHandler) waitForResponse(
	ctx context.Context,
	respCh chan *mcp.Response,
) (*mcp.Response, error) {
	timeout := h.calculateTimeout(ctx)
	startTime := time.Now()

	select {
	case resp := <-respCh:
		h.recordSuccess(time.Since(startTime))

		return resp, nil
	case <-time.After(timeout):
		h.recordError()

		return nil, NewTimeoutError(h.client.url, "websocket", timeout, nil)
	case <-ctx.Done():
		h.recordError()

		return nil, ctx.Err()
	case <-h.client.shutdownCh:
		return nil, NewShutdownError(h.client.url, "websocket", "client is shutting down", nil)
	}
}

func (h *WebSocketRequestHandler) calculateTimeout(ctx context.Context) time.Duration {
	timeout := h.client.config.Timeout

	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			return remaining
		}
	}

	return timeout
}

func (h *WebSocketRequestHandler) recordError() {
	h.client.updateMetrics(func(m *ClientMetrics) {
		m.ErrorCount++
	})
}

func (h *WebSocketRequestHandler) recordSuccess(duration time.Duration) {
	h.client.updateMetrics(func(m *ClientMetrics) {
		m.RequestCount++
		m.LastUsed = time.Now()

		if m.RequestCount == 1 {
			m.AverageLatency = duration
		} else {
			m.AverageLatency = (m.AverageLatency + duration) / DivisionFactorTwo
		}
	})
}
