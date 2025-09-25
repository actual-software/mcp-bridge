package tracing

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestWebSocketSpanAttributes(t *testing.T) {
	tests := getWebSocketSpanAttributesTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifyWebSocketSpanAttributes(t, tt)
		})
	}
}

type wsSpanAttributesTestCase struct {
	name          string
	setupRequest  func() *http.Request
	remoteAddr    string
	expectedAttrs map[string]string
	expectedCount int
}

func getWebSocketSpanAttributesTestCases() []wsSpanAttributesTestCase {
	return []wsSpanAttributesTestCase{
		{
			name:          "basic WebSocket request",
			setupRequest:  setupBasicWebSocketRequest,
			remoteAddr:    "192.168.1.1",
			expectedAttrs: getBasicExpectedAttrs(),
			expectedCount: 7,
		},
		{
			name:          "WebSocket with protocol and version headers",
			setupRequest:  setupWebSocketWithProtocolRequest,
			remoteAddr:    "10.0.0.5",
			expectedAttrs: getProtocolExpectedAttrs(),
			expectedCount: 9,
		},
		{
			name:          "empty headers",
			setupRequest:  setupEmptyHeadersRequest,
			remoteAddr:    "127.0.0.1",
			expectedAttrs: getEmptyHeadersExpectedAttrs(),
			expectedCount: 7,
		},
	}
}

func setupBasicWebSocketRequest() *http.Request {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "ws://example.com/socket", http.NoBody)
	if err != nil {
		panic(err)
	}

	req.Host = "example.com"
	req.Header.Set("User-Agent", "test-client")

	return req
}

func setupWebSocketWithProtocolRequest() *http.Request {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"wss://secure.example.com/api/v1/socket", http.NoBody)
	if err != nil {
		panic(err)
	}

	req.Host = "secure.example.com"
	req.Header.Set("User-Agent", "mcp-bridge/1.0")
	req.Header.Set("Sec-WebSocket-Protocol", "mcp")
	req.Header.Set("Sec-WebSocket-Version", "13")

	return req
}

func setupEmptyHeadersRequest() *http.Request {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "ws://localhost:8080/", http.NoBody)
	if err != nil {
		panic(err)
	}

	req.Host = "localhost:8080"

	return req
}

func getBasicExpectedAttrs() map[string]string {
	return map[string]string{
		"net.transport":   "websocket",
		"net.peer.ip":     "192.168.1.1",
		"http.scheme":     "ws",
		"http.method":     "GET",
		"http.target":     "/socket",
		"http.host":       "example.com",
		"http.user_agent": "test-client",
	}
}

func getProtocolExpectedAttrs() map[string]string {
	return map[string]string{
		"net.transport":      "websocket",
		"net.peer.ip":        "10.0.0.5",
		"http.scheme":        "ws",
		"http.method":        "GET",
		"http.target":        "/api/v1/socket",
		"http.host":          "secure.example.com",
		"http.user_agent":    "mcp-bridge/1.0",
		"websocket.protocol": "mcp",
		"websocket.version":  "13",
	}
}

func getEmptyHeadersExpectedAttrs() map[string]string {
	return map[string]string{
		"net.transport":   "websocket",
		"net.peer.ip":     "127.0.0.1",
		"http.scheme":     "ws",
		"http.method":     "GET",
		"http.target":     "/",
		"http.host":       "localhost:8080",
		"http.user_agent": "",
	}
}

func verifyWebSocketSpanAttributes(t *testing.T, tt wsSpanAttributesTestCase) {
	t.Helper()

	req := tt.setupRequest()
	attrs := WebSocketSpanAttributes(req, tt.remoteAddr)

	assert.Len(t, attrs, tt.expectedCount)

	// Convert to map for easier assertion
	attrMap := make(map[string]string)
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsString()
	}

	for key, expectedValue := range tt.expectedAttrs {
		assert.Equal(t, expectedValue, attrMap[key], "Attribute %s", key)
	}
}

func TestMCPMessageAttributes(t *testing.T) {
	tests := getMCPMessageAttributesTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifyMCPMessageAttributes(t, tt)
		})
	}
}

