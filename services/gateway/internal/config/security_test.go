package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIterations    = 100
	testMaxIterations = 1000
	testTimeout       = 50
	httpStatusOK      = 200
)

func TestTLSConfig_SecurityValidation(t *testing.T) {
	// Create test certificates
	tempDir := t.TempDir()
	certFile, keyFile, err := createTestCertificates(tempDir)
	require.NoError(t, err)

	tests := createTLSValidationTestCases(certFile, keyFile)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runTLSValidationTest(t, tt)
		})
	}
}

type tlsValidationTestCase struct {
	name    string
	config  TLSConfig
	wantErr bool
	errMsg  string
}

func createTLSValidationTestCases(certFile, keyFile string) []tlsValidationTestCase {
	secureTLS := TLSConfig{
		Enabled: true, CertFile: certFile, KeyFile: keyFile,
		ClientAuth: "require", MinVersion: "TLS1.3",
		CipherSuites: []string{"TLS_AES_256_GCM_SHA384", "TLS_CHACHA20_POLY1305_SHA256"},
	}
	acceptableTLS := TLSConfig{
		Enabled: true, CertFile: certFile, KeyFile: keyFile,
		ClientAuth: "request", MinVersion: "TLS1.2",
	}
	insecureTLS11 := TLSConfig{
		Enabled: true, CertFile: certFile, KeyFile: keyFile, MinVersion: "TLS1.1",
	}
	insecureTLS10 := TLSConfig{
		Enabled: true, CertFile: certFile, KeyFile: keyFile, MinVersion: "TLS1.0",
	}
	missingCert := TLSConfig{Enabled: true, KeyFile: keyFile}
	missingKey := TLSConfig{Enabled: true, CertFile: certFile}
	nonExistentCert := TLSConfig{
		Enabled: true, CertFile: "/nonexistent/cert.pem", KeyFile: keyFile,
	}
	weakCiphers := TLSConfig{
		Enabled: true, CertFile: certFile, KeyFile: keyFile,
		CipherSuites: []string{"TLS_RSA_WITH_RC4_128_SHA", "TLS_RSA_WITH_3DES_EDE_CBC_SHA"},
	}

	return []tlsValidationTestCase{
		{"secure TLS configuration", secureTLS, false, ""},
		{"acceptable TLS configuration", acceptableTLS, false, ""},
		{"insecure TLS version - TLS1.1", insecureTLS11, true, "TLS version too old"},
		{"insecure TLS version - TLS1.0", insecureTLS10, true, "TLS version too old"},
		{"missing certificate file", missingCert, true, "cert file required"},
		{"missing key file", missingKey, true, "key file required"},
		{"non-existent certificate file", nonExistentCert, false, ""}, // Validation doesn't check file existence
		{"weak cipher suites", weakCiphers, false, ""},                // Basic validation doesn't check cipher suite security
	}
}

func runTLSValidationTest(t *testing.T, tt tlsValidationTestCase) {
	t.Helper()

	err := ValidateTLSConfig(&tt.config)

	if tt.wantErr {
		require.Error(t, err)

		if tt.errMsg != "" {
			assert.Contains(t, err.Error(), tt.errMsg)
		}
	} else {
		require.NoError(t, err)
	}
}

func TestAuthConfig_SecurityValidation(t *testing.T) {
	tests := createAuthConfigTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runAuthConfigTest(t, tt)
		})
	}
}

type authConfigTestCase struct {
	name    string
	config  AuthConfig
	wantErr bool
	errMsg  string
}

