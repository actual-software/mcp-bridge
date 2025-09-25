package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// Default timeouts for MCP operations.
	defaultTimeout = 10 * time.Second
	longTimeout    = 30 * time.Second
)

// MCP protocol types and helpers

// MCPRequest represents an MCP JSON-RPC request.
type MCPRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      string      `json:"id"`
}

// MCPResponse represents an MCP JSON-RPC response.
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
	ID      string      `json:"id"`
}

// MCPError represents an MCP error.
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// MCPClient provides high-level MCP protocol interactions.
type MCPClient struct {
	router       *RouterController
	requestID    int
	requestMu    sync.Mutex // Protect requestID for concurrent access
	initialized  bool
	capabilities map[string]interface{}
	tools        []Tool
}

// NewMCPClient creates a new MCP client.
func NewMCPClient(router *RouterController) *MCPClient {
	return &MCPClient{
		router:       router,
		requestID:    1,
		capabilities: make(map[string]interface{}),
		tools:        make([]Tool, 0),
	}
}

// Initialize sends an MCP initialize request.
func (c *MCPClient) Initialize() (*MCPResponse, error) {
	fmt.Printf("[DEBUG] MCP Client: Starting initialization...\n")

	req := MCPRequest{
		JSONRPC: "2.0",
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "1.0",
			"capabilities": map[string]interface{}{
				"tools": true,
			},
			"clientInfo": map[string]interface{}{
				"name":    "mcp-e2e-test-client",
				"version": "1.0.0",
			},
		},
		ID: c.nextRequestID(),
	}

	fmt.Printf("[DEBUG] MCP Client: Sending initialize request with ID: %s\n", req.ID)
	fmt.Printf("[DEBUG] MCP Client: Request: %+v\n", req)

	respData, err := c.router.SendRequestAndWait(req, defaultTimeout)
	if err != nil {
		fmt.Printf("[DEBUG] MCP Client: Initialize request failed: %v\n", err)

		return nil, fmt.Errorf("initialize request failed: %w", err)
	}

	fmt.Printf("[DEBUG] MCP Client: Received response data: %s\n", string(respData))

	var resp MCPResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse initialize response: %w", err)
	}

	if resp.Error != nil {
		return &resp, fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	// Parse capabilities
	if result, ok := resp.Result.(map[string]interface{}); ok {
		if caps, ok := result["capabilities"].(map[string]interface{}); ok {
			c.capabilities = caps
		}
	}

	c.initialized = true

	return &resp, nil
}

// ListTools requests the list of available tools.
func (c *MCPClient) ListTools() ([]Tool, error) {
	if !c.initialized {
		return nil, errors.New("client not initialized")
	}

	req := MCPRequest{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      c.nextRequestID(),
	}

	resp, err := c.sendToolsListRequest(req)
	if err != nil {
		return nil, err
	}

	tools, err := c.parseToolsResponse(resp)
	if err != nil {
		return nil, err
	}

	c.tools = tools

	return tools, nil
}

// sendToolsListRequest sends the tools/list request and returns the response.
func (c *MCPClient) sendToolsListRequest(req MCPRequest) (*MCPResponse, error) {
	respData, err := c.router.SendRequestAndWait(req, defaultTimeout)
	if err != nil {
		return nil, fmt.Errorf("tools/list request failed: %w", err)
	}

	var resp MCPResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tools/list response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error: %s", resp.Error.Message)
	}

	return &resp, nil
}

// parseToolsResponse parses the tools from the response.
func (c *MCPClient) parseToolsResponse(resp *MCPResponse) ([]Tool, error) {
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return nil, nil
	}

	toolsArray, ok := result["tools"].([]interface{})
	if !ok {
		return nil, nil
	}

	tools := make([]Tool, 0, len(toolsArray))

	for _, toolData := range toolsArray {
		tool, err := c.parseToolData(toolData)
		if err != nil {
			continue // Skip invalid tools
		}

		tools = append(tools, tool)
	}

	return tools, nil
}

