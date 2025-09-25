
//go:build integration
// +build integration

package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
	"github.com/poiley/mcp-bridge/services/gateway/internal/health"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
	"github.com/poiley/mcp-bridge/services/gateway/internal/ratelimit"
	"github.com/poiley/mcp-bridge/services/gateway/internal/router"
	"github.com/poiley/mcp-bridge/services/gateway/internal/server"
	"github.com/poiley/mcp-bridge/services/gateway/internal/session"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// AuthTestSuite represents the authentication integration test suite
type AuthTestSuite struct {
	t           *testing.T
	ctx         context.Context
	cancel      context.CancelFunc
	logger      *zap.Logger
	mockRedis   *miniredis.Miniredis
	mockBackend *httptest.Server
	mockOAuth2  *MockOAuth2Server
	testCerts   *TestCertificates
}

// MockOAuth2Server represents a mock OAuth2 authorization server
type MockOAuth2Server struct {
	server       *httptest.Server
	clientID     string
	clientSecret string
	validTokens  map[string]*OAuth2TokenInfo
	mu           sync.RWMutex
}

// OAuth2TokenInfo holds information about OAuth2 tokens
type OAuth2TokenInfo struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Scope        string
	UserID       string
}

// TestCertificates holds test certificates for mTLS testing
type TestCertificates struct {
	CACert     []byte
	CAKey      []byte
	ClientCert []byte
	ClientKey  []byte
	ServerCert []byte
	ServerKey  []byte
}

// AuthConfig represents different authentication configurations for testing
type AuthConfig struct {
	Type      string
	JWT       *JWTAuthConfig
	OAuth2    *OAuth2AuthConfig
	mTLS      *mTLSAuthConfig
	RateLimit *RateLimitConfig
}

type JWTAuthConfig struct {
	SecretKey string
	Issuer    string
	Audience  string
	Algorithm string
}

type OAuth2AuthConfig struct {
	AuthorizeURL string
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

type mTLSAuthConfig struct {
	CACertPath     string
	ClientCertPath string
	ClientKeyPath  string
	RequiredCN     string
}

type RateLimitConfig struct {
	RequestsPerSecond int
	BurstSize         int
}

// NewAuthTestSuite creates and initializes a new authentication test suite
func NewAuthTestSuite(t *testing.T) *AuthTestSuite {
	ctx, cancel := context.WithCancel(context.Background())

	suite := &AuthTestSuite{
		t:      t,
		ctx:    ctx,
		cancel: cancel,
		logger: zaptest.NewLogger(t),
	}

	suite.setupEnvironment()
	return suite
}

// setupEnvironment initializes the test environment
func (s *AuthTestSuite) setupEnvironment() {
	// Start mock Redis
	mockRedis, err := miniredis.Run()
	require.NoError(s.t, err)
	s.mockRedis = mockRedis
	s.t.Cleanup(func() { mockRedis.Close() })

	// Setup test certificates
	s.testCerts = s.generateTestCertificates()

	// Setup mock OAuth2 server
	s.mockOAuth2 = s.createMockOAuth2Server()

	// Setup mock backend
	s.mockBackend = s.createMockBackend()
}

// generateTestCertificates creates test certificates for mTLS testing
func (s *AuthTestSuite) generateTestCertificates() *TestCertificates {
	caKey, caCertDER := s.generateCACertificate()
	clientKey, clientCertDER := s.generateClientCertificate(caCertDER, caKey)
	serverKey, serverCertDER := s.generateServerCertificate(caCertDER, caKey)
	
	return s.encodeTestCertificates(caKey, caCertDER, clientKey, clientCertDER, serverKey, serverCertDER)
}

func (s *AuthTestSuite) generateCACertificate() (*rsa.PrivateKey, []byte) {
	// Generate CA private key
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(s.t, err)

	// Create CA certificate template
	caTemplate := x509.Certificate{
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
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// Create CA certificate
	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	require.NoError(s.t, err)

	return caKey, caCertDER
}

func (s *AuthTestSuite) generateClientCertificate(caCertDER []byte, caKey *rsa.PrivateKey) (*rsa.PrivateKey, []byte) {
	// Generate client private key
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(s.t, err)

	// Create client certificate template
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization:  []string{"Test Client"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
			CommonName:    "test-client",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	// Parse CA certificate for signing
	caCert, err := x509.ParseCertificate(caCertDER)
	require.NoError(s.t, err)

	// Create client certificate
	clientCertDER, err := x509.CreateCertificate(rand.Reader, &clientTemplate, caCert, &clientKey.PublicKey, caKey)
	require.NoError(s.t, err)

	return clientKey, clientCertDER
}

func (s *AuthTestSuite) generateServerCertificate(caCertDER []byte, caKey *rsa.PrivateKey) (*rsa.PrivateKey, []byte) {
	// Generate server private key
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(s.t, err)

	// Create server certificate template
	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			Organization:  []string{"Test Server"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
			CommonName:    "localhost",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:    []string{"localhost"},
	}

	// Parse CA certificate for signing
	caCert, err := x509.ParseCertificate(caCertDER)
	require.NoError(s.t, err)

	// Create server certificate
	serverCertDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, caCert, &serverKey.PublicKey, caKey)
	require.NoError(s.t, err)

	return serverKey, serverCertDER
}

func (s *AuthTestSuite) encodeTestCertificates(caKey *rsa.PrivateKey, caCertDER []byte, clientKey *rsa.PrivateKey, clientCertDER []byte, serverKey *rsa.PrivateKey, serverCertDER []byte) *TestCertificates {
	// Encode certificates and keys to PEM format
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})
	clientCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER})
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)})
	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER})
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	return &TestCertificates{
		CACert:     caCertPEM,
		CAKey:      caKeyPEM,
		ClientCert: clientCertPEM,
		ClientKey:  clientKeyPEM,
		ServerCert: serverCertPEM,
		ServerKey:  serverKeyPEM,
	}
}

