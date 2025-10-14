package gateway

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"

	"github.com/gorilla/websocket"
	"go.uber.org/zap/zaptest"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"
	gatewayTestutil "github.com/actual-software/mcp-bridge/services/gateway/test/testutil"
	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"github.com/actual-software/mcp-bridge/test/testutil"
)

func TestNewClient(t *testing.T) {
	tests := createNewClientTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := testutil.NewTestLogger(t)
			client, err := NewClient(tt.config, logger)

			validateNewClientResult(t, tt, client, err)
		})
	}
}

func createNewClientTests() []struct {
	name      string
	config    config.GatewayConfig
	wantError bool
} {
	return []struct {
		name      string
		config    config.GatewayConfig
		wantError bool
	}{
		{
			name: "Valid config",
			config: config.GatewayConfig{
				URL: "wss://gateway.example.com",
				Auth: common.AuthConfig{
					Type:  "bearer",
					Token: "test-token",
				},
				TLS: common.TLSConfig{
					Verify: true,
				},
			},
			wantError: false,
		},
		{
			name: "Config with TLS options",
			config: config.GatewayConfig{
				URL: "wss://gateway.example.com",
				Auth: common.AuthConfig{
					Type:  "bearer",
					Token: "test-token",
				},
				TLS: common.TLSConfig{
					Verify:       false,
					MinVersion:   "1.2",
					CipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
				},
			},
			wantError: false,
		},
	}
}

func validateNewClientResult(t *testing.T, tt struct {
	name      string
	config    config.GatewayConfig
	wantError bool
}, client *Client, err error) {
	t.Helper()

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")
		}

		return
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)

		return
	}

	if client == nil {
		t.Error("Expected client to be created")

		return
	}

	if client.tlsConfig == nil {
		t.Error("Expected TLS config to be set")
	}

	if client.httpClient == nil {
		t.Error("Expected HTTP client to be set")
	}
}

func TestClient_configureTLS(t *testing.T) {
	tests := createConfigureTLSTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := testutil.NewTestLogger(t)
			tlsConfig := prepareTLSConfigForTest(t, tt.tlsConfig, tt.name)

			client := &Client{
				config: config.GatewayConfig{
					TLS: tlsConfig,
				},
				logger: logger,
			}

			err := client.configureTLS()
			if err != nil {
				t.Fatalf("configureTLS() error = %v", err)
			}

			if tt.checkFunc != nil {
				if err := tt.checkFunc(client.tlsConfig); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func createConfigureTLSTests() []struct {
	name      string
	tlsConfig common.TLSConfig
	checkFunc func(*tls.Config) error
} {
	return []struct {
		name      string
		tlsConfig common.TLSConfig
		checkFunc func(*tls.Config) error
	}{
		{
			name: "Default TLS config",
			tlsConfig: common.TLSConfig{
				Verify: true,
			},
			checkFunc: func(cfg *tls.Config) error {
				if cfg.InsecureSkipVerify {
					return errors.New("InsecureSkipVerify should be false")
				}
				if cfg.MinVersion != tls.VersionTLS13 {
					return errors.New("MinVersion should be TLS 1.3")
				}

				return nil
			},
		},
		{
			name: "TLS 1.2 with skip verify",
			tlsConfig: common.TLSConfig{
				Verify:     false,
				MinVersion: "1.2",
			},
			checkFunc: func(cfg *tls.Config) error {
				if !cfg.InsecureSkipVerify {
					return errors.New("InsecureSkipVerify should be true")
				}
				if cfg.MinVersion != tls.VersionTLS12 {
					return errors.New("MinVersion should be TLS 1.2")
				}

				return nil
			},
		},
		{
			name: "With CA cert and cipher suites",
			tlsConfig: common.TLSConfig{
				Verify:       true,
				MinVersion:   "1.3",
				CAFile:       "", // Will be set to temp file path in test
				CipherSuites: []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
			},
			checkFunc: func(cfg *tls.Config) error {
				// Just verify it was configured without error.
				return nil
			},
		},
	}
}

func prepareTLSConfigForTest(t *testing.T, tlsConfig common.TLSConfig, testName string) common.TLSConfig {
	t.Helper()

	if testName == "With CA cert and cipher suites" {
		caCert := getTestCACertContent()
		caFile, cleanup := testutil.TempFile(t, caCert)
		t.Cleanup(cleanup)

		tlsConfig.CAFile = caFile
	}

	return tlsConfig
}

func getTestCACertContent() string {
	return `-----BEGIN CERTIFICATE-----
MIIDhTCCAm2gAwIBAgIUIFCgv8sjzj7TYRNCiWM9r3JbiW4wDQYJKoZIhvcNAQEL
BQAwUjELMAkGA1UEBhMCVVMxDTALBgNVBAgMBFRlc3QxDTALBgNVBAcMBFRlc3Qx
DTALBgNVBAoMBFRlc3QxFjAUBgNVBAMMDXRlc3QtY2EubG9jYWwwHhcNMjUwODAz
MjM1NzE5WhcNMjYwODAzMjM1NzE5WjBSMQswCQYDVQQGEwJVUzENMAsGA1UECAwE
VGVzdDENMAsGA1UEBwwEVGVzdDENMAsGA1UECgwEVGVzdDEWMBQGA1UEAwwNdGVz
dC1jYS5sb2NhbDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANm1eqUn
NVYdlSgNB8a4gZVsDnhz8o+gQQK/SyGnyqHmQ43jsqhAUsn5azo6fRbBexjd5KsU
+GrYwHOr3fBR7Cb1x3lWCzag/tKKcXFdeLK6en3ebEr+HGsrLUCOFojY3vJc/iMu
qz/h7ZOQ2t8Z1VEP0NnBNeVxkIhA6sMEaO47jnwIwojVb/oCd8HW2HvnmIwTCzlM
TtG9oqpEuaVV5+C+g05oBLURCMr+KcpCqGtmsl4BLSdsYkyqsnJHJDCEPcJxgwmF
mYiymwtnqL2x+GRr5A989hT+tiz50N04pKuYezOzK4iszYJfTNMKB650SE14Kedc
WtF7QizY7Bq+IOkCAwEAAaNTMFEwHQYDVR0OBBYEFCzN5S6ugjuGZiwzc6DNsYVK
IJeaMB8GA1UdIwQYMBaAFCzN5S6ugjuGZiwzc6DNsYVKIJeaMA8GA1UdEwEB/wQF
MAMBAf8wDQYJKoZIhvcNAQELBQADggEBAEct9y8mNmhZU9lp4oC9dNfA8bEaBew5
Dw/FLDH+WFMkJ0bbTjagaykogor5aBur1Bnv2dqUC/t44E8cx5Cj5zHipiB+lzpb
NKf8pI6/Yv2Bq24OtldTLXsiMqWrOJljm35VGN2RjX0snvs0YMgEkr/AwlM84kEt
R9mz6Xt0/QzvkHqj8HOdEQcIqdadPF/xQiTbm2ZM4DXzKUR6UTKrib4DmCX7RH49
Pw2PcgCtWHGdANdL7+nuk4KBo43oeHoxtQefjyw9DV+yOSdEXL6AFOSXIU6q9f06

PIHoJdYUmhCwEjxX4LniJH2cIHTW5tfdKTm/e+8qsvj/CbEmgjAv5RE=
-----END CERTIFICATE-----`
}

func TestClient_Connect(t *testing.T) {
	tests := createConnectTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler(t))
			defer server.Close()

			client := createConnectTestClient(t, server.URL, tt)

			ctx := context.Background()
			err := client.Connect(ctx)

			validateConnectResult(t, err, client, tt)
		})
	}
}

