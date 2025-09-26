package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestMakeOriginChecker(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	tests := createOriginCheckerTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testOriginChecker(t, tt, logger)
		})
	}
}

func createOriginCheckerTests() []struct {
	name           string
	allowedOrigins []string
	requestOrigin  string
	expectedAllow  bool
} {
	var tests []struct {
		name           string
		allowedOrigins []string
		requestOrigin  string
		expectedAllow  bool
	}

	tests = append(tests, createBasicOriginTests()...)
	tests = append(tests, createSpecialOriginTests()...)
	tests = append(tests, createEdgeCaseOriginTests()...)

	return tests
}

func createBasicOriginTests() []struct {
	name           string
	allowedOrigins []string
	requestOrigin  string
	expectedAllow  bool
} {
	return []struct {
		name           string
		allowedOrigins []string
		requestOrigin  string
		expectedAllow  bool
	}{
		{
			name:           "Allow configured origin",
			allowedOrigins: []string{"https://app.example.com"},
			requestOrigin:  "https://app.example.com",
			expectedAllow:  true,
		},
		{
			name:           "Reject non-configured origin",
			allowedOrigins: []string{"https://app.example.com"},
			requestOrigin:  "https://evil.com",
			expectedAllow:  false,
		},
		{
			name:           "Allow localhost by default when no origins configured",
			allowedOrigins: []string{},
			requestOrigin:  "http://localhost",
			expectedAllow:  true,
		},
		{
			name:           "Allow wildcard origin",
			allowedOrigins: []string{"*"},
			requestOrigin:  "https://any-site.com",
			expectedAllow:  true,
		},
	}
}

func createSpecialOriginTests() []struct {
	name           string
	allowedOrigins []string
	requestOrigin  string
	expectedAllow  bool
} {
	return []struct {
		name           string
		allowedOrigins []string
		requestOrigin  string
		expectedAllow  bool
	}{
		{
			name:           "Case insensitive origin matching",
			allowedOrigins: []string{"https://App.Example.COM"},
			requestOrigin:  "https://app.example.com",
			expectedAllow:  true,
		},
		{
			name:           "Allow origin with port",
			allowedOrigins: []string{"http://localhost:3000"},
			requestOrigin:  "http://localhost:3000",
			expectedAllow:  true,
		},
		{
			name:           "Reject different port",
			allowedOrigins: []string{"http://localhost:3000"},
			requestOrigin:  "http://localhost:3001",
			expectedAllow:  false,
		},
		{
			name:           "Allow multiple origins",
			allowedOrigins: []string{"https://app1.example.com", "https://app2.example.com"},
			requestOrigin:  "https://app2.example.com",
			expectedAllow:  true,
		},
	}
}

func createEdgeCaseOriginTests() []struct {
	name           string
	allowedOrigins []string
	requestOrigin  string
	expectedAllow  bool
} {
	return []struct {
		name           string
		allowedOrigins []string
		requestOrigin  string
		expectedAllow  bool
	}{
		{
			name:           "No origin header - allow",
			allowedOrigins: []string{"https://app.example.com"},
			requestOrigin:  "",
			expectedAllow:  true,
		},
		{
			name:           "Invalid origin URL - reject",
			allowedOrigins: []string{"https://app.example.com"},
			requestOrigin:  "not-a-valid-url",
			expectedAllow:  false,
		},
		{
			name:           "Normalize origin paths",
			allowedOrigins: []string{"https://app.example.com/path"},
			requestOrigin:  "https://app.example.com",
			expectedAllow:  true,
		},
	}
}

func testOriginChecker(t *testing.T, tt struct {
	name           string
	allowedOrigins []string
	requestOrigin  string
	expectedAllow  bool
}, logger *zap.Logger) {
	t.Helper()
	t.Parallel()

	checker := makeOriginChecker(tt.allowedOrigins, logger)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if tt.requestOrigin != "" {
		req.Header.Set("Origin", tt.requestOrigin)
	}

	allowed := checker(req)
	assert.Equal(t, tt.expectedAllow, allowed,
		"Origin %s should be allowed=%v with config %v",
		tt.requestOrigin, tt.expectedAllow, tt.allowedOrigins)
}

func TestMakeOriginChecker_InvalidConfig(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()

	// Test with invalid origin URLs in config
	allowedOrigins := []string{
		"https://valid.com",
		"not-a-valid-url",
		"https://another-valid.com",
	}

	checker := makeOriginChecker(allowedOrigins, logger)

	// Should still work with valid origins
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://valid.com")
	assert.True(t, checker(req))

	req.Header.Set("Origin", "https://another-valid.com")
	assert.True(t, checker(req))
}