// createMockOAuth2Server creates a mock OAuth2 authorization server
func (s *AuthTestSuite) createMockOAuth2Server() *MockOAuth2Server {
	oauth := &MockOAuth2Server{
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		validTokens:  make(map[string]*OAuth2TokenInfo),
	}

	mux := http.NewServeMux()

	// Authorization endpoint
	mux.HandleFunc("/oauth2/authorize", oauth.handleAuthorize)

	// Token endpoint
	mux.HandleFunc("/oauth2/token", oauth.handleToken)

	// Token introspection endpoint
	mux.HandleFunc("/oauth2/introspect", oauth.handleIntrospect)

	// User info endpoint
	mux.HandleFunc("/oauth2/userinfo", oauth.handleUserInfo)

	oauth.server = httptest.NewServer(mux)
	s.t.Cleanup(func() { oauth.server.Close() })

	return oauth
}

// createMockBackend creates a mock MCP backend for authentication testing
func (s *AuthTestSuite) createMockBackend() *httptest.Server {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// WebSocket endpoint for MCP communication
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			var req mcp.Request
			if err := conn.ReadJSON(&req); err != nil {
				break
			}

			// Echo response
			response := mcp.Response{
				JSONRPC: "2.0",
				Result: map[string]interface{}{
					"method": req.Method,
					"id":     req.ID,
				},
				ID: req.ID,
			}

			if err := conn.WriteJSON(response); err != nil {
				break
			}
		}
	})

	server := httptest.NewServer(mux)
	s.t.Cleanup(func() { server.Close() })

	return server
}

// Cleanup shuts down all test components
func (s *AuthTestSuite) Cleanup() {
	s.cancel()
}

// TestAuthenticationFlows tests comprehensive authentication flows
func TestAuthenticationFlows(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping authentication integration test in short mode")
	}

	suite := NewAuthTestSuite(t)
	defer suite.Cleanup()

	t.Run("JWT_Bearer_Token_Authentication", func(t *testing.T) {
		suite.testJWTBearerAuthentication(t)
	})

	t.Run("OAuth2_Authentication_Flow", func(t *testing.T) {
		suite.testOAuth2Authentication(t)
	})

	t.Run("mTLS_Certificate_Authentication", func(t *testing.T) {
		suite.testMTLSAuthentication(t)
	})

	t.Run("Multi_Factor_Authentication", func(t *testing.T) {
		suite.testMultiFactorAuthentication(t)
	})

	t.Run("Rate_Limited_Authentication", func(t *testing.T) {
		suite.testRateLimitedAuthentication(t)
	})

	t.Run("Authentication_Error_Handling", func(t *testing.T) {
		suite.testAuthenticationErrorHandling(t)
	})

	t.Run("Session_Based_Authentication", func(t *testing.T) {
		suite.testSessionBasedAuthentication(t)
	})

	t.Run("Concurrent_Authentication_Requests", func(t *testing.T) {
		suite.testConcurrentAuthenticationRequests(t)
	})
}

