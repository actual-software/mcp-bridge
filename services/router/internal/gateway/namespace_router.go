package gateway

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

const (
	// StandardRoutingPriority is the standard priority for namespace routing rules.
	StandardRoutingPriority = 2
	// HighRoutingPriority is the high priority for specialized namespace routing rules.
	HighRoutingPriority = 3
)

// NamespaceRoutingRule defines a rule for routing MCP methods to specific endpoints.
type NamespaceRoutingRule struct {
	// Pattern is a regular expression or glob pattern to match method names.
	Pattern string `mapstructure:"pattern"`

	// Tags are the endpoint tags that should handle methods matching this pattern.
	Tags []string `mapstructure:"tags"`

	// Priority determines the order of rule evaluation (lower number = higher priority).
	Priority int `mapstructure:"priority"`

	// Description provides human-readable documentation for this rule.
	Description string `mapstructure:"description"`

	// Compiled regex for efficient matching.
	regex *regexp.Regexp
}

// NamespaceRoutingConfig defines configuration for namespace-based routing.
type NamespaceRoutingConfig struct {
	Enabled bool                   `mapstructure:"enabled"`
	Rules   []NamespaceRoutingRule `mapstructure:"rules"`
}

// DefaultNamespaceRoutingRules returns a set of default routing rules for common MCP patterns.
func DefaultNamespaceRoutingRules() []NamespaceRoutingRule {
	var rules []NamespaceRoutingRule

	// Add system rules
	rules = append(rules, getSystemRoutingRules()...)

	// Add MCP capability rules
	rules = append(rules, getMCPCapabilityRules()...)

	// Add dotted namespace rules
	rules = append(rules, getDottedNamespaceRules()...)

	// Add slash namespace rules
	rules = append(rules, getSlashNamespaceRules()...)

	return rules
}

// getSystemRoutingRules returns system-level routing rules.
func getSystemRoutingRules() []NamespaceRoutingRule {
	return []NamespaceRoutingRule{
		{
			Pattern:     `^initialize$`,
			Tags:        []string{"system", "default"},
			Priority:    1,
			Description: "Route initialization requests to system endpoints",
		},
	}
}

// getMCPCapabilityRules returns MCP capability-based routing rules.
func getMCPCapabilityRules() []NamespaceRoutingRule {
	return []NamespaceRoutingRule{
		{
			Pattern:     `^tools/.*`,
			Tags:        []string{"tools", "default"},
			Priority:    StandardRoutingPriority,
			Description: "Route tool-related requests to tool-capable endpoints",
		},
		{
			Pattern:     `^prompts/.*`,
			Tags:        []string{"prompts", "default"},
			Priority:    StandardRoutingPriority,
			Description: "Route prompt-related requests to prompt-capable endpoints",
		},
		{
			Pattern:     `^resources/.*`,
			Tags:        []string{"resources", "default"},
			Priority:    StandardRoutingPriority,
			Description: "Route resource-related requests to resource-capable endpoints",
		},
	}
}

// getDottedNamespaceRules returns dotted notation namespace rules.
func getDottedNamespaceRules() []NamespaceRoutingRule {
	return []NamespaceRoutingRule{
		{
			Pattern:     `^docker\..*`,
			Tags:        []string{"docker", "default"},
			Priority:    HighRoutingPriority,
			Description: "Route docker namespace methods to docker-capable endpoints",
		},
		{
			Pattern:     `^k8s\..*`,
			Tags:        []string{"k8s", "default"},
			Priority:    HighRoutingPriority,
			Description: "Route k8s namespace methods to k8s-capable endpoints",
		},
		{
			Pattern:     `^fs\..*`,
			Tags:        []string{"fs", "default"},
			Priority:    HighRoutingPriority,
			Description: "Route filesystem namespace methods to fs-capable endpoints",
		},
		{
			Pattern:     `^web\..*`,
			Tags:        []string{"web", "default"},
			Priority:    HighRoutingPriority,
			Description: "Route web namespace methods to web-capable endpoints",
		},
		{
			Pattern:     `^db\..*`,
			Tags:        []string{"db", "default"},
			Priority:    HighRoutingPriority,
			Description: "Route database namespace methods to db-capable endpoints",
		},
	}
}

