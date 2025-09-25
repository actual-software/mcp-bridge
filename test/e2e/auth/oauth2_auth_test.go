// Auth test files allow flexible style
//

package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OAuth2ServerState manages stateful OAuth2 server behavior.
type OAuth2ServerState struct {
	mu             sync.RWMutex
	tokens         map[string]*TokenInfo
	clients        map[string]*ClientInfo
	authCodes      map[string]*AuthCodeInfo
	refreshTokens  map[string]*RefreshTokenInfo
	privateKey     *rsa.PrivateKey
	publicKey      *rsa.PublicKey
	keyID          string
	issuer         string
	revokedTokens  map[string]time.Time
	failureMode    bool
	networkLatency time.Duration
}

// TokenInfo represents an OAuth2 token with lifecycle information.
type TokenInfo struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Scope        string    `json:"scope"`
	IssuedAt     time.Time `json:"-"`
	ExpiresAt    time.Time `json:"-"`
	Subject      string    `json:"-"`
	ClientID     string    `json:"-"`
}

// ClientInfo represents OAuth2 client information.
type ClientInfo struct {
	ClientID     string
	ClientSecret string
	RedirectURIs []string
	Scopes       []string
	GrantTypes   []string
}

// AuthCodeInfo represents authorization code information.
type AuthCodeInfo struct {
	Code        string
	ClientID    string
	RedirectURI string
	Scope       string
	Subject     string
	ExpiresAt   time.Time
	Used        bool
}

// RefreshTokenInfo represents refresh token information.
type RefreshTokenInfo struct {
	RefreshToken string
	AccessToken  string
	ClientID     string
	Subject      string
	Scope        string
	ExpiresAt    time.Time
}

// NewOAuth2ServerState creates a new stateful OAuth2 server.
func NewOAuth2ServerState() (*OAuth2ServerState, error) {
	// Generate RSA key pair for JWT signing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	state := &OAuth2ServerState{
		mu:             sync.RWMutex{}, // Zero value mutex for thread-safe operations
		tokens:         make(map[string]*TokenInfo),
		clients:        make(map[string]*ClientInfo),
		authCodes:      make(map[string]*AuthCodeInfo),
		refreshTokens:  make(map[string]*RefreshTokenInfo),
		privateKey:     privateKey,
		publicKey:      &privateKey.PublicKey,
		keyID:          "test-key-1",
		issuer:         "test-oauth2-server",
		revokedTokens:  make(map[string]time.Time),
		failureMode:    false, // Default to normal operation mode
		networkLatency: getEnvDuration("OAUTH2_MOCK_LATENCY", 100*time.Millisecond),
	}

	// Register default test client
	state.RegisterClient(&ClientInfo{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read", "mcp:write", "openid", "profile"},
		GrantTypes:   []string{"authorization_code", "client_credentials", "refresh_token"},
	})

	return state, nil
}

// RegisterClient registers a new OAuth2 client.
func (s *OAuth2ServerState) RegisterClient(client *ClientInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clients[client.ClientID] = client
}

// SetFailureMode enables/disables failure simulation.
func (s *OAuth2ServerState) SetFailureMode(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failureMode = enabled
}

// SetNetworkLatency sets simulated network latency.
func (s *OAuth2ServerState) SetNetworkLatency(latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.networkLatency = latency
}

// handleTokenEndpoint handles the OAuth2 token endpoint.
func handleTokenEndpoint(w http.ResponseWriter, r *http.Request, state *OAuth2ServerState) {
	// Simulate network latency
	if state.networkLatency > 0 {
		time.Sleep(state.networkLatency)
	}

	// Check for failure mode
	state.mu.RLock()
	failureMode := state.failureMode
	state.mu.RUnlock()

	if failureMode {
		http.Error(w, `{"error":"server_error","error_description":"Simulated server failure"}`,
			http.StatusInternalServerError)

		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"invalid_request","error_description":"Only POST method allowed"}`,
			http.StatusMethodNotAllowed)

		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_request","error_description":"Invalid form data"}`, http.StatusBadRequest)

		return
	}

	grantType := r.FormValue("grant_type")
	switch grantType {
	case "client_credentials":
		handleClientCredentials(w, r, state)
	case "authorization_code":
		handleAuthorizationCode(w, r, state)
	case "refresh_token":
		handleRefreshToken(w, r, state)
	default:
		http.Error(w, `{"error":"unsupported_grant_type","error_description":"Grant type not supported"}`,
			http.StatusBadRequest)
	}
}

