package openai

import (
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"dolphin/internal/llm"
	"dolphin/internal/types"
)

// BuildMessages converts an LLMRequest to an OpenAI-flavored message array.
// Providers may use this directly or build their own when the endpoint differs.
func BuildMessages(req llm.LLMRequest, logger *zap.Logger) []Message {
	var msgs []Message
	if req.System != "" {
		msgs = append(msgs, Message{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role { //nolint:exhaustive // RoleUser/RoleSystem share the default path
		case types.RoleTool:
			msgs = append(msgs, Message{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			})
		case types.RoleAssistant:
			if logger != nil {
				logger.Info("build openai assistant msg",
					zap.Bool("has_content", m.Content != ""),
					zap.Int("tool_calls", len(m.ToolCalls)),
				)
			}
			if len(m.ToolCalls) > 0 {
				tcs := make([]ToolCall, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					tcs[i] = ToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					}
				}
				msgs = append(msgs, Message{
					Role:             "assistant",
					Content:          nil,
					ReasoningContent: m.Thinking,
					ToolCalls:        tcs,
				})
			} else {
				msgs = append(msgs, Message{
					Role:             "assistant",
					Content:          m.Content,
					ReasoningContent: m.Thinking,
				})
			}
		default:
			msgs = append(msgs, Message{Role: string(m.Role), Content: m.Content})
		}
	}
	return msgs
}

// RequestBody is the OpenAI chat completions request body. Exported so providers
// can marshal a custom variant (e.g. add vendor-specific fields) when needed.
type RequestBody struct {
	Model           string    `json:"model"`
	Messages        []Message `json:"messages"`
	Temperature     float64   `json:"temperature"`
	TopP            float64   `json:"top_p,omitempty"`
	MaxTokens       int       `json:"max_tokens"`
	Stream          bool      `json:"stream"`
	Stop            []string  `json:"stop,omitempty"`
	Tools           []Tool    `json:"tools,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	Thinking        *Thinking `json:"thinking,omitempty"`
}

// BuildRequest assembles the standard OpenAI chat completions request body.
// Providers needing different fields should construct their own body and
// marshal it directly.
func BuildRequest(model string, messages []Message, cfg llm.Config, req llm.LLMRequest) ([]byte, error) {
	temperature := req.Temperature
	if temperature == 0 {
		temperature = cfg.Temperature
	}
	if temperature == 0 {
		temperature = 1.0
	}

	body := RequestBody{
		Model:           model,
		Messages:        messages,
		Temperature:     temperature,
		TopP:            req.TopP,
		MaxTokens:       req.MaxTokens,
		Stream:          req.Stream,
		Stop:            req.Stop,
		ReasoningEffort: req.ReasoningEffort,
	}
	if req.Thinking {
		body.Thinking = &Thinking{Type: "enabled"}
	}
	if tools := BuildTools(req.Tools); len(tools) > 0 {
		body.Tools = tools
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}
	return data, nil
}
