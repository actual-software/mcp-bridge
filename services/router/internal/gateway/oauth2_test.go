package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
)

func TestOAuth2Token_IsExpired(t *testing.T) {
	tests := []struct {
		name    string
		token   OAuth2Token
		expired bool
	}{
		{
			name: "not expired",
			token: OAuth2Token{
				AccessToken: "valid",
				ExpiresAt:   time.Now().Add(time.Hour),
			},
			expired: false,
		},
		{
			name: "expired",
			token: OAuth2Token{
				AccessToken: "expired",
				ExpiresAt:   time.Now().Add(-time.Hour),
			},
			expired: true,
		},
		{
			name: "expires within buffer",
			token: OAuth2Token{
				AccessToken: "soon",
				ExpiresAt:   time.Now().Add(20 * time.Second),
			},
			expired: true, // Should be considered expired due to 30s buffer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsExpired(); got != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

func TestOAuth2Client_GetToken_ClientCredentials(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var requestCount int32

	server := setupOAuth2TestServer(t, &requestCount)
	defer server.Close()

	cfg := config.GatewayConfig{
		Auth: common.AuthConfig{
			Type:          "oauth2",
			GrantType:     "client_credentials",
			TokenEndpoint: server.URL,
			ClientID:      "test-client",
			ClientSecret:  "test-secret",
			Scope:         "read write",
		},
	}

	client := NewOAuth2Client(cfg, logger, http.DefaultClient)

	// First call should request token.
	token1, err := client.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}

	if token1 != "test-access-token" {
		t.Errorf("Expected token 'test-access-token', got %s", token1)
	}

	// Second call should use cached token.
	token2, err := client.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}

	if token2 != token1 {
		t.Errorf("Expected cached token, got different token")
	}

	// Verify only one request was made.
	if count := atomic.LoadInt32(&requestCount); count != 1 {
		t.Errorf("Expected 1 request, got %d", count)
	}
}

// setupOAuth2TestServer creates an OAuth2 test server with client credentials validation.
func setupOAuth2TestServer(t *testing.T, requestCount *int32) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(requestCount, 1)

		if err := validateOAuth2Request(t, r); err != nil {
			t.Errorf("OAuth2 request validation failed: %v", err)

			return
		}

		sendOAuth2TokenResponse(t, w)
	}))
}

// validateOAuth2Request validates the OAuth2 request format and credentials.
func validateOAuth2Request(t *testing.T, r *http.Request) error {
	t.Helper()

	if r.Method != http.MethodPost {
		return fmt.Errorf("expected POST, got %s", r.Method)
	}

	if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		return fmt.Errorf("expected Content-Type application/x-www-form-urlencoded, got %s", ct)
	}

	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("failed to parse form: %w", err)
	}

	if gt := r.FormValue("grant_type"); gt != "client_credentials" {
		return fmt.Errorf("expected grant_type=client_credentials, got %s", gt)
	}

	if clientID := r.FormValue("client_id"); clientID != "test-client" {
		return fmt.Errorf("expected client_id=test-client, got %s", clientID)
	}

	if clientSecret := r.FormValue("client_secret"); clientSecret != "test-secret" {
		return fmt.Errorf("expected client_secret=test-secret, got %s", clientSecret)
	}

	if scope := r.FormValue("scope"); scope != "read write" {
		return fmt.Errorf("expected scope='read write', got %s", scope)
	}

	return nil
}

// sendOAuth2TokenResponse sends a standard OAuth2 token response.
func sendOAuth2TokenResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	resp := OAuth2Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
		Scope:       "read write",
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Logf("Failed to encode response: %v", err)
	}
}

