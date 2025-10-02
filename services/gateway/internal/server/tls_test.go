package server

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/test/testutil"
)

func TestCreateTLSConfig(t *testing.T) {
	// Create temporary directory for test certificates
	tempDir := t.TempDir()

	// Create test certificates
	certFile, keyFile, caFile := testutil.CreateTestCertificates(t, tempDir)

	tests := createTLSConfigTests(certFile, keyFile, caFile)

	// Execute all TLS configuration test scenarios
	runTLSConfigTests(t, tests)
}

type tlsConfigTest struct {
	name      string
	tlsConfig config.TLSConfig
	wantErr   bool
	validate  func(t *testing.T, cfg *tls.Config)
	scenario  string
}

func createTLSConfigTests(certFile, keyFile, caFile string) []tlsConfigTest {
	tests := []tlsConfigTest{}

	tests = append(tests, createBasicTLSTests(certFile, keyFile)...)
	tests = append(tests, createAdvancedTLSTests(certFile, keyFile, caFile)...)
	tests = append(tests, createTLSErrorTests(certFile, keyFile)...)

	return tests
}

func createBasicTLSTests(certFile, keyFile string) []tlsConfigTest {
	return []tlsConfigTest{
		{
			name: "Basic TLS configuration",
			scenario: "Standard TLS setup with server certificate and private key. " +
				"Tests default settings including TLS 1.2 minimum version and no client authentication. " +
				"This represents the most common production TLS configuration for web services.",
			tlsConfig: config.TLSConfig{
				Enabled:  true,
				CertFile: certFile,
				KeyFile:  keyFile,
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *tls.Config) {
				t.Helper()
				assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
				assert.Equal(t, tls.NoClientCert, cfg.ClientAuth)
			},
		},
		{
			name: "TLS 1.3 configuration",
			scenario: "Modern TLS 1.3 setup with enhanced security features. " +
				"Tests explicit TLS version selection and validates that appropriate cipher suites " +
				"are automatically selected. TLS 1.3 provides improved security and performance.",
			tlsConfig: config.TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				MinVersion: "1.3",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *tls.Config) {
				t.Helper()
				assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
				assert.NotEmpty(t, cfg.CipherSuites)
			},
		},
	}
}

func createAdvancedTLSTests(certFile, keyFile, caFile string) []tlsConfigTest {
	return []tlsConfigTest{
		{
			name: "mTLS with client verification",
			scenario: "Mutual TLS configuration requiring client certificate validation. " +
				"Tests CA certificate loading and client authentication enforcement. " +
				"This provides maximum security by verifying both server and client identities.",
			tlsConfig: config.TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				CAFile:     caFile,
				ClientAuth: "require",
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *tls.Config) {
				t.Helper()
				assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
				assert.NotNil(t, cfg.ClientCAs)
			},
		},
		{
			name: "Custom cipher suites",
			scenario: "TLS configuration with specific cipher suite selection. " +
				"Tests custom cryptographic algorithm selection for compliance or security requirements. " +
				"Validates that only specified cipher suites are enabled in the TLS configuration.",
			tlsConfig: config.TLSConfig{
				Enabled:  true,
				CertFile: certFile,
				KeyFile:  keyFile,
				CipherSuites: []string{
					"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
					"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *tls.Config) {
				t.Helper()
				assert.Len(t, cfg.CipherSuites, 2)
				assert.Contains(t, cfg.CipherSuites, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384)
				assert.Contains(t, cfg.CipherSuites, tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256)
			},
		},
	}
}

func createTLSErrorTests(certFile, keyFile string) []tlsConfigTest {
	tests := []tlsConfigTest{}

	tests = append(tests, createTLSValidationErrorTests(certFile, keyFile)...)
	tests = append(tests, createTLSConfigurationErrorTests(certFile, keyFile)...)

	return tests
}

