package secure

import (
	"fmt"
	"sync"
)

// mockStore is an in-memory token store for testing edge cases.
// It implements the same input validation as real stores but without
// external dependencies like keychain or filesystem operations.
// This allows edge case tests to run quickly and reliably.
type mockStore struct {
	data map[string]string
	mu   sync.RWMutex
}

// newMockStore creates a new in-memory mock token store for testing.
func newMockStore() *mockStore {
	return &mockStore{
		data: make(map[string]string),
	}
}

// Store saves a token with the given key.
func (s *mockStore) Store(key, token string) error {
	// Apply the same validation as real stores
	if err := sanitizeTokenInput(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}
	if err := sanitizeTokenInput(token); err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = token

	return nil
}

// Retrieve gets a token by key.
func (s *mockStore) Retrieve(key string) (string, error) {
	// Apply the same validation as real stores
	if err := sanitizeTokenInput(key); err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	token, exists := s.data[key]
	if !exists {
		return "", ErrTokenNotFound
	}

	return token, nil
}

// Delete removes a token by key.
func (s *mockStore) Delete(key string) error {
	// Apply the same validation as real stores
	if err := sanitizeTokenInput(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)

	return nil
}

// List returns all stored token keys.
func (s *mockStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}

	return keys, nil
}
