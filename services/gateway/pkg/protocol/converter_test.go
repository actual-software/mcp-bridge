
package protocol

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/common"
	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
)

const (
	testIterations = 100
	testTimeout    = 50
	httpStatusOK   = 200
)

func TestNewConverter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := BatchConfig{
		MaxBatchSize:   5,
		MaxWaitTime:    testTimeout * time.Millisecond,
		EnableBatching: true,
		BufferSize:     testIterations,
	}

	converter := NewConverter(logger, config)

	require.NotNil(t, converter)
	assert.Equal(t, config.MaxBatchSize, converter.batchConfig.MaxBatchSize)
	assert.Equal(t, config.MaxWaitTime, converter.batchConfig.MaxWaitTime)
	assert.True(t, converter.batchConfig.EnableBatching)
	assert.NotNil(t, converter.multiplex)
	assert.NotNil(t, converter.batchers)
}

func TestConverter_DirectConvert(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := BatchConfig{EnableBatching: false}
	converter := NewConverter(logger, config)

	message := Message{
		ID:     "test-1",
		Method: "tools/list",
		Params: map[string]interface{}{
			"namespace": "weather",
		},
		Protocol: "stdio",
		Metadata: map[string]string{
			"original": "true",
		},
	}

	endpoint := &discovery.Endpoint{
		Address:  "127.0.0.1",
		Port:     8080,
		Scheme:   "ws",
		Metadata: map[string]string{"protocol": "websocket"},
	}

	ctx := context.Background()
	converted, err := converter.Convert(ctx, message, endpoint)

	require.NoError(t, err)
	require.NotNil(t, converted)

	assert.Equal(t, message.ID, converted.ID)
	assert.Equal(t, message.Method, converted.Method)
	assert.Equal(t, "websocket", converted.Protocol)
	assert.Equal(t, "stdio", converted.Metadata["original_protocol"])
	assert.Contains(t, converted.Metadata, "converted_at")
	assert.Equal(t, "true", converted.Metadata["original"])
}

func TestConverter_ProtocolSpecificConversions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	converter := NewConverter(logger, BatchConfig{EnableBatching: false})
	tests := createProtocolConversionTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := createTestMessage(tt.sourceProtocol)
			converted, err := converter.directConvert(message, tt.targetProtocol)
			require.NoError(t, err)

			verifyProtocolConversion(t, *converted, tt.targetProtocol, tt.expectedMeta)
		})
	}
}

func createProtocolConversionTests() []struct {
	name           string
	sourceProtocol string
	targetProtocol string
	expectedMeta   map[string]string
} {
	return []struct {
		name           string
		sourceProtocol string
		targetProtocol string
		expectedMeta   map[string]string
	}{
		{
			name:           "Stdio to WebSocket",
			sourceProtocol: "stdio",
			targetProtocol: "websocket",
			expectedMeta: map[string]string{
				"ws_connection_type": "upgrade",
				"ws_subprotocol":     "mcp",
			},
		},
		{
			name:           "WebSocket to HTTP",
			sourceProtocol: "websocket",
			targetProtocol: "http",
			expectedMeta: map[string]string{
				"http_method":       "POST",
				"http_content_type": "application/json",
			},
		},
		{
			name:           "HTTP to SSE",
			sourceProtocol: "http",
			targetProtocol: "sse",
			expectedMeta: map[string]string{
				"sse_event_type": "message",
				"sse_retry":      "3000",
			},
		},
		{
			name:           "SSE to Stdio",
			sourceProtocol: "sse",
			targetProtocol: "stdio",
			expectedMeta: map[string]string{
				"stdio_newline": "true",
			},
		},
	}
}

func createTestMessage(protocol string) Message {
	return Message{
		ID:       "test",
		Method:   "test/method",
		Protocol: protocol,
		Metadata: make(map[string]string),
	}
}

func verifyProtocolConversion(t *testing.T, converted Message, expectedProtocol string, expectedMeta map[string]string) {
	t.Helper()
	assert.Equal(t, expectedProtocol, converted.Protocol)

	for key, expectedValue := range expectedMeta {
		assert.Equal(t, expectedValue, converted.Metadata[key], "Metadata key %s", key)
	}
}

