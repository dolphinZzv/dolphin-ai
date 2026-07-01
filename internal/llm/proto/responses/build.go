package responses

import (
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"dolphin/internal/llm"
	"dolphin/internal/types"
)

// BuildInput converts llm.LLMRequest messages into the Responses API "input"
// array format. System messages are NOT placed in the input array — they are
// returned separately as the instructions string so the caller can set the
// "instructions" field on the request body.
//
// Mapping:
//   - system → instructions (returned as second value, empty string skips)
//   - user → {role: "user", content: text or [{input_text...}, {input_image...}]}
//   - assistant (+tool_calls) → {role: "assistant", content: [{output_text}, {function_call}...]}
//   - tool → {type: "function_call_output", call_id, output}
func BuildInput(req llm.LLMRequest, logger *zap.Logger) ([]InputItem, string) {
	var input []InputItem
	instructions := req.System

	for _, m := range req.Messages {
		switch m.Role { //nolint:exhaustive
		case types.RoleTool:
			input = append(input, InputItem{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: m.Text(),
			})

		case types.RoleAssistant:
			if len(m.ToolCalls) > 0 {
				// Assistant with tool calls: emit as content-part array.
				var parts []InputContentPart
				if t := m.Text(); t != "" {
					parts = append(parts, InputContentPart{Type: "output_text", Text: t})
				}
				for _, tc := range m.ToolCalls {
					parts = append(parts, InputContentPart{
						Type: "function_call",
						Text: tc.Arguments, // stored in Text field for JSON round-trip
					})
				}
				// Build the function_call inline parts; for Responses API these
				// live under a structured content block.
				input = append(input, buildAssistantInput(m, logger))
			} else {
				input = append(input, InputItem{
					Role:    "assistant",
					Content: m.Text(),
				})
			}

		default:
			// User and any other role.
			if m.HasImage() {
				input = append(input, buildImageInput(m, logger))
			} else {
				input = append(input, InputItem{
					Role:    string(m.Role),
					Content: m.Text(),
				})
			}
		}
	}
	return input, instructions
}

// buildAssistantInput converts an assistant message with tool calls into the
// Responses API input format: {role: "assistant", content: [parts...]}
// where parts are {type: "output_text", text: ...} and
// {type: "function_call", call_id: ..., name: ..., arguments: ...}.
func buildAssistantInput(m types.Message, logger *zap.Logger) InputItem {
	var parts []map[string]any
	if t := m.Text(); t != "" {
		parts = append(parts, map[string]any{"type": "output_text", "text": t})
	}
	for _, tc := range m.ToolCalls {
		parts = append(parts, map[string]any{
			"type":      "function_call",
			"call_id":   tc.ID,
			"name":      tc.Name,
			"arguments": tc.Arguments,
		})
	}
	return InputItem{Role: "assistant", Content: parts}
}

// buildImageInput converts a user message with image parts into the Responses
// API content-part format.
func buildImageInput(m types.Message, logger *zap.Logger) InputItem {
	parts := []map[string]any{{"type": "input_text", "text": m.Text()}}
	for _, p := range m.Parts {
		if p.Type != types.PartImage {
			continue
		}
		mimeStr, b64, err := p.LoadBase64()
		if err != nil {
			if logger != nil {
				logger.Warn("responses build: image unavailable",
					zap.String("path", p.Path),
					zap.Error(err),
				)
			}
			name := p.Filename
			if name == "" {
				name = p.Path
			}
			parts = append(parts, map[string]any{
				"type": "input_text", "text": fmt.Sprintf("[image: %s (unavailable)]", name),
			})
			continue
		}
		parts = append(parts, map[string]any{
			"type": "input_image",
			"image_url": map[string]string{
				"url": "data:" + mimeStr + ";base64," + b64,
			},
		})
	}
	return InputItem{Role: string(m.Role), Content: parts}
}

// BuildRequest assembles the Responses API request body.
func BuildRequest(model string, input []InputItem, instructions string, cfg llm.Config, req llm.LLMRequest) ([]byte, error) {
	temperature := req.Temperature
	if temperature == 0 {
		temperature = cfg.Temperature
	}
	if temperature == 0 {
		temperature = 1.0
	}

	body := RequestBody{
		Model:           model,
		Input:           input,
		Instructions:    instructions,
		Temperature:     temperature,
		TopP:            req.TopP,
		MaxOutputTokens: req.MaxTokens,
		Stream:          req.Stream,
		Stop:            req.Stop,
	}
	if tools := BuildTools(req.Tools); len(tools) > 0 {
		body.Tools = tools
	}

	return json.Marshal(body)
}