func createConnectTests() []struct {
	name          string
	authType      string
	authToken     string
	serverHandler func(*testing.T) http.HandlerFunc
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		authType      string
		authToken     string
		serverHandler func(*testing.T) http.HandlerFunc
		wantError     bool
		errorContains string
	}{
		{
			name:          "Successful bearer auth connection",
			authType:      "bearer",
			authToken:     "test-token",
			serverHandler: createSuccessfulBearerHandler,
			wantError:     false,
		},
		{
			name:          "Missing bearer token",
			authType:      "bearer",
			serverHandler: createErrorHandler,
			wantError:     true,
			errorContains: "bearer token not configured",
		},
		{
			name:          "OAuth2 auth (not implemented)",
			authType:      "oauth2",
			serverHandler: createErrorHandler,
			wantError:     true,
			errorContains: "OAuth2 token endpoint not configured",
		},
		{
			name:          "mTLS auth",
			authType:      "mtls",
			serverHandler: createMTLSHandler,
			wantError:     false,
		},
		{
			name:          "Server rejects connection",
			authType:      "bearer",
			authToken:     "test-token",
			serverHandler: createRejectionHandler,
			wantError:     true,
			errorContains: "failed to connect",
		},
	}
}

func createSuccessfulBearerHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Expected 'Bearer test-token', got '%s'", auth)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)

			return
		}

		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Failed to upgrade: %v", err)

			return
		}

		defer func() { _ = conn.Close() }()

		time.Sleep(testIterations * time.Millisecond)
	}
}

func createErrorHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Should not reach here", http.StatusInternalServerError)
	}
}

func createMTLSHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		_ = conn.Close()
	}
}

func createRejectionHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}
}

