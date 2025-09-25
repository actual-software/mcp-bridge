package wire

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// Transport provides framed message transport over a connection.
type Transport struct {
	conn       net.Conn
	reader     *bufio.Reader
	writer     *bufio.Writer
	writeMutex sync.Mutex
	readMutex  sync.Mutex
}

// NewTransport creates a new transport from a connection.
func NewTransport(conn net.Conn) *Transport {
	return &Transport{
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}
}

// SendRequest sends an MCP request.
func (t *Transport) SendRequest(req *mcp.Request) error {
	// Marshal request to JSON
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create frame
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeRequest,
		Payload:     payload,
	}

	// Send frame
	t.writeMutex.Lock()
	defer t.writeMutex.Unlock()

	if err := frame.Encode(t.writer); err != nil {
		return fmt.Errorf("failed to encode frame: %w", err)
	}

	if err := t.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// SendResponse sends an MCP response.
func (t *Transport) SendResponse(resp *mcp.Response) error {
	// Marshal response to JSON
	payload, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	// Create frame
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeResponse,
		Payload:     payload,
	}

	// Send frame
	t.writeMutex.Lock()
	defer t.writeMutex.Unlock()

	if err := frame.Encode(t.writer); err != nil {
		return fmt.Errorf("failed to encode frame: %w", err)
	}

	if err := t.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// ReceiveMessage receives a message from the transport.
func (t *Transport) ReceiveMessage() (MessageType, interface{}, error) {
	t.readMutex.Lock()
	defer t.readMutex.Unlock()

	// Decode frame
	frame, err := Decode(t.reader)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to decode frame: %w", err)
	}

	// Handle based on message type
	return t.handleReceivedFrame(frame)
}

// handleReceivedFrame processes a received frame based on its message type.
func (t *Transport) handleReceivedFrame(frame *Frame) (MessageType, interface{}, error) {
	switch frame.MessageType {
	case MessageTypeVersionNegotiation:
		return t.handleVersionNegotiation(frame)
	case MessageTypeVersionAck:
		return t.handleVersionAck(frame)
	case MessageTypeRequest:
		return t.handleRequest(frame)
	case MessageTypeResponse:
		return t.handleResponse(frame)
	case MessageTypeHealthCheck:
		return t.handleHealthCheck(frame)
	case MessageTypeError:
		return t.handleError(frame)
	case MessageTypeControl:
		return t.handleControl(frame)
	default:
		return frame.MessageType, nil, fmt.Errorf("unknown message type: 0x%04X", frame.MessageType)
	}
}

// handleVersionNegotiation processes version negotiation messages.
func (t *Transport) handleVersionNegotiation(frame *Frame) (MessageType, interface{}, error) {
	var negotiation VersionNegotiationPayload
	if err := json.Unmarshal(frame.Payload, &negotiation); err != nil {
		return frame.MessageType, nil, fmt.Errorf("failed to unmarshal version negotiation: %w", err)
	}

	return frame.MessageType, &negotiation, nil
}

// handleVersionAck processes version acknowledgment messages.
func (t *Transport) handleVersionAck(frame *Frame) (MessageType, interface{}, error) {
	var ack VersionAckPayload
	if err := json.Unmarshal(frame.Payload, &ack); err != nil {
		return frame.MessageType, nil, fmt.Errorf("failed to unmarshal version ack: %w", err)
	}

	return frame.MessageType, &ack, nil
}

// handleRequest processes request messages, supporting both authenticated and regular requests.
func (t *Transport) handleRequest(frame *Frame) (MessageType, interface{}, error) {
	// Try to unmarshal as AuthMessage first
	var authMsg AuthMessage
	if err := json.Unmarshal(frame.Payload, &authMsg); err == nil && authMsg.Message != nil {
		// This is an authenticated message
		return frame.MessageType, &authMsg, nil
	}

	// Fall back to regular request
	var req mcp.Request
	if err := json.Unmarshal(frame.Payload, &req); err != nil {
		return frame.MessageType, nil, fmt.Errorf("failed to unmarshal request: %w", err)
	}

	return frame.MessageType, &req, nil
}