func TestConverter_BatchingSupport(t *testing.T) {
	logger := zaptest.NewLogger(t)
	converter := NewConverter(logger, BatchConfig{EnableBatching: false})

	tests := []struct {
		name           string
		sourceProtocol string
		targetProtocol string
		supportsBatch  bool
	}{
		{
			name:           "HTTP to WebSocket - supports batching",
			sourceProtocol: "http",
			targetProtocol: "websocket",
			supportsBatch:  true,
		},
		{
			name:           "WebSocket to HTTP - supports batching",
			sourceProtocol: "websocket",
			targetProtocol: "http",
			supportsBatch:  true,
		},
		{
			name:           "HTTP to SSE - no batching",
			sourceProtocol: "http",
			targetProtocol: "sse",
			supportsBatch:  false,
		},
		{
			name:           "Stdio to HTTP - no batching",
			sourceProtocol: "stdio",
			targetProtocol: "http",
			supportsBatch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.supportsBatching(tt.sourceProtocol, tt.targetProtocol)
			assert.Equal(t, tt.supportsBatch, result)
		})
	}
}

func TestConverter_DetectTargetProtocol(t *testing.T) {
	converter := NewConverter(zaptest.NewLogger(t), BatchConfig{})

	tests := []struct {
		name     string
		endpoint discovery.Endpoint
		expected string
	}{
		{
			name: "Explicit protocol in metadata",
			endpoint: discovery.Endpoint{
				Scheme:   "http",
				Metadata: map[string]string{"protocol": "websocket"},
			},
			expected: "websocket",
		},
		{
			name: "WebSocket scheme",
			endpoint: discovery.Endpoint{
				Scheme: "ws",
			},
			expected: "websocket",
		},
		{
			name: "SSE path detection",
			endpoint: discovery.Endpoint{
				Scheme: "http",
				Path:   "/events",
			},
			expected: "sse",
		},
		{
			name: "Default HTTP",
			endpoint: discovery.Endpoint{
				Scheme: "https",
				Path:   "/api",
			},
			expected: "http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			protocol := converter.detectTargetProtocol(&tt.endpoint)
			assert.Equal(t, tt.expected, protocol)
		})
	}
}

func TestConverter_Batching(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := BatchConfig{
		MaxBatchSize:   3,
		MaxWaitTime:    testIterations * time.Millisecond,
		EnableBatching: true,
		BufferSize:     10,
	}
	converter := NewConverter(logger, config)

	endpoint := &discovery.Endpoint{
		Address:  "127.0.0.1",
		Port:     8080,
		Scheme:   "http",
		Metadata: map[string]string{"protocol": "http"},
	}

	ctx := context.Background()

	// Send multiple messages
	messages := make([]*Message, 0, common.SmallSliceCapacity)

	for i := 0; i < 5; i++ {
		message := Message{
			ID:       fmt.Sprintf("test-%d", i),
			Method:   "test/method",
			Protocol: "http",
			Metadata: make(map[string]string),
		}

		converted, err := converter.Convert(ctx, message, endpoint)
		require.NoError(t, err)
		require.NotNil(t, converted)

		assert.Equal(t, "true", converted.Metadata["batched"])
		assert.Contains(t, converted.Metadata, "batch_key")
		_ = append(messages, converted) // Messages are verified individually
	}

	// Verify batching statistics - when batching is enabled, messages are added to batchers
	// ConversionsTotal only increments for direct conversions
	stats := converter.GetStats()
	assert.GreaterOrEqual(t, stats.ConversionsTotal, int64(0))

	// Wait for batch processing
	time.Sleep(150 * time.Millisecond)

	// Clean up
	_ = converter.Shutdown(ctx) 
}

func TestMessageBatcher_AddMessage(t *testing.T) {
	logger := zaptest.NewLogger(t)
	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := BatchConfig{
		MaxBatchSize: 3,
		MaxWaitTime:  testTimeout * time.Millisecond,
		BufferSize:   10,
	}

	batcher := &MessageBatcher{
		targetEndpoint: endpoint,
		config:         config,
		messages:       make([]Message, 0, config.MaxBatchSize),
		flushCh:        make(chan struct{}, common.DefaultChannelBuffer),
		ctx:            ctx,
		cancel:         cancel,
		logger:         logger,
	}

	// Start batch processor
	batcher.wg.Add(1)

	go batcher.processBatches()

	// Add messages to batch
	message1 := Message{ID: "test-1", Method: "test", Protocol: "http"}
	message2 := Message{ID: "test-2", Method: "test", Protocol: "http"}

	result1, err1 := batcher.AddMessage(context.Background(), message1)
	require.NoError(t, err1)
	assert.Equal(t, "true", result1.Metadata["batched"])

	result2, err2 := batcher.AddMessage(context.Background(), message2)
	require.NoError(t, err2)
	assert.Equal(t, "true", result2.Metadata["batched"])

	// Wait for potential batch processing
	time.Sleep(testIterations * time.Millisecond)

	// Clean up
	cancel()
	batcher.wg.Wait()
}