func createTLSValidationErrorTests(certFile, keyFile string) []tlsConfigTest {
	return []tlsConfigTest{
		{
			name: "Invalid cipher suite",
			scenario: "Error case: TLS configuration with unrecognized cipher suite name. " +
				"Tests validation of cipher suite names and proper error handling for typos " +
				"or unsupported algorithms. Should fail during configuration validation.",
			tlsConfig: config.TLSConfig{
				Enabled:      true,
				CertFile:     certFile,
				KeyFile:      keyFile,
				CipherSuites: []string{"INVALID_CIPHER_SUITE"},
			},
			wantErr: true,
		},
		{
			name: "Missing certificate file",
			scenario: "Error case: TLS enabled but server certificate file path is empty. " +
				"Tests validation of required certificate files and ensures proper error " +
				"reporting when essential TLS components are missing.",
			tlsConfig: config.TLSConfig{
				Enabled:  true,
				CertFile: "",
				KeyFile:  keyFile,
			},
			wantErr: true,
		},
		{
			name: "Missing key file",
			scenario: "Error case: TLS enabled with certificate but missing private key file. " +
				"Tests validation of certificate-key pairs and ensures both components " +
				"are required for proper TLS operation.",
			tlsConfig: config.TLSConfig{
				Enabled:  true,
				CertFile: certFile,
				KeyFile:  "",
			},
			wantErr: true,
		},
	}
}

func createTLSConfigurationErrorTests(certFile, keyFile string) []tlsConfigTest {
	return []tlsConfigTest{
		{
			name: "Invalid min version",
			scenario: "Error case: TLS configuration with unsupported minimum version (TLS 1.1). " +
				"Tests security policy enforcement by rejecting outdated TLS versions " +
				"that are considered insecure by modern standards.",
			tlsConfig: config.TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				MinVersion: "1.1",
			},
			wantErr: true,
		},
		{
			name: "Invalid client auth",
			scenario: "Error case: TLS configuration with unrecognized client authentication mode. " +
				"Tests validation of client authentication options (none, request, require, etc.) " +
				"and proper error handling for configuration mistakes.",
			tlsConfig: config.TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				ClientAuth: "invalid",
			},
			wantErr: true,
		},
		{
			name: "mTLS require without CA file",
			scenario: "Error case: mTLS client authentication required but no CA certificate provided. " +
				"Tests logical validation ensuring that client certificate verification " +
				"requires a trusted CA certificate to validate against.",
			tlsConfig: config.TLSConfig{
				Enabled:    true,
				CertFile:   certFile,
				KeyFile:    keyFile,
				ClientAuth: "require",
				CAFile:     "", // Missing CA file
			},
			wantErr: true,
		},
	}
}

func runTLSConfigTests(t *testing.T, tests []tlsConfigTest) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Log detailed scenario description for better test understanding
			t.Logf("Test scenario: %s", tt.scenario)

			server := &GatewayServer{
				config: &config.Config{
					Server: config.ServerConfig{
						TLS: tt.tlsConfig,
					},
				},
				logger: zap.NewNop(),
			}

			cfg, err := server.createTLSConfig()
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestGetCipherSuite(t *testing.T) {
	tests := []struct {
		name     string
		cipher   string
		expected uint16
	}{
		// TLS 1.2 cipher suites
		{"TLS 1.2 RSA", "TLS_RSA_WITH_AES_128_GCM_SHA256", tls.TLS_RSA_WITH_AES_128_GCM_SHA256},
		{"TLS 1.2 ECDHE RSA", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384", tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384},
		{"TLS 1.2 ECDHE ECDSA", "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384", tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384},
		{"TLS 1.2 ChaCha20", "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256", tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256},

		// TLS 1.3 cipher suites
		{"TLS 1.3 AES-128", "TLS_AES_128_GCM_SHA256", tls.TLS_AES_128_GCM_SHA256},
		{"TLS 1.3 AES-256", "TLS_AES_256_GCM_SHA384", tls.TLS_AES_256_GCM_SHA384},
		{"TLS 1.3 ChaCha20", "TLS_CHACHA20_POLY1305_SHA256", tls.TLS_CHACHA20_POLY1305_SHA256},

		// Invalid cipher
		{"Invalid", "INVALID_CIPHER", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCipherSuite(tt.cipher)
			assert.Equal(t, tt.expected, result)
		})
	}
}
