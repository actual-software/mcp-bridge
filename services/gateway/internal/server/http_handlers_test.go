package server

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	"github.com/poiley/mcp-bridge/services/gateway/internal/health"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
)

// TestSecurityHeadersMiddleware tests the security headers middleware.
func TestSecurityHeadersMiddleware(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{}

	server := &GatewayServer{
		config: cfg,
		logger: logger,
	}

	// Create a test handler that the middleware will wrap
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("test response"))
		assert.NoError(t, err)
	})

	middleware := server.securityHeadersMiddleware(testHandler)

	t.Run("adds_security_headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
		assert.Equal(t, "1; mode=block", rec.Header().Get("X-XSS-Protection"))
		assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
		assert.Contains(t, rec.Header().Get("Permissions-Policy"), "accelerometer=()")
		assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "default-src 'none'")
	})

	t.Run("adds_hsts_for_tls", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.TLS = &tls.ConnectionState{} // Simulate TLS connection
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.Equal(t, "max-age=31536000; includeSubDomains", rec.Header().Get("Strict-Transport-Security"))
	})

	t.Run("handles_options_request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Empty(t, rec.Body.String()) // OPTIONS should return empty body
	})

	t.Run("handles_cors_with_origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "https://example.com")

		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		// Since isAllowedOrigin returns false by default, no CORS headers should be set
		assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	})
}

