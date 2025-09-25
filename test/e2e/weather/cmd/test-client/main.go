
// Package weather provides a test client for manual testing
package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"

	"github.com/gorilla/websocket"
)

const (
	// JSON-RPC request IDs.
	toolsListRequestID = 2
	weatherRequestID   = 3
	// New York City coordinates for testing.
	newYorkLatitude  = 40.7128
	newYorkLongitude = -74.0060
)


const minRequiredArgs = 2

func main() {
	if len(os.Args) < minRequiredArgs {
		fmt.Println("Usage: go run test_client.go <ws_url>")
		fmt.Println("Example: go run test_client.go ws://localhost:8081/mcp")
		os.Exit(1)
	}

	wsURL := os.Args[1]

	// Parse URL
	u, err := url.Parse(wsURL)
	if err != nil {
		log.Fatal("Invalid URL:", err)
	}

	if err := runWeatherClient(u.String()); err != nil {
		log.Fatal("Error running weather client:", err)
	}
}

func runWeatherClient(wsURL string) error {
	fmt.Printf("Connecting to %s...\n", wsURL)

	// Connect to WebSocket
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	defer func() { _ = c.Close() }()

	fmt.Println("Connected! Sending initialize request...")

	// Send initialize request
	if err := sendInitializeRequest(c); err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}

	// List tools
	if err := listTools(c); err != nil {
		return fmt.Errorf("list tools failed: %w", err)
	}

	// Get weather for New York
	if err := getWeatherData(c); err != nil {
		return fmt.Errorf("get weather data failed: %w", err)
	}

	fmt.Println("\nTest completed successfully! Press Ctrl+C to exit.")

	// Wait for interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	<-interrupt

	return nil
}

func sendInitializeRequest(c *websocket.Conn) error {
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
		"id": 1,
	}

	if err := c.WriteJSON(initReq); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	// Read initialize response
	var initResp map[string]interface{}
	if err := c.ReadJSON(&initResp); err != nil {
		return fmt.Errorf("read: %w", err)
	}

	fmt.Printf("Initialize response: %v\n", initResp)

	return nil
}

func listTools(c *websocket.Conn) error {
	fmt.Println("Listing available tools...")

	toolsReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/list",
		"params":  map[string]interface{}{},
		"id":      toolsListRequestID,
	}

	if err := c.WriteJSON(toolsReq); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	var toolsResp map[string]interface{}
	if err := c.ReadJSON(&toolsResp); err != nil {
		return fmt.Errorf("read: %w", err)
	}

	fmt.Printf("Tools response: %v\n", toolsResp)

	return nil
}

func getWeatherData(c *websocket.Conn) error {
	fmt.Println("Getting weather for New York...")

	weatherReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "get_current_weather",
			"arguments": map[string]interface{}{
				"latitude":  newYorkLatitude,
				"longitude": newYorkLongitude,
			},
		},
		"id": weatherRequestID,
	}

	if err := c.WriteJSON(weatherReq); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	var weatherResp map[string]interface{}
	if err := c.ReadJSON(&weatherResp); err != nil {
		return fmt.Errorf("read: %w", err)
	}

	fmt.Printf("Weather response: %v\n", weatherResp)

	// Parse and display weather data nicely
	displayWeatherData(weatherResp)

	return nil
}

func displayWeatherData(weatherResp map[string]interface{}) {
	result, ok := weatherResp["result"].(map[string]interface{})
	if !ok {
		return
	}

	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		return
	}

	item, ok := content[0].(map[string]interface{})
	if !ok {
		return
	}

	text, ok := item["text"].(string)
	if !ok {
		return
	}

	fmt.Println("\n🌤️  Weather Data:")
	fmt.Println(text)
}
