// Package gateway provides WebSocket and TCP client implementations for connecting.
// to MCP (Model Context Protocol) gateway servers with enterprise-grade features.
//
// This package implements high-availability patterns including:
// - Connection pooling and load balancing across multiple gateway endpoints
// - Circuit breaker pattern for fault tolerance and failure isolation
// - Comprehensive authentication support (Bearer, OAuth2, mTLS)
// - Deadlock-free concurrent WebSocket operations with proper mutex ordering
// - Automatic reconnection with exponential backoff and jitter
//
// The main types are:
//
// Client: WebSocket client with full MCP protocol support.
// TCPClient: TCP client with binary protocol support.
// PoolClient: High-level client that manages multiple endpoints.
// GatewayPool: Load balancer and health monitor for gateway endpoints.
//
// Thread Safety:
// All client types are designed for safe concurrent use. The WebSocket client
// uses a carefully designed mutex ordering to prevent deadlocks during
// simultaneous send/receive operations.
package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

const (
	// NamespaceDefault represents the default namespace.
	NamespaceDefault = "default"
)

// Client manages the WebSocket connection to the gateway.
type Client struct {
	config config.GatewayConfig
	logger *zap.Logger

	// WebSocket connection.
	conn   *websocket.Conn
	connMu sync.Mutex

	// Message handling.
	writeMu sync.Mutex
	readMu  sync.Mutex

	// TLS configuration.
	tlsConfig *tls.Config

	// HTTP client for OAuth2.
	httpClient *http.Client
}

// NewClient creates a new gateway client.
func NewClient(cfg config.GatewayConfig, logger *zap.Logger) (*Client, error) {
	c := &Client{
		config: cfg,
		logger: logger,
	}

	// Configure TLS.
	if err := c.configureTLS(); err != nil {
		return nil, fmt.Errorf("failed to configure TLS: %w", err)
	}

	// Configure HTTP client for OAuth2.
	c.httpClient = &http.Client{
		Timeout: defaultTimeoutSeconds * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     c.tlsConfig,
			DisableKeepAlives:   true, // Disable connection reuse for tests
			MaxIdleConnsPerHost: 0,    // No idle connections
		},
	}

	return c, nil
}

// configureTLS sets up the TLS configuration.
func (c *Client) configureTLS() error {
	builder := InitializeTLSConfiguration(c)

	return builder.BuildTLSConfiguration()
}

// Connect establishes the WebSocket connection to the gateway.
func (c *Client) Connect(ctx context.Context) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	builder := InitializeConnectionBuilder(c, ctx)

	conn, err := builder.EstablishConnection()
	if err != nil {
		return err
	}

	c.conn = conn
	c.logger.Info("Successfully connected to gateway")

	return nil
}

