//go:build integration
// +build integration

package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
	"github.com/poiley/mcp-bridge/services/gateway/internal/health"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
	"github.com/poiley/mcp-bridge/services/gateway/internal/ratelimit"
	"github.com/poiley/mcp-bridge/services/gateway/internal/router"
	"github.com/poiley/mcp-bridge/services/gateway/internal/server"
	"github.com/poiley/mcp-bridge/services/gateway/internal/session"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestEnvironment holds all the components for integration testing
type TestEnvironment struct {
	Config        *config.Config
	Gateway       *TestGatewayServer
	MockRedis     *miniredis.Miniredis
	MockDiscovery *MockServiceDiscovery
	Logger        *zap.Logger
	ServerAddr    string
	MetricsAddr   string
	JWTPrivateKey *rsa.PrivateKey
	JWTPublicKey  *rsa.PublicKey
	TempDir       string
}

// TestGatewayServer wraps the real gateway server for testing
type TestGatewayServer struct {
	*server.GatewayServer
	httpServer *http.Server
	listener   net.Listener
}

// SetupTestEnvironment creates a complete test environment
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
	logger := zaptest.NewLogger(t)

	// Create temp directory for test files
	tempDir := t.TempDir()

	// Generate RSA keys for JWT
	privateKey, publicKey := generateTestKeys(t)

	// Save public key to file
	publicKeyPath := filepath.Join(tempDir, "public.pem")
	savePublicKey(t, publicKey, publicKeyPath)

	// Start mock Redis
	mockRedis, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start mock Redis: %v", err)
	}

	// Create configuration
	cfg := &config.Config{
		Version: 1,
		Server: config.ServerConfig{
			Port:                 0, // Random port
			MetricsPort:          0, // Random port
			MaxConnections:       testIterations,
			ConnectionBufferSize: 65536,
		},
		Auth: config.AuthConfig{
			Provider: "jwt",
			JWT: config.JWTConfig{
				Issuer:        "test-gateway",
				Audience:      "test-gateway",
				PublicKeyPath: publicKeyPath,
			},
		},
		Routing: config.RoutingConfig{
			Strategy:            "round_robin",
			HealthCheckInterval: "100ms",
			CircuitBreaker: config.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 5,
				SuccessThreshold: 2,
				TimeoutSeconds:   30,
			},
		},
		Discovery: config.ServiceDiscoveryConfig{
			Provider:          "kubernetes",
			NamespaceSelector: []string{"test", "default"},
		},
		Sessions: config.SessionConfig{
			Provider: "redis",
			Redis: config.RedisConfig{
				URL: fmt.Sprintf("redis://%s", mockRedis.Addr()),
				DB:  0,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "debug",
			Format: "json",
		},
		Metrics: config.MetricsConfig{
			Enabled: true,
			Path:    "/metrics",
		},
		RateLimit: config.RateLimitConfig{
			Enabled:        false, // Disable for testing
			RequestsPerSec: testIterations,
			Burst:          10,
		},
	}

	// Create mock discovery
	mockDiscovery := NewMockServiceDiscovery()

	// Create components
	authProvider, err := auth.InitializeAuthenticationProvider(cfg.Auth, logger)
	if err != nil {
		t.Fatalf("Failed to create auth provider: %v", err)
	}

	sessionManager, err := session.CreateSessionStorageManager(context.Background(), cfg.Sessions, logger)
	if err != nil {
		t.Fatalf("Failed to create session manager: %v", err)
	}

	metricsRegistry := metrics.InitializeMetricsRegistry()
	requestRouter := router.InitializeRequestRouter(context.Background(), cfg.Routing, mockDiscovery, metricsRegistry, logger)
	healthChecker := health.CreateHealthMonitor(mockDiscovery, logger)
	rateLimiter := ratelimit.CreateLocalMemoryRateLimiter(logger)

	// Create gateway server
	gateway := server.BootstrapGatewayServer(
		cfg,
		authProvider,
		sessionManager,
		requestRouter,
		healthChecker,
		metricsRegistry,
		rateLimiter,
		logger,
	)

	// Create test wrapper
	testGateway := &TestGatewayServer{
		GatewayServer: gateway,
	}

	env := &TestEnvironment{
		Config:        cfg,
		Gateway:       testGateway,
		MockRedis:     mockRedis,
		MockDiscovery: mockDiscovery,
		Logger:        logger,
		JWTPrivateKey: privateKey,
		JWTPublicKey:  publicKey,
		TempDir:       tempDir,
	}

	// Start server
	env.startServer(t)

	// Start metrics server
	env.startMetricsServer(t)

	return env
}

