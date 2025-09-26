//nolint:ireturn // This file contains factory functions that must return interfaces
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// File permissions for config directory.
	configDirPerm = 0o700
	// File permissions for token file.
	tokenFilePerm = 0o600
	// PBKDF2 iterations for key derivation.
	pbkdf2Iterations = 10000
	// Key length for AES encryption.
	keyLength = 32
	// Operating system identifiers.
	osLinux   = "linux"
	osDarwin  = "darwin"
	osWindows = "windows"
)

// TokenStore provides secure storage for authentication tokens.
type TokenStore interface {
	// Store saves a token securely
	Store(key, token string) error
	// Retrieve gets a token from secure storage
	Retrieve(key string) (string, error)
	// Delete removes a token from storage
	Delete(key string) error
	// List returns all stored token keys
	List() ([]string, error)
}

// NewTokenStore creates the appropriate token store for the current platform.
// Returns TokenStore interface because different platforms require different
// implementations (Keychain on macOS, Credential Store on Windows, etc.)
// This is a factory pattern where the interface return is necessary.
//
//nolint:ireturn // Factory must return interface for platform-specific implementations
func NewTokenStore(appName string) (TokenStore, error) {
	// Try platform-specific stores first
	switch runtime.GOOS {
	case osDarwin:
		store := newKeychainStore(appName)

		return store, nil
	case osWindows:
		store, err := newCredentialStore(appName)
		if err == nil {
			return store, nil
		}
	case osLinux:
		store, err := newSecretServiceStore(appName)
		if err == nil {
			return store, nil
		}
	}

	// Fall back to encrypted file storage
	return newEncryptedFileStore(appName)
}

// encryptedFileStore provides file-based encrypted token storage as a fallback.
type encryptedFileStore struct {
	appName  string
	filePath string
	password []byte
	mu       sync.Mutex
}

//nolint:ireturn // Factory function must return interface for platform-specific implementations
func newEncryptedFileStore(appName string) (TokenStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", appName)
	if err := os.MkdirAll(configDir, configDirPerm); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Generate a deterministic password based on app name and machine ID
	password := generatePassword(appName)

	return &encryptedFileStore{
		appName:  appName,
		filePath: filepath.Join(configDir, "tokens.enc"),
		password: password,
		mu:       sync.Mutex{}, // Zero value mutex for concurrent access protection
	}, nil
}

func generatePassword(appName string) []byte {
	// Use machine-specific data to generate a deterministic password
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	salt := fmt.Sprintf("%s-%s-%s", appName, hostname, username)

	return pbkdf2.Key([]byte(salt), []byte(appName), pbkdf2Iterations, keyLength, sha256.New)
}

type tokenData struct {
	Tokens map[string]string `json:"tokens"`
}

func (s *encryptedFileStore) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.password)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *encryptedFileStore) decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.password)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	return gcm.Open(nil, nonce, ciphertext, nil)
}

func (s *encryptedFileStore) load() (*tokenData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &tokenData{Tokens: make(map[string]string)}, nil
		}

		return nil, err
	}

	decrypted, err := s.decrypt(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt tokens: %w", err)
	}

	var tokenData tokenData
	if err := json.Unmarshal(decrypted, &tokenData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tokens: %w", err)
	}

	if tokenData.Tokens == nil {
		tokenData.Tokens = make(map[string]string)
	}

	return &tokenData, nil
}

func (s *encryptedFileStore) save(td *tokenData) error {
	data, err := json.Marshal(td)
	if err != nil {
		return fmt.Errorf("failed to marshal tokens: %w", err)
	}

	encrypted, err := s.encrypt(data)
	if err != nil {
		return fmt.Errorf("failed to encrypt tokens: %w", err)
	}

	// Write to temporary file first
	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, encrypted, tokenFilePerm); err != nil {
		return fmt.Errorf("failed to write encrypted tokens: %w", err)
	}

	// Atomic rename
	return os.Rename(tmpFile, s.filePath)
}

func (s *encryptedFileStore) Store(key, token string) error {
	tokenData, err := s.load()
	if err != nil {
		return err
	}

	tokenData.Tokens[key] = base64.StdEncoding.EncodeToString([]byte(token))

	return s.save(tokenData)
}

func (s *encryptedFileStore) Retrieve(key string) (string, error) {
	tokenData, err := s.load()
	if err != nil {
		return "", err
	}

	encoded, ok := tokenData.Tokens[key]
	if !ok {
		return "", fmt.Errorf("token not found: %s", key)
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode token: %w", err)
	}

	return string(decoded), nil
}

func (s *encryptedFileStore) Delete(key string) error {
	tokenData, err := s.load()
	if err != nil {
		return err
	}

	delete(tokenData.Tokens, key)

	return s.save(tokenData)
}

func (s *encryptedFileStore) List() ([]string, error) {
	tokenData, err := s.load()
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(tokenData.Tokens))
	for k := range tokenData.Tokens {
		keys = append(keys, k)
	}

	return keys, nil
}