// testJWTBearerAuthentication tests JWT Bearer token authentication
func (s *AuthTestSuite) testJWTBearerAuthentication(t *testing.T) {
	authConfig := &AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			SecretKey: "jwt-integration-test-secret-key-12345",
			Issuer:    "auth-integration-test",
			Audience:  "auth-integration-test",
			Algorithm: "HS256",
		},
	}

	gateway := s.createGatewayWithAuth(t, authConfig)
	defer s.shutdownGateway(t, gateway)

	// Test valid JWT token
	validToken := s.generateJWTToken(t, authConfig.JWT, map[string]interface{}{
		"sub":    "test-user",
		"scopes": []string{"mcp:read", "mcp:write"},
		"exp":    time.Now().Add(time.Hour).Unix(),
	})

	client := s.createClientWithAuth(t, gateway.serverAddr, "Bearer", validToken)
	defer func() { _ = client.Close() }()

	// Test authenticated request
	response := s.sendTestRequest(t, client, "tools/list")
	assert.Nil(t, response.Error)
	assert.NotNil(t, response.Result)

	// Test expired token
	expiredToken := s.generateJWTToken(t, authConfig.JWT, map[string]interface{}{
		"sub": "test-user",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})

	_, err := s.tryConnectWithAuth(t, gateway.serverAddr, "Bearer", expiredToken)
	assert.Error(t, err, "Expired token should be rejected")

	// Test invalid signature
	invalidToken := validToken + "invalid"
	_, err = s.tryConnectWithAuth(t, gateway.serverAddr, "Bearer", invalidToken)
	assert.Error(t, err, "Invalid token signature should be rejected")

	t.Log("✅ JWT Bearer token authentication test completed successfully")
}

// testOAuth2Authentication tests OAuth2 authentication flow
func (s *AuthTestSuite) testOAuth2Authentication(t *testing.T) {
	authConfig := &AuthConfig{
		Type: "oauth2",
		OAuth2: &OAuth2AuthConfig{
			AuthorizeURL: s.mockOAuth2.server.URL + "/oauth2/authorize",
			TokenURL:     s.mockOAuth2.server.URL + "/oauth2/token",
			ClientID:     s.mockOAuth2.clientID,
			ClientSecret: s.mockOAuth2.clientSecret,
			Scopes:       []string{"mcp:read", "mcp:write"},
		},
	}

	gateway := s.createGatewayWithAuth(t, authConfig)
	defer s.shutdownGateway(t, gateway)

	// Simulate OAuth2 token exchange
	accessToken := s.simulateOAuth2Flow(t, s.mockOAuth2)

	client := s.createClientWithAuth(t, gateway.serverAddr, "Bearer", accessToken)
	defer func() { _ = client.Close() }()

	// Test authenticated request
	response := s.sendTestRequest(t, client, "tools/list")
	assert.Nil(t, response.Error)
	assert.NotNil(t, response.Result)

	// Test token refresh
	refreshedToken := s.simulateTokenRefresh(t, s.mockOAuth2, accessToken)
	assert.NotEmpty(t, refreshedToken)

	t.Log("✅ OAuth2 authentication test completed successfully")
}

// testMTLSAuthentication tests mutual TLS authentication
func (s *AuthTestSuite) testMTLSAuthentication(t *testing.T) {
	authConfig := &AuthConfig{
		Type: "mtls",
		mTLS: &mTLSAuthConfig{
			RequiredCN: "test-client",
		},
	}

	gateway := s.createGatewayWithAuth(t, authConfig)
	defer s.shutdownGateway(t, gateway)

	// Test valid client certificate
	client := s.createClientWithMTLS(t, gateway.serverAddr, s.testCerts)
	defer func() { _ = client.Close() }()

	response := s.sendTestRequest(t, client, "tools/list")
	assert.Nil(t, response.Error)
	assert.NotNil(t, response.Result)

	// Test connection without client certificate
	_, err := s.tryConnectWithoutCert(t, gateway.serverAddr)
	assert.Error(t, err, "Connection without client cert should be rejected")

	t.Log("✅ mTLS authentication test completed successfully")
}