func createConnectTestClient(t *testing.T, serverURL string, tt struct {
	name          string
	authType      string
	authToken     string
	serverHandler func(*testing.T) http.HandlerFunc
	wantError     bool
	errorContains string
}) *Client {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	logger := testutil.NewTestLogger(t)

	authConfig := common.AuthConfig{
		Type:  tt.authType,
		Token: tt.authToken,
	}

	if tt.authType == "mtls" {
		certFile, keyFile := createMTLSCertificates(t)
		authConfig.ClientCert = certFile
		authConfig.ClientKey = keyFile
	}

	return &Client{
		config: config.GatewayConfig{
			URL:  wsURL,
			Auth: authConfig,
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}
}

func createMTLSCertificates(t *testing.T) (string, string) {
	t.Helper()

	// Use the gateway testutil to create proper certificates
	tempDir := t.TempDir()
	certFile, keyFile, _ := gatewayTestutil.CreateTestCertificates(t, tempDir)

	return certFile, keyFile
}

func validateConnectResult(t *testing.T, err error, client *Client, tt struct {
	name          string
	authType      string
	authToken     string
	serverHandler func(*testing.T) http.HandlerFunc
	wantError     bool
	errorContains string
}) {
	t.Helper()

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")

			return
		}
		if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
		}

		return
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)

		return
	}

	if client.conn == nil {
		t.Error("Expected connection to be established")

		return
	}

	_ = client.conn.Close()
}

// createMessageEchoServer creates a server that echoes back messages with validation.
func createMessageEchoServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Failed to upgrade: %v", err)

			return
		}

		defer func() { _ = conn.Close() }()

		for {
			var msg WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}

			if msg.Source != "local-router" {
				t.Errorf("Expected source 'local-router', got '%s'", msg.Source)
			}

			if msg.Timestamp == "" {
				t.Error("Expected timestamp to be set")
			}

			if err := conn.WriteJSON(msg); err != nil {
				break
			}
		}
	}))
}

func TestClient_SendRequest(t *testing.T) {
	ctx := context.Background()
	server := createMessageEchoServer(t)
	defer server.Close()

	client := createTestGatewayClient(t, server.URL)
	connectTestClient(t, client)
	defer closeTestClient(t, client)

	tests := getSendRequestTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifySendRequest(t, ctx, client, tt)
		})
	}
}

func createTestGatewayClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	logger := testutil.NewTestLogger(t)

	return &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}
}

func connectTestClient(t *testing.T, client *Client) {
	t.Helper()
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
}

func closeTestClient(t *testing.T, client *Client) {
	t.Helper()
	if err := client.Close(); err != nil {
		t.Logf("Failed to close client in cleanup: %v", err)
	}
}

func getSendRequestTestCases() []struct {
	name          string
	request       *mcp.Request
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		request       *mcp.Request
		wantError     bool
		errorContains string
	}{
		{
			name: "Send simple request",
			request: &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "initialize",
				ID:      "test-123",
			},
			wantError: false,
		},
		{
			name: "Send tool call request",
			request: &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "k8s.getPods",
				Params: map[string]string{
					"namespace": "default",
				},
				ID: 42,
			},
			wantError: false,
		},
	}
}

func verifySendRequest(t *testing.T, ctx context.Context, client *Client, tt struct {
	name          string
	request       *mcp.Request
	wantError     bool
	errorContains string
}) {
	t.Helper()
	err := client.SendRequest(ctx, tt.request)

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")
		} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
		}
	} else {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	}
}

func TestClient_SendRequest_NotConnected(t *testing.T) {
	ctx := context.Background()
	logger := testutil.NewTestLogger(t)
	client := &Client{
		logger: logger,
	}

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      1,
	}

	err := client.SendRequest(ctx, req)
	if err == nil {
		t.Error("Expected error when not connected")
	}

	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("Expected 'not connected' error, got: %v", err)
	}
}

// createTestMessageServer creates a server that sends predefined test messages.
func createTestMessageServer(t *testing.T, messages []WireMessage) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		// Send test messages.
		for _, msg := range messages {
			if err := conn.WriteJSON(msg); err != nil {
				break
			}

			time.Sleep(testTimeout * time.Millisecond)
		}

		// Keep connection open.
		time.Sleep(httpStatusOK * time.Millisecond)
	}))
}

// validateSuccessResponse validates a successful response.
func validateSuccessResponse(t *testing.T, resp *mcp.Response, expectedID interface{}) {
	t.Helper()

	if resp.ID != expectedID {
		t.Errorf("Expected ID %v, got %v", expectedID, resp.ID)
	}

	if resp.Error != nil {
		t.Error("Expected no error in response")
	}
}

// validateErrorResponse validates an error response.
func validateErrorResponse(t *testing.T, resp *mcp.Response, expectedID interface{}, expectedCode int) {
	t.Helper()

	if resp.ID != expectedID {
		t.Errorf("Expected ID %v, got %v", expectedID, resp.ID)
	}

	if resp.Error == nil {
		t.Error("Expected error in response")

		return
	}

	if resp.Error.Code != expectedCode {
		t.Errorf("Expected error code %d, got %d", expectedCode, resp.Error.Code)
	}
}

