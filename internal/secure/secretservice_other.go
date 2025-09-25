//go:build !linux
// +build !linux

package secure

import "errors"

// newSecretServiceStore is not available on non-Linux platforms.
//
//nolint:ireturn // Platform-specific stub returns interface for consistency
func newSecretServiceStore(_ string) (TokenStore, error) {
	return nil, errors.New("secret service store not available on this platform") 
}
