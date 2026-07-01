// Package openai provides optional OpenAI-flavored building blocks for model
// providers: message types, request/body builders, chunk/complete decoders,
// and error decoding. It is a toolkit, not a compatibility claim — a provider
// uses these when they match the target endpoint and supplies its own when they
// don't. Nothing here registers a provider or assumes any model's behavior.
package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/types"
)

// Message is one element of the OpenAI chat completions message array.
type Message struct {
	Role             string     `json:"role"`
	Content          any        `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is an assistant-issued tool invocation.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Tool is a tool definition in the request body.
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function is the function schema inside a Tool.
type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Thinking toggles extended thinking on endpoints that support it.
type Thinking struct {
	Type string `json:"type"`
}

// ErrorBody is the standard OpenAI error envelope.
type ErrorBody struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ChatURL builds a chat completions URL from a base URL, accounting for
// base URLs that already include a version segment.
func ChatURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	apiPath := "/v1/chat/completions"
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.HasSuffix(trimmed, "/v2") || strings.HasSuffix(trimmed, "/v3") || strings.HasSuffix(trimmed, "/v4") {
		apiPath = "/chat/completions"
	}
	return baseURL + apiPath
}

// ModelsURL builds the models-list URL from a base URL.
func ModelsURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.HasSuffix(trimmed, "/v2") || strings.HasSuffix(trimmed, "/v3") || strings.HasSuffix(trimmed, "/v4") {
		return trimmed + "/models"
	}
	return trimmed + "/v1/models"
}

// DecodeError parses a non-200 response body as an OpenAI error envelope.
func DecodeError(status int, body []byte) error {
	var eb ErrorBody
	if json.Unmarshal(body, &eb) == nil && eb.Error.Message != "" {
		return fmt.Errorf("llm: %s (status %d)", eb.Error.Message, status)
	}
	return nil
}

// DefaultSchema returns schema if non-empty, else a minimal object schema.
func DefaultSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return schema
}

// BuildTools converts ToolDefs to OpenAI tool definitions.
func BuildTools(tools []types.ToolDef) []Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]Tool, len(tools))
	for i, t := range tools {
		out[i] = Tool{
			Type: "function",
			Function: Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  DefaultSchema(t.Schema),
			},
		}
	}
	return out
}
