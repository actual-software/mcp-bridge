package gateway

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/test/testutil"
)

// Test certificate generation helpers.

func generateTestCA(t *testing.T) (caCert *x509.Certificate, caKey *rsa.PrivateKey, caPEM []byte) {
	t.Helper()
	// Generate CA private key.
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create CA certificate template.
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Test CA"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Create CA certificate.
	caCertDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	// Parse the certificate.
	caCert, err = x509.ParseCertificate(caCertDER)
	require.NoError(t, err)

	// Encode to PEM.
	caPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertDER,
	})

	return caCert, caKey, caPEM
}

func generateTestCertificate(
	t *testing.T,
	caCert *x509.Certificate,
	caKey *rsa.PrivateKey,
	isClient bool,
) (certPEM, keyPEM []byte) {
	t.Helper()
	// Generate private key.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create certificate template.
	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization:  []string{"Test Client"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:    []string{"localhost"},
	}

	if !isClient {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}

	// Create certificate.
	certDER, err := x509.CreateCertificate(rand.Reader, &template, caCert, &key.PublicKey, caKey)
	require.NoError(t, err)

	// Encode certificate to PEM.
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	// Encode private key to PEM.
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	return certPEM, keyPEM
}

func TestClient_ConfigureTLS(t *testing.T) {
	tmpDir, certPath, keyPath, caPath := setupTLSTestCertificates(t)
	tests := createTLSConfigurationTests(tmpDir, certPath, keyPath, caPath)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := testutil.NewTestLogger(t)
			client, err := NewClient(tt.config, logger)

			validateTLSTestResult(t, tt, client, err)
		})
	}
}

func setupTLSTestCertificates(t *testing.T) (tmpDir, certPath, keyPath, caPath string) {
	t.Helper()

	// Create temporary directory for test certificates.
	tmpDir = t.TempDir()

	// Generate test CA.
	caCert, caKey, caPEM := generateTestCA(t)
	caPath = filepath.Join(tmpDir, "ca.crt")
	require.NoError(t, os.WriteFile(caPath, caPEM, 0o600))

	// Generate client certificate.
	certPEM, keyPEM := generateTestCertificate(t, caCert, caKey, true)
	certPath = filepath.Join(tmpDir, "client.crt")
	keyPath = filepath.Join(tmpDir, "client.key")

	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))

	return tmpDir, certPath, keyPath, caPath
}

func createTLSConfigurationTests(tmpDir, certPath, keyPath, caPath string) []struct {
	name      string
	config    config.GatewayConfig
	wantError bool
	errorMsg  string
	validate  func(t *testing.T, tlsConfig *tls.Config)
} {
	var tests []struct {
		name      string
		config    config.GatewayConfig
		wantError bool
		errorMsg  string
		validate  func(t *testing.T, tlsConfig *tls.Config)
	}

	tests = append(tests, createBasicTLSTests(caPath)...)
	tests = append(tests, createMTLSTests(certPath, keyPath)...)
	tests = append(tests, createTLSErrorTests(tmpDir, keyPath)...)

	return tests
}

func createBasicTLSTests(caPath string) []struct {
	name      string
	config    config.GatewayConfig
	wantError bool
	errorMsg  string
	validate  func(t *testing.T, tlsConfig *tls.Config)
} {
	var tests []struct {
		name      string
		config    config.GatewayConfig
		wantError bool
		errorMsg  string
		validate  func(t *testing.T, tlsConfig *tls.Config)
	}

	tests = append(tests, createBasicTLSConfigTests()...)
	tests = append(tests, createCustomCATLSTests(caPath)...)
	tests = append(tests, createCipherSuiteTLSTests()...)

	return tests
}

func createBasicTLSConfigTests() []struct {
	name      string
	config    config.GatewayConfig
	wantError bool
	errorMsg  string
	validate  func(t *testing.T, tlsConfig *tls.Config)
} {
	return []struct {
		name      string
		config    config.GatewayConfig
		wantError bool
		errorMsg  string
		validate  func(t *testing.T, tlsConfig *tls.Config)
	}{
		{
			name: "Basic TLS configuration",
			config: config.GatewayConfig{
				TLS: common.TLSConfig{
					Verify:     true,
					MinVersion: "1.3",
				},
			},
			wantError: false,
			validate: func(t *testing.T, tlsConfig *tls.Config) {
				t.Helper()
				assert.False(t, tlsConfig.InsecureSkipVerify)
				assert.Equal(t, uint16(tls.VersionTLS13), tlsConfig.MinVersion)
			},
		},
	}
}

