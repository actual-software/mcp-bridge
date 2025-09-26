package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockProvider struct {
	mock.Mock
}

func (m *mockProvider) Authenticate(r *http.Request) (*Claims, error) {
	args := m.Called(r)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	claims, ok := args.Get(0).(*Claims)
	if !ok {
		return nil, args.Error(1)
	}

	return claims, args.Error(1)
}

func (m *mockProvider) ValidateToken(tokenString string) (*Claims, error) {
	args := m.Called(tokenString)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	claims, ok := args.Get(0).(*Claims)
	if !ok {
		return nil, args.Error(1)
	}

	return claims, args.Error(1)
}

func TestMessageAuthenticator_ValidateMessageToken(t *testing.T) {
	t.Parallel()

	tests := createMessageAuthTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runMessageAuthTest(t, tt)
		})
	}
}

type messageAuthTestCase struct {
	name        string
	token       string
	sessionID   string
	claims      *Claims
	validateErr error
	wantErr     bool
	errMsg      string
}

func createMessageAuthTestCases() []messageAuthTestCase {
	validClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "session-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	expiredClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "session-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	mismatchClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "session-456",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	emptyClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	return []messageAuthTestCase{
		{"Valid token", "valid-token", "session-123", validClaims, nil, false, ""},
		{"Missing token", "", "session-123", nil, nil, true, "authentication token is required"},
		{"Invalid token", "invalid-token", "session-123", nil, assert.AnError, true, "invalid token"},
		{"Expired token", "expired-token", "session-123", expiredClaims, nil, true, "token has expired"},
		{"Session mismatch", "valid-token", "session-123", mismatchClaims, nil, true, "session mismatch"},
		{
			"No session ID in token", "valid-token", "session-123", emptyClaims, nil, false, "",
		}, // Should pass if session ID is empty in claims
	}
}

func runMessageAuthTest(t *testing.T, tt messageAuthTestCase) {
	t.Helper()

	logger := zap.NewNop()
	ctx := context.Background()

	provider := new(mockProvider)
	if tt.token != "" {
		provider.On("ValidateToken", tt.token).Return(tt.claims, tt.validateErr)
	}

	ma := CreateMessageLevelAuthenticator(provider, logger, 0) // No cache for testing

	err := ma.ValidateMessageToken(ctx, tt.token, tt.sessionID)
	if tt.wantErr {
		require.Error(t, err)

		if tt.errMsg != "" {
			assert.Contains(t, err.Error(), tt.errMsg)
		}
	} else {
		require.NoError(t, err)
	}

	provider.AssertExpectations(t)
}

func TestMessageAuthenticator_Cache(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	ctx := context.Background()
	provider := new(mockProvider)

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "session-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	// Provider should only be called once due to caching
	provider.On("ValidateToken", "cached-token").Return(claims, nil).Once()

	ma := CreateMessageLevelAuthenticator(provider, logger, 5) // 5 second cache

	// First call should hit the provider
	err := ma.ValidateMessageToken(ctx, "cached-token", "session-123")
	require.NoError(t, err)

	// Second call should use cache
	err = ma.ValidateMessageToken(ctx, "cached-token", "session-123")
	require.NoError(t, err)

	provider.AssertExpectations(t)
}

func TestGenerateMessageToken(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	sessionID := "session-123"
	duration := time.Hour

	token, err := GenerateMessageToken(sessionID, secret, duration)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	// Verify the token
	parsedToken, err := jwt.ParseWithClaims(token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	require.NoError(t, err)
	assert.True(t, parsedToken.Valid)

	claims, ok := parsedToken.Claims.(*Claims)
	require.True(t, ok)
	assert.Equal(t, sessionID, claims.Subject)
	assert.True(t, claims.ExpiresAt.After(time.Now()))
	assert.True(t, claims.ExpiresAt.Before(time.Now().Add(duration+time.Minute)))
}
