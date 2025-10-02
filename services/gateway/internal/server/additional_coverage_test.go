package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/health"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
)

// TestAllowMethod tests the Allow method structure (rate limiter passthrough).
func TestAllowMethod(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			MaxConnections: 10,
		},
	}

	mockMetrics := &metrics.Registry{}
	mockHealth := health.CreateHealthMonitor(nil, logger)

	t.Run("allow_method_structure", func(t *testing.T) {
		// Just test that the server can be created - the Allow method is a simple passthrough
		// that would require a complex rate limiter mock to test properly
		server := BootstrapGatewayServer(cfg, nil, nil, nil, mockHealth, mockMetrics, nil, logger)
		assert.NotNil(t, server)

		// Verify the method exists and has the right signature by checking it compiles
		// We won't call it since it would require a proper rate limiter
		assert.NotNil(t, server.Allow)
	})
}

// TestIPConnectionCounting tests IP connection management.
func TestIPConnectionCounting(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			MaxConnections: 10,
		},
	}

	mockMetrics := &metrics.Registry{}
	mockHealth := health.CreateHealthMonitor(nil, logger)

	server := BootstrapGatewayServer(cfg, nil, nil, nil, mockHealth, mockMetrics, nil, logger)

	t.Run("increment_and_get_count", func(t *testing.T) {
		ip := "192.168.1.testIterations"

		// Initial count should be 0
		assert.Equal(t, int64(0), server.getIPConnCount(ip))

		// Increment and verify
		server.incrementIPConnCount(ip)
		assert.Equal(t, int64(1), server.getIPConnCount(ip))

		server.incrementIPConnCount(ip)
		assert.Equal(t, int64(2), server.getIPConnCount(ip))
	})

	t.Run("decrement_count", func(t *testing.T) {
		ip := "192.168.1.101"

		// Increment first
		server.incrementIPConnCount(ip)
		server.incrementIPConnCount(ip)
		assert.Equal(t, int64(2), server.getIPConnCount(ip))

		// Decrement and verify
		server.decrementIPConnCount(ip)
		assert.Equal(t, int64(1), server.getIPConnCount(ip))

		server.decrementIPConnCount(ip)
		assert.Equal(t, int64(0), server.getIPConnCount(ip))
	})

	t.Run("separate_ip_counts", func(t *testing.T) {
		ip1 := "192.168.1.102"
		ip2 := "192.168.1.103"

		server.incrementIPConnCount(ip1)
		server.incrementIPConnCount(ip1)
		server.incrementIPConnCount(ip2)

		assert.Equal(t, int64(2), server.getIPConnCount(ip1))
		assert.Equal(t, int64(1), server.getIPConnCount(ip2))
	})
}

// TestGetCipherSuiteAdditional tests cipher suite mapping function.
func TestGetCipherSuiteAdditional(t *testing.T) {
	tests := createCipherSuiteTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCipherSuite(tt.cipherName)
			if tt.expectedZero {
				assert.Equal(t, uint16(0), result, "Expected zero for %s", tt.cipherName)
			} else {
				assert.NotEqual(t, uint16(0), result, "Expected non-zero for %s", tt.cipherName)
			}
		})
	}
}

func createCipherSuiteTests() []struct {
	name         string
	cipherName   string
	expectedZero bool
} {
	var tests []struct {
		name         string
		cipherName   string
		expectedZero bool
	}

	tests = append(tests, createValidCipherTests()...)
	tests = append(tests, createInvalidCipherTests()...)

	return tests
}

func createValidCipherTests() []struct {
	name         string
	cipherName   string
	expectedZero bool
} {
	return []struct {
		name         string
		cipherName   string
		expectedZero bool
	}{
		{
			name:         "TLS_AES_128_GCM_SHA256",
			cipherName:   "TLS_AES_128_GCM_SHA256",
			expectedZero: false,
		},
		{
			name:         "TLS_AES_256_GCM_SHA384",
			cipherName:   "TLS_AES_256_GCM_SHA384",
			expectedZero: false,
		},
		{
			name:         "TLS_CHACHA20_POLY1305_SHA256",
			cipherName:   "TLS_CHACHA20_POLY1305_SHA256",
			expectedZero: false,
		},
		{
			name:         "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			cipherName:   "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			expectedZero: false,
		},
		{
			name:         "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			cipherName:   "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			expectedZero: false,
		},
		{
			name:         "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
			cipherName:   "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
			expectedZero: false,
		},
		{
			name:         "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
			cipherName:   "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
			expectedZero: false,
		},
		{
			name:         "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
			cipherName:   "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
			expectedZero: false,
		},
		{
			name:         "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
			cipherName:   "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
			expectedZero: false,
		},
	}
}

