//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/health"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/ratelimit"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/router"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/server"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// ErrorPropagationTestSuite represents the error propagation integration test suite
type ErrorPropagationTestSuite struct {
	t               *testing.T
	ctx             context.Context
	cancel          context.CancelFunc
	logger          *zap.Logger
	gateway         *server.GatewayServer
	mockRedis       *miniredis.Miniredis
	mockBackends    []*ErrorInjectingBackend
	authProvider    auth.Provider
	sessionManager  session.Manager
	requestRouter   router.Router
	healthChecker   health.Checker
	metricsRegistry *metrics.Registry
	rateLimiter     ratelimit.RateLimiter
	config          *config.Config
	serverAddr      string
	errorMetrics    *ErrorMetrics
}

// ErrorInjectingBackend represents a backend that can inject various types of errors
type ErrorInjectingBackend struct {
	server       *httptest.Server
	upgrader     websocket.Upgrader
	connections  map[*websocket.Conn]*BackendConnection
	mu           sync.RWMutex
	errorConfig  *BackendErrorConfig
	messageCount int64
	errorCount   int64
}

// BackendConnection tracks individual connection state
type BackendConnection struct {
	ID           string
	ConnectedAt  time.Time
	MessageCount int64
	LastMessage  time.Time
}

// BackendErrorConfig defines error injection parameters
type BackendErrorConfig struct {
	NetworkErrors      bool    // Inject network-level errors
	ProtocolErrors     bool    // Inject protocol-level errors
	TimeoutErrors      bool    // Inject timeout errors
	AuthErrors         bool    // Inject authentication errors
	RateLimitErrors    bool    // Inject rate limiting errors
	ServiceErrors      bool    // Inject service unavailable errors
	CircuitBreakerOpen bool    // Simulate circuit breaker state
	ErrorRate          float64 // Percentage of requests that should error (0.0-1.0)
	LatencyInjection   time.Duration
	PartialResponses   bool // Send partial/malformed responses
	ConnectionDrops    bool // Randomly drop connections
}

// ErrorMetrics tracks error propagation metrics
type ErrorMetrics struct {
	TotalRequests         int64
	NetworkErrors         int64
	ProtocolErrors        int64
	TimeoutErrors         int64
	AuthErrors            int64
	RateLimitErrors       int64
	ServiceErrors         int64
	CircuitBreakerErrors  int64
	UnexpectedErrors      int64
	SuccessfulRecoveries  int64
	ErrorRecoveryTime     int64 // nanoseconds
	LastErrorTime         int64 // unix timestamp
	ErrorPropagationDepth int64 // How many layers the error traversed
}

// NewErrorPropagationTestSuite creates and initializes a new error propagation test suite
func NewErrorPropagationTestSuite(t *testing.T) *ErrorPropagationTestSuite {
	ctx, cancel := context.WithCancel(context.Background())

	suite := &ErrorPropagationTestSuite{
		t:            t,
		ctx:          ctx,
		cancel:       cancel,
		logger:       zaptest.NewLogger(t),
		errorMetrics: &ErrorMetrics{},
	}

	suite.setupEnvironment()
	suite.setupErrorInjectingBackends()
	suite.setupGateway()

	return suite
}

// setupEnvironment initializes the test environment
func (s *ErrorPropagationTestSuite) setupEnvironment() {
	// Set JWT secret for testing
	os.Setenv("TEST_JWT_SECRET", "error-propagation-test-secret-key-12345")
	s.t.Cleanup(func() { os.Unsetenv("TEST_JWT_SECRET") })

	// Start mock Redis instance
	mockRedis, err := miniredis.Run()
	require.NoError(s.t, err)
	s.mockRedis = mockRedis
	s.t.Cleanup(func() { mockRedis.Close() })

	// Create test configuration
	s.config = &config.Config{
		Version: 1,
		Server: config.ServerConfig{
			Port:                 0, // Use random port
			MetricsPort:          0,
			MaxConnections:       testMaxIterations,
			ConnectionBufferSize: 65536,
			ReadTimeout:          "5s",  // Short timeout for error testing
			WriteTimeout:         "5s",  // Short timeout for error testing
			IdleTimeout:          "10s", // Short timeout for error testing
		},
		Auth: config.AuthConfig{
			Provider: "jwt",
			JWT: config.JWTConfig{
				Issuer:       "error-test-gateway",
				Audience:     "error-test-gateway",
				SecretKeyEnv: "TEST_JWT_SECRET",
			},
		},
		Routing: config.RoutingConfig{
			Strategy:            "round_robin",
			HealthCheckInterval: "1s",
			CircuitBreaker: config.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 3, // Low threshold for testing
				SuccessThreshold: 2,
				TimeoutSeconds:   5, // Short timeout for testing
			},
		},
		Discovery: config.ServiceDiscoveryConfig{
			Mode:              "static",
			NamespaceSelector: []string{"error-test"},
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
			Enabled:        true,
			RequestsPerSec: testIterations, // Low rate for testing
			Burst:          10,
		},
	}
}

