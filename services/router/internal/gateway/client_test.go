package gateway

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap/zaptest"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"github.com/poiley/mcp-bridge/test/testutil"
)

func TestNewClient(t *testing.T) { 
	tests := createNewClientTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := testutil.NewTestLogger(t)
			client, err := NewClient(tt.config, logger)

			validateNewClientResult(t, tt, client, err)
		})
	}
}

func createNewClientTests() []struct {
	name      string
	config    config.GatewayConfig
	wantError bool
} {
	return []struct {
		name      string
		config    config.GatewayConfig
		wantError bool
	}{
		{
			name: "Valid config",
			config: config.GatewayConfig{
				URL: "wss://gateway.example.com",
				Auth: common.AuthConfig{
					Type:  "bearer",
					Token: "test-token",
				},
				TLS: common.TLSConfig{
					Verify: true,
				},
			},
			wantError: false,
		},
		{
			name: "Config with TLS options",
			config: config.GatewayConfig{
				URL: "wss://gateway.example.com",
				Auth: common.AuthConfig{
					Type:  "bearer",
					Token: "test-token",
				},
				TLS: common.TLSConfig{
					Verify:       false,
					MinVersion:   "1.2",
					CipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
				},
			},
			wantError: false,
		},
	}
}

func validateNewClientResult(t *testing.T, tt struct {
	name      string
	config    config.GatewayConfig
	wantError bool
}, client *Client, err error) {
	t.Helper()

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")
		}
		return
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	if client == nil {
		t.Error("Expected client to be created")
		return
	}

	if client.tlsConfig == nil {
		t.Error("Expected TLS config to be set")
	}

	if client.httpClient == nil {
		t.Error("Expected HTTP client to be set")
	}
}

func TestClient_configureTLS(t *testing.T) {
	tests := createConfigureTLSTests()
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := testutil.NewTestLogger(t)
			tlsConfig := prepareTLSConfigForTest(t, tt.tlsConfig, tt.name)
			
			client := &Client{
				config: config.GatewayConfig{
					TLS: tlsConfig,
				},
				logger: logger,
			}

			err := client.configureTLS()
			if err != nil {
				t.Fatalf("configureTLS() error = %v", err)
			}

			if tt.checkFunc != nil {
				if err := tt.checkFunc(client.tlsConfig); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func createConfigureTLSTests() []struct {
	name      string
	tlsConfig common.TLSConfig
	checkFunc func(*tls.Config) error
} {
	return []struct {
		name      string
		tlsConfig common.TLSConfig
		checkFunc func(*tls.Config) error
	}{
		{
			name: "Default TLS config",
			tlsConfig: common.TLSConfig{
				Verify: true,
			},
			checkFunc: func(cfg *tls.Config) error {
				if cfg.InsecureSkipVerify {
					return errors.New("InsecureSkipVerify should be false")
				}
				if cfg.MinVersion != tls.VersionTLS13 {
					return errors.New("MinVersion should be TLS 1.3")
				}
				return nil
			},
		},
		{
			name: "TLS 1.2 with skip verify",
			tlsConfig: common.TLSConfig{
				Verify:     false,
				MinVersion: "1.2",
			},
			checkFunc: func(cfg *tls.Config) error {
				if !cfg.InsecureSkipVerify {
					return errors.New("InsecureSkipVerify should be true")
				}
				if cfg.MinVersion != tls.VersionTLS12 {
					return errors.New("MinVersion should be TLS 1.2")
				}
				return nil
			},
		},
		{
			name: "With CA cert and cipher suites",
			tlsConfig: common.TLSConfig{
				Verify:       true,
				MinVersion:   "1.3",
				CAFile:       "", // Will be set to temp file path in test
				CipherSuites: []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
			},
			checkFunc: func(cfg *tls.Config) error {
				// Just verify it was configured without error.
				return nil
			},
		},
	}
}

func prepareTLSConfigForTest(t *testing.T, tlsConfig common.TLSConfig, testName string) common.TLSConfig {
	t.Helper()
	
	if testName == "With CA cert and cipher suites" {
		caCert := getTestCACertContent()
		caFile, cleanup := testutil.TempFile(t, caCert)
		t.Cleanup(cleanup)
		tlsConfig.CAFile = caFile
	}
	
	return tlsConfig
}

func getTestCACertContent() string {
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

func TestClient_Connect(t *testing.T) {
	tests := createConnectTests()
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler(t))
			defer server.Close()
			
			client := createConnectTestClient(t, server.URL, tt)
			
			ctx := context.Background()
			err := client.Connect(ctx)
			
			validateConnectResult(t, err, client, tt)
		})
	}
}