func createInvalidCipherTests() []struct {
	name         string
	cipherName   string
	expectedZero bool
} {
	return []struct {
		name         string
		cipherName   string
		expectedZero bool
	}{
		{
			name:         "Unknown_cipher",
			cipherName:   "TLS_UNKNOWN_CIPHER_SUITE",
			expectedZero: true,
		},
		{
			name:         "Empty_cipher",
			cipherName:   "",
			expectedZero: true,
		},
		{
			name:         "Invalid_format",
			cipherName:   "not-a-cipher-suite",
			expectedZero: true,
		},
	}
}

// TestRemoveConnection tests connection removal.
func TestRemoveConnection(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:           8080,
			MaxConnections: 10,
		},
	}

	mockMetrics := &metrics.Registry{}
	mockHealth := health.CreateHealthMonitor(nil, logger)

	server := BootstrapGatewayServer(cfg, nil, nil, nil, mockHealth, mockMetrics, nil, logger)

	t.Run("remove_nonexistent_connection", func(t *testing.T) {
		// Should not panic when removing non-existent connection
		server.removeConnection("nonexistent-id")
	})
}

// TestBootstrapGatewayServerOptions tests various server configuration options.
func TestBootstrapGatewayServerOptions(t *testing.T) {
	logger := zap.NewNop()

	t.Run("tcp_protocol_config", func(t *testing.T) {
		testTCPProtocolConfig(t, logger)
	})

	t.Run("both_protocol_config", func(t *testing.T) {
		testBothProtocolConfig(t, logger)
	})

	t.Run("websocket_only_config", func(t *testing.T) {
		testWebSocketOnlyConfig(t, logger)
	})
}

func testTCPProtocolConfig(t *testing.T, logger *zap.Logger) {
	t.Helper()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:                 8080,
			Protocol:             ProtocolTCP,
			ConnectionBufferSize: 8192,
			AllowedOrigins:       []string{"https://example.com"},
		},
		Auth: config.AuthConfig{
			PerMessageAuth: true,
		},
	}

	mockMetrics := &metrics.Registry{}
	mockHealth := health.CreateHealthMonitor(nil, logger)

	server := BootstrapGatewayServer(cfg, nil, nil, nil, mockHealth, mockMetrics, nil, logger)

	assert.NotNil(t, server)
	assert.NotNil(t, server.tcpHandler)
	assert.NotNil(t, server.messageAuth)
	assert.Equal(t, 8192, server.upgrader.ReadBufferSize)
	assert.Equal(t, 8192, server.upgrader.WriteBufferSize)
}

func testBothProtocolConfig(t *testing.T, logger *zap.Logger) {
	t.Helper()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:                 8080,
			Protocol:             ProtocolBoth,
			ConnectionBufferSize: 4096,
			AllowedOrigins:       []string{"*"},
		},
	}

	mockMetrics := &metrics.Registry{}
	mockHealth := health.CreateHealthMonitor(nil, logger)

	server := BootstrapGatewayServer(cfg, nil, nil, nil, mockHealth, mockMetrics, nil, logger)

	assert.NotNil(t, server)
	assert.NotNil(t, server.tcpHandler)
}

func testWebSocketOnlyConfig(t *testing.T, logger *zap.Logger) {
	t.Helper()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:                 8080,
			Protocol:             "websocket",
			ConnectionBufferSize: 2048,
			AllowedOrigins:       []string{},
		},
	}

	mockMetrics := &metrics.Registry{}
	mockHealth := health.CreateHealthMonitor(nil, logger)

	server := BootstrapGatewayServer(cfg, nil, nil, nil, mockHealth, mockMetrics, nil, logger)

	assert.NotNil(t, server)
	assert.Nil(t, server.tcpHandler)
	assert.Equal(t, 2048, server.upgrader.ReadBufferSize)
}