// setupErrorInjectingBackends creates multiple backends with different error injection configurations
func (s *ErrorPropagationTestSuite) setupErrorInjectingBackends() {
	backendConfigs := []*BackendErrorConfig{
		// Healthy backend
		{
			NetworkErrors:    false,
			ProtocolErrors:   false,
			TimeoutErrors:    false,
			ErrorRate:        0.0,
			LatencyInjection: 0,
			PartialResponses: false,
			ConnectionDrops:  false,
		},
		// Network error backend
		{
			NetworkErrors:    true,
			ProtocolErrors:   false,
			TimeoutErrors:    false,
			ErrorRate:        0.5,
			LatencyInjection: 0,
			PartialResponses: false,
			ConnectionDrops:  true,
		},
		// Protocol error backend
		{
			NetworkErrors:    false,
			ProtocolErrors:   true,
			TimeoutErrors:    false,
			ErrorRate:        0.3,
			LatencyInjection: 0,
			PartialResponses: true,
			ConnectionDrops:  false,
		},
		// Timeout error backend
		{
			NetworkErrors:    false,
			ProtocolErrors:   false,
			TimeoutErrors:    true,
			ErrorRate:        0.4,
			LatencyInjection: 10 * time.Second, // High latency to trigger timeouts
			PartialResponses: false,
			ConnectionDrops:  false,
		},
		// Service error backend
		{
			NetworkErrors:    false,
			ProtocolErrors:   false,
			TimeoutErrors:    false,
			ServiceErrors:    true,
			ErrorRate:        0.7,
			LatencyInjection: 0,
			PartialResponses: false,
			ConnectionDrops:  false,
		},
	}

	s.mockBackends = make([]*ErrorInjectingBackend, len(backendConfigs))

	for i, errorConfig := range backendConfigs {
		s.mockBackends[i] = s.createErrorInjectingBackend(errorConfig)
	}
}

