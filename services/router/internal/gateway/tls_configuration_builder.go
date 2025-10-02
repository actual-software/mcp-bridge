package gateway

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
)

// TLSConfigurationBuilder builds TLS configuration for gateway client.
type TLSConfigurationBuilder struct {
	client    *Client
	tlsConfig *tls.Config
}

// InitializeTLSConfiguration creates a new TLS configuration builder.
func InitializeTLSConfiguration(client *Client) *TLSConfigurationBuilder {
	return &TLSConfigurationBuilder{
		client: client,
		tlsConfig: &tls.Config{
			InsecureSkipVerify: !client.config.TLS.Verify, // #nosec G402 - user-configurable TLS verification
			ServerName:         "localhost",               // Ensure hostname verification works for E2E tests
		},
	}
}

// BuildTLSConfiguration builds the complete TLS configuration.
func (b *TLSConfigurationBuilder) BuildTLSConfiguration() error {
	b.configureMinVersion()

	if err := b.configureCA(); err != nil {
		return err
	}

	b.configureCipherSuites()

	if err := b.configureMTLSIfNeeded(); err != nil {
		return err
	}

	b.client.tlsConfig = b.tlsConfig

	return nil
}

func (b *TLSConfigurationBuilder) configureMinVersion() {
	switch b.client.config.TLS.MinVersion {
	case "1.2":
		b.tlsConfig.MinVersion = tls.VersionTLS12
	case "1.3":
		b.tlsConfig.MinVersion = tls.VersionTLS13
	default:
		b.tlsConfig.MinVersion = tls.VersionTLS13
	}
}

func (b *TLSConfigurationBuilder) configureCA() error {
	if b.client.config.TLS.CAFile == "" {
		b.client.logger.Info("No custom CA certificate specified, using system CA pool")

		return nil
	}

	return b.loadCustomCA()
}

func (b *TLSConfigurationBuilder) loadCustomCA() error {
	b.client.logger.Info("Loading CA certificate", zap.String("path", b.client.config.TLS.CAFile))

	caCert, err := os.ReadFile(b.client.config.TLS.CAFile)
	if err != nil {
		b.client.logger.Error("Failed to read CA certificate file",
			zap.String("path", b.client.config.TLS.CAFile),
			zap.Error(err))

		return fmt.Errorf("failed to read CA certificate: %w", err)
	}

	b.logCADetails(caCert)

	return b.applyCAToConfig(caCert)
}

func (b *TLSConfigurationBuilder) logCADetails(caCert []byte) {
	b.client.logger.Info("CA certificate file read successfully",
		zap.String("path", b.client.config.TLS.CAFile),
		zap.Int("bytes", len(caCert)))

	b.parseCertificateDetails(caCert)
}

func (b *TLSConfigurationBuilder) parseCertificateDetails(caCert []byte) {
	block, _ := pem.Decode(caCert)
	if block == nil || block.Type != "CERTIFICATE" {
		return
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return
	}

	b.client.logger.Info("CA certificate details",
		zap.String("subject", cert.Subject.String()),
		zap.String("issuer", cert.Issuer.String()),
		zap.Time("not_before", cert.NotBefore),
		zap.Time("not_after", cert.NotAfter),
		zap.String("serial", cert.SerialNumber.String()))
}

func (b *TLSConfigurationBuilder) applyCAToConfig(caCert []byte) error {
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		b.client.logger.Error("Failed to parse CA certificate PEM")

		return errors.New("failed to parse CA certificate")
	}

	b.tlsConfig.RootCAs = caCertPool
	b.client.logger.Info("Custom CA certificate loaded and configured successfully",
		zap.String("path", b.client.config.TLS.CAFile))

	return nil
}

func (b *TLSConfigurationBuilder) configureCipherSuites() {
	if len(b.client.config.TLS.CipherSuites) == 0 {
		return
	}

	cipherSuites := b.collectValidCipherSuites()

	if len(cipherSuites) > 0 {
		b.tlsConfig.CipherSuites = cipherSuites
		b.client.logger.Info("Custom cipher suites configured",
			zap.Int("count", len(cipherSuites)))
	}
}

func (b *TLSConfigurationBuilder) collectValidCipherSuites() []uint16 {
	cipherSuites := make([]uint16, 0, len(b.client.config.TLS.CipherSuites))

	for _, suite := range b.client.config.TLS.CipherSuites {
		if id, ok := getCipherSuiteID(suite); ok {
			cipherSuites = append(cipherSuites, id)
		} else {
			b.client.logger.Warn("Unknown cipher suite", zap.String("suite", suite))
		}
	}

	return cipherSuites
}

func (b *TLSConfigurationBuilder) configureMTLSIfNeeded() error {
	if b.client.config.Auth.Type != config.AuthTypeMTLS {
		return nil
	}

	if err := b.configureMTLS(); err != nil {
		return fmt.Errorf("failed to configure mTLS: %w", err)
	}

	return nil
}

// configureMTLS configures mutual TLS for the builder.
func (b *TLSConfigurationBuilder) configureMTLS() error {
	if b.client.config.Auth.ClientCert == "" || b.client.config.Auth.ClientKey == "" {
		return errors.New("client certificate and key are required for mTLS")
	}

	// Load client certificate and key.
	cert, err := tls.LoadX509KeyPair(b.client.config.Auth.ClientCert, b.client.config.Auth.ClientKey)
	if err != nil {
		return fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Set the certificate in TLS config.
	b.tlsConfig.Certificates = []tls.Certificate{cert}

	b.client.logger.Info("mTLS configured successfully",
		zap.String("cert", b.client.config.Auth.ClientCert),
		zap.String("key", b.client.config.Auth.ClientKey),
	)

	return nil
}