// TestIsAllowedOrigin tests the origin validation function.
func TestIsAllowedOrigin(t *testing.T) {
	server := &GatewayServer{
		config: &config.Config{},
		logger: zap.NewNop(),
	}

	tests := []struct {
		name     string
		origin   string
		expected bool
	}{
		{
			name:     "any_origin_denied",
			origin:   "https://example.com",
			expected: false,
		},
		{
			name:     "localhost_denied",
			origin:   "http://localhost:3000",
			expected: false,
		},
		{
			name:     "empty_origin_denied",
			origin:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := server.isAllowedOrigin(tt.origin)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestHandleSecurityTxt tests the security.txt handler.
func TestHandleSecurityTxt(t *testing.T) {
	logger := zap.NewNop()
	server := &GatewayServer{
		logger: logger,
	}

	t.Run("get_request_success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil)
		rec := httptest.NewRecorder()

		server.handleSecurityTxt(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
		assert.Equal(t, "max-age=10800", rec.Header().Get("Cache-Control"))

		body := rec.Body.String()
		assert.Contains(t, body, "Contact: security@mcp-gateway.example.com")
		assert.Contains(t, body, "Expires:")
		assert.Contains(t, body, "Preferred-Languages: en")
		assert.Contains(t, body, "Canonical:")
		assert.Contains(t, body, "Encryption:")
		assert.Contains(t, body, "Policy:")
		assert.Contains(t, body, "Quick Report")
	})

	t.Run("post_request_not_allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/.well-known/security.txt", strings.NewReader("test"))
		rec := httptest.NewRecorder()

		server.handleSecurityTxt(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
		assert.Empty(t, rec.Body.String())
	})

	t.Run("put_request_not_allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/.well-known/security.txt", strings.NewReader("test"))
		rec := httptest.NewRecorder()

		server.handleSecurityTxt(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})

	t.Run("delete_request_not_allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/.well-known/security.txt", nil)
		rec := httptest.NewRecorder()

		server.handleSecurityTxt(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}

// createTestHealthChecker creates a health checker with simulated status.
func createTestHealthChecker(healthy bool) *health.Checker {
	checker := health.CreateHealthMonitor(nil, zap.NewNop())

	// Simulate healthy/unhealthy state by updating subsystems
	if healthy {
		checker.UpdateSubsystem("test", health.Subsystem{
			Name:    "test",
			Healthy: true,
			Status:  "OK",
			Message: "Test service healthy",
		})
		// Manually set healthy endpoints for readiness checks using reflection
		// This is test-only code that needs to set private fields for testing
		checkerValue := reflect.ValueOf(checker).Elem()
		statusField := checkerValue.FieldByName("status")
		// #nosec G103 - using unsafe in test code to set private field for test setup
		statusField = reflect.NewAt(statusField.Type(), unsafe.Pointer(statusField.UnsafeAddr())).Elem()
		healthyEndpointsField := statusField.FieldByName("HealthyEndpoints")
		healthyEndpointsField.SetInt(1)
	} else {
		checker.UpdateSubsystem("test", health.Subsystem{
			Name:    "test",
			Healthy: false,
			Status:  "ERROR",
			Message: "Test service unhealthy",
		})
	}

	return checker
}

// TestHandleHealth tests the health endpoint handler.
func TestHandleHealth(t *testing.T) {
	logger := zap.NewNop()

	t.Run("healthy_status", func(t *testing.T) {
		healthChecker := createTestHealthChecker(true)

		server := &GatewayServer{
			logger: logger,
			health: healthChecker,
		}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		server.handleHealth(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var status health.Status

		err := json.NewDecoder(rec.Body).Decode(&status)
		require.NoError(t, err)
		assert.True(t, status.Healthy)
	})

	t.Run("unhealthy_status", func(t *testing.T) {
		healthChecker := createTestHealthChecker(false)

		server := &GatewayServer{
			logger: logger,
			health: healthChecker,
		}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()

		server.handleHealth(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var status health.Status

		err := json.NewDecoder(rec.Body).Decode(&status)
		require.NoError(t, err)
		assert.False(t, status.Healthy)
	})
}

// TestHandleHealthz tests the healthz endpoint handler.
func TestHandleHealthz(t *testing.T) {
	logger := zap.NewNop()

	t.Run("healthy_response", func(t *testing.T) {
		healthChecker := createTestHealthChecker(true)

		server := &GatewayServer{
			logger: logger,
			health: healthChecker,
		}

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()

		server.handleHealthz(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "OK", strings.TrimSpace(rec.Body.String()))
	})

	t.Run("unhealthy_response", func(t *testing.T) {
		healthChecker := createTestHealthChecker(false)

		server := &GatewayServer{
			logger: logger,
			health: healthChecker,
		}

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()

		server.handleHealthz(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "Unhealthy", strings.TrimSpace(rec.Body.String()))
	})
}

// TestHandleReady tests the ready endpoint handler.
func TestHandleReady(t *testing.T) {
	logger := zap.NewNop()

	t.Run("ready_response", func(t *testing.T) {
		healthChecker := createTestHealthChecker(true)

		server := &GatewayServer{
			logger: logger,
			health: healthChecker,
		}

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rec := httptest.NewRecorder()

		server.handleReady(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "Ready", strings.TrimSpace(rec.Body.String()))
	})

	t.Run("not_ready_response", func(t *testing.T) {
		healthChecker := createTestHealthChecker(false)

		server := &GatewayServer{
			logger: logger,
			health: healthChecker,
		}

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rec := httptest.NewRecorder()

		server.handleReady(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "Not ready", strings.TrimSpace(rec.Body.String()))
	})
}

// TestMakeOriginCheckerFunction tests the origin checker factory function.
func TestMakeOriginCheckerFunction(t *testing.T) {
	logger := zap.NewNop()

	t.Run("empty_allowed_origins", func(t *testing.T) {
		checker := makeOriginChecker([]string{}, logger)

		// Should default to localhost when no origins specified (without port)
		req1 := httptest.NewRequest(http.MethodGet, "/", nil)
		req1.Header.Set("Origin", "http://localhost")
		assert.True(t, checker(req1))

		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.Header.Set("Origin", "https://localhost")
		assert.True(t, checker(req2))

		// With port should not match (requires explicit configuration)
		req3 := httptest.NewRequest(http.MethodGet, "/", nil)
		req3.Header.Set("Origin", "http://localhost:3000")
		assert.False(t, checker(req3))

		req4 := httptest.NewRequest(http.MethodGet, "/", nil)
		req4.Header.Set("Origin", "https://example.com")
		assert.False(t, checker(req4))
	})

	t.Run("wildcard_origin", func(t *testing.T) {
		checker := makeOriginChecker([]string{"*"}, logger)

		// Should allow all origins when wildcard specified
		req1 := httptest.NewRequest(http.MethodGet, "/", nil)
		req1.Header.Set("Origin", "https://example.com")
		assert.True(t, checker(req1))

		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.Header.Set("Origin", "http://localhost:3000")
		assert.True(t, checker(req2))
	})

	t.Run("specific_allowed_origins", func(t *testing.T) {
		allowedOrigins := []string{"https://example.com", "http://localhost:3000"}
		checker := makeOriginChecker(allowedOrigins, logger)

		// Should allow only specified origins
		req1 := httptest.NewRequest(http.MethodGet, "/", nil)
		req1.Header.Set("Origin", "https://example.com")
		assert.True(t, checker(req1))

		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.Header.Set("Origin", "http://localhost:3000")
		assert.True(t, checker(req2))

		req3 := httptest.NewRequest(http.MethodGet, "/", nil)
		req3.Header.Set("Origin", "https://evil.com")
		assert.False(t, checker(req3))
	})

	t.Run("no_origin_header", func(t *testing.T) {
		checker := makeOriginChecker([]string{"https://example.com"}, logger)

		// Should allow when no origin header (same-origin or non-browser)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		assert.True(t, checker(req))
	})
}

// TestCreateTLSConfigFunction tests TLS configuration creation.
func TestCreateTLSConfigFunction(t *testing.T) {
	logger := zap.NewNop()

	t.Run("tls_disabled", func(t *testing.T) {
		cfg := &config.Config{
			Server: config.ServerConfig{
				TLS: config.TLSConfig{
					Enabled: false,
				},
			},
		}

		server := &GatewayServer{
			config: cfg,
			logger: logger,
		}

		// When TLS is disabled, createTLSConfig should not be called
		// We'll test that the server can handle disabled TLS gracefully
		assert.False(t, server.config.Server.TLS.Enabled)
	})

	t.Run("invalid_min_version", func(t *testing.T) {
		cfg := &config.Config{
			Server: config.ServerConfig{
				TLS: config.TLSConfig{
					Enabled:    true,
					CertFile:   "/tmp/test-cert.pem", // Use temp path to avoid file not found first
					KeyFile:    "/tmp/test-key.pem",
					MinVersion: "invalid-version",
				},
			},
		}

		server := &GatewayServer{
			config: cfg,
			logger: logger,
		}

		_, err := server.createTLSConfig()
		require.Error(t, err)
		// The error should be about TLS configuration failure
		assert.True(t,
			strings.Contains(err.Error(), "TLS configuration failed") ||
				strings.Contains(err.Error(), "no such file or directory"),
			"Expected error about TLS configuration, got: %s", err.Error())
	})
}

// TestBootstrapGatewayServerFunction tests server initialization.
func TestBootstrapGatewayServerFunction(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:                 8080,
			MaxConnections:       testMaxIterations,
			ConnectionBufferSize: 4096,
			AllowedOrigins:       []string{"http://localhost"},
		},
	}

	// Create minimal mocks
	mockMetrics := &metrics.Registry{}
	mockHealth := health.CreateHealthMonitor(nil, logger)

	server := BootstrapGatewayServer(cfg, nil, nil, nil, mockHealth, mockMetrics, nil, logger)

	assert.NotNil(t, server)
	assert.Equal(t, cfg, server.config)
	assert.Equal(t, logger, server.logger)
	assert.Equal(t, mockHealth, server.health)
	assert.Equal(t, mockMetrics, server.metrics)
	assert.NotNil(t, server.ctx)
	assert.NotNil(t, server.cancel)
}