func createCustomCATLSTests(caPath string) []struct {
	name      string
	config    config.GatewayConfig
	wantError bool
	errorMsg  string
	validate  func(t *testing.T, tlsConfig *tls.Config)
} {
	return []struct {
		name      string
		config    config.GatewayConfig
		wantError bool
		errorMsg  string
		validate  func(t *testing.T, tlsConfig *tls.Config)
	}{
		{
			name: "TLS with custom CA",
			config: config.GatewayConfig{
				TLS: common.TLSConfig{
					Verify:     true,
					CAFile:     caPath,
					MinVersion: "1.2",
				},
			},
			wantError: false,
			validate: func(t *testing.T, tlsConfig *tls.Config) {
				t.Helper()
				assert.NotNil(t, tlsConfig.RootCAs)
				assert.Equal(t, uint16(tls.VersionTLS12), tlsConfig.MinVersion)
			},
		},
	}
}

func createCipherSuiteTLSTests() []struct {
	name      string
	config    config.GatewayConfig
	wantError bool
	errorMsg  string
	validate  func(t *testing.T, tlsConfig *tls.Config)
} {
	return []struct {
		name      string
		config    config.GatewayConfig
		wantError bool
		errorMsg  string
		validate  func(t *testing.T, tlsConfig *tls.Config)
	}{
		{
			name: "TLS with cipher suites",
			config: config.GatewayConfig{
				TLS: common.TLSConfig{
					Verify: true,
					CipherSuites: []string{
						"TLS_AES_256_GCM_SHA384",
						"TLS_CHACHA20_POLY1305_SHA256",
						"UNKNOWN_CIPHER", // Should be ignored with warning
					},
				},
			},
			wantError: false,
			validate: func(t *testing.T, tlsConfig *tls.Config) {
				t.Helper()
				assert.Len(t, tlsConfig.CipherSuites, 2)
				assert.Contains(t, tlsConfig.CipherSuites, tls.TLS_AES_256_GCM_SHA384)
				assert.Contains(t, tlsConfig.CipherSuites, tls.TLS_CHACHA20_POLY1305_SHA256)
			},
		},
	}
}

func createMTLSTests(certPath, keyPath string) []struct {
	name      string
	config    config.GatewayConfig
	wantError bool
	errorMsg  string
	validate  func(t *testing.T, tlsConfig *tls.Config)
} {
	return []struct {
		name      string
		config    config.GatewayConfig
		wantError bool
		errorMsg  string
		validate  func(t *testing.T, tlsConfig *tls.Config)
	}{
		{
			name: "mTLS configuration",
			config: config.GatewayConfig{
				Auth: common.AuthConfig{
					Type:       "mtls",
					ClientCert: certPath,
					ClientKey:  keyPath,
				},
				TLS: common.TLSConfig{
					Verify: true,
				},
			},
			wantError: false,
			validate: func(t *testing.T, tlsConfig *tls.Config) {
				t.Helper()
				assert.Len(t, tlsConfig.Certificates, 1)
			},
		},
	}
}

func createTLSErrorTests(tmpDir, keyPath string) []struct {
	name      string
	config    config.GatewayConfig
	wantError bool
	errorMsg  string
	validate  func(t *testing.T, tlsConfig *tls.Config)
} {
	return []struct {
		name      string
		config    config.GatewayConfig
		wantError bool
		errorMsg  string
		validate  func(t *testing.T, tlsConfig *tls.Config)
	}{
		{
			name: "Invalid CA certificate",
			config: config.GatewayConfig{
				TLS: common.TLSConfig{
					Verify: true,
					CAFile: filepath.Join(tmpDir, "nonexistent.crt"),
				},
			},
			wantError: true,
			errorMsg:  "failed to read CA certificate",
		},
		{
			name: "mTLS missing client certificate",
			config: config.GatewayConfig{
				Auth: common.AuthConfig{
					Type:      "mtls",
					ClientKey: keyPath,
				},
				TLS: common.TLSConfig{
					Verify: true,
				},
			},
			wantError: true,
			errorMsg:  "client certificate and key are required for mTLS",
		},
		{
			name: "mTLS invalid certificate",
			config: config.GatewayConfig{
				Auth: common.AuthConfig{
					Type:       "mtls",
					ClientCert: filepath.Join(tmpDir, "invalid.crt"),
					ClientKey:  keyPath,
				},
				TLS: common.TLSConfig{
					Verify: true,
				},
			},
			wantError: true,
			errorMsg:  "failed to load client certificate",
		},
	}
}

