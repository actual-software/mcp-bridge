package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	"github.com/poiley/mcp-bridge/services/gateway/internal/health"
	"github.com/poiley/mcp-bridge/services/gateway/internal/ratelimit"
	"github.com/poiley/mcp-bridge/services/gateway/internal/router"
	"github.com/poiley/mcp-bridge/services/gateway/internal/session"
	"github.com/poiley/mcp-bridge/services/gateway/test/testutil"
)

func TestWebSocketOriginValidation(t *testing.T) {
	_, testServer := setupWebSocketOriginTestServer(t)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")
	tests := createWebSocketOriginTests()

	runWebSocketOriginTests(t, tests, wsURL)
}

func setupWebSocketOriginTestServer(t *testing.T) (*GatewayServer, *httptest.Server) {
	t.Helper()

	cfg := createOriginTestConfig()
	mockAuth := createOriginTestAuth()
	mockSessions := createOriginTestSessions(mockAuth)
	testRouter := createOriginTestRouter()
	
	server := createOriginTestGatewayServer(t, cfg, mockAuth, mockSessions, testRouter)
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))

	return server, testServer
}

func createOriginTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Port:                 8443,
			MaxConnections:       10,
			ConnectionBufferSize: 65536,
			AllowedOrigins:       []string{"https://trusted.example.com", "http://localhost:3000"},
		},
	}
}

func createOriginTestAuth() *mockAuthProvider {
	mockAuth := &mockAuthProvider{
		claims: &auth.Claims{
			RateLimit: auth.RateLimitConfig{
				RequestsPerMinute: testMaxIterations,
				Burst:             testTimeout,
			},
		},
	}
	mockAuth.claims.Subject = "test-user"

	return mockAuth
}

func createOriginTestSessions(mockAuth *mockAuthProvider) *mockSessionManager {
	return &mockSessionManager{
		session: &session.Session{
			ID:        "test-session",
			User:      "test-user",
			RateLimit: mockAuth.claims.RateLimit,
		},
	}
}

func createOriginTestRouter() *router.Router {
	routerCfg := config.RoutingConfig{
		Strategy: "round_robin",
		CircuitBreaker: config.CircuitBreakerConfig{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			TimeoutSeconds:   30,
		},
	}
	mockDiscovery := &mockServiceDiscovery{}

	return router.InitializeRequestRouter(
		context.Background(),
		routerCfg,
		mockDiscovery,
		testutil.CreateTestMetricsRegistry(),
		zap.NewNop(),
	)
}

func createOriginTestGatewayServer(
	t *testing.T,
	cfg *config.Config,
	mockAuth *mockAuthProvider,
	mockSessions *mockSessionManager,
	testRouter *router.Router,
) *GatewayServer {
	t.Helper()
	mockHealth := health.CreateHealthMonitor(nil, zap.NewNop())
	registry := testutil.CreateTestMetricsRegistry()
	mockRateLimiter := ratelimit.CreateLocalMemoryRateLimiter(zap.NewNop())
	logger := testutil.NewTestLogger(t)

	return BootstrapGatewayServer(
		cfg,
		mockAuth,
		mockSessions,
		testRouter,
		mockHealth,
		registry,
		mockRateLimiter,
		logger,
	)
}

func createWebSocketOriginTests() []struct {
	name          string
	origin        string
	expectConnect bool
} {
	return []struct {
		name          string
		origin        string
		expectConnect bool
	}{
		{
			name:          "Allowed origin - trusted site",
			origin:        "https://trusted.example.com",
			expectConnect: true,
		},
		{
			name:          "Allowed origin - localhost with port",
			origin:        "http://localhost:3000",
			expectConnect: true,
		},
		{
			name:          "Rejected origin - untrusted site",
			origin:        "https://evil.example.com",
			expectConnect: false,
		},
		{
			name:          "Rejected origin - different port",
			origin:        "http://localhost:4000",
			expectConnect: false,
		},
		{
			name:          "No origin header - allowed",
			origin:        "",
			expectConnect: true,
		},
	}
}

func runWebSocketOriginTests(t *testing.T, tests []struct {
	name          string
	origin        string
	expectConnect bool
}, wsURL string) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create dialer with custom origin
			dialer := websocket.DefaultDialer
			headers := http.Header{}
			headers.Set("Authorization", "Bearer test-token")

			if tt.origin != "" {
				headers.Set("Origin", tt.origin)
			}

			conn, resp, err := dialer.Dial(wsURL, headers)
			if resp != nil {
				defer func() { _ = resp.Body.Close() }()
			}

			if tt.expectConnect {
				require.NoError(t, err, "Expected successful connection")
				assert.NotNil(t, conn)

				if conn != nil {
					_ = conn.Close()
				}
			} else {
				// Connection should be rejected
				assert.Error(t, err, "Expected connection to be rejected")

				if resp != nil {
					assert.Equal(t, http.StatusForbidden, resp.StatusCode,
						"Expected 403 Forbidden for rejected origin")
				}
			}
		})
	}
}

func TestWebSocketOriginValidation_Wildcard(t *testing.T) {
	_, testServer := setupWildcardOriginTestServer(t)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")
	origins := []string{
		"https://any-site.com",
		"http://evil.example.com",
		"https://trusted.example.com",
	}

	runWildcardOriginTests(t, origins, wsURL)
}

func setupWildcardOriginTestServer(t *testing.T) (*GatewayServer, *httptest.Server) {
	t.Helper()

	cfg := createWildcardOriginTestConfig()
	mockAuth := createOriginTestAuth()
	mockSessions := createOriginTestSessions(mockAuth)
	testRouter := createOriginTestRouter()
	
	server := createOriginTestGatewayServer(t, cfg, mockAuth, mockSessions, testRouter)
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))

	return server, testServer
}

func createWildcardOriginTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Port:                 8443,
			MaxConnections:       10,
			ConnectionBufferSize: 65536,
			AllowedOrigins:       []string{"*"}, // Wildcard - allows all
		},
	}
}

func runWildcardOriginTests(t *testing.T, origins []string, wsURL string) {
	t.Helper()
	// Test that any origin is allowed with wildcard
	for _, origin := range origins {
		t.Run("Wildcard allows "+origin, func(t *testing.T) {
			dialer := websocket.DefaultDialer
			headers := http.Header{}
			headers.Set("Authorization", "Bearer test-token")
			headers.Set("Origin", origin)

			conn, resp, err := dialer.Dial(wsURL, headers)
			if resp != nil {
				defer func() { _ = resp.Body.Close() }()
			}

			require.NoError(t, err, "Wildcard should allow any origin")
			assert.NotNil(t, conn)

			if conn != nil {
				_ = conn.Close()
			}
		})
	}
}
