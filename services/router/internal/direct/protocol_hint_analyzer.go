package direct

import (
	"net/url"
	"strings"
)

const (
	// SchemeWebSocket represents the WebSocket scheme.
	SchemeWebSocket = "ws"
	// SchemeWebSocketSecure represents the secure WebSocket scheme.
	SchemeWebSocketSecure = "wss"
	// SchemeStdio represents the stdio scheme.
	SchemeStdio = "stdio"
)

// ProtocolHintAnalyzer analyzes URLs to provide protocol hints.
type ProtocolHintAnalyzer struct {
	manager *DirectClientManager
}

// CreateProtocolHintAnalyzer creates a new hint analyzer.
func CreateProtocolHintAnalyzer(manager *DirectClientManager) *ProtocolHintAnalyzer {
	return &ProtocolHintAnalyzer{
		manager: manager,
	}
}

// GetHints analyzes a server URL and returns protocol hints.
func (a *ProtocolHintAnalyzer) GetHints(serverURL string) []ClientType {
	// Check stdio first (special case).
	if hints := a.checkStdioScheme(serverURL); hints != nil {
		return hints
	}

	// Parse URL for scheme-based hints.
	if hints := a.analyzeURLScheme(serverURL); hints != nil {
		return hints
	}

	// Check for command patterns.
	if hints := a.checkCommandPattern(serverURL); hints != nil {
		return hints
	}

	// Return default order.
	return a.getDefaultOrder()
}

func (a *ProtocolHintAnalyzer) checkStdioScheme(serverURL string) []ClientType {
	if strings.HasPrefix(serverURL, "stdio://") {
		return []ClientType{ClientTypeStdio}
	}

	return nil
}

func (a *ProtocolHintAnalyzer) analyzeURLScheme(serverURL string) []ClientType {
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		return nil
	}

	analyzer := &SchemeAnalyzer{
		url: parsedURL,
	}

	return analyzer.Analyze()
}

func (a *ProtocolHintAnalyzer) checkCommandPattern(serverURL string) []ClientType {
	if strings.Contains(serverURL, "://") {
		return nil
	}

	// Looks like a command if it has fields.
	if len(strings.Fields(serverURL)) > 0 {
		return []ClientType{ClientTypeStdio}
	}

	return nil
}

func (a *ProtocolHintAnalyzer) getDefaultOrder() []ClientType {
	hints := make([]ClientType, 0, len(a.manager.config.AutoDetection.PreferredOrder))
	for _, protocolStr := range a.manager.config.AutoDetection.PreferredOrder {
		hints = append(hints, ClientType(protocolStr))
	}

	return hints
}

// SchemeAnalyzer analyzes URL schemes for protocol hints.
type SchemeAnalyzer struct {
	url *url.URL
}

// Analyze analyzes the URL scheme.
func (s *SchemeAnalyzer) Analyze() []ClientType {
	switch s.url.Scheme {
	case SchemeWebSocket, SchemeWebSocketSecure:
		return s.websocketHints()
	case SchemeHTTP, SchemeHTTPS:
		return s.httpHints()
	case SchemeStdio:
		return s.stdioHints()
	default:
		return nil
	}
}

func (s *SchemeAnalyzer) websocketHints() []ClientType {
	return []ClientType{ClientTypeWebSocket}
}

func (s *SchemeAnalyzer) httpHints() []ClientType {
	if s.hasSSEPattern() {
		return []ClientType{ClientTypeSSE, ClientTypeHTTP}
	}

	return []ClientType{ClientTypeHTTP, ClientTypeSSE}
}

func (s *SchemeAnalyzer) hasSSEPattern() bool {
	path := s.url.Path

	return strings.Contains(path, "sse") ||
		strings.Contains(path, "events") ||
		strings.Contains(path, "stream")
}

func (s *SchemeAnalyzer) stdioHints() []ClientType {
	return []ClientType{ClientTypeStdio}
}