func validateTLSTestResult(t *testing.T, tt struct {
	name      string
	config    config.GatewayConfig
	wantError bool
	errorMsg  string
	validate  func(t *testing.T, tlsConfig *tls.Config)
}, client *Client, err error) {
	t.Helper()

	if tt.wantError {
		require.Error(t, err)

		if tt.errorMsg != "" {
			assert.Contains(t, err.Error(), tt.errorMsg)
		}
	} else {
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.NotNil(t, client.tlsConfig)

		if tt.validate != nil {
			tt.validate(t, client.tlsConfig)
		}
	}
}

func TestClient_ConfigureMTLS(t *testing.T) {
	tmpDir, certPath, keyPath, invalidCertPath := setupMTLSTestFiles(t)
	tests := createMTLSConfigurationTests(tmpDir, certPath, keyPath, invalidCertPath)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createMTLSTestClient(t, tt)
			err := client.configureMTLS()

			validateMTLSTestResult(t, tt, client, err)
		})
	}
}

func setupMTLSTestFiles(t *testing.T) (tmpDir, certPath, keyPath, invalidCertPath string) {
	t.Helper()

	// Create temporary directory for test certificates.
	tmpDir = t.TempDir()

	// Generate test certificates.
	caCert, caKey, _ := generateTestCA(t)
	certPEM, keyPEM := generateTestCertificate(t, caCert, caKey, true)

	certPath = filepath.Join(tmpDir, "client.crt")
	keyPath = filepath.Join(tmpDir, "client.key")
	invalidCertPath = filepath.Join(tmpDir, "invalid.crt")

	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))
	require.NoError(t, os.WriteFile(invalidCertPath, []byte("invalid cert data"), 0o600))

	return tmpDir, certPath, keyPath, invalidCertPath
}

func createMTLSConfigurationTests(tmpDir, certPath, keyPath, invalidCertPath string) []struct {
	name      string
	config    common.AuthConfig
	tlsConfig *tls.Config
	wantError bool
	errorMsg  string
} {
	return []struct {
		name      string
		config    common.AuthConfig
		tlsConfig *tls.Config
		wantError bool
		errorMsg  string
	}{
		{
			name: "Valid mTLS configuration",
			config: common.AuthConfig{
				ClientCert: certPath,
				ClientKey:  keyPath,
			},
			tlsConfig: &tls.Config{}, // #nosec G402 - test configuration only
			wantError: false,
		},
		{
			name: "Missing client certificate",
			config: common.AuthConfig{
				ClientKey: keyPath,
			},
			tlsConfig: &tls.Config{}, // #nosec G402 - test configuration only
			wantError: true,
			errorMsg:  "client certificate and key are required for mTLS",
		},
		{
			name: "Missing client key",
			config: common.AuthConfig{
				ClientCert: certPath,
			},
			tlsConfig: &tls.Config{}, // #nosec G402 - test configuration only
			wantError: true,
			errorMsg:  "client certificate and key are required for mTLS",
		},
		{
			name: "Invalid certificate file",
			config: common.AuthConfig{
				ClientCert: invalidCertPath,
				ClientKey:  keyPath,
			},
			tlsConfig: &tls.Config{}, // #nosec G402 - test configuration only
			wantError: true,
			errorMsg:  "failed to load client certificate",
		},
		{
			name: "Nonexistent certificate file",
			config: common.AuthConfig{
				ClientCert: filepath.Join(tmpDir, "nonexistent.crt"),
				ClientKey:  keyPath,
			},
			tlsConfig: &tls.Config{}, // #nosec G402 - test configuration only
			wantError: true,
			errorMsg:  "failed to load client certificate",
		},
	}
}