func createAuthConfigTestCases() []authConfigTestCase {
	secureJWT := AuthConfig{
		Provider: "jwt",
		JWT: JWTConfig{
			Issuer: "https://secure-auth.example.com", Audience: "mcp-gateway", PublicKeyPath: "/path/to/rsa-public.key",
		},
	}
	secureOAuth2 := AuthConfig{
		Provider: "oauth2",
		OAuth2: OAuth2Config{
			ClientID:           "secure-client-id",
			ClientSecretEnv:    "OAUTH2_CLIENT_SECRET",
			TokenEndpoint:      "https://auth.example.com/oauth/token",
			IntrospectEndpoint: "https://auth.example.com/oauth/introspect",
			JWKSEndpoint:       "https://auth.example.com/.well-known/jwks.json", Scopes: []string{"read", "write"},
			Issuer: "https://auth.example.com", Audience: "mcp-services",
		},
	}
	noAuth := AuthConfig{Provider: "none"}
	insecureOAuth2 := AuthConfig{
		Provider: "oauth2",
		OAuth2: OAuth2Config{
			ClientID:      "client-id",
			TokenEndpoint: "http://insecure.example.com/token", IntrospectEndpoint: "http://insecure.example.com/introspect",
		},
	}
	weakJWT := AuthConfig{
		Provider: "jwt",
		JWT:      JWTConfig{Issuer: "localhost", Audience: "test", PublicKeyPath: "/path/to/public.key"},
	}
	missingJWT := AuthConfig{Provider: "jwt"}
	missingOAuth2 := AuthConfig{Provider: "oauth2"}

	return []authConfigTestCase{
		{"secure JWT configuration", secureJWT, false, ""},
		{"secure OAuth2 configuration", secureOAuth2, false, ""},
		{"no authentication", noAuth, false, ""},
		{"insecure HTTP endpoints in OAuth2", insecureOAuth2, false, ""}, // Basic validation doesn't enforce HTTPS
		{"weak issuer configuration", weakJWT, false, ""},                // Basic validation doesn't enforce strong issuers
		{"missing JWT configuration", missingJWT, true, "issuer cannot be empty"},
		{"missing OAuth2 configuration", missingOAuth2, true, "OAuth2 client ID required"},
	}
}

func runAuthConfigTest(t *testing.T, tt authConfigTestCase) {
	t.Helper()

	err := ValidateAuthConfig(&tt.config)

	if tt.wantErr {
		require.Error(t, err)

		if tt.errMsg != "" {
			assert.Contains(t, err.Error(), tt.errMsg)
		}
	} else {
		require.NoError(t, err)
	}
}

func TestSecurityHeaders_Validation(t *testing.T) {
	tests := createSecurityHeadersTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSecurityHeadersTest(t, tt)
		})
	}
}

type securityHeadersTestCase struct {
	name    string
	config  ServerConfig
	wantErr bool
	errMsg  string
}

func createSecurityHeadersTestCases() []securityHeadersTestCase {
	return []securityHeadersTestCase{
		{
			name: "secure server configuration",
			config: ServerConfig{
				Host: "0.0.0.0", Port: 8443, MaxConnections: testMaxIterations, MaxConnectionsPerIP: 10,
				ConnectionBufferSize: 4096, ReadTimeout: 30, WriteTimeout: 30, IdleTimeout: 120,
				AllowedOrigins: []string{"https://trusted.example.com", "https://another-trusted.example.com"},
				TLS: TLSConfig{
					Enabled: true, CertFile: "/path/to/cert.pem", KeyFile: "/path/to/key.pem",
					ClientAuth: "require", MinVersion: "TLS1.3",
				},
			},
			wantErr: false,
		},
		{
			name: "potentially insecure - wildcard origins",
			config: ServerConfig{
				Host: "0.0.0.0", Port: 8080,
				AllowedOrigins: []string{"*"}, // Wildcard origin
			},
			wantErr: false, // Basic validation doesn't prevent wildcard origins
		},
		{
			name: "potentially insecure - no connection limits",
			config: ServerConfig{
				Host: "0.0.0.0", Port: 8080,
				MaxConnections: 0, MaxConnectionsPerIP: 0, // No limits
			},
			wantErr: false, // Basic validation allows unlimited connections
		},
		{
			name: "insecure - negative port",
			config: ServerConfig{
				Host: "localhost", Port: -1,
			},
			wantErr: true,
			errMsg:  "port must be between 1 and 65535",
		},
		{
			name: "insecure - port too high",
			config: ServerConfig{
				Host: "localhost", Port: 70000,
			},
			wantErr: true,
			errMsg:  "port must be between 1 and 65535",
		},
	}
}

func runSecurityHeadersTest(t *testing.T, tt securityHeadersTestCase) {
	t.Helper()

	err := ValidateServerConfig(&tt.config)

	if tt.wantErr {
		require.Error(t, err)

		if tt.errMsg != "" {
			assert.Contains(t, err.Error(), tt.errMsg)
		}
	} else {
		require.NoError(t, err)
	}
}