// testMultiFactorAuthentication tests multi-factor authentication
func (s *AuthTestSuite) testMultiFactorAuthentication(t *testing.T) {
	authConfig := &AuthConfig{
		Type: "multi",
		JWT: &JWTAuthConfig{
			SecretKey: "mfa-test-secret-key-12345",
			Issuer:    "mfa-test",
			Audience:  "mfa-test",
			Algorithm: "HS256",
		},
		mTLS: &mTLSAuthConfig{
			RequiredCN: "test-client",
		},
	}

	gateway := s.createGatewayWithAuth(t, authConfig)
	defer s.shutdownGateway(t, gateway)

	// Test with both JWT and mTLS
	jwtToken := s.generateJWTToken(t, authConfig.JWT, map[string]interface{}{
		"sub": "test-user",
		"mfa": true,
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	client := s.createClientWithMTLSAndJWT(t, gateway.serverAddr, s.testCerts, jwtToken)
	defer func() { _ = client.Close() }()

	response := s.sendTestRequest(t, client, "tools/list")
	assert.Nil(t, response.Error)
	assert.NotNil(t, response.Result)

	// Test with only JWT (should fail)
	_, err := s.tryConnectWithAuth(t, gateway.serverAddr, "Bearer", jwtToken)
	assert.Error(t, err, "JWT-only connection should be rejected for MFA")

	t.Log("✅ Multi-factor authentication test completed successfully")
}

// testRateLimitedAuthentication tests rate-limited authentication
func (s *AuthTestSuite) testRateLimitedAuthentication(t *testing.T) {
	authConfig := &AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			SecretKey: "rate-limit-test-secret-key-12345",
			Issuer:    "rate-limit-test",
			Audience:  "rate-limit-test",
			Algorithm: "HS256",
		},
		RateLimit: &RateLimitConfig{
			RequestsPerSecond: 2,
			BurstSize:         2,
		},
	}

	gateway := s.createGatewayWithAuth(t, authConfig)
	defer s.shutdownGateway(t, gateway)

	token := s.generateJWTToken(t, authConfig.JWT, map[string]interface{}{
		"sub": "rate-limited-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	client := s.createClientWithAuth(t, gateway.serverAddr, "Bearer", token)
	defer func() { _ = client.Close() }()

	// Send requests up to rate limit
	successCount := 0
	errorCount := 0

	for i := 0; i < 5; i++ {
		response := s.sendTestRequest(t, client, "tools/list")
		if response.Error != nil {
			errorCount++
		} else {
			successCount++
		}

		if i < 2 {
			time.Sleep(testIterations * time.Millisecond) // Allow some requests to succeed
		}
	}

	assert.Greater(t, successCount, 0, "Some requests should succeed")
	assert.Greater(t, errorCount, 0, "Some requests should be rate limited")

	t.Log("✅ Rate-limited authentication test completed successfully")
}

// testAuthenticationErrorHandling tests various authentication error scenarios
func (s *AuthTestSuite) testAuthenticationErrorHandling(t *testing.T) {
	authConfig := &AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			SecretKey: "error-test-secret-key-12345",
			Issuer:    "error-test",
			Audience:  "error-test",
			Algorithm: "HS256",
		},
	}

	gateway := s.createGatewayWithAuth(t, authConfig)
	defer s.shutdownGateway(t, gateway)

	// Test missing authorization header
	_, err := s.tryConnectWithoutAuth(t, gateway.serverAddr)
	assert.Error(t, err, "Connection without auth should be rejected")

	// Test malformed authorization header
	_, err = s.tryConnectWithAuth(t, gateway.serverAddr, "Invalid", "malformed-token")
	assert.Error(t, err, "Malformed auth header should be rejected")

	// Test wrong audience
	wrongAudienceToken := s.generateJWTToken(t, &JWTAuthConfig{
		SecretKey: authConfig.JWT.SecretKey,
		Issuer:    authConfig.JWT.Issuer,
		Audience:  "wrong-audience",
		Algorithm: authConfig.JWT.Algorithm,
	}, map[string]interface{}{
		"sub": "test-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err = s.tryConnectWithAuth(t, gateway.serverAddr, "Bearer", wrongAudienceToken)
	assert.Error(t, err, "Wrong audience token should be rejected")

	// Test wrong issuer
	wrongIssuerToken := s.generateJWTToken(t, &JWTAuthConfig{
		SecretKey: authConfig.JWT.SecretKey,
		Issuer:    "wrong-issuer",
		Audience:  authConfig.JWT.Audience,
		Algorithm: authConfig.JWT.Algorithm,
	}, map[string]interface{}{
		"sub": "test-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err = s.tryConnectWithAuth(t, gateway.serverAddr, "Bearer", wrongIssuerToken)
	assert.Error(t, err, "Wrong issuer token should be rejected")

	t.Log("✅ Authentication error handling test completed successfully")
}

// testSessionBasedAuthentication tests session-based authentication
func (s *AuthTestSuite) testSessionBasedAuthentication(t *testing.T) {
	authConfig := &AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			SecretKey: "session-test-secret-key-12345",
			Issuer:    "session-test",
			Audience:  "session-test",
			Algorithm: "HS256",
		},
	}

	gateway := s.createGatewayWithAuth(t, authConfig)
	defer s.shutdownGateway(t, gateway)

	token := s.generateJWTToken(t, authConfig.JWT, map[string]interface{}{
		"sub":        "session-user",
		"session_id": "test-session-123",
		"exp":        time.Now().Add(time.Hour).Unix(),
	})

	// Create first client
	client1 := s.createClientWithAuth(t, gateway.serverAddr, "Bearer", token)
	defer func() { _ = client1.Close() }()

	response1 := s.sendTestRequest(t, client1, "tools/list")
	assert.Nil(t, response1.Error)

	// Create second client with same session
	client2 := s.createClientWithAuth(t, gateway.serverAddr, "Bearer", token)
	defer func() { _ = client2.Close() }()

	response2 := s.sendTestRequest(t, client2, "tools/list")
	assert.Nil(t, response2.Error)

	// Verify session persistence across connections
	assert.NotNil(t, response1.Result)
	assert.NotNil(t, response2.Result)

	t.Log("✅ Session-based authentication test completed successfully")
}

// testConcurrentAuthenticationRequests tests concurrent authentication requests
func (s *AuthTestSuite) testConcurrentAuthenticationRequests(t *testing.T) {
	authConfig := &AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			SecretKey: "concurrent-test-secret-key-12345",
			Issuer:    "concurrent-test",
			Audience:  "concurrent-test",
			Algorithm: "HS256",
		},
	}

	gateway := s.createGatewayWithAuth(t, authConfig)
	defer s.shutdownGateway(t, gateway)

	const numClients = 20
	var wg sync.WaitGroup
	successCount := int64(0)
	errorCount := int64(0)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			token := s.generateJWTToken(t, authConfig.JWT, map[string]interface{}{
				"sub": fmt.Sprintf("concurrent-user-%d", index),
				"exp": time.Now().Add(time.Hour).Unix(),
			})

			client, err := s.tryConnectWithAuth(t, gateway.serverAddr, "Bearer", token)
			if err != nil {
				errorCount++
				return
			}
			defer func() { _ = client.Close() }()

			response := s.sendTestRequest(t, client, "tools/list")
			if response.Error != nil {
				errorCount++
			} else {
				successCount++
			}
		}(i)
	}

	wg.Wait()

	assert.Greater(t, successCount, int64(numClients*0.8), "Most concurrent requests should succeed")
	assert.Less(t, errorCount, int64(numClients*0.2), "Few concurrent requests should fail")

	t.Log("✅ Concurrent authentication requests test completed successfully")
}