func TestClient_ReceiveResponse(t *testing.T) {
	testMessages := createTestResponseMessages()
	server := createTestMessageServer(t, testMessages)

	client := createTestGatewayClient(t, server.URL)
	connectTestClient(t, client)
	defer closeTestClient(t, client)

	verifyReceiveResponses(t, client)
}

func createTestResponseMessages() []WireMessage {
	return []WireMessage{
		{
			ID:        "test-123",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Source:    "gateway",
			MCPPayload: map[string]interface{}{
				"jsonrpc": constants.TestJSONRPCVersion,
				"result": map[string]string{
					"status": "ok",
				},
				"id": "test-123",
			},
		},
		{
			ID:        42,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Source:    "gateway",
			MCPPayload: map[string]interface{}{
				"jsonrpc": constants.TestJSONRPCVersion,
				"error": map[string]interface{}{
					"code":    -32600,
					"message": "Invalid request",
				},
				"id": 42,
			},
		},
	}
}

func verifyReceiveResponses(t *testing.T, client *Client) {
	t.Helper()

	// Receive first response (success)
	resp1, err := client.ReceiveResponse()
	if err != nil {
		t.Fatalf("Failed to receive first response: %v", err)
	}
	validateSuccessResponse(t, resp1, "test-123")

	// Receive second response (error)
	resp2, err := client.ReceiveResponse()
	if err != nil {
		t.Fatalf("Failed to receive second response: %v", err)
	}
	validateErrorResponse(t, resp2, float64(42), -32600) // JSON unmarshals numbers as float64
}

func TestClient_ReceiveResponse_InvalidPayload(t *testing.T) {
	server := createInvalidPayloadServer(t)
	defer server.Close()

	client := createTestGatewayClient(t, server.URL)
	connectTestClient(t, client)
	defer closeTestClient(t, client)

	verifyInvalidPayloadError(t, client)
}

func createInvalidPayloadServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Send message without MCP payload
		msg := WireMessage{
			ID:        "test-123",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Source:    "gateway",
			// MCPPayload is nil
		}
		if err := conn.WriteJSON(msg); err != nil {
			t.Logf("Failed to write JSON response: %v", err)
		}
		time.Sleep(testIterations * time.Millisecond)
	}))
}

func verifyInvalidPayloadError(t *testing.T, client *Client) {
	t.Helper()
	_, err := client.ReceiveResponse()
	if err == nil {
		t.Error("Expected error for message without MCP payload")

		return

	}
	if !strings.Contains(err.Error(), "without MCP payload") {
		t.Errorf("Expected 'without MCP payload' error, got: %v", err)
	}
}

func TestClient_SendPing(t *testing.T) {
	pingReceived := make(chan bool, 1)
	server := createPingHandlerServer(t, pingReceived)
	defer server.Close()

	client := createTestGatewayClient(t, server.URL)
	connectTestClient(t, client)
	defer closeTestClient(t, client)

	verifyPingPong(t, client, pingReceived)
}

func createPingHandlerServer(t *testing.T, pingReceived chan<- bool) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		conn.SetPingHandler(func(appData string) error {
			pingReceived <- true

			return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
		})

		// Read messages to handle ping
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}))
}

func verifyPingPong(t *testing.T, client *Client, pingReceived <-chan bool) {
	t.Helper()

	// Send ping
	err := client.SendPing()
	if err != nil {
		t.Errorf("Failed to send ping: %v", err)
	}

	// Verify ping was received
	select {
	case <-pingReceived:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for ping")
	}
}

func TestClient_Close(t *testing.T) {
	closeReceived := make(chan bool, 1)
	server := createCloseHandlerServer(t, closeReceived)
	defer server.Close()

	client := createTestGatewayClient(t, server.URL)
	// Test closing when not connected
	verifyCloseWhenDisconnected(t, client)

	// Connect and test normal close
	connectTestClient(t, client)
	verifyCloseWhenConnected(t, client, closeReceived)
}

func createCloseHandlerServer(t *testing.T, closeReceived chan<- bool) *httptest.Server {

	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		conn.SetCloseHandler(func(code int, text string) error {
			closeReceived <- true

			return nil
		})

		// Read messages
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}))
}

func verifyCloseWhenDisconnected(t *testing.T, client *Client) {
	t.Helper()
	err := client.Close()
	if err != nil {
		t.Errorf("Close should not error when not connected: %v", err)
	}
}

func verifyCloseWhenConnected(t *testing.T, client *Client, closeReceived <-chan bool) {
	t.Helper()

	// Close connection
	err := client.Close()
	if err != nil {
		t.Errorf("Failed to close: %v", err)
	}

	// Verify close was received
	select {
	case <-closeReceived:
		// Success
	case <-time.After(1 * time.Second):
		// Close message is best effort, so timeout is ok
	}

	// Verify connection is nil
	if client.IsConnected() {
		t.Error("Expected IsConnected to return false after close")
	}
}