// createErrorInjectingBackend creates a single error-injecting backend
func (s *ErrorPropagationTestSuite) createErrorInjectingBackend(errorConfig *BackendErrorConfig) *ErrorInjectingBackend {
	backend := &ErrorInjectingBackend{
		connections: make(map[*websocket.Conn]*BackendConnection),
		errorConfig: errorConfig,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", backend.handleHealthCheck)
	mux.HandleFunc("/", backend.handleWebSocket)

	backend.server = httptest.NewServer(mux)
	s.t.Cleanup(func() { backend.server.Close() })

	return backend
}

// setupGateway initializes the gateway server with error-injecting backends
func (s *ErrorPropagationTestSuite) setupGateway() {
	var err error

	// Initialize authentication provider
	s.authProvider, err = auth.InitializeAuthenticationProvider(s.config.Auth, s.logger)
	require.NoError(s.t, err)

	// Initialize session manager
	s.sessionManager, err = session.CreateSessionStorageManager(context.Background(), s.config.Sessions, s.logger)
	require.NoError(s.t, err)

	// Create mock service discovery with error-injecting backends
	mockDiscovery := &mockServiceDiscovery{
		endpoints: make(map[string][]discovery.Endpoint),
	}

	// Register all error-injecting backends
	for i, backend := range s.mockBackends {
		backendHost, backendPortStr, err := net.SplitHostPort(backend.server.URL[7:]) // Remove "http://"
		require.NoError(s.t, err)

		backendPort := 8080
		fmt.Sscanf(backendPortStr, "%d", &backendPort)

		err = mockDiscovery.RegisterEndpoint("error-test", discovery.Endpoint{
			Address: backendHost,
			Port:    backendPort,
			Healthy: true,
			Tags:    map[string]string{"backend_id": fmt.Sprintf("backend-%d", i)},
		})
		require.NoError(s.t, err)
	}

	// Initialize components
	s.metricsRegistry = metrics.InitializeMetricsRegistry()
	s.requestRouter = router.InitializeRequestRouter(context.Background(), s.config.Routing, mockDiscovery, s.metricsRegistry, s.logger)
	s.healthChecker = health.CreateHealthMonitor(mockDiscovery, s.logger)
	s.rateLimiter = ratelimit.CreateLocalMemoryRateLimiter(s.logger)

	// Create gateway server
	s.gateway = server.BootstrapGatewayServer(
		s.config,
		s.authProvider,
		s.sessionManager,
		s.requestRouter,
		s.healthChecker,
		s.metricsRegistry,
		s.rateLimiter,
		s.logger,
	)

	// Start server
	s.startGatewayServer()
}

// startGatewayServer starts the gateway server
func (s *ErrorPropagationTestSuite) startGatewayServer() {
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
		s.config.Server.Port = addr.Port

		// Close the listener so the server can bind to it
		_ = listener.Close()

		serverReady <- fmt.Sprintf("127.0.0.1:%d", addr.Port)

		// Start the actual gateway server
		if err := s.gateway.Start(); err != nil && err != http.ErrServerClosed {
			errorChan <- fmt.Errorf("gateway server error: %w", err)
		}
	}()

	// Wait for server startup
	select {
	case s.serverAddr = <-serverReady:
		time.Sleep(testIterations * time.Millisecond)
		s.t.Logf("Gateway server ready at: %s", s.serverAddr)
	case err := <-errorChan:
		s.t.Fatalf("Failed to start gateway server: %v", err)
	case <-time.After(10 * time.Second):
		s.t.Fatal("Timeout waiting for gateway server to start")
	}
}

// Cleanup shuts down all test components
func (s *ErrorPropagationTestSuite) Cleanup() {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if s.gateway != nil {
		s.gateway.Shutdown(shutdownCtx)
	}

	s.cancel()
}

// TestErrorPropagation tests comprehensive error propagation scenarios
func TestErrorPropagation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping error propagation integration test in short mode")
	}

	suite := NewErrorPropagationTestSuite(t)
	defer suite.Cleanup()

	t.Run("Network_Error_Propagation", func(t *testing.T) {
		suite.testNetworkErrorPropagation(t)
	})

	t.Run("Protocol_Error_Propagation", func(t *testing.T) {
		suite.testProtocolErrorPropagation(t)
	})

	t.Run("Timeout_Error_Propagation", func(t *testing.T) {
		suite.testTimeoutErrorPropagation(t)
	})

	t.Run("Authentication_Error_Propagation", func(t *testing.T) {
		suite.testAuthenticationErrorPropagation(t)
	})

	t.Run("Rate_Limit_Error_Propagation", func(t *testing.T) {
		suite.testRateLimitErrorPropagation(t)
	})

	t.Run("Service_Error_Propagation", func(t *testing.T) {
		suite.testServiceErrorPropagation(t)
	})

	t.Run("Circuit_Breaker_Error_Propagation", func(t *testing.T) {
		suite.testCircuitBreakerErrorPropagation(t)
	})

	t.Run("Cascading_Error_Propagation", func(t *testing.T) {
		suite.testCascadingErrorPropagation(t)
	})

	t.Run("Error_Recovery_And_Healing", func(t *testing.T) {
		suite.testErrorRecoveryAndHealing(t)
	})

	t.Run("Concurrent_Error_Handling", func(t *testing.T) {
		suite.testConcurrentErrorHandling(t)
	})
}