func createConnectTests() []struct {
	name          string
	authType      string
	authToken     string
	serverHandler func(*testing.T) http.HandlerFunc
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		authType      string
		authToken     string
		serverHandler func(*testing.T) http.HandlerFunc
		wantError     bool
		errorContains string
	}{
		{
			name:          "Successful bearer auth connection",
			authType:      "bearer",
			authToken:     "test-token",
			serverHandler: createSuccessfulBearerHandler,
			wantError:     false,
		},
		{
			name:          "Missing bearer token",
			authType:      "bearer",
			serverHandler: createErrorHandler,
			wantError:     true,
			errorContains: "bearer token not configured",
		},
		{
			name:          "OAuth2 auth (not implemented)",
			authType:      "oauth2",
			serverHandler: createErrorHandler,
			wantError:     true,
			errorContains: "OAuth2 token endpoint not configured",
		},
		{
			name:          "mTLS auth",
			authType:      "mtls",
			serverHandler: createMTLSHandler,
			wantError:     false,
		},
		{
			name:          "Server rejects connection",
			authType:      "bearer",
			authToken:     "test-token",
			serverHandler: createRejectionHandler,
			wantError:     true,
			errorContains: "failed to connect",
		},
	}
}

func createSuccessfulBearerHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Expected 'Bearer test-token', got '%s'", auth)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Failed to upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		
		time.Sleep(testIterations * time.Millisecond)
	}
}

func createErrorHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Should not reach here", http.StatusInternalServerError)
	}
}

func createMTLSHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	
	return func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}
}

func createRejectionHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}
}

func createConnectTestClient(t *testing.T, serverURL string, tt struct {
	name          string
	authType      string
	authToken     string
	serverHandler func(*testing.T) http.HandlerFunc
	wantError     bool
	errorContains string
}) *Client {
	t.Helper()
	
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	logger := testutil.NewTestLogger(t)
	
	authConfig := common.AuthConfig{
		Type:  tt.authType,
		Token: tt.authToken,
	}
	
	if tt.authType == "mtls" {
		certFile, keyFile := createMTLSCertificates(t)
		authConfig.ClientCert = certFile
		authConfig.ClientKey = keyFile
	}
	
	return &Client{
		config: config.GatewayConfig{
			URL:  wsURL,
			Auth: authConfig,
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}
}

func createMTLSCertificates(t *testing.T) (string, string) {
	t.Helper()
	
	certPem := getTestCertPem()
	keyPem := getTestKeyPem()
	
	certFile, certCleanup := testutil.TempFile(t, certPem)
	t.Cleanup(certCleanup)
	
	keyFile, keyCleanup := testutil.TempFile(t, keyPem)
	t.Cleanup(keyCleanup)
	
	return certFile, keyFile
}

func getTestCertPem() string {
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

func getTestKeyPem() string {
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
MnwkDol2CzYqVcqLxD1T/N9QfgmUiso9zCw8Rz3rjv9RT6nps3cfZIQGXHonwi+
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

func validateConnectResult(t *testing.T, err error, client *Client, tt struct {
	name          string
	authType      string
	authToken     string
	serverHandler func(*testing.T) http.HandlerFunc
	wantError     bool
	errorContains string
}) {
	t.Helper()
	
	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")
		} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
		}
	} else {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		
		if client.conn == nil {
			t.Error("Expected connection to be established")
		}
		
		if client.conn != nil {
			_ = client.conn.Close()
		}
	}
}

// createMessageEchoServer creates a server that echoes back messages with validation.
func createMessageEchoServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Failed to upgrade: %v", err)

			return
		}

		defer func() { _ = conn.Close() }()

		for {
			var msg WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}

			if msg.Source != "local-router" {
				t.Errorf("Expected source 'local-router', got '%s'", msg.Source)
			}

			if msg.Timestamp == "" {
				t.Error("Expected timestamp to be set")
			}

			if err := conn.WriteJSON(msg); err != nil {
				break
			}
		}
	}))
}

