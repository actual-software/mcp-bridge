
package wire

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIterations = 100
	testTimeout    = 50
)

func TestFrame_Encode(t *testing.T) {
	tests := createFrameEncodeTests()
	runFrameEncodeTests(t, tests)
}

func createFrameEncodeTests() []struct {
	name    string
	frame   Frame
	wantErr bool
} {
	return []struct {
		name    string
		frame   Frame
		wantErr bool
	}{
		{
			name: "Simple request frame",
			frame: Frame{
				Version:     CurrentVersion,
				MessageType: MessageTypeRequest,
				Payload:     []byte(`{"method":"test"}`),
			},
			wantErr: false,
		},
		{
			name: "Empty payload",
			frame: Frame{
				Version:     CurrentVersion,
				MessageType: MessageTypeResponse,
				Payload:     []byte{},
			},
			wantErr: false,
		},
		{
			name: "Large payload",
			frame: Frame{
				Version:     CurrentVersion,
				MessageType: MessageTypeControl,
				Payload:     bytes.Repeat([]byte("x"), 1024),
			},
			wantErr: false,
		},
	}
}

func runFrameEncodeTests(t *testing.T, tests []struct {
	name    string
	frame   Frame
	wantErr bool
}) {
	t.Helper()
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			err := tt.frame.Encode(&buf)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			validateEncodedFrame(t, &buf, &tt.frame)
		})
	}
}

func validateEncodedFrame(t *testing.T, buf *bytes.Buffer, frame *Frame) {
	t.Helper()
	
	// Verify header
	data := buf.Bytes()
	assert.GreaterOrEqual(t, len(data), HeaderSize)

	// Check magic bytes
	magic := binary.BigEndian.Uint32(data[0:4])
	assert.Equal(t, MagicBytes, magic)

	// Check version
	version := binary.BigEndian.Uint16(data[4:6])
	assert.Equal(t, frame.Version, version)

	// Check message type
	msgType := binary.BigEndian.Uint16(data[6:8])
	assert.Equal(t, uint16(frame.MessageType), msgType)

	// Check payload length
	payloadLen := binary.BigEndian.Uint32(data[8:12])
	assert.Equal(t, uint32(len(frame.Payload)), payloadLen)

	// Check payload
	if len(frame.Payload) > 0 {
		assert.Equal(t, frame.Payload, data[HeaderSize:])
	}
}

func TestDecode(t *testing.T) {
	tests := createDecodeTests()
	runDecodeTests(t, tests)
}

func createDecodeTests() []struct {
	name      string
	buildData func() []byte
	wantFrame *Frame
	wantErr   bool
	errorMsg  string
} {
	tests := []struct {
		name      string
		buildData func() []byte
		wantFrame *Frame
		wantErr   bool
		errorMsg  string
	}{}

	tests = append(tests, createValidDecodeTests()...)
	tests = append(tests, createInvalidDecodeTests()...)

	return tests
}

func createValidDecodeTests() []struct {
	name      string
	buildData func() []byte
	wantFrame *Frame
	wantErr   bool
	errorMsg  string
} {
	return []struct {
		name      string
		buildData func() []byte
		wantFrame *Frame
		wantErr   bool
		errorMsg  string
	}{
		{
			name:      "Valid frame",
			buildData: buildValidFrameData,
			wantFrame: &Frame{
				Version:     CurrentVersion,
				MessageType: MessageTypeRequest,
				Payload:     []byte(`{"test":"data"}`),
			},
			wantErr: false,
		},
	}
}

func createInvalidDecodeTests() []struct {
	name      string
	buildData func() []byte
	wantFrame *Frame
	wantErr   bool
	errorMsg  string
} {
	return []struct {
		name      string
		buildData func() []byte
		wantFrame *Frame
		wantErr   bool
		errorMsg  string
	}{
		{
			name:      "Invalid magic bytes",
			buildData: buildInvalidMagicFrameData,
			wantErr:   true,
			errorMsg:  "invalid magic bytes",
		},
		{
			name:      "Payload too large",
			buildData: buildLargePayloadFrameData,
			wantErr:   true,
			errorMsg:  "payload too large",
		},
		{
			name:      "Truncated header",
			buildData: buildTruncatedHeaderFrameData,
			wantErr:   true,
			errorMsg:  "failed to read message type",
		},
		{
			name:      "Truncated payload",
			buildData: buildTruncatedPayloadFrameData,
			wantErr:   true,
			errorMsg:  "failed to read payload",
		},
	}
}

func buildValidFrameData() []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, MagicBytes)
	_ = binary.Write(&buf, binary.BigEndian, CurrentVersion)
	_ = binary.Write(&buf, binary.BigEndian, uint16(MessageTypeRequest))
	payload := []byte(`{"test":"data"}`)
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(payload)))
	_, _ = buf.Write(payload)
	return buf.Bytes()
}

func buildInvalidMagicFrameData() []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint32(0xDEADBEEF))
	_ = binary.Write(&buf, binary.BigEndian, CurrentVersion)
	_ = binary.Write(&buf, binary.BigEndian, uint16(MessageTypeRequest))
	_ = binary.Write(&buf, binary.BigEndian, uint32(0))
	return buf.Bytes()
}

func buildLargePayloadFrameData() []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, MagicBytes)
	_ = binary.Write(&buf, binary.BigEndian, CurrentVersion)
	_ = binary.Write(&buf, binary.BigEndian, uint16(MessageTypeRequest))
	_ = binary.Write(&buf, binary.BigEndian, uint32(11*1024*1024))
	return buf.Bytes()
}

func buildTruncatedHeaderFrameData() []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, MagicBytes)
	_ = binary.Write(&buf, binary.BigEndian, CurrentVersion)
	// Missing message type and length
	return buf.Bytes()
}