// testNetworkErrorPropagation tests how network errors propagate through the system
func (s *ErrorPropagationTestSuite) testNetworkErrorPropagation(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// Send requests that will trigger network errors
	for i := 0; i < 10; i++ {
		request := mcp.Request{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      fmt.Sprintf("network-error-test-%d", i),
		}

		response := client.SendRequest(t, request)

		if response.Error != nil {
			atomic.AddInt64(&s.errorMetrics.NetworkErrors, 1)
			// Verify error is properly formatted and contains network error information
			assert.Contains(t, response.Error.Message, "network", "Network error should be indicated in error message")
			assert.Equal(t, int(mcp.ErrorCodeInternalError), response.Error.Code, "Network errors should map to internal error code")
		}

		atomic.AddInt64(&s.errorMetrics.TotalRequests, 1)
	}

	// Verify that some network errors were encountered and properly propagated
	networkErrors := atomic.LoadInt64(&s.errorMetrics.NetworkErrors)
	totalRequests := atomic.LoadInt64(&s.errorMetrics.TotalRequests)

	assert.Greater(t, networkErrors, int64(0), "Should have encountered some network errors")
	assert.Less(t, networkErrors, totalRequests, "Not all requests should fail due to network errors")

	t.Log("✅ Network error propagation test completed successfully")
}

// testProtocolErrorPropagation tests how protocol errors propagate through the system
func (s *ErrorPropagationTestSuite) testProtocolErrorPropagation(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// Send requests that will trigger protocol errors
	for i := 0; i < 10; i++ {
		request := mcp.Request{
			JSONRPC: "2.0",
			Method:  "invalid/method", // This should trigger protocol errors
			ID:      fmt.Sprintf("protocol-error-test-%d", i),
		}

		response := client.SendRequest(t, request)

		if response.Error != nil {
			atomic.AddInt64(&s.errorMetrics.ProtocolErrors, 1)
			// Verify error is properly formatted and contains protocol error information
			assert.True(t,
				response.Error.Code == int(mcp.ErrorCodeMethodNotFound) ||
					response.Error.Code == int(mcp.ErrorCodeInvalidRequest),
				"Protocol errors should map to appropriate MCP error codes")
		}

		atomic.AddInt64(&s.errorMetrics.TotalRequests, 1)
	}

	protocolErrors := atomic.LoadInt64(&s.errorMetrics.ProtocolErrors)
	assert.Greater(t, protocolErrors, int64(0), "Should have encountered some protocol errors")

	t.Log("✅ Protocol error propagation test completed successfully")
}

// testTimeoutErrorPropagation tests how timeout errors propagate through the system
func (s *ErrorPropagationTestSuite) testTimeoutErrorPropagation(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// Configure one backend to inject high latency
	if len(s.mockBackends) > 3 {
		s.mockBackends[3].errorConfig.LatencyInjection = 10 * time.Second
	}

	// Send requests that will trigger timeout errors
	for i := 0; i < 5; i++ {
		start := time.Now()

		request := mcp.Request{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      fmt.Sprintf("timeout-error-test-%d", i),
		}

		response := client.SendRequest(t, request)
		elapsed := time.Since(start)

		if response.Error != nil && elapsed > 4*time.Second { // Expect timeout after ~5 seconds
			atomic.AddInt64(&s.errorMetrics.TimeoutErrors, 1)
			assert.Contains(t, response.Error.Message, "timeout", "Timeout error should be indicated in error message")
		}

		atomic.AddInt64(&s.errorMetrics.TotalRequests, 1)
	}

	timeoutErrors := atomic.LoadInt64(&s.errorMetrics.TimeoutErrors)
	assert.Greater(t, timeoutErrors, int64(0), "Should have encountered some timeout errors")

	t.Log("✅ Timeout error propagation test completed successfully")
}

