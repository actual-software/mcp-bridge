package gateway

import (
	"testing"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

func TestNewNamespaceRouter(t *testing.T) {
	logger := zap.NewNop()

	config := NamespaceRoutingConfig{
		Enabled: true,
		Rules: []NamespaceRoutingRule{
			{
				Pattern:     "docker\\..*",
				Tags:        []string{"docker", "containers"},
				Priority:    1,
				Description: "Docker commands",
			},
		},
	}

	router, err := NewNamespaceRouter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create namespace router: %v", err)
	}

	if router == nil {
		t.Fatal("Expected non-nil namespace router")
	}

	if !router.config.Enabled {
		t.Error("Expected router to be enabled")
	}

	if len(router.rules) == 0 {
		t.Error("Expected router to have rules")
	}
}

func TestNamespaceRouter_RouteRequest(t *testing.T) {
	logger := zap.NewNop()
	router := setupNamespaceRouterForTesting(t, logger)
	tests := createNamespaceRouterTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runNamespaceRouterTest(t, router, tt)
		})
	}
}

func setupNamespaceRouterForTesting(t *testing.T, logger *zap.Logger) *NamespaceRouter {
	t.Helper()

	config := NamespaceRoutingConfig{
		Enabled: true,
		Rules: []NamespaceRoutingRule{
			{
				Pattern:     "docker\\..*",
				Tags:        []string{"docker", "containers"},
				Priority:    1,
				Description: "Docker commands",
			},
			{
				Pattern:     "k8s\\..*",
				Tags:        []string{"kubernetes", "k8s"},
				Priority:    2,
				Description: "Kubernetes commands",
			},
			{
				Pattern:     "tools/.*",
				Tags:        []string{"tools"},
				Priority:    3,
				Description: "Tool commands",
			},
		},
	}

	router, err := NewNamespaceRouter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create namespace router: %v", err)
	}

	return router
}

type namespaceRouterTest struct {
	name         string
	method       string
	expectedTags []string
	expectMatch  bool
}

func createNamespaceRouterTests() []namespaceRouterTest {
	return []namespaceRouterTest{
		{
			name:         "docker command",
			method:       "docker.list",
			expectedTags: []string{"docker", "containers"},
			expectMatch:  true,
		},
		{
			name:         "docker container command",
			method:       "docker.container.start",
			expectedTags: []string{"docker", "containers"},
			expectMatch:  true,
		},
		{
			name:         "kubernetes command",
			method:       "k8s.pods.list",
			expectedTags: []string{"kubernetes", "k8s"},
			expectMatch:  true,
		},
		{
			name:         "kubernetes service command",
			method:       "k8s.services.get",
			expectedTags: []string{"kubernetes", "k8s"},
			expectMatch:  true,
		},
		{
			name:         "tools command",
			method:       "tools/git",
			expectedTags: []string{"tools"},
			expectMatch:  true,
		},
		{
			name:         "tools nested command",
			method:       "tools/git/status",
			expectedTags: []string{"tools"},
			expectMatch:  true,
		},
		{
			name:        "no match",
			method:      "unknown.command",
			expectMatch: false,
		},
		{
			name:        "partial match",
			method:      "dockertest.command",
			expectMatch: false,
		},
	}
}

func runNamespaceRouterTest(t *testing.T, router *NamespaceRouter, tt namespaceRouterTest) {
	t.Helper()

	req := &mcp.Request{Method: tt.method}
	tags, err := router.RouteRequest(req)

	handleRouterError(t, err, tt.method, tt.expectMatch)
	validateRouterResults(t, tags, tt)
}

func handleRouterError(t *testing.T, err error, method string, expectMatch bool) {
	t.Helper()

	if err != nil && expectMatch {
		t.Errorf("Unexpected error for method %s: %v", method, err)
	}
}

func validateRouterResults(t *testing.T, tags []string, tt namespaceRouterTest) {
	t.Helper()

	if tt.expectMatch {
		validateExpectedMatch(t, tags, tt)
	} else {
		validateNoMatch(t, tags, tt.method)
	}
}