func TestOAuth2Client_GetToken_PasswordGrant(t *testing.T) {
	logger := zaptest.NewLogger(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse form.
		if err := r.ParseForm(); err != nil {
			t.Errorf("Failed to parse form: %v", err)
		}

		// Check grant type.
		if gt := r.FormValue("grant_type"); gt != "password" {
			t.Errorf("Expected grant_type=password, got %s", gt)
		}

		// Check credentials.
		if username := r.FormValue("username"); username != "testuser" {
			t.Errorf("Expected username=testuser, got %s", username)
		}

		if password := r.FormValue("password"); password != "testpass" {
			t.Errorf("Expected password=testpass, got %s", password)
		}

		// Send response.
		resp := OAuth2Token{
			AccessToken:  "user-token",
			TokenType:    "Bearer",
			ExpiresIn:    7200,
			RefreshToken: "refresh-token",
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Logf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	cfg := config.GatewayConfig{
		Auth: common.AuthConfig{
			Type:          "oauth2",
			GrantType:     "password",
			TokenEndpoint: server.URL,
			Username:      "testuser",
			Password:      "testpass",
			ClientID:      "app",
		},
	}

	client := NewOAuth2Client(cfg, logger, http.DefaultClient)

	token, err := client.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}

	if token != "user-token" {
		t.Errorf("Expected token 'user-token', got %s", token)
	}

	// Verify refresh token was stored.
	if client.token.RefreshToken != "refresh-token" {
		t.Errorf("Expected refresh token to be stored")
	}
}

func TestOAuth2Client_RefreshToken(t *testing.T) {
	logger := zaptest.NewLogger(t)
	refreshCount := 0

	server := setupOAuth2RefreshTestServer(t, &refreshCount)
	defer server.Close()

	cfg := config.GatewayConfig{
		Auth: common.AuthConfig{
			Type:          "oauth2",
			GrantType:     "client_credentials",
			TokenEndpoint: server.URL,
			ClientID:      "test-client",
			ClientSecret:  "test-secret",
		},
	}

	client := NewOAuth2Client(cfg, logger, http.DefaultClient)

	// Get initial token.
	token1, err := client.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}

	if token1 != "initial-token" {
		t.Errorf("Expected initial-token, got %s", token1)
	}

	// Wait for token to expire.
	time.Sleep(2 * time.Second)

	// Next call should trigger refresh.
	token2, err := client.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken() after expiry error = %v", err)
	}

	if token2 != "refreshed-token" {
		t.Errorf("Expected refreshed-token, got %s", token2)
	}

	if refreshCount != 1 {
		t.Errorf("Expected 1 refresh, got %d", refreshCount)
	}
}

// setupOAuth2RefreshTestServer creates a server that handles both initial and refresh token requests.
func setupOAuth2RefreshTestServer(t *testing.T, refreshCount *int) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("Failed to parse form: %v", err)

			return
		}

		grantType := r.FormValue("grant_type")
		handleOAuth2GrantType(t, w, r, grantType, refreshCount)
	}))
}

// handleOAuth2GrantType processes different OAuth2 grant types.
func handleOAuth2GrantType(t *testing.T, w http.ResponseWriter, r *http.Request, grantType string, refreshCount *int) {
	t.Helper()

	switch grantType {
	case "client_credentials":
		sendInitialOAuth2Token(t, w)
	case "refresh_token":
		handleRefreshTokenRequest(t, w, r, refreshCount)
	default:
		t.Errorf("Unexpected grant_type: %s", grantType)
		w.WriteHeader(http.StatusBadRequest)
	}
}

// sendInitialOAuth2Token sends the initial OAuth2 token with short expiry.
func sendInitialOAuth2Token(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	resp := OAuth2Token{
		AccessToken:  "initial-token",
		TokenType:    "Bearer",
		ExpiresIn:    1, // Expires in 1 second
		RefreshToken: "refresh-token",
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Logf("Failed to encode response: %v", err)
	}
}

// handleRefreshTokenRequest processes refresh token requests and validates refresh token.
func handleRefreshTokenRequest(t *testing.T, w http.ResponseWriter, r *http.Request, refreshCount *int) {
	t.Helper()

	(*refreshCount)++

	if rt := r.FormValue("refresh_token"); rt != "refresh-token" {
		t.Errorf("Expected refresh_token=refresh-token, got %s", rt)
	}

	resp := OAuth2Token{
		AccessToken:  "refreshed-token",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: "new-refresh-token",
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Logf("Failed to encode response: %v", err)
	}
}

func TestOAuth2Client_ErrorHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tests := getOAuth2ErrorTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testOAuth2ErrorCase(t, tt, logger)
		})
	}
}

type oauth2ErrorTest struct {
	name        string
	handler     http.HandlerFunc
	wantErr     bool
	errContains string
}

func getOAuth2ErrorTests() []oauth2ErrorTest {
	return []oauth2ErrorTest{
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("Internal Server Error"))
			},
			wantErr:     true,
			errContains: "Internal Server Error",
		},
		{
			name: "invalid JSON response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("not-json"))
			},
			wantErr:     true,
			errContains: "parse",
		},
		{
			name: "unauthorized",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			},
			wantErr:     true,
			errContains: "invalid_client",
		},
	}
}

