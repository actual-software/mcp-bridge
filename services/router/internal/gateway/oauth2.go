package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
)

// OAuth2Token represents an OAuth2 access token.
type OAuth2Token struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"-"`
}

// IsExpired returns true if the token is expired.
func (t *OAuth2Token) IsExpired() bool {
	// Add a 30-second buffer to avoid using tokens that are about to expire.
	return time.Now().After(t.ExpiresAt.Add(-defaultTimeoutSeconds * time.Second))
}

// OAuth2Client handles OAuth2 authentication.
type OAuth2Client struct {
	config     config.GatewayConfig
	logger     *zap.Logger
	httpClient *http.Client

	// Token cache.
	tokenMu sync.RWMutex
	token   *OAuth2Token
}

// NewOAuth2Client creates a new OAuth2 client.
func NewOAuth2Client(cfg config.GatewayConfig, logger *zap.Logger, httpClient *http.Client) *OAuth2Client {
	return &OAuth2Client{
		config:     cfg,
		logger:     logger,
		httpClient: httpClient,
	}
}

// GetToken returns a valid OAuth2 token, refreshing if necessary.
func (c *OAuth2Client) GetToken(ctx context.Context) (string, error) {
	// Try to return existing valid token without lock
	if token := c.getValidTokenIfExists(); token != "" {
		return token, nil
	}

	// Need to acquire or refresh token with write lock
	return c.acquireOrRefreshToken(ctx)
}

// getValidTokenIfExists checks for valid token with read lock.
func (c *OAuth2Client) getValidTokenIfExists() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()

	if c.token != nil && !c.token.IsExpired() {
		return c.token.AccessToken
	}

	return ""
}

// acquireOrRefreshToken handles token acquisition/refresh with write lock.
func (c *OAuth2Client) acquireOrRefreshToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double-check after acquiring write lock.
	if c.token != nil && !c.token.IsExpired() {
		return c.token.AccessToken, nil
	}

	if err := c.refreshOrAcquireNewToken(ctx); err != nil {
		return "", err
	}

	return c.token.AccessToken, nil
}

// refreshOrAcquireNewToken attempts refresh first, falls back to new token acquisition.
func (c *OAuth2Client) refreshOrAcquireNewToken(ctx context.Context) error {
	// Try refresh if possible
	if c.token != nil && c.token.RefreshToken != "" {
		if err := c.attemptTokenRefresh(ctx); err == nil {
			return nil
		}
	}

	// Fall back to acquiring new token
	return c.acquireNewToken(ctx)
}

// attemptTokenRefresh tries to refresh the current token.
func (c *OAuth2Client) attemptTokenRefresh(ctx context.Context) error {
	token, err := c.refreshToken(ctx, c.token.RefreshToken)
	if err != nil {
		c.logger.Warn("Failed to refresh token, acquiring new one", zap.Error(err))

		return err
	}

	c.token = token

	return nil
}

// acquireNewToken gets a completely new token.
func (c *OAuth2Client) acquireNewToken(ctx context.Context) error {
	token, err := c.acquireToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire OAuth2 token: %w", err)
	}

	c.token = token

	return nil
}

// acquireToken acquires a new OAuth2 token.
func (c *OAuth2Client) acquireToken(ctx context.Context) (*OAuth2Token, error) {
	acquirer := CreateOAuth2TokenAcquirer(c)

	return acquirer.AcquireToken(ctx)
}

// refreshToken refreshes an OAuth2 token.
func (c *OAuth2Client) refreshToken(ctx context.Context, refreshToken string) (*OAuth2Token, error) {
	refresher := CreateOAuth2TokenRefresher(c)

	return refresher.RefreshToken(ctx, refreshToken)
}

// prepareClientCredentialsRequest prepares a client credentials grant request.
func (c *OAuth2Client) prepareClientCredentialsRequest() string {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	if c.config.Auth.ClientID != "" {
		data.Set("client_id", c.config.Auth.ClientID)
	}

	if c.config.Auth.ClientSecret != "" {
		data.Set("client_secret", c.config.Auth.ClientSecret)
	}

	if c.config.Auth.Scope != "" {
		data.Set("scope", c.config.Auth.Scope)
	}

	return data.Encode()
}

// preparePasswordRequest prepares a password grant request.
func (c *OAuth2Client) preparePasswordRequest() string {
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("username", c.config.Auth.Username)
	data.Set("password", c.config.Auth.Password)

	if c.config.Auth.ClientID != "" {
		data.Set("client_id", c.config.Auth.ClientID)
	}

	if c.config.Auth.ClientSecret != "" {
		data.Set("client_secret", c.config.Auth.ClientSecret)
	}

	if c.config.Auth.Scope != "" {
		data.Set("scope", c.config.Auth.Scope)
	}

	return data.Encode()
}
