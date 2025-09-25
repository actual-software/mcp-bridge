package secure

import (
	"errors"
	"runtime"
)

const (
	// OSTypeLinux represents the Linux operating system.
	OSTypeLinux = "linux"
	// OSTypeDarwin represents the macOS/Darwin operating system.
	OSTypeDarwin = "darwin"
	// OSTypeWindows represents the Windows operating system.
	OSTypeWindows = "windows"
)

// TokenStore provides a secure interface for storing and retrieving authentication tokens.
type TokenStore interface {
	// Store securely saves a token with the given key.
	Store(key, token string) error

	// Retrieve gets a token by key, returns error if not found.
	Retrieve(key string) (string, error)

	// Delete removes a token by key.
	Delete(key string) error

	// List returns all stored token keys (may not be supported on all platforms).
	List() ([]string, error)
}

// ErrTokenNotFound is returned when a token is not found.
var ErrTokenNotFound = errors.New("token not found")

// ErrListNotSupported is returned when List() is not supported on the platform.
var ErrListNotSupported = errors.New("listing tokens not supported on this platform")

// NewTokenStore creates a new platform-appropriate token store.
func NewTokenStore(appName string) (TokenStore, error) {
	switch runtime.GOOS {
	case OSTypeDarwin:
		// Try macOS Keychain first.
		if store, err := newKeychainStore(appName); err == nil {
			return store, nil
		}
		// Fall back to encrypted file store.
		return newEncryptedFileStore(appName)

	case OSTypeWindows:
		// Try Windows Credential Manager first.
		if store, err := newCredentialStore(appName); err == nil {
			return store, nil
		}
		// Fall back to encrypted file store.
		return newEncryptedFileStore(appName)

	case OSTypeLinux:
		// Try Secret Service (GNOME/KDE) first.
		if store, err := newSecretServiceStore(appName); err == nil {
			return store, nil
		}
		// Fall back to encrypted file store.
		return newEncryptedFileStore(appName)

	default:
		// Unknown platform, use encrypted file store.
		return newEncryptedFileStore(appName)
	}
}
