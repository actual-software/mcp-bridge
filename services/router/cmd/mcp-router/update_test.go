package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// VersionDev represents the development version.
	VersionDev = "dev"
)

type mockRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

func TestCheckForUpdatesBackground(t *testing.T) {
	tests := createUpdateBackgroundTests()
	runUpdateBackgroundTests(t, tests)
}

func createUpdateBackgroundTests() []struct {
	name           string
	currentVersion string
	latestVersion  string
	expectOutput   bool
} {
	return []struct {
		name           string
		currentVersion string
		latestVersion  string
		expectOutput   bool
	}{
		{
			name:           "Update available",
			currentVersion: "v1.0.0",
			latestVersion:  "v2.0.0",
			expectOutput:   true,
		},
		{
			name:           "Same version",
			currentVersion: "v1.0.0",
			latestVersion:  "v1.0.0",
			expectOutput:   false,
		},
		{
			name:           "Dev version skips check",
			currentVersion: "dev",
			latestVersion:  "v2.0.0",
			expectOutput:   false,
		},
		{
			name:           "Empty latest version",
			currentVersion: "v1.0.0",
			latestVersion:  "",
			expectOutput:   false,
		},
	}
}

func runUpdateBackgroundTests(t *testing.T, tests []struct {
	name           string
	currentVersion string
	latestVersion  string
	expectOutput   bool
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupVersionForTest(t, tt.currentVersion)

			if tt.currentVersion == VersionDev {
				checkForUpdates()

				return
			}

			server := createMockUpdateServer(t, tt.latestVersion)
			defer server.Close()

			setupMockHTTPClientForBackground(t, server.URL)
			output := captureUpdateCheckOutput(t)

			verifyUpdateOutput(t, output, tt)
		})
	}
}

func setupVersionForTest(t *testing.T, version string) {
	t.Helper()

	oldVersion := Version
	Version = version

	t.Cleanup(func() { Version = oldVersion })
}

func createMockUpdateServer(t *testing.T, latestVersion string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/poiley/mcp-bridge/releases/latest", r.URL.Path)

		release := mockRelease{
			TagName: latestVersion,
		}
		if err := json.NewEncoder(w).Encode(release); err != nil {
			t.Logf("Failed to encode response: %v", err)
		}
	}))
}

func setupMockHTTPClientForBackground(t *testing.T, serverURL string) {
	t.Helper()

	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: &mockTransport{serverURL: serverURL},
	}

	t.Cleanup(func() { http.DefaultClient = oldClient })
}

func captureUpdateCheckOutput(t *testing.T) string {
	t.Helper()

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	checkForUpdates()

	_ = w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Logf("Failed to copy data: %v", err)
	}

	return buf.String()
}

func verifyUpdateOutput(t *testing.T, output string, tt struct {
	name           string
	currentVersion string
	latestVersion  string
	expectOutput   bool
}) {
	t.Helper()

	if tt.expectOutput {
		assert.Contains(t, output, "Update available")
		assert.Contains(t, output, tt.latestVersion)
		assert.Contains(t, output, tt.currentVersion)
	} else {
		assert.Empty(t, output)
	}
}

func TestCheckForUpdatesVerbose(t *testing.T) {
	tests := createUpdateVerboseTests()
	runUpdateVerboseTests(t, tests)
}

func createUpdateVerboseTests() []struct {
	name           string
	currentVersion string
	release        mockRelease
	serverError    bool
	expectError    bool
	expectUpdate   bool
} {
	return []struct {
		name           string
		currentVersion string
		release        mockRelease
		serverError    bool
		expectError    bool
		expectUpdate   bool
	}{
		{
			name:           "New version available",
			currentVersion: "v1.0.0",
			release: mockRelease{
				TagName:     "v2.0.0",
				Name:        "Version 2.0.0",
				PublishedAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
				HTMLURL:     "https://github.com/poiley/mcp-bridge/releases/tag/v2.0.0",
			},
			expectUpdate: true,
		},
		{
			name:           "Same version",
			currentVersion: "v2.0.0",
			release: mockRelease{
				TagName: "v2.0.0",
			},
			expectUpdate: false,
		},
		{
			name:           "Server error",
			currentVersion: "v1.0.0",
			serverError:    true,
			expectError:    true,
		},
	}
}

func runUpdateVerboseTests(t *testing.T, tests []struct {
	name           string
	currentVersion string
	release        mockRelease
	serverError    bool
	expectError    bool
	expectUpdate   bool
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupVersionForTest(t, tt.currentVersion)

			server := createMockVerboseUpdateServer(t, tt)
			defer server.Close()

			setupMockHTTPClientForVerbose(t, server.URL)
			output, err := captureVerboseUpdateOutput(t)

			verifyVerboseUpdateOutput(t, output, err, tt)
		})
	}
}

func createMockVerboseUpdateServer(t *testing.T, tt struct {
	name           string
	currentVersion string
	release        mockRelease
	serverError    bool
	expectError    bool
	expectUpdate   bool
}) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tt.serverError {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(tt.release); err != nil {
			t.Logf("Failed to encode response: %v", err)
		}
	}))
}