// Helper methods for gateway creation and management

type testGateway struct {
	server     *server.GatewayServer
	serverAddr string
	config     *config.Config
}

func (s *AuthTestSuite) createGatewayWithAuth(t *testing.T, authConfig *AuthConfig) *testGateway {
	cfg := s.buildGatewayConfig(authConfig)
	s.setAuthEnvironmentVariables(authConfig)
	
	components := s.initializeGatewayComponents(t, cfg)
	gatewayServer := s.createGatewayServer(cfg, components)
	serverAddr := s.startGatewayServer(t, gatewayServer, cfg)

	return &testGateway{
		server:     gatewayServer,
		serverAddr: serverAddr,
		config:     cfg,
	}
}

type gatewayComponents struct {
	authProvider    auth.Provider
	sessionManager  session.Manager
	mockDiscovery   *mockServiceDiscovery
	metricsRegistry *metrics.Registry
	requestRouter   router.Router
	healthChecker   health.Checker
	rateLimiter     ratelimit.RateLimiter
}

func (s *AuthTestSuite) buildGatewayConfig(authConfig *AuthConfig) *config.Config {
	cfg := &config.Config{
		Version: 1,
		Server: config.ServerConfig{
			Port:                 0, // Use random port
			MetricsPort:          0,
			MaxConnections:       testMaxIterations,
			ConnectionBufferSize: 65536,
		},
		Auth: s.buildAuthConfig(authConfig),
		Routing: config.RoutingConfig{
			Strategy:            "round_robin",
			HealthCheckInterval: "1s",
		},
		Discovery: config.ServiceDiscoveryConfig{
			Mode: "static",
		},
		Sessions: config.SessionConfig{
			Provider: "redis",
			Redis: config.RedisConfig{
				URL: fmt.Sprintf("redis://%s", s.mockRedis.Addr()),
				DB:  0,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "debug",
			Format: "json",
		},
		Metrics: config.MetricsConfig{
			Enabled: true,
		},
	}

	if authConfig.RateLimit != nil {
		cfg.RateLimit = config.RateLimitConfig{
			Enabled:        true,
			RequestsPerSec: authConfig.RateLimit.RequestsPerSecond,
			Burst:          authConfig.RateLimit.BurstSize,
		}
	}

	return cfg
}

func (s *AuthTestSuite) initializeGatewayComponents(t *testing.T, cfg *config.Config) *gatewayComponents {
	authProvider, err := auth.InitializeAuthenticationProvider(cfg.Auth, s.logger)
	require.NoError(t, err)

	sessionManager, err := session.CreateSessionStorageManager(context.Background(), cfg.Sessions, s.logger)
	require.NoError(t, err)

	mockDiscovery := s.createMockDiscovery(t)
	metricsRegistry := metrics.InitializeMetricsRegistry()
	requestRouter := router.InitializeRequestRouter(context.Background(), cfg.Routing, mockDiscovery, metricsRegistry, s.logger)
	healthChecker := health.CreateHealthMonitor(mockDiscovery, s.logger)
	rateLimiter := ratelimit.CreateLocalMemoryRateLimiter(s.logger)

	return &gatewayComponents{
		authProvider:    authProvider,
		sessionManager:  sessionManager,
		mockDiscovery:   mockDiscovery,
		metricsRegistry: metricsRegistry,
		requestRouter:   requestRouter,
		healthChecker:   healthChecker,
		rateLimiter:     rateLimiter,
	}
}

func (s *AuthTestSuite) createMockDiscovery(t *testing.T) *mockServiceDiscovery {
	mockDiscovery := &mockServiceDiscovery{
		endpoints: make(map[string][]discovery.Endpoint),
	}

	// Register mock backend
	backendHost, backendPortStr, err := net.SplitHostPort(s.mockBackend.URL[7:]) // Remove "http://"
	require.NoError(t, err)

	backendPort := 8080
	fmt.Sscanf(backendPortStr, "%d", &backendPort)

	err = mockDiscovery.RegisterEndpoint("test", discovery.Endpoint{
		Address: backendHost,
		Port:    backendPort,
		Healthy: true,
	})
	require.NoError(t, err)

	return mockDiscovery
}

func (s *AuthTestSuite) createGatewayServer(cfg *config.Config, components *gatewayComponents) *server.Gateway {
	return server.BootstrapGatewayServer(
		cfg,
		components.authProvider,
		components.sessionManager,
		components.requestRouter,
		components.healthChecker,
		components.metricsRegistry,
		components.rateLimiter,
		s.logger,
	)
}

func (s *AuthTestSuite) buildAuthConfig(authConfig *AuthConfig) config.AuthConfig {
	cfg := config.AuthConfig{
		Provider: authConfig.Type,
	}

	switch authConfig.Type {
	case "jwt":
		cfg.JWT = config.JWTConfig{
			Issuer:       authConfig.JWT.Issuer,
			Audience:     authConfig.JWT.Audience,
			SecretKeyEnv: "TEST_JWT_SECRET",
		}
	case "oauth2":
		cfg.OAuth2 = config.OAuth2Config{
			AuthorizeURL: authConfig.OAuth2.AuthorizeURL,
			TokenURL:     authConfig.OAuth2.TokenURL,
			ClientID:     authConfig.OAuth2.ClientID,
			ClientSecret: authConfig.OAuth2.ClientSecret,
			Scopes:       authConfig.OAuth2.Scopes,
		}
	case "mtls":
		cfg.MTLS = config.MTLSConfig{
			CAFile:     "ca.pem",
			RequiredCN: authConfig.mTLS.RequiredCN,
		}
	case "multi":
		cfg.JWT = config.JWTConfig{
			Issuer:       authConfig.JWT.Issuer,
			Audience:     authConfig.JWT.Audience,
			SecretKeyEnv: "TEST_JWT_SECRET",
		}
		cfg.MTLS = config.MTLSConfig{
			CAFile:     "ca.pem",
			RequiredCN: authConfig.mTLS.RequiredCN,
		}
	}

	return cfg
}

func (s *AuthTestSuite) setAuthEnvironmentVariables(authConfig *AuthConfig) {
	if authConfig.JWT != nil {
		os.Setenv("TEST_JWT_SECRET", authConfig.JWT.SecretKey)
	}
}

func (s *AuthTestSuite) startGatewayServer(t *testing.T, gateway *server.GatewayServer, cfg *config.Config) string {
	serverReady := make(chan string)
	errorChan := make(chan error, 1)

	go func() {
		// Create a custom listener to get the actual port
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			errorChan <- fmt.Errorf("failed to start gateway server listener: %w", err)
			return
		}

		// Get the actual port assigned
		addr := listener.Addr().(*net.TCPAddr)
		cfg.Server.Port = addr.Port

		// Close the listener so the server can bind to it
		_ = listener.Close()

		serverReady <- fmt.Sprintf("127.0.0.1:%d", addr.Port)

		// Start the actual gateway server
		if err := gateway.Start(); err != nil && err != http.ErrServerClosed {
			errorChan <- fmt.Errorf("gateway server error: %w", err)
		}
	}()

	// Wait for server startup
	select {
	case serverAddr := <-serverReady:
		time.Sleep(testIterations * time.Millisecond)
		return serverAddr
	case err := <-errorChan:
		t.Fatalf("Failed to start gateway server: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for gateway server to start")
	}

	return ""
}

