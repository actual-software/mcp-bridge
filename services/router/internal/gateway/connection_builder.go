package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// DefaultBufferSize is the default buffer size for WebSocket connections.
	DefaultBufferSize = 65536 // 64KB
)

// ConnectionBuilder handles WebSocket connection establishment.
type ConnectionBuilder struct {
	client  *Client
	ctx     context.Context
	headers http.Header
}

// InitializeConnectionBuilder creates a new connection builder.
func InitializeConnectionBuilder(client *Client, ctx context.Context) *ConnectionBuilder {
	return &ConnectionBuilder{
		client:  client,
		ctx:     ctx,
		headers: http.Header{},
	}
}

// EstablishConnection builds and establishes the WebSocket connection.
// Note: Caller must hold connMu lock.
func (b *ConnectionBuilder) EstablishConnection() (*websocket.Conn, error) {
	b.closeExistingConnection()

	parsedURL, err := b.parseURL()
	if err != nil {
		return nil, err
	}

	if err := b.configureAuthentication(); err != nil {
		return nil, err
	}

	dialer := b.createDialer()

	conn, err := b.performConnection(parsedURL, dialer)
	if err != nil {
		return nil, err
	}

	b.logConnectionDetails(conn)
	b.configureConnectionSettings(conn)

	return conn, nil
}

func (b *ConnectionBuilder) configureConnectionSettings(conn *websocket.Conn) {
	_ = conn.SetReadDeadline(time.Time{}) // No read deadline
	conn.SetPongHandler(func(string) error {
		b.client.logger.Debug("Received pong")

		return nil
	})
}

func (b *ConnectionBuilder) closeExistingConnection() {
	if b.client.conn != nil {
		_ = b.client.conn.Close()
	}
}

func (b *ConnectionBuilder) parseURL() (*url.URL, error) {
	u, err := url.Parse(b.client.config.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway URL: %w", err)
	}

	return u, nil
}

func (b *ConnectionBuilder) configureAuthentication() error {
	switch b.client.config.Auth.Type {
	case "bearer":
		return b.configureBearerAuth()
	case "oauth2":
		return b.configureOAuth2()
	case "mtls":
		return b.configureMTLSAuth()
	default:
		return nil
	}
}

func (b *ConnectionBuilder) configureBearerAuth() error {
	if b.client.config.Auth.Token == "" {
		return errors.New("bearer token not configured")
	}

	b.headers.Set("Authorization", "Bearer "+b.client.config.Auth.Token)

	return nil
}

func (b *ConnectionBuilder) configureOAuth2() error {
	token, err := b.client.getOAuth2Token(b.ctx)
	if err != nil {
		return fmt.Errorf("failed to get OAuth2 token: %w", err)
	}

	b.headers.Set("Authorization", "Bearer "+token)

	return nil
}

func (b *ConnectionBuilder) configureMTLSAuth() error {
	if err := b.client.configureMTLS(); err != nil {
		return fmt.Errorf("failed to configure mTLS: %w", err)
	}

	return nil
}

func (b *ConnectionBuilder) createDialer() websocket.Dialer {
	return websocket.Dialer{
		TLSClientConfig:  b.client.tlsConfig,
		HandshakeTimeout: time.Duration(b.client.config.Connection.TimeoutMs) * time.Millisecond,
		ReadBufferSize:   DefaultBufferSize,
		WriteBufferSize:  DefaultBufferSize,
		NetDialContext: (&net.Dialer{
			Timeout:   time.Duration(b.client.config.Connection.TimeoutMs) * time.Millisecond,
			KeepAlive: 0, // Disable keep-alive
		}).DialContext,
	}
}

func (b *ConnectionBuilder) performConnection(u *url.URL, dialer websocket.Dialer) (*websocket.Conn, error) {
	b.logConnectionAttempt(u)

	conn, resp, err := dialer.DialContext(b.ctx, u.String(), b.headers)
	if resp != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				b.client.logger.Debug("Failed to close response body", zap.Error(err))
			}
		}()
	}

	if err != nil {
		b.logConnectionError(err, resp, u)

		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	b.client.logger.Info("WebSocket connection established successfully")

	return conn, nil
}

func (b *ConnectionBuilder) logConnectionAttempt(u *url.URL) {
	b.client.logger.Info("Connecting to gateway",
		zap.String("url", u.String()),
		zap.String("auth_type", b.client.config.Auth.Type),
		zap.Bool("tls_verify", b.client.config.TLS.Verify),
		zap.String("tls_server_name", b.client.tlsConfig.ServerName),
		zap.Bool("has_custom_ca", b.client.tlsConfig.RootCAs != nil))
}

func (b *ConnectionBuilder) logConnectionError(err error, resp *http.Response, u *url.URL) {
	if resp != nil {
		b.client.logger.Error("WebSocket upgrade failed",
			zap.Int("status", resp.StatusCode),
			zap.String("status_text", resp.Status),
			zap.Error(err))
	} else {
		b.client.logger.Error("Connection failed",
			zap.Error(err),
			zap.String("url", u.String()))
	}
}

func (b *ConnectionBuilder) logConnectionDetails(conn *websocket.Conn) {
	tlsConn, ok := conn.UnderlyingConn().(*tls.Conn)
	if !ok {
		return
	}

	state := tlsConn.ConnectionState()
	b.client.logger.Info("TLS connection details",
		zap.Uint16("tls_version", state.Version),
		zap.Uint16("cipher_suite", state.CipherSuite),
		zap.String("server_name", state.ServerName),
		zap.Bool("handshake_complete", state.HandshakeComplete),
		zap.Int("peer_certificates_count", len(state.PeerCertificates)))

	b.logPeerCertificates(state)
}

func (b *ConnectionBuilder) logPeerCertificates(state tls.ConnectionState) {
	if len(state.PeerCertificates) == 0 {
		return
	}

	for i, cert := range state.PeerCertificates {
		b.client.logger.Info("Peer certificate",
			zap.Int("index", i),
			zap.String("subject", cert.Subject.String()),
			zap.String("issuer", cert.Issuer.String()),
			zap.Time("not_before", cert.NotBefore),
			zap.Time("not_after", cert.NotAfter))
	}
}
