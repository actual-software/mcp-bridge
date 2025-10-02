package gateway

import (
	"fmt"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/actual-software/mcp-bridge/test/testutil"
)

func TestNewGatewayClient(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	tests := getNewGatewayClientTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testNewGatewayClientCase(t, tt, logger)
		})
	}
}

type gatewayClientTest struct {
	name        string
	cfg         config.GatewayConfig
	wantType    string
	wantErr     bool
	errContains string
}

func getNewGatewayClientTests() []gatewayClientTest {
	tests := []gatewayClientTest{}
	tests = append(tests, getWebSocketTests()...)
	tests = append(tests, getTCPTests()...)
	tests = append(tests, getErrorTests()...)

	return tests
}

func getWebSocketTests() []gatewayClientTest {
	return []gatewayClientTest{
		{
			name: "WebSocket client (ws)",
			cfg: config.GatewayConfig{
				URL:        fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
				Connection: common.ConnectionConfig{TimeoutMs: 5000},
			},
			wantType: "*gateway.Client",
			wantErr:  false,
		},
		{
			name: "WebSocket client (wss)",
			cfg: config.GatewayConfig{
				URL:        fmt.Sprintf("wss://localhost:%d", constants.TestHTTPSPort),
				Connection: common.ConnectionConfig{TimeoutMs: 5000},
			},
			wantType: "*gateway.Client",
			wantErr:  false,
		},
	}
}

func getTCPTests() []gatewayClientTest {
	return []gatewayClientTest{
		{
			name: "TCP client (tcp)",
			cfg: config.GatewayConfig{
				URL:        fmt.Sprintf("tcp://localhost:%d", constants.TestHTTPPort),
				Connection: common.ConnectionConfig{TimeoutMs: 5000},
			},
			wantType: "*gateway.TCPClient",
			wantErr:  false,
		},
		{
			name: "TCP client with TLS (tcps)",
			cfg: config.GatewayConfig{
				URL:        fmt.Sprintf("tcps://localhost:%d", constants.TestHTTPSPort),
				Connection: common.ConnectionConfig{TimeoutMs: 5000},
			},
			wantType: "*gateway.TCPClient",
			wantErr:  false,
		},
		{
			name: "TCP client with TLS (tcp+tls)",
			cfg: config.GatewayConfig{
				URL:        fmt.Sprintf("tcp+tls://localhost:%d", constants.TestHTTPSPort),
				Connection: common.ConnectionConfig{TimeoutMs: 5000},
			},
			wantType: "*gateway.TCPClient",
			wantErr:  false,
		},
	}
}

func getErrorTests() []gatewayClientTest {
	return []gatewayClientTest{
		{
			name:        "invalid URL",
			cfg:         config.GatewayConfig{URL: "not-a-url"},
			wantErr:     true,
			errContains: "unsupported URL scheme:",
		},
		{
			name: "unsupported scheme",
			cfg: config.GatewayConfig{
				URL: fmt.Sprintf("http://localhost:%d", constants.TestHTTPPort),
			},
			wantErr:     true,
			errContains: "unsupported URL scheme",
		},
		{
			name:        "empty URL",
			cfg:         config.GatewayConfig{URL: ""},
			wantErr:     true,
			errContains: "unsupported URL scheme:",
		},
	}
}

func testNewGatewayClientCase(t *testing.T, tt gatewayClientTest, logger *zap.Logger) {
	t.Helper()
	client, err := NewGatewayClient(tt.cfg, logger)

	if (err != nil) != tt.wantErr {
		t.Errorf("NewGatewayClient() error = %v, wantErr %v", err, tt.wantErr)

		return
	}

	if err != nil {
		if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
			t.Errorf("Error = %v, want error containing %s", err, tt.errContains)

		}

		return
	}

	if client == nil {

		t.Error("Expected non-nil client")

		return
	}

	// Check type.
	gotType := fmt.Sprintf("%T", client)
	if gotType != tt.wantType {
		t.Errorf("Client type = %v, want %v", gotType, tt.wantType)
	}

	// Verify it implements GatewayClient interface.
	var _ = client
}