func setupMockHTTPClientForVerbose(t *testing.T, serverURL string) {
	t.Helper()

	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: &mockTransport{serverURL: serverURL},
	}

	t.Cleanup(func() { http.DefaultClient = oldClient })
}

func captureVerboseUpdateOutput(t *testing.T) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := checkForUpdatesVerbose()

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, copyErr := io.Copy(&buf, r); copyErr != nil {
		t.Logf("Failed to copy data: %v", copyErr)
	}

	return buf.String(), err
}

func verifyVerboseUpdateOutput(t *testing.T, output string, err error, tt struct {
	name           string
	currentVersion string
	release        mockRelease
	serverError    bool
	expectError    bool
	expectUpdate   bool
}) {
	t.Helper()

	if tt.expectError {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
		assert.Contains(t, output, "Checking for updates...")
		assert.Contains(t, output, "Current version: "+tt.currentVersion)
		assert.Contains(t, output, "Latest version:  "+tt.release.TagName)

		if tt.expectUpdate {
			assert.Contains(t, output, "A new version is available!")
			assert.Contains(t, output, "Released: 2024-01-15")
			assert.Contains(t, output, tt.release.HTMLURL)
			assert.Contains(t, output, "To update, run:")
		} else {
			assert.Contains(t, output, "You are running the latest version")
		}
	}
}

func TestUpdateCheckNetworkErrors(t *testing.T) {
	tests := []struct {
		name        string
		setupMock   func() *httptest.Server
		expectError bool
		errorMsg    string
	}{
		{
			name: "Connection timeout",
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					time.Sleep(11 * time.Second) // Longer than client timeout
				}))
			},
			expectError: true,
			errorMsg:    "failed to check for updates",
		},
		{
			name: "Invalid JSON response",
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("invalid json"))
				}))
			},
			expectError: true,
			errorMsg:    "failed to parse release info",
		},
		{
			name: "httpStatusNotFound Not Found",
			setupMock: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			},
			expectError: true,
			errorMsg:    "failed to parse release info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server.
			server := tt.setupMock()
			defer server.Close()

			// Mock HTTP client with short timeout for timeout test.
			oldClient := http.DefaultClient
			http.DefaultClient = &http.Client{
				Timeout:   testIterations * time.Millisecond,
				Transport: &mockTransport{serverURL: server.URL},
			}

			defer func() { http.DefaultClient = oldClient }()

			// Run verbose check.
			err := checkForUpdatesVerbose()

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func setupMockRelease() mockRelease {
	return mockRelease{
		TagName:     "v3.0.0",
		Name:        "Version 3.0.0 - Major Update",
		PublishedAt: time.Date(2024, 2, 1, 15, 30, 0, 0, time.UTC),
		HTMLURL:     "https://github.com/poiley/mcp-bridge/releases/tag/v3.0.0",
	}
}

func setupMockServer(t *testing.T, release mockRelease) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(release); err != nil {
			t.Logf("Failed to encode response: %v", err)
		}
	}))
}

func setupMockHTTPClient(serverURL string) func() {
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: &mockTransport{serverURL: serverURL},
	}

	return func() { http.DefaultClient = oldClient }
}

func setupMockVersion(version string) func() {
	oldVersion := Version
	Version = version

	return func() { Version = oldVersion }
}

func TestUpdateCheckCommandMock(t *testing.T) {
	release := setupMockRelease()

	server := setupMockServer(t, release)
	defer server.Close()

	restoreHTTP := setupMockHTTPClient(server.URL)
	defer restoreHTTP()

	restoreVersion := setupMockVersion("v1.0.0")
	defer restoreVersion()

	// Run command.
	cmd := updateCheckCmd()

	// Capture output.
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	// Execute command.
	cmd.Run(cmd, []string{})

	// Restore.
	_ = wOut.Close()
	_ = wErr.Close()

	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Read output.
	var outBuf, errBuf bytes.Buffer

	if _, err := io.Copy(&outBuf, rOut); err != nil {
		t.Logf("Failed to copy data: %v", err)
	}

	if _, err := io.Copy(&errBuf, rErr); err != nil {
		t.Logf("Failed to copy data: %v", err)
	}

	output := outBuf.String()
	errorOutput := errBuf.String()

	// Verify output.
	assert.Contains(t, output, "Checking for updates...")
	assert.Contains(t, output, "Current version: v1.0.0")
	assert.Contains(t, output, "Latest version:  v3.0.0")
	assert.Contains(t, output, "A new version is available!")
	assert.Empty(t, errorOutput)
}

// mockTransport redirects GitHub API requests to test server.
type mockTransport struct {
	serverURL string
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace GitHub API URL with test server URL.
	if strings.Contains(req.URL.String(), "api.github.com") {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(t.serverURL, "http://")
	}

	return http.DefaultTransport.RoundTrip(req)
}