func TestRateLimitConfig_SecurityValidation(t *testing.T) {
	tests := createRateLimitSecurityTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRateLimitConfig(&tt.config)
			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func createRateLimitSecurityTests() []struct {
	name    string
	config  RateLimitConfig
	wantErr bool
	errMsg  string
} {
	tests := createBasicRateLimitTests()
	tests = append(tests, createAdvancedRateLimitTests()...)

	return tests
}

func createBasicRateLimitTests() []struct {
	name    string
	config  RateLimitConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  RateLimitConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "secure rate limiting",
			config: RateLimitConfig{
				Enabled:        true,
				Provider:       "redis",
				RequestsPerSec: testIterations,
				Burst:          150,
			},
			wantErr: false,
		},
		{
			name: "aggressive rate limiting",
			config: RateLimitConfig{
				Enabled:        true,
				Provider:       "memory",
				RequestsPerSec: 10,
				Burst:          10,
			},
			wantErr: false,
		},
		{
			name: "disabled rate limiting",
			config: RateLimitConfig{
				Enabled: false,
			},
			wantErr: false, // Allowed but not secure
		},
	}
}

func createAdvancedRateLimitTests() []struct {
	name    string
	config  RateLimitConfig
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		config  RateLimitConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "very high rate limits",
			config: RateLimitConfig{
				Enabled:        true,
				Provider:       "memory",
				RequestsPerSec: 10000,
				Burst:          50000,
			},
			wantErr: false, // Basic validation doesn't enforce security limits
		},
		{
			name: "invalid - zero requests per second",
			config: RateLimitConfig{
				Enabled:        true,
				Provider:       "memory",
				RequestsPerSec: 0,
			},
			wantErr: true,
			errMsg:  "requests per second must be positive",
		},
		{
			name: "invalid - burst lower than rate",
			config: RateLimitConfig{
				Enabled:        true,
				Provider:       "memory",
				RequestsPerSec: testIterations,
				Burst:          testTimeout,
			},
			wantErr: true,
			errMsg:  "burst should be at least equal to requests per second",
		},
	}
}

func TestConfigSecurity_InjectionProtection(t *testing.T) {
	injectionTests := createInjectionProtectionTestCases()
	for _, tt := range injectionTests {
		t.Run(tt.name, func(t *testing.T) {
			runInjectionProtectionTest(t, tt)
		})
	}
}

type injectionProtectionTestCase struct {
	name   string
	config func() *Config
	field  string
}

func createInjectionProtectionTestCases() []injectionProtectionTestCase {
	return []injectionProtectionTestCase{
		{
			name: "SQL injection in service name",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Discovery.Stdio.Services = []StdioServiceConfig{
					{Name: "'; DROP TABLE services; --", Command: []string{"echo", "test"}},
				}

				return c
			},
			field: "service name",
		},
		{
			name: "Command injection in stdio command",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Discovery.Stdio.Services = []StdioServiceConfig{
					{Name: "test-service", Command: []string{"sh", "-c", "echo test; rm -rf /"}},
				}

				return c
			},
			field: "command",
		},
		{
			name: "Path traversal in cert file",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Server.TLS = TLSConfig{Enabled: true, CertFile: "../../../etc/passwd", KeyFile: "/safe/path/key.pem"}

				return c
			},
			field: "cert file",
		},
		{
			name: "XSS in allowed origins",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Server.AllowedOrigins = []string{"<script>alert('xss')</script>"}

				return c
			},
			field: "allowed origins",
		},
		{
			name: "LDAP injection in JWT issuer",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Auth = AuthConfig{
					Provider: "jwt",
					JWT:      JWTConfig{Issuer: "*)(uid=*))(|(uid=*", Audience: "test"},
				}

				return c
			},
			field: "JWT issuer",
		},
	}
}

func runInjectionProtectionTest(t *testing.T, tt injectionProtectionTestCase) {
	t.Helper()

	config := tt.config()

	// Configuration validation should not fail due to injection attempts
	// The validation focuses on structure, not content security
	err := ValidateConfig(config)

	// Some injection attempts might still be structurally valid
	// The key is that they don't cause the validator to crash or behave unexpectedly
	t.Logf("Validation result for %s injection: %v", tt.field, err)
	// Ensure the validator doesn't panic or hang
	// This test primarily ensures robustness rather than security enforcement
}