// startServer starts the WebSocket server on a random port
func (env *TestEnvironment) startServer(t *testing.T) {
	// Create HTTP mux
	mux := http.NewServeMux()

	// We need to expose the WebSocket handler
	// Since handleWebSocket is private, we'll create a public wrapper
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// For testing, we'll need to either:
		// 1. Make handleWebSocket public in the server package
		// 2. Use reflection to call it
		// 3. Create a test-specific handler

		// For now, we'll create a simple handler that calls the server's public methods
		env.Gateway.ServeHTTP(w, r)
	})

	// Create listener on random port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	env.ServerAddr = listener.Addr().String()
	env.Gateway.listener = listener

	// Create HTTP server
	httpServer := &http.Server{
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 10 * time.Second, // G112: Prevent Slowloris attacks
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	env.Gateway.httpServer = httpServer

	// Start server in background
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Logf("Server error: %v", err)
		}
	}()

	// Wait for server to be ready
	time.Sleep(testIterations * time.Millisecond)
}

// startMetricsServer starts the metrics server on a random port
func (env *TestEnvironment) startMetricsServer(t *testing.T) {
	mux := http.NewServeMux()
	// Add Prometheus metrics handler
	mux.Handle(env.Config.Metrics.Path, promhttp.Handler())

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to create metrics listener: %v", err)
	}

	env.MetricsAddr = listener.Addr().String()

	metricsServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second, // G112: Prevent Slowloris attacks
	}

	go func() {
		if err := metricsServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Logf("Metrics server error: %v", err)
		}
	}()

	time.Sleep(testIterations * time.Millisecond)
}

// ServeHTTP implements http.Handler for TestGatewayServer
func (tgs *TestGatewayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// This is a simplified version for testing
	// In a real implementation, we'd need to expose the handleWebSocket method
	// or create a test-specific WebSocket handler

	// For now, return a simple response
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Test Gateway Server"))
}

// Cleanup cleans up all test resources
func (env *TestEnvironment) Cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shutdown gateway
	if env.Gateway != nil {
		env.Gateway.Shutdown(ctx)
		if env.Gateway.httpServer != nil {
			env.Gateway.httpServer.Shutdown(ctx)
		}
	}

	// Close Redis
	if env.MockRedis != nil {
		env.MockRedis.Close()
	}

	// Clean up temp directory
	os.RemoveAll(env.TempDir)
}

// GenerateTestToken generates a valid JWT token for testing
func (env *TestEnvironment) GenerateTestToken(subject string, scopes []string) (string, error) {
	now := time.Now()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    env.Config.Auth.JWT.Issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{env.Config.Auth.JWT.Audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Scopes: scopes,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(env.JWTPrivateKey)
}

// Helper functions

func generateTestKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}
	return privateKey, &privateKey.PublicKey
}

func savePublicKey(t *testing.T, publicKey *rsa.PublicKey, path string) {
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("Failed to marshal public key: %v", err)
	}

	pubKeyPEM := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create public key file: %v", err)
	}
	defer func() { _ = file.Close() }()

	if err := pem.Encode(file, pubKeyPEM); err != nil {
		t.Fatalf("Failed to write public key: %v", err)
	}
}

// MockServiceDiscovery implements discovery.ServiceDiscovery for testing
type MockServiceDiscovery struct {
	endpoints map[string][]discovery.Endpoint
	mu        sync.RWMutex
}

func NewMockServiceDiscovery() *MockServiceDiscovery {
	return &MockServiceDiscovery{
		endpoints: make(map[string][]discovery.Endpoint),
	}
}

func (m *MockServiceDiscovery) GetEndpoints(namespace string) []discovery.Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]discovery.Endpoint{}, m.endpoints[namespace]...)
}

func (m *MockServiceDiscovery) GetAllEndpoints() map[string][]discovery.Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]discovery.Endpoint)
	for k, v := range m.endpoints {
		result[k] = append([]discovery.Endpoint{}, v...)
	}
	return result
}

func (m *MockServiceDiscovery) RegisterEndpoint(namespace string, endpoint discovery.Endpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.endpoints[namespace] = append(m.endpoints[namespace], endpoint)
	return nil
}

func (m *MockServiceDiscovery) DeregisterEndpoint(namespace string, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	endpoints := m.endpoints[namespace]
	for i, ep := range endpoints {
		if ep.Address == address {
			m.endpoints[namespace] = append(endpoints[:i], endpoints[i+1:]...)
			break
		}
	}
	return nil
}

func (m *MockServiceDiscovery) Start(ctx context.Context) error {
	return nil
}

func (m *MockServiceDiscovery) Stop() {
	// no-op
}

func (m *MockServiceDiscovery) ListNamespaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	namespaces := make([]string, 0, len(m.endpoints))
	for ns := range m.endpoints {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

func (m *MockServiceDiscovery) Close() error {
	return nil
}