// handleIntrospectEndpoint handles the token introspection endpoint.
func handleIntrospectEndpoint(t *testing.T, w http.ResponseWriter, r *http.Request, state *OAuth2ServerState) {
	t.Helper()

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusMethodNotAllowed)

		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"active":false}`, http.StatusOK)

		return
	}

	token := r.FormValue("token")
	introspectionResponse := introspectToken(token, state)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(introspectionResponse); err != nil {
		t.Logf("Failed to encode introspection response: %v", err)
	}
}

// handleJWKSEndpoint handles the JWKS endpoint.
func handleJWKSEndpoint(t *testing.T, w http.ResponseWriter, _ *http.Request, state *OAuth2ServerState) {
	t.Helper()
	// Convert to base64url encoding for JWK
	n := state.publicKey.N.Bytes()
	e := big.NewInt(int64(state.publicKey.E)).Bytes()

	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"use": "sig",
				"kid": state.keyID,
				"n":   base64URLEncode(n),
				"e":   base64URLEncode(e),
				"alg": "RS256",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(jwks); err != nil {
		t.Logf("Failed to encode JWKS: %v", err)
	}
}

// handleRevokeEndpoint handles the token revocation endpoint.
func handleRevokeEndpoint(w http.ResponseWriter, r *http.Request, state *OAuth2ServerState) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusMethodNotAllowed)

		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)

		return
	}

	token := r.FormValue("token")
	if token == "" {
		http.Error(w, `{"error":"invalid_request","error_description":"Token parameter required"}`, http.StatusBadRequest)

		return
	}

	// Validate client authentication
	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID = r.FormValue("client_id")
		clientSecret = r.FormValue("client_secret")
	}

	if !validateClient(clientID, clientSecret, state) {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)

		return
	}

	// Revoke the token
	state.mu.Lock()
	state.revokedTokens[token] = time.Now()
	// Also remove from active tokens
	delete(state.tokens, token)
	state.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

// handleAuthorizeEndpoint handles the authorization endpoint.
func handleAuthorizeEndpoint(w http.ResponseWriter, _ *http.Request) {
	// Implementation for authorization endpoint
	// This would handle the authorization code flow initiation
	http.Error(w, "Not implemented in mock", http.StatusNotImplemented)
}

// CreateRealisticOAuth2Server creates a stateful, realistic OAuth2 mock server.
func CreateRealisticOAuth2Server(t *testing.T) (*httptest.Server, *OAuth2ServerState) {
	t.Helper()

	state, err := NewOAuth2ServerState()
	require.NoError(t, err)

	mux := http.NewServeMux()

	// Token endpoint with proper validation and error handling
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		handleTokenEndpoint(w, r, state)
	})

	// Token introspection endpoint
	mux.HandleFunc("/introspect", func(w http.ResponseWriter, r *http.Request) {
		handleIntrospectEndpoint(t, w, r, state)
	})

	// JWKS endpoint with proper key rotation simulation
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		handleJWKSEndpoint(t, w, r, state)
	})

	// Token revocation endpoint
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		handleRevokeEndpoint(w, r, state)
	})

	// Authorization endpoint for authorization code flow
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		handleAuthorizeEndpoint(w, r)
	})

	server := httptest.NewServer(mux)
	state.issuer = server.URL

	t.Cleanup(func() {
		server.Close()
	})

	return server, state
}

// Helper functions for token handling

func handleClientCredentials(w http.ResponseWriter, r *http.Request, state *OAuth2ServerState) {
	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID = r.FormValue("client_id")
		clientSecret = r.FormValue("client_secret")
	}

	if !validateClient(clientID, clientSecret, state) {
		http.Error(w, `{"error":"invalid_client","error_description":"Client authentication failed"}`,
			http.StatusUnauthorized)

		return
	}

	scope := r.FormValue("scope")
	if scope == "" {
		scope = "mcp:read mcp:write"
	}

	// Validate requested scopes
	if !validateScopes(clientID, scope, state) {
		http.Error(w, `{"error":"invalid_scope","error_description":"Requested scope not allowed"}`, http.StatusBadRequest)

		return
	}

	// Generate JWT token
	token, err := generateJWTToken(clientID, clientID, scope, state)
	if err != nil {
		http.Error(w, `{"error":"server_error","error_description":"Failed to generate token"}`,
			http.StatusInternalServerError)

		return
	}

	tokenInfo := &TokenInfo{
		AccessToken:  token,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: "", // No refresh token for client credentials flow
		Scope:        scope,
		IssuedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
		Subject:      clientID,
		ClientID:     clientID,
	}

	state.mu.Lock()
	state.tokens[token] = tokenInfo
	state.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(tokenInfo); err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
	}
}

func handleAuthorizationCode(w http.ResponseWriter, r *http.Request,
	state *OAuth2ServerState, // OAuth2 authorization code handler with required complexity
) {
	code := r.FormValue("code")
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")
	redirectURI := r.FormValue("redirect_uri")

	if !validateClient(clientID, clientSecret, state) {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)

		return
	}

	authCode := validateAndRetrieveAuthCode(w, code, clientID, redirectURI, state)
	if authCode == nil {
		return
	}

	tokenInfo := generateTokensFromAuthCode(w, authCode, clientID, state)
	if tokenInfo == nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(tokenInfo); err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
	}
}

func validateAndRetrieveAuthCode(w http.ResponseWriter, code, clientID, redirectURI string,
	state *OAuth2ServerState) *AuthCodeInfo {
	state.mu.RLock()
	authCode, exists := state.authCodes[code]
	state.mu.RUnlock()

	if !exists || authCode.Used || time.Now().After(authCode.ExpiresAt) {
		http.Error(w, `{"error":"invalid_grant","error_description":"Authorization code is invalid or expired"}`,
			http.StatusBadRequest)

		return nil
	}

	if authCode.ClientID != clientID || authCode.RedirectURI != redirectURI {
		http.Error(w, `{"error":"invalid_grant","error_description":"Authorization code does not match client"}`,
			http.StatusBadRequest)

		return nil
	}

	// Mark code as used
	state.mu.Lock()

	authCode.Used = true

	state.mu.Unlock()

	return authCode
}

func generateTokensFromAuthCode(w http.ResponseWriter, authCode *AuthCodeInfo,
	clientID string, state *OAuth2ServerState) *TokenInfo {
	accessToken, err := generateJWTToken(authCode.ClientID, authCode.Subject, authCode.Scope, state)
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)

		return nil
	}

	refreshToken := generateRefreshToken()
	tokenInfo := &TokenInfo{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: refreshToken,
		Scope:        authCode.Scope,
		IssuedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
		Subject:      authCode.Subject,
		ClientID:     clientID,
	}

	state.mu.Lock()
	state.tokens[accessToken] = tokenInfo
	state.refreshTokens[refreshToken] = &RefreshTokenInfo{
		RefreshToken: refreshToken,
		AccessToken:  accessToken,
		ClientID:     clientID,
		Subject:      authCode.Subject,
		Scope:        authCode.Scope,
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour), // 30 days
	}
	state.mu.Unlock()

	return tokenInfo
}

func handleRefreshToken(w http.ResponseWriter, r *http.Request, state *OAuth2ServerState) {
	refreshToken := r.FormValue("refresh_token")
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	if !validateClient(clientID, clientSecret, state) {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)

		return
	}

	state.mu.RLock()
	refreshInfo, exists := state.refreshTokens[refreshToken]
	state.mu.RUnlock()

	if !exists || time.Now().After(refreshInfo.ExpiresAt) {
		http.Error(w, `{"error":"invalid_grant","error_description":"Refresh token is invalid or expired"}`,
			http.StatusBadRequest)

		return
	}

	if refreshInfo.ClientID != clientID {
		http.Error(w, `{"error":"invalid_grant","error_description":"Refresh token does not belong to client"}`,
			http.StatusBadRequest)

		return
	}

	// Generate new access token
	newAccessToken, err := generateJWTToken(clientID, refreshInfo.Subject, refreshInfo.Scope, state)
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)

		return
	}

	tokenInfo := &TokenInfo{
		AccessToken:  newAccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		RefreshToken: "", // New access token doesn't include refresh token
		Scope:        refreshInfo.Scope,
		IssuedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
		Subject:      refreshInfo.Subject,
		ClientID:     clientID,
	}

	state.mu.Lock()
	// Remove old access token
	delete(state.tokens, refreshInfo.AccessToken)
	// Store new access token
	state.tokens[newAccessToken] = tokenInfo
	// Update refresh token info
	refreshInfo.AccessToken = newAccessToken

	state.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(tokenInfo); err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
	}
}

func introspectToken(token string, state *OAuth2ServerState) map[string]interface{} {
	state.mu.RLock()
	defer state.mu.RUnlock()

	// Check if token is revoked
	if _, revoked := state.revokedTokens[token]; revoked {
		return map[string]interface{}{"active": false}
	}

	// Check if token exists and is valid
	tokenInfo, exists := state.tokens[token]
	if !exists || time.Now().After(tokenInfo.ExpiresAt) {
		return map[string]interface{}{"active": false}
	}

	return map[string]interface{}{
		"active":    true,
		"sub":       tokenInfo.Subject,
		"client_id": tokenInfo.ClientID,
		"scope":     tokenInfo.Scope,
		"exp":       tokenInfo.ExpiresAt.Unix(),
		"iat":       tokenInfo.IssuedAt.Unix(),
		"iss":       state.issuer,
	}
}

func validateClient(clientID, clientSecret string, state *OAuth2ServerState) bool {
	state.mu.RLock()
	defer state.mu.RUnlock()

	client, exists := state.clients[clientID]
	if !exists {
		return false
	}

	return client.ClientSecret == clientSecret
}

func validateScopes(clientID, requestedScope string, state *OAuth2ServerState) bool {
	state.mu.RLock()
	defer state.mu.RUnlock()

	client, exists := state.clients[clientID]
	if !exists {
		return false
	}

	requestedScopes := strings.Fields(requestedScope)
	for _, requested := range requestedScopes {
		found := false

		for _, allowed := range client.Scopes {
			if requested == allowed {
				found = true

				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}

func generateJWTToken(clientID, subject, scope string, state *OAuth2ServerState) (string, error) {
	claims := jwt.MapClaims{
		"iss":       state.issuer,
		"sub":       subject,
		"aud":       []string{clientID},
		"exp":       time.Now().Add(time.Hour).Unix(),
		"iat":       time.Now().Unix(),
		"scope":     scope,
		"client_id": clientID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = state.keyID

	return token.SignedString(state.privateKey)
}

func generateRefreshToken() string {
	// Generate a secure random refresh token
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// In case of error, fall back to a pseudo-random token
		return fmt.Sprintf("rt_fallback_%d", time.Now().UnixNano())
	}

	return fmt.Sprintf("rt_%x", bytes)
}

// TestOAuth2Integration demonstrates comprehensive OAuth2 testing.
func TestOAuth2Integration(t *testing.T) {
	server, state := CreateRealisticOAuth2Server(t)
	defer server.Close()

	t.Run("client_credentials_flow", func(t *testing.T) {
		testClientCredentialsFlow(t, server.URL, state)
	})

	t.Run("token_expiration", func(t *testing.T) {
		testTokenExpiration(t, server.URL, state)
	})

	t.Run("token_revocation", func(t *testing.T) {
		testTokenRevocation(t, server.URL, state)
	})

	t.Run("invalid_client", func(t *testing.T) {
		testInvalidClient(t, server.URL, state)
	})

	t.Run("server_failure_mode", func(t *testing.T) {
		testServerFailureMode(t, server.URL, state)
	})

	t.Run("network_latency", func(t *testing.T) {
		testNetworkLatency(t, server.URL, state)
	})
}

func testClientCredentialsFlow(t *testing.T, serverURL string, _ *OAuth2ServerState) {
	t.Helper()

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("scope", "mcp:read mcp:write")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/token",
		strings.NewReader(data.Encode()))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("test-client", "test-secret")

	client := &http.Client{
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar needed for this test
		Timeout:       0,   // No timeout set, use default
	}
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var tokenResponse TokenInfo

	err = json.NewDecoder(resp.Body).Decode(&tokenResponse)
	require.NoError(t, err)

	assert.Equal(t, "Bearer", tokenResponse.TokenType)
	assert.Equal(t, "mcp:read mcp:write", tokenResponse.Scope)
	assert.NotEmpty(t, tokenResponse.AccessToken)
	assert.Positive(t, tokenResponse.ExpiresIn)
}

func testTokenExpiration(t *testing.T, serverURL string, state *OAuth2ServerState) {
	t.Helper()
	// Create a token with very short expiration
	state.mu.Lock()

	expiredToken := "expired-token"
	state.tokens[expiredToken] = &TokenInfo{
		AccessToken:  expiredToken,
		TokenType:    "Bearer",                       // Standard OAuth2 token type
		ExpiresIn:    3600,                           // Standard expiration time in seconds
		RefreshToken: "",                             // No refresh token for this test token
		Scope:        "mcp:read",                     // Default scope for test
		IssuedAt:     time.Now().Add(-2 * time.Hour), // Issued 2 hours ago
		ExpiresAt:    time.Now().Add(-time.Hour),     // Expired 1 hour ago
		Subject:      "test-user",
		ClientID:     "test-client",
	}
	state.mu.Unlock()

	// Test introspection of expired token
	data := url.Values{}
	data.Set("token", expiredToken)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/introspect",
		strings.NewReader(data.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar needed for this test
		Timeout:       0,   // No timeout set, use default
	}
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	var introspection map[string]interface{}

	err = json.NewDecoder(resp.Body).Decode(&introspection)
	require.NoError(t, err)

	if active, ok := introspection["active"].(bool); ok {
		assert.False(t, active)
	}
}

func testTokenRevocation(t *testing.T, serverURL string,
	_ *OAuth2ServerState,
) {
	t.Helper()
	// First create a valid token
	tokenResponse := createTokenForRevocation(t, serverURL)

	// Now revoke the token
	revokeToken(t, serverURL, tokenResponse.AccessToken)

	// Verify token is no longer active
	verifyTokenInactive(t, serverURL, tokenResponse.AccessToken)
}

func createTokenForRevocation(t *testing.T, serverURL string) TokenInfo {
	t.Helper()

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("scope", "mcp:read")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/token",
		strings.NewReader(data.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("test-client", "test-secret")

	client := &http.Client{
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar needed for this test
		Timeout:       0,   // No timeout set, use default
	}
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	var tokenResponse TokenInfo

	err = json.NewDecoder(resp.Body).Decode(&tokenResponse)
	require.NoError(t, err)

	return tokenResponse
}

func revokeToken(t *testing.T, serverURL, accessToken string) {
	t.Helper()

	revokeData := url.Values{}
	revokeData.Set("token", accessToken)

	revokeReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/revoke",
		strings.NewReader(revokeData.Encode()))
	require.NoError(t, err)
	revokeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	revokeReq.SetBasicAuth("test-client", "test-secret")

	client := &http.Client{}
	revokeResp, err := client.Do(revokeReq)
	require.NoError(t, err)

	defer func() {
		if err := revokeResp.Body.Close(); err != nil {
			t.Logf("Failed to close revoke response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusOK, revokeResp.StatusCode)
}

func verifyTokenInactive(t *testing.T, serverURL, accessToken string) {
	t.Helper()

	introspectData := url.Values{}
	introspectData.Set("token", accessToken)

	introspectReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		serverURL+"/introspect", strings.NewReader(introspectData.Encode()))
	require.NoError(t, err)
	introspectReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	introspectResp, err := client.Do(introspectReq)
	require.NoError(t, err)

	defer func() {
		if err := introspectResp.Body.Close(); err != nil {
			t.Logf("Failed to close introspect response body: %v", err)
		}
	}()

	var introspection map[string]interface{}

	err = json.NewDecoder(introspectResp.Body).Decode(&introspection)
	require.NoError(t, err)

	if active, ok := introspection["active"].(bool); ok {
		assert.False(t, active)
	}
}

func testInvalidClient(t *testing.T, serverURL string, _ *OAuth2ServerState) {
	t.Helper()

	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/token",
		strings.NewReader(data.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("invalid-client", "wrong-secret")

	client := &http.Client{
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar needed for this test
		Timeout:       0,   // No timeout set, use default
	}
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	var errorResponse map[string]interface{}

	err = json.NewDecoder(resp.Body).Decode(&errorResponse)
	require.NoError(t, err)

	assert.Equal(t, "invalid_client", errorResponse["error"])
}

func testServerFailureMode(t *testing.T, serverURL string, state *OAuth2ServerState) {
	t.Helper()
	// Enable failure mode
	state.SetFailureMode(true)
	defer state.SetFailureMode(false)

	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/token",
		strings.NewReader(data.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("test-client", "test-secret")

	client := &http.Client{
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar needed for this test
		Timeout:       0,   // No timeout set, use default
	}
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func testNetworkLatency(t *testing.T, serverURL string, state *OAuth2ServerState) {
	t.Helper()
	// Set high latency
	originalLatency := state.networkLatency

	state.SetNetworkLatency(500 * time.Millisecond)
	defer state.SetNetworkLatency(originalLatency)

	start := time.Now()

	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/token",
		strings.NewReader(data.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("test-client", "test-secret")

	client := &http.Client{
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar needed for this test
		Timeout:       0,   // No timeout set, use default
	}
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	duration := time.Since(start)
	assert.GreaterOrEqual(t, duration, 500*time.Millisecond)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// Helper functions.
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}

	return defaultValue
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(data), "=")
}