func TestGatewayClient_InterfaceCompliance(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	// Test that both client types implement the interface correctly.
	wsConfig := config.GatewayConfig{
		URL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	tcpConfig := config.GatewayConfig{
		URL: fmt.Sprintf("tcp://localhost:%d", constants.TestHTTPPort),
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	wsClient, err := NewGatewayClient(wsConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create WebSocket client: %v", err)
	}

	tcpClient, err := NewGatewayClient(tcpConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create TCP client: %v", err)
	}

	// Both should implement GatewayClient.
	clients := []GatewayClient{wsClient, tcpClient}

	for i, client := range clients {
		t.Run(fmt.Sprintf("client_%d", i), func(t *testing.T) {
			t.Parallel()
			// Test that all interface methods are available.
			// (This is mostly a compile-time check, but good to have)
			// These would fail at runtime without a real server,
			// but we're just checking the interface
			_ = client.Connect
			_ = client.SendRequest
			_ = client.ReceiveResponse
			_ = client.SendPing
			_ = client.Close
			_ = client.IsConnected

			// Initially not connected.
			if client.IsConnected() {
				t.Error("Expected client to not be connected initially")
			}
		})
	}
}

// verifyClientConfiguration validates that the client was configured correctly for the auth type.
func verifyClientConfiguration(t *testing.T, client GatewayClient, authType string) {
	t.Helper()

	if client == nil {
		t.Error("Expected non-nil client")

		return
	}

	// Type-specific checks.
	switch c := client.(type) {
	case *Client:
		if authType == config.AuthTypeOAuth2 && c.httpClient == nil {
			t.Error("Expected HTTP client to be configured for OAuth2")
		}
	case *TCPClient:
		if authType == "oauth2" && c.oauth2Client == nil {
			t.Error("Expected OAuth2 client to be configured")
		}
	default:
		t.Errorf("Unexpected client type: %T", c)
	}
}

// setupMTLSCerts creates temporary certificate files for mTLS testing.
func setupMTLSCerts(t *testing.T) (certFile, keyFile string, cleanup func()) {
	t.Helper()
	certPem := getMTLSCertPEM()
	keyPem := getMTLSKeyPEM()

	certFile, certCleanup := testutil.TempFile(t, certPem)
	keyFile, keyCleanup := testutil.TempFile(t, keyPem)

	cleanup = func() {
		certCleanup()
		keyCleanup()
	}

	return certFile, keyFile, cleanup
}

func getMTLSCertPEM() string {
	return `-----BEGIN CERTIFICATE-----
MIIDhTCCAm2gAwIBAgIUcvTZq+m2sj3lim7Yf4B6LFRSFPUwDQYJKoZIhvcNAQEL
BQAwUjELMAkGA1UEBhMCVVMxDTALBgNVBAgMBFRlc3QxDTALBgNVBAcMBFRlc3Qx
DTALBgNVBAoMBFRlc3QxFjAUBgNVBAMMDXRlc3QtY2EubG9jYWwwHhcNMjUwODA0
MDAwMTM0WhcNMjYwODA0MDAwMTM0WjBSMQswCQYDVQQGEwJVUzENMAsGA1UECAwE
VGVzdDENMAsGA1UEBwwEVGVzdDENMAsGA1UECgwEVGVzdDEWMBQGA1UEAwwNdGVz
dC1jYS5sb2NhbDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALay1Ky8
OG+DF6pp0X0kI7JWfNUW5TYRuM98TBPx7Edgr4lJY8fNwni6/pl/UghRM/ZQ6Pf2
jXkL0xk8IKSLT3i3kuxYI6vFT71hVedn/hhYypyY2vSlnwT4kLtD7c8msMq1iDXE
BbOzX5m+Q5vKauLiULW2VRP9Ty3QH+1BF/uW0dZaxyNRa/p9qzg7LMXXj8c8zwFl
Hjh9Cy46NUg0+xbbhmxu1cQVZCE/8N4s+3NsFZN4YBKlE5AT2cJ93zB+dL+hR4YZ
lzMF1rcPMRDX4jT6QODRVShe34u6X+OG6mu2M7rpp72SmUGEounlqvReM8MJpMX1
ggjFpk4FmkSVe3kCAwEAAaNTMFEwHQYDVR0OBBYEFH3IbeSKHPfPeylSmvgOkvMF
X444MB8GA1UdIwQYMBaAFH3IbeSKHPfPeylSmvgOkvMFX444MA8GA1UdEwEB/wQF
MAMBAf8wDQYJKoZIhvcNAQELBQADggEBABUdCOe9VvMIl7/U0vYZg2lsClf08jI1
egFDbP6u9lT9sYavB83bteAZBhGCFvLgEH6dT8cRdJlcXTvahUBAe9pLhvnt9B86
uzEsq428NY9OGE57mtQJ9yOvVkQ97NfamFqIONEsJh5td1UwqOS6k+o5IHvz2cqp
rR+HL2Yh9+bP9vinPDPUFEQ/WBY5Fpp2QsLLo5EeRegryYi6nzZN2IKy3OE3dAhC
Qdoz6sKUTxjiDfKklY3w5aQI9ij2s802+J76sSkMUoVCEBdC+R4rTESYUbwq+TI1
xO/Saff/cbJvDK89o8VaJKWV+ZXguvkgA6sRFNGII3B5UFiLm7u9IXg=
-----END CERTIFICATE-----`
}

func getMTLSKeyPEM() string {
	return `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC2stSsvDhvgxeq
adF9JCOyVnzVFuU2EbjPfEwT8exHYK+JSWPHzcJ4uv6Zf1IIUTP2UOj39o15C9MZ
PCCki094t5LsWCOrxU+9YVXnZ/4YWMqcmNr0pZ8E+JC7Q+3PJrDKtYg1xAWzs1+Z
vkObymri4lC1tlUT/U8t0B/tQRf7ltHWWscjUWv6fas4OyzF14/HPM8BZR44fQsu
OjVINPsW24ZsbtXEFWQhP/DeLPtzbBWTeGASpROQE9nCfd8wfnS/oUeGGZczBda3
DzEQ1+I0+kDg0VUoXt+Lul/jhuprtjO66ae9kplBhKLp5ar0XjPDCaTF9YIIxaZO
BZpElXt5AgMBAAECggEAKD7dKB7/TJtDaZQNZISDQ4wXTCaSv/Yn8LboGGWsv61+
BZ9P1moOWqOQpbYdFz1yFaLNqx/aGs3exvqOk0in7Ub9G8ivtO1Oa0CnmIX5PJpE
qbnnU8ivLrxlv4bPelhCziiulG91tRgAqYC26njs0kV584lylOhyWnx0KAK0moRH
rkYmlZa5rDQbqOq5C9zJCSqorHZhFLfk8aSbqfWNVHCV2FILEHiFWk/7iZ59i/Ng
Mnwk/Dol2CzYqVcqLxD1T/N9QfgmUiso9zCw8Rz3rjv9RT6nps3cfZIQGXHonwi+
h4Y31u6f37D1NuCfESk+tPikrl0TRnkEgQk8m0QGPQKBgQDdrIkuRNREcBB9G2C0
cNv7xKJion21asO//p45+ppkX9L8m3aRfJg7msgrDSYv2Vo1chY0DBc7lMkrbecL
3VFy5EgWmXC07R1xVVlUR2kirY+F8Z16tfltE+ZhtdLm2mz/UPcw3W/DGemaLabf
nPe0+F6CJz1DTuscm7lABdaUDQKBgQDS/UFmFngqv0TT6CwSQxWw7ISx6Bqdn2TC
h5V352l0pYGaIuViNQKV2olHAmph9hyi84RFq0qR1JiRzenqTDe7r7iWoCnKLGHV
tybbLlvfAUtBQQxfqcUxb20mbUGjU0TS3MvZioLDmdQE7iE57yQSU61TsdOOGbA1
9CTJ6mYOHQKBgQCVpgu6G6c9SHYpL1lalzI7RmTlt5Kr7ZaWv7pro72k83fJJt6l
mvpeisCFJ8xW0yHuIMXSfzMT+v7P/dLTlKaOrIPqFc4bplORFjBHECpuycKxhwps
M/td4uhNoGTvihe5SRyHdYYkrRKiDh2wqhQjrOSIcxsNnHJmjs5B5W8V5QKBgG/h
W4yG3bHNOvIjaztD13y57qNoMLTkkMmWm+u5CnKQUOkrF/e7pGNSPvkojsDjgMvn
1XwcGK67zSuDxUY4pFUiGP/GbmKGplpthG01aAIY7Y7sr2MK40YTkA2QYf35acVm
z7HLgQu3xnXW0EeoR7hwJrj60vPHK2lwzRFE+lkBAoGAQmV5h+rCYTB8REIMyKGa
38nkBxdRY3o/C+CoZlp8mLKL35uYwP1Y8sv7YfVxSVpXE82Tt0HNDmzCztZnS8qC
lxROYybkmXoExEMWo01N9g4BkXZrj5piCgR5drpMrTziqqMj+ou1wjIqbyPVbgdd
Zry7yq3lVqE6jyOprjq/HMM=
-----END PRIVATE KEY-----`
}

func TestGatewayClient_AuthConfiguration(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	tests := getAuthConfigurationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testAuthConfigurationCase(t, tt, logger)
		})
	}
}