func validateExpectedMatch(t *testing.T, tags []string, tt namespaceRouterTest) {
	t.Helper()

	if len(tags) == 0 {
		t.Errorf("Expected tags for method %s, got none", tt.method)

		return
	}

	// Check if expected tags are present
	for _, expectedTag := range tt.expectedTags {
		if !containsTag(tags, expectedTag) {
			t.Errorf("Expected tag %s for method %s, but not found in %v", expectedTag, tt.method, tags)
		}
	}
}

func validateNoMatch(t *testing.T, tags []string, method string) {
	t.Helper()

	if len(tags) > 0 {
		t.Errorf("Expected no tags for method %s, got %v", method, tags)
	}
}

func containsTag(tags []string, expectedTag string) bool {
	for _, tag := range tags {
		if tag == expectedTag {
			return true
		}
	}

	return false
}

func TestNamespaceRouter_DefaultRules(t *testing.T) {
	logger := zap.NewNop()

	// Test with default rules.
	config := NamespaceRoutingConfig{
		Enabled: true,
		Rules:   []NamespaceRoutingRule{}, // Empty rules should get defaults
	}

	router, err := NewNamespaceRouter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create namespace router: %v", err)
	}

	// Test that default rules are applied.
	tests := []struct {
		method      string
		expectMatch bool
	}{
		{"docker.list", true},
		{"k8s.pods", true},
		{"tools/git", true},
		{"fs/read", true},
		{"web/fetch", true},
		{"db/query", true},
		{"random.method", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			req := &mcp.Request{Method: tt.method}

			tags, err := router.RouteRequest(req)
			if err != nil && tt.expectMatch {
				t.Errorf("Unexpected error for method %s: %v", tt.method, err)

				return
			}

			hasMatch := len(tags) > 0

			if hasMatch != tt.expectMatch {
				t.Errorf("Method %s: expected match=%v, got match=%v (tags: %v)",
					tt.method, tt.expectMatch, hasMatch, tags)
			}
		})
	}
}

func TestNamespaceRouter_PriorityOrdering(t *testing.T) {
	logger := zap.NewNop()

	config := NamespaceRoutingConfig{
		Enabled: true,
		Rules: []NamespaceRoutingRule{
			{
				Pattern:     ".*\\.specific",
				Tags:        []string{"specific"},
				Priority:    1, // Higher priority (lower number)
				Description: "Specific commands",
			},
			{
				Pattern:     "test\\..*",
				Tags:        []string{"general"},
				Priority:    2, // Lower priority
				Description: "Test commands",
			},
		},
	}

	router, err := NewNamespaceRouter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create namespace router: %v", err)
	}

	// This method matches both patterns, but should use the higher priority one.
	req := &mcp.Request{Method: "test.specific"}

	tags, err := router.RouteRequest(req)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)

		return
	}

	if len(tags) == 0 {
		t.Error("Expected tags for test.specific")

		return
	}

	// Should get the specific rule (priority 1) instead of the general rule (priority 2).
	found := false

	for _, tag := range tags {
		if tag == "specific" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("Expected 'specific' tag from higher priority rule, got tags: %v", tags)
	}
}

func TestNamespaceRouter_Disabled(t *testing.T) {
	logger := zap.NewNop()

	config := NamespaceRoutingConfig{
		Enabled: false, // Disabled
		Rules: []NamespaceRoutingRule{
			{
				Pattern:     "docker\\..*",
				Tags:        []string{"docker"},
				Priority:    1,
				Description: "Docker commands",
			},
		},
	}

	router, err := NewNamespaceRouter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create namespace router: %v", err)
	}

	// Even with rules defined, should return no tags when disabled.
	req := &mcp.Request{Method: "docker.list"}

	tags, err := router.RouteRequest(req)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)

		return
	}

	if len(tags) > 0 {
		t.Errorf("Expected no tags when router disabled, got %v", tags)
	}
}