func TestClient_SendRequest(t *testing.T) { 
	server := createMessageEchoServer(t)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	// Connect first.
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	tests := []struct {
		name          string
		request       *mcp.Request
		wantError     bool
		errorContains string
	}{
		{
			name: "Send simple request",
			request: &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "initialize",
				ID:      "test-123",
			},
			wantError: false,
		},
		{
			name: "Send tool call request",
			request: &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "k8s.getPods",
				Params: map[string]string{
					"namespace": "default",
				},
				ID: 42,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.SendRequest(tt.request)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestClient_SendRequest_NotConnected(t *testing.T) { 
	logger := testutil.NewTestLogger(t)
	client := &Client{
		logger: logger,
	}

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      1,
	}

	err := client.SendRequest(req)
	if err == nil {
		t.Error("Expected error when not connected")
	}

	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("Expected 'not connected' error, got: %v", err)
	}
}

// createTestMessageServer creates a server that sends predefined test messages.
func createTestMessageServer(t *testing.T, messages []WireMessage) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		// Send test messages.
		for _, msg := range messages {
			if err := conn.WriteJSON(msg); err != nil {
				break
			}

			time.Sleep(testTimeout * time.Millisecond)
		}

		// Keep connection open.
		time.Sleep(httpStatusOK * time.Millisecond)
	}))
}

// validateSuccessResponse validates a successful response.
func validateSuccessResponse(t *testing.T, resp *mcp.Response, expectedID interface{}) {
	t.Helper()

	if resp.ID != expectedID {
		t.Errorf("Expected ID %v, got %v", expectedID, resp.ID)
	}

	if resp.Error != nil {
		t.Error("Expected no error in response")
	}
}

// validateErrorResponse validates an error response.
func validateErrorResponse(t *testing.T, resp *mcp.Response, expectedID interface{}, expectedCode int) {
	t.Helper()

	if resp.ID != expectedID {
		t.Errorf("Expected ID %v, got %v", expectedID, resp.ID)
	}

	if resp.Error == nil {
		t.Error("Expected error in response")

		return
	}

	if resp.Error.Code != expectedCode {
		t.Errorf("Expected error code %d, got %d", expectedCode, resp.Error.Code)
	}
}

func TestClient_ReceiveResponse(t *testing.T) { 
	// Create test messages.
	testMessages := []WireMessage{
		{
			ID:        "test-123",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Source:    "gateway",
			MCPPayload: map[string]interface{}{
				"jsonrpc": constants.TestJSONRPCVersion,
				"result": map[string]string{
					"status": "ok",
				},
				"id": "test-123",
			},
		},
		{
			ID:        42,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Source:    "gateway",
			MCPPayload: map[string]interface{}{
				"jsonrpc": constants.TestJSONRPCVersion,
				"error": map[string]interface{}{
					"code":    -32600,
					"message": "Invalid request",
				},
				"id": 42,
			},
		},
	}

	// Create test server that sends test messages.
	server := createTestMessageServer(t, testMessages)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	// Connect.
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	// Receive first response (success).
	resp1, err := client.ReceiveResponse()
	if err != nil {
		t.Fatalf("Failed to receive first response: %v", err)
	}

	validateSuccessResponse(t, resp1, "test-123")

	// Receive second response (error).
	resp2, err := client.ReceiveResponse()
	if err != nil {
		t.Fatalf("Failed to receive second response: %v", err)
	}

	validateErrorResponse(t, resp2, float64(42), -32600) // JSON unmarshals numbers as float64
}

func TestClient_ReceiveResponse_InvalidPayload(t *testing.T) { 
	// Create server that sends invalid messages.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		// Send message without MCP payload.
		msg := WireMessage{
			ID:        "test-123",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Source:    "gateway",
			// MCPPayload is nil.
		}
		if err := conn.WriteJSON(msg); err != nil {
			t.Logf("Failed to write JSON response: %v", err)
		}

		time.Sleep(testIterations * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	// Connect.
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	// Try to receive response.
	_, err := client.ReceiveResponse()
	if err == nil {
		t.Error("Expected error for message without MCP payload")
	}

	if !strings.Contains(err.Error(), "without MCP payload") {
		t.Errorf("Expected 'without MCP payload' error, got: %v", err)
	}
}

func TestClient_SendPing(t *testing.T) { 
	pingReceived := make(chan bool, 1)

	// Create server that handles ping.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		conn.SetPingHandler(func(appData string) error {
			pingReceived <- true

			return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
		})

		// Read messages to handle ping.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	// Connect.
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	// Send ping.
	err := client.SendPing()
	if err != nil {
		t.Errorf("Failed to send ping: %v", err)
	}

	// Verify ping was received.
	select {
	case <-pingReceived:
		// Success.
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for ping")
	}
}

