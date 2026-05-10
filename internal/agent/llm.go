package agent

import (
	"context"
	"encoding/json"
)

// ProviderType identifies the LLM backend.
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
)

// Message represents a message in the conversation.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// TextContent is a helper to create text content.
func TextContent(text string) json.RawMessage {
	b, _ := json.Marshal([]map[string]any{
		{"type": "text", "text": text},
	})
	return b
}

// ProviderRequest is the unified request to an LLM.
type ProviderRequest struct {
	Messages  []Message
	System    string
	Tools     []ToolDef
	MaxTokens int
	Model     string
}

// ToolDef describes a tool available to the LLM.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolCall represents a tool call requested by the LLM.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// ProviderResponse is the unified response from an LLM.
type ProviderResponse struct {
	Content    json.RawMessage // text content
	ToolCalls  []ToolCall
	Usage      *Usage
	StopReason string
}

// Usage tracks token usage.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamChunk represents one streaming chunk.
type StreamChunk struct {
	Content        json.RawMessage
	ToolCallBegin  *ToolCallBegin
	ToolCallDelta  string
	BlockDelta     string // generic content block delta (text, thinking, partial_json)
	DeltaType      string // "text", "thinking", "input_json"
	BlockSignature string // thinking signature (message-level delta)
	Usage          *Usage
	Done           bool
}

type ToolCallBegin struct {
	ID   string
	Name string
}

// Provider is the interface for LLM backends.
type Provider interface {
	Type() ProviderType
	Complete(ctx context.Context, req ProviderRequest) (*ProviderResponse, error)
	CompleteStream(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error)
}
