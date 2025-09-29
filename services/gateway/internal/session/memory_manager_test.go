package session

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
)

// Test constants.
const (
	testValue = "value"
)

func TestMemoryManager_CreateAndGet(t *testing.T) {
	logger := zap.NewNop()

	mgr := CreateInMemorySessionStore(logger, 5*time.Minute)

	defer func() { _ = mgr.Close() }()

	// Create test claims
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Scopes: []string{"read", "write"},
		RateLimit: auth.RateLimitConfig{
			RequestsPerMinute: 60,
			Burst:             10,
		},
	}

	// Create session
	session, err := mgr.CreateSession(claims)
	require.NoError(t, err)
	assert.NotEmpty(t, session.ID)
	assert.Equal(t, "test-user", session.User)
	assert.Equal(t, claims.Scopes, session.Scopes)

	// Get session
	retrieved, err := mgr.GetSession(session.ID)
	require.NoError(t, err)
	assert.Equal(t, session.ID, retrieved.ID)
	assert.Equal(t, session.User, retrieved.User)
}

func TestMemoryManager_ExpiredSession(t *testing.T) {
	logger := zap.NewNop()

	mgr := CreateInMemorySessionStore(logger, 5*time.Minute)

	defer func() { _ = mgr.Close() }()

	// Create test claims with expired time
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // Already expired
		},
	}

	// Create session
	session, err := mgr.CreateSession(claims)
	require.NoError(t, err)

	// Try to get expired session
	_, err = mgr.GetSession(session.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestMemoryManager_CleanupExpiredSessions(t *testing.T) {
	logger := zap.NewNop()
	// Use very short cleanup interval for testing
	mgr, ok := CreateInMemorySessionStore(logger, testTimeout*time.Millisecond).(*MemoryManager)
	if !ok {
		t.Fatal("Expected MemoryManager")
	}

	defer func() { _ = mgr.Close() }()

	// Create session that is already expired to test cleanup
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), // Already expired
		},
	}

	session, err := mgr.CreateSession(claims)
	require.NoError(t, err)

	// Even though expired, it should exist in the storage initially since cleanup hasn't run
	mgr.mu.RLock()
	_, exists := mgr.sessions[session.ID]
	mgr.mu.RUnlock()
	assert.True(t, exists, "Session should exist in storage before cleanup")

	// Wait for cleanup cycles to run
	time.Sleep(httpStatusOK * time.Millisecond)

	// Now check that cleanup has removed the expired session
	mgr.mu.RLock()
	_, exists = mgr.sessions[session.ID]
	mgr.mu.RUnlock()
	assert.False(t, exists, "Session should be removed from storage after cleanup")

	// Verify GetSession also returns error for expired session
	_, err = mgr.GetSession(session.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Check stats - no sessions should remain
	stats := mgr.GetStats()
	assert.Equal(t, 0, stats["total_sessions"])
}

func TestMemoryManager_UpdateSession(t *testing.T) {
	logger := zap.NewNop()

	mgr := CreateInMemorySessionStore(logger, 5*time.Minute)

	defer func() { _ = mgr.Close() }()

	// Create test claims
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	// Create session
	session, err := mgr.CreateSession(claims)
	require.NoError(t, err)

	originalUpdatedAt := session.UpdatedAt

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Update session
	session.Metadata["key"] = testValue
	err = mgr.UpdateSession(session)
	require.NoError(t, err)

	// Verify update
	retrieved, err := mgr.GetSession(session.ID)
	require.NoError(t, err)
	assert.Equal(t, testValue, retrieved.Metadata["key"])
	assert.True(t, retrieved.UpdatedAt.After(originalUpdatedAt))
}

func TestMemoryManager_RemoveSession(t *testing.T) {
	logger := zap.NewNop()

	mgr := CreateInMemorySessionStore(logger, 5*time.Minute)

	defer func() { _ = mgr.Close() }()

	// Create test claims
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	// Create session
	session, err := mgr.CreateSession(claims)
	require.NoError(t, err)

	// Remove session
	err = mgr.RemoveSession(session.ID)
	require.NoError(t, err)

	// Session should be gone
	_, err = mgr.GetSession(session.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMemoryManager_Close(t *testing.T) {
	logger := zap.NewNop()

	mgr, ok := CreateInMemorySessionStore(logger, testIterations*time.Millisecond).(*MemoryManager)
	if !ok {
		t.Fatal("Expected MemoryManager")
	}

	// Create a session
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	_, err := mgr.CreateSession(claims)
	require.NoError(t, err)

	// Close the manager
	err = mgr.Close()
	assert.NoError(t, err)

	// Verify cleanup goroutine stopped
	select {
	case <-mgr.cleanupStopped:
		// Good - cleanup goroutine stopped
	case <-time.After(time.Second):
		t.Fatal("Cleanup goroutine did not stop")
	}

	// Sessions should be cleared
	mgr.mu.RLock()
	assert.Nil(t, mgr.sessions)
	mgr.mu.RUnlock()
}

func TestMemoryManager_GetStats(t *testing.T) {
	logger := zap.NewNop()

	mgr, ok := CreateInMemorySessionStore(logger, 5*time.Minute).(*MemoryManager)
	if !ok {
		t.Fatal("Expected MemoryManager")
	}

	defer func() { _ = mgr.Close() }()

	// Create mix of active and expired sessions
	for i := 0; i < 3; i++ {
		claims := &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-user",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
		}
		_, err := mgr.CreateSession(claims)
		require.NoError(t, err)
	}

	// Create expired sessions
	for i := 0; i < 2; i++ {
		claims := &auth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-user",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			},
		}
		_, err := mgr.CreateSession(claims)
		require.NoError(t, err)
	}

	stats := mgr.GetStats()
	assert.Equal(t, 5, stats["total_sessions"])
	assert.Equal(t, 3, stats["active_sessions"])
	assert.Equal(t, 2, stats["expired_sessions"])
}