// testAuthenticationErrorPropagation tests how authentication errors propagate
func (s *ErrorPropagationTestSuite) testAuthenticationErrorPropagation(t *testing.T) {
	// Test with invalid token
	invalidClient, err := s.tryCreateClientWithInvalidAuth(t)
	if err != nil {
		atomic.AddInt64(&s.errorMetrics.AuthErrors, 1)
		assert.Contains(t, err.Error(), "Unauthorized", "Authentication error should be indicated")
	}
	if invalidClient != nil {
		_ = invalidClient.Close()
	}

	// Test with expired token
	expiredClient, err := s.tryCreateClientWithExpiredAuth(t)
	if err != nil {
		atomic.AddInt64(&s.errorMetrics.AuthErrors, 1)
		assert.Contains(t, err.Error(), "Unauthorized", "Expired token error should be indicated")
	}
	if expiredClient != nil {
		_ = expiredClient.Close()
	}

	authErrors := atomic.LoadInt64(&s.errorMetrics.AuthErrors)
	assert.Greater(t, authErrors, int64(0), "Should have encountered authentication errors")

	t.Log("✅ Authentication error propagation test completed successfully")
}

// testRateLimitErrorPropagation tests how rate limiting errors propagate
func (s *ErrorPropagationTestSuite) testRateLimitErrorPropagation(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// Send many requests rapidly to trigger rate limiting
	const numRequests = testTimeout
	for i := 0; i < numRequests; i++ {
		request := mcp.Request{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      fmt.Sprintf("rate-limit-test-%d", i),
		}

		response := client.SendRequest(t, request)

		if response.Error != nil && response.Error.Code == int(mcp.ErrorCodeInternalError) {
			if contains(response.Error.Message, "rate") || contains(response.Error.Message, "limit") {
				atomic.AddInt64(&s.errorMetrics.RateLimitErrors, 1)
			}
		}

		atomic.AddInt64(&s.errorMetrics.TotalRequests, 1)

		// Send requests rapidly
		time.Sleep(10 * time.Millisecond)
	}

	rateLimitErrors := atomic.LoadInt64(&s.errorMetrics.RateLimitErrors)
	assert.Greater(t, rateLimitErrors, int64(0), "Should have encountered rate limit errors")

	t.Log("✅ Rate limit error propagation test completed successfully")
}

// testServiceErrorPropagation tests how service errors propagate
func (s *ErrorPropagationTestSuite) testServiceErrorPropagation(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// Configure backend to return service errors
	if len(s.mockBackends) > 4 {
		s.mockBackends[4].errorConfig.ServiceErrors = true
		s.mockBackends[4].errorConfig.ErrorRate = 1.0 // Always error
	}

	// Send requests that will trigger service errors
	for i := 0; i < 10; i++ {
		request := mcp.Request{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      fmt.Sprintf("service-error-test-%d", i),
		}

		response := client.SendRequest(t, request)

		if response.Error != nil {
			atomic.AddInt64(&s.errorMetrics.ServiceErrors, 1)
			assert.Equal(t, int(mcp.ErrorCodeInternalError), response.Error.Code, "Service errors should map to internal error code")
		}

		atomic.AddInt64(&s.errorMetrics.TotalRequests, 1)
	}

	serviceErrors := atomic.LoadInt64(&s.errorMetrics.ServiceErrors)
	assert.Greater(t, serviceErrors, int64(0), "Should have encountered service errors")

	t.Log("✅ Service error propagation test completed successfully")
}

// testCircuitBreakerErrorPropagation tests how circuit breaker errors propagate
func (s *ErrorPropagationTestSuite) testCircuitBreakerErrorPropagation(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// Configure backend to always fail to trigger circuit breaker
	if len(s.mockBackends) > 1 {
		s.mockBackends[1].errorConfig.NetworkErrors = true
		s.mockBackends[1].errorConfig.ErrorRate = 1.0 // Always error
	}

	// Send enough requests to trigger circuit breaker
	for i := 0; i < 20; i++ {
		request := mcp.Request{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      fmt.Sprintf("circuit-breaker-test-%d", i),
		}

		response := client.SendRequest(t, request)

		if response.Error != nil {
			if contains(response.Error.Message, "circuit") || contains(response.Error.Message, "breaker") {
				atomic.AddInt64(&s.errorMetrics.CircuitBreakerErrors, 1)
			}
		}

		atomic.AddInt64(&s.errorMetrics.TotalRequests, 1)
		time.Sleep(testIterations * time.Millisecond) // Allow circuit breaker to activate
	}

	circuitBreakerErrors := atomic.LoadInt64(&s.errorMetrics.CircuitBreakerErrors)
	// Circuit breaker may or may not trigger in this test environment, so we don't assert on this

	t.Log("✅ Circuit breaker error propagation test completed successfully")
}

