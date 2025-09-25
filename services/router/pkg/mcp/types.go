package mcp

// Request represents an MCP request message.
type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      interface{} `json:"id"`
}

// Response represents an MCP response message.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// Message represents a generic MCP message (used for testing and generic handling).
type Message struct {
	ID      interface{} `json:"id"`
	Type    string      `json:"type,omitempty"`
	Method  string      `json:"method,omitempty"`
	Payload interface{} `json:"payload,omitempty"`
	JSONRPC string      `json:"jsonrpc,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
	Params  interface{} `json:"params,omitempty"`
}

// Error represents an MCP error.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard error codes.
const (
	ErrorCodeParseError     = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603

	// Custom error codes.
	ErrorCodeToolNotFound       = -32001
	ErrorCodeUnauthorized       = -32002
	ErrorCodeRateLimitExceeded  = -32003
	ErrorCodeBackendUnavailable = -32004
	ErrorCodeRequestTimeout     = -32005
)

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"inputSchema"`
	Namespace   string      `json:"namespace,omitempty"`
	Version     string      `json:"version,omitempty"`
}

// InitializeParams represents parameters for the initialize method.
type InitializeParams struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ClientInfo      ClientInfo   `json:"clientInfo"`
}

// InitializeResult represents the result of the initialize method.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

// Capabilities represents MCP capabilities.
type Capabilities struct {
	Tools     *ToolsCapability `json:"tools,omitempty"`
	Resources bool             `json:"resources,omitempty"`
	Prompts   bool             `json:"prompts,omitempty"`
}

// ToolsCapability represents tools capability details.
type ToolsCapability struct {
	Namespaces []string `json:"namespaces,omitempty"`
	TotalTools int      `json:"totalTools,omitempty"`
}

// ClientInfo represents client information.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerInfo represents server information.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ListToolsParams represents parameters for tools/list method.
type ListToolsParams struct {
	Namespace string `json:"namespace,omitempty"`
}

// ListToolsResult represents the result of tools/list method.
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// CallToolParams represents parameters for tools/call method.
type CallToolParams struct {
	Name      string      `json:"name"`
	Arguments interface{} `json:"arguments,omitempty"`
}

// CallToolResult represents the result of tools/call method.
type CallToolResult []ToolContent

// ToolContent represents content returned by a tool.
type ToolContent struct {
	Type     string      `json:"type"` // "text", "image", "resource"
	Text     string      `json:"text,omitempty"`
	MimeType string      `json:"mimeType,omitempty"`
	Data     string      `json:"data,omitempty"` // base64 for images
	Resource interface{} `json:"resource,omitempty"`
}

// Helper functions.

// NewRequest creates a new MCP request.
func NewRequest(method string, params, id interface{}) *Request {
	return &Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}
}

// NewResponse creates a new MCP response.
func NewResponse(result, id interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
}

// NewErrorResponse creates a new MCP error response.
func NewErrorResponse(code int, message string, data, id interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	}
}
