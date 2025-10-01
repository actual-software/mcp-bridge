//go:build !linux

package secure

import "errors"

// secretServiceStore stub for non-Linux platforms.
type secretServiceStore struct{}

// newSecretServiceStore returns an error on non-Linux platforms.
//
//nolint:ireturn // Factory pattern requires interface return
func newSecretServiceStore(appName string) (TokenStore, error) {
	return nil, errors.New("secret service store not available on this platform")
}

// Placeholder methods to satisfy interface (never called due to constructor error).
func (s *secretServiceStore) Store(key, token string) error       { return nil }
func (s *secretServiceStore) Retrieve(key string) (string, error) { return "", nil }
func (s *secretServiceStore) Delete(key string) error             { return nil }
func (s *secretServiceStore) List() ([]string, error)             { return nil, nil }