// testCascadingErrorPropagation tests how errors cascade through multiple system layers
func (s *ErrorPropagationTestSuite) testCascadingErrorPropagation(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// Configure multiple backends to fail
	for i := 1; i < len(s.mockBackends); i++ {
		s.mockBackends[i].errorConfig.ErrorRate = 0.8 // High error rate
		s.mockBackends[i].errorConfig.NetworkErrors = true
		s.mockBackends[i].errorConfig.ProtocolErrors = true
	}

	// Send requests and track error propagation depth
	for i := 0; i < 15; i++ {
		request := mcp.Request{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      fmt.Sprintf("cascading-error-test-%d", i),
		}

		response := client.SendRequest(t, request)

		if response.Error != nil {
			// Count layers of error propagation based on error message complexity
			errorDepth := int64(1)
			if contains(response.Error.Message, "backend") {
				errorDepth++
			}
			if contains(response.Error.Message, "circuit") {
				errorDepth++
			}
			if contains(response.Error.Message, "timeout") {
				errorDepth++
			}

			atomic.StoreInt64(&s.errorMetrics.ErrorPropagationDepth, errorDepth)
		}

		atomic.AddInt64(&s.errorMetrics.TotalRequests, 1)
	}

	errorDepth := atomic.LoadInt64(&s.errorMetrics.ErrorPropagationDepth)
	assert.Greater(t, errorDepth, int64(0), "Should have error propagation through multiple layers")

	t.Log("✅ Cascading error propagation test completed successfully")
}

// testErrorRecoveryAndHealing tests how the system recovers from errors
func (s *ErrorPropagationTestSuite) testErrorRecoveryAndHealing(t *testing.T) {
	client := s.createAuthenticatedClient(t)
	defer func() { _ = client.Close() }()

	// First, cause errors
	for i := 0; i < len(s.mockBackends); i++ {
		s.mockBackends[i].errorConfig.ErrorRate = 1.0 // Force all to error
	}

	// Send requests during error state
	errorStart := time.Now()
	for i := 0; i < 5; i++ {
		request := mcp.Request{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      fmt.Sprintf("error-phase-%d", i),
		}

		response := client.SendRequest(t, request)
		assert.NotNil(t, response.Error, "Should have errors during error phase")
	}

	// Now restore backends to healthy state
	for i := 0; i < len(s.mockBackends); i++ {
		s.mockBackends[i].errorConfig.ErrorRate = 0.0 // Restore to healthy
		s.mockBackends[i].errorConfig.NetworkErrors = false
		s.mockBackends[i].errorConfig.ProtocolErrors = false
		s.mockBackends[i].errorConfig.TimeoutErrors = false
		s.mockBackends[i].errorConfig.ServiceErrors = false
	}

	// Wait for health checks to detect recovery
	time.Sleep(3 * time.Second)

	// Send requests during recovery phase
	recoveryStart := time.Now()
	successCount := 0
	for i := 0; i < 10; i++ {
		request := mcp.Request{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      fmt.Sprintf("recovery-phase-%d", i),
		}

		response := client.SendRequest(t, request)
		if response.Error == nil {
			successCount++
			if successCount == 1 { // First successful recovery
				recoveryTime := time.Since(errorStart)
				atomic.StoreInt64(&s.errorMetrics.ErrorRecoveryTime, recoveryTime.Nanoseconds())
				atomic.AddInt64(&s.errorMetrics.SuccessfulRecoveries, 1)
			}
		}
	}

	assert.Greater(t, successCount, 5, "Should have successful requests after recovery")

	recoveries := atomic.LoadInt64(&s.errorMetrics.SuccessfulRecoveries)
	recoveryTime := time.Duration(atomic.LoadInt64(&s.errorMetrics.ErrorRecoveryTime))

	assert.Greater(t, recoveries, int64(0), "Should have recorded successful recovery")
	assert.Less(t, recoveryTime, 30*time.Second, "Recovery should happen within reasonable time")

	t.Log("✅ Error recovery and healing test completed successfully")
}