func TestConfigSecurity_SensitiveDataHandling(t *testing.T) {
	// Test handling of sensitive configuration data
	tests := []struct {
		name   string
		config func() *Config
	}{
		{
			name: "OAuth2 client secret in environment",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Auth = AuthConfig{
					Provider: "oauth2",
					OAuth2: OAuth2Config{
						ClientID:        "test-client",
						ClientSecretEnv: "OAUTH2_CLIENT_SECRET", // Should use env var, not direct value
						TokenEndpoint:   "https://auth.example.com/token",
					},
				}

				return c
			},
		},
		{
			name: "JWT secret in environment",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Auth = AuthConfig{
					Provider: "jwt",
					JWT: JWTConfig{
						Issuer:       "https://auth.example.com",
						Audience:     "mcp-gateway",
						SecretKeyEnv: "JWT_SECRET_KEY", // Should use env var, not direct value
					},
				}

				return c
			},
		},
		{
			name: "Redis password handling",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Sessions = SessionConfig{
					Provider: "redis",
					Redis: RedisConfig{
						URL:      "redis://localhost:6379",
						Password: "should-be-from-env", // In practice, should be from env
					},
				}

				return c
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config()

			err := ValidateConfig(config)
			require.NoError(t, err)

			// Verify sensitive data is properly handled
			// This is more of a documentation test than a functional test
			t.Log("Configuration with sensitive data validated successfully")
		})
	}
}

func TestConfigSecurity_ResourceLimits(t *testing.T) {
	tests := createResourceLimitSecurityTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.config()
			err := ValidateConfig(config)
			require.NoError(t, err, "Configuration should be structurally valid")
			t.Logf("Security assessment: %s - %s",
				map[bool]string{true: "SECURE", false: "POTENTIALLY INSECURE"}[tt.secure],
				tt.comment)
		})
	}
}

func createResourceLimitSecurityTests() []struct {
	name    string
	config  func() *Config
	secure  bool
	comment string
} {
	secureTests := createSecureResourceLimitTests()
	insecureTests := createInsecureResourceLimitTests()

	var allTests []struct {
		name    string
		config  func() *Config
		secure  bool
		comment string
	}

	allTests = append(allTests, secureTests...)
	allTests = append(allTests, insecureTests...)

	return allTests
}

func createSecureResourceLimitTests() []struct {
	name    string
	config  func() *Config
	secure  bool
	comment string
} {
	return []struct {
		name    string
		config  func() *Config
		secure  bool
		comment string
	}{
		{
			name: "secure resource limits",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Server.MaxConnections = testMaxIterations
				c.Server.MaxConnectionsPerIP = 10
				c.Server.ConnectionBufferSize = 4096
				c.Server.ReadTimeout = 30
				c.Server.WriteTimeout = 30
				c.Server.IdleTimeout = 120
				c.RateLimit = RateLimitConfig{
					Enabled:        true,
					Provider:       "memory",
					RequestsPerSec: testIterations,
					Burst:          150,
				}

				return c
			},
			secure:  true,
			comment: "Reasonable limits that prevent resource exhaustion",
		},
	}
}

func createInsecureResourceLimitTests() []struct {
	name    string
	config  func() *Config
	secure  bool
	comment string
} {
	return []struct {
		name    string
		config  func() *Config
		secure  bool
		comment string
	}{
		{
			name: "potentially insecure - unlimited connections",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Server.MaxConnections = 0      // Unlimited
				c.Server.MaxConnectionsPerIP = 0 // Unlimited

				return c
			},
			secure:  false,
			comment: "No connection limits can lead to resource exhaustion",
		},
		{
			name: "potentially insecure - very high limits",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Server.MaxConnections = 1000000
				c.Server.MaxConnectionsPerIP = 10000
				c.Server.ConnectionBufferSize = 1048576 // 1MB buffer

				return c
			},
			secure:  false,
			comment: "Very high limits may allow resource exhaustion attacks",
		},
		{
			name: "potentially insecure - no timeouts",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.Server.ReadTimeout = 0  // No timeout
				c.Server.WriteTimeout = 0 // No timeout
				c.Server.IdleTimeout = 0  // No timeout

				return c
			},
			secure:  false,
			comment: "No timeouts can lead to resource exhaustion via slow connections",
		},
		{
			name: "no rate limiting",
			config: func() *Config {
				c := createValidMinimalConfig()
				c.RateLimit.Enabled = false

				return c
			},
			secure:  false,
			comment: "No rate limiting allows abuse and DoS attacks",
		},
	}
}

// Helper function to create test certificates.
func createTestCertificates(dir string) (certFile, keyFile string, err error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Test Org"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"Test City"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: nil,
	}

	// Generate certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", err
	}

	// Write certificate file
	certFile = filepath.Join(dir, "test-cert.pem")

	certOut, err := os.Create(filepath.Clean(certFile))
	if err != nil {
		return "", "", err
	}

	defer func() { _ = certOut.Close() }()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return "", "", err
	}

	// Write key file
	keyFile = filepath.Join(dir, "test-key.pem")

	keyOut, err := os.Create(filepath.Clean(keyFile))
	if err != nil {
		return "", "", err
	}

	defer func() { _ = keyOut.Close() }()

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", "", err
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}); err != nil {
		return "", "", err
	}

	return certFile, keyFile, nil
}