func TestNamespaceRouter_InvalidRegex(t *testing.T) {
	logger := zap.NewNop()

	config := NamespaceRoutingConfig{
		Enabled: true,
		Rules: []NamespaceRoutingRule{
			{
				Pattern:     "invalid[regex", // Invalid regex pattern
				Tags:        []string{"invalid"},
				Priority:    1,
				Description: "Invalid regex",
			},
			{
				Pattern:     "^valid\\..*",
				Tags:        []string{"valid"},
				Priority:    2,
				Description: "Valid regex",
			},
		},
	}

	router, err := NewNamespaceRouter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create namespace router: %v", err)
	}

	// Should skip invalid regex rule and use valid one.
	req := &mcp.Request{Method: "valid.command"}

	tags, err := router.RouteRequest(req)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)

		return
	}

	if len(tags) == 0 {
		t.Error("Expected tags from valid regex rule")

		return
	}

	found := false

	for _, tag := range tags {
		if tag == "valid" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("Expected 'valid' tag, got %v", tags)
	}

	// Invalid pattern should not match anything.
	req2 := &mcp.Request{Method: "invalid.command"}

	tags, _ = router.RouteRequest(req2)
	if len(tags) > 0 {
		t.Errorf("Expected no tags from invalid regex, got %v", tags)
	}
}

func TestNamespaceRouter_EmptyMethod(t *testing.T) {
	logger := zap.NewNop()

	config := NamespaceRoutingConfig{
		Enabled: true,
		Rules: []NamespaceRoutingRule{
			{
				Pattern:     ".*",
				Tags:        []string{"catchall"},
				Priority:    1,
				Description: "Catch all",
			},
		},
	}

	router, err := NewNamespaceRouter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create namespace router: %v", err)
	}

	// Empty method should not match anything.
	req := &mcp.Request{Method: ""}
	tags, err := router.RouteRequest(req)
	// Empty method should return an error.
	if err == nil {
		t.Error("Expected error for empty method")

		return
	}

	if len(tags) > 0 {
		t.Errorf("Expected no tags for empty method, got %v", tags)
	}
}

func TestNamespaceRouter_MultipleMatches(t *testing.T) {
	logger := zap.NewNop()
	config := createMultipleMatchesConfig()
	router := setupMultipleMatchesRouter(t, config, logger)
	testMultipleMatchesRouting(t, router)
}

func createMultipleMatchesConfig() NamespaceRoutingConfig {
	return NamespaceRoutingConfig{
		Enabled: true,
		Rules: []NamespaceRoutingRule{
			{
				Pattern:     "test\\..*",
				Tags:        []string{"test", "general"},
				Priority:    1,
				Description: "Test commands",
			},
			{
				Pattern:     ".*\\.command",
				Tags:        []string{"command", "suffix"},
				Priority:    2,
				Description: "Command suffix",
			},
			{
				Pattern:     "test\\.command",
				Tags:        []string{"specific"},
				Priority:    0, // Highest priority
				Description: "Specific test command",
			},
		},
	}
}

func setupMultipleMatchesRouter(t *testing.T, config NamespaceRoutingConfig, logger *zap.Logger) *NamespaceRouter {
	t.Helper()
	router, err := NewNamespaceRouter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create namespace router: %v", err)
	}

	return router
}

func testMultipleMatchesRouting(t *testing.T, router *NamespaceRouter) {
	t.Helper()
	req := &mcp.Request{Method: "test.command"}
	tags, err := router.RouteRequest(req)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)

		return
	}

	if len(tags) == 0 {
		t.Error("Expected tags for test.command")

		return
	}

	verifyPriorityTags(t, tags)
}

func verifyPriorityTags(t *testing.T, tags []string) {
	t.Helper()
	expectedTags := []string{"specific"}

	if len(tags) != len(expectedTags) {
		t.Errorf("Expected %d tags, got %d: %v", len(expectedTags), len(tags), tags)

		return
	}

	for _, expectedTag := range expectedTags {
		if !containsTag(tags, expectedTag) {
			t.Errorf("Expected tag %s not found in %v", expectedTag, tags)
		}
	}
}
