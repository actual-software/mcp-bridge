//go:build !darwin

// Package secure provides secure storage functionality for authentication tokens.
package secure

// keychainStore stub for non-macOS platforms.
type keychainStore struct{}

// newKeychainStore is not available on non-Darwin platforms.
//
//nolint:ireturn // Platform-specific stub returns interface for consistency.
func newKeychainStore(_ string) TokenStore {
	// Return nil on non-Darwin platforms - the factory will fall back to file store
	return nil
}

// Placeholder methods to satisfy interface (never called due to constructor returning nil).
func (k *keychainStore) Store(_, _ string) error     { return nil }
func (k *keychainStore) Retrieve(_ string) (string, error) { return "", nil }
func (k *keychainStore) Delete(_ string) error       { return nil }
func (k *keychainStore) List() ([]string, error)     { return nil, nil }