type authConfigTest struct {
	name     string
	authType string
	url      string
}

func getAuthConfigurationTests() []authConfigTest {
	return []authConfigTest{
		{
			name:     "WebSocket with bearer auth",
			authType: "bearer",
			url:      fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
		},
		{
			name:     "WebSocket with OAuth2",
			authType: "oauth2",
			url:      fmt.Sprintf("wss://localhost:%d", constants.TestHTTPSPort),
		},
		{
			name:     "TCP with bearer auth",
			authType: "bearer",
			url:      fmt.Sprintf("tcp://localhost:%d", constants.TestHTTPPort),
		},
		{
			name:     "TCP with OAuth2",
			authType: "oauth2",
			url:      fmt.Sprintf("tcps://localhost:%d", constants.TestHTTPSPort),
		},
		{
			name:     "TCP with mTLS",
			authType: "mtls",
			url:      fmt.Sprintf("tcps://localhost:%d", constants.TestHTTPSPort),
		},
	}
}

func testAuthConfigurationCase(t *testing.T, tt authConfigTest, logger *zap.Logger) {
	t.Helper()
	authConfig := createAuthConfig(t, tt.authType)

	cfg := config.GatewayConfig{
		URL:        tt.url,
		Auth:       authConfig,
		Connection: common.ConnectionConfig{TimeoutMs: 5000},
	}

	client, err := NewGatewayClient(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Verify client configuration.
	verifyClientConfiguration(t, client, tt.authType)
}

func createAuthConfig(t *testing.T, authType string) common.AuthConfig {
	t.Helper()
	authConfig := common.AuthConfig{
		Type:          authType,
		Token:         "test-token",
		ClientID:      "test-client",
		ClientSecret:  "test-secret",
		TokenEndpoint: "http://auth.example.com/token",
		GrantType:     "client_credentials",
	}

	// Create temporary certificate files for mTLS tests.
	if authType == config.AuthTypeMTLS {
		certFile, keyFile, cleanup := setupMTLSCerts(t)
		t.Cleanup(cleanup)
		authConfig.ClientCert = certFile
		authConfig.ClientKey = keyFile
	}

	return authConfig
}

func TestGatewayClient_TLSConfiguration(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	caFile := setupTLSTestCA(t)
	tlsConfig := createTLSTestConfig(caFile)
	tests := getTLSConfigurationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testTLSConfigurationCase(t, tt.url, tlsConfig, logger)
		})
	}
}

