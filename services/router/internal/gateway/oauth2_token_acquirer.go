package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	// GrantTypeClientCredentials represents the OAuth2 client credentials grant type.
	GrantTypeClientCredentials = "client_credentials"
)

// OAuth2TokenAcquirer handles OAuth2 token acquisition.
type OAuth2TokenAcquirer struct {
	client *OAuth2Client
}

// CreateOAuth2TokenAcquirer creates a new token acquirer.
func CreateOAuth2TokenAcquirer(client *OAuth2Client) *OAuth2TokenAcquirer {
	return &OAuth2TokenAcquirer{
		client: client,
	}
}

// AcquireToken acquires a new OAuth2 token.
func (a *OAuth2TokenAcquirer) AcquireToken(ctx context.Context) (*OAuth2Token, error) {
	if err := a.validateConfig(); err != nil {
		return nil, err
	}

	requestBody, err := a.prepareRequestBody()
	if err != nil {
		return nil, err
	}

	request, err := a.createTokenRequest(ctx, requestBody)
	if err != nil {
		return nil, err
	}

	return a.executeTokenRequest(request)
}

func (a *OAuth2TokenAcquirer) validateConfig() error {
	if a.client.config.Auth.TokenEndpoint == "" {
		return errors.New("OAuth2 token endpoint not configured")
	}

	return nil
}

func (a *OAuth2TokenAcquirer) prepareRequestBody() (string, error) {
	switch a.client.config.Auth.GrantType {
	case GrantTypeClientCredentials:
		return a.client.prepareClientCredentialsRequest(), nil
	case "password":
		return a.client.preparePasswordRequest(), nil
	default:
		return "", fmt.Errorf("unsupported OAuth2 grant type: %s", a.client.config.Auth.GrantType)
	}
}

func (a *OAuth2TokenAcquirer) createTokenRequest(ctx context.Context, body string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.client.config.Auth.TokenEndpoint,
		strings.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	a.configureRequestHeaders(req)

	return req, nil
}

func (a *OAuth2TokenAcquirer) configureRequestHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if a.shouldUseBasicAuth() {
		req.SetBasicAuth(a.client.config.Auth.ClientID, a.client.config.Auth.ClientSecret)
	}
}

func (a *OAuth2TokenAcquirer) shouldUseBasicAuth() bool {
	return a.client.config.Auth.ClientID != "" && a.client.config.Auth.ClientSecret != ""
}

func (a *OAuth2TokenAcquirer) executeTokenRequest(req *http.Request) (*OAuth2Token, error) {
	resp, err := a.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			a.client.logger.Debug("Failed to close response body", zap.Error(err))
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return a.parseTokenResponse(respBody)
}

func (a *OAuth2TokenAcquirer) parseTokenResponse(respBody []byte) (*OAuth2Token, error) {
	var token OAuth2Token
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	a.setTokenExpiration(&token)
	a.logTokenAcquisition(&token)

	return &token, nil
}

func (a *OAuth2TokenAcquirer) setTokenExpiration(token *OAuth2Token) {
	if token.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	} else {
		// Default to 1 hour if not specified.
		token.ExpiresAt = time.Now().Add(time.Hour)
	}
}

func (a *OAuth2TokenAcquirer) logTokenAcquisition(token *OAuth2Token) {
	a.client.logger.Info("Successfully acquired OAuth2 token",
		zap.String("token_type", token.TokenType),
		zap.String("scope", token.Scope),
		zap.Time("expires_at", token.ExpiresAt))
}

// OAuth2TokenRefresher handles OAuth2 token refresh.
type OAuth2TokenRefresher struct {
	client *OAuth2Client
}

// CreateOAuth2TokenRefresher creates a new token refresher.
func CreateOAuth2TokenRefresher(client *OAuth2Client) *OAuth2TokenRefresher {
	return &OAuth2TokenRefresher{
		client: client,
	}
}

// RefreshToken refreshes an OAuth2 token.
func (r *OAuth2TokenRefresher) RefreshToken(ctx context.Context, refreshToken string) (*OAuth2Token, error) {
	if err := r.validateConfig(); err != nil {
		return nil, err
	}

	requestBody := r.prepareRefreshRequest(refreshToken)

	request, err := r.createRefreshRequest(ctx, requestBody)
	if err != nil {
		return nil, err
	}

	return r.executeRefreshRequest(request)
}

func (r *OAuth2TokenRefresher) validateConfig() error {
	if r.client.config.Auth.TokenEndpoint == "" {
		return errors.New("OAuth2 token endpoint not configured")
	}

	return nil
}

func (r *OAuth2TokenRefresher) prepareRefreshRequest(refreshToken string) string {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)

	if r.client.config.Auth.ClientID != "" {
		data.Set("client_id", r.client.config.Auth.ClientID)
	}

	if r.client.config.Auth.ClientSecret != "" {
		data.Set("client_secret", r.client.config.Auth.ClientSecret)
	}

	return data.Encode()
}

func (r *OAuth2TokenRefresher) createRefreshRequest(ctx context.Context, body string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		r.client.config.Auth.TokenEndpoint,
		strings.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return req, nil
}

func (r *OAuth2TokenRefresher) executeRefreshRequest(req *http.Request) (*OAuth2Token, error) {
	acquirer := CreateOAuth2TokenAcquirer(r.client)

	return acquirer.executeTokenRequest(req)
}
