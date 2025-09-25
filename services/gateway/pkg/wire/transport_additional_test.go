
package wire

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// testingT provides common interface for *testing.T and *testing.B.
type testingT interface {
	Helper()
	Cleanup(func())
}

// createConnectedTransportsForBench creates connected transports for benchmarks.
func createConnectedTransportsForBench(tb testingT) (clientTransport, serverTransport *Transport) {
	tb.Helper()
	// Create a pipe for testing
	client, server := net.Pipe()

	clientTransport = NewTransport(client)
	serverTransport = NewTransport(server)

	tb.Cleanup(func() {
		_ = clientTransport.Close() 
		_ = serverTransport.Close() 
	})

	return
}

func TestTransport_SendControl(t *testing.T) {
	client, server := createConnectedTransports(t)

	controlMsg := map[string]interface{}{
		"type":    "ping",
		"data":    "test-control-data",
		"timeout": 30,
	}

	// Send control message from client
	errChan := make(chan error, 1)

	go func() {
		errChan <- client.SendControl(controlMsg)
	}()

	// Receive on server
	msgType, msg, err := server.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeControl, msgType)

	receivedControl, ok := msg.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ping", receivedControl["type"])
	assert.Equal(t, "test-control-data", receivedControl["data"])

	// Check send completed
	select {
	case err := <-errChan:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Send timeout")
	}
}

func TestTransport_SendVersionNegotiation(t *testing.T) {
	client, server := createConnectedTransports(t)

	payload := &VersionNegotiationPayload{
		MinVersion: MinVersion,
		MaxVersion: MaxVersion,
		Preferred:  CurrentVersion,
		Supported:  []uint16{1, 2, 3},
	}

	// Send version negotiation from client
	errChan := make(chan error, 1)

	go func() {
		errChan <- client.SendVersionNegotiation(payload)
	}()

	// Receive on server
	msgType, msg, err := server.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeVersionNegotiation, msgType)

	receivedPayload, ok := msg.(*VersionNegotiationPayload)
	require.True(t, ok)
	assert.Equal(t, payload.MinVersion, receivedPayload.MinVersion)
	assert.Equal(t, payload.MaxVersion, receivedPayload.MaxVersion)
	assert.Equal(t, payload.Preferred, receivedPayload.Preferred)
	assert.Equal(t, payload.Supported, receivedPayload.Supported)

	// Check send completed
	select {
	case err := <-errChan:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Send timeout")
	}
}

func TestTransport_SendVersionAck(t *testing.T) {
	client, server := createConnectedTransports(t)

	payload := &VersionAckPayload{
		AgreedVersion: CurrentVersion,
	}

	// Send version ack from server
	errChan := make(chan error, 1)

	go func() {
		errChan <- server.SendVersionAck(payload)
	}()

	// Receive on client
	msgType, msg, err := client.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeVersionAck, msgType)

	receivedPayload, ok := msg.(*VersionAckPayload)
	require.True(t, ok)
	assert.Equal(t, payload.AgreedVersion, receivedPayload.AgreedVersion)

	// Check send completed
	select {
	case err := <-errChan:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Send timeout")
	}
}

func TestTransport_EncodeJSON(t *testing.T) {
	client, _ := createConnectedTransports(t)
	tests := createEncodeJSONTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := client.EncodeJSON(tt.data)
			verifyEncodeJSONResult(t, data, err, tt.wantErr, tt.data)
		})
	}
}

func createEncodeJSONTests() []struct {
	name    string
	data    interface{}
	wantErr bool
} {
	return []struct {
		name    string
		data    interface{}
		wantErr bool
	}{
		{
			name: "Simple object",
			data: map[string]interface{}{
				"test":  "value",
				"count": 42,
			},
			wantErr: false,
		},
		{
			name: "MCP Request",
			data: &mcp.Request{
				JSONRPC: "2.0",
				ID:      "test-123",
				Method:  "tools/call",
				Params:  map[string]interface{}{"name": "test.tool"},
			},
			wantErr: false,
		},
		{
			name: "MCP Response",
			data: &mcp.Response{
				JSONRPC: "2.0",
				ID:      "resp-456",
				Result:  "success",
			},
			wantErr: false,
		},
		{
			name:    "Invalid data - channel",
			data:    make(chan int),
			wantErr: true,
		},
		{
			name:    "Nil data",
			data:    nil,
			wantErr: false,
		},
	}
}