func (s *AuthTestSuite) shutdownGateway(t *testing.T, gateway *testGateway) {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if gateway.server != nil {
		gateway.server.Shutdown(shutdownCtx)
	}
}

// Helper methods for client creation and testing

func (s *AuthTestSuite) createClientWithAuth(t *testing.T, serverAddr, authType, token string) *testClient {
	wsURL := fmt.Sprintf("ws://%s/", serverAddr)
	headers := http.Header{
		"Authorization": []string{fmt.Sprintf("%s %s", authType, token)},
	}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	if resp.Body != nil {
		resp.Body.Close()
	}

	return &testClient{
		conn:   conn,
		logger: s.logger,
	}
}

func (s *AuthTestSuite) createClientWithMTLS(t *testing.T, serverAddr string, certs *TestCertificates) *testClient {
	// Parse client certificate and key
	clientCert, err := tls.X509KeyPair(certs.ClientCert, certs.ClientKey)
	require.NoError(t, err)

	// Parse CA certificate
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(certs.CACert)

	// Create TLS configuration
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   "localhost",
	}

	// Create custom dialer with TLS config
	dialer := &websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	wsURL := fmt.Sprintf("wss://%s/", serverAddr)
	conn, resp, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	if resp.Body != nil {
		resp.Body.Close()
	}

	return &testClient{
		conn:   conn,
		logger: s.logger,
	}
}

