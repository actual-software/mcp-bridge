// Package session provides session management functionality for tracking authenticated user sessions.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

const (
	defaultRetryCount      = 10
	defaultMaxConnections  = 5
	defaultSessionTTLHours = 24 // Default session TTL in hours
	sessionIDBytes         = 16 // Number of random bytes for session ID
)

// Manager manages client sessions.
type Manager interface {
	CreateSession(claims *auth.Claims) (*Session, error)
	GetSession(id string) (*Session, error)
	UpdateSession(session *Session) error
	RemoveSession(id string) error
	Close() error
	RedisClient() *redis.Client
}

// Session represents a client session.
type Session struct {
	ID        string               `json:"id"`
	User      string               `json:"user"`
	CreatedAt time.Time            `json:"created_at"`
	UpdatedAt time.Time            `json:"updated_at"`
	ExpiresAt time.Time            `json:"expires_at"`
	Scopes    []string             `json:"scopes"`
	Metadata  map[string]string    `json:"metadata"`
	RateLimit auth.RateLimitConfig `json:"rate_limit"`
}

// RedisManager implements session management using Redis.
type RedisManager struct {
	client *redis.Client
	logger *zap.Logger
	ttl    time.Duration
}

// CreateSessionStorageManager creates a session storage manager based on configuration.
//

func CreateSessionStorageManager(ctx context.Context, cfg config.SessionConfig, logger *zap.Logger) (Manager, error) {
	switch cfg.Provider {
	case "redis":
		mgr, err := InitializeRedisSessionStore(ctx, cfg.Redis, logger)
		if err != nil {
			return nil, WrapSessionCreationError(ctx, err, "redis")
		}
		// Override TTL if specified
		if cfg.TTL > 0 {
			if redisMgr, ok := mgr.(*RedisManager); ok {
				redisMgr.ttl = time.Duration(cfg.TTL) * time.Second
			}
		}

		return mgr, nil

	case "memory":
		cleanupInterval := time.Duration(cfg.CleanupInterval) * time.Second
		if cleanupInterval <= 0 {
			cleanupInterval = defaultMaxConnections * time.Minute
		}

		return CreateInMemorySessionStore(logger, cleanupInterval), nil

	default:
		return nil, customerrors.New(
			customerrors.TypeValidation,
			"unsupported session provider: "+cfg.Provider,
		).WithComponent("session").
			WithContext("provider", cfg.Provider)
	}
}

// InitializeRedisSessionStore creates a Redis-backed session storage manager.
//

func InitializeRedisSessionStore(ctx context.Context, cfg config.RedisConfig, logger *zap.Logger) (Manager, error) {
	// Parse Redis URL or use individual settings
	var opt *redis.Options

	if cfg.URL != "" {
		var err error

		opt, err = redis.ParseURL(cfg.URL)
		if err != nil {
			return nil, customerrors.Wrap(err, "failed to parse Redis URL").
				WithComponent("session").
				WithContext("redis_url", cfg.URL)
		}
	} else {
		return nil, customerrors.New(customerrors.TypeValidation, "redis URL is required").
			WithComponent("session")
	}

	// Override with explicit settings if provided
	if cfg.Password != "" {
		opt.Password = cfg.Password
	}

	if cfg.DB != 0 {
		opt.DB = cfg.DB
	}

	// Create Redis client
	client := redis.NewClient(opt)

	// Test connection
	pingCtx, cancel := context.WithTimeout(ctx, defaultMaxConnections*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		return nil, customerrors.Wrap(err, "failed to connect to Redis").
			WithComponent("session").
			WithOperation("redis_connect")
	}

	return &RedisManager{
		client: client,
		logger: logger,
		ttl:    defaultSessionTTLHours * time.Hour, // Default session TTL
	}, nil
}

// CreateSession creates a new session.
func (m *RedisManager) CreateSession(claims *auth.Claims) (*Session, error) {
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

	// Store in Redis
	key := "session:" + session.ID

	data, err := json.Marshal(session)
	if err != nil {
		return nil, WrapSessionStoreError(context.Background(), err, "marshal", session.ID)
	}

	// Calculate TTL based on token expiration
	ttl := time.Until(session.ExpiresAt)
	if ttl > m.ttl {
		ttl = m.ttl
	}

	ctx := context.Background()
	if err := m.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return nil, WrapSessionStoreError(context.Background(), err, "set", session.ID)
	}

	m.logger.Info("Session created",
		zap.String("session_id", session.ID),
		zap.String("user", session.User),
		zap.Duration("ttl", ttl),
	)

	return session, nil
}

// GetSession retrieves a session.
func (m *RedisManager) GetSession(id string) (*Session, error) {
	key := "session:" + id
	ctx := context.Background()

	data, err := m.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, NewSessionNotFoundError(id)
		}

		return nil, WrapSessionStoreError(context.Background(), err, "get", id)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, WrapSessionStoreError(context.Background(), err, "unmarshal", id)
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		// Remove expired session
		if err := m.RemoveSession(id); err != nil {
			// Log but don't fail - expired session cleanup is best effort
			// The session is already considered expired anyway
			_ = err // Explicitly ignore error
		}

		return nil, NewSessionExpiredError(session.ID, session.ExpiresAt)
	}

	return &session, nil
}

// UpdateSession updates a session.
func (m *RedisManager) UpdateSession(session *Session) error {
	session.UpdatedAt = time.Now()

	key := "session:" + session.ID

	data, err := json.Marshal(session)
	if err != nil {
		return WrapSessionStoreError(context.Background(), err, "marshal", session.ID)
	}

	// Calculate remaining TTL
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return NewSessionExpiredError(session.ID, session.ExpiresAt)
	}

	if ttl > m.ttl {
		ttl = m.ttl
	}

	ctx := context.Background()
	if err := m.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return WrapSessionStoreError(context.Background(), err, "update", session.ID)
	}

	return nil
}

// RemoveSession removes a session.
func (m *RedisManager) RemoveSession(id string) error {
	key := "session:" + id
	ctx := context.Background()

	if err := m.client.Del(ctx, key).Err(); err != nil {
		return WrapSessionDeleteError(context.Background(), err, id)
	}

	m.logger.Info("Session removed", zap.String("session_id", id))

	return nil
}

// Close closes the session manager.
func (m *RedisManager) Close() error {
	return m.client.Close()
}

// RedisClient returns the underlying Redis client.
func (m *RedisManager) RedisClient() *redis.Client {
	return m.client
}

// HasScope checks if the session has a specific scope.
func (s *Session) HasScope(scope string) bool {
	for _, sc := range s.Scopes {
		if sc == scope {
			return true
		}
	}

	return false
}

// generateSessionID generates a random session ID.
func generateSessionID() string {
	bytes := make([]byte, sessionIDBytes)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if random fails
		return hex.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), defaultRetryCount)))
	}

	return hex.EncodeToString(bytes)
}
