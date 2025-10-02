package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
)

const (
	oauth2MaxRetries        = 2
	defaultOAuth2RateLimit  = 1000
	defaultOAuth2Burst      = 50
	maxTokenRefreshAttempts = 3
	bearerTokenParts        = 2
	rateLimitScopeParts     = 3 // Format: "rate_limit:rpm:burst"

	// HTTP client configuration.
	httpClientTimeoutSeconds = 10

	// Cache configuration.
	cacheExpirationMinutes      = 5
	cacheCleanupIntervalMinutes = 1
)

// OAuth2Provider implements OAuth2-based authentication with token introspection.
type OAuth2Provider struct {
	config       config.OAuth2Config
	logger       *zap.Logger
	httpClient   *http.Client
	clientSecret string

	// Cache for introspection results
	cache      map[string]*cachedIntrospection
	cacheMutex sync.RWMutex
}

type cachedIntrospection struct {
	claims    *Claims
	expiresAt time.Time
}

type introspectionResponse struct {
	Active    bool     `json:"active"`
	Scope     string   `json:"scope"`
	ClientID  string   `json:"client_id"`
	Username  string   `json:"username"`
	TokenType string   `json:"token_type"`
	Exp       int64    `json:"exp"`
	Iat       int64    `json:"iat"`
	Sub       string   `json:"sub"`
	Aud       []string `json:"aud"`
	Iss       string   `json:"iss"`
	Jti       string   `json:"jti"`
}

// createOAuth2AuthProvider creates an OAuth2-based authentication provider.
//

func createOAuth2AuthProvider(cfg config.OAuth2Config, logger *zap.Logger) (*OAuth2Provider, error) {
	// Get client secret from environment
	clientSecret := ""
	if cfg.ClientSecretEnv != "" {
		clientSecret = os.Getenv(cfg.ClientSecretEnv)
		if clientSecret == "" {
			return nil, customerrors.NewValidationError(
				fmt.Sprintf("OAuth2 client secret environment variable %s not set", cfg.ClientSecretEnv)).
				WithComponent("auth_oauth2")
		}
	}

	p := &OAuth2Provider{
		config:       cfg,
		logger:       logger,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: httpClientTimeoutSeconds * time.Second,
		},
		cache: make(map[string]*cachedIntrospection),
	}

	// Start cache cleanup routine
	go p.cleanupCache()

	return p, nil
}

// Authenticate authenticates an HTTP request using OAuth2 bearer token.
func (p *OAuth2Provider) Authenticate(r *http.Request) (*Claims, error) {
	// Extract token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, NewMissingTokenError()
	}

	// Check Bearer scheme
	parts := strings.SplitN(authHeader, " ", bearerTokenParts)
	if len(parts) != bearerTokenParts || parts[0] != "Bearer" {
		return nil, NewInvalidTokenError("invalid Authorization header format")
	}

	tokenString := parts[1]

	return p.ValidateToken(tokenString)
}

// ValidateToken validates an OAuth2 token via introspection.
func (p *OAuth2Provider) ValidateToken(tokenString string) (*Claims, error) {
	// Check cache first
	if claims := p.getCachedClaims(tokenString); claims != nil {
		return claims, nil
	}

	// Try JWT validation if JWKS endpoint is configured
	if p.config.JWKSEndpoint != "" {
		if claims, err := p.validateJWT(tokenString); err == nil {
			p.cacheIntrospection(tokenString, claims)

			return claims, nil
		}
	}

	// Fall back to introspection
	introspection, err := p.introspectToken(tokenString)
	if err != nil {
		return nil, fmt.Errorf("token introspection failed: %w", err)
	}

	if !introspection.Active {
		return nil, errors.New("token is not active")
	}

	// Convert introspection response to claims
	claims := p.introspectionToClaims(introspection)

	// Cache the result
	p.cacheIntrospection(tokenString, claims)

	return claims, nil
}

func (p *OAuth2Provider) introspectToken(token string) (*introspectionResponse, error) {
	// Prepare introspection request
	data := url.Values{}
	data.Set("token", token)
	data.Set("token_type_hint", "access_token")

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		p.config.IntrospectEndpoint,
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(p.config.ClientID, p.clientSecret)

	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("introspection request failed: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Log but don't fail the main operation
			_ = err // Explicitly ignore error
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("introspection returned status %d", resp.StatusCode)
	}

	// Parse response
	var introspection introspectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&introspection); err != nil {
		return nil, fmt.Errorf("failed to parse introspection response: %w", err)
	}

	return &introspection, nil
}

func (p *OAuth2Provider) validateJWT(_ string) (*Claims, error) {
	// This would require implementing JWKS fetching and caching
	// For now, return an error to fall back to introspection
	return nil, errors.New("JWKS validation not implemented")
}

func (p *OAuth2Provider) introspectionToClaims(introspection *introspectionResponse) *Claims {
	// Parse scopes
	scopes := strings.Fields(introspection.Scope)

	// Create claims
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   introspection.Sub,
			Issuer:    introspection.Iss,
			ID:        introspection.Jti,
			ExpiresAt: jwt.NewNumericDate(time.Unix(introspection.Exp, 0)),
			IssuedAt:  jwt.NewNumericDate(time.Unix(introspection.Iat, 0)),
		},
		Scopes: scopes,
		RateLimit: RateLimitConfig{
			RequestsPerMinute: defaultOAuth2RateLimit, // Default rate limit
			Burst:             defaultOAuth2Burst,
		},
	}

	// Set audience if available
	if len(introspection.Aud) > 0 {
		claims.Audience = introspection.Aud
	}

	// Extract rate limit from scopes if present
	for _, scope := range scopes {
		if !strings.HasPrefix(scope, "rate_limit:") {
			continue
		}

		// Parse rate limit from scope (e.g., "rate_limit:defaultAPIPort:100")
		parts := strings.Split(scope, ":")
		if len(parts) != rateLimitScopeParts {
			continue
		}

		var rpm, burst int
		if _, err := fmt.Sscanf(parts[1], "%d", &rpm); err == nil {
			claims.RateLimit.RequestsPerMinute = rpm
		}

		if _, err := fmt.Sscanf(parts[2], "%d", &burst); err == nil {
			claims.RateLimit.Burst = burst
		}
	}

	return claims
}

func (p *OAuth2Provider) getCachedClaims(token string) *Claims {
	p.cacheMutex.RLock()
	defer p.cacheMutex.RUnlock()

	cached, exists := p.cache[token]
	if !exists {
		return nil
	}

	// Check if cache entry is still valid
	if time.Now().After(cached.expiresAt) {
		return nil
	}

	return cached.claims
}

func (p *OAuth2Provider) cacheIntrospection(token string, claims *Claims) {
	p.cacheMutex.Lock()
	defer p.cacheMutex.Unlock()

	// Cache for configured duration or until token expires, whichever is sooner
	expiresAt := time.Now().Add(cacheExpirationMinutes * time.Minute)
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(expiresAt) {
		expiresAt = claims.ExpiresAt.Time
	}

	p.cache[token] = &cachedIntrospection{
		claims:    claims,
		expiresAt: expiresAt,
	}
}

func (p *OAuth2Provider) cleanupCache() {
	ticker := time.NewTicker(cacheCleanupIntervalMinutes * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.cacheMutex.Lock()

		now := time.Now()
		for token, cached := range p.cache {
			if now.After(cached.expiresAt) {
				delete(p.cache, token)
			}
		}

		p.cacheMutex.Unlock()
	}
}