// parseToolData parses individual tool data.
func (c *MCPClient) parseToolData(toolData interface{}) (Tool, error) {
	toolMap, ok := toolData.(map[string]interface{})
	if !ok {
		return Tool{}, errors.New("invalid tool data")
	}

	tool := Tool{}
	if name, ok := toolMap["name"].(string); ok {
		tool.Name = name
	}

	if desc, ok := toolMap["description"].(string); ok {
		tool.Description = desc
	}

	tool.InputSchema = toolMap["inputSchema"]

	return tool, nil
}

// CallTool executes a specific tool with arguments.
func (c *MCPClient) CallTool(toolName string, arguments map[string]interface{}) (*MCPResponse, error) {
	if !c.initialized {
		return nil, errors.New("client not initialized")
	}

	req := MCPRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
		ID: c.nextRequestID(),
	}

	respData, err := c.router.SendRequestAndWait(req, longTimeout)
	if err != nil {
		return nil, fmt.Errorf("tools/call request failed: %w", err)
	}

	var resp MCPResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse tools/call response: %w", err)
	}

	return &resp, nil
}

// GetCapabilities returns the server capabilities.
func (c *MCPClient) GetCapabilities() map[string]interface{} {
	return c.capabilities
}

// GetTools returns the cached list of tools.
func (c *MCPClient) GetTools() []Tool {
	return c.tools
}

// IsInitialized returns true if the client has been initialized.
func (c *MCPClient) IsInitialized() bool {
	return c.initialized
}

// nextRequestID generates the next request ID (thread-safe).
func (c *MCPClient) nextRequestID() string {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()

	id := fmt.Sprintf("req-%d", c.requestID)
	c.requestID++

	return id
}

// Helper functions for test assertions

// AssertValidMCPResponse validates that a response is a valid MCP response.
func AssertValidMCPResponse(resp *MCPResponse) error {
	if resp.JSONRPC != "2.0" {
		return fmt.Errorf("invalid JSONRPC version: %s", resp.JSONRPC)
	}

	if resp.ID == "" {
		return errors.New("missing response ID")
	}

	if resp.Result == nil && resp.Error == nil {
		return errors.New("response must have either result or error")
	}

	if resp.Result != nil && resp.Error != nil {
		return errors.New("response cannot have both result and error")
	}

	return nil
}

// AssertToolCallSuccess validates that a tool call was successful.
func AssertToolCallSuccess(resp *MCPResponse) error {
	if err := AssertValidMCPResponse(resp); err != nil {
		return err
	}

	if err := validateToolCallResponse(resp); err != nil {
		return err
	}

	return validateContentBlocks(resp.Result)
}

// validateToolCallResponse validates basic tool call response structure.
func validateToolCallResponse(resp *MCPResponse) error {
	if resp.Error != nil {
		return fmt.Errorf("tool call failed: %s", resp.Error.Message)
	}

	if resp.Result == nil {
		return errors.New("tool call result is nil")
	}

	return nil
}

// validateContentBlocks validates the content blocks in the response.
func validateContentBlocks(result interface{}) error {
	// Validate result is an array of content blocks
	resultArray, ok := result.([]interface{})
	if !ok {
		return errors.New("tool call result must be an array")
	}

	if len(resultArray) == 0 {
		return errors.New("tool call result array is empty")
	}

	// Validate each content block
	for i, block := range resultArray {
		if err := validateSingleContentBlock(i, block); err != nil {
			return err
		}
	}

	return nil
}

// validateSingleContentBlock validates a single content block.
func validateSingleContentBlock(index int, block interface{}) error {
	blockMap, ok := block.(map[string]interface{})
	if !ok {
		return fmt.Errorf("content block %d is not an object", index)
	}

	blockType, ok := blockMap["type"].(string)
	if !ok {
		return fmt.Errorf("content block %d missing type", index)
	}

	return validateContentBlockByType(index, blockType, blockMap)
}