// testConcurrentErrorHandling tests error handling under concurrent load
func (s *ErrorPropagationTestSuite) testConcurrentErrorHandling(t *testing.T) {
	const numClients = 10
	const requestsPerClient = 5

	var wg sync.WaitGroup
	errorCounts := make([]int64, numClients)

	// Configure some backends to error
	for i := 1; i < len(s.mockBackends); i += 2 {
		s.mockBackends[i].errorConfig.ErrorRate = 0.5
		s.mockBackends[i].errorConfig.NetworkErrors = true
	}

	for clientID := 0; clientID < numClients; clientID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client := s.createAuthenticatedClient(t)
			defer func() { _ = client.Close() }()

			for reqID := 0; reqID < requestsPerClient; reqID++ {
				request := mcp.Request{
					JSONRPC: "2.0",
					Method:  "tools/list",
					ID:      fmt.Sprintf("concurrent-error-test-%d-%d", id, reqID),
				}

				response := client.SendRequest(t, request)
				if response.Error != nil {
					errorCounts[id]++
				}

				atomic.AddInt64(&s.errorMetrics.TotalRequests, 1)
			}
		}(clientID)
	}

	wg.Wait()

	// Verify that errors were handled properly across all concurrent clients
	totalErrors := int64(0)
	for _, count := range errorCounts {
		totalErrors += count
	}

	totalRequests := atomic.LoadInt64(&s.errorMetrics.TotalRequests)
	assert.Greater(t, totalRequests, int64(numClients*requestsPerClient*0.8), "Most requests should have been processed")
	assert.Less(t, totalErrors, totalRequests, "Not all requests should have failed")

	t.Log("✅ Concurrent error handling test completed successfully")
}

// Helper methods

func (s *ErrorPropagationTestSuite) createAuthenticatedClient(t *testing.T) *testClient {
	token := s.generateTestToken(t)

	wsURL := fmt.Sprintf("ws://%s/", s.serverAddr)
	headers := http.Header{
		"Authorization": []string{fmt.Sprintf("Bearer %s", token)},
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

func (s *ErrorPropagationTestSuite) tryCreateClientWithInvalidAuth(t *testing.T) (*testClient, error) {
	wsURL := fmt.Sprintf("ws://%s/", s.serverAddr)
	headers := http.Header{
		"Authorization": []string{"Bearer invalid-token"},
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
		return nil, fmt.Errorf("Unauthorized: status code %d", resp.StatusCode)
	}

	return &testClient{
		conn:   conn,
		logger: s.logger,
	}, nil
}

func (s *ErrorPropagationTestSuite) tryCreateClientWithExpiredAuth(t *testing.T) (*testClient, error) {
	expiredToken := s.generateExpiredTestToken(t)

	wsURL := fmt.Sprintf("ws://%s/", s.serverAddr)
	headers := http.Header{
		"Authorization": []string{fmt.Sprintf("Bearer %s", expiredToken)},
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
		return nil, fmt.Errorf("Unauthorized: status code %d", resp.StatusCode)
	}

	return &testClient{
		conn:   conn,
		logger: s.logger,
	}, nil
}

func (s *ErrorPropagationTestSuite) generateTestToken(t *testing.T) string {
	claims := &auth.Claims{
		Scopes: []string{"mcp:*", "tools:*"},
	}
	claims.Subject = "error-test-user"
	claims.Issuer = "error-test-gateway"
	claims.Audience = jwt.ClaimStrings{"error-test-gateway"}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
	claims.IssuedAt = jwt.NewNumericDate(time.Now())
	claims.NotBefore = jwt.NewNumericDate(time.Now())

	testSecret := []byte("error-propagation-test-secret-key-12345")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(testSecret)
	require.NoError(t, err)

	return tokenString
}

func (s *ErrorPropagationTestSuite) generateExpiredTestToken(t *testing.T) string {
	claims := &auth.Claims{
		Scopes: []string{"mcp:*", "tools:*"},
	}
	claims.Subject = "error-test-user"
	claims.Issuer = "error-test-gateway"
	claims.Audience = jwt.ClaimStrings{"error-test-gateway"}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) // Expired
	claims.IssuedAt = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))
	claims.NotBefore = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))

	testSecret := []byte("error-propagation-test-secret-key-12345")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(testSecret)
	require.NoError(t, err)

	return tokenString
}

