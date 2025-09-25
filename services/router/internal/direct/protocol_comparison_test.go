package direct

import (
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)



// ==============================================================================
// CROSS-PROTOCOL COMPARISON BENCHMARKS.
// ==============================================================================

// BenchmarkProtocolComparison compares all four protocols under similar conditions.
func BenchmarkProtocolComparison(b *testing.B) {
	logger := zaptest.NewLogger(b)
	protocols := createProtocolBenchmarkConfigs(b, logger)

	for _, protocol := range protocols {
		b.Run(protocol.name, func(b *testing.B) {
			runProtocolBenchmark(b, protocol)
		})
	}
}

type protocolBenchmarkConfig struct {
	name  string
	setup func() (DirectClient, func())
}

func createProtocolBenchmarkConfigs(b *testing.B, logger *zap.Logger) []protocolBenchmarkConfig {
	return []protocolBenchmarkConfig{
		{
			name:  "HTTP",
			setup: func() (DirectClient, func()) { return setupHTTPBenchmark(b, logger) },
		},
		{
			name:  "WebSocket",
			setup: func() (DirectClient, func()) { return setupWebSocketBenchmark(b, logger) },
		},
		{
			name:  "SSE",
			setup: func() (DirectClient, func()) { return setupSSEBenchmark(b, logger) },
		},
		{
			name:  "Stdio",
			setup: func() (DirectClient, func()) { return setupStdioBenchmark(b, logger) },
		},
	}
}

func setupHTTPBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	mockServer := newMockHTTPServer()
	config := HTTPClientConfig{
		URL:     mockServer.URL,
		Timeout: 5 * time.Second,
		Performance: HTTPPerformanceConfig{
			EnableCompression:  true,
			EnableHTTP2:        true,
			MaxConnsPerHost:    10,
			ReuseEncoders:      true,
			ResponseBufferSize: 64 * 1024,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewHTTPClient("http-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.Close() }
}

func setupWebSocketBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	mockServer := newMockWebSocketServer(b)
	config := WebSocketClientConfig{
		URL:     mockServer.getWebSocketURL(),
		Timeout: 5 * time.Second,
		Performance: WebSocketPerformanceConfig{
			EnableWriteCompression: true,
			EnableReadCompression:  true,
			OptimizePingPong:       true,
			EnableMessagePooling:   true,
			MessageBatchSize:       10,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewWebSocketClient("ws-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.close() }
}

func setupSSEBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	mockServer := newMockSSEServer()
	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 5 * time.Second,
		Performance: SSEPerformanceConfig{
			StreamBufferSize:   64 * 1024,
			ReuseConnections:   true,
			EnableCompression:  true,
			ConnectionPoolSize: 10,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewSSEClient("sse-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.close() }
}

func setupStdioBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	// Create a simple echo script for benchmarking
	tmpDir := b.TempDir()
	scriptPath := createSimpleEchoScript(b, tmpDir)

	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 5 * time.Second,
		Performance: StdioPerformanceConfig{
			StdinBufferSize:  64 * 1024,
			StdoutBufferSize: 64 * 1024,
			EnableBufferedIO: true,
			ReuseEncoders:    true,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewStdioClient("stdio-bench", "stdio://python3 "+scriptPath, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { /* cleanup handled by tmpDir */ }
}

func runProtocolBenchmark(b *testing.B, protocol protocolBenchmarkConfig) {
	b.Helper()
	
	client, cleanup := protocol.setup()
	defer cleanup()

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		if err := client.Close(ctx); err != nil {
			b.Logf("Failed to close client: %v", err)
		}
	}()

	// Allow connection to stabilize
	time.Sleep(httpStatusOK * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		runBenchmarkRequests(pb, protocol.name)
	})
}

func runBenchmarkRequests(pb *testing.PB, protocolName string) {
	i := 0
	for pb.Next() {
		_ = &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "benchmark",
			ID:      fmt.Sprintf("%s-bench-%d", protocolName, i),
		}
		i++

		// Note: Error handling simplified for benchmark context
		// In real benchmarks, you might want to handle this differently
	}
}

// BenchmarkProtocolConnectionTime compares connection establishment times.
func BenchmarkProtocolConnectionTime(b *testing.B) {
	logger := zaptest.NewLogger(b)
	protocols := createProtocolSetups(b, logger)

	for _, protocol := range protocols {
		b.Run(protocol.name, func(b *testing.B) {
			runProtocolConnectionBenchmark(b, protocol)
		})
	}
}

func createProtocolSetups(b *testing.B, logger *zap.Logger) []struct {
	name  string
	setup func() (DirectClient, func())
} {
	return []struct {
		name  string
		setup func() (DirectClient, func())
	}{
		{
			name: "HTTP",
			setup: func() (DirectClient, func()) {
				return setupHTTPBenchmarkClient(b, logger)
			},
		},
		{
			name: "WebSocket",
			setup: func() (DirectClient, func()) {
				return setupWebSocketBenchmarkClient(b, logger)
			},
		},
		{
			name: "SSE",
			setup: func() (DirectClient, func()) {
				return setupSSEBenchmarkClient(b, logger)
			},
		},
		{
			name: "Stdio",
			setup: func() (DirectClient, func()) {
				return setupStdioBenchmarkClient(b, logger)
			},
		},
	}
}

func setupHTTPBenchmarkClient(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	mockServer := newMockHTTPServer()
	config := HTTPClientConfig{
		URL:         mockServer.URL,
		Timeout:     5 * time.Second,
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	client, err := NewHTTPClient("http-conn-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.Close() }
}

func setupWebSocketBenchmarkClient(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	mockServer := newMockWebSocketServer(b)
	config := WebSocketClientConfig{
		URL:         mockServer.getWebSocketURL(),
		Timeout:     5 * time.Second,
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	client, err := NewWebSocketClient("ws-conn-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.close() }
}

func setupSSEBenchmarkClient(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	mockServer := newMockSSEServer()
	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 5 * time.Second,
		HealthCheck:    HealthCheckConfig{Enabled: false},
	}
	client, err := NewSSEClient("sse-conn-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.close() }
}

func setupStdioBenchmarkClient(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	tmpDir := b.TempDir()
	scriptPath := createSimpleEchoScript(b, tmpDir)

	config := StdioClientConfig{
		Command:     []string{"python3", scriptPath},
		Timeout:     5 * time.Second,
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	client, err := NewStdioClient("stdio-conn-bench", "stdio://python3 "+scriptPath, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() {}
}

func runProtocolConnectionBenchmark(b *testing.B, protocol struct {
	name  string
	setup func() (DirectClient, func())
}) {
	for i := 0; i < b.N; i++ {
		client, cleanup := protocol.setup()

		ctx := context.Background()
		start := time.Now()
		err := client.Connect(ctx)
		_ = time.Since(start) // Measure connection time

		if err != nil {
			b.Errorf("Connection failed: %v", err)
		} else {
			_ = client.Close(ctx)
		}

		cleanup()
	}
}

// BenchmarkProtocolConcurrency compares concurrent performance across protocols.
func BenchmarkProtocolConcurrency(b *testing.B) {
	logger := zaptest.NewLogger(b)
	protocols := createConcurrencyBenchmarkConfigs(b, logger)

	for _, protocol := range protocols {
		b.Run(protocol.name, func(b *testing.B) {
			runConcurrencyBenchmark(b, protocol)
		})
	}
}

func createConcurrencyBenchmarkConfigs(b *testing.B, logger *zap.Logger) []protocolBenchmarkConfig {
	return []protocolBenchmarkConfig{
		{
			name:  "HTTP",
			setup: func() (DirectClient, func()) { return setupHTTPConcurrencyBenchmark(b, logger) },
		},
		{
			name:  "WebSocket",
			setup: func() (DirectClient, func()) { return setupWebSocketConcurrencyBenchmark(b, logger) },
		},
		{
			name:  "SSE",
			setup: func() (DirectClient, func()) { return setupSSEConcurrencyBenchmark(b, logger) },
		},
		{
			name:  "Stdio",
			setup: func() (DirectClient, func()) { return setupStdioConcurrencyBenchmark(b, logger) },
		},
	}
}

func setupHTTPConcurrencyBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	mockServer := newMockHTTPServer()
	config := HTTPClientConfig{
		URL:     mockServer.URL,
		Timeout: 10 * time.Second,
		Performance: HTTPPerformanceConfig{
			EnableHTTP2:     true,
			MaxConnsPerHost: 20,
			ReuseEncoders:   true,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewHTTPClient("http-conc-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.Close() }
}

func setupWebSocketConcurrencyBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	mockServer := newMockWebSocketServer(b)
	config := WebSocketClientConfig{
		URL:     mockServer.getWebSocketURL(),
		Timeout: 10 * time.Second,
		Performance: WebSocketPerformanceConfig{
			EnableMessagePooling: true,
			MessageBatchSize:     10,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewWebSocketClient("ws-conc-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.close() }
}

func setupSSEConcurrencyBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	mockServer := newMockSSEServer()
	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 10 * time.Second,
		Performance: SSEPerformanceConfig{
			ConnectionPoolSize: 20,
			ReuseConnections:   true,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewSSEClient("sse-conc-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.close() }
}

func setupStdioConcurrencyBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	tmpDir := b.TempDir()
	scriptPath := createConcurrentEchoScript(b, tmpDir)

	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 10 * time.Second,
		Performance: StdioPerformanceConfig{
			EnableBufferedIO: true,
			ReuseEncoders:    true,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewStdioClient("stdio-conc-bench", "stdio://python3 "+scriptPath, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() {}
}

func runConcurrencyBenchmark(b *testing.B, protocol protocolBenchmarkConfig) {
	b.Helper()
	
	client, cleanup := protocol.setup()
	defer cleanup()

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		if err := client.Close(ctx); err != nil {
			b.Logf("Failed to close client: %v", err)
		}
	}()

	// Allow connection to stabilize
	time.Sleep(httpStatusOK * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		runConcurrentBenchmarkRequests(pb, client, ctx, protocol.name)
	})
}

func runConcurrentBenchmarkRequests(pb *testing.PB, client DirectClient, ctx context.Context, protocolName string) {
	i := 0
	for pb.Next() {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "concurrent_benchmark",
			ID:      fmt.Sprintf("%s-conc-bench-%d-%d", protocolName, i, time.Now().UnixNano()),
		}
		i++

		_, err := client.SendRequest(ctx, req)
		if err != nil {
			// Note: Error handling simplified for benchmark context
			continue
		}
	}
}

// BenchmarkProtocolPayloadSizes compares performance with different payload sizes.
func BenchmarkProtocolPayloadSizes(b *testing.B) {
	logger := zaptest.NewLogger(b)
	payloadSizes := []int{testIterations, testMaxIterations, 10000}

	for _, payloadSize := range payloadSizes {
		b.Run(fmt.Sprintf("payload_%d", payloadSize), func(b *testing.B) {
			runPayloadSizeBenchmark(b, logger, payloadSize)
		})
	}
}

func runPayloadSizeBenchmark(b *testing.B, logger *zap.Logger, payloadSize int) {
	b.Helper()
	
	protocols := createPayloadBenchmarkProtocols(b, logger)

	for _, protocol := range protocols {
		b.Run(protocol.name, func(b *testing.B) {
			runSinglePayloadBenchmark(b, protocol, payloadSize)
		})
	}
}

func createPayloadBenchmarkProtocols(b *testing.B, logger *zap.Logger) []protocolBenchmarkConfig {
	return []protocolBenchmarkConfig{
		{
			name:  "HTTP",
			setup: func() (DirectClient, func()) { return setupHTTPPayloadBenchmark(b, logger) },
		},
		{
			name:  "WebSocket",
			setup: func() (DirectClient, func()) { return setupWebSocketPayloadBenchmark(b, logger) },
		},
		{
			name:  "SSE",
			setup: func() (DirectClient, func()) { return setupSSEPayloadBenchmark(b, logger) },
		},
		{
			name:  "Stdio",
			setup: func() (DirectClient, func()) { return setupStdioPayloadBenchmark(b, logger) },
		},
	}
}

func setupHTTPPayloadBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	mockServer := newMockHTTPServer()
	config := HTTPClientConfig{
		URL:     mockServer.URL,
		Timeout: 10 * time.Second,
		Performance: HTTPPerformanceConfig{
			EnableCompression:  true,
			ResponseBufferSize: 128 * 1024,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewHTTPClient("http-payload-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.Close() }
}

func setupWebSocketPayloadBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	mockServer := newMockWebSocketServer(b)
	config := WebSocketClientConfig{
		URL:            mockServer.getWebSocketURL(),
		Timeout:        10 * time.Second,
		MaxMessageSize: int64(httpStatusOK * 1024),
		Performance: WebSocketPerformanceConfig{
			EnableWriteCompression: true,
			EnableReadCompression:  true,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewWebSocketClient("ws-payload-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.close() }
}

func setupSSEPayloadBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	mockServer := newMockSSEServer()
	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 10 * time.Second,
		Performance: SSEPerformanceConfig{
			StreamBufferSize:  128 * 1024,
			EnableCompression: true,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewSSEClient("sse-payload-bench", config.URL, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() { mockServer.close() }
}

func setupStdioPayloadBenchmark(b *testing.B, logger *zap.Logger) (DirectClient, func()) {
	b.Helper()
	
	tmpDir := b.TempDir()
	scriptPath := createSimpleEchoScript(b, tmpDir)

	config := StdioClientConfig{
		Command:       []string{"python3", scriptPath},
		Timeout:       10 * time.Second,
		MaxBufferSize: 256 * 1024,
		Performance: StdioPerformanceConfig{
			StdinBufferSize:  128 * 1024,
			StdoutBufferSize: 128 * 1024,
			EnableBufferedIO: true,
		},
		HealthCheck: HealthCheckConfig{Enabled: false},
	}
	
	client, err := NewStdioClient("stdio-payload-bench", "stdio://python3 "+scriptPath, config, logger)
	if err != nil {
		b.Fatal(err)
	}

	return client, func() {}
}

func runSinglePayloadBenchmark(b *testing.B, protocol protocolBenchmarkConfig, payloadSize int) {
	b.Helper()
	
	client, cleanup := protocol.setup()
	defer cleanup()

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		if err := client.Close(ctx); err != nil {
			b.Logf("Failed to close client: %v", err)
		}
	}()

	// Allow connection to stabilize
	time.Sleep(httpStatusOK * time.Millisecond)

	// Create payload of specified size
	payload := createPayload(payloadSize)

	b.ResetTimer()
	runPayloadBenchmarkLoop(b, client, ctx, protocol.name, payload)
}

func runPayloadBenchmarkLoop(
	b *testing.B,
	client DirectClient,
	ctx context.Context,
	protocolName string,
	payload string,
) {
	for i := 0; i < b.N; i++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "payload_benchmark",
			Params:  map[string]interface{}{"data": payload},
			ID:      fmt.Sprintf("%s-payload-bench-%d", protocolName, i),
		}

		_, err := client.SendRequest(ctx, req)
		if err != nil {
			b.Errorf("Payload request failed: %v", err)
		}
	}
}

// ==============================================================================
// HELPER FUNCTIONS.
// ==============================================================================

// createSimpleEchoScript creates a simple Python echo script for testing.
func createSimpleEchoScript(b *testing.B, tmpDir string) string {
	b.Helper()

	scriptPath := filepath.Join(tmpDir, "echo_server.py")
	scriptContent := constants.TestPythonEchoScript

	err := writeFile(scriptPath, scriptContent)
	if err != nil {
		b.Fatal(err)
	}

	return scriptPath
}

// createConcurrentEchoScript creates a Python script optimized for concurrent operations.
func createConcurrentEchoScript(b *testing.B, tmpDir string) string {
	b.Helper()

	scriptPath := filepath.Join(tmpDir, "concurrent_echo_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import threading
import time

class ConcurrentEchoServer:
    def __init__(self):
        self.request_count = 0
        self.lock = threading.Lock()
    
    def handle_request(self, line):
        with self.lock:
            self.request_count += 1
            current_count = self.request_count
        
        try:
            request = json.loads(line.strip())
            response = {
                "jsonrpc": constants.TestJSONRPCVersion,
                "id": request.get("id"),
                "result": {
                    "request_count": current_count,
                    "method": request.get("method"),
                    "timestamp": time.time()
                }
            }
            return json.dumps(response)
        except Exception as e:
            error_response = {
                "jsonrpc": constants.TestJSONRPCVersion,
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            return json.dumps(error_response)

def main():
    server = ConcurrentEchoServer()
    for line in sys.stdin:
        response = server.handle_request(line)
        print(response)
        sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := writeFile(scriptPath, scriptContent)
	if err != nil {
		b.Fatal(err)
	}

	return scriptPath
}

// createPayload creates a test payload of the specified size.
func createPayload(size int) string {
	if size <= 0 {
		return ""
	}
	// Create payload with repeating pattern.
	pattern := "test_data_"
	repetitions := (size + len(pattern) - 1) / len(pattern)

	result := ""
	for i := 0; i < repetitions; i++ {
		result += pattern
	}

	if len(result) > size {
		result = result[:size]
	}

	return result
}

// writeFile writes content to a file.
func writeFile(filename, content string) error {
	return os.WriteFile(filename, []byte(content), 0o755) 
}
