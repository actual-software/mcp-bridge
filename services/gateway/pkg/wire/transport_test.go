package wire

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

func createConnectedTransports(t *testing.T) (clientTransport, serverTransport *Transport) {
	t.Helper()
	// Create a pipe for testing
	client, server := net.Pipe()

	clientTransport = NewTransport(client)
	serverTransport = NewTransport(server)

	t.Cleanup(func() {
		_ = clientTransport.Close()
		_ = serverTransport.Close()
	})

	return
}

func TestTransport_SendReceiveRequest(t *testing.T) {
	client, server := createConnectedTransports(t)

	// Test request
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "test-123",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "test.tool",
		},
	}

	// Send from client
	errChan := make(chan error, 1)

	go func() {
		errChan <- client.SendRequest(req)
	}()

	// Receive on server
	msgType, msg, err := server.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeRequest, msgType)

	receivedReq, ok := msg.(*mcp.Request)
	require.True(t, ok)
	assert.Equal(t, req.ID, receivedReq.ID)
	assert.Equal(t, req.Method, receivedReq.Method)

	// Check send completed
	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Send timeout")
	}
}

func TestTransport_SendReceiveResponse(t *testing.T) {
	client, server := createConnectedTransports(t)

	// Test response
	resp := &mcp.Response{
		JSONRPC: "2.0",
		ID:      "test-456",
		Result: map[string]interface{}{
			"status": "ok",
		},
	}

	// Send from server
	errChan := make(chan error, 1)

	go func() {
		errChan <- server.SendResponse(resp)
	}()

	// Receive on client
	msgType, msg, err := client.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeResponse, msgType)

	receivedResp, ok := msg.(*mcp.Response)
	require.True(t, ok)
	assert.Equal(t, resp.ID, receivedResp.ID)
	assert.NotNil(t, receivedResp.Result)

	// Check send completed
	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Send timeout")
	}
}

func TestTransport_SendReceiveError(t *testing.T) {
	client, server := createConnectedTransports(t)

	errorMsg := "test error message"

	// Send error from server
	errChan := make(chan error, 1)

	go func() {
		errChan <- server.SendError(errorMsg)
	}()

	// Receive on client
	msgType, msg, err := client.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeError, msgType)

	receivedError, ok := msg.(string)
	require.True(t, ok)
	assert.Equal(t, errorMsg, receivedError)

	// Check send completed
	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Send timeout")
	}
}

func TestTransport_SendReceiveHealthCheck(t *testing.T) {
	client, server := createConnectedTransports(t)

	// Send health check from client
	errChan := make(chan error, 1)

	go func() {
		errChan <- client.SendHealthCheck()
	}()

	// Receive on server
	msgType, msg, err := server.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeHealthCheck, msgType)
	assert.Nil(t, msg)

	// Check send completed
	select {
	case err := <-errChan:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Send timeout")
	}
}

func TestTransport_Bidirectional(t *testing.T) {
	client, server := createConnectedTransports(t)

	// Client sends request
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      "bidi-test",
		Method:  "test",
	}

	// Server receives and responds
	done := make(chan bool)

	go func() {
		// Receive request
		msgType, msg, err := server.ReceiveMessage()
		assert.NoError(t, err)
		assert.Equal(t, MessageTypeRequest, msgType)

		receivedReq, ok := msg.(*mcp.Request)
		if !ok {
			t.Errorf("Expected *mcp.Request, got %T", msg)

			return
		}

		// Send response
		resp := &mcp.Response{
			JSONRPC: "2.0",
			ID:      receivedReq.ID,
			Result:  "processed",
		}

		err = server.SendResponse(resp)
		assert.NoError(t, err)

		done <- true
	}()

	// Client sends request
	err := client.SendRequest(req)
	require.NoError(t, err)

	// Client receives response
	msgType, msg, err := client.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeResponse, msgType)

	resp, ok := msg.(*mcp.Response)
	if !ok {
		t.Errorf("Expected *mcp.Response, got %T", msg)

		return
	}

	assert.Equal(t, req.ID, resp.ID)
	assert.Equal(t, "processed", resp.Result)

	// Wait for server goroutine
	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Server timeout")
	}
}

func TestTransport_ConcurrentSend(t *testing.T) {
	client, server := createConnectedTransports(t)

	// Multiple goroutines sending concurrently
	numSenders := 10
	errChan := make(chan error, numSenders)

	for i := 0; i < numSenders; i++ {
		go func(id int) {
			req := &mcp.Request{
				JSONRPC: "2.0",
				ID:      fmt.Sprintf("concurrent-%d", id),
				Method:  "test",
			}
			errChan <- client.SendRequest(req)
		}(i)
	}

	// Receive all messages
	received := make(map[string]bool)

	for i := 0; i < numSenders; i++ {
		msgType, msg, err := server.ReceiveMessage()
		require.NoError(t, err)
		assert.Equal(t, MessageTypeRequest, msgType)

		req, ok := msg.(*mcp.Request)
		if !ok {
			t.Errorf("Expected *mcp.Request, got %T", msg)

			continue
		}

		reqID, ok := req.ID.(string)
		if !ok {
			t.Errorf("Expected string ID, got %T", req.ID)

			continue
		}

		received[reqID] = true
	}

	// Check all sends completed
	for i := 0; i < numSenders; i++ {
		err := <-errChan
		require.NoError(t, err)
	}

	// Verify all messages received
	assert.Len(t, received, numSenders)
}

func TestTransport_Deadlines(t *testing.T) {
	client, server := createConnectedTransports(t)

	// Set a read deadline in the past
	err := client.SetReadDeadline(time.Now().Add(-time.Second))
	require.NoError(t, err)

	// Try to receive - should timeout
	_, _, err = client.ReceiveMessage()
	require.Error(t, err)

	// Reset deadline
	err = client.SetReadDeadline(time.Time{})
	require.NoError(t, err)

	// Send and receive should work again
	go func() {
		_ = server.SendHealthCheck()
	}()

	msgType, _, err := client.ReceiveMessage()
	require.NoError(t, err)
	assert.Equal(t, MessageTypeHealthCheck, msgType)
}

func TestTransport_InvalidMessage(t *testing.T) {
	// Create a mock connection that returns invalid data
	client, server := net.Pipe()

	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	transport := NewTransport(client)

	// Write invalid data directly to the pipe
	go func() {
		// Write something that's not a valid frame
		_, _ = server.Write([]byte("invalid data"))
		_ = server.Close()
	}()

	// Try to receive - should get error
	_, _, err := transport.ReceiveMessage()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "magic bytes")
}

func TestTransport_Addresses(t *testing.T) {
	client, server := createConnectedTransports(t)

	// Check that addresses are accessible
	assert.NotNil(t, client.LocalAddr())
	assert.NotNil(t, client.RemoteAddr())
	assert.NotNil(t, server.LocalAddr())
	assert.NotNil(t, server.RemoteAddr())

	// Local addr of client should equal remote addr of server
	assert.Equal(t, client.LocalAddr().String(), server.RemoteAddr().String())
	assert.Equal(t, server.LocalAddr().String(), client.RemoteAddr().String())
}
