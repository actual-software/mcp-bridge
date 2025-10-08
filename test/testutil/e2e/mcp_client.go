// Package e2e provides end-to-end test utilities for MCP protocol testing.
package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

const (
	// DefaultToolCallTimeout is the default timeout for tool calls.
	DefaultToolCallTimeout = 30 * time.Second
	// DefaultRequestTimeout is the default timeout for request/response operations.
	DefaultRequestTimeout = 10 * time.Second
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

// RouterInterface defines the interface that router controllers must implement.
type RouterInterface interface {
	SendRequestAndWait(req MCPRequest, timeout time.Duration) ([]byte, error)
}

// MCPClient provides client functionality for MCP protocol testing.
type MCPClient struct {
	router       RouterInterface
	logger       *zap.Logger
	requestID    int
	initialized  bool
	capabilities map[string]interface{}
	tools        []Tool
}

// NewMCPClient creates a new MCP client.
func NewMCPClient(router RouterInterface, logger *zap.Logger) *MCPClient {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &MCPClient{
		router:       router,
		logger:       logger,
		requestID:    0,
		initialized:  false,
		capabilities: make(map[string]interface{}),
		tools:        make([]Tool, 0),
	}
}

// Initialize sends an MCP initialize request.
func (c *MCPClient) Initialize() (*MCPResponse, error) {
	c.logger.Debug("Starting MCP client initialization")

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

	c.logger.Debug("Sending initialize request",
		zap.Any("request_id", req.ID),
		zap.Any("request", req))

	respData, err := c.router.SendRequestAndWait(req, DefaultRequestTimeout)
	if err != nil {
		c.logger.Debug("Initialize request failed", zap.Error(err))

		return nil, fmt.Errorf("initialize request failed: %w", err)
	}

	c.logger.Debug("Received initialize response", zap.String("response_data", string(respData)))

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

	resp, err := c.sendToolsListRequest()
	if err != nil {
		return nil, err
	}

	tools := c.parseToolsResponse(resp)
	c.tools = tools

	return tools, nil
}

func (c *MCPClient) sendToolsListRequest() (*MCPResponse, error) {
	req := MCPRequest{
		JSONRPC: "2.0",
		Method:  "tools/list",
		Params:  nil, // No parameters needed for tools/list
		ID:      c.nextRequestID(),
	}

	respData, err := c.router.SendRequestAndWait(req, DefaultRequestTimeout)
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

func (c *MCPClient) parseToolsResponse(resp *MCPResponse) []Tool {
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return []Tool{}
	}

	toolsArray, ok := result["tools"].([]interface{})
	if !ok {
		return []Tool{}
	}

	tools := make([]Tool, 0, len(toolsArray))
	for _, toolData := range toolsArray {
		if tool := parseToolData(toolData); tool != nil {
			tools = append(tools, *tool)
		}
	}

	return tools
}

func parseToolData(toolData interface{}) *Tool {
	toolMap, ok := toolData.(map[string]interface{})
	if !ok {
		return nil
	}

	tool := &Tool{
		Name:        "",
		Description: "",
		InputSchema: nil,
	}

	if name, ok := toolMap["name"].(string); ok {
		tool.Name = name
	}

	if desc, ok := toolMap["description"].(string); ok {
		tool.Description = desc
	}

	tool.InputSchema = toolMap["inputSchema"]

	return tool
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

	respData, err := c.router.SendRequestAndWait(req, DefaultToolCallTimeout)
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

// nextRequestID generates the next request ID.
func (c *MCPClient) nextRequestID() string {
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

	resultArray, err := getResultArray(resp)
	if err != nil {
		return err
	}

	return validateContentBlocks(resultArray)
}

func validateToolCallResponse(resp *MCPResponse) error {
	if resp.Error != nil {
		return fmt.Errorf("tool call failed: %s", resp.Error.Message)
	}

	if resp.Result == nil {
		return errors.New("tool call result is nil")
	}

	return nil
}

func getResultArray(resp *MCPResponse) ([]interface{}, error) {
	// MCP tool responses have format: {"result": {"content": [...]}}
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return nil, errors.New("tool call result must be a map")
	}

	contentArray, ok := resultMap["content"].([]interface{})
	if !ok {
		return nil, errors.New("tool call result must have a 'content' array")
	}

	if len(contentArray) == 0 {
		return nil, errors.New("tool call result content array is empty")
	}

	return contentArray, nil
}

func validateContentBlocks(resultArray []interface{}) error {
	for i, block := range resultArray {
		if err := validateSingleContentBlock(i, block); err != nil {
			return err
		}
	}

	return nil
}

func validateSingleContentBlock(index int, block interface{}) error {
	blockMap, ok := block.(map[string]interface{})
	if !ok {
		return fmt.Errorf("content block %d is not an object", index)
	}

	blockType, ok := blockMap["type"].(string)
	if !ok {
		return fmt.Errorf("content block %d missing type", index)
	}

	return validateBlockTypeFields(index, blockType, blockMap)
}

func validateBlockTypeFields(index int, blockType string, blockMap map[string]interface{}) error {
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
//

func ExtractTextFromToolResponse(resp *MCPResponse) (string, error) {
	if resp.Error != nil {
		return "", errors.New("cannot extract text from error response")
	}

	// MCP tool responses have format: {"result": {"content": [...]}}
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		return "", errors.New("result is not a map")
	}

	contentArray, ok := resultMap["content"].([]interface{})
	if !ok {
		return "", errors.New("result must have a 'content' array")
	}

	textParts := extractTextParts(contentArray)

	if len(textParts) == 0 {
		return "", errors.New("no text content found in response")
	}

	return joinTextParts(textParts), nil
}

func extractTextParts(resultArray []interface{}) []string {
	var textParts []string

	for _, block := range resultArray {
		if text := extractTextFromBlock(block); text != "" {
			textParts = append(textParts, text)
		}
	}

	return textParts
}

func extractTextFromBlock(block interface{}) string {
	blockMap, ok := block.(map[string]interface{})
	if !ok {
		return ""
	}

	blockType, ok := blockMap["type"].(string)
	if !ok || blockType != "text" {
		return ""
	}

	text, ok := blockMap["text"].(string)
	if !ok {
		return ""
	}

	return text
}

func joinTextParts(textParts []string) string {
	result := ""

	for i, part := range textParts {
		if i > 0 {
			result += " "
		}

		result += part
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
		Params:  nil, // No parameters needed for tools/list
		ID:      fmt.Sprintf("list-%d", time.Now().UnixNano()),
	}
}