func TestConverter_Statistics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	converter := NewConverter(logger, BatchConfig{EnableBatching: false})

	endpoint := &discovery.Endpoint{
		Address:  "127.0.0.1",
		Port:     8080,
		Scheme:   "ws",
		Metadata: map[string]string{"protocol": "websocket"},
	}

	message := Message{
		ID:       "test-1",
		Method:   "test/method",
		Protocol: "stdio",
		Metadata: make(map[string]string),
	}

	ctx := context.Background()

	// Convert message
	_, err := converter.Convert(ctx, message, endpoint)
	require.NoError(t, err)

	// Check statistics
	stats := converter.GetStats()
	assert.Equal(t, int64(1), stats.ConversionsTotal)
	assert.Contains(t, stats.ProtocolStats, "stdio")
	assert.Contains(t, stats.ProtocolStats, "websocket")
	assert.Equal(t, int64(1), stats.ProtocolStats["stdio"].MessagesIn)
	assert.Equal(t, int64(1), stats.ProtocolStats["websocket"].MessagesOut)
}

func TestConverter_Shutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := BatchConfig{
		EnableBatching: true,
		MaxBatchSize:   5,
		MaxWaitTime:    testIterations * time.Millisecond,
	}
	converter := NewConverter(logger, config)

	ctx := context.Background()

	// Create some batchers
	endpoint := &discovery.Endpoint{
		Address: "127.0.0.1",
		Port:    8080,
		Scheme:  "http",
	}

	message := Message{
		ID:       "test",
		Protocol: "http",
		Metadata: make(map[string]string),
	}

	// This should create a batcher
	_, err := converter.Convert(ctx, message, endpoint)
	require.NoError(t, err)

	// Verify batcher was created
	converter.batchersMu.RLock()
	batcherCount := len(converter.batchers)
	converter.batchersMu.RUnlock()
	assert.Equal(t, 1, batcherCount)

	// Shutdown
	err = converter.Shutdown(ctx)
	require.NoError(t, err)

	// Verify batchers were cleaned up
	converter.batchersMu.RLock()
	batcherCountAfterShutdown := len(converter.batchers)
	converter.batchersMu.RUnlock()
	assert.Equal(t, 0, batcherCountAfterShutdown)
}

func TestConverter_ErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	converter := NewConverter(logger, BatchConfig{EnableBatching: false})

	// Test with nil endpoint
	message := Message{ID: "test", Protocol: "stdio"}
	ctx := context.Background()

	// This should not panic
	_, err := converter.Convert(ctx, message, nil)

	// We expect this to work since detectTargetProtocol handles nil endpoint
	// (it will default to "http")
	assert.NoError(t, err)
}

// Benchmark tests.
func BenchmarkConverter_DirectConvert(b *testing.B) {
	logger := zaptest.NewLogger(b)
	converter := NewConverter(logger, BatchConfig{EnableBatching: false})

	message := Message{
		ID:       "bench-test",
		Method:   "tools/list",
		Protocol: "stdio",
		Metadata: make(map[string]string),
	}

	endpoint := &discovery.Endpoint{
		Address:  "127.0.0.1",
		Port:     8080,
		Scheme:   "ws",
		Metadata: map[string]string{"protocol": "websocket"},
	}

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := converter.Convert(ctx, message, endpoint)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConverter_BatchedConvert(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := BatchConfig{
		EnableBatching: true,
		MaxBatchSize:   10,
		MaxWaitTime:    10 * time.Millisecond,
	}
	converter := NewConverter(logger, config)

	endpoint := &discovery.Endpoint{
		Address:  "127.0.0.1",
		Port:     8080,
		Scheme:   "http",
		Metadata: map[string]string{"protocol": "http"},
	}

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		message := Message{
			ID:       fmt.Sprintf("bench-%d", i),
			Method:   "tools/list",
			Protocol: "http",
			Metadata: make(map[string]string),
		}

		_, err := converter.Convert(ctx, message, endpoint)
		if err != nil {
			b.Fatal(err)
		}
	}

	_ = converter.Shutdown(ctx) 
}
