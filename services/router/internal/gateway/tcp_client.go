package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

const (
	// ProtocolVersion is the current protocol version.
	ProtocolVersion = 0x0001
)

// TCPClient manages TCP connections to the gateway using binary protocol.
type TCPClient struct {
	config config.GatewayConfig
	logger *zap.Logger

	// TCP connection.
	conn   net.Conn
	connMu sync.Mutex

	// Message handling.
	writeMu sync.Mutex
	readMu  sync.Mutex
	reader  *bufio.Reader

	// TLS configuration.
	tlsConfig *tls.Config

	// HTTP client for OAuth2.
	httpClient *http.Client

	// OAuth2 client.
	oauth2Client *OAuth2Client

	// Protocol version.
	protocolVersion uint16
}

// NewTCPClient creates a new TCP-based gateway client.
func NewTCPClient(cfg config.GatewayConfig, logger *zap.Logger) (*TCPClient, error) {
	c := &TCPClient{
		config:          cfg,
		logger:          logger,
		protocolVersion: ProtocolVersion, // Current protocol version
	}

	// Configure TLS.
	if err := c.configureTLS(); err != nil {
		return nil, fmt.Errorf("failed to configure TLS: %w", err)
	}

	// Configure HTTP client for OAuth2.
	c.httpClient = &http.Client{
		Timeout: defaultTimeoutSeconds * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: c.tlsConfig,
		},
	}

	// Create OAuth2 client if needed.
	if cfg.Auth.Type == config.AuthTypeOAuth2 {
		c.oauth2Client = NewOAuth2Client(cfg, logger, c.httpClient)
	}

	return c, nil
}

// configureTLS sets up the TLS configuration (reuses existing logic).
func (c *TCPClient) configureTLS() error {
	// This method is identical to the WebSocket client's configureTLS.
	// We'll reuse the same TLS configuration logic.
	client := &Client{config: c.config, logger: c.logger}
	if err := client.configureTLS(); err != nil {
		return err
	}

	c.tlsConfig = client.tlsConfig

	return nil
}

// Connect establishes the TCP connection to the gateway.
func (c *TCPClient) Connect(ctx context.Context) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	c.closeExistingConnection()

	address, scheme, err := c.parseConnectionDetails()
	if err != nil {
		return err
	}

	conn, err := c.establishConnection(ctx, address, scheme)
	if err != nil {
		return err
	}

	return c.finalizeConnection(conn)
}

// closeExistingConnection closes any existing connection.
func (c *TCPClient) closeExistingConnection() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

// parseConnectionDetails parses URL and determines connection parameters.
func (c *TCPClient) parseConnectionDetails() (string, string, error) {
	u, err := url.Parse(c.config.URL)
	if err != nil {
		return "", "", fmt.Errorf("invalid gateway URL: %w", err)
	}

	host := u.Hostname()
	port := c.determinePort(u)

	address := net.JoinHostPort(host, port)

	c.logger.Info("Connecting to gateway via TCP",
		zap.String("address", address),
		zap.String("scheme", u.Scheme),
		zap.Bool("tls", u.Scheme == SchemeTCPS || u.Scheme == SchemeTCPTLS),
	)

	return address, u.Scheme, nil
}

// determinePort determines the port to use based on scheme.
func (c *TCPClient) determinePort(u *url.URL) string {
	port := u.Port()
	if port != "" {
		return port
	}

	switch u.Scheme {
	case "tcp":
		return strconv.Itoa(defaultHTTPPort)
	case "tcps", "tcp+tls":
		return strconv.Itoa(defaultHTTPSPort)
	default:
		return strconv.Itoa(defaultHTTPPort) // fallback
	}
}

// establishConnection creates the actual network connection.
func (c *TCPClient) establishConnection(ctx context.Context, address, scheme string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: time.Duration(c.config.Connection.TimeoutMs) * time.Millisecond,
	}

	var conn net.Conn

	var err error

	if scheme == "tcps" || scheme == "tcp+tls" {
		// TLS connection.
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    c.tlsConfig,
		}
		conn, err = tlsDialer.DialContext(ctx, "tcp", address)
	} else {
		// Plain TCP connection.
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return conn, nil
}

// finalizeConnection completes the connection setup with handshake.
func (c *TCPClient) finalizeConnection(conn net.Conn) error {
	c.conn = conn
	c.reader = bufio.NewReader(conn)

	// Send initial handshake/authentication if needed.
	if err := c.performHandshake(); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			c.logger.Debug("Failed to close connection", zap.Error(closeErr))
		}

		return fmt.Errorf("handshake failed: %w", err)
	}

	c.logger.Info("Successfully connected to gateway via TCP")

	return nil
}

