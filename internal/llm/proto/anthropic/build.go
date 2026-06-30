package anthropic

import (
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"dolphin/internal/llm"
	"dolphin/internal/types"
)

// BuildMessages converts an LLMRequest to an Anthropic-flavored message array.
// Consecutive tool_result messages are collapsed into one user message so each
// tool_use has its result in the immediately following message.
func BuildMessages(req llm.LLMRequest, logger *zap.Logger) []Message {
	var msgs []Message
	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]
		switch m.Role { //nolint:exhaustive // RoleUser/RoleSystem share the default text path
		case types.RoleTool:
			var blocks []map[string]any
			for j := i; j < len(req.Messages); j++ {
				tm := req.Messages[j]
				if tm.Role != types.RoleTool {
					break
				}
				block := map[string]any{
					"type":        "tool_result",
					"tool_use_id": tm.ToolCallID,
					"content":     tm.Text(),
				}
				if tm.IsError {
					block["is_error"] = true
				}
				blocks = append(blocks, block)
				i = j
			}
			data, _ := json.Marshal(blocks)
			msgs = append(msgs, Message{Role: "user", Content: data})

		case types.RoleAssistant:
			if logger != nil {
				logger.Info("llm build assistant msg",
					zap.Bool("has_thinking", m.Thinking != ""),
					zap.Int("thinking_len", len(m.Thinking)),
					zap.Bool("has_signature", m.ThinkingSignature != ""),
					zap.Bool("has_content", m.Text() != ""),
					zap.Int("tool_calls", len(m.ToolCalls)),
				)
			}
			if m.Thinking != "" || len(m.ToolCalls) > 0 {
				var blocks []map[string]any
				if m.Thinking != "" {
					block := map[string]any{"type": "thinking", "thinking": m.Thinking}
					if m.ThinkingSignature != "" {
						block["signature"] = m.ThinkingSignature
					}
					blocks = append(blocks, block)
				}
				if m.Text() != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": m.Text()})
				}
				for _, tc := range m.ToolCalls {
					var input any = map[string]any{}
					if tc.Arguments != "" {
						_ = json.Unmarshal([]byte(tc.Arguments), &input)
					}
					blocks = append(blocks, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": input,
					})
				}
				data, _ := json.Marshal(blocks)
				msgs = append(msgs, Message{Role: "assistant", Content: data})
			} else {
				data, _ := json.Marshal(m.Text())
				msgs = append(msgs, Message{Role: "assistant", Content: data})
			}

		default:
			// User/system: emit content blocks when the message carries image
			// attachments, so vision-capable models can read them. Text-only
			// messages stay a plain string (preserves prior behavior).
			if m.HasImage() {
				blocks := []map[string]any{{"type": "text", "text": m.Text()}}
				for _, p := range m.Parts {
					if p.Type != types.PartImage {
						continue
					}
					mimeStr, b64, err := p.LoadBase64()
					if err != nil {
						if logger != nil {
							logger.Warn("anthropic build: image unavailable",
								zap.String("path", p.Path),
								zap.Error(err),
							)
						}
						blocks = append(blocks, map[string]any{
							"type": "text", "text": fmt.Sprintf("[image: %s (unavailable)]", p.Filename),
						})
						continue
					}
					blocks = append(blocks, map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": mimeStr,
							"data":       b64,
						},
					})
				}
				data, _ := json.Marshal(blocks)
				msgs = append(msgs, Message{Role: string(m.Role), Content: data})
			} else {
				data, _ := json.Marshal(m.Text())
				msgs = append(msgs, Message{Role: string(m.Role), Content: data})
			}
		}
	}
	return msgs
}

// BuildRequest marshals a full Anthropic messages request body.
func BuildRequest(model string, messages []Message, cfg llm.Config, req llm.LLMRequest) ([]byte, error) {
	temperature := req.Temperature
	if temperature == 0 {
		temperature = cfg.Temperature
	}
	if temperature == 0 {
		temperature = 1.0
	}

	body := Request{
		Model:       model,
		Messages:    messages,
		System:      req.System,
		MaxTokens:   req.MaxTokens,
		Temperature: temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		Stop:        req.Stop,
	}
	if req.ReasoningEffort != "" {
		body.OutputConfig = &OutputConfig{Effort: req.ReasoningEffort}
	}
	if req.Thinking {
		budget := req.MaxTokens
		if budget == 0 {
			budget = 4096
		}
		body.Thinking = &Thinking{Type: "enabled", BudgetTokens: budget}
	}
	if len(req.Tools) > 0 {
		body.Tools = BuildTools(req.Tools)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}
	return data, nil
}
