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
				Content:    m.Text(),
				ToolCallID: m.ToolCallID,
			})
		case types.RoleAssistant:
			if logger != nil {
				logger.Info("build openai assistant msg",
					zap.Bool("has_content", m.Text() != ""),
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
					Content:          m.Text(),
					ReasoningContent: m.Thinking,
				})
			}
		default:
			// User/system: emit a content-part array when the message carries
			// image attachments, so vision-capable models can read them.
			// Text-only messages stay a plain string (preserves prior behavior).
			if m.HasImage() {
				parts := []map[string]any{{"type": "text", "text": m.Text()}}
				for _, p := range m.Parts {
					if p.Type != types.PartImage {
						continue
					}
					mimeStr, b64, err := p.LoadBase64()
					if err != nil {
						if logger != nil {
							logger.Warn("openai build: image unavailable",
								zap.String("path", p.Path),
								zap.Error(err),
							)
						}
						name := p.Filename
						parts = append(parts, map[string]any{
							"type": "text", "text": fmt.Sprintf("[image: %s (unavailable)]", name),
						})
						continue
					}
					parts = append(parts, map[string]any{
						"type":      "image_url",
						"image_url": map[string]string{"url": "data:" + mimeStr + ";base64," + b64},
					})
				}
				msgs = append(msgs, Message{Role: string(m.Role), Content: parts})
			} else {
				msgs = append(msgs, Message{Role: string(m.Role), Content: m.Text()})
			}
		}
	}
	return msgs
}

// StreamOptions holds the stream_options block sent alongside stream:true.
// OpenAI requires stream_options.include_usage=true to include usage in the
// streaming SSE chunk stream.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// RequestBody is the OpenAI chat completions request body. Exported so providers
// can marshal a custom variant (e.g. add vendor-specific fields) when needed.
type RequestBody struct {
	Model           string         `json:"model"`
	Messages        []Message      `json:"messages"`
	Temperature     float64        `json:"temperature"`
	TopP            float64        `json:"top_p,omitempty"`
	MaxTokens       int            `json:"max_tokens"`
	Stream          bool           `json:"stream"`
	StreamOptions   *StreamOptions `json:"stream_options,omitempty"`
	Stop            []string       `json:"stop,omitempty"`
	Tools           []Tool         `json:"tools,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	Thinking        *Thinking      `json:"thinking,omitempty"`
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
	if req.Stream {
		body.StreamOptions = &StreamOptions{IncludeUsage: true}
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