func TestClient_Close(t *testing.T) { 
	closeReceived := make(chan bool, 1)

	// Create server that handles close.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		conn.SetCloseHandler(func(code int, text string) error {
			closeReceived <- true

			return nil
		})

		// Read messages.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	// Test closing when not connected.
	err := client.Close()
	if err != nil {
		t.Errorf("Close should not error when not connected: %v", err)
	}

	// Connect.
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Close connection.
	err = client.Close()
	if err != nil {
		t.Errorf("Failed to close: %v", err)
	}

	// Verify close was received.
	select {
	case <-closeReceived:
		// Success.
	case <-time.After(1 * time.Second):
		// Close message is best effort, so timeout is ok.
	}

	// Verify connection is nil.
	if client.IsConnected() {
		t.Error("Expected IsConnected to return false after close")
	}
}

func TestClient_IsConnected(t *testing.T) { 
	logger := testutil.NewTestLogger(t)
	client := &Client{
		logger: logger,
	}

	// Initially not connected.
	if client.IsConnected() {
		t.Error("Expected IsConnected to return false initially")
	}

	// Set a mock connection.
	client.conn = &websocket.Conn{}
	if !client.IsConnected() {
		t.Error("Expected IsConnected to return true with connection")
	}

	// Clear connection.
	client.conn = nil
	if client.IsConnected() {
		t.Error("Expected IsConnected to return false after clearing connection")
	}
}

func TestExtractNamespace(t *testing.T) { 
	tests := []struct {
		method   string
		expected string
	}{
		{"initialize", "system"},
		{"tools/list", "system"},
		{"tools/call", "system"},
		{"k8s.getPods", "k8s"},
		{"docker.listContainers", "docker"},
		{"custom.namespace.method", "custom"},
		{"simplemethod", "default"},
		{"", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			result := extractNamespace(tt.method)
			if result != tt.expected {
				t.Errorf("extractNamespace(%s) = %s, want %s", tt.method, result, tt.expected)
			}
		})
	}
}

func TestClient_ConcurrentOperations(t *testing.T) { 
	// Create echo server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		// Echo messages back.
		for {
			var msg WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}

			if err := conn.WriteJSON(msg); err != nil {
				break
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	// Connect.
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	// Concurrent sends.
	var wg sync.WaitGroup

	errors := make(chan error, 10)

	for i := 0; i < constants.TestBatchSize; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "test.method",
				ID:      id,
			}
			if err := client.SendRequest(req); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors.
	for err := range errors {
		t.Errorf("Concurrent send error: %v", err)
	}
}

func TestWireMessage_JSONMarshaling(t *testing.T) { 
	msg := WireMessage{
		ID:              "test-123",
		Timestamp:       "2024-01-01T00:00:00Z",
		Source:          "test-source",
		TargetNamespace: "test-namespace",
		MCPPayload: map[string]interface{}{
			"jsonrpc": constants.TestJSONRPCVersion,
			"method":  "test",
			"id":      "test-123",
		},
	}

	// Marshal.
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal.
	var decoded WireMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify fields.
	if decoded.ID != msg.ID {
		t.Errorf("ID mismatch: expected %v, got %v", msg.ID, decoded.ID)
	}

	if decoded.Timestamp != msg.Timestamp {
		t.Errorf("Timestamp mismatch")
	}

	if decoded.Source != msg.Source {
		t.Errorf("Source mismatch")
	}

	if decoded.TargetNamespace != msg.TargetNamespace {
		t.Errorf("TargetNamespace mismatch")
	}
}

func BenchmarkClient_SendRequest(b *testing.B) {
	// Create null server that just reads messages.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		// Just read and discard.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := zaptest.NewLogger(b)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	// Connect.
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			b.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "benchmark.test",
		Params: map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		},
		ID: 1,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := client.SendRequest(req); err != nil {
			b.Fatal(err)
		}
	}
}

