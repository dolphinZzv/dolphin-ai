package mcp

import "encoding/json"

// JSON-RPC 2.0 message types

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

type ErrorObject struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func NewResponse(id json.RawMessage, result interface{}) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}

func NewError(id json.RawMessage, code int, message string) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &ErrorObject{Code: code, Message: message}}
}

func NewParseError(id json.RawMessage) Response {
	return NewError(id, -32700, "Parse error")
}

func NewInvalidRequest(id json.RawMessage) Response {
	return NewError(id, -32600, "Invalid Request")
}

func NewMethodNotFound(id json.RawMessage) Response {
	return NewError(id, -32601, "Method not found")
}

func NewInternalError(id json.RawMessage, msg string) Response {
	return NewError(id, -32603, msg)
}

// Standard MCP method names
const (
	MethodInitialize              = "initialize"
	MethodToolsList               = "tools/list"
	MethodToolsCall               = "tools/call"
	MethodResourcesList           = "resources/list"
	MethodResourcesTemplatesList  = "resources/templates/list"
	MethodResourcesRead           = "resources/read"
	MethodPromptsList             = "prompts/list"
	MethodPromptsGet              = "prompts/get"
	MethodPing                    = "ping"
	MethodNotificationsInitialized = "notifications/initialized"
)

// Tool definition
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
	Handler     ToolHandler `json:"-"`
}

type ToolHandler func(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response

// Resource definition
type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceTemplate defines a parameterized resource URI pattern (RFC 6570)
type ResourceTemplate struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Prompt definition
type PromptDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}