// performHandshake performs initial protocol handshake with version negotiation.
func (c *TCPClient) performHandshake() error {
	// Send version negotiation message.
	negotiation := map[string]interface{}{
		"min_version":        uint16(ProtocolVersion),
		"max_version":        uint16(ProtocolVersion),
		"preferred_version":  uint16(ProtocolVersion),
		"supported_versions": []uint16{ProtocolVersion},
	}

	payload, err := json.Marshal(negotiation)
	if err != nil {
		return fmt.Errorf("failed to marshal version negotiation: %w", err)
	}

	// Create and send version negotiation frame.
	// Validate payload size before conversion
	payloadLen := len(payload)
	if payloadLen > math.MaxUint32 {
		return fmt.Errorf("payload too large for protocol: %d bytes exceeds uint32 limit", payloadLen)
	}

	// Explicit conversion after bounds check
	payloadLength := uint32(payloadLen)
	frame := &BinaryFrame{
		Magic:       MagicBytes,
		Version:     c.protocolVersion,
		MessageType: MessageTypeVersionNegotiation,
		PayloadLen:  payloadLength,
		Payload:     payload,
	}

	if err := frame.Write(c.conn); err != nil {
		return fmt.Errorf("failed to send version negotiation: %w", err)
	}

	// Read version acknowledgment.
	_ = c.conn.SetReadDeadline(time.Now().Add(defaultRetryCount * time.Second))

	ackFrame, err := ReadBinaryFrame(c.reader)
	if err != nil {
		return fmt.Errorf("failed to read version ack: %w", err)
	}

	if ackFrame.MessageType != MessageTypeVersionAck {
		return fmt.Errorf("expected version ack, got message type: %d", ackFrame.MessageType)
	}

	// Parse version ack.
	var ack map[string]interface{}
	if err := json.Unmarshal(ackFrame.Payload, &ack); err != nil {
		return fmt.Errorf("failed to unmarshal version ack: %w", err)
	}

	agreedVersion, ok := ack["agreed_version"].(float64)
	if !ok {
		return errors.New("invalid version ack format")
	}

	c.protocolVersion = uint16(agreedVersion)
	c.logger.Info("Protocol version negotiated",
		zap.Uint16("version", c.protocolVersion))

	return nil
}

// SendRequest sends an MCP request to the gateway using binary protocol.
func (c *TCPClient) SendRequest(ctx context.Context, req *mcp.Request) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.conn == nil {
		return errors.New("not connected")
	}

	// Get authentication token.
	authToken := ""

	switch c.config.Auth.Type {
	case config.AuthTypeBearerToken:
		authToken = c.config.Auth.Token
	case config.AuthTypeOAuth2:
		if c.oauth2Client != nil {
			token, err := c.oauth2Client.GetToken(context.Background())
			if err != nil {
				return fmt.Errorf("failed to get OAuth2 token: %w", err)
			}

			authToken = token
		}
	}

	// Create authenticated message.
	authMsg := struct {
		ID              interface{} `json:"id"`
		Timestamp       string      `json:"timestamp"`
		Source          string      `json:"source"`
		TargetNamespace string      `json:"target_namespace,omitempty"`
		AuthToken       string      `json:"auth_token,omitempty"`
		MCPPayload      interface{} `json:"mcp_payload"`
	}{
		ID:              req.ID,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Source:          "local-router",
		TargetNamespace: extractNamespace(req.Method),
		AuthToken:       authToken,
		MCPPayload:      req,
	}

	// Marshal to JSON.
	payload, err := json.Marshal(authMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create binary frame.
	frame := &BinaryFrame{
		Version:     c.protocolVersion,
		MessageType: MessageTypeRequest,
		Payload:     payload,
	}

	c.logger.Debug("Sending TCP request",
		zap.Any("id", req.ID),
		zap.String("method", req.Method),
		zap.Int("payload_size", len(payload)),
	)

	// Set write deadline.
	deadline := time.Now().Add(defaultRetryCount * time.Second)
	_ = c.conn.SetWriteDeadline(deadline)

	// Send frame.
	return frame.Write(c.conn)
}

// ReceiveResponse receives an MCP response from the gateway.
func (c *TCPClient) ReceiveResponse() (*mcp.Response, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if c.conn == nil {
		return nil, errors.New("not connected")
	}

	// Read frame.
	frame, err := ReadBinaryFrame(c.reader)
	if err != nil {
		// Check for connection-related errors and provide more descriptive messages.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
			strings.Contains(err.Error(), "EOF") {
			return nil, fmt.Errorf("connection closed unexpectedly: %w", err)
		}

		return nil, fmt.Errorf("failed to read frame: %w", err)
	}

	// Validate frame type.
	if frame.MessageType != MessageTypeResponse {
		return nil, fmt.Errorf("unexpected message type: %d", frame.MessageType)
	}

	// Unmarshal wire message.
	var wireMsg struct {
		MCPPayload json.RawMessage `json:"mcp_payload"`
	}

	if err := json.Unmarshal(frame.Payload, &wireMsg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal wire message: %w", err)
	}

	// Unmarshal MCP response.
	var resp mcp.Response
	if err := json.Unmarshal(wireMsg.MCPPayload, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal MCP response: %w", err)
	}

	c.logger.Debug("Received TCP response",
		zap.Any("id", resp.ID),
		zap.Bool("has_error", resp.Error != nil),
	)

	return &resp, nil
}

// SendPing sends a ping message for keepalive.
func (c *TCPClient) SendPing() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.conn == nil {
		return errors.New("not connected")
	}

	c.logger.Debug("Sending TCP ping")

	// Create health check frame.
	frame := &BinaryFrame{
		Version:     c.protocolVersion,
		MessageType: MessageTypeHealthCheck,
		Payload:     []byte("{}"), // Empty JSON object
	}

	deadline := time.Now().Add(defaultMaxConnections * time.Second)
	_ = c.conn.SetWriteDeadline(deadline)

	return frame.Write(c.conn)
}

// Close closes the TCP connection.
func (c *TCPClient) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return nil
	}

	c.logger.Info("Closing TCP gateway connection")

	err := c.conn.Close()
	c.conn = nil
	c.reader = nil

	return err
}

// IsConnected returns true if the client is connected.
func (c *TCPClient) IsConnected() bool {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	return c.conn != nil
}