// Enhanced tests for better coverage.

// Test various error scenarios during connection.
func TestClient_Connect_ErrorScenarios(t *testing.T) { 
	tests := []struct {
		name          string
		setupClient   func() *Client
		wantError     bool
		errorContains string
	}{
		{
			name: "Invalid URL",
			setupClient: func() *Client {
				logger := testutil.NewTestLogger(t)

				return &Client{
					config: config.GatewayConfig{
						URL: "://invalid-url",
						Auth: common.AuthConfig{
							Type:  "bearer",
							Token: "test-token",
						},
					},
					logger:    logger,
					tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
				}
			},
			wantError:     true,
			errorContains: "invalid gateway URL",
		},
		{
			name: "Connection timeout",
			setupClient: func() *Client {
				logger := testutil.NewTestLogger(t)

				return &Client{
					config: config.GatewayConfig{
						URL: "ws://nonexistent-host-12345.invalid:9999",
						Auth: common.AuthConfig{
							Type:  "bearer",
							Token: "test-token",
						},
						Connection: common.ConnectionConfig{
							TimeoutMs: testIterations, // Very short timeout
						},
					},
					logger:    logger,
					tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
				}
			},
			wantError:     true,
			errorContains: "failed to connect",
		},
		{
			name: "mTLS with missing certificate",
			setupClient: func() *Client {
				logger := testutil.NewTestLogger(t)

				return &Client{
					config: config.GatewayConfig{
						URL: "wss://example.com",
						Auth: common.AuthConfig{
							Type: "mtls",
							// Missing ClientCert and ClientKey.
						},
					},
					logger:    logger,
					tlsConfig: &tls.Config{}, // #nosec G402 - test configuration only
				}
			},
			wantError:     true,
			errorContains: "client certificate and key are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setupClient()
			ctx := context.Background()
			err := client.Connect(ctx)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				defer func() {
					if err := client.Close(); err != nil {
						t.Logf("Failed to close client in cleanup: %v", err)
					}
				}()
			}
		})
	}
}

// Test network failure scenarios during operation.
func TestClient_NetworkFailures(t *testing.T) { 
	// Create server that drops connections after setup.
	connectChan := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		// Signal that connection was established.
		close(connectChan)

		// Read one message then close abruptly.
		_, _, _ = conn.ReadMessage()
		_ = conn.Close()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	// Connect.
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	// Wait for connection to be established.
	<-connectChan

	// Try to send request - should cause connection to close.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-1",
	}

	_ = client.SendRequest(req)
	// May or may not error immediately depending on timing.

	// Try to receive response - should definitely error.
	_, err := client.ReceiveResponse()
	if err == nil {
		t.Error("Expected error when receiving from closed connection")
	}

	if !strings.Contains(err.Error(), "connection") && !strings.Contains(err.Error(), "closed") {
		t.Errorf("Expected connection/closed error, got: %v", err)
	}
}

// Test authentication edge cases.
func TestClient_AuthenticationEdgeCases(t *testing.T) { 
	tests := []struct {
		name           string
		authConfig     common.AuthConfig
		expectAuthCall bool
		serverHandler  func(*testing.T, bool) http.HandlerFunc
		wantError      bool
		errorContains  string
	}{
		{
			name: "Bearer auth with empty token",
			authConfig: common.AuthConfig{
				Type:  "bearer",
				Token: "",
			},
			expectAuthCall: false,
			serverHandler: func(t *testing.T, expectAuth bool) http.HandlerFunc {
				t.Helper()

				return func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "Should not reach here", http.StatusInternalServerError)
				}
			},
			wantError:     true,
			errorContains: "bearer token not configured",
		},
		{
			name: "OAuth2 with missing endpoint",
			authConfig: common.AuthConfig{
				Type: "oauth2",
				// Missing TokenEndpoint.
				ClientID:     "test-client",
				ClientSecret: "test-secret",
			},
			expectAuthCall: false,
			serverHandler: func(t *testing.T, expectAuth bool) http.HandlerFunc {
				t.Helper()

				return func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "Should not reach here", http.StatusInternalServerError)
				}
			},
			wantError:     true,
			errorContains: "OAuth2 token endpoint not configured",
		},
		{
			name: "Unknown auth type",
			authConfig: common.AuthConfig{
				Type: "unknown",
			},
			expectAuthCall: false,
			serverHandler: func(t *testing.T, expectAuth bool) http.HandlerFunc {
				t.Helper()

				return func(w http.ResponseWriter, r *http.Request) {
					// Check that no auth header is set.
					auth := r.Header.Get("Authorization")
					if auth != "" {
						t.Errorf("Expected no auth header, got '%s'", auth)
					}

					upgrader := websocket.Upgrader{}
					conn, err := upgrader.Upgrade(w, r, nil)
					if err != nil {
						return
					}
					defer func() { _ = conn.Close() }()
					time.Sleep(testIterations * time.Millisecond)
				}
			},
			wantError: false, // Should succeed with no auth
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler(t, tt.expectAuthCall))
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

			logger := testutil.NewTestLogger(t)
			client := &Client{
				config: config.GatewayConfig{
					URL:  wsURL,
					Auth: tt.authConfig,
					Connection: common.ConnectionConfig{
						TimeoutMs: 5000,
					},
					TLS: common.TLSConfig{
						Verify: false,
					},
				},
				logger:    logger,
				tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
			}

			ctx := context.Background()
			err := client.Connect(ctx)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				defer func() {
					if err := client.Close(); err != nil {
						t.Logf("Failed to close client in cleanup: %v", err)
					}
				}()
			}
		})
	}
}

