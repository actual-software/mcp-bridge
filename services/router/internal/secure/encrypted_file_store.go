package secure

import (
	"errors"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

// encryptedFileStore stores tokens in an encrypted file.
type encryptedFileStore struct {
	appName  string
	filePath string
	key      []byte
	mu       sync.RWMutex
}

// tokenData represents the structure stored in the encrypted file.
type tokenData struct {
	Tokens map[string]string `json:"tokens"`
}

// newEncryptedFileStore creates a new encrypted file-based token store.
func newEncryptedFileStore(appName string) (TokenStore, error) {
	// Get user config directory.
	configDir, err := os.UserConfigDir()
	if err != nil {
		// Fall back to user home directory.
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return nil, fmt.Errorf("failed to get config directory: %w", err)
		}

		configDir = filepath.Join(homeDir, ".config")
	}

	// Create app-specific directory.
	appDir := filepath.Join(configDir, "mcp-router")
	if err := os.MkdirAll(appDir, DirectoryPermissions); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	filePath := filepath.Join(appDir, "tokens.enc")

	// Generate a key based on the app name and system properties.
	key := generateKey(appName)

	store := &encryptedFileStore{
		appName:  appName,
		filePath: filePath,
		key:      key,
	}

	return store, nil
}

// generateKey creates a deterministic encryption key from app name and system info.
func generateKey(appName string) []byte {
	// Use PBKDF2 with app name as password and a fixed salt derived from system.
	// This provides reasonable security while being deterministic.
	salt := sha256.Sum256([]byte("mcp-router-salt-" + appName))

	return pbkdf2.Key([]byte(appName), salt[:], KeyDerivationIterations, KeyDerivationKeySize, sha256.New)
}

// Store saves a token with the given key.
func (s *encryptedFileStore) Store(key, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load existing tokens.
	data, err := s.loadTokens()
	if err != nil {
		return err
	}

	// Update the token.
	data.Tokens[key] = token

	// Save back to file.
	return s.saveTokens(data)
}

// Retrieve gets a token by key.
func (s *encryptedFileStore) Retrieve(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.loadTokens()
	if err != nil {
		return "", err
	}

	token, exists := data.Tokens[key]
	if !exists {
		return "", ErrTokenNotFound
	}

	return token, nil
}

// Delete removes a token by key.
func (s *encryptedFileStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadTokens()
	if err != nil {
		return err
	}

	delete(data.Tokens, key)

	return s.saveTokens(data)
}

// List returns all stored token keys.
func (s *encryptedFileStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.loadTokens()
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(data.Tokens))
	for key := range data.Tokens {
		keys = append(keys, key)
	}

	return keys, nil
}

// loadTokens loads and decrypts the token data from file.
func (s *encryptedFileStore) loadTokens() (*tokenData, error) {
	// If file doesn't exist, return empty data.
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		return &tokenData{Tokens: make(map[string]string)}, nil
	}

	// Read encrypted file.
	encryptedData, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	// If file is empty, return empty data.
	if len(encryptedData) == 0 {
		return &tokenData{Tokens: make(map[string]string)}, nil
	}

	// Decrypt the data.
	decryptedData, err := s.decrypt(encryptedData)
	if err != nil {
		// Handle corruption by returning empty data and allowing recovery
		// The corrupted file will be overwritten on next save
		
		return &tokenData{Tokens: make(map[string]string)}, nil
	}

	// Parse JSON.
	var data tokenData
	if err := json.Unmarshal(decryptedData, &data); err != nil {
		// Handle JSON corruption by returning empty data and allowing recovery
		
		return &tokenData{Tokens: make(map[string]string)}, nil
	}

	// Initialize map if nil.
	if data.Tokens == nil {
		data.Tokens = make(map[string]string)
	}

	return &data, nil
}

// saveTokens encrypts and saves the token data to file.
func (s *encryptedFileStore) saveTokens(data *tokenData) error {
	// Marshal to JSON.
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal token data: %w", err)
	}

	// Encrypt the data.
	encryptedData, err := s.encrypt(jsonData)
	if err != nil {
		return fmt.Errorf("failed to encrypt token data: %w", err)
	}

	// Write to file with proper permissions.
	return os.WriteFile(s.filePath, encryptedData, FilePermissions)
}

// encrypt encrypts data using AES-GCM.
func (s *encryptedFileStore) encrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
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

	ciphertext := gcm.Seal(nonce, nonce, data, nil)

	return ciphertext, nil
}

// decrypt decrypts data using AES-GCM.
func (s *encryptedFileStore) decrypt(data []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
