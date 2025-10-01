package gateway

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"
)

// verifyBinaryFrameData checks the written binary frame data matches expectations.
func verifyBinaryFrameData(t *testing.T, data []byte, frame *BinaryFrame) {
	t.Helper()

	if len(data) < HeaderSize {
		t.Errorf("Written data too short: got %d bytes, want at least %d", len(data), HeaderSize)

		return
	}

	// Check magic bytes.
	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != MagicBytes {
		t.Errorf("Invalid magic bytes: got 0x%08X, want 0x%08X", magic, MagicBytes)
	}

	// Check version.
	version := binary.BigEndian.Uint16(data[4:6])
	if version != frame.Version {
		t.Errorf("Invalid version: got %d, want %d", version, frame.Version)
	}

	// Check message type.
	msgType := binary.BigEndian.Uint16(data[6:8])
	if MessageType(msgType) != frame.MessageType {
		t.Errorf("Invalid message type: got %d, want %d", msgType, frame.MessageType)
	}

	// Check reserved padding (bytes 8:12 should be zero).
	reserved := binary.BigEndian.Uint32(data[8:12])
	if reserved != 0 {
		t.Errorf("Reserved padding should be zero: got %d", reserved)
	}

	// Check payload length (now at bytes 12:16).
	payloadLen := binary.BigEndian.Uint32(data[12:16])
	if int(payloadLen) != len(frame.Payload) {
		t.Errorf("Invalid payload length: got %d, want %d", payloadLen, len(frame.Payload))
	}

	// Check total length.
	expectedLen := HeaderSize + len(frame.Payload)
	if len(data) != expectedLen {
		t.Errorf("Invalid total length: got %d, want %d", len(data), expectedLen)
	}
}

func TestBinaryFrame_WriteTo(t *testing.T) {
	tests := []struct {
		name    string
		frame   *BinaryFrame
		wantErr bool
	}{
		{
			name: "valid frame with payload",
			frame: &BinaryFrame{
				Version:     0x0001,
				MessageType: MessageTypeRequest,
				Payload:     []byte(`{"method":"test","id":1}`),
			},
			wantErr: false,
		},
		{
			name: "valid frame without payload",
			frame: &BinaryFrame{
				Version:     0x0001,
				MessageType: MessageTypeHealthCheck,
				Payload:     []byte{},
			},
			wantErr: false,
		},
		{
			name: "frame with large payload",
			frame: &BinaryFrame{
				Version:     0x0001,
				MessageType: MessageTypeResponse,
				Payload:     bytes.Repeat([]byte("a"), 65536),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			err := tt.frame.Write(&buf)
			if (err != nil) != tt.wantErr {
				t.Errorf("WriteTo() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr {
				verifyBinaryFrameData(t, buf.Bytes(), tt.frame)
			}
		})
	}
}

// validateReadBinaryFrameResult validates the result of ReadBinaryFrame test.
func validateReadBinaryFrameResult(t *testing.T, got *BinaryFrame, want *BinaryFrame) {
	t.Helper()

	if got.Version != want.Version {
		t.Errorf("Version = %v, want %v", got.Version, want.Version)
	}

	if got.MessageType != want.MessageType {
		t.Errorf("MessageType = %v, want %v", got.MessageType, want.MessageType)
	}

	if !bytes.Equal(got.Payload, want.Payload) {
		t.Errorf("Payload = %v, want %v", got.Payload, want.Payload)
	}
}

func TestReadBinaryFrame(t *testing.T) {
	tests := createReadBinaryFrameTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader(tt.data)
			got, err := ReadBinaryFrame(reader)

			if (err != nil) != tt.wantErr {
				t.Errorf("ReadBinaryFrame() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if tt.wantErr && tt.errString != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errString) {
					t.Errorf("ReadBinaryFrame() error = %v, want error containing %q", err, tt.errString)
				}

				return
			}

			if !tt.wantErr {
				validateReadBinaryFrameResult(t, got, tt.want)
			}
		})
	}
}

func createReadBinaryFrameTests() []struct {
	name      string
	data      []byte
	want      *BinaryFrame
	wantErr   bool
	errString string
} {
	return []struct {
		name      string
		data      []byte
		want      *BinaryFrame
		wantErr   bool
		errString string
	}{
		{
			name: "valid frame",
			data: createValidFrameData(),
			want: &BinaryFrame{
				Version:     0x0001,
				MessageType: MessageTypeRequest,
				Payload:     []byte(`{"test":"data"}`),
			},
			wantErr: false,
		},
		{
			name:      "invalid magic bytes",
			data:      createInvalidMagicBytesData(),
			wantErr:   true,
			errString: "invalid magic bytes",
		},
		{
			name:      "payload too large",
			data:      createPayloadTooLargeData(),
			wantErr:   true,
			errString: "payload too large",
		},
		{
			name:      "incomplete header",
			data:      []byte{0x4D, 0x43, 0x50}, // Only 3 bytes
			wantErr:   true,
			errString: "failed to read",
		},
		{
			name:      "incomplete payload",
			data:      createIncompletePayloadData(),
			wantErr:   true,
			errString: "failed to read payload",
		},
	}
}