func TestClient_IsConnected(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	client := &Client{
		logger: logger,
	}

	// Initially not connected.
	if client.IsConnected() {
		t.Error("Expected IsConnected to return false initially")
	}

	// Set a mock connection.
	client.conn = &websocket.Conn{}
	if !client.IsConnected() {
		t.Error("Expected IsConnected to return true with connection")
	}

	// Clear connection.
	client.conn = nil
	if client.IsConnected() {
		t.Error("Expected IsConnected to return false after clearing connection")
	}
}

func TestExtractNamespace(t *testing.T) {
	// Create a client with default namespace
	client := &Client{defaultNamespace: "default"}

	tests := []struct {
		method   string
		expected string
	}{
		{"initialize", "system"},
		{"tools/list", "system"},
		{"tools/call", "system"},
		{"ping", "system"},
		{"docker.listContainers", "docker"},
		{"custom.namespace.method", "custom"},
		{"simplemethod", "default"},
		{"", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			result := client.extractNamespace(tt.method)
			if result != tt.expected {
				t.Errorf("extractNamespace(%s) = %s, want %s", tt.method, result, tt.expected)
			}
		})
	}
}

func TestClient_ConcurrentOperations(t *testing.T) {
	// ctx := context.Background() - unused
	server := createEchoServer(t)
	defer server.Close()

	client := createTestGatewayClient(t, server.URL)
	connectTestClient(t, client)
	defer closeTestClient(t, client)

	runConcurrentSends(t, client)
}

func createEchoServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Echo messages back
		for {
			var msg WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}
			if err := conn.WriteJSON(msg); err != nil {
				break
			}
		}
	}))
}

func runConcurrentSends(t *testing.T, client *Client) {
	t.Helper()

	var wg sync.WaitGroup
	errors := make(chan error, constants.TestBatchSize)

	for i := 0; i < constants.TestBatchSize; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "test.method",
				ID:      id,
			}
			ctx := context.Background()
			if err := client.SendRequest(ctx, req); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent send error: %v", err)
	}
}

func TestWireMessage_JSONMarshaling(t *testing.T) {
	// ctx := context.Background() - unused
	msg := WireMessage{
		ID:              "test-123",
		Timestamp:       "2024-01-01T00:00:00Z",
		Source:          "test-source",
		TargetNamespace: "test-namespace",
		MCPPayload: map[string]interface{}{
			"jsonrpc": constants.TestJSONRPCVersion,
			"method":  "test",
			"id":      "test-123",
		},
	}

	// Marshal.
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal.
	var decoded WireMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify fields.
	if decoded.ID != msg.ID {
		t.Errorf("ID mismatch: expected %v, got %v", msg.ID, decoded.ID)
	}

	if decoded.Timestamp != msg.Timestamp {
		t.Errorf("Timestamp mismatch")
	}

	if decoded.Source != msg.Source {
		t.Errorf("Source mismatch")
	}

	if decoded.TargetNamespace != msg.TargetNamespace {
		t.Errorf("TargetNamespace mismatch")
	}
}

func BenchmarkClient_SendRequest(b *testing.B) {
	server := createNullServer(b)
	defer server.Close()

	client := createBenchmarkClient(b, server.URL)
	connectBenchmarkClient(b, client)
	defer closeBenchmarkClient(b, client)

	runSendRequestBenchmark(b, client)
}

func createNullServer(b *testing.B) *httptest.Server {
	b.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Just read and discard
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}))
}

func createBenchmarkClient(b *testing.B, serverURL string) *Client {
	b.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	logger := zaptest.NewLogger(b)

	return &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}
}

func connectBenchmarkClient(b *testing.B, client *Client) {
	b.Helper()
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
}

func closeBenchmarkClient(b *testing.B, client *Client) {
	b.Helper()
	if err := client.Close(); err != nil {
		b.Logf("Failed to close client in cleanup: %v", err)
	}
}

func runSendRequestBenchmark(b *testing.B, client *Client) {
	b.Helper()
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "benchmark.test",
		Params: map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		},
		ID: 1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		if err := client.SendRequest(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}

// Enhanced tests for better coverage.

// Test various error scenarios during connection.
func TestClient_Connect_ErrorScenarios(t *testing.T) {
	// ctx := context.Background() - unused
	tests := getConnectErrorScenarios(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupClient()
			verifyConnectError(t, client, tt)
		})
	}
}

