package types

import (
	"encoding/json"
	"time"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSystem    Role = "system"
)

type Message struct {
	Role              Role       `json:"role"`
	Content           string     `json:"content"`
	Thinking          string     `json:"thinking,omitempty"`
	ThinkingSignature string     `json:"thinking_signature,omitempty"`
	ToolCallID        string     `json:"tool_call_id,omitempty"`
	ToolCalls         []ToolCall `json:"tool_calls,omitempty"`
	IsError           bool       `json:"is_error,omitempty"`
	IsPartial         bool       `json:"is_partial,omitempty"`
	// IsSummary marks a synthetic user message that summarizes earlier
	// turns after context compaction. It sits at the head of Messages so
	// downstream code can tell it apart from real user input.
	IsSummary bool      `json:"is_summary,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}