// configureMTLS configures mutual TLS.
func (c *Client) configureMTLS() error {
	if c.config.Auth.ClientCert == "" || c.config.Auth.ClientKey == "" {
		return errors.New("client certificate and key are required for mTLS")
	}

	// Load client certificate and key.
	cert, err := tls.LoadX509KeyPair(c.config.Auth.ClientCert, c.config.Auth.ClientKey)
	if err != nil {
		return fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Set the certificate in TLS config.
	c.tlsConfig.Certificates = []tls.Certificate{cert}

	c.logger.Info("mTLS configured successfully",
		zap.String("cert", c.config.Auth.ClientCert),
		zap.String("key", c.config.Auth.ClientKey),
	)

	return nil
}

// getOAuth2Token retrieves an OAuth2 token.
func (c *Client) getOAuth2Token(ctx context.Context) (string, error) {
	oauth2Client := NewOAuth2Client(c.config, c.logger, c.httpClient)

	return oauth2Client.GetToken(ctx)
}

// SendRequest sends an MCP request to the gateway with proper concurrency control.
//
// This method implements a deadlock-free approach by:
// 1. Briefly checking connection state without holding locks during I/O
// 2. Using separate mutexes for connection state and write operations
// 3. Double-checking connection validity before actual WebSocket write
//
// Parameters:
//   - req: The MCP request to send, must have a valid ID and Method
//
// Returns:
//   - error: nil on success, error describing failure mode otherwise
//
// Thread Safety: Safe for concurrent use. Multiple goroutines can call this
// method simultaneously without risk of deadlock or data races.
func (c *Client) SendRequest(ctx context.Context, req *mcp.Request) error {
	startTime := time.Now()

	// Check connection state with minimal lock time.
	c.connMu.Lock()
	conn := c.conn
	c.connMu.Unlock()

	if conn == nil {
		return errors.New("not connected")
	}

	c.logger.Debug("Attempting to acquire write mutex",
		zap.Any("request_id", req.ID),
		zap.String("method", req.Method))

	// Hold write mutex for the actual write operation.
	c.writeMu.Lock()
	mutexAcquiredAt := time.Now()
	c.logger.Debug("Write mutex acquired",
		zap.Any("request_id", req.ID),
		zap.Duration("wait_time", mutexAcquiredAt.Sub(startTime)))
	defer c.writeMu.Unlock()

	// Double-check connection hasn't changed during mutex acquisition.
	c.connMu.Lock()

	if c.conn == nil || c.conn != conn {
		c.connMu.Unlock()

		return errors.New("connection changed during send")
	}

	currentConn := c.conn
	c.connMu.Unlock()

	// Create wire message with auth token if configured.
	msg := &WireMessage{
		ID:              req.ID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Source:          "local-router",
		TargetNamespace: extractNamespace(req.Method),
		MCPPayload:      req,
	}

	// Add auth token if using bearer authentication.
	if c.config.Auth.Type == "bearer" && c.config.Auth.Token != "" {
		msg.AuthToken = c.config.Auth.Token
	}

	c.logger.Info("DIAG: Sending request to gateway WebSocket",
		zap.Any("id", req.ID),
		zap.String("method", req.Method),
	)

	// Set write deadline.
	deadline := time.Now().Add(defaultRetryCount * time.Second)
	if err := currentConn.SetWriteDeadline(deadline); err != nil {
		c.logger.Debug("Failed to set write deadline", zap.Error(err))
	}

	beforeWrite := time.Now()
	// Send as JSON.
	err := currentConn.WriteJSON(msg)
	writeComplete := time.Now()

	if err != nil {
		c.logger.Error("DIAG: Gateway WebSocket write failed",
			zap.Any("request_id", req.ID),
			zap.Duration("write_duration", writeComplete.Sub(beforeWrite)),
			zap.Error(err))
	} else {
		c.logger.Info("DIAG: Gateway WebSocket write completed",
			zap.Any("request_id", req.ID),
			zap.Duration("write_duration", writeComplete.Sub(beforeWrite)),
			zap.Duration("total_send_duration", writeComplete.Sub(startTime)))
	}

	return err
}

// ReceiveResponse receives an MCP response from the gateway with proper concurrency control.
//
// This method implements a deadlock-free approach similar to SendRequest:
// 1. Briefly checks connection state without holding locks during I/O operations
// 2. Uses read-specific mutex to prevent concurrent read operations
// 3. Includes comprehensive error classification and panic recovery
//
// The method handles various WebSocket error conditions:
// - Normal/abnormal connection closures
// - Network timeouts and I/O errors
// - Invalid message formats and protocol violations
//
// Returns:
//   - *mcp.Response: The received response on success
//   - error: Detailed error information on failure
//
// Thread Safety: Safe for concurrent use. Only one goroutine should call this
// method at a time per client instance (typically from handleWSToStdout).
func (c *Client) ReceiveResponse() (*mcp.Response, error) {
	// Use the descriptive response receiver instead of complex inline logic.
	receiver := InitializeResponseReceiver(c)

	return receiver.ReceiveGatewayResponse()
}

// SendPing sends a ping message for keepalive.
func (c *Client) SendPing() error {
	// Check connection state with minimal lock time.
	c.connMu.Lock()
	conn := c.conn
	c.connMu.Unlock()

	if conn == nil {
		return errors.New("not connected")
	}

	// Hold write mutex for the actual write operation.
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// Double-check connection hasn't changed during mutex acquisition.
	c.connMu.Lock()

	if c.conn == nil || c.conn != conn {
		c.connMu.Unlock()

		return errors.New("connection changed during ping")
	}

	currentConn := c.conn
	c.connMu.Unlock()

	c.logger.Debug("Sending ping")

	deadline := time.Now().Add(defaultMaxConnections * time.Second)
	if err := currentConn.SetWriteDeadline(deadline); err != nil {
		c.logger.Debug("Failed to set write deadline", zap.Error(err))
	}

	return currentConn.WriteControl(
		websocket.PingMessage,
		[]byte("keepalive"),
		deadline,
	)
}

// Close closes the WebSocket connection.
func (c *Client) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return nil
	}

	c.logger.Info("Closing gateway connection")

	// Send close message.
	deadline := time.Now().Add(defaultMaxConnections * time.Second)
	_ = c.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		deadline,
	)

	err := c.conn.Close()
	c.conn = nil

	return err
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	return c.conn != nil
}

// markConnectionClosed safely marks the connection as closed.
func (c *Client) markConnectionClosed() {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		// Don't close the connection here as it may already be closed.
		// Just mark it as nil so other methods know it's closed.
		c.conn = nil
	}
}

// extractNamespace extracts the namespace from a method name.
func extractNamespace(method string) string {
	// Handle standard MCP methods - route to system namespace.
	if method == "initialize" || method == "tools/list" || method == "tools/call" || method == "ping" {
		return "system"
	}

	// Extract namespace from tool name (e.g., "k8s.getPods" -> "k8s")
	for i := 0; i < len(method); i++ {
		if method[i] == '.' {
			return method[:i]
		}
	}

	return NamespaceDefault
}

// getCipherSuiteID maps cipher suite name to TLS constant.
func getCipherSuiteID(name string) (uint16, bool) {
	suiteMap := map[string]uint16{
		"TLS_AES_128_GCM_SHA256":                        tls.TLS_AES_128_GCM_SHA256,
		"TLS_AES_256_GCM_SHA384":                        tls.TLS_AES_256_GCM_SHA384,
		"TLS_CHACHA20_POLY1305_SHA256":                  tls.TLS_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	}

	id, ok := suiteMap[name]

	return id, ok
}

// WireMessage represents the wire protocol message format.
type WireMessage struct {
	ID              interface{} `json:"id"`
	Timestamp       string      `json:"timestamp"`
	Source          string      `json:"source"`
	TargetNamespace string      `json:"target_namespace,omitempty"`
	AuthToken       string      `json:"auth_token,omitempty"`
	MCPPayload      interface{} `json:"mcp_payload"`
}