func getConnectErrorScenarios(t *testing.T) []struct {
	name          string
	setupClient   func() *Client
	wantError     bool
	errorContains string
} {
	t.Helper()

	return []struct {
		name          string
		setupClient   func() *Client
		wantError     bool
		errorContains string
	}{
		{
			name:          "Invalid URL",
			setupClient:   createInvalidURLClient(t),
			wantError:     true,
			errorContains: "invalid gateway URL",
		},
		{
			name:          "Connection timeout",
			setupClient:   createTimeoutClient(t),
			wantError:     true,
			errorContains: "failed to connect",
		},
		{
			name:          "mTLS with missing certificate",
			setupClient:   createMTLSMissingCertClient(t),
			wantError:     true,
			errorContains: "client certificate and key are required",
		},
	}
}

func createInvalidURLClient(t *testing.T) func() *Client {
	t.Helper()

	return func() *Client {
		logger := testutil.NewTestLogger(t)

		return &Client{
			config: config.GatewayConfig{
				URL: "://invalid-url",
				Auth: common.AuthConfig{
					Type:  "bearer",
					Token: "test-token",
				},
			},
			logger:    logger,
			tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
		}
	}
}

func createTimeoutClient(t *testing.T) func() *Client {
	t.Helper()

	return func() *Client {
		logger := testutil.NewTestLogger(t)

		return &Client{
			config: config.GatewayConfig{
				URL: "ws://nonexistent-host-12345.invalid:9999",
				Auth: common.AuthConfig{
					Type:  "bearer",
					Token: "test-token",
				},
				Connection: common.ConnectionConfig{
					TimeoutMs: testIterations, // Very short timeout
				},
			},
			logger:    logger,
			tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
		}
	}
}

func createMTLSMissingCertClient(t *testing.T) func() *Client {
	t.Helper()

	return func() *Client {
		logger := testutil.NewTestLogger(t)

		return &Client{
			config: config.GatewayConfig{
				URL: "wss://example.com",
				Auth: common.AuthConfig{
					Type: "mtls",
					// Missing ClientCert and ClientKey
				},
			},
			logger:    logger,
			tlsConfig: &tls.Config{}, // #nosec G402 - test configuration only
		}
	}
}

func verifyConnectError(t *testing.T, client *Client, tt struct {
	name          string
	setupClient   func() *Client
	wantError     bool
	errorContains string
}) {
	t.Helper()
	ctx := context.Background()
	err := client.Connect(ctx)

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")

			return
		}
		if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
		}

		return
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)

		return
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()
}

// Test network failure scenarios during operation.
func TestClient_NetworkFailures(t *testing.T) {
	// ctx := context.Background() - unused
	connectChan := make(chan struct{})
	server := createDropConnectionServer(t, connectChan)
	defer server.Close()

	client := createTestGatewayClient(t, server.URL)
	connectTestClient(t, client)
	defer closeTestClient(t, client)

	verifyNetworkFailureHandling(t, client, connectChan)
}

func createDropConnectionServer(t *testing.T, connectChan chan<- struct{}) *httptest.Server {

	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// Signal that connection was established
		close(connectChan)

		// Read one message then close abruptly
		_, _, _ = conn.ReadMessage()
		_ = conn.Close()
	}))
}

func verifyNetworkFailureHandling(t *testing.T, client *Client, connectChan <-chan struct{}) {
	t.Helper()

	// Wait for connection to be established
	<-connectChan

	// Try to send request - should cause connection to close
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-1",
	}
	ctx := context.Background()
	_ = client.SendRequest(ctx, req)
	// May or may not error immediately depending on timing

	// Try to receive response - should definitely error
	_, err := client.ReceiveResponse()
	if err == nil {
		t.Error("Expected error when receiving from closed connection")
	}

	if !strings.Contains(err.Error(), "connection") && !strings.Contains(err.Error(), "closed") {
		t.Errorf("Expected connection/closed error, got: %v", err)
	}
}

// Test authentication edge cases.
func TestClient_AuthenticationEdgeCases(t *testing.T) {
	// ctx := context.Background() - unused
	tests := getAuthenticationEdgeCaseTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runAuthenticationTest(t, tt)
		})
	}
}

func getAuthenticationEdgeCaseTests() []struct {
	name           string
	authConfig     common.AuthConfig
	expectAuthCall bool
	serverHandler  func(*testing.T, bool) http.HandlerFunc
	wantError      bool
	errorContains  string
} {
	return []struct {
		name           string
		authConfig     common.AuthConfig
		expectAuthCall bool
		serverHandler  func(*testing.T, bool) http.HandlerFunc
		wantError      bool
		errorContains  string
	}{
		{
			name:           "Bearer auth with empty token",
			authConfig:     common.AuthConfig{Type: "bearer", Token: ""},
			expectAuthCall: false,
			serverHandler:  createErrorAuthHandler,
			wantError:      true,
			errorContains:  "bearer token not configured",
		},
		{
			name: "OAuth2 with missing endpoint",
			authConfig: common.AuthConfig{
				Type:         "oauth2",
				ClientID:     "test-client",
				ClientSecret: "test-secret",
			},
			expectAuthCall: false,
			serverHandler:  createErrorAuthHandler,
			wantError:      true,
			errorContains:  "OAuth2 token endpoint not configured",
		},
		{
			name:           "Unknown auth type",
			authConfig:     common.AuthConfig{Type: "unknown"},
			expectAuthCall: false,
			serverHandler:  createNoAuthHandler,
			wantError:      false,
		},
	}
}

