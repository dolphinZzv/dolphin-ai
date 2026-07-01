// Package responses provides building blocks for the OpenAI Responses API
// (/v1/responses). It follows the same pattern as proto/openai but targets
// the newer Responses protocol: input array instead of messages, output
// items instead of choices, and different SSE event types.
package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/types"
)

// ---------------------------------------------------------------------------
// SSE event type constants
// ---------------------------------------------------------------------------

const (
	EventResponseCreated                    = "response.created"
	EventResponseInProgress                 = "response.in_progress"
	EventResponseCompleted                  = "response.completed"
	EventResponseOutputItemAdded            = "response.output_item.added"
	EventResponseOutputItemDone             = "response.output_item.done"
	EventResponseContentPartAdded           = "response.content_part.added"
	EventResponseContentPartDone            = "response.content_part.done"
	EventResponseOutputTextDelta            = "response.output_text.delta"
	EventResponseOutputTextDone             = "response.output_text.done"
	EventResponseFunctionCallArgumentsDelta = "response.function_call_arguments.delta"
	EventResponseFunctionCallArgumentsDone  = "response.function_call_arguments.done"
	EventError                              = "error"
)

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

// InputItem is one element of the Responses API "input" array.
// It supports both role-based message items and type-based items
// (e.g. function_call_output).
type InputItem struct {
	Type    string `json:"type,omitempty"`    // "message" (default) or "function_call_output"
	Role    string `json:"role,omitempty"`    // "user", "assistant", "system", "developer"
	Content any    `json:"content,omitempty"` // string or []InputContentPart
	CallID  string `json:"call_id,omitempty"` // for function_call_output
	Output  string `json:"output,omitempty"`  // for function_call_output
}

// InputContentPart is a structured content block within an InputItem.
type InputContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL any    `json:"image_url,omitempty"` // string or struct{URL,Detail}
}

// Tool is a tool definition for the Responses API.
type Tool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// RequestBody is the Responses API request body.
type RequestBody struct {
	Model           string      `json:"model"`
	Input           []InputItem `json:"input"`
	Instructions    string      `json:"instructions,omitempty"`
	Temperature     float64     `json:"temperature,omitempty"`
	TopP            float64     `json:"top_p,omitempty"`
	MaxOutputTokens int         `json:"max_output_tokens,omitempty"`
	Stream          bool        `json:"stream"`
	Stop            []string    `json:"stop,omitempty"`
	Tools           []Tool      `json:"tools,omitempty"`
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// OutputItem is one element of the non-streaming response "output" array.
type OutputItem struct {
	ID        string              `json:"id,omitempty"`
	Type      string              `json:"type"` // "message", "function_call"
	Role      string              `json:"role,omitempty"`
	Content   []OutputContentPart `json:"content,omitempty"`
	CallID    string              `json:"call_id,omitempty"`
	Name      string              `json:"name,omitempty"`
	Arguments string              `json:"arguments,omitempty"`
	Status    string              `json:"status,omitempty"`
}

// OutputContentPart is a content block inside an output message.
type OutputContentPart struct {
	Type string `json:"type"` // "output_text", "refusal"
	Text string `json:"text,omitempty"`
}

// Usage mirrors the Responses API usage block.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	InputTokensDetails       any `json:"input_tokens_details,omitempty"`
	OutputTokens             int `json:"output_tokens"`
	OutputTokensDetails      any `json:"output_tokens_details,omitempty"`
	TotalTokens              int `json:"total_tokens"`
}

// Response is the non-streaming Responses API response envelope.
type Response struct {
	ID     string       `json:"id"`
	Object string       `json:"object"`
	Status string       `json:"status"`
	Output []OutputItem `json:"output"`
	Usage  *Usage       `json:"usage,omitempty"`
}

// ErrorBody is the standard OpenAI error envelope.
type ErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

// ---------------------------------------------------------------------------
// Streaming event structs (for partial decoding in the chunk decoder)
// ---------------------------------------------------------------------------

// sseEvent is the minimal common structure for dispatching SSE events by type.
type sseEvent struct {
	Type string `json:"type"`
}

// outputTextDelta is the payload for response.output_text.delta events.
type outputTextDelta struct {
	Type       string `json:"type"`
	ItemID     string `json:"item_id"`
	OutputIdx  int    `json:"output_index"`
	ContentIdx int    `json:"content_index"`
	Delta      string `json:"delta"`
}

// outputItemAdded is the payload for response.output_item.added.
type outputItemAdded struct {
	Type       string     `json:"type"`
	OutputIdx  int        `json:"output_index"`
	Item       outputItem `json:"item"`
}

// outputItem is the item embedded in output_item.added.
type outputItem struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	CallID string `json:"call_id"`
	Status string `json:"status"`
}

// funcCallArgsDelta is the payload for response.function_call_arguments.delta.
type funcCallArgsDelta struct {
	Type      string `json:"type"`
	ItemID    string `json:"item_id"`
	OutputIdx int    `json:"output_index"`
	Delta     string `json:"delta"`
}

// funcCallArgsDone is the payload for response.function_call_arguments.done.
type funcCallArgsDone struct {
	Type       string `json:"type"`
	ItemID     string `json:"item_id"`
	OutputIdx  int    `json:"output_index"`
	Arguments  string `json:"arguments"`
}

// responseCompleted is the payload for response.completed.
type responseCompleted struct {
	Type     string   `json:"type"`
	Response Response `json:"response"`
}

// ---------------------------------------------------------------------------
// URL helpers
// ---------------------------------------------------------------------------

// ChatURL builds the Responses API endpoint URL from a base URL.
func ChatURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.HasSuffix(trimmed, "/v2") || strings.HasSuffix(trimmed, "/v3") {
		return trimmed + "/responses"
	}
	return trimmed + "/v1/responses"
}

// ModelsURL builds the models-list URL from a base URL.
// Shares the /v1/models endpoint with the Chat API.
func ModelsURL(baseURL string) string {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.HasSuffix(trimmed, "/v2") || strings.HasSuffix(trimmed, "/v3") {
		return trimmed + "/models"
	}
	return trimmed + "/v1/models"
}

// ---------------------------------------------------------------------------
// Error decoding
// ---------------------------------------------------------------------------

// DecodeError parses an OpenAI error response body.
func DecodeError(status int, body []byte) error {
	var eb ErrorBody
	if err := json.Unmarshal(body, &eb); err == nil && eb.Error.Message != "" {
		return fmt.Errorf("llm: %s (status %d)", eb.Error.Message, status)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tool building
// ---------------------------------------------------------------------------

// BuildTools converts []types.ToolDef to the Responses API tool format.
func BuildTools(tools []types.ToolDef) []Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(tools))
	for _, td := range tools {
		var params json.RawMessage
		if td.Schema != nil {
			if b, err := json.Marshal(td.Schema); err == nil {
				params = b
			}
		}
		out = append(out, Tool{
			Type:        "function",
			Name:        td.Name,
			Description: td.Description,
			Parameters:  params,
		})
	}
	return out
}