func buildTruncatedPayloadFrameData() []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, MagicBytes)
	_ = binary.Write(&buf, binary.BigEndian, CurrentVersion)
	_ = binary.Write(&buf, binary.BigEndian, uint16(MessageTypeRequest))
	_ = binary.Write(&buf, binary.BigEndian, uint32(testIterations))
	_, _ = buf.WriteString("short")
	return buf.Bytes()
}

func runDecodeTests(t *testing.T, tests []struct {
	name      string
	buildData func() []byte
	wantFrame *Frame
	wantErr   bool
	errorMsg  string
}) {
	t.Helper()
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.buildData()
			frame, err := Decode(bytes.NewReader(data))

			if tt.wantErr {
				assert.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantFrame.Version, frame.Version)
			assert.Equal(t, tt.wantFrame.MessageType, frame.MessageType)
			assert.Equal(t, tt.wantFrame.Payload, frame.Payload)
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	frames := []Frame{
		{
			Version:     CurrentVersion,
			MessageType: MessageTypeRequest,
			Payload:     []byte(`{"jsonrpc":"2.0","method":"test","id":1}`),
		},
		{
			Version:     CurrentVersion,
			MessageType: MessageTypeResponse,
			Payload:     []byte(`{"jsonrpc":"2.0","result":"ok","id":1}`),
		},
		{
			Version:     CurrentVersion,
			MessageType: MessageTypeHealthCheck,
			Payload:     []byte{},
		},
		{
			Version:     CurrentVersion,
			MessageType: MessageTypeError,
			Payload:     []byte(`{"error":"test error"}`),
		},
	}

	for i, original := range frames {
		t.Run(fmt.Sprintf("Frame_%d", i), func(t *testing.T) {
			// Encode
			var buf bytes.Buffer

			err := original.Encode(&buf)
			require.NoError(t, err)

			// Decode
			decoded, err := Decode(&buf)
			require.NoError(t, err)

			// Compare
			assert.Equal(t, original.Version, decoded.Version)
			assert.Equal(t, original.MessageType, decoded.MessageType)
			assert.Equal(t, original.Payload, decoded.Payload)
		})
	}
}

func TestEncodeMessage(t *testing.T) {
	payload := []byte(`{"test":"data"}`)
	data, err := EncodeMessage(MessageTypeRequest, payload)
	require.NoError(t, err)

	// Verify we can decode it
	frame, err := DecodeMessage(data)
	require.NoError(t, err)

	assert.Equal(t, CurrentVersion, frame.Version)
	assert.Equal(t, MessageTypeRequest, frame.MessageType)
	assert.Equal(t, payload, frame.Payload)
}

func TestDecodeMessage(t *testing.T) {
	// Create a valid frame
	var buf bytes.Buffer

	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeResponse,
		Payload:     []byte(`{"result":"success"}`),
	}
	err := frame.Encode(&buf)
	require.NoError(t, err)

	// Decode from bytes
	decoded, err := DecodeMessage(buf.Bytes())
	require.NoError(t, err)

	assert.Equal(t, frame.Version, decoded.Version)
	assert.Equal(t, frame.MessageType, decoded.MessageType)
	assert.Equal(t, frame.Payload, decoded.Payload)
}

func TestDecodeUnsupportedVersion(t *testing.T) {
	// Create a frame with unsupported version
	frame := &Frame{
		Version:     0x0002, // Unsupported version
		MessageType: MessageTypeError,
		Payload:     []byte(`{"error":"test error"}`),
	}

	// Encode it
	var buf bytes.Buffer

	err := frame.Encode(&buf)
	require.NoError(t, err)

	// Try to decode - should fail with unsupported version error
	_, err = Decode(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported protocol version")
}

func TestMessageType_String(t *testing.T) {
	// Test that message types have expected values
	assert.Equal(t, MessageTypeRequest, MessageType(0x0001))
	assert.Equal(t, MessageTypeResponse, MessageType(0x0002))
	assert.Equal(t, MessageTypeControl, MessageType(0x0003))
	assert.Equal(t, MessageTypeHealthCheck, MessageType(0x0004))
	assert.Equal(t, MessageTypeError, MessageType(0x0005))
}

func BenchmarkEncode(b *testing.B) {
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeRequest,
		Payload:     []byte(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"test.tool","arguments":{}},"id":"550e8400-e29b-41d4-a716-446655440000"}`),
	}

	var buf bytes.Buffer

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		_ = frame.Encode(&buf) 
	}
}

func BenchmarkDecode(b *testing.B) {
	// Prepare encoded data
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeRequest,
		Payload:     []byte(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"test.tool","arguments":{}},"id":"550e8400-e29b-41d4-a716-446655440000"}`),
	}

	var buf bytes.Buffer

	_ = frame.Encode(&buf) 
	data := buf.Bytes()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = Decode(bytes.NewReader(data)) 
	}
}

// TestConcurrentEncodeDecode tests thread safety.
func TestConcurrentEncodeDecode(t *testing.T) {
	frame := &Frame{
		Version:     CurrentVersion,
		MessageType: MessageTypeRequest,
		Payload:     []byte(`{"test":"concurrent"}`),
	}

	// Run multiple goroutines encoding and decoding
	errChan := make(chan error, 20)

	for i := 0; i < 10; i++ {
		go func() {
			var buf bytes.Buffer
			if err := frame.Encode(&buf); err != nil {
				errChan <- err

				return
			}

			if _, err := Decode(&buf); err != nil {
				errChan <- err

				return
			}

			errChan <- nil
		}()
	}

	// Collect results
	for i := 0; i < 10; i++ {
		err := <-errChan
		assert.NoError(t, err)
	}
}
