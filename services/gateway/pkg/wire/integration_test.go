package wire

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// TestBinaryWireProtocol tests the binary wire protocol implementation.
func TestBinaryWireProtocol(t *testing.T) {
	req := createTestRequest()
	client, server := createConnectedTransports(t)
	done := make(chan bool)

	runServerSide(t, server, req, done)
	runClientSide(t, client, req)
	waitForCompletion(t, done)
}

func createTestRequest() *mcp.Request {
	return &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-123",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "test.tool",
			"arguments": map[string]interface{}{
				"key": "value",
			},
		},
	}
}

func runServerSide(t *testing.T, server *Transport, expectedReq *mcp.Request, done chan bool) {
	t.Helper()

	go func() {
		// Receive request on server
		msgType, msg, err := server.ReceiveMessage()
		assert.NoError(t, err)
		assert.Equal(t, MessageTypeRequest, msgType)

		receivedReq, ok := msg.(*mcp.Request)
		if !ok {
			t.Errorf("Expected *mcp.Request, got %T", msg)

			return
		}

		assert.Equal(t, expectedReq.ID, receivedReq.ID)
		assert.Equal(t, expectedReq.Method, receivedReq.Method)
		assert.Equal(t, expectedReq.JSONRPC, receivedReq.JSONRPC)

		// Send response from server
		resp := &mcp.Response{
			JSONRPC: "2.0",
			ID:      receivedReq.ID,
			Result: map[string]interface{}{
				"status": "success",
				"data":   "test data",
			},
		}

		err = server.SendResponse(resp)
		assert.NoError(t, err)

		done <- true
	}()
}

func runClientSide(t *testing.T, client *Transport, req *mcp.Request) {
	t.Helper()

	// Send request
	err := client.SendRequest(req)
	require.NoError(t, err)

	// Receive response on client
	msgType, msg, err := client.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeResponse, msgType)

	receivedResp, ok := msg.(*mcp.Response)
	require.True(t, ok, "Expected *mcp.Response, got %T", msg)

	assert.Equal(t, req.ID, receivedResp.ID)
	assert.NotNil(t, receivedResp.Result)
}

func waitForCompletion(t *testing.T, done chan bool) {
	t.Helper()

	// Wait for server to complete
	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Test timeout")
	}
}

// TestBinaryProtocolMagicBytes tests that the protocol correctly uses magic bytes.
func TestBinaryProtocolMagicBytes(t *testing.T) {
	// Encode message
	data, err := EncodeMessage(MessageTypeRequest, []byte(`{"jsonrpc":"2.0","id":"magic-test","method":"test"}`))
	require.NoError(t, err)

	// Verify magic bytes
	assert.GreaterOrEqual(t, len(data), 4)
	magicBytes := data[0:4]
	assert.Equal(t, []byte{0x4D, 0x43, 0x50, 0x42}, magicBytes) // "MCPB"

	// Verify version
	assert.GreaterOrEqual(t, len(data), 6)
	version := (uint16(data[4]) << 8) | uint16(data[5])
	assert.Equal(t, uint16(0x0001), version)

	// Verify message type
	assert.GreaterOrEqual(t, len(data), 8)
	msgType := (uint16(data[6]) << 8) | uint16(data[7])
	assert.Equal(t, uint16(MessageTypeRequest), msgType)
}