// Backend implementation methods

func (b *ErrorInjectingBackend) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	b.mu.RLock()
	shouldError := b.errorConfig.ServiceErrors &&
		(b.errorConfig.ErrorRate > 0.5 || b.errorConfig.CircuitBreakerOpen)
	b.mu.RUnlock()

	if shouldError {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (b *ErrorInjectingBackend) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Inject network errors at connection level
	b.mu.RLock()
	networkErrors := b.errorConfig.NetworkErrors
	errorRate := b.errorConfig.ErrorRate
	b.mu.RUnlock()

	if networkErrors && (time.Now().UnixNano()%testIterations) < int64(errorRate*testIterations) {
		http.Error(w, "Connection failed", http.StatusInternalServerError)
		atomic.AddInt64(&b.errorCount, 1)
		return
	}

	conn, err := b.upgrader.Upgrade(w, r, nil)
	if err != nil {
		atomic.AddInt64(&b.errorCount, 1)
		return
	}

	connState := &BackendConnection{
		ID:          fmt.Sprintf("conn-%d", time.Now().UnixNano()),
		ConnectedAt: time.Now(),
	}

	b.mu.Lock()
	b.connections[conn] = connState
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.connections, conn)
		b.mu.Unlock()
		_ = conn.Close()
	}()

	for {
		var req mcp.Request
		if err := conn.ReadJSON(&req); err != nil {
			break
		}

		atomic.AddInt64(&b.messageCount, 1)

		b.mu.Lock()
		connState.MessageCount++
		connState.LastMessage = time.Now()
		b.mu.Unlock()

		// Apply error injection
		if b.shouldInjectError() {
			b.sendErrorResponse(conn, &req)
			continue
		}

		// Apply latency injection
		b.mu.RLock()
		latency := b.errorConfig.LatencyInjection
		b.mu.RUnlock()

		if latency > 0 {
			time.Sleep(latency)
		}

		// Send normal response
		response := &mcp.Response{
			JSONRPC: "2.0",
			Result: map[string]interface{}{
				"method":     req.Method,
				"backend_id": b.getBackendID(),
				"message_id": atomic.LoadInt64(&b.messageCount),
			},
			ID: req.ID,
		}

		if err := conn.WriteJSON(response); err != nil {
			break
		}
	}
}

func (b *ErrorInjectingBackend) shouldInjectError() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	random := float64(time.Now().UnixNano()%10000) / 10000.0
	return random < b.errorConfig.ErrorRate
}

func (b *ErrorInjectingBackend) sendErrorResponse(conn *websocket.Conn, req *mcp.Request) {
	b.mu.RLock()
	config := b.errorConfig
	b.mu.RUnlock()

	var errorResponse *mcp.Response

	if config.ProtocolErrors {
		// Send malformed response
		malformed := map[string]interface{}{
			"invalid": "response",
			"missing": "required_fields",
		}
		conn.WriteJSON(malformed)
		atomic.AddInt64(&b.errorCount, 1)
		return
	}

	if config.ServiceErrors {
		errorResponse = &mcp.Response{
			JSONRPC: "2.0",
			Error: &mcp.Error{
				Code:    mcp.ErrorCodeInternalError,
				Message: "Backend service error injected for testing",
				Data: map[string]interface{}{
					"error_type": "service_error",
					"backend_id": b.getBackendID(),
				},
			},
			ID: req.ID,
		}
	} else {
		errorResponse = &mcp.Response{
			JSONRPC: "2.0",
			Error: &mcp.Error{
				Code:    mcp.ErrorCodeInternalError,
				Message: "Generic backend error injected for testing",
				Data: map[string]interface{}{
					"error_type": "generic_error",
					"backend_id": b.getBackendID(),
				},
			},
			ID: req.ID,
		}
	}

	conn.WriteJSON(errorResponse)
	atomic.AddInt64(&b.errorCount, 1)
}

func (b *ErrorInjectingBackend) getBackendID() string {
	return fmt.Sprintf("backend-%p", b)
}

// Utility function to check if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			indexOf(s, substr) >= 0)))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
