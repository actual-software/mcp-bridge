package session

import (
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/go-redis/redis/v8"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
)

// MemoryManager provides an in-memory session storage implementation.
// This is suitable for development/testing or single-instance deployments.
type MemoryManager struct {
	sessions       map[string]*Session
	mu             sync.RWMutex
	logger         *zap.Logger
	cleanupTicker  *time.Ticker
	cleanupStop    chan struct{}
	cleanupStopped chan struct{}
}

// CreateInMemorySessionStore creates an in-memory session storage for single-instance deployments.
//

func CreateInMemorySessionStore(logger *zap.Logger, cleanupInterval time.Duration) Manager {
	if cleanupInterval <= 0 {
		cleanupInterval = defaultMaxConnections * time.Minute // Default cleanup interval
	}

	m := &MemoryManager{
		sessions:       make(map[string]*Session),
		logger:         logger,
		cleanupTicker:  time.NewTicker(cleanupInterval),
		cleanupStop:    make(chan struct{}),
		cleanupStopped: make(chan struct{}),
	}

	// Start cleanup goroutine
	go m.cleanupExpiredSessions()

	return m
}

// cleanupExpiredSessions runs periodically to remove expired sessions.
func (m *MemoryManager) cleanupExpiredSessions() {
	defer close(m.cleanupStopped)
	defer m.cleanupTicker.Stop()

	m.logger.Info("Session cleanup goroutine started")

	for {
		select {
		case <-m.cleanupTicker.C:
			m.performCleanup()
		case <-m.cleanupStop:
			m.logger.Info("Session cleanup goroutine stopping")

			return
		}
	}
}

// performCleanup removes expired sessions.
func (m *MemoryManager) performCleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	expired := 0

	for id, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			delete(m.sessions, id)

			expired++
		}
	}

	if expired > 0 {
		m.logger.Info("Cleaned up expired sessions",
			zap.Int("expired", expired),
			zap.Int("remaining", len(m.sessions)))
	}
}

// CreateSession creates a new session.
func (m *MemoryManager) CreateSession(claims *auth.Claims) (*Session, error) {
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

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	m.logger.Info("Session created",
		zap.String("session_id", session.ID),
		zap.String("user", session.User),
		zap.Time("expires_at", session.ExpiresAt),
	)

	return session, nil
}

// GetSession retrieves a session.
func (m *MemoryManager) GetSession(id string) (*Session, error) {
	m.mu.RLock()
	session, exists := m.sessions[id]
	m.mu.RUnlock()

	if !exists {
		return nil, NewSessionNotFoundError(id)
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		// Remove expired session
		if err := m.RemoveSession(id); err != nil {
			// Log but don't fail - expired session cleanup is best effort
			_ = err // Explicitly ignore error
		}

		return nil, NewSessionExpiredError(session.ID, session.ExpiresAt)
	}

	return session, nil
}

// UpdateSession updates a session.
func (m *MemoryManager) UpdateSession(session *Session) error {
	session.UpdatedAt = time.Now()

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		return NewSessionExpiredError(session.ID, session.ExpiresAt)
	}

	m.mu.Lock()

	if _, exists := m.sessions[session.ID]; !exists {
		m.mu.Unlock()

		return NewSessionNotFoundError(session.ID)
	}

	m.sessions[session.ID] = session
	m.mu.Unlock()

	return nil
}

// RemoveSession removes a session.
func (m *MemoryManager) RemoveSession(id string) error {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()

	m.logger.Info("Session removed", zap.String("session_id", id))

	return nil
}

// Close closes the session manager and stops the cleanup goroutine.
func (m *MemoryManager) Close() error {
	m.logger.Info("Closing memory session manager")

	// Stop cleanup goroutine
	close(m.cleanupStop)

	// Wait for cleanup to finish
	select {
	case <-m.cleanupStopped:
		// Cleanup finished
	case <-time.After(defaultMaxConnections * time.Second):
		m.logger.Warn("Cleanup goroutine did not stop in time")
	}

	// Clear all sessions
	m.mu.Lock()
	m.sessions = nil
	m.mu.Unlock()

	return nil
}

// RedisClient returns nil for memory manager.
func (m *MemoryManager) RedisClient() *redis.Client {
	return nil
}

// GetStats returns statistics about the session manager.
func (m *MemoryManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	activeCount := 0
	now := time.Now()

	for _, session := range m.sessions {
		if now.Before(session.ExpiresAt) {
			activeCount++
		}
	}

	return map[string]interface{}{
		"total_sessions":   len(m.sessions),
		"active_sessions":  activeCount,
		"expired_sessions": len(m.sessions) - activeCount,
	}
}
