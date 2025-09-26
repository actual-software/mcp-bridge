//go:build darwin

package secure

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// keychainStore implements TokenStore using macOS Keychain.
type keychainStore struct {
	service string
}

//nolint:ireturn // Returns interface for consistency with other platform stores
func newKeychainStore(appName string) TokenStore {
	return &keychainStore{
		service: fmt.Sprintf("com.%s.mcp-router", appName),
	}
}

func (k *keychainStore) Store(key, token string) error {
	// Validate inputs to prevent command injection
	if !isValidKeychainParam(key) {
		return errors.New("invalid key parameter")
	}

	if !isValidKeychainParam(token) {
		return errors.New("invalid token parameter")
	}

	// First, try to delete existing entry (ignore error if key doesn't exist)

	_ = k.Delete(key)

	// Add new entry to keychain
	// #nosec G204 - inputs are validated above
	cmd := exec.CommandContext(context.Background(), "security", "add-generic-password",
		"-a", key,
		"-s", k.service,
		"-w", token,
		"-U") // Update if exists

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to store token in keychain: %w, output: %s", err, output)
	}

	return nil
}

func (k *keychainStore) Retrieve(key string) (string, error) {
	// Validate input to prevent command injection
	if !isValidKeychainParam(key) {
		return "", errors.New("invalid key parameter")
	}

	// #nosec G204 - input is validated above
	cmd := exec.CommandContext(context.Background(), "security", "find-generic-password",
		"-a", key,
		"-s", k.service,
		"-w") // Just output password

	output, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "could not be found") {
			return "", fmt.Errorf("token not found: %s", key)
		}

		return "", fmt.Errorf("failed to retrieve token from keychain: %w", err)
	}

	// Remove trailing newline
	token := strings.TrimSpace(string(output))

	return token, nil
}

func (k *keychainStore) Delete(key string) error {
	// Validate input to prevent command injection
	if !isValidKeychainParam(key) {
		return errors.New("invalid key parameter")
	}

	// #nosec G204 - input is validated above
	cmd := exec.CommandContext(context.Background(), "security", "delete-generic-password",
		"-a", key,
		"-s", k.service)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// It's okay if the item doesn't exist
		if strings.Contains(string(output), "could not be found") {
			return nil
		}

		return fmt.Errorf("failed to delete token from keychain: %w, output: %s", err, output)
	}

	return nil
}

func (k *keychainStore) List() ([]string, error) {
	// List all entries for this service
	cmd := exec.CommandContext(context.Background(), "security", "dump-keychain")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list keychain entries: %w", err)
	}

	// Parse the output to find entries for our service
	var keys []string

	lines := strings.Split(string(output), "\n")

	inOurService := false

	for _, line := range lines {
		if strings.Contains(line, fmt.Sprintf(`"svce"<blob>=%q`, k.service)) {
			inOurService = true

			continue
		}

		if inOurService && strings.Contains(line, `"acct"<blob>="`) {
			// Extract account name
			start := strings.Index(line, `"acct"<blob>="`) + len(`"acct"<blob>="`)
			end := strings.Index(line[start:], `"`)

			if end > 0 {
				key := line[start : start+end]
				keys = append(keys, key)
				inOurService = false
			}
		}

		// Reset if we hit another keychain entry
		if strings.HasPrefix(strings.TrimSpace(line), "keychain:") {
			inOurService = false
		}
	}

	return keys, nil
}

// isValidKeychainParam validates that a parameter is safe for use with the security command.
// It allows alphanumeric characters, hyphens, underscores, dots, and basic punctuation.
func isValidKeychainParam(param string) bool {
	if param == "" {
		return false
	}
	// Allow alphanumeric, hyphens, underscores, dots, and some safe punctuation
	validPattern := regexp.MustCompile(`^[a-zA-Z0-9._@:-]+$`)

	return validPattern.MatchString(param)
}
