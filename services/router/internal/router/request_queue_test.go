package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRequestQueue_EnqueueDequeue(t *testing.T) { 
	logger, _ := zap.NewDevelopment()
	queue := NewRequestQueue(10, logger)

	// Test enqueue.
	ctx := context.Background()
	data := []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)

	respChan, err := queue.Enqueue(data, ctx)
	require.NoError(t, err)
	require.NotNil(t, respChan)

	assert.Equal(t, 1, queue.Size())

	// Test dequeue all.
	requests := queue.DequeueAll()
	assert.Len(t, requests, 1)
	assert.Equal(t, data, requests[0].Data)
	assert.Equal(t, 0, queue.Size())
}

func TestRequestQueue_MaxSize(t *testing.T) { 
	logger, _ := zap.NewDevelopment()
	queue := NewRequestQueue(2, logger)

	ctx := context.Background()

	// Fill the queue.
	_, err := queue.Enqueue([]byte(`{"id":1}`), ctx)
	require.NoError(t, err)

	_, err = queue.Enqueue([]byte(`{"id":2}`), ctx)
	require.NoError(t, err)

	// Queue should be full now.
	assert.True(t, queue.IsFull())

	// Try to enqueue when full.
	_, err = queue.Enqueue([]byte(`{"id":3}`), ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue full")
}

func TestRequestQueue_Clear(t *testing.T) { 
	logger, _ := zap.NewDevelopment()
	queue := NewRequestQueue(10, logger)

	ctx := context.Background()

	// Add some requests.
	respChan1, _ := queue.Enqueue([]byte(`{"id":1}`), ctx)
	respChan2, _ := queue.Enqueue([]byte(`{"id":2}`), ctx)

	assert.Equal(t, 2, queue.Size())

	// Clear the queue.
	testErr := errors.New("test error")
	queue.Clear(testErr)

	assert.Equal(t, 0, queue.Size())
	assert.True(t, queue.IsEmpty())

	// Verify error was sent to response channels.
	select {
	case err := <-respChan1:
		assert.Equal(t, testErr, err)
	case <-time.After(time.Second):
		t.Fatal("Expected error on response channel 1")
	}

	select {
	case err := <-respChan2:
		assert.Equal(t, testErr, err)
	case <-time.After(time.Second):
		t.Fatal("Expected error on response channel 2")
	}
}

func TestRequestQueue_Metrics(t *testing.T) { 
	logger, _ := zap.NewDevelopment()
	queue := NewRequestQueue(2, logger)

	ctx := context.Background()

	// Enqueue some requests.
	_, err := queue.Enqueue([]byte(`{"id":1}`), ctx)
	require.NoError(t, err)
	_, err = queue.Enqueue([]byte(`{"id":2}`), ctx)
	require.NoError(t, err)

	// Try to enqueue when full (should be dropped).
	_, _ = queue.Enqueue([]byte(`{"id":3}`), ctx)

	// Process some requests.
	queue.DequeueAll()

	queued, dropped, processed := queue.GetMetrics()
	assert.Equal(t, int64(2), queued)
	assert.Equal(t, int64(1), dropped)
	assert.Equal(t, int64(2), processed)
}
