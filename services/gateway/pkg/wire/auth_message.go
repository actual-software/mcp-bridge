// Package wire provides binary protocol implementation for MCP messages over TCP connections.
package wire

import (
	"encoding/json"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// AuthMessage wraps an MCP message with authentication token.
type AuthMessage struct {
	AuthToken string      `json:"auth_token,omitempty"`
	Message   interface{} `json:"message"`
}

// NewAuthRequest creates an authenticated request message.
func NewAuthRequest(req *mcp.Request, authToken string) *AuthMessage {
	return &AuthMessage{
		AuthToken: authToken,
		Message:   req,
	}
}

// NewAuthResponse creates an authenticated response message.
func NewAuthResponse(resp *mcp.Response, authToken string) *AuthMessage {
	return &AuthMessage{
		AuthToken: authToken,
		Message:   resp,
	}
}

// UnmarshalAuthMessage unmarshals an auth message and returns the inner message.
func UnmarshalAuthMessage(data []byte) (*AuthMessage, error) {
	var authMsg AuthMessage
	if err := json.Unmarshal(data, &authMsg); err != nil {
		return nil, err
	}

	return &authMsg, nil
}

// ExtractRequest extracts an MCP request from an auth message.
func (am *AuthMessage) ExtractRequest() (*mcp.Request, error) {
	msgData, err := json.Marshal(am.Message)
	if err != nil {
		return nil, err
	}

	var req mcp.Request
	if err := json.Unmarshal(msgData, &req); err != nil {
		return nil, err
	}

	return &req, nil
}

// ExtractResponse extracts an MCP response from an auth message.
func (am *AuthMessage) ExtractResponse() (*mcp.Response, error) {
	msgData, err := json.Marshal(am.Message)
	if err != nil {
		return nil, err
	}

	var resp mcp.Response
	if err := json.Unmarshal(msgData, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}
