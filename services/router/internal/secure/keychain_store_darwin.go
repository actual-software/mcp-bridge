//go:build darwin

package secure

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultRetryCount = 10
	maxConcurrentOps  = 10               // Limit concurrent keychain operations to prevent resource exhaustion
	operationTimeout  = 30 * time.Second // Increased from 10s to handle slow operations
	retryBackoffBase  = 100 * time.Millisecond // Base backoff duration for exponential retry
	asciiMaxChar      = 127                    // Maximum ASCII character value
	maxBadControlRatio = 0.2                  // Maximum ratio of bad control characters for valid base64
)

// keychainStore implements TokenStore using macOS Keychain Services.
type keychainStore struct {
	appName   string
	mu        sync.Mutex    // Protect individual store operations
	semaphore chan struct{} // Per-instance semaphore to limit concurrent operations
}

// newKeychainStore creates a new macOS Keychain-based token store.
func newKeychainStore(appName string) (TokenStore, error) {
	// Check if security command is available.
	if _, err := exec.LookPath("security"); err != nil {
		return nil, errors.New("security command not found, keychain unavailable")
	}

	return &keychainStore{
		appName:   appName,
		semaphore: make(chan struct{}, maxConcurrentOps),
	}, nil
}

// executeKeychainOperation executes a keychain operation with proper concurrency control,
// timeout handling, and error recovery.

func (s *keychainStore) executeKeychainOperation(operation string, fn func(context.Context) error) error {
	if err := s.acquireOperationPermits(operation); err != nil {
		return err
	}
	defer s.releaseOperationPermits()

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.executeWithRetry(operation, fn)
}

func (s *keychainStore) acquireOperationPermits(operation string) error {
	// Acquire semaphore first to limit concurrent operations
	select {
	case s.semaphore <- struct{}{}:
		return nil
	case <-time.After(operationTimeout):
		return fmt.Errorf("keychain %s operation: semaphore acquisition timed out", operation)
	}
}

func (s *keychainStore) releaseOperationPermits() {
	// Ensure semaphore is always released, even on panic
	select {
	case <-s.semaphore:
	default:
		// Should never happen, but just in case
	}
}

func (s *keychainStore) executeWithRetry(operation string, fn func(context.Context) error) error {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()

	// Execute operation with retry on recoverable errors
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := s.waitForRetry(ctx, attempt, operation); err != nil {
				return err
			}
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}

		// Check if error is recoverable
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("keychain %s operation: timed out after %v", operation, operationTimeout)
		}

		if !s.isRecoverableError(lastErr) {
			// Non-recoverable error - return immediately
			break
		}
	}

	return fmt.Errorf("keychain %s operation failed after retries: %w", operation, lastErr)
}

func (s *keychainStore) waitForRetry(ctx context.Context, attempt int, operation string) error {
	// Exponential backoff between retries
	backoff := time.Duration(attempt*attempt) * retryBackoffBase
	select {
	case <-time.After(backoff):
		return nil
	case <-ctx.Done():
		return fmt.Errorf("keychain %s operation: context cancelled during retry", operation)
	}
}

func (s *keychainStore) isRecoverableError(err error) bool {
	// Check for specific recoverable errors
	errorStr := err.Error()
	return strings.Contains(errorStr, "resource temporarily unavailable") ||
		strings.Contains(errorStr, "operation not permitted") ||
		strings.Contains(errorStr, "fork/exec")
}

// Store saves a token to the macOS Keychain.
func (s *keychainStore) Store(key, token string) error {
	return s.executeKeychainOperation("store", func(ctx context.Context) error {
		serviceName := s.serviceName()

		
		cmd := exec.CommandContext(ctx, "security", "add-generic-password",
			"-a", key, // account name
			"-s", serviceName, // service name
			"-w", token, // password
			"-U") // update if exists

		// Set process group to enable cleanup of child processes
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		err := cmd.Run()

		// Ensure process is fully cleaned up
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}

		return err
	})
}

// Retrieve gets a token from the macOS Keychain.
func (s *keychainStore) Retrieve(key string) (string, error) {
	var result string

	err := s.executeKeychainOperation("retrieve", func(ctx context.Context) error {
		serviceName := s.serviceName()

		
		cmd := exec.CommandContext(ctx, "security", "find-generic-password",
			"-a", key, // account name
			"-s", serviceName, // service name
			"-w") // return password only

		// Set process group to enable cleanup of child processes
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		output, err := cmd.Output()

		// Ensure process is fully cleaned up
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}

		if err != nil {
			if strings.Contains(err.Error(), "could not be found") {
				return ErrTokenNotFound
			}

			return err
		}

		// Remove trailing newline.
		token := strings.TrimSpace(string(output))

		// Check if the token is hex-encoded (keychain does this for special characters)
		if isHexEncoded(token) {
			if decoded, err := hex.DecodeString(token); err == nil {
				token = string(decoded)
			}
		}

		result = token

		return nil
	})

	return result, err
}