// handleResponse processes response messages, supporting both authenticated and regular responses.
func (t *Transport) handleResponse(frame *Frame) (MessageType, interface{}, error) {
	// Try to unmarshal as AuthMessage first
	var authMsg AuthMessage
	if err := json.Unmarshal(frame.Payload, &authMsg); err == nil && authMsg.Message != nil {
		// This is an authenticated message
		return frame.MessageType, &authMsg, nil
	}

	// Fall back to regular response
	var resp mcp.Response
	if err := json.Unmarshal(frame.Payload, &resp); err != nil {
		return frame.MessageType, nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return frame.MessageType, &resp, nil
}

// handleHealthCheck processes health check messages.
func (t *Transport) handleHealthCheck(frame *Frame) (MessageType, interface{}, error) {
	// Health checks may have JSON payload
	if len(frame.Payload) == 0 {
		return frame.MessageType, nil, nil
	}

	var healthMsg map[string]interface{}
	if err := json.Unmarshal(frame.Payload, &healthMsg); err != nil {
		return frame.MessageType, nil, fmt.Errorf("failed to unmarshal health check: %w", err)
	}

	return frame.MessageType, healthMsg, nil
}

// handleError processes error messages.
func (t *Transport) handleError(frame *Frame) (MessageType, interface{}, error) {
	// Error messages are just strings
	return frame.MessageType, string(frame.Payload), nil
}

// handleControl processes control messages.
func (t *Transport) handleControl(frame *Frame) (MessageType, interface{}, error) {
	// Control messages are JSON objects
	var control map[string]interface{}
	if len(frame.Payload) > 0 {
		if err := json.Unmarshal(frame.Payload, &control); err != nil {
			return frame.MessageType, nil, fmt.Errorf("failed to unmarshal control message: %w", err)
		}
	}

	return frame.MessageType, control, nil
}

// SendError sends an error message.
func (t *Transport) SendError(errMsg string) error {
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeError,
		Payload:     []byte(errMsg),
	}

	t.writeMutex.Lock()
	defer t.writeMutex.Unlock()

	if err := frame.Encode(t.writer); err != nil {
		return fmt.Errorf("failed to encode error frame: %w", err)
	}

	if err := t.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// SendHealthCheck sends a health check message.
func (t *Transport) SendHealthCheck() error {
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeHealthCheck,
		Payload:     []byte{},
	}

	t.writeMutex.Lock()
	defer t.writeMutex.Unlock()

	if err := frame.Encode(t.writer); err != nil {
		return fmt.Errorf("failed to encode health check frame: %w", err)
	}

	if err := t.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// SendControl sends a control message.
func (t *Transport) SendControl(control map[string]interface{}) error {
	// Marshal control message to JSON
	payload, err := json.Marshal(control)
	if err != nil {
		return fmt.Errorf("failed to marshal control message: %w", err)
	}

	// Create frame
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeControl,
		Payload:     payload,
	}

	// Send frame
	t.writeMutex.Lock()
	defer t.writeMutex.Unlock()

	if err := frame.Encode(t.writer); err != nil {
		return fmt.Errorf("failed to encode control frame: %w", err)
	}

	if err := t.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// SendVersionNegotiation sends a version negotiation message.
func (t *Transport) SendVersionNegotiation(negotiation *VersionNegotiationPayload) error {
	// Marshal negotiation to JSON
	payload, err := json.Marshal(negotiation)
	if err != nil {
		return fmt.Errorf("failed to marshal version negotiation: %w", err)
	}

	// Create frame
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeVersionNegotiation,
		Payload:     payload,
	}

	// Send frame
	t.writeMutex.Lock()
	defer t.writeMutex.Unlock()

	if err := frame.Encode(t.writer); err != nil {
		return fmt.Errorf("failed to encode version negotiation frame: %w", err)
	}

	if err := t.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// SendVersionAck sends a version acknowledgment message.
func (t *Transport) SendVersionAck(ack *VersionAckPayload) error {
	// Marshal ack to JSON
	payload, err := json.Marshal(ack)
	if err != nil {
		return fmt.Errorf("failed to marshal version ack: %w", err)
	}

	// Create frame
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeVersionAck,
		Payload:     payload,
	}

	// Send frame
	t.writeMutex.Lock()
	defer t.writeMutex.Unlock()

	if err := frame.Encode(t.writer); err != nil {
		return fmt.Errorf("failed to encode version ack frame: %w", err)
	}

	if err := t.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// Close closes the transport.
func (t *Transport) Close() error {
	return t.conn.Close()
}

// EncodeJSON is a helper to encode any value to JSON.
func (t *Transport) EncodeJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// SetDeadline sets read and write deadlines.
func (t *Transport) SetDeadline(deadline time.Time) error {
	return t.conn.SetDeadline(deadline)
}

// SetReadDeadline sets the read deadline.
func (t *Transport) SetReadDeadline(deadline time.Time) error {
	return t.conn.SetReadDeadline(deadline)
}

// SetWriteDeadline sets the write deadline.
func (t *Transport) SetWriteDeadline(deadline time.Time) error {
	return t.conn.SetWriteDeadline(deadline)
}

// RemoteAddr returns the remote address.
func (t *Transport) RemoteAddr() net.Addr {
	return t.conn.RemoteAddr()
}

// LocalAddr returns the local address.
func (t *Transport) LocalAddr() net.Addr {
	return t.conn.LocalAddr()
}

// GetWriter returns the underlying writer for direct frame encoding.
func (t *Transport) GetWriter() io.Writer {
	return t.writer
}

// Flush flushes the writer.
func (t *Transport) Flush() error {
	t.writeMutex.Lock()
	defer t.writeMutex.Unlock()

	return t.writer.Flush()
}
