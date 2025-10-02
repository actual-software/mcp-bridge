package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// ResponseReceiver handles receiving and processing responses from the gateway.
type ResponseReceiver struct {
	client *Client
	logger *zap.Logger
}

// InitializeResponseReceiver creates a new response receiver.
func InitializeResponseReceiver(client *Client) *ResponseReceiver {
	return &ResponseReceiver{
		client: client,
		logger: client.logger,
	}
}

// ReceiveGatewayResponse receives and processes a response from the gateway.
func (r *ResponseReceiver) ReceiveGatewayResponse() (*mcp.Response, error) {
	// Validate connection.
	conn, err := r.validateAndGetConnection()
	if err != nil {
		return nil, err
	}

	// Setup read operation with recovery.
	defer r.recoverFromPanic()

	// Perform read operation.
	msg, err := r.readMessageFromConnection(conn)
	if err != nil {
		return nil, err
	}

	// Process and validate response.
	return r.processResponseMessage(msg)
}

// validateAndGetConnection validates the connection is ready for reading.
func (r *ResponseReceiver) validateAndGetConnection() (*websocket.Conn, error) {
	// Get connection with minimal lock time.
	r.client.connMu.Lock()
	conn := r.client.conn
	r.client.connMu.Unlock()

	if conn == nil {
		return nil, errors.New("not connected")
	}

	// Acquire read lock.
	r.client.readMu.Lock()

	// Double-check connection hasn't changed.
	r.client.connMu.Lock()
	defer r.client.connMu.Unlock()

	if r.client.conn == nil || r.client.conn != conn {
		r.client.readMu.Unlock()

		return nil, errors.New("connection changed during receive")
	}

	return r.client.conn, nil
}

// recoverFromPanic handles panic recovery during read operations.
func (r *ResponseReceiver) recoverFromPanic() {
	r.client.readMu.Unlock()

	if rec := recover(); rec != nil {
		r.logger.Error("Panic during WebSocket read", zap.Any("panic", rec))
		r.client.markConnectionClosed()
	}
}

// readMessageFromConnection reads a message from the WebSocket connection.
func (r *ResponseReceiver) readMessageFromConnection(conn *websocket.Conn) (*WireMessage, error) {
	// Set read deadline.
	if err := r.setReadDeadline(conn); err != nil {
		r.logger.Warn("Failed to set read deadline", zap.Error(err))
	}

	// Read message.
	var msg WireMessage
	if err := conn.ReadJSON(&msg); err != nil {
		return nil, r.handleReadError(err)
	}

	// Reset deadline after successful read.
	if err := r.resetReadDeadline(conn); err != nil {
		r.logger.Warn("Failed to reset read deadline", zap.Error(err))
	}

	return &msg, nil
}

// setReadDeadline sets the read deadline for the connection.
func (r *ResponseReceiver) setReadDeadline(conn *websocket.Conn) error {
	return conn.SetReadDeadline(time.Now().Add(defaultTimeoutSeconds * time.Second))
}

// resetReadDeadline resets the read deadline.
func (r *ResponseReceiver) resetReadDeadline(conn *websocket.Conn) error {
	return conn.SetReadDeadline(time.Time{})
}

// handleReadError classifies and handles read errors.
func (r *ResponseReceiver) handleReadError(err error) error {
	// Check for normal close errors.
	if r.isNormalCloseError(err) {
		r.logger.Info("WebSocket connection closed", zap.Error(err))
		r.client.markConnectionClosed()

		return fmt.Errorf("connection closed: %w", err)
	}

	// Check for unexpected close errors.
	if r.isUnexpectedCloseError(err) {
		r.logger.Error("Unexpected WebSocket close", zap.Error(err))
		r.client.markConnectionClosed()

		return fmt.Errorf("unexpected close: %w", err)
	}

	// Check for timeout errors.
	if r.isTimeoutError(err) {
		r.logger.Debug("WebSocket read timeout")

		return fmt.Errorf("read timeout: %w", err)
	}

	// Generic read error.
	r.logger.Error("WebSocket read error", zap.Error(err))

	return fmt.Errorf("read error: %w", err)
}

// isNormalCloseError checks if the error is a normal close.
func (r *ResponseReceiver) isNormalCloseError(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseAbnormalClosure)
}

// isUnexpectedCloseError checks if the error is an unexpected close.
func (r *ResponseReceiver) isUnexpectedCloseError(err error) bool {
	return websocket.IsUnexpectedCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway)
}

// isTimeoutError checks if the error is a timeout.
func (r *ResponseReceiver) isTimeoutError(err error) bool {
	var netErr net.Error

	return errors.As(err, &netErr) && netErr.Timeout()
}

// processResponseMessage processes the wire message into an MCP response.
func (r *ResponseReceiver) processResponseMessage(msg *WireMessage) (*mcp.Response, error) {
	// Validate message has payload.
	if err := r.validateMessagePayload(msg); err != nil {
		return nil, err
	}

	// Convert to MCP response.
	resp, err := r.convertToMCPResponse(msg)
	if err != nil {
		return nil, err
	}

	// Validate response structure.
	if err := r.validateResponseStructure(resp); err != nil {
		return nil, err
	}

	r.logResponseReceived(resp)

	return resp, nil
}

// validateMessagePayload validates the message has an MCP payload.
func (r *ResponseReceiver) validateMessagePayload(msg *WireMessage) error {
	if msg.MCPPayload == nil {
		r.logger.Error("Received message without MCP payload", zap.Any("message", msg))

		return errors.New("received message without MCP payload")
	}

	return nil
}

// convertToMCPResponse converts the wire message to an MCP response.
func (r *ResponseReceiver) convertToMCPResponse(msg *WireMessage) (*mcp.Response, error) {
	// Marshal payload to JSON.
	data, err := json.Marshal(msg.MCPPayload)
	if err != nil {
		r.logger.Error("Failed to marshal MCP payload",
			zap.Error(err),
			zap.Any("payload", msg.MCPPayload))

		return nil, fmt.Errorf("failed to marshal MCP payload: %w", err)
	}

	// Unmarshal to MCP response.
	var resp mcp.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		r.logger.Error("Failed to unmarshal MCP response",
			zap.Error(err),
			zap.ByteString("data", data))

		return nil, fmt.Errorf("failed to unmarshal MCP response: %w", err)
	}

	return &resp, nil
}

// validateResponseStructure validates the response has required fields.
func (r *ResponseReceiver) validateResponseStructure(resp *mcp.Response) error {
	if resp.ID == nil && resp.Error == nil && resp.Result == nil {
		r.logger.Warn("Received response with no ID, error, or result",
			zap.Any("response", resp))

		return errors.New("invalid response structure")
	}

	return nil
}

// logResponseReceived logs the received response.
func (r *ResponseReceiver) logResponseReceived(resp *mcp.Response) {
	r.logger.Debug("Received response",
		zap.Any("id", resp.ID),
		zap.Bool("has_error", resp.Error != nil),
		zap.Bool("has_result", resp.Result != nil))
}