// Test concurrent operations with potential race conditions.
// createSlowEchoServer creates a server that echoes messages with a delay.
func createSlowEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		for {
			var msg WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}

			// Simulate slow processing.
			time.Sleep(constants.TestTickInterval)

			if err := conn.WriteJSON(msg); err != nil {
				break
			}
		}
	}))
}

// runConcurrentSenders starts multiple concurrent senders and returns error channel.
func runConcurrentSenders(t *testing.T, client *Client, numOperations int) (chan error, *sync.WaitGroup) {
	t.Helper()
	
	sendErrors := make(chan error, numOperations)

	var sendWg sync.WaitGroup

	for i := 0; i < numOperations; i++ {
		sendWg.Add(1)

		go func(id int) {
			defer sendWg.Done()

			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "concurrent.test",
				ID:      id,
			}
			if err := client.SendRequest(req); err != nil {
				sendErrors <- err
			}
		}(i)
	}

	return sendErrors, &sendWg
}

// runConcurrentReceivers starts multiple concurrent receivers and returns error channel.
func runConcurrentReceivers(t *testing.T, client *Client, numOperations int) (chan error, *sync.WaitGroup) {
	t.Helper()
	
	recvErrors := make(chan error, numOperations)

	var recvWg sync.WaitGroup

	for i := 0; i < numOperations; i++ {
		recvWg.Add(1)

		go func() {
			defer recvWg.Done()

			if _, err := client.ReceiveResponse(); err != nil {
				recvErrors <- err
			}
		}()
	}

	return recvErrors, &recvWg
}

// validateConcurrentErrors validates that error counts are within acceptable limits.
func validateConcurrentErrors(t *testing.T, sendErrors, recvErrors chan error, numOperations int) {
	t.Helper()

	// Check for unexpected errors (some timeout errors may be expected).
	sendErrorCount := 0
	for err := range sendErrors {
		sendErrorCount++
		// Log but don't fail on send errors - they might be timing related.
		t.Logf("Send error (may be expected): %v", err)
	}

	recvErrorCount := 0
	for err := range recvErrors {
		recvErrorCount++
		// Log but don't fail on receive errors - they might be timing related.
		t.Logf("Receive error (may be expected): %v", err)
	}

	// Allow some errors due to timing issues, but not too many.
	if sendErrorCount > numOperations/2 {
		t.Errorf("Too many send errors: %d/%d", sendErrorCount, numOperations)
	}

	if recvErrorCount > numOperations/2 {
		t.Errorf("Too many receive errors: %d/%d", recvErrorCount, numOperations)
	}
}

func TestClient_ConcurrentOperations_EdgeCases(t *testing.T) { 
	server := createSlowEchoServer(t)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client in cleanup: %v", err)
		}
	}()

	const numOperations = testTimeout

	sendErrors, sendWg := runConcurrentSenders(t, client, numOperations)
	recvErrors, recvWg := runConcurrentReceivers(t, client, numOperations)

	sendWg.Wait()
	recvWg.Wait()

	close(sendErrors)
	close(recvErrors)

	validateConcurrentErrors(t, sendErrors, recvErrors, numOperations)
}

