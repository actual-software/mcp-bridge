package wire

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Constants for the wire protocol.
const (
	// MagicBytes identifies MCP binary protocol messages.
	MagicBytes uint32 = 0x4D435042 // "MCPB"

	// CurrentVersion is the current protocol version.
	CurrentVersion uint16 = 0x0001

	// MinVersion is the minimum supported protocol version.
	MinVersion uint16 = 0x0001

	// MaxVersion is the maximum supported protocol version.
	MaxVersion uint16 = 0x0001

	// HeaderSize is the size of the fixed header.
	HeaderSize = 12 // 4 (magic) + 2 (version) + 2 (type) + 4 (length)
)

// MessageType represents the type of message.
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

// Frame represents a wire protocol frame.
type Frame struct {
	Version     uint16
	MessageType MessageType
	Payload     []byte
}

// Encode encodes a frame to the writer.
func (f *Frame) Encode(w io.Writer) error {
	// Write magic bytes
	if err := binary.Write(w, binary.BigEndian, MagicBytes); err != nil {
		return fmt.Errorf("failed to write magic bytes: %w", err)
	}

	// Write version
	if err := binary.Write(w, binary.BigEndian, f.Version); err != nil {
		return fmt.Errorf("failed to write version: %w", err)
	}

	// Write message type
	if err := binary.Write(w, binary.BigEndian, f.MessageType); err != nil {
		return fmt.Errorf("failed to write message type: %w", err)
	}

	// Write payload length
	payloadLen := uint32(len(f.Payload)) // #nosec G115 - payload length is controlled and bounded
	if err := binary.Write(w, binary.BigEndian, payloadLen); err != nil {
		return fmt.Errorf("failed to write payload length: %w", err)
	}

	// Write payload
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return fmt.Errorf("failed to write payload: %w", err)
		}
	}

	return nil
}

// Decode decodes a frame from the reader.
func Decode(r io.Reader) (*Frame, error) {
	// Read and validate header
	header, err := readFrameHeader(r)
	if err != nil {
		return nil, err
	}

	// Read payload
	payload, err := readFramePayload(r, header.PayloadLen)
	if err != nil {
		return nil, err
	}

	return &Frame{
		Version:     header.Version,
		MessageType: MessageType(header.MessageType),
		Payload:     payload,
	}, nil
}

// frameHeader holds the decoded frame header.
type frameHeader struct {
	Version     uint16
	MessageType uint16
	PayloadLen  uint32
}

// readFrameHeader reads and validates the frame header.
func readFrameHeader(r io.Reader) (*frameHeader, error) {
	// Validate magic bytes
	if err := validateMagicBytes(r); err != nil {
		return nil, err
	}

	// Read version
	version, err := readVersion(r)
	if err != nil {
		return nil, err
	}

	// Read message type
	msgType, err := readMessageType(r)
	if err != nil {
		return nil, err
	}

	// Read and validate payload length
	payloadLen, err := readPayloadLength(r)
	if err != nil {
		return nil, err
	}

	return &frameHeader{
		Version:     version,
		MessageType: msgType,
		PayloadLen:  payloadLen,
	}, nil
}

// validateMagicBytes reads and validates magic bytes.
func validateMagicBytes(r io.Reader) error {
	var magic uint32
	if err := binary.Read(r, binary.BigEndian, &magic); err != nil {
		return fmt.Errorf("failed to read magic bytes: %w", err)
	}

	if magic != MagicBytes {
		return fmt.Errorf("invalid magic bytes: 0x%08X", magic)
	}

	return nil
}

// readVersion reads and validates the protocol version.
func readVersion(r io.Reader) (uint16, error) {
	var version uint16
	if err := binary.Read(r, binary.BigEndian, &version); err != nil {
		return 0, fmt.Errorf("failed to read version: %w", err)
	}

	if version < MinVersion || version > MaxVersion {
		return 0, fmt.Errorf(
			"unsupported protocol version: 0x%04X (supported: 0x%04X-0x%04X)",
			version, MinVersion, MaxVersion,
		)
	}

	return version, nil
}

// readMessageType reads the message type.
func readMessageType(r io.Reader) (uint16, error) {
	var msgType uint16
	if err := binary.Read(r, binary.BigEndian, &msgType); err != nil {
		return 0, fmt.Errorf("failed to read message type: %w", err)
	}

	return msgType, nil
}

// readPayloadLength reads and validates the payload length.
func readPayloadLength(r io.Reader) (uint32, error) {
	var payloadLen uint32
	if err := binary.Read(r, binary.BigEndian, &payloadLen); err != nil {
		return 0, fmt.Errorf("failed to read payload length: %w", err)
	}
	// Validate payload length (max 10MB)
	if payloadLen > 10*1024*1024 {
		return 0, fmt.Errorf("payload too large: %d bytes", payloadLen)
	}

	return payloadLen, nil
}

// readFramePayload reads the frame payload.
func readFramePayload(r io.Reader, payloadLen uint32) ([]byte, error) {
	if payloadLen == 0 {
		return []byte{}, nil
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	return payload, nil
}

// EncodeMessage encodes a message with the given type.
func EncodeMessage(msgType MessageType, payload []byte) ([]byte, error) {
	var buf bytes.Buffer

	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: msgType,
		Payload:     payload,
	}

	if err := frame.Encode(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// DecodeMessage is a convenience function to decode from bytes.
func DecodeMessage(data []byte) (*Frame, error) {
	return Decode(bytes.NewReader(data))
}

// VersionNegotiationPayload represents the version negotiation message.
type VersionNegotiationPayload struct {
	MinVersion uint16   `json:"min_version"`
	MaxVersion uint16   `json:"max_version"`
	Preferred  uint16   `json:"preferred_version"`
	Supported  []uint16 `json:"supported_versions"`
}

// VersionAckPayload represents the version acknowledgment message.
type VersionAckPayload struct {
	AgreedVersion uint16 `json:"agreed_version"`
}

// NegotiateVersion selects the best version from client and server ranges.
func NegotiateVersion(clientMin, clientMax, serverMin, serverMax uint16) (uint16, error) {
	// Find the highest version supported by both
	minSupported := clientMin
	if serverMin > minSupported {
		minSupported = serverMin
	}

	maxSupported := clientMax
	if serverMax < maxSupported {
		maxSupported = serverMax
	}

	// Check if there's any overlap
	if maxSupported < minSupported {
		return 0, fmt.Errorf("no compatible protocol version: client supports %d-%d, server supports %d-%d",
			clientMin, clientMax, serverMin, serverMax)
	}

	// Return the highest compatible version
	return maxSupported, nil
}

// IsVersionSupported checks if a version is within the supported range.
func IsVersionSupported(version uint16) bool {
	return version >= MinVersion && version <= MaxVersion
}