func createMTLSTestClient(t *testing.T, tt struct {
	name      string
	config    common.AuthConfig
	tlsConfig *tls.Config
	wantError bool
	errorMsg  string
}) *Client {
	t.Helper()

	logger := testutil.NewTestLogger(t)

	return &Client{
		config: config.GatewayConfig{
			Auth: tt.config,
		},
		logger:    logger,
		tlsConfig: tt.tlsConfig,
	}
}

func validateMTLSTestResult(t *testing.T, tt struct {
	name      string
	config    common.AuthConfig
	tlsConfig *tls.Config
	wantError bool
	errorMsg  string
}, client *Client, err error) {
	t.Helper()

	if tt.wantError {
		require.Error(t, err)

		if tt.errorMsg != "" {
			assert.Contains(t, err.Error(), tt.errorMsg)
		}
	} else {
		require.NoError(t, err)
		assert.Len(t, client.tlsConfig.Certificates, 1)
	}
}

func TestGetCipherSuiteID(t *testing.T) {
	tests := []struct {
		name   string
		suite  string
		wantID uint16
		wantOK bool
	}{
		{
			name:   "TLS 1.3 AES-128",
			suite:  "TLS_AES_128_GCM_SHA256",
			wantID: tls.TLS_AES_128_GCM_SHA256,
			wantOK: true,
		},
		{
			name:   "TLS 1.3 AES-256",
			suite:  "TLS_AES_256_GCM_SHA384",
			wantID: tls.TLS_AES_256_GCM_SHA384,
			wantOK: true,
		},
		{
			name:   "TLS 1.3 ChaCha20",
			suite:  "TLS_CHACHA20_POLY1305_SHA256",
			wantID: tls.TLS_CHACHA20_POLY1305_SHA256,
			wantOK: true,
		},
		{
			name:   "TLS 1.2 ECDHE RSA",
			suite:  "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			wantID: tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			wantOK: true,
		},
		{
			name:   "Unknown cipher suite",
			suite:  "TLS_UNKNOWN_CIPHER",
			wantID: 0,
			wantOK: false,
		},
		{
			name:   "Empty string",
			suite:  "",
			wantID: 0,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := getCipherSuiteID(tt.suite)
			assert.Equal(t, tt.wantOK, ok)

			if tt.wantOK {
				assert.Equal(t, tt.wantID, id)
			}
		})
	}
}

func TestClient_ConfigureTLS_Integration(t *testing.T) {
	// This test verifies the full TLS configuration flow.
	tmpDir := t.TempDir()

	// Generate full certificate chain.
	caCert, caKey, caPEM := generateTestCA(t)
	clientCertPEM, clientKeyPEM := generateTestCertificate(t, caCert, caKey, true)

	// Write files.
	caPath := filepath.Join(tmpDir, "ca.crt")
	certPath := filepath.Join(tmpDir, "client.crt")
	keyPath := filepath.Join(tmpDir, "client.key")

	require.NoError(t, os.WriteFile(caPath, caPEM, 0o600))
	require.NoError(t, os.WriteFile(certPath, clientCertPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, clientKeyPEM, 0o600))

	// Create client with full mTLS configuration.
	config := config.GatewayConfig{
		Auth: common.AuthConfig{
			Type:       "mtls",
			ClientCert: certPath,
			ClientKey:  keyPath,
		},
		TLS: common.TLSConfig{
			Verify:     true,
			CAFile:     caPath,
			MinVersion: "1.3",
			CipherSuites: []string{
				"TLS_AES_256_GCM_SHA384",
				"TLS_CHACHA20_POLY1305_SHA256",
			},
		},
	}

	logger := testutil.NewTestLogger(t)
	client, err := NewClient(config, logger)
	require.NoError(t, err)

	// Verify complete TLS configuration.
	assert.NotNil(t, client.tlsConfig)
	assert.False(t, client.tlsConfig.InsecureSkipVerify)
	assert.Equal(t, uint16(tls.VersionTLS13), client.tlsConfig.MinVersion)
	assert.NotNil(t, client.tlsConfig.RootCAs)
	assert.Len(t, client.tlsConfig.Certificates, 1)
	assert.Len(t, client.tlsConfig.CipherSuites, 2)

	// Verify HTTP client is configured with TLS.
	assert.NotNil(t, client.httpClient)
	transport, ok := client.httpClient.Transport.(*http.Transport)
	assert.True(t, ok, "type assertion failed")
	assert.Equal(t, client.tlsConfig, transport.TLSClientConfig)
}