func (s *AuthTestSuite) createClientWithMTLSAndJWT(t *testing.T, serverAddr string, certs *TestCertificates, jwtToken string) *testClient {
	// Parse client certificate and key
	clientCert, err := tls.X509KeyPair(certs.ClientCert, certs.ClientKey)
	require.NoError(t, err)

	// Parse CA certificate
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(certs.CACert)

	// Create TLS configuration
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   "localhost",
	}

	// Create headers with JWT token
	headers := http.Header{
		"Authorization": []string{fmt.Sprintf("Bearer %s", jwtToken)},
	}

	// Create custom dialer with TLS config
	dialer := &websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	wsURL := fmt.Sprintf("wss://%s/", serverAddr)
	conn, resp, err := dialer.Dial(wsURL, headers)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	if resp.Body != nil {
		resp.Body.Close()
	}

	return &testClient{
		conn:   conn,
		logger: s.logger,
	}
}

func (s *AuthTestSuite) tryConnectWithAuth(t *testing.T, serverAddr, authType, token string) (*testClient, error) {
	wsURL := fmt.Sprintf("ws://%s/", serverAddr)
	headers := http.Header{
		"Authorization": []string{fmt.Sprintf("%s %s", authType, token)},
	}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return &testClient{
		conn:   conn,
		logger: s.logger,
	}, nil
}

func (s *AuthTestSuite) tryConnectWithoutAuth(t *testing.T, serverAddr string) (*testClient, error) {
	wsURL := fmt.Sprintf("ws://%s/", serverAddr)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)

	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return &testClient{
		conn:   conn,
		logger: s.logger,
	}, nil
}

func (s *AuthTestSuite) tryConnectWithoutCert(t *testing.T, serverAddr string) (*testClient, error) {
	wsURL := fmt.Sprintf("wss://%s/", serverAddr)
	dialer := &websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return &testClient{
		conn:   conn,
		logger: s.logger,
	}, nil
}

func (s *AuthTestSuite) sendTestRequest(t *testing.T, client *testClient, method string) *mcp.Response {
	request := mcp.Request{
		JSONRPC: "2.0",
		Method:  method,
		ID:      fmt.Sprintf("auth-test-%d", time.Now().UnixNano()),
	}

	return client.SendRequest(t, request)
}

// Helper methods for token generation and OAuth2 simulation

func (s *AuthTestSuite) generateJWTToken(t *testing.T, jwtConfig *JWTAuthConfig, claims map[string]interface{}) string {
	// Set default claims
	if _, ok := claims["iss"]; !ok {
		claims["iss"] = jwtConfig.Issuer
	}
	if _, ok := claims["aud"]; !ok {
		claims["aud"] = jwtConfig.Audience
	}
	if _, ok := claims["iat"]; !ok {
		claims["iat"] = time.Now().Unix()
	}
	if _, ok := claims["nbf"]; !ok {
		claims["nbf"] = time.Now().Unix()
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(claims))
	tokenString, err := token.SignedString([]byte(jwtConfig.SecretKey))
	require.NoError(t, err)

	return tokenString
}

func (s *AuthTestSuite) simulateOAuth2Flow(t *testing.T, oauth *MockOAuth2Server) string {
	// Generate access token
	accessToken := fmt.Sprintf("access_token_%d", time.Now().UnixNano())
	refreshToken := fmt.Sprintf("refresh_token_%d", time.Now().UnixNano())

	tokenInfo := &OAuth2TokenInfo{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Hour),
		Scope:        "mcp:read mcp:write",
		UserID:       "test-user",
	}

	oauth.mu.Lock()
	oauth.validTokens[accessToken] = tokenInfo
	oauth.mu.Unlock()

	return accessToken
}