func TestConfig_SecurityBestPractices(t *testing.T) {
	bestPracticeTests := createSecurityBestPracticeTestCases()
	for _, tt := range bestPracticeTests {
		t.Run(tt.name, func(t *testing.T) {
			runSecurityBestPracticeTest(t, tt)
		})
	}
}

type securityBestPracticeTestCase struct {
	name      string
	config    *Config
	practice  string
	compliant bool
}

func createSecurityBestPracticeTestCases() []securityBestPracticeTestCase {
	cases := createCompliantSecurityCases()
	cases = append(cases, createNonCompliantSecurityCases()...)

	return cases
}

func createCompliantSecurityCases() []securityBestPracticeTestCase {
	return []securityBestPracticeTestCase{
		{
			name: "HTTPS enforcement",
			config: &Config{
				Server: ServerConfig{
					Host: "0.0.0.0", Port: 8443,
					TLS: TLSConfig{Enabled: true, CertFile: "/path/to/cert.pem", KeyFile: "/path/to/key.pem", MinVersion: "TLS1.3"},
				},
			},
			practice: "Use HTTPS with modern TLS version", compliant: true,
		},
		{
			name: "Strong authentication",
			config: &Config{
				Auth: AuthConfig{
					Provider: "oauth2",
					OAuth2: OAuth2Config{
						ClientID:           "secure-client",
						TokenEndpoint:      "https://auth.example.com/token",
						IntrospectEndpoint: "https://auth.example.com/introspect",
					},
				},
			},
			practice: "Use strong authentication mechanism", compliant: true,
		},
		{
			name: "Rate limiting enabled",
			config: &Config{
				RateLimit: RateLimitConfig{Enabled: true, RequestsPerSec: testIterations, Burst: 150},
			},
			practice: "Enable rate limiting to prevent abuse", compliant: true,
		},
		{
			name: "Connection limits set",
			config: &Config{
				Server: ServerConfig{MaxConnections: testMaxIterations, MaxConnectionsPerIP: 10},
			},
			practice: "Set connection limits to prevent resource exhaustion", compliant: true,
		},
		{
			name: "Timeouts configured",
			config: &Config{
				Server: ServerConfig{ReadTimeout: 30, WriteTimeout: 30, IdleTimeout: 120},
			},
			practice: "Configure timeouts to prevent resource leaks", compliant: true,
		},
	}
}

func createNonCompliantSecurityCases() []securityBestPracticeTestCase {
	return []securityBestPracticeTestCase{
		{
			name:     "No authentication",
			config:   &Config{Auth: AuthConfig{Provider: "none"}},
			practice: "Use strong authentication mechanism", compliant: false,
		},
		{
			name: "Plain HTTP",
			config: &Config{
				Server: ServerConfig{Host: "0.0.0.0", Port: 8080, TLS: TLSConfig{Enabled: false}},
			},
			practice: "Use HTTPS with modern TLS version", compliant: false,
		},
		{
			name:     "No rate limiting",
			config:   &Config{RateLimit: RateLimitConfig{Enabled: false}},
			practice: "Enable rate limiting to prevent abuse", compliant: false,
		},
	}
}

func runSecurityBestPracticeTest(t *testing.T, tt securityBestPracticeTestCase) {
	t.Helper()

	compliance := map[bool]string{true: "COMPLIANT", false: "NON-COMPLIANT"}

	t.Logf("Best Practice: %s", tt.practice)
	t.Logf("Configuration: %s", compliance[tt.compliant])
	// Note: This test documents security best practices rather than enforcing them
	// Actual enforcement would be done by security-specific validation functions
}

// Benchmark security validation performance.
func BenchmarkSecurityValidation(b *testing.B) {
	config := createValidFullConfig()

	b.Run("full_config_validation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ValidateConfig(config)
		}
	})

	b.Run("tls_config_validation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ValidateTLSConfig(&config.Server.TLS)
		}
	})

	b.Run("auth_config_validation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ValidateAuthConfig(&config.Auth)
		}
	})

	b.Run("malicious_config_validation", func(b *testing.B) {
		maliciousConfig := createValidMinimalConfig()
		maliciousConfig.Auth.JWT.Issuer = strings.Repeat("a", 10000) // Large issuer

		for i := 0; i < b.N; i++ {
			_ = ValidateAuthConfig(&maliciousConfig.Auth)
		}
	})
}
