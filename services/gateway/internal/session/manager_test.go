package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	"github.com/poiley/mcp-bridge/services/gateway/test/testutil"
	"go.uber.org/zap"
)

const (
	testIterations = 100
	testTimeout    = 50
	httpStatusOK   = 200
)

// MockManager implements Manager interface for testing.
type MockManager struct {
	sessions map[string]*Session
	err      error
}

func NewMockManager() *MockManager {
	return &MockManager{
		sessions: make(map[string]*Session),
	}
}

func (m *MockManager) CreateSession(claims *auth.Claims) (*Session, error) {
	if m.err != nil {
		return nil, m.err
	}

	session := &Session{
		ID:        generateSessionID(),
		User:      claims.Subject,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ExpiresAt: claims.ExpiresAt.Time,
		Scopes:    claims.Scopes,
		Metadata:  make(map[string]string),
		RateLimit: claims.RateLimit,
	}

	m.sessions[session.ID] = session

	return session, nil
}

func (m *MockManager) GetSession(id string) (*Session, error) {
	if m.err != nil {
		return nil, m.err
	}

	session, exists := m.sessions[id]
	if !exists {
		return nil, errors.New("session not found")
	}

	if time.Now().After(session.ExpiresAt) {
		delete(m.sessions, id)

		return nil, errors.New("session expired")
	}

	return session, nil
}

func (m *MockManager) UpdateSession(session *Session) error {
	if m.err != nil {
		return m.err
	}

	if _, exists := m.sessions[session.ID]; !exists {
		return errors.New("session not found")
	}

	session.UpdatedAt = time.Now()
	m.sessions[session.ID] = session

	return nil
}

func (m *MockManager) RemoveSession(id string) error {
	if m.err != nil {
		return m.err
	}

	delete(m.sessions, id)

	return nil
}

func (m *MockManager) Close() error {
	return nil
}

func (m *MockManager) SetError(err error) {
	m.err = err
}

func TestInitializeRedisSessionStore(t *testing.T) {
	// Start miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}

	defer mr.Close()

	logger := testutil.NewTestLogger(t)
	tests := createRedisSessionStoreTests(mr.Addr())

	runRedisSessionStoreTests(t, tests, logger)
}

func createRedisSessionStoreTests(addr string) []struct {
	name      string
	config    config.RedisConfig
	wantError bool
	errorMsg  string
} {
	return []struct {
		name      string
		config    config.RedisConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "Valid Redis URL",
			config: config.RedisConfig{
				URL: "redis://" + addr,
			},
			wantError: false,
		},
		{
			name: "Invalid Redis URL",
			config: config.RedisConfig{
				URL: "invalid://url",
			},
			wantError: true,
			errorMsg:  "failed to parse Redis URL",
		},
		{
			name:      "Missing Redis URL",
			config:    config.RedisConfig{},
			wantError: true,
			errorMsg:  "redis URL is required",
		},
		{
			name: "With DB only",
			config: config.RedisConfig{
				URL: "redis://" + addr,
				DB:  1,
			},
			wantError: false,
		},
	}
}

func runRedisSessionStoreTests(t *testing.T, tests []struct {
	name      string
	config    config.RedisConfig
	wantError bool
	errorMsg  string
}, logger *zap.Logger) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := InitializeRedisSessionStore(context.Background(), tt.config, logger)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got nil")

					return
				}

				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}

				return
			}

			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
				return
			}

			if manager == nil {
				t.Error("Expected manager to be created")
				return
			}

			_ = manager.Close()
		})
	}
}

func TestRedisManager_CreateSession(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}

	defer mr.Close()

	logger := testutil.NewTestLogger(t)

	manager, err := InitializeRedisSessionStore(context.Background(), config.RedisConfig{
		URL: "redis://" + mr.Addr(),
	}, logger)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = manager.Close() }()

	// Test claims
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Scopes: []string{"read", "write"},
		RateLimit: auth.RateLimitConfig{
			RequestsPerMinute: testIterations,
			Burst:             10,
		},
	}

	// Create session
	session, err := manager.CreateSession(claims)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Verify session fields
	if session.ID == "" {
		t.Error("Session ID should not be empty")
	}

	if session.User != "test-user" {
		t.Errorf("Expected user 'test-user', got '%s'", session.User)
	}

	if len(session.Scopes) != 2 {
		t.Errorf("Expected 2 scopes, got %d", len(session.Scopes))
	}

	if session.RateLimit.RequestsPerMinute != testIterations {
		t.Errorf("Expected rate limit testIterations, got %d", session.RateLimit.RequestsPerMinute)
	}

	// Verify session is stored in Redis
	key := "session:" + session.ID
	if !mr.Exists(key) {
		t.Error("Session not found in Redis")
	}

	// Verify TTL is set
	ttl := mr.TTL(key)
	if ttl <= 0 {
		t.Error("Session TTL not set")
	}
}