func testOAuth2ErrorCase(t *testing.T, tt oauth2ErrorTest, logger *zap.Logger) {
	t.Helper()
	server := httptest.NewServer(tt.handler)
	defer server.Close()

	cfg := config.GatewayConfig{
		Auth: common.AuthConfig{
			Type:          "oauth2",
			GrantType:     "client_credentials",
			TokenEndpoint: server.URL,
			ClientID:      "test",
			ClientSecret:  "test",
		},
	}

	client := NewOAuth2Client(cfg, logger, http.DefaultClient)
	_, err := client.GetToken(context.Background())

	if (err != nil) != tt.wantErr {
		t.Errorf("GetToken() error = %v, wantErr %v", err, tt.wantErr)
	}

	if err != nil && tt.errContains != "" {
		if !containsOAuth2(err.Error(), tt.errContains) {
			t.Errorf("Error = %v, want error containing %s", err, tt.errContains)
		}
	}
}

func TestOAuth2Client_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var requestCount int32
	server := createConcurrentOAuth2Server(t, &requestCount)
	defer server.Close()

	client := createOAuth2TestClient(server.URL, logger)
	runConcurrentOAuth2Test(t, client)
	verifySingleRequest(t, &requestCount)
}

func createConcurrentOAuth2Server(t *testing.T, requestCount *int32) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(requestCount, 1)
		time.Sleep(testIterations * time.Millisecond)

		resp := OAuth2Token{
			AccessToken: "concurrent-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Logf("Failed to encode response: %v", err)
		}
	}))
}

func createOAuth2TestClient(serverURL string, logger *zap.Logger) *OAuth2Client {
	cfg := config.GatewayConfig{
		Auth: common.AuthConfig{
			Type:          "oauth2",
			GrantType:     "client_credentials",
			TokenEndpoint: serverURL,
			ClientID:      "test",
			ClientSecret:  "test",
		},
	}

	return NewOAuth2Client(cfg, logger, http.DefaultClient)
}

func runConcurrentOAuth2Test(t *testing.T, client *OAuth2Client) {
	t.Helper()
	const numGoroutines = 10
	errors := make(chan error, numGoroutines)
	tokens := make(chan string, numGoroutines)

	launchOAuth2Goroutines(client, errors, tokens, numGoroutines)
	collectOAuth2Results(t, errors, tokens, numGoroutines)
}

func launchOAuth2Goroutines(client *OAuth2Client, errors chan error, tokens chan string, count int) {
	for i := 0; i < count; i++ {
		go func() {
			token, err := client.GetToken(context.Background())
			if err != nil {
				errors <- err
			} else {
				tokens <- token
			}
		}()
	}
}

func collectOAuth2Results(t *testing.T, errors chan error, tokens chan string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		select {
		case err := <-errors:
			t.Errorf("Goroutine error: %v", err)
		case token := <-tokens:
			if token != "concurrent-token" {
				t.Errorf("Expected concurrent-token, got %s", token)
			}
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for goroutine")
		}
	}
}

func verifySingleRequest(t *testing.T, requestCount *int32) {
	t.Helper()
	if count := atomic.LoadInt32(requestCount); count != 1 {
		t.Errorf("Expected 1 request, got %d", count)
	}
}

func TestOAuth2Client_ContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Server that delays response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.GatewayConfig{
		Auth: common.AuthConfig{
			Type:          "oauth2",
			GrantType:     "client_credentials",
			TokenEndpoint: server.URL,
			ClientID:      "test",
		},
	}

	// Create client with short timeout.
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}
	client := NewOAuth2Client(cfg, logger, httpClient)

	// Create context with timeout.
	ctx, cancel := context.WithTimeout(context.Background(), testIterations*time.Millisecond)
	defer cancel()

	// Should fail due to context cancellation.
	_, err := client.GetToken(ctx)
	if err == nil {
		t.Error("Expected error due to context cancellation")
	}

	if !containsOAuth2(err.Error(), "context") {
		t.Errorf("Expected context error, got %v", err)
	}
}

func TestOAuth2Client_UnsupportedGrantType(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.GatewayConfig{
		Auth: common.AuthConfig{
			Type:          "oauth2",
			GrantType:     "implicit", // Unsupported
			TokenEndpoint: "http://example.com",
		},
	}

	client := NewOAuth2Client(cfg, logger, http.DefaultClient)

	_, err := client.GetToken(context.Background())
	if err == nil {
		t.Error("Expected error for unsupported grant type")
	}

	if !containsOAuth2(err.Error(), "unsupported") {
		t.Errorf("Expected unsupported grant type error, got %v", err)
	}
}

// Helper function for OAuth2 tests.
func containsOAuth2(s, substr string) bool {
	return strings.Contains(s, substr)
}
