package direct

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// WebSocketMessageReader handles WebSocket message reading.
type WebSocketMessageReader struct {
	client *WebSocketClient
}

// CreateWebSocketMessageReader creates a new message reader.
func CreateWebSocketMessageReader(client *WebSocketClient) *WebSocketMessageReader {
	return &WebSocketMessageReader{
		client: client,
	}
}

// ReadMessageLoop continuously reads messages from WebSocket.
func (r *WebSocketMessageReader) ReadMessageLoop(ctx context.Context) {
	defer r.cleanup()

	for r.shouldContinueReading() {
		r.readSingleMessage(ctx)
	}
}

func (r *WebSocketMessageReader) cleanup() {
	r.client.wg.Done()
	// Use safe channel close to avoid panic from double close
	r.safeCloseDoneChannel()
}

func (r *WebSocketMessageReader) safeCloseDoneChannel() {
	select {
	case <-r.client.doneCh:
		// Already closed.
	default:
		close(r.client.doneCh)
	}
}

func (r *WebSocketMessageReader) shouldContinueReading() bool {
	select {
	case <-r.client.shutdownCh:
		return false
	default:
		// Also check if the client is in a valid state for reading
		r.client.mu.RLock()
		state := r.client.state
		r.client.mu.RUnlock()

		// Don't continue reading if client is closed, closing, or in error state
		if state == StateClosed || state == StateClosing || state == StateError {
			return false
		}

		return true
	}
}

func (r *WebSocketMessageReader) readSingleMessage(ctx context.Context) {
	conn := r.getConnection()
	if conn == nil {
		r.handleNilConnection()

		return
	}

	// Additional state check right before reading to minimize race conditions
	r.client.mu.RLock()
	state := r.client.state
	r.client.mu.RUnlock()

	if state == StateClosed || state == StateClosing || state == StateError {
		// Don't log if shutting down to avoid logging after test completion
		select {
		case <-r.client.shutdownCh:
			return
		default:
			r.client.logger.Debug("skipping read on closed/closing/error state connection")

			return
		}
	}

	response, err := r.readResponse(conn)
	if err != nil {
		r.handleReadError(ctx, err)

		return
	}

	r.routeResponse(response)
}

func (r *WebSocketMessageReader) getConnection() *websocket.Conn {
	r.client.connMu.RLock()
	defer r.client.connMu.RUnlock()

	return r.client.conn
}

func (r *WebSocketMessageReader) handleNilConnection() {
	r.client.logger.Debug("WebSocket connection is nil, waiting for reconnection")
	time.Sleep(time.Second)
}

func (r *WebSocketMessageReader) readResponse(conn *websocket.Conn) (response *mcp.Response, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			// Handle the "repeated read on failed websocket connection" panic
			r.client.logger.Error("recovered from websocket read panic, marking connection as failed")
			r.client.mu.Lock()
			r.client.state = StateError
			r.client.mu.Unlock()
			// Return an error to indicate the read failed
			err = errors.New("websocket connection failed - recovered from panic")
			response = nil
		}
	}()

	var resp mcp.Response

	err = conn.ReadJSON(&resp)

	return &resp, err
}

func (r *WebSocketMessageReader) handleReadError(ctx context.Context, err error) {
	if r.isClosedError(err) {
		r.handleClosedConnection(ctx)

		return
	}

	r.logReadError(err)
	r.recordErrorMetrics()
}

func (r *WebSocketMessageReader) isClosedError(err error) bool {
	if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return true
	}

	return errors.Is(err, io.EOF)
}

func (r *WebSocketMessageReader) handleClosedConnection(ctx context.Context) {
	r.client.logger.Info("WebSocket connection closed")

	r.client.mu.Lock()

	if r.client.state != StateClosing && r.client.state != StateClosed {
		r.client.state = StateDisconnected
	}

	r.client.mu.Unlock()

	r.client.updateMetrics(func(m *ClientMetrics) {
		m.IsHealthy = false
	})

	r.attemptReconnection(ctx)
}

func (r *WebSocketMessageReader) attemptReconnection(ctx context.Context) {
	if r.client.shouldReconnect() {
		go r.client.reconnect(ctx)
	}
}

func (r *WebSocketMessageReader) logReadError(err error) {
	r.client.logger.Error("failed to read response",
		zap.Error(err),
		zap.String("client_name", r.client.name),
		zap.Duration("uptime", time.Since(r.client.startTime)))
}

func (r *WebSocketMessageReader) recordErrorMetrics() {
	r.client.updateMetrics(func(m *ClientMetrics) {
		m.ErrorCount++
		m.IsHealthy = false
	})
}

func (r *WebSocketMessageReader) routeResponse(response *mcp.Response) {
	responseID := r.extractResponseID(response)
	if responseID == "" {
		return
	}

	r.deliverResponse(responseID, response)
}

func (r *WebSocketMessageReader) extractResponseID(response *mcp.Response) string {
	responseID, ok := response.ID.(string)
	if !ok {
		r.client.logger.Warn("received response with invalid ID type",
			zap.Any("response_id", response.ID))

		return ""
	}

	return responseID
}

func (r *WebSocketMessageReader) deliverResponse(responseID string, response *mcp.Response) {
	r.client.requestMapMu.RLock()
	defer r.client.requestMapMu.RUnlock()

	ch, exists := r.client.requestMap[responseID]
	if !exists {
		r.client.logger.Warn("received response for unknown request",
			zap.String("request_id", responseID))

		return
	}

	select {
	case ch <- response:
		// Response delivered successfully.
	default:
		r.client.logger.Warn("response channel full, dropping response",
			zap.String("request_id", responseID),
			zap.String("client_name", r.client.name),
			zap.Uint64("active_requests", uint64(len(r.client.requestMap))))
	}
}