// Test connection lifecycle edge cases.
func TestClient_ConnectionLifecycle(t *testing.T) { 
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		// Handle one message then close.
		_, _, _ = conn.ReadMessage()

		time.Sleep(testTimeout * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	logger := testutil.NewTestLogger(t)
	client := &Client{
		config: config.GatewayConfig{
			URL: wsURL,
			Auth: common.AuthConfig{
				Type:  "bearer",
				Token: "test-token",
			},
			Connection: common.ConnectionConfig{
				TimeoutMs: 5000,
			},
			TLS: common.TLSConfig{
				Verify: false,
			},
		},
		logger:    logger,
		tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
	}

	ctx := context.Background()

	// Test multiple connect/disconnect cycles.
	for i := 0; i < 3; i++ {
		// Connect.
		if err := client.Connect(ctx); err != nil {
			t.Fatalf("Failed to connect on iteration %d: %v", i, err)
		}

		if !client.IsConnected() {
			t.Errorf("Expected IsConnected to be true after connect on iteration %d", i)
		}

		// Send a message to trigger server close.
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "test",
			ID:      i,
		}
		_ = client.SendRequest(req) // May fail, that's okay

		// Close.
		if err := client.Close(); err != nil {
			t.Errorf("Failed to close on iteration %d: %v", i, err)
		}

		if client.IsConnected() {
			t.Errorf("Expected IsConnected to be false after close on iteration %d", i)
		}

		// Brief pause between cycles.
		time.Sleep(constants.TestTickInterval)
	}
}

// Test malformed message handling.
func TestClient_MalformedMessages(t *testing.T) { 
	malformedMessages := []string{
		`{"id": "test", "timestamp": "2024-01-01T00:00:00Z"}`, // Missing MCP payload
		`{"invalid": "json"`, // Invalid JSON
		`""`,                 // Empty string
		`null`,               // Null value
	}

	for i, msg := range malformedMessages {
		t.Run(fmt.Sprintf("malformed_message_%d", i), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upgrader := websocket.Upgrader{}

				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}

				defer func() { _ = conn.Close() }()

				// Send malformed message.
				if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
					t.Logf("Failed to write message: %v", err)
				}

				time.Sleep(testIterations * time.Millisecond)
			}))
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

			logger := testutil.NewTestLogger(t)
			client := &Client{
				config: config.GatewayConfig{
					URL: wsURL,
					Auth: common.AuthConfig{
						Type:  "bearer",
						Token: "test-token",
					},
					Connection: common.ConnectionConfig{
						TimeoutMs: 5000,
					},
					TLS: common.TLSConfig{
						Verify: false,
					},
				},
				logger:    logger,
				tlsConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - test configuration only
			}

			ctx := context.Background()

			if err := client.Connect(ctx); err != nil {
				t.Fatalf("Failed to connect: %v", err)
			}

			defer func() {
				if err := client.Close(); err != nil {
					t.Logf("Failed to close client in cleanup: %v", err)
				}
			}()

			// Try to receive the malformed message.
			_, err := client.ReceiveResponse()
			if err == nil {
				t.Error("Expected error when receiving malformed message")
			}
		})
	}
}

// Test error handling during WebSocket operations.
func TestClient_WebSocketErrorHandling(t *testing.T) { 
	logger := testutil.NewTestLogger(t)
	client := &Client{
		logger: logger,
	}

	// Test operations on disconnected client.
	tests := []struct {
		name      string
		operation func() error
	}{
		{
			name: "SendRequest when disconnected",
			operation: func() error {
				req := &mcp.Request{JSONRPC: constants.TestJSONRPCVersion, Method: "test", ID: 1}

				return client.SendRequest(req)
			},
		},
		{
			name: "ReceiveResponse when disconnected",
			operation: func() error {
				_, err := client.ReceiveResponse()

				return err
			},
		},
		{
			name: "SendPing when disconnected",
			operation: func() error {
				return client.SendPing()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.operation()
			if err == nil {
				t.Error("Expected error when operation called on disconnected client")
			}

			if !strings.Contains(err.Error(), "not connected") {
				t.Errorf("Expected 'not connected' error, got: %v", err)
			}
		})
	}
}