func TestRedisManager_GetSession(t *testing.T) {
	// Setup
	mr, manager := setupRedisManagerTest(t)

	defer func() { _ = manager.Close() }()

	// Test valid session retrieval
	testValidSessionRetrieval(t, manager)

	// Test non-existent session
	testNonExistentSession(t, manager)

	// Test expired session handling
	testExpiredSessionHandling(t, manager, mr)
}

func setupRedisManagerTest(t *testing.T) (*miniredis.Miniredis, Manager) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { mr.Close() })

	logger := testutil.NewTestLogger(t)

	manager, err := InitializeRedisSessionStore(context.Background(), config.RedisConfig{
		URL: "redis://" + mr.Addr(),
	}, logger)
	if err != nil {
		t.Fatal(err)
	}

	return mr, manager
}

func testValidSessionRetrieval(t *testing.T, manager Manager) {
	t.Helper()

	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Scopes: []string{"read"},
	}

	createdSession, err := manager.CreateSession(claims)
	if err != nil {
		t.Fatal(err)
	}

	retrievedSession, err := manager.GetSession(createdSession.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if retrievedSession.ID != createdSession.ID {
		t.Error("Session ID mismatch")
	}

	if retrievedSession.User != createdSession.User {
		t.Error("Session user mismatch")
	}
}

func testNonExistentSession(t *testing.T, manager Manager) {
	t.Helper()

	_, err := manager.GetSession("non-existent")
	if err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Errorf("Expected 'session not found' error, got: %v", err)
	}
}

func testExpiredSessionHandling(t *testing.T, manager Manager, mr *miniredis.Miniredis) {
	t.Helper()

	expiredClaims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "expired-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}

	expiredSession, err := manager.CreateSession(expiredClaims)
	if err != nil {
		t.Fatal(err)
	}

	// Should return expired error
	_, err = manager.GetSession(expiredSession.ID)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "expired") {
		t.Errorf("Expected 'expired' error, got: %v", err)
	}

	// Verify expired session was removed
	if mr.Exists("session:" + expiredSession.ID) {
		t.Error("Expired session should have been removed")
	}
}

func TestRedisManager_UpdateSession(t *testing.T) {
	manager := setupRedisManagerForUpdate(t)

	defer func() { _ = manager.Close() }()

	session := createTestSessionForUpdate(t, manager)
	testSessionUpdate(t, manager, session)
	testExpiredSessionUpdate(t, manager)
}

func setupRedisManagerForUpdate(t *testing.T) Manager {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}

	logger := testutil.NewTestLogger(t)

	manager, err := InitializeRedisSessionStore(context.Background(), config.RedisConfig{
		URL: "redis://" + mr.Addr(),
	}, logger)
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

func createTestSessionForUpdate(t *testing.T, manager Manager) *Session {
	t.Helper()

	// Create a session
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Scopes: []string{"read"},
	}

	session, err := manager.CreateSession(claims)
	if err != nil {
		t.Fatal(err)
	}

	return session
}

func testSessionUpdate(t *testing.T, manager Manager, session *Session) {
	t.Helper()

	originalUpdatedAt := session.UpdatedAt

	// Update session metadata
	session.Metadata["key"] = "value"

	time.Sleep(10 * time.Millisecond) // Ensure time difference

	err := manager.UpdateSession(session)
	if err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	// Verify UpdatedAt was changed
	if !session.UpdatedAt.After(originalUpdatedAt) {
		t.Error("UpdatedAt should be updated")
	}

	// Retrieve and verify
	retrieved, err := manager.GetSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}

	if retrieved.Metadata["key"] != "value" {
		t.Error("Metadata not persisted")
	}
}

func testExpiredSessionUpdate(t *testing.T, manager Manager) {
	t.Helper()

	// Test updating expired session
	expiredSession := &Session{
		ID:        "expired-id",
		ExpiresAt: time.Now().Add(-time.Hour),
	}

	err := manager.UpdateSession(expiredSession)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "expired") {
		t.Errorf("Expected 'expired' error, got: %v", err)
	}
}