func createErrorAuthHandler(t *testing.T, expectAuth bool) http.HandlerFunc {

	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Should not reach here", http.StatusInternalServerError)
	}
}

func createNoAuthHandler(t *testing.T, expectAuth bool) http.HandlerFunc {

	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		// Check that no auth header is set
		auth := r.Header.Get("Authorization")
		if auth != "" {
			t.Errorf("Expected no auth header, got '%s'", auth)
		}

		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		time.Sleep(testIterations * time.Millisecond)
	}
}

func runAuthenticationTest(t *testing.T, tt struct {
	name           string
	authConfig     common.AuthConfig
	expectAuthCall bool
	serverHandler  func(*testing.T, bool) http.HandlerFunc
	wantError      bool
	errorContains  string
}) {
	t.Helper()

	server := httptest.NewServer(tt.serverHandler(t, tt.expectAuthCall))
	defer server.Close()

	client := createAuthTestClient(t, server.URL, tt.authConfig)
	ctx := context.Background()
	err := client.Connect(ctx)

	verifyAuthResult(t, err, tt.wantError, tt.errorContains, client)
}

func createAuthTestClient(t *testing.T, serverURL string, authConfig common.AuthConfig) *Client {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	logger := testutil.NewTestLogger(t)

	return &Client{
		config: config.GatewayConfig{
			URL:  wsURL,
			Auth: authConfig,
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}
}

func verifyAuthResult(t *testing.T, err error, wantError bool, errorContains string, client *Client) {
	t.Helper()

	if wantError {
		if err == nil {

			t.Error("Expected error but got none")

			return
		}
		if errorContains != "" && !strings.Contains(err.Error(), errorContains) {

			t.Errorf("Expected error containing '%s', got '%s'", errorContains, err.Error())

		}

		return
	}

	if err != nil {

		t.Errorf("Unexpected error: %v", err)

		return
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()
}

// Test concurrent operations with potential race conditions.
// createSlowEchoServer creates a server that echoes messages with a delay.

func createSlowEchoServer(t *testing.T) *httptest.Server {

	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		for {
			var msg WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}

			// Simulate slow processing.
			time.Sleep(constants.TestTickInterval)

			if err := conn.WriteJSON(msg); err != nil {
				break
			}
		}
	}))
}

// runConcurrentSenders starts multiple concurrent senders and returns error channel.
func runConcurrentSenders(t *testing.T, client *Client, numOperations int) (chan error, *sync.WaitGroup) {
	t.Helper()

	sendErrors := make(chan error, numOperations)

	var sendWg sync.WaitGroup

	for i := 0; i < numOperations; i++ {
		sendWg.Add(1)

		go func(id int) {
			defer sendWg.Done()

			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "concurrent.test",
				ID:      id,
			}
			ctx := context.Background()
			if err := client.SendRequest(ctx, req); err != nil {
				sendErrors <- err
			}
		}(i)
	}

	return sendErrors, &sendWg
}

// runConcurrentReceivers starts multiple concurrent receivers and returns error channel.
func runConcurrentReceivers(t *testing.T, client *Client, numOperations int) (chan error, *sync.WaitGroup) {
	t.Helper()

	recvErrors := make(chan error, numOperations)

	var recvWg sync.WaitGroup

	for i := 0; i < numOperations; i++ {
		recvWg.Add(1)

		go func() {
			defer recvWg.Done()

			if _, err := client.ReceiveResponse(); err != nil {
				recvErrors <- err
			}
		}()
	}

	return recvErrors, &recvWg
}

// validateConcurrentErrors validates that error counts are within acceptable limits.
func validateConcurrentErrors(t *testing.T, sendErrors, recvErrors chan error, numOperations int) {
	t.Helper()

	// Check for unexpected errors (some timeout errors may be expected).
	sendErrorCount := 0
	for err := range sendErrors {
		sendErrorCount++
		// Log but don't fail on send errors - they might be timing related.
		t.Logf("Send error (may be expected): %v", err)
	}

	recvErrorCount := 0
	for err := range recvErrors {
		recvErrorCount++
		// Log but don't fail on receive errors - they might be timing related.
		t.Logf("Receive error (may be expected): %v", err)
	}

	// Allow some errors due to timing issues, but not too many.
	if sendErrorCount > numOperations/2 {
		t.Errorf("Too many send errors: %d/%d", sendErrorCount, numOperations)
	}

	if recvErrorCount > numOperations/2 {
		t.Errorf("Too many receive errors: %d/%d", recvErrorCount, numOperations)
	}
}