func (s *AuthTestSuite) simulateTokenRefresh(t *testing.T, oauth *MockOAuth2Server, oldToken string) string {
	oauth.mu.RLock()
	tokenInfo, exists := oauth.validTokens[oldToken]
	oauth.mu.RUnlock()

	if !exists {
		return ""
	}

	// Generate new access token
	newAccessToken := fmt.Sprintf("refreshed_access_token_%d", time.Now().UnixNano())
	newTokenInfo := &OAuth2TokenInfo{
		AccessToken:  newAccessToken,
		RefreshToken: tokenInfo.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Hour),
		Scope:        tokenInfo.Scope,
		UserID:       tokenInfo.UserID,
	}

	oauth.mu.Lock()
	delete(oauth.validTokens, oldToken)
	oauth.validTokens[newAccessToken] = newTokenInfo
	oauth.mu.Unlock()

	return newAccessToken
}

// OAuth2 server handlers

func (oauth *MockOAuth2Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	// Simulate authorization code grant
	code := fmt.Sprintf("auth_code_%d", time.Now().UnixNano())
	redirectURI := r.URL.Query().Get("redirect_uri")

	if redirectURI == "" {
		http.Error(w, "Missing redirect_uri", http.StatusBadRequest)
		return
	}

	redirectURL, _ := url.Parse(redirectURI)
	q := redirectURL.Query()
	q.Set("code", code)
	q.Set("state", r.URL.Query().Get("state"))
	redirectURL.RawQuery = q.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

func (oauth *MockOAuth2Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	grantType := r.Form.Get("grant_type")

	var response map[string]interface{}

	switch grantType {
	case "authorization_code":
		code := r.Form.Get("code")
		if code == "" {
			http.Error(w, "Missing code", http.StatusBadRequest)
			return
		}

		accessToken := fmt.Sprintf("access_token_%d", time.Now().UnixNano())
		refreshToken := fmt.Sprintf("refresh_token_%d", time.Now().UnixNano())

		tokenInfo := &OAuth2TokenInfo{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    time.Now().Add(time.Hour),
			Scope:        "mcp:read mcp:write",
			UserID:       "test-user",
		}

		oauth.mu.Lock()
		oauth.validTokens[accessToken] = tokenInfo
		oauth.mu.Unlock()

		response = map[string]interface{}{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         "mcp:read mcp:write",
		}

	case "refresh_token":
		refreshToken := r.Form.Get("refresh_token")
		if refreshToken == "" {
			http.Error(w, "Missing refresh_token", http.StatusBadRequest)
			return
		}

		// Find token by refresh token
		var oldTokenInfo *OAuth2TokenInfo
		oauth.mu.RLock()
		for _, info := range oauth.validTokens {
			if info.RefreshToken == refreshToken {
				oldTokenInfo = info
				break
			}
		}
		oauth.mu.RUnlock()

		if oldTokenInfo == nil {
			http.Error(w, "Invalid refresh token", http.StatusUnauthorized)
			return
		}

		newAccessToken := fmt.Sprintf("refreshed_access_token_%d", time.Now().UnixNano())
		newTokenInfo := &OAuth2TokenInfo{
			AccessToken:  newAccessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    time.Now().Add(time.Hour),
			Scope:        oldTokenInfo.Scope,
			UserID:       oldTokenInfo.UserID,
		}

		oauth.mu.Lock()
		oauth.validTokens[newAccessToken] = newTokenInfo
		oauth.mu.Unlock()

		response = map[string]interface{}{
			"access_token": newAccessToken,
			"token_type":   "Bearer",
			"expires_in":   3600,
			"scope":        oldTokenInfo.Scope,
		}

	default:
		http.Error(w, "Unsupported grant type", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (oauth *MockOAuth2Server) handleIntrospect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	token := r.Form.Get("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusBadRequest)
		return
	}

	oauth.mu.RLock()
	tokenInfo, exists := oauth.validTokens[token]
	oauth.mu.RUnlock()

	response := map[string]interface{}{
		"active": exists && time.Now().Before(tokenInfo.ExpiresAt),
	}

	if exists && time.Now().Before(tokenInfo.ExpiresAt) {
		response["client_id"] = oauth.clientID
		response["scope"] = tokenInfo.Scope
		response["exp"] = tokenInfo.ExpiresAt.Unix()
		response["sub"] = tokenInfo.UserID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (oauth *MockOAuth2Server) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
		return
	}

	token := authHeader[7:] // Remove "Bearer "

	oauth.mu.RLock()
	tokenInfo, exists := oauth.validTokens[token]
	oauth.mu.RUnlock()

	if !exists || time.Now().After(tokenInfo.ExpiresAt) {
		http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	response := map[string]interface{}{
		"sub":   tokenInfo.UserID,
		"email": tokenInfo.UserID + "@example.com",
		"name":  "Test User",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
