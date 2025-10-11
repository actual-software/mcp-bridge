package gateway

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"syscall"
)

// Binary protocol constants.

// wrapConnectionError wraps connection-related errors with a more descriptive message.
func wrapConnectionError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// Check for connection-related errors
	errStr := err.Error()
	if errors.Is(err, io.EOF) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset") {
		return fmt.Errorf("%s: connection closed unexpectedly", operation)
	}

	return fmt.Errorf("%s: %w", operation, err)
}

// MessageType represents the type of message in binary protocol.
type MessageType uint16

const (
	// MessageTypeRequest represents an MCP request.
	MessageTypeRequest MessageType = 0x0001

	// MessageTypeResponse represents an MCP response.
	MessageTypeResponse MessageType = 0x0002

	// MessageTypeControl represents a control message.
	MessageTypeControl MessageType = 0x0003

	// MessageTypeHealthCheck represents a health check.
	MessageTypeHealthCheck MessageType = 0x0004

	// MessageTypeError represents an error message.
	MessageTypeError MessageType = 0x0005

	// MessageTypeVersionNegotiation represents version negotiation.
	MessageTypeVersionNegotiation MessageType = 0x0006

	// MessageTypeVersionAck represents version acknowledgment.
	MessageTypeVersionAck MessageType = 0x0007
)

// String returns the string representation of MessageType.
func (m MessageType) String() string {
	return fmt.Sprintf("MessageType(%d)", uint16(m))
}

// BinaryFrame represents a binary protocol frame.
type BinaryFrame struct {
	Magic       uint32
	Version     uint16
	MessageType MessageType
	PayloadLen  uint32
	Payload     []byte
}

// WriteToWriter writes the frame to the writer.
func (f *BinaryFrame) WriteToWriter(w io.Writer) error {
	// Write magic bytes.
	if err := binary.Write(w, binary.BigEndian, uint32(MagicBytes)); err != nil {
		return wrapConnectionError(err, "failed to write magic bytes")
	}

	// Write version.
	if err := binary.Write(w, binary.BigEndian, f.Version); err != nil {
		return wrapConnectionError(err, "failed to write version")
	}

	// Write message type.
	if err := binary.Write(w, binary.BigEndian, f.MessageType); err != nil {
		return wrapConnectionError(err, "failed to write message type")
	}

	// Write reserved padding (4 bytes) to align to 16-byte header.
	if err := binary.Write(w, binary.BigEndian, uint32(0)); err != nil {
		return wrapConnectionError(err, "failed to write reserved padding")
	}

	// Write payload length with bounds checking.
	if len(f.Payload) > math.MaxUint32 {
		return fmt.Errorf("payload too large: %d bytes exceeds uint32 limit", len(f.Payload))
	}
	// Safe conversion after bounds check
	payloadLen := uint32(len(f.Payload)) // #nosec G115 - bounds checked above
	if err := binary.Write(w, binary.BigEndian, payloadLen); err != nil {
		return wrapConnectionError(err, "failed to write payload length")
	}

	// Write payload.
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return wrapConnectionError(err, "failed to write payload")
		}
	}

	return nil
}

// Write writes the frame to a writer.
func (f *BinaryFrame) Write(w io.Writer) error {
	return f.WriteToWriter(w)
}

// ReadBinaryFrame reads a frame from the reader.
func ReadBinaryFrame(r io.Reader) (*BinaryFrame, error) {
	// Read and verify magic bytes.
	var magic uint32
	if err := binary.Read(r, binary.BigEndian, &magic); err != nil {
		return nil, wrapConnectionError(err, "failed to read magic bytes")
	}

	if magic != MagicBytes {
		return nil, fmt.Errorf("invalid magic bytes: 0x%08X", magic)
	}

	// Read version.
	var version uint16
	if err := binary.Read(r, binary.BigEndian, &version); err != nil {
		return nil, wrapConnectionError(err, "failed to read version")
	}

	// Read message type.
	var msgType uint16
	if err := binary.Read(r, binary.BigEndian, &msgType); err != nil {
		return nil, wrapConnectionError(err, "failed to read message type")
	}

	// Read reserved padding (4 bytes) to align with 16-byte header.
	var reserved uint32
	if err := binary.Read(r, binary.BigEndian, &reserved); err != nil {
		return nil, wrapConnectionError(err, "failed to read reserved padding")
	}

	// Read payload length.
	var payloadLen uint32
	if err := binary.Read(r, binary.BigEndian, &payloadLen); err != nil {
		return nil, wrapConnectionError(err, "failed to read payload length")
	}

	// Validate payload length.
	if payloadLen > MaxPayloadSize {
		return nil, fmt.Errorf("payload too large: %d bytes (max %d)", payloadLen, MaxPayloadSize)
	}

	// Read payload.
	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, wrapConnectionError(err, "failed to read payload")
		}
	}

	return &BinaryFrame{
		Version:     version,
		MessageType: MessageType(msgType),
		Payload:     payload,
	}, nil
}