func verifyEncodeJSONResult(t *testing.T, data []byte, err error, wantErr bool, originalData interface{}) {
	t.Helper()
	if wantErr {
		assert.Error(t, err)
		assert.Nil(t, data)
	} else {
		assert.NoError(t, err)
		assert.NotNil(t, data)

		// Verify it's valid JSON
		if originalData != nil {
			var decoded interface{}
			err = json.Unmarshal(data, &decoded)
			assert.NoError(t, err)
		}
	}
}

func TestTransport_SetDeadline(t *testing.T) {
	client, _ := createConnectedTransports(t)

	// Set deadline
	deadline := time.Now().Add(time.Hour)
	err := client.SetDeadline(deadline)
	assert.NoError(t, err)

	// Clear deadline
	err = client.SetDeadline(time.Time{})
	assert.NoError(t, err)
}

func TestTransport_SetWriteDeadline(t *testing.T) {
	client, _ := createConnectedTransports(t)

	// Set write deadline
	deadline := time.Now().Add(time.Hour)
	err := client.SetWriteDeadline(deadline)
	assert.NoError(t, err)

	// Clear write deadline
	err = client.SetWriteDeadline(time.Time{})
	assert.NoError(t, err)
}

func TestTransport_GetWriter(t *testing.T) {
	client, _ := createConnectedTransports(t)

	writer := client.GetWriter()
	assert.NotNil(t, writer)

	// Verify we can write to it
	data := []byte("test data")
	n, err := writer.Write(data)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
}

func TestTransport_Flush(t *testing.T) {
	client, _ := createConnectedTransports(t)

	// This should not error even if there's nothing to flush
	err := client.Flush()
	assert.NoError(t, err)
}

func TestTransport_ReceiveMessage_EdgeCases(t *testing.T) {
	t.Run("Receive unknown message type", testReceiveUnknownMessageType)
	t.Run("Receive malformed JSON payload", testReceiveMalformedJSON)
	t.Run("Receive version negotiation", testReceiveVersionNegotiation)
	t.Run("Receive version ack", testReceiveVersionAck)
	t.Run("Receive control message", testReceiveControlMessage)
}

func testReceiveUnknownMessageType(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	transport := NewTransport(client)

	// Send a frame with unknown message type
	go func() {
		frame := &Frame{
			Version:     CurrentVersion,
			MessageType: MessageType(0x9999), // Unknown type
			Payload:     []byte(`{"test": "data"}`),
		}
		_ = frame.Encode(server)
		_ = server.Close()
	}()

	msgType, msg, err := transport.ReceiveMessage()
	// Unknown message types should cause an error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown message type")
	assert.Equal(t, MessageType(0x9999), msgType)
	assert.Nil(t, msg)
}

func testReceiveMalformedJSON(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	transport := NewTransport(client)

	// Send a frame with malformed JSON
	go func() {
		frame := &Frame{
			Version:     CurrentVersion,
			MessageType: MessageTypeRequest,
			Payload:     []byte(`{"incomplete": json`), // Malformed JSON
		}
		_ = frame.Encode(server)
		_ = server.Close()
	}()

	msgType, msg, err := transport.ReceiveMessage()
	assert.Error(t, err)
	assert.Equal(t, MessageTypeRequest, msgType)
	assert.Nil(t, msg)
}

func testReceiveVersionNegotiation(t *testing.T) {
	client, server := createConnectedTransports(t)

	payload := &VersionNegotiationPayload{
		MinVersion: 1,
		MaxVersion: 3,
		Preferred:  2,
		Supported:  []uint16{1, 2, 3},
	}

	go func() {
		_ = server.SendVersionNegotiation(payload)
	}()

	msgType, msg, err := client.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeVersionNegotiation, msgType)

	receivedPayload, ok := msg.(*VersionNegotiationPayload)
	require.True(t, ok)
	assert.Equal(t, payload.MinVersion, receivedPayload.MinVersion)
}

