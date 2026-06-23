// Package anthropic provides optional Anthropic-flavored building blocks for
// model providers: message types, request body builder, event-stream decoder,
// and complete-response decoder. Like proto/openai it is a toolkit, not a
// compatibility claim.
package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/types"
)

// Message is one element of the Anthropic messages array. Content is raw JSON
// because Anthropic uses heterogeneous content block arrays.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// Request is the Anthropic messages request body.
type Request struct {
	Model        string        `json:"model"`
	Messages     []Message     `json:"messages"`
	System       string        `json:"system,omitempty"`
	MaxTokens    int           `json:"max_tokens"`
	Temperature  float64       `json:"temperature"`
	TopP         float64       `json:"top_p,omitempty"`
	Stream       bool          `json:"stream"`
	Stop         []string      `json:"stop_sequences,omitempty"`
	Tools        []Tool        `json:"tools,omitempty"`
	OutputConfig *OutputConfig `json:"output_config,omitempty"`
	Thinking     *Thinking     `json:"thinking,omitempty"`
}

// OutputConfig carries reasoning effort.
type OutputConfig struct {
	Effort string `json:"effort"`
}

// Thinking enables extended thinking with a token budget.
type Thinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// Tool is an Anthropic tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Error is one error entry in an Anthropic error envelope.
type Error struct {
	Message string `json:"message"`
}

// ErrorBody is the Anthropic error envelope.
type ErrorBody struct {
	Error Error `json:"error"`
}

// ChatURL builds the messages URL from a base URL.
func ChatURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return baseURL + "/v1/messages"
}

// ModelsURL builds the models-list URL from a base URL.
func ModelsURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return strings.TrimRight(baseURL, "/") + "/v1/models"
}

// DefaultSchema returns schema if non-empty, else a minimal object schema.
func DefaultSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return schema
}

// DecodeError parses a non-200 response body as an Anthropic error envelope.
func DecodeError(status int, body []byte) error {
	var eb ErrorBody
	if json.Unmarshal(body, &eb) == nil && eb.Error.Message != "" {
		return fmt.Errorf("llm: %s (status %d)", eb.Error.Message, status)
	}
	return nil
}

// BuildTools converts ToolDefs to Anthropic tool definitions.
func BuildTools(tools []types.ToolDef) []Tool {
	out := make([]Tool, len(tools))
	for i, t := range tools {
		out[i] = Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: DefaultSchema(t.Schema),
		}
	}
	return out
}
