package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

const (
	authHeaderParts = 2 // Format: "Bearer <token>"
)

// Provider defines the authentication provider interface.
type Provider interface {
	Authenticate(r *http.Request) (*Claims, error)
	ValidateToken(tokenString string) (*Claims, error)
}

// Claims represents the JWT claims.
type Claims struct {
	jwt.RegisteredClaims
	Scopes    []string        `json:"scopes"`
	RateLimit RateLimitConfig `json:"rate_limit"`
}

// RateLimitConfig represents rate limiting configuration.
type RateLimitConfig struct {
	RequestsPerMinute int `json:"requests_per_minute"`
	Burst             int `json:"burst"`
}

// JWTProvider implements JWT-based authentication.
type JWTProvider struct {
	config    config.JWTConfig
	logger    *zap.Logger
	secretKey []byte
	publicKey *rsa.PublicKey
}

// InitializeAuthenticationProvider creates an authentication provider based on configuration.
//

func InitializeAuthenticationProvider(cfg config.AuthConfig, logger *zap.Logger) (Provider, error) {
	switch cfg.Provider {
	case "jwt":
		return createJWTAuthProvider(cfg.JWT, logger)
	case "oauth2":
		return createOAuth2AuthProvider(cfg.OAuth2, logger)
	default:
		return nil, customerrors.NewValidationError("unsupported auth provider: " + cfg.Provider).
			WithComponent("auth")
	}
}

// createJWTAuthProvider creates a JWT-based authentication provider.
//

func createJWTAuthProvider(cfg config.JWTConfig, logger *zap.Logger) (*JWTProvider, error) {
	p := &JWTProvider{
		config: cfg,
		logger: logger,
	}

	// Load secret key from environment
	if cfg.SecretKeyEnv != "" {
		secretKey := os.Getenv(cfg.SecretKeyEnv)
		if secretKey == "" {
			return nil, customerrors.NewValidationError(
				fmt.Sprintf("JWT secret key environment variable %s not set", cfg.SecretKeyEnv)).
				WithComponent("auth_jwt")
		}

		p.secretKey = []byte(secretKey)
	}

	// Load public key if specified (for RS256)
	if cfg.PublicKeyPath != "" {
		keyData, err := os.ReadFile(cfg.PublicKeyPath)
		if err != nil {
			return nil, customerrors.Wrap(err, "failed to read public key").
				WithComponent("auth_jwt").
				WithContext("path", cfg.PublicKeyPath)
		}

		publicKey, err := jwt.ParseRSAPublicKeyFromPEM(keyData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key: %w", err)
		}

		p.publicKey = publicKey
	}

	return p, nil
}

// Authenticate authenticates an HTTP request.
func (p *JWTProvider) Authenticate(r *http.Request) (*Claims, error) {
	// Extract token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, errors.New("missing Authorization header")
	}

	// Check Bearer scheme
	parts := strings.SplitN(authHeader, " ", authHeaderParts)
	if len(parts) != authHeaderParts || parts[0] != "Bearer" {
		return nil, errors.New("invalid Authorization header format")
	}

	tokenString := parts[1]

	return p.ValidateToken(tokenString)
}

// ValidateToken validates a JWT token.
func (p *JWTProvider) ValidateToken(tokenString string) (*Claims, error) {
	token, err := p.parseToken(tokenString)
	if err != nil {
		return nil, err
	}

	claims, err := p.extractClaims(token)
	if err != nil {
		return nil, err
	}

	if err := p.validateClaims(claims); err != nil {
		return nil, err
	}

	p.setDefaultRateLimit(claims)
	p.logTokenValidation(claims)

	return claims, nil
}

// parseToken parses and validates the JWT token structure.
func (p *JWTProvider) parseToken(tokenString string) (*jwt.Token, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, p.getSigningKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	return token, nil
}

// getSigningKey returns the appropriate signing key based on the token's signing method.
func (p *JWTProvider) getSigningKey(token *jwt.Token) (interface{}, error) {
	switch token.Method.(type) {
	case *jwt.SigningMethodHMAC:
		if p.secretKey == nil {
			return nil, errors.New("HMAC key not configured")
		}

		return p.secretKey, nil
	case *jwt.SigningMethodRSA:
		if p.publicKey == nil {
			return nil, errors.New("RSA public key not configured")
		}

		return p.publicKey, nil
	default:
		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	}
}

// extractClaims extracts and validates the claims from the token.
func (p *JWTProvider) extractClaims(token *jwt.Token) (*Claims, error) {
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// validateClaims validates all standard JWT claims.
func (p *JWTProvider) validateClaims(claims *Claims) error {
	now := time.Now()

	if err := p.validateTimeBasedClaims(claims, now); err != nil {
		return err
	}

	if err := p.validateIssuer(claims); err != nil {
		return err
	}

	return p.validateAudience(claims)
}

// validateTimeBasedClaims validates expiration and not-before claims.
func (p *JWTProvider) validateTimeBasedClaims(claims *Claims, now time.Time) error {
	if claims.ExpiresAt != nil && now.After(claims.ExpiresAt.Time) {
		return errors.New("token expired")
	}

	if claims.NotBefore != nil && now.Before(claims.NotBefore.Time) {
		return errors.New("token not yet valid")
	}

	return nil
}

// validateIssuer validates the token issuer.
func (p *JWTProvider) validateIssuer(claims *Claims) error {
	if p.config.Issuer != "" && claims.Issuer != p.config.Issuer {
		return fmt.Errorf("invalid issuer: %s", claims.Issuer)
	}

	return nil
}

// validateAudience validates the token audience.
func (p *JWTProvider) validateAudience(claims *Claims) error {
	if p.config.Audience == "" {
		return nil
	}

	for _, aud := range claims.Audience {
		if aud == p.config.Audience {
			return nil
		}
	}

	return errors.New("invalid audience")
}

// setDefaultRateLimit sets default rate limit values if not specified.
func (p *JWTProvider) setDefaultRateLimit(claims *Claims) {
	if claims.RateLimit.RequestsPerMinute == 0 {
		claims.RateLimit.RequestsPerMinute = 1000
		claims.RateLimit.Burst = 50
	}
}

// logTokenValidation logs successful token validation.
func (p *JWTProvider) logTokenValidation(claims *Claims) {
	p.logger.Debug("Token validated successfully",
		zap.String("subject", claims.Subject),
		zap.Strings("scopes", claims.Scopes),
		zap.Time("expires_at", claims.ExpiresAt.Time),
	)
}

// HasScope checks if the claims contain a specific scope.
func (c *Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}

	return false
}

// CanAccessNamespace checks if the claims allow access to a namespace.
func (c *Claims) CanAccessNamespace(namespace string) bool {
	// Check for wildcard scope
	if c.HasScope("mcp:*") {
		return true
	}

	// Check for specific namespace scope
	scope := fmt.Sprintf("mcp:%s:*", namespace)
	if c.HasScope(scope) {
		return true
	}

	// Check for read-only scope
	readScope := fmt.Sprintf("mcp:%s:read", namespace)

	return c.HasScope(readScope)
}
