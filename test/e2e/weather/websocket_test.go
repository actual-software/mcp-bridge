// Package weather provides WebSocket MCP endpoint testing
package weather

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWeatherMCPWebSocketEndpoint tests the MCP WebSocket endpoint.
//

func TestWeatherMCPWebSocketEndpoint(t *testing.T) {
	// Connect to the Weather MCP Server via WebSocket
	url := "ws://localhost:8080/mcp"
	t.Logf("Connecting to Weather MCP Server at %s", url)

	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}

	defer func() { _ = conn.Close() }()

	t.Log("✅ WebSocket connection established")

	// Test MCP protocol steps
	testMCPInitialize(t, conn)
	testToolsList(t, conn)
	testWeatherToolCall(t, conn)

	t.Log("🎉 Complete WebSocket MCP protocol test successful!")
	t.Log("✅ Weather MCP Server fully functional with real Open-Meteo API integration")
}

func testMCPInitialize(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	// Test MCP initialize message
	initMessage := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"clientInfo": map[string]interface{}{
				"name":    "weather-e2e-test",
				"version": "1.0.0",
			},
		},
	}

	// Send initialize message
	if err := conn.WriteJSON(initMessage); err != nil {
		t.Fatalf("Failed to send initialize message: %v", err)
	}

	t.Log("📤 Sent MCP initialize message")

	// Read response
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Logf("Warning: Failed to set read deadline: %v", err)
	}

	var response map[string]interface{}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	t.Log("📥 Received MCP initialize response")

	validateInitializeResponse(t, response)
}

func validateInitializeResponse(t *testing.T, response map[string]interface{}) {
	t.Helper()

	if response["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %v", response["jsonrpc"])
	}

	if response["id"] != float64(1) {
		t.Errorf("Expected id 1, got %v", response["id"])
	}

	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected result object in response")
	}

	protocolVersion, ok := result["protocolVersion"].(string)
	if !ok || protocolVersion == "" {
		t.Error("Expected protocolVersion in result")
	}

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Error("Expected serverInfo in result")
	}

	serverName, ok := serverInfo["name"].(string)
	if !ok || serverName != "weather-mcp-server" {
		t.Errorf("Expected server name 'weather-mcp-server', got %v", serverName)
	}

	t.Logf("✅ MCP initialize successful - Protocol: %s, Server: %s", protocolVersion, serverName)
}

func testToolsList(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	toolsListMessage := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	if err := conn.WriteJSON(toolsListMessage); err != nil {
		t.Fatalf("Failed to send tools/list message: %v", err)
	}

	t.Log("📤 Sent tools/list message")

	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Logf("Warning: Failed to set read deadline: %v", err)
	}

	var toolsResponse map[string]interface{}
	if err := conn.ReadJSON(&toolsResponse); err != nil {
		t.Fatalf("Failed to read tools response: %v", err)
	}

	t.Log("📥 Received tools/list response")

	validateToolsResponse(t, toolsResponse)
}

func validateToolsResponse(t *testing.T, toolsResponse map[string]interface{}) {
	t.Helper()

	toolsResult, ok := toolsResponse["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected result object in tools response")
	}

	tools, ok := toolsResult["tools"].([]interface{})
	if !ok {
		t.Fatal("Expected tools array in result")
	}

	if len(tools) == 0 {
		t.Error("Expected at least one tool")
	}

	t.Logf("Available tools: %d", len(tools))

	// Look for weather tool
	var weatherTool map[string]interface{}
	for i, tool := range tools {
		toolMap, ok := tool.(map[string]interface{})
		if !ok {
			t.Logf("Tool %d: not a map", i)

			continue
		}

		t.Logf("Tool %d: %s", i, toolMap["name"])

		if toolMap["name"] == "get_current_weather" {
			weatherTool = toolMap

			break
		}
	}

	if weatherTool == nil {
		t.Fatal("Expected to find get_current_weather tool")
	}

	t.Logf("✅ Found weather tool: %s", weatherTool["description"])
}

func testWeatherToolCall(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	weatherCallMessage := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "get_current_weather",
			"arguments": map[string]interface{}{
				"location":  "San Francisco, CA",
				"latitude":  37.7749,
				"longitude": -122.4194,
			},
		},
	}

	if err := conn.WriteJSON(weatherCallMessage); err != nil {
		t.Fatalf("Failed to send weather tool call: %v", err)
	}

	t.Log("📤 Sent weather tool call for San Francisco")

	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Logf("Warning: Failed to set read deadline: %v", err)
	}

	var weatherResponse map[string]interface{}
	if err := conn.ReadJSON(&weatherResponse); err != nil {
		t.Fatalf("Failed to read weather response: %v", err)
	}

	t.Log("📥 Received weather tool response")

	validateWeatherResponse(t, weatherResponse)
}

func validateWeatherResponse(t *testing.T, weatherResponse map[string]interface{}) {
	t.Helper()

	weatherResult, ok := weatherResponse["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected result object in weather response")
	}

	content, ok := weatherResult["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array in weather result")
	}

	firstContent, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatal("Expected content object")
	}

	text, ok := firstContent["text"].(string)
	if !ok || text == "" {
		t.Fatal("Expected text content in weather result")
	}

	t.Logf("✅ Weather API call successful - received weather data: %s", text[:100]+"...")

	// Validate the weather data contains expected information
	if !contains(text, "Temperature") {
		t.Error("Weather response doesn't contain temperature information")
	}
}