type mcpMessageAttributesTestCase struct {
	name          string
	msgType       string
	method        string
	id            interface{}
	expectedAttrs map[string]string
	expectedCount int
}

func getMCPMessageAttributesTestCases() []mcpMessageAttributesTestCase {
	return []mcpMessageAttributesTestCase{
		{
			name:    "request message with string ID",
			msgType: "request",
			method:  "tools/list",
			id:      "req-123",
			expectedAttrs: map[string]string{
				"mcp.message.type": "request",
				"mcp.method":       "tools/list",
				"mcp.message.id":   "req-123",
			},
			expectedCount: 3,
		},
		{
			name:    "response message with numeric ID",
			msgType: "response",
			method:  "",
			id:      42,
			expectedAttrs: map[string]string{
				"mcp.message.type": "response",
				"mcp.message.id":   "42",
			},
			expectedCount: 2,
		},
		{
			name:    "notification message",
			msgType: "notification",
			method:  "progress",
			id:      nil,
			expectedAttrs: map[string]string{
				"mcp.message.type": "notification",
				"mcp.method":       "progress",
			},
			expectedCount: 2,
		},
		{
			name:    "minimal message",
			msgType: "error",
			method:  "",
			id:      nil,
			expectedAttrs: map[string]string{
				"mcp.message.type": "error",
			},
			expectedCount: 1,
		},
	}
}

func verifyMCPMessageAttributes(t *testing.T, tt mcpMessageAttributesTestCase) {
	t.Helper()

	attrs := MCPMessageAttributes(tt.msgType, tt.method, tt.id)

	assert.Len(t, attrs, tt.expectedCount)

	// Convert to map for easier assertion
	attrMap := make(map[string]string)
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsString()
	}

	for key, expectedValue := range tt.expectedAttrs {
		assert.Equal(t, expectedValue, attrMap[key], "Attribute %s", key)
	}
}

func TestWebSocketMessageEvent(t *testing.T) {
	tests := []struct {
		name        string
		setupCtx    func() context.Context
		direction   string
		messageType int
		size        int
		expectEvent bool
	}{
		{
			name: "text message sent",
			setupCtx: func() context.Context {
				tracer := noop.NewTracerProvider().Tracer("test")
				ctx, span := tracer.Start(context.Background(), "test")

				return trace.ContextWithSpan(ctx, span)
			},
			direction:   "sent",
			messageType: websocket.TextMessage,
			size:        256,
			expectEvent: true,
		},
		{
			name: "binary message received",
			setupCtx: func() context.Context {
				tracer := noop.NewTracerProvider().Tracer("test")
				ctx, span := tracer.Start(context.Background(), "test")

				return trace.ContextWithSpan(ctx, span)
			},
			direction:   "received",
			messageType: websocket.BinaryMessage,
			size:        1024,
			expectEvent: true,
		},
		{
			name:        "no span in context",
			setupCtx:    context.Background,
			direction:   "sent",
			messageType: websocket.TextMessage,
			size:        100,
			expectEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()

			// This function doesn't return anything, so we just verify it doesn't panic
			require.NotPanics(t, func() {
				WebSocketMessageEvent(ctx, tt.direction, tt.messageType, tt.size)
			})
		})
	}
}

func TestConnectionPoolAttributes(t *testing.T) {
	tests := []struct {
		name        string
		poolType    string
		size        int
		active      int
		idle        int
		expectedLen int
	}{
		{
			name:        "WebSocket pool",
			poolType:    "websocket",
			size:        10,
			active:      3,
			idle:        7,
			expectedLen: 4,
		},
		{
			name:        "TCP pool",
			poolType:    "tcp",
			size:        20,
			active:      15,
			idle:        5,
			expectedLen: 4,
		},
		{
			name:        "empty pool",
			poolType:    "http",
			size:        0,
			active:      0,
			idle:        0,
			expectedLen: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := ConnectionPoolAttributes(tt.poolType, tt.size, tt.active, tt.idle)

			assert.Len(t, attrs, tt.expectedLen)

			// Verify specific attributes
			attrMap := make(map[string]interface{})

			for _, attr := range attrs {
				key := string(attr.Key)
				if key == "pool.type" {
					attrMap[key] = attr.Value.AsString()
				} else {
					attrMap[key] = int(attr.Value.AsInt64())
				}
			}

			assert.Equal(t, tt.poolType, attrMap["pool.type"])
			assert.Equal(t, tt.size, attrMap["pool.size"])
			assert.Equal(t, tt.active, attrMap["pool.connections.active"])
			assert.Equal(t, tt.idle, attrMap["pool.connections.idle"])
		})
	}
}

