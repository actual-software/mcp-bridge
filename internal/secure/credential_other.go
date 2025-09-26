//go:build !windows

// Package secure provides secure storage functionality for authentication tokens.
package secure

import "errors"

// newCredentialStore is not available on non-Windows platforms.
//
//nolint:ireturn // Platform-specific stub returns interface for consistency
func newCredentialStore(_ string) (TokenStore, error) {
	return nil, errors.New("credential store not available on this platform")
}