func TestClient_ConcurrentOperations_EdgeCases(t *testing.T) {
	ctx := context.Background()
	server := createSlowEchoServer(t)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	const numOperations = testTimeout

	sendErrors, sendWg := runConcurrentSenders(t, client, numOperations)
	recvErrors, recvWg := runConcurrentReceivers(t, client, numOperations)

	sendWg.Wait()
	recvWg.Wait()

	close(sendErrors)
	close(recvErrors)

	validateConcurrentErrors(t, sendErrors, recvErrors, numOperations)
}

// Test connection lifecycle edge cases.
func TestClient_ConnectionLifecycle(t *testing.T) {
	// ctx := context.Background() - unused
	server := createLifecycleTestServer(t)
	defer server.Close()

	client := createLifecycleTestClient(t, server.URL)
	testConnectionLifecycles(t, client)
}

func createLifecycleTestServer(t *testing.T) *httptest.Server {

	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Handle one message then close.
		_, _, _ = conn.ReadMessage()
		time.Sleep(testTimeout * time.Millisecond)
	}))
}

func createLifecycleTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")

	logger := testutil.NewTestLogger(t)

	return &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}
}

func testConnectionLifecycles(t *testing.T, client *Client) {
	t.Helper()
	ctx := context.Background()

	// Test multiple connect/disconnect cycles.
	for i := 0; i < 3; i++ {
		testSingleLifecycle(t, client, ctx, i)
		time.Sleep(constants.TestTickInterval)
	}
}

func testSingleLifecycle(t *testing.T, client *Client, ctx context.Context, iteration int) {
	t.Helper()
	// Connect.
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect on iteration %d: %v", iteration, err)
	}

	if !client.IsConnected() {
		t.Errorf("Expected IsConnected to be true after connect on iteration %d", iteration)
	}

	// Send a message to trigger server close.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      iteration,
	}
	_ = client.SendRequest(ctx, req) // May fail, that's okay

	// Close.
	if err := client.Close(); err != nil {
		t.Errorf("Failed to close on iteration %d: %v", iteration, err)
	}

	if client.IsConnected() {
		t.Errorf("Expected IsConnected to be false after close on iteration %d", iteration)
	}
}

// Test malformed message handling.
func TestClient_MalformedMessages(t *testing.T) {
	// ctx := context.Background() - unused
	malformedMessages := getMalformedMessages()

	for i, msg := range malformedMessages {
		t.Run(fmt.Sprintf("malformed_message_%d", i), func(t *testing.T) {
			testSingleMalformedMessage(t, msg)
		})
	}
}

func getMalformedMessages() []string {
	return []string{
		`{"id": "test", "timestamp": "2024-01-01T00:00:00Z"}`, // Missing MCP payload
		`{"invalid": "json"`, // Invalid JSON
		`""`,                 // Empty string
		`null`,               // Null value
	}
}

func testSingleMalformedMessage(t *testing.T, msg string) {
	t.Helper()
	server := createMalformedMessageServer(t, msg)
	defer server.Close()

	client := createMalformedMessageClient(t, server.URL)
	testMalformedMessageReceive(t, client)
}

func createMalformedMessageServer(t *testing.T, msg string) *httptest.Server {

	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Send malformed message.
		if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			t.Logf("Failed to write message: %v", err)
		}
		time.Sleep(testIterations * time.Millisecond)
	}))
}

func createMalformedMessageClient(t *testing.T, serverURL string) *Client {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")

	logger := testutil.NewTestLogger(t)

	return &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}
}

func testMalformedMessageReceive(t *testing.T, client *Client) {
	t.Helper()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	// Try to receive the malformed message.
	_, err := client.ReceiveResponse()
	if err == nil {
		t.Error("Expected error when receiving malformed message")
	}
}

// Test error handling during WebSocket operations.
func TestClient_WebSocketErrorHandling(t *testing.T) {
	ctx := context.Background()
	logger := testutil.NewTestLogger(t)
	client := &Client{
		logger: logger,
	}

	// Test operations on disconnected client.
	tests := []struct {
		name      string
		operation func() error
	}{
		{
			name: "SendRequest when disconnected",
			operation: func() error {
				req := &mcp.Request{JSONRPC: constants.TestJSONRPCVersion, Method: "test", ID: 1}

				return client.SendRequest(ctx, req)
			},
		},
		{
			name: "ReceiveResponse when disconnected",
			operation: func() error {
				_, err := client.ReceiveResponse()

				return err
			},
		},
		{
			name: "SendPing when disconnected",
			operation: func() error {
				return client.SendPing()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.operation()
			if err == nil {
				t.Error("Expected error when operation called on disconnected client")
			}

			if !strings.Contains(err.Error(), "not connected") {
				t.Errorf("Expected 'not connected' error, got: %v", err)
			}
		})
	}
}
