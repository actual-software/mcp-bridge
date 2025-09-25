//go:build !darwin

package secure

import "fmt"

// keychainStore stub for non-macOS platforms
type keychainStore struct{}

// newKeychainStore returns an error on non-macOS platforms
func newKeychainStore(appName string) (TokenStore, error) {
	return nil, fmt.Errorf("keychain store not available on this platform")
}

// Placeholder methods to satisfy interface (never called due to constructor error).
func (s *keychainStore) Store(key, token string) error       { return nil }
func (s *keychainStore) Retrieve(key string) (string, error) { return "", nil }
func (s *keychainStore) Delete(key string) error             { return nil }
func (s *keychainStore) List() ([]string, error)             { return nil, nil }