// validateContentBlockByType validates content block based on its type.
func validateContentBlockByType(index int, blockType string, blockMap map[string]interface{}) error {
	switch blockType {
	case "text":
		if _, ok := blockMap["text"].(string); !ok {
			return fmt.Errorf("text content block %d missing text field", index)
		}
	case "image":
		if _, ok := blockMap["data"].(string); !ok {
			return fmt.Errorf("image content block %d missing data field", index)
		}
	case "resource":
		if _, ok := blockMap["resource"]; !ok {
			return fmt.Errorf("resource content block %d missing resource field", index)
		}
	default:
		return fmt.Errorf("unknown content block type: %s", blockType)
	}

	return nil
}

// AssertToolCallError validates that a tool call failed with expected error.
func AssertToolCallError(resp *MCPResponse, expectedCode int) error {
	if err := AssertValidMCPResponse(resp); err != nil {
		return err
	}

	if resp.Error == nil {
		return errors.New("expected tool call to fail but it succeeded")
	}

	if resp.Result != nil {
		return errors.New("tool call error response should not have result")
	}

	if resp.Error.Code != expectedCode {
		return fmt.Errorf("expected error code %d but got %d", expectedCode, resp.Error.Code)
	}

	if resp.Error.Message == "" {
		return errors.New("error message is empty")
	}

	return nil
}

// ExtractTextFromToolResponse extracts text content from a tool response.
func ExtractTextFromToolResponse(resp *MCPResponse) (string, error) {
	if resp.Error != nil {
		return "", errors.New("cannot extract text from error response")
	}

	resultArray, err := validateResponseResult(resp.Result)
	if err != nil {
		return "", err
	}

	textParts := extractTextPartsFromBlocks(resultArray)

	if len(textParts) == 0 {
		return "", errors.New("no text content found in response")
	}

	return joinTextParts(textParts), nil
}

// validateResponseResult validates and extracts the result array.
func validateResponseResult(result interface{}) ([]interface{}, error) {
	resultArray, ok := result.([]interface{})
	if !ok {
		return nil, errors.New("result is not an array")
	}

	return resultArray, nil
}

// extractTextPartsFromBlocks extracts text content from response blocks.
func extractTextPartsFromBlocks(resultArray []interface{}) []string {
	var textParts []string

	for _, block := range resultArray {
		if textContent := extractTextFromBlock(block); textContent != "" {
			textParts = append(textParts, textContent)
		}
	}

	return textParts
}

// extractTextFromBlock extracts text from a single block.
func extractTextFromBlock(block interface{}) string {
	blockMap, ok := block.(map[string]interface{})
	if !ok {
		return ""
	}

	if blockType, ok := blockMap["type"].(string); ok && blockType == "text" {
		if text, ok := blockMap["text"].(string); ok {
			return text
		}
	}

	return ""
}

// joinTextParts joins text parts with spaces.
func joinTextParts(textParts []string) string {
	if len(textParts) == 1 {
		return textParts[0]
	}

	result := textParts[0]
	for _, part := range textParts[1:] {
		result += " " + part
	}

	return result
}

// CreateToolCallRequest creates a properly formatted tool call request.
func CreateToolCallRequest(toolName string, arguments map[string]interface{}) MCPRequest {
	return MCPRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
		ID: fmt.Sprintf("test-%d", time.Now().UnixNano()),
	}
}

// CreateInitializeRequest creates a properly formatted initialize request.
func CreateInitializeRequest() MCPRequest {
	return MCPRequest{
		JSONRPC: "2.0",
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "1.0",
			"capabilities": map[string]interface{}{
				"tools": true,
			},
			"clientInfo": map[string]interface{}{
				"name":    "mcp-e2e-test-client",
				"version": "1.0.0",
			},
		},
		ID: fmt.Sprintf("init-%d", time.Now().UnixNano()),
	}
}

// CreateListToolsRequest creates a properly formatted tools/list request.
func CreateListToolsRequest() MCPRequest {
	return MCPRequest{
		JSONRPC: "2.0",
		Method:  "tools/list",
		ID:      fmt.Sprintf("list-%d", time.Now().UnixNano()),
	}
}