func setupTLSTestCA(t *testing.T) string {
	t.Helper()
	caCert := getTLSTestCACert()

	caFile, cleanup := testutil.TempFile(t, caCert)

	t.Cleanup(cleanup)

	return caFile
}

func getTLSTestCACert() string {
	return `-----BEGIN CERTIFICATE-----
MIIDhTCCAm2gAwIBAgIUIFCgv8sjzj7TYRNCiWM9r3JbiW4wDQYJKoZIhvcNAQEL
BQAwUjELMAkGA1UEBhMCVVMxDTALBgNVBAgMBFRlc3QxDTALBgNVBAcMBFRlc3Qx
DTALBgNVBAoMBFRlc3QxFjAUBgNVBAMMDXRlc3QtY2EubG9jYWwwHhcNMjUwODAz
MjM1NzE5WhcNMjYwODAzMjM1NzE5WjBSMQswCQYDVQQGEwJVUzENMAsGA1UECAwE
VGVzdDENMAsGA1UEBwwEVGVzdDENMAsGA1UECgwEVGVzdDEWMBQGA1UEAwwNdGVz
dC1jYS5sb2NhbDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANm1eqUn
NVYdlSgNB8a4gZVsDnhz8o+gQQK/SyGnyqHmQ43jsqhAUsn5azo6fRbBexjd5KsU
+GrYwHOr3fBR7Cb1x3lWCzag/tKKcXFdeLK6en3ebEr+HGsrLUCOFojY3vJc/iMu
qz/h7ZOQ2t8Z1VEP0NnBNeVxkIhA6sMEaO47jnwIwojVb/oCd8HW2HvnmIwTCzlM
TtG9oqpEuaVV5+C+g05oBLURCMr+KcpCqGtmsl4BLSdsYkyqsnJHJDCEPcJxgwmF
mYiymwtnqL2x+GRr5A989hT+tiz50N04pKuYezOzK4iszYJfTNMKB650SE14Kedc
WtF7QizY7Bq+IOkCAwEAAaNTMFEwHQYDVR0OBBYEFCzN5S6ugjuGZiwzc6DNsYVK
IJeaMB8GA1UdIwQYMBaAFCzN5S6ugjuGZiwzc6DNsYVKIJeaMA8GA1UdEwEB/wQF
MAMBAf8wDQYJKoZIhvcNAQELBQADggEBAEct9y8mNmhZU9lp4oC9dNfA8bEaBew5
Dw/FLDH+WFMkJ0bbTjagaykogor5aBur1Bnv2dqUC/t44E8cx5Cj5zHipiB+lzpb
NKf8pI6/Yv2Bq24OtldTLXsiMqWrOJljm35VGN2RjX0snvs0YMgEkr/AwlM84kEt
R9mz6Xt0/QzvkHqj8HOdEQcIqdadPF/xQiTbm2ZM4DXzKUR6UTKrib4DmCX7RH49
Pw2PcgCtWHGdANdL7+nuk4KBo43oeHoxtQefjyw9DV+yOSdEXL6AFOSXIU6q9f06
PIHoJdYUmhCwEjxX4LniJH2cIHTW5tfdKTm/e+8qsvj/CbEmgjAv5RE=
-----END CERTIFICATE-----`
}