// Delete removes a token from the macOS Keychain.
func (s *keychainStore) Delete(key string) error {
	return s.executeKeychainOperation("delete", func(ctx context.Context) error {
		serviceName := s.serviceName()

		
		cmd := exec.CommandContext(ctx, "security", "delete-generic-password",
			"-a", key, // account name
			"-s", serviceName) // service name

		// Set process group to enable cleanup of child processes
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		err := cmd.Run()

		// Ensure process is fully cleaned up
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}

		if err != nil {
			// Don't treat "item not found" as an error.
			// Exit status 44 means "The specified item could not be found in the keychain"
			exitError := &exec.ExitError{}
			if errors.As(err, &exitError) {
				return nil
			}

			if strings.Contains(err.Error(), "could not be found") {
				return nil
			}

			return err
		}

		return nil
	})
}

// List returns all stored token keys.
func (s *keychainStore) List() ([]string, error) {
	// Note: macOS Keychain doesn't provide an easy way to list accounts for a service.
	// This would require parsing complex security output, so we return not supported.
	return nil, ErrListNotSupported
}

// serviceName returns the keychain service name for this app.
func (s *keychainStore) serviceName() string {
	return "mcp-router-" + s.appName
}

// isHexEncoded checks if a string looks like keychain-encoded data
// The keychain hex-encodes when tokens contain special characters/binary data.
func isHexEncoded(s string) bool { 
	if !isValidHexString(s) {
		return false
	}
	
	decoded, err := decodeAndValidateLength(s)
	if err != nil {
		return false
	}
	
	return containsProblematicCharacters(decoded) && passesQualityChecks(decoded)
}

func isValidHexString(s string) bool {
	// Must be even length and only contain hex characters
	if len(s)%2 != 0 || len(s) == 0 {
		return false
	}
	
	matched, _ := regexp.MatchString("^[0-9a-fA-F]+$", s)
	return matched
}

func decodeAndValidateLength(s string) ([]byte, error) {
	// Only decode relatively short hex strings - keychain typically doesn't
	// hex-encode long data, and long hex strings are very likely legitimate tokens
	decoded, err := hex.DecodeString(s)
	if err != nil || len(decoded) > 64 {
		// Conservative limit: anything longer is probably a real hex token
		return nil, fmt.Errorf("decode failed or too long")
	}
	return decoded, nil
}

func containsProblematicCharacters(decoded []byte) bool {
	decodedStr := string(decoded)
	
	// Check for specific characters that we know cause keychain encoding
	hasNewlineTab := strings.Contains(decodedStr, "\n") ||
		strings.Contains(decodedStr, "\t") ||
		strings.Contains(decodedStr, "\r")
	hasBackslash := strings.Contains(decodedStr, "\\")
	
	// Check for non-ASCII Unicode characters (by looking for valid UTF-8)
	hasNonASCII := false

	for _, r := range decodedStr {
		if r > asciiMaxChar {
			hasNonASCII = true
			break
		}
	}
	
	// Must contain known problematic characters
	return hasNewlineTab || hasBackslash || hasNonASCII
}

func passesQualityChecks(decoded []byte) bool {
	if !hasAcceptableControlCharRatio(decoded) {
		return false
	}
	
	return isValidUTF8Content(decoded)
}

func hasAcceptableControlCharRatio(decoded []byte) bool {
	// Additional safety: check if it's mostly readable content
	// Count control characters (excluding the known good ones)
	badControlChars := 0

	for _, b := range decoded {
		if b < 32 && b != '\n' && b != '\r' && b != '\t' {
			badControlChars++
		}
	}
	
	// If more than 20% of bytes are bad control characters, it's likely random data
	return float64(badControlChars)/float64(len(decoded)) <= maxBadControlRatio
}

func isValidUTF8Content(decoded []byte) bool {
	// Check if the string is valid UTF-8 (important for Unicode text)
	decodedStr := string(decoded)
	hasNonASCII := false

	for _, r := range decodedStr {
		if r > asciiMaxChar {
			hasNonASCII = true
			break
		}
	}
	
	if hasNonASCII && strings.ToValidUTF8(decodedStr, "") != decodedStr {
		return false // Invalid UTF-8 suggests random binary data
	}
	
	return true
}