// getSlashNamespaceRules returns slash notation namespace rules  .
func getSlashNamespaceRules() []NamespaceRoutingRule {
	return []NamespaceRoutingRule{
		{
			Pattern:     `^fs/.*`,
			Tags:        []string{"fs", "default"},
			Priority:    HighRoutingPriority,
			Description: "Route filesystem methods to fs-capable endpoints",
		},
		{
			Pattern:     `^web/.*`,
			Tags:        []string{"web", "default"},
			Priority:    HighRoutingPriority,
			Description: "Route web methods to web-capable endpoints",
		},
		{
			Pattern:     `^db/.*`,
			Tags:        []string{"db", "default"},
			Priority:    HighRoutingPriority,
			Description: "Route database methods to db-capable endpoints",
		},
	}
}

// NamespaceRouter handles routing of MCP requests based on method namespaces.
type NamespaceRouter struct {
	config NamespaceRoutingConfig
	rules  []NamespaceRoutingRule
	logger *zap.Logger
}

// NewNamespaceRouter creates a new namespace router with the given configuration.
func NewNamespaceRouter(config NamespaceRoutingConfig, logger *zap.Logger) (*NamespaceRouter, error) {
	router := &NamespaceRouter{
		config: config,
		logger: logger,
	}

	// Use provided rules or defaults.
	rules := config.Rules
	if len(rules) == 0 {
		rules = DefaultNamespaceRoutingRules()
		router.logger.Info("Using default namespace routing rules", zap.Int("rule_count", len(rules)))
	}

	// Compile regex patterns and sort by priority.
	router.rules = make([]NamespaceRoutingRule, 0, len(rules))

	for _, rule := range rules {
		compiledRule := rule

		if rule.Pattern != "" {
			regex, err := regexp.Compile(rule.Pattern)
			if err != nil {
				router.logger.Error("Failed to compile routing rule pattern",
					zap.String("pattern", rule.Pattern),
					zap.Error(err))

				continue
			}

			compiledRule.regex = regex
		}

		router.rules = append(router.rules, compiledRule)
	}

	// Sort rules by priority (lower number = higher priority).
	for i := 0; i < len(router.rules)-1; i++ {
		for j := i + 1; j < len(router.rules); j++ {
			if router.rules[i].Priority > router.rules[j].Priority {
				router.rules[i], router.rules[j] = router.rules[j], router.rules[i]
			}
		}
	}

	router.logger.Info("Namespace router initialized",
		zap.Int("rules", len(router.rules)),
		zap.Bool("enabled", config.Enabled))

	return router, nil
}

// RouteRequest determines which endpoint tags should handle the given MCP request.
func (nr *NamespaceRouter) RouteRequest(req *mcp.Request) ([]string, error) {
	if !nr.config.Enabled {
		return []string{}, nil
	}

	if req.Method == "" {
		return []string{}, errors.New("empty method in request")
	}

	// Apply routing rules in priority order.
	for _, rule := range nr.rules {
		if nr.matchesRule(req.Method, rule) {
			tags := nr.expandTags(req.Method, rule)
			nr.logger.Debug("Request routed by rule",
				zap.String("method", req.Method),
				zap.String("pattern", rule.Pattern),
				zap.Strings("tags", tags),
				zap.String("description", rule.Description))

			return tags, nil
		}
	}

	// No rules matched - return empty tags.
	nr.logger.Debug("No routing rule matched request",
		zap.String("method", req.Method))

	return []string{}, nil
}

// matchesRule checks if a method matches a routing rule.
func (nr *NamespaceRouter) matchesRule(method string, rule NamespaceRoutingRule) bool {
	if rule.regex != nil {
		return rule.regex.MatchString(method)
	}

	// Fallback to simple string matching.
	return strings.Contains(method, rule.Pattern)
}

