//go:build darwin

package secure

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	// Serialize keychain operations - macOS keychain deadlocks under concurrent access.
	maxConcurrentOps   = 1
	operationTimeout   = 30 * time.Second       // Increased from 10s to handle slow operations
	retryBackoffBase   = 500 * time.Millisecond // Base backoff duration for exponential retry
	asciiMaxChar       = 127                    // Maximum ASCII character value
	maxBadControlRatio = 0.2                    // Maximum ratio of bad control characters for valid base64
)

// keychainStore implements TokenStore using macOS Keychain Services.
type keychainStore struct {
	appName   string
	semaphore chan struct{} // Per-instance semaphore to limit concurrent operations
}

// newKeychainStore creates a new macOS Keychain-based token store.
//
//nolint:ireturn // Factory pattern requires interface return
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
	// Acquire semaphore to limit concurrent keychain operations
	if err := s.acquireOperationPermits(operation); err != nil {
		return err
	}
	defer s.releaseOperationPermits()

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

	for attempt := 0; attempt < 5; attempt++ {
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

	// Exit status 44 can happen during concurrent updates - retry
	if strings.Contains(errorStr, "exit status 44") {
		return true
	}

	return strings.Contains(errorStr, "resource temporarily unavailable") ||
		strings.Contains(errorStr, "operation not permitted") ||
		strings.Contains(errorStr, "fork/exec")
}

// executeCommandWithTimeout executes a command with proper timeout handling.
// It ensures the process is killed if the context expires, preventing hung processes.
func (s *keychainStore) executeCommandWithTimeout(ctx context.Context, cmd *exec.Cmd) error {
	// Start the command
	if err := cmd.Start(); err != nil {
		return err
	}

	// Create a channel to signal completion
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Wait for either completion or context cancellation
	select {
	case err := <-done:
		// Clean up process resources
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}

		return err
	case <-ctx.Done():
		// Context expired - forcibly kill the process
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done // Wait for Wait() to finish
		// Clean up process resources
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}

		return fmt.Errorf("command killed due to timeout: %w", ctx.Err())
	}
}

// executeCommandWithOutputAndTimeout executes a command with output capture and timeout handling.
func (s *keychainStore) executeCommandWithOutputAndTimeout(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	// Capture stdout
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Create channels for output and completion
	type result struct {
		output []byte
		err    error
	}
	done := make(chan result, 1)

	go func() {
		// Read all output
		output, err := io.ReadAll(stdout)
		// Wait for command to complete
		waitErr := cmd.Wait()
		if waitErr != nil && err == nil {
			err = waitErr
		}
		done <- result{output: output, err: err}
	}()

	// Wait for either completion or context cancellation
	select {
	case res := <-done:
		// Clean up process resources
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}

		return res.output, res.err
	case <-ctx.Done():
		// Context expired - forcibly kill the process
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done // Wait for goroutine to finish
		// Clean up process resources
		if cmd.Process != nil {
			_ = cmd.Process.Release()
		}

		return nil, fmt.Errorf("command killed due to timeout: %w", ctx.Err())
	}
}

// sanitizeKeychainInput is an alias for the shared validation function.
// Kept for clarity that this is validating keychain inputs specifically.
func sanitizeKeychainInput(s string) error {
	return sanitizeTokenInput(s)
}

// Store saves a token to the macOS Keychain.
func (s *keychainStore) Store(key, token string) error {
	// Validate inputs before attempting keychain operations
	if err := sanitizeKeychainInput(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}
	if err := sanitizeKeychainInput(token); err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	return s.executeKeychainOperation("store", func(ctx context.Context) error {
		serviceName := s.serviceName()

		// #nosec G204 - calling system keychain utility with sanitized inputs
		cmd := exec.CommandContext(ctx, "security", "add-generic-password",
			"-a", key, // account name
			"-s", serviceName, // service name
			"-w", token, // password
			"-U") // update if exists

		return s.executeCommandWithTimeout(ctx, cmd)
	})
}

// Retrieve gets a token from the macOS Keychain.
func (s *keychainStore) Retrieve(key string) (string, error) {
	// Validate key before attempting keychain operations
	if err := sanitizeKeychainInput(key); err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}

	var result string

	err := s.executeKeychainOperation("retrieve", func(ctx context.Context) error {
		serviceName := s.serviceName()

		// #nosec G204 - calling system keychain utility with sanitized inputs
		cmd := exec.CommandContext(ctx, "security", "find-generic-password",
			"-a", key, // account name
			"-s", serviceName, // service name
			"-w") // return password only

		output, err := s.executeCommandWithOutputAndTimeout(ctx, cmd)
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
	// Validate key before attempting keychain operations
	if err := sanitizeKeychainInput(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	return s.executeKeychainOperation("delete", func(ctx context.Context) error {
		serviceName := s.serviceName()

		// #nosec G204 - calling system keychain utility with sanitized inputs
		cmd := exec.CommandContext(ctx, "security", "delete-generic-password",
			"-a", key, // account name
			"-s", serviceName) // service name

		err := s.executeCommandWithTimeout(ctx, cmd)
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
