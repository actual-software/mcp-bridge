//go:build !windows

package secure

import (
	"errors"
)

// credentialStore stub for non-Windows platforms.
type credentialStore struct{}

// newCredentialStore returns an error on non-Windows platforms.
//
//nolint:ireturn // Factory pattern requires interface return
func newCredentialStore(appName string) (TokenStore, error) {
	return nil, errors.New("credential store not available on this platform")
}

// Placeholder methods to satisfy interface (never called due to constructor error).
func (s *credentialStore) Store(key, token string) error       { return nil }
func (s *credentialStore) Retrieve(key string) (string, error) { return "", nil }
func (s *credentialStore) Delete(key string) error             { return nil }
func (s *credentialStore) List() ([]string, error)             { return nil, nil }