// expandTags expands tag templates with captured groups from regex matches.
func (nr *NamespaceRouter) expandTags(method string, rule NamespaceRoutingRule) []string {
	if rule.regex == nil {
		return rule.Tags
	}

	matches := rule.regex.FindStringSubmatch(method)
	if len(matches) <= 1 {
		return rule.Tags
	}

	expandedTags := make([]string, 0, len(rule.Tags))

	for _, tag := range rule.Tags {
		expandedTag := tag

		// Replace $1, $2, etc. with captured groups
		for i := 1; i < len(matches); i++ {
			placeholder := fmt.Sprintf("$%d", i)
			if strings.Contains(expandedTag, placeholder) {
				expandedTag = strings.ReplaceAll(expandedTag, placeholder, matches[i])
			}
		}

		expandedTags = append(expandedTags, expandedTag)
	}

	return expandedTags
}

// ExtractNamespace extracts the namespace from an MCP method name.
func (nr *NamespaceRouter) ExtractNamespace(method string) string {
	if method == "" {
		return "default"
	}

	// Handle special system methods.
	if method == "initialize" || strings.HasPrefix(method, "tools/") {
		return "system"
	}

	// Extract namespace from dotted notation (e.g., "docker.ps" -> "docker")
	if idx := strings.Index(method, "."); idx > 0 {
		return method[:idx]
	}

	// Extract namespace from slash notation (e.g., "k8s/get-pods" -> "k8s")
	if idx := strings.Index(method, "/"); idx > 0 {
		return method[:idx]
	}

	return "default"
}

// GetRoutingStats returns statistics about routing rule usage.
func (nr *NamespaceRouter) GetRoutingStats() map[string]interface{} {
	stats := map[string]interface{}{
		"enabled":    nr.config.Enabled,
		"rule_count": len(nr.rules),
		"rules":      make([]map[string]interface{}, 0, len(nr.rules)),
	}

	for _, rule := range nr.rules {
		ruleStats := map[string]interface{}{
			"pattern":     rule.Pattern,
			"tags":        rule.Tags,
			"priority":    rule.Priority,
			"description": rule.Description,
		}

		rulesSlice, ok := stats["rules"].([]map[string]interface{})
		if !ok {
			rulesSlice = make([]map[string]interface{}, 0)
		}

		stats["rules"] = append(rulesSlice, ruleStats)
	}

	return stats
}

// AddRule adds a new routing rule with the specified priority.
func (nr *NamespaceRouter) AddRule(rule NamespaceRoutingRule) error {
	// Compile regex pattern.
	if rule.Pattern != "" {
		regex, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return fmt.Errorf("failed to compile pattern %s: %w", rule.Pattern, err)
		}

		rule.regex = regex
	}

	// Insert rule in priority order.
	inserted := false

	for i, existingRule := range nr.rules {
		if rule.Priority < existingRule.Priority {
			// Insert at position i.
			nr.rules = append(nr.rules[:i], append([]NamespaceRoutingRule{rule}, nr.rules[i:]...)...)
			inserted = true

			break
		}
	}

	if !inserted {
		nr.rules = append(nr.rules, rule)
	}

	nr.logger.Info("Added namespace routing rule",
		zap.String("pattern", rule.Pattern),
		zap.Strings("tags", rule.Tags),
		zap.Int("priority", rule.Priority))

	return nil
}

// RemoveRule removes routing rules matching the given pattern.
func (nr *NamespaceRouter) RemoveRule(pattern string) int {
	originalCount := len(nr.rules)
	newRules := make([]NamespaceRoutingRule, 0, len(nr.rules))

	for _, rule := range nr.rules {
		if rule.Pattern != pattern {
			newRules = append(newRules, rule)
		}
	}

	nr.rules = newRules
	removed := originalCount - len(nr.rules)

	if removed > 0 {
		nr.logger.Info("Removed namespace routing rules",
			zap.String("pattern", pattern),
			zap.Int("removed_count", removed))
	}

	return removed
}

// ValidateRequest validates that a request can be routed properly.
func (nr *NamespaceRouter) ValidateRequest(req *mcp.Request) error {
	if req == nil {
		return errors.New("request is nil")
	}

	if req.Method == "" {
		return errors.New("request method is empty")
	}

	// Check if any rule would match this request.
	for _, rule := range nr.rules {
		if nr.matchesRule(req.Method, rule) {
			return nil
		}
	}

	return fmt.Errorf("no routing rule matches method: %s", req.Method)
}