func TestRedisManager_RemoveSession(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}

	defer mr.Close()

	logger := testutil.NewTestLogger(t)

	manager, err := InitializeRedisSessionStore(context.Background(), config.RedisConfig{
		URL: "redis://" + mr.Addr(),
	}, logger)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = manager.Close() }()

	// Create a session
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	session, err := manager.CreateSession(claims)
	if err != nil {
		t.Fatal(err)
	}

	// Remove session
	err = manager.RemoveSession(session.ID)
	if err != nil {
		t.Fatalf("Failed to remove session: %v", err)
	}

	// Verify session is gone
	_, err = manager.GetSession(session.ID)
	if err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Errorf("Expected 'session not found' error, got: %v", err)
	}

	// Remove non-existent session (should not error)
	err = manager.RemoveSession("non-existent")
	if err != nil {
		t.Errorf("Removing non-existent session should not error, got: %v", err)
	}
}

func TestSession_HasScope(t *testing.T) {
	session := &Session{
		Scopes: []string{"read", "write", "admin"},
	}

	tests := []struct {
		scope    string
		expected bool
	}{
		{"read", true},
		{"write", true},
		{"admin", true},
		{"delete", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			result := session.HasScope(tt.scope)
			if result != tt.expected {
				t.Errorf("HasScope(%s) = %v, expected %v", tt.scope, result, tt.expected)
			}
		})
	}
}

func TestGenerateSessionID(t *testing.T) {
	// Generate multiple IDs
	ids := make(map[string]bool)

	for i := 0; i < testIterations; i++ {
		id := generateSessionID()

		// Check length (16 bytes = 32 hex chars)
		if len(id) != 32 {
			t.Errorf("Expected session ID length 32, got %d", len(id))
		}

		// Check uniqueness
		if ids[id] {
			t.Errorf("Duplicate session ID generated: %s", id)
		}

		ids[id] = true

		// Check it's valid hex
		if !isValidHex(id) {
			t.Errorf("Session ID is not valid hex: %s", id)
		}
	}
}

func isValidHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}

func TestRedisManager_SessionTTL(t *testing.T) {
	rm, mr := setupRedisManagerForTTL(t)

	defer func() { _ = rm.Close() }()

	defer mr.Close()

	tests := createSessionTTLTests(rm)
	runSessionTTLTests(t, tests, rm, mr)
}

func setupRedisManagerForTTL(t *testing.T) (*RedisManager, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}

	logger := testutil.NewTestLogger(t)

	manager, err := InitializeRedisSessionStore(context.Background(), config.RedisConfig{
		URL: "redis://" + mr.Addr(),
	}, logger)
	if err != nil {
		t.Fatal(err)
	}

	rm, ok := manager.(*RedisManager)
	if !ok {
		t.Fatal("Expected RedisManager")
	}

	return rm, mr
}

func createSessionTTLTests(rm *RedisManager) []struct {
	name        string
	expiresIn   time.Duration
	expectedTTL time.Duration
} {
	return []struct {
		name        string
		expiresIn   time.Duration
		expectedTTL time.Duration
	}{
		{
			name:        "Short expiration",
			expiresIn:   1 * time.Hour,
			expectedTTL: 1 * time.Hour,
		},
		{
			name:        "Long expiration capped",
			expiresIn:   48 * time.Hour,
			expectedTTL: rm.ttl, // Should be capped at manager TTL (24h)
		},
	}
}

func runSessionTTLTests(t *testing.T, tests []struct {
	name        string
	expiresIn   time.Duration
	expectedTTL time.Duration
}, manager Manager, mr *miniredis.Miniredis) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &auth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "test-user",
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(tt.expiresIn)),
				},
			}

			session, err := manager.CreateSession(claims)
			if err != nil {
				t.Fatal(err)
			}

			// Check Redis TTL
			key := "session:" + session.ID
			ttl := mr.TTL(key)

			// Allow some margin for test execution time
			margin := 5 * time.Second
			if ttl > tt.expectedTTL+margin || ttl < tt.expectedTTL-margin {
				t.Errorf("Expected TTL around %v, got %v", tt.expectedTTL, ttl)
			}
		})
	}
}

func TestRedisManager_ConcurrentAccess(t *testing.T) {
	// Setup
	manager, session := setupConcurrentTest(t)

	defer func() { _ = manager.Close() }()

	// Run concurrent operations
	errors := runConcurrentOperations(manager, session)

	// Check results
	checkConcurrentResults(t, errors, manager, session.ID)
}