func testReceiveVersionAck(t *testing.T) {
	client, server := createConnectedTransports(t)

	payload := &VersionAckPayload{
		AgreedVersion: 2,
	}

	go func() {
		_ = server.SendVersionAck(payload)
	}()

	msgType, msg, err := client.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeVersionAck, msgType)

	receivedPayload, ok := msg.(*VersionAckPayload)
	require.True(t, ok)
	assert.Equal(t, payload.AgreedVersion, receivedPayload.AgreedVersion)
}

func testReceiveControlMessage(t *testing.T) {
	client, server := createConnectedTransports(t)

	controlData := map[string]interface{}{
		"action": "shutdown",
		"reason": "maintenance",
	}

	go func() {
		_ = server.SendControl(controlData)
	}()

	msgType, msg, err := client.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeControl, msgType)

	receivedData, ok := msg.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "shutdown", receivedData["action"])
	assert.Equal(t, "maintenance", receivedData["reason"])
}

func TestTransport_SendReceive_ErrorConditions(t *testing.T) {
	t.Run("Send request with encoding error", func(t *testing.T) {
		client, _ := createConnectedTransports(t)

		// Request with channel (cannot be JSON encoded)
		req := &mcp.Request{
			JSONRPC: "2.0",
			ID:      "error-test",
			Method:  "test",
			Params:  make(chan int), // This will cause JSON encoding to fail
		}

		err := client.SendRequest(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "json: unsupported type")
	})

	t.Run("Send response with encoding error", func(t *testing.T) {
		client, _ := createConnectedTransports(t)

		// Response with channel (cannot be JSON encoded)
		resp := &mcp.Response{
			JSONRPC: "2.0",
			ID:      "error-test",
			Result:  make(chan int), // This will cause JSON encoding to fail
		}

		err := client.SendResponse(resp)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "json: unsupported type")
	})

	t.Run("Send control with encoding error", func(t *testing.T) {
		client, _ := createConnectedTransports(t)

		// Control data with channel (cannot be JSON encoded)
		controlData := map[string]interface{}{
			"channel": make(chan int), // This will cause JSON encoding to fail
		}

		err := client.SendControl(controlData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "json: unsupported type")
	})
}

// Test concurrent access to transport methods.
func TestTransport_ConcurrentAccess(t *testing.T) {
	client, server := createConnectedTransports(t)

	// Test concurrent calls to various transport methods
	errChan := make(chan error, 10)

	// Concurrent deadline operations
	go func() {
		for i := 0; i < 5; i++ {
			deadline := time.Now().Add(time.Duration(i) * time.Second)
			errChan <- client.SetDeadline(deadline)
		}
	}()

	go func() {
		for i := 0; i < 5; i++ {
			deadline := time.Now().Add(time.Duration(i) * time.Second)
			errChan <- client.SetWriteDeadline(deadline)
		}
	}()

	// Concurrent address operations
	go func() {
		for i := 0; i < 5; i++ {
			_ = client.LocalAddr()
			_ = client.RemoteAddr()
		}

		errChan <- nil
	}()

	// Concurrent flush operations (without writes to avoid deadlocks)
	go func() {
		for i := 0; i < 5; i++ {
			err := client.Flush()
			if err != nil {
				errChan <- err

				return
			}
		}

		errChan <- nil
	}()

	// Wait for all operations to complete
	for i := 0; i < 4; i++ {
		select {
		case err := <-errChan:
			if err != nil {
				t.Errorf("Concurrent operation failed: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}

	// Close connections
	assert.NoError(t, server.Close())
	assert.NoError(t, client.Close())
}

// Benchmark the new functions.
func BenchmarkTransport_EncodeJSON(b *testing.B) {
	client, _ := createConnectedTransportsForBench(b)

	data := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "bench-test",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "test.tool",
			"args": map[string]interface{}{
				"input": "benchmark data",
			},
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = client.EncodeJSON(data) 
	}
}

func BenchmarkTransport_SendControl(b *testing.B) {
	client, server := createConnectedTransportsForBench(b)

	controlData := map[string]interface{}{
		"type": "benchmark",
		"data": "test control message",
	}

	// Start goroutine to receive messages
	go func() {
		for i := 0; i < b.N; i++ {
			_, _, _ = server.ReceiveMessage() 
		}
	}()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = client.SendControl(controlData) 
	}
}