func createValidFrameData() []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, uint32(MagicBytes))
	_ = binary.Write(buf, binary.BigEndian, uint16(0x0001))
	_ = binary.Write(buf, binary.BigEndian, uint16(MessageTypeRequest))
	_ = binary.Write(buf, binary.BigEndian, uint32(0)) // Reserved padding

	payload := []byte(`{"test":"data"}`)
	if len(payload) <= math.MaxUint32 {
		_ = binary.Write(buf, binary.BigEndian, uint32(len(payload))) // #nosec G115 - bounds checked above
	}

	_, _ = buf.Write(payload)

	return buf.Bytes()
}

func createInvalidMagicBytesData() []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, uint32(0x12345678))
	_ = binary.Write(buf, binary.BigEndian, uint16(0x0001))
	_ = binary.Write(buf, binary.BigEndian, uint16(MessageTypeRequest))
	_ = binary.Write(buf, binary.BigEndian, uint32(0)) // Reserved padding
	_ = binary.Write(buf, binary.BigEndian, uint32(0)) // Payload length

	return buf.Bytes()
}

func createPayloadTooLargeData() []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, uint32(MagicBytes))
	_ = binary.Write(buf, binary.BigEndian, uint16(0x0001))
	_ = binary.Write(buf, binary.BigEndian, uint16(MessageTypeRequest))
	_ = binary.Write(buf, binary.BigEndian, uint32(0)) // Reserved padding
	_ = binary.Write(buf, binary.BigEndian, uint32(MaxPayloadSize+1))

	return buf.Bytes()
}

func createIncompletePayloadData() []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, uint32(MagicBytes))
	_ = binary.Write(buf, binary.BigEndian, uint16(0x0001))
	_ = binary.Write(buf, binary.BigEndian, uint16(MessageTypeRequest))
	_ = binary.Write(buf, binary.BigEndian, uint32(0))   // Reserved padding
	_ = binary.Write(buf, binary.BigEndian, uint32(100)) // Claims 100 bytes
	buf.WriteString("short")                             // But only provides 5

	return buf.Bytes()
}

func TestBinaryFrame_RoundTrip(t *testing.T) {
	frames := []*BinaryFrame{
		{
			Version:     0x0001,
			MessageType: MessageTypeRequest,
			Payload:     []byte(`{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}`),
		},
		{
			Version:     0x0001,
			MessageType: MessageTypeResponse,
			Payload:     []byte(`{"jsonrpc":"2.0","result":{"protocolVersion":"1.0"},"id":1}`),
		},
		{
			Version:     0x0001,
			MessageType: MessageTypeHealthCheck,
			Payload:     []byte{},
		},
		{
			Version:     0x0002,
			MessageType: MessageTypeError,
			Payload:     []byte(`{"error":"test error"}`),
		},
	}

	for i, original := range frames {
		t.Run(fmt.Sprintf("frame_%d", i), func(t *testing.T) {
			// Write to buffer.
			var buf bytes.Buffer
			if err := original.Write(&buf); err != nil {
				t.Fatalf("WriteTo() error = %v", err)
			}

			// Read back.
			decoded, err := ReadBinaryFrame(&buf)
			if err != nil {
				t.Fatalf("ReadBinaryFrame() error = %v", err)
			}

			// Compare.
			if decoded.Version != original.Version {
				t.Errorf("Version = %v, want %v", decoded.Version, original.Version)
			}

			if decoded.MessageType != original.MessageType {
				t.Errorf("MessageType = %v, want %v", decoded.MessageType, original.MessageType)
			}

			if !bytes.Equal(decoded.Payload, original.Payload) {
				t.Errorf("Payload = %v, want %v", decoded.Payload, original.Payload)
			}
		})
	}
}

func TestMessageType_String(t *testing.T) {
	tests := []struct {
		msgType MessageType
		want    string
	}{
		{MessageTypeRequest, "MessageType(1)"},
		{MessageTypeResponse, "MessageType(2)"},
		{MessageTypeControl, "MessageType(3)"},
		{MessageTypeHealthCheck, "MessageType(4)"},
		{MessageTypeError, "MessageType(5)"},
		{MessageType(999), "MessageType(999)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := fmt.Sprintf("%v", tt.msgType)
			if got != tt.want {
				t.Errorf("MessageType.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkBinaryFrame_WriteTo(b *testing.B) {
	frame := &BinaryFrame{
		Version:     0x0001,
		MessageType: MessageTypeRequest,
		Payload:     []byte(`{"jsonrpc":"2.0","method":"tools/call","params":{"tool":"test","args":{}},"id":123}`),
	}

	var buf bytes.Buffer

	buf.Grow(1024)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		_ = frame.Write(&buf)
	}
}

func BenchmarkReadBinaryFrame(b *testing.B) {
	// Prepare test data.
	frame := &BinaryFrame{
		Version:     0x0001,
		MessageType: MessageTypeRequest,
		Payload:     []byte(`{"jsonrpc":"2.0","method":"tools/call","params":{"tool":"test","args":{}},"id":123}`),
	}

	var buf bytes.Buffer

	_ = frame.Write(&buf)
	data := buf.Bytes()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(data)
		_, _ = ReadBinaryFrame(reader)
	}
}