func setupConcurrentTest(t *testing.T) (*RedisManager, *Session) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { mr.Close() })

	logger := testutil.NewTestLogger(t)

	manager, err := InitializeRedisSessionStore(context.Background(), config.RedisConfig{
		URL: "redis://" + mr.Addr(),
	}, logger)
	if err != nil {
		t.Fatal(err)
	}

	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Scopes: []string{"read"},
	}

	session, err := manager.CreateSession(claims)
	if err != nil {
		t.Fatal(err)
	}

	redisManager, ok := manager.(*RedisManager)
	if !ok {
		t.Fatal("Expected RedisManager type")
	}
	return redisManager, session
}

func runConcurrentOperations(manager *RedisManager, session *Session) chan error {
	done := make(chan bool)
	errors := make(chan error, testIterations)

	// Start readers
	startConcurrentReaders(manager, session.ID, done, errors)

	// Start updaters
	startConcurrentUpdaters(manager, session.ID, done, errors)

	// Wait for completion
	for i := 0; i < 15; i++ {
		<-done
	}

	close(errors)

	return errors
}

func startConcurrentReaders(manager *RedisManager, sessionID string, done chan bool, errors chan error) {
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < testIterations; j++ {
				_, err := manager.GetSession(sessionID)
				if err != nil {
					errors <- err
				}
			}

			done <- true
		}()
	}
}

func startConcurrentUpdaters(manager *RedisManager, sessionID string, done chan bool, errors chan error) {
	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < testTimeout; j++ {
				s, err := manager.GetSession(sessionID)
				if err != nil {
					errors <- err

					continue
				}

				s.Metadata[string(rune(id))] = string(rune(j))
				if err := manager.UpdateSession(s); err != nil {
					errors <- err
				}
			}

			done <- true
		}(i)
	}
}

func checkConcurrentResults(t *testing.T, errors chan error, manager *RedisManager, sessionID string) {
	t.Helper()

	// Check for errors

	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}

	// Verify session still exists and is valid
	finalSession, err := manager.GetSession(sessionID)
	if err != nil {
		t.Errorf("Failed to get session after concurrent access: %v", err)
	}

	if finalSession == nil {
		t.Error("Session should still exist")
	}
}

func TestRedisConnectionFailure(t *testing.T) {
	logger := testutil.NewTestLogger(t)

	// Try to connect to non-existent Redis
	_, err := InitializeRedisSessionStore(context.Background(), config.RedisConfig{
		URL: "redis://localhost:99999",
	}, logger)
	if err == nil {
		t.Error("Expected connection error")
	}

	if !strings.Contains(err.Error(), "failed to connect to Redis") {
		t.Errorf("Expected connection error, got: %v", err)
	}
}

func TestSession_JSONSerialization(t *testing.T) {
	session := &Session{
		ID:        "test-id",
		User:      "test-user",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
		Scopes:    []string{"read", "write"},
		Metadata: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
		RateLimit: auth.RateLimitConfig{
			RequestsPerMinute: testIterations,
			Burst:             10,
		},
	}

	// Marshal
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("Failed to marshal session: %v", err)
	}

	// Unmarshal
	var decoded Session

	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal session: %v", err)
	}

	// Verify fields
	if decoded.ID != session.ID {
		t.Error("ID mismatch after serialization")
	}

	if decoded.User != session.User {
		t.Error("User mismatch after serialization")
	}

	if len(decoded.Scopes) != len(session.Scopes) {
		t.Error("Scopes mismatch after serialization")
	}

	if len(decoded.Metadata) != len(session.Metadata) {
		t.Error("Metadata mismatch after serialization")
	}

	if decoded.RateLimit.RequestsPerMinute != session.RateLimit.RequestsPerMinute {
		t.Error("RateLimit mismatch after serialization")
	}
}

func BenchmarkSessionCreation(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}

	defer mr.Close()

	logger := testutil.NewBenchLogger(b)

	manager, err := InitializeRedisSessionStore(context.Background(), config.RedisConfig{
		URL: "redis://" + mr.Addr(),
	}, logger)
	if err != nil {
		b.Fatal(err)
	}

	defer func() { _ = manager.Close() }()

	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "bench-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Scopes: []string{"read", "write"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := manager.CreateSession(claims)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSessionRetrieval(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}

	defer mr.Close()

	logger := testutil.NewBenchLogger(b)

	manager, err := InitializeRedisSessionStore(context.Background(), config.RedisConfig{
		URL: "redis://" + mr.Addr(),
	}, logger)
	if err != nil {
		b.Fatal(err)
	}

	defer func() { _ = manager.Close() }()

	// Create a session
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "bench-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	session, err := manager.CreateSession(claims)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := manager.GetSession(session.ID)
		if err != nil {
			b.Fatal(err)
		}
	}
}