func TestGatewayRouteAttributes(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		targetHost  string
		targetPort  int
		expectedLen int
	}{
		{
			name:        "production service",
			serviceName: "mcp-server",
			targetHost:  "backend.example.com",
			targetPort:  8080,
			expectedLen: 3,
		},
		{
			name:        "localhost service",
			serviceName: "local-dev",
			targetHost:  "127.0.0.1",
			targetPort:  3000,
			expectedLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := GatewayRouteAttributes(tt.serviceName, tt.targetHost, tt.targetPort)

			assert.Len(t, attrs, tt.expectedLen)

			// Verify attributes
			attrMap := make(map[string]interface{})

			for _, attr := range attrs {
				key := string(attr.Key)
				if key == "gateway.target.port" {
					attrMap[key] = int(attr.Value.AsInt64())
				} else {
					attrMap[key] = attr.Value.AsString()
				}
			}

			assert.Equal(t, tt.serviceName, attrMap["gateway.service"])
			assert.Equal(t, tt.targetHost, attrMap["gateway.target.host"])
			assert.Equal(t, tt.targetPort, attrMap["gateway.target.port"])
		})
	}
}

func TestAuthenticationAttributes(t *testing.T) {
	tests := []struct {
		name        string
		authType    string
		success     bool
		userID      string
		expectedLen int
	}{
		{
			name:        "successful bearer auth",
			authType:    "bearer",
			success:     true,
			userID:      "user123",
			expectedLen: 3,
		},
		{
			name:        "failed OAuth2 auth",
			authType:    "oauth2",
			success:     false,
			userID:      "",
			expectedLen: 2,
		},
		{
			name:        "successful mTLS auth",
			authType:    "mtls",
			success:     true,
			userID:      "client-cert-123",
			expectedLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := AuthenticationAttributes(tt.authType, tt.success, tt.userID)

			assert.Len(t, attrs, tt.expectedLen)

			// Verify attributes
			attrMap := make(map[string]interface{})

			for _, attr := range attrs {
				key := string(attr.Key)
				if key == "auth.success" {
					attrMap[key] = attr.Value.AsBool()
				} else {
					attrMap[key] = attr.Value.AsString()
				}
			}

			assert.Equal(t, tt.authType, attrMap["auth.type"])
			assert.Equal(t, tt.success, attrMap["auth.success"])

			if tt.userID != "" {
				assert.Equal(t, tt.userID, attrMap["auth.user.id"])
			}
		})
	}
}

func TestErrorAttributes(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		expectedLen int
		expectNil   bool
	}{
		{
			name:        "standard error",
			err:         errors.New("connection failed"),
			expectedLen: 3,
			expectNil:   false,
		},
		{
			name:        "custom error type",
			err:         &url.Error{Op: "dial", URL: "ws://example.com", Err: errors.New("timeout")},
			expectedLen: 3,
			expectNil:   false,
		},
		{
			name:        "nil error",
			err:         nil,
			expectedLen: 0,
			expectNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := ErrorAttributes(tt.err)

			if tt.expectNil {
				assert.Nil(t, attrs)

				return
			}

			assert.Len(t, attrs, tt.expectedLen)

			// Verify attributes
			attrMap := make(map[string]interface{})

			for _, attr := range attrs {
				key := string(attr.Key)
				if key == "error" {
					attrMap[key] = attr.Value.AsBool()
				} else {
					attrMap[key] = attr.Value.AsString()
				}
			}

			assert.Equal(t, true, attrMap["error"])
			assert.NotEmpty(t, attrMap["error.type"])
			assert.Equal(t, tt.err.Error(), attrMap["error.message"])
		})
	}
}

