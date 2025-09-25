// Package constants provides test-specific constants for the router service tests.
package constants

import "time"

// Test timeouts and delays.
const (
	// TestShortTimeout is for quick test operations (1s).
	TestShortTimeout = time.Second
	// TestMediumTimeout is for medium test operations (2s).
	TestMediumTimeout = 2 * time.Second
	// TestLongTimeout is for longer test operations (5s).
	TestLongTimeout = 5 * time.Second
	// TestTickInterval is the default test tick interval (10ms).
	TestTickInterval = 10 * time.Millisecond
	// TestLongTickInterval is for longer test ticks (50ms).
	TestLongTickInterval = 50 * time.Millisecond
	// TestSleepShort is a short test sleep (100ms).
	TestSleepShort = 100 * time.Millisecond
	// TestTimeoutMs is the default test timeout in milliseconds.
	TestTimeoutMs = 5000
)

// Test iteration and count constants.
const (
	// TestConcurrentRoutines is the number of concurrent test routines.
	TestConcurrentRoutines = 100
	// TestMaxRetries is the max retries for tests.
	TestMaxRetries = 3
	// TestUnknownState is an unknown state value for testing.
	TestUnknownState = 999
)

// Test JSON-RPC constants.
const (
	// TestJSONRPCVersion is the JSON-RPC version for tests.
	TestJSONRPCVersion = "2.0"
	// TestProtocolVersion is the protocol version for tests.
	TestProtocolVersion = "1.0"
)

// Test script constants.
const (
	// TestPythonEchoScript is a Python script that echoes MCP requests for testing.
	TestPythonEchoScript = `#!/usr/bin/env python3
import json
import sys

for line in sys.stdin:
    try:
        request = json.loads(line.strip())
        response = {
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": {"echo": request.get("method", "unknown")}
        }
        print(json.dumps(response))
        sys.stdout.flush()
    except:
        pass
`
)

// Test HTTP status codes and ports.
const (
	// TestHTTPPort is the default test HTTP port.
	TestHTTPPort = 8080
	// TestHTTPSPort is the default test HTTPS port.
	TestHTTPSPort = 8443
	// TestWebSocketPort is the default test WebSocket port.
	TestWebSocketPort = 8081
	// TestMetricsPort is the default test metrics port.
	TestMetricsPort = 9090
)

// Test buffer and batch sizes.
const (
	// TestBufferSize is the default test buffer size.
	TestBufferSize = 1024
	// TestSmallBufferSize is a small test buffer size.
	TestSmallBufferSize = 256
	// TestLargeBufferSize is a large test buffer size.
	TestLargeBufferSize = 4096
	// TestBatchSize is the default test batch size.
	TestBatchSize = 10
)

// Test thresholds and limits.
const (
	// TestMaxMessages is the max messages for tests.
	TestMaxMessages = 1000
	// TestMaxConnections is the max connections for tests.
	TestMaxConnections = 50
	// TestErrorThreshold is the error threshold for tests.
	TestErrorThreshold = 5
	// TestHealthCheckCount is the number of health checks in tests.
	TestHealthCheckCount = 3
)