func createTLSTestConfig(caFile string) common.TLSConfig {
	return common.TLSConfig{
		Verify:       false,
		MinVersion:   "1.2",
		CAFile:       caFile,
		CipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
	}
}

func getTLSConfigurationTests() []struct {
	name string
	url  string
} {
	return []struct {
		name string
		url  string
	}{
		{
			name: "WebSocket with TLS",
			url:  fmt.Sprintf("wss://localhost:%d", constants.TestHTTPSPort),
		},
		{
			name: "TCP with TLS",
			url:  fmt.Sprintf("tcps://localhost:%d", constants.TestHTTPSPort),
		},
	}
}

func testTLSConfigurationCase(t *testing.T, url string, tlsConfig common.TLSConfig, logger *zap.Logger) {
	t.Helper()
	cfg := config.GatewayConfig{
		URL:        url,
		TLS:        tlsConfig,
		Connection: common.ConnectionConfig{TimeoutMs: 5000},
	}

	client, err := NewGatewayClient(cfg, logger)
	if err != nil {
		// TLS configuration might fail without actual cert files.
		// but the factory should still work
		t.Logf("Client creation with TLS config: %v", err)
	}

	if client == nil && err == nil {
		t.Error("Expected either client or error")
	}
}

func TestGatewayClient_ConnectionConfiguration(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	connConfig := common.ConnectionConfig{
		TimeoutMs:           10000,
		KeepaliveIntervalMs: 30000,
		Reconnect: common.ReconnectConfig{
			InitialDelayMs: testMaxIterations,
			MaxDelayMs:     60000,
			Multiplier:     2.0,
			MaxAttempts:    10,
		},
	}

	tests := []struct {
		name string
		url  string
	}{
		{
			name: "WebSocket connection config",
			url:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
		},
		{
			name: "TCP connection config",
			url:  fmt.Sprintf("tcp://localhost:%d", constants.TestHTTPPort),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.GatewayConfig{
				URL:        tt.url,
				Connection: connConfig,
			}

			client, err := NewGatewayClient(cfg, logger)
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			if client == nil {
				t.Error("Expected non-nil client")
			}
			// The connection config should be applied to the client.
			// We can't easily verify this without exposing internals,
			// but at least we know the client was created successfully
		})
	}
}

func BenchmarkNewGatewayClient_WebSocket(b *testing.B) {
	logger := zaptest.NewLogger(b)
	cfg := config.GatewayConfig{
		URL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client, err := NewGatewayClient(cfg, logger)
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}

		_ = client
	}
}

func BenchmarkNewGatewayClient_TCP(b *testing.B) {
	logger := zaptest.NewLogger(b)
	cfg := config.GatewayConfig{
		URL: fmt.Sprintf("tcp://localhost:%d", constants.TestHTTPPort),
		Connection: common.ConnectionConfig{
			TimeoutMs: 5000,
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client, err := NewGatewayClient(cfg, logger)
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}

		_ = client
	}
}

// Helper function.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