func TestRateLimitAttributes(t *testing.T) {
	tests := []struct {
		name        string
		limited     bool
		limit       int
		remaining   int
		resetTime   int64
		expectedLen int
	}{
		{
			name:        "rate limited",
			limited:     true,
			limit:       100,
			remaining:   0,
			resetTime:   1640995200,
			expectedLen: 4,
		},
		{
			name:        "within limits",
			limited:     false,
			limit:       1000,
			remaining:   750,
			resetTime:   1640995800,
			expectedLen: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := RateLimitAttributes(tt.limited, tt.limit, tt.remaining, tt.resetTime)

			assert.Len(t, attrs, tt.expectedLen)

			// Verify attributes
			attrMap := make(map[string]interface{})

			for _, attr := range attrs {
				key := string(attr.Key)
				switch key {
				case "ratelimit.limited":
					attrMap[key] = attr.Value.AsBool()
				case "ratelimit.reset_time":
					attrMap[key] = attr.Value.AsInt64()
				default:
					attrMap[key] = int(attr.Value.AsInt64())
				}
			}

			assert.Equal(t, tt.limited, attrMap["ratelimit.limited"])
			assert.Equal(t, tt.limit, attrMap["ratelimit.limit"])
			assert.Equal(t, tt.remaining, attrMap["ratelimit.remaining"])
			assert.Equal(t, tt.resetTime, attrMap["ratelimit.reset_time"])
		})
	}
}

func TestHealthCheckAttributes(t *testing.T) {
	tests := []struct {
		name        string
		checkType   string
		healthy     bool
		latencyMs   int64
		expectedLen int
	}{
		{
			name:        "healthy endpoint",
			checkType:   "http",
			healthy:     true,
			latencyMs:   25,
			expectedLen: 3,
		},
		{
			name:        "unhealthy WebSocket",
			checkType:   "websocket",
			healthy:     false,
			latencyMs:   5000,
			expectedLen: 3,
		},
		{
			name:        "TCP health check",
			checkType:   "tcp",
			healthy:     true,
			latencyMs:   1,
			expectedLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := HealthCheckAttributes(tt.checkType, tt.healthy, tt.latencyMs)

			assert.Len(t, attrs, tt.expectedLen)

			// Verify attributes
			attrMap := make(map[string]interface{})

			for _, attr := range attrs {
				key := string(attr.Key)
				switch key {
				case "health.check.healthy":
					attrMap[key] = attr.Value.AsBool()
				case "health.check.latency_ms":
					attrMap[key] = attr.Value.AsInt64()
				default:
					attrMap[key] = attr.Value.AsString()
				}
			}

			assert.Equal(t, tt.checkType, attrMap["health.check.type"])
			assert.Equal(t, tt.healthy, attrMap["health.check.healthy"])
			assert.Equal(t, tt.latencyMs, attrMap["health.check.latency_ms"])
		})
	}
}

func TestCircuitBreakerAttributes(t *testing.T) {
	tests := []struct {
		name        string
		state       string
		failures    int
		successes   int
		expectedLen int
	}{
		{
			name:        "circuit closed",
			state:       "closed",
			failures:    0,
			successes:   100,
			expectedLen: 3,
		},
		{
			name:        "circuit open",
			state:       "open",
			failures:    5,
			successes:   0,
			expectedLen: 3,
		},
		{
			name:        "circuit half-open",
			state:       "half-open",
			failures:    2,
			successes:   3,
			expectedLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := CircuitBreakerAttributes(tt.state, tt.failures, tt.successes)

			assert.Len(t, attrs, tt.expectedLen)

			// Verify attributes
			attrMap := make(map[string]interface{})

			for _, attr := range attrs {
				key := string(attr.Key)
				if key == "circuitbreaker.state" {
					attrMap[key] = attr.Value.AsString()
				} else {
					attrMap[key] = int(attr.Value.AsInt64())
				}
			}

			assert.Equal(t, tt.state, attrMap["circuitbreaker.state"])
			assert.Equal(t, tt.failures, attrMap["circuitbreaker.failures"])
			assert.Equal(t, tt.successes, attrMap["circuitbreaker.successes"])
		})
	}
}
