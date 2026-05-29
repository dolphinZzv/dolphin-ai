package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"dolphin/internal/types"
)

type pendingToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

type StreamDecoder struct {
	scanner          *bufio.Scanner
	pendingToolCalls map[int]*pendingToolCall
}

func NewStreamDecoder(r io.Reader) *StreamDecoder {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &StreamDecoder{scanner: scanner}
}

func (d *StreamDecoder) flushToolCalls() []types.ToolCall {
	if len(d.pendingToolCalls) == 0 {
		return nil
	}
	tcs := make([]types.ToolCall, 0, len(d.pendingToolCalls))
	for i := 0; i < len(d.pendingToolCalls); i++ {
		ptc := d.pendingToolCalls[i]
		if ptc != nil {
			tcs = append(tcs, types.ToolCall{
				ID:        ptc.id,
				Name:      ptc.name,
				Arguments: ptc.arguments.String(),
			})
		}
	}
	d.pendingToolCalls = nil
	return tcs
}

func (d *StreamDecoder) Decode() (LLMChunk, error) {
	for d.scanner.Scan() {
		line := d.scanner.Text()

		if line == "" {
			continue
		}

		if line == "data: [DONE]" {
			if tc := d.flushToolCalls(); tc != nil {
				return LLMChunk{ToolCalls: tc, Done: true}, nil
			}
			return LLMChunk{Done: true}, nil
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		var payload struct {
			Choices []struct {
				Delta struct {
					Content   string            `json:"content"`
					ToolCalls []json.RawMessage `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				TotalTokens         int `json:"total_tokens"`
				PromptTokensDetails *struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue
		}

		// Final chunk may carry usage with empty choices.
		if len(payload.Choices) == 0 {
			if payload.Usage != nil {
				return LLMChunk{
					Done:         true,
					InputTokens:  payload.Usage.PromptTokens,
					OutputTokens: payload.Usage.CompletionTokens,
					TotalTokens:  payload.Usage.TotalTokens,
					PromptCachedTokens: func() int {
						if payload.Usage.PromptTokensDetails != nil {
							return payload.Usage.PromptTokensDetails.CachedTokens
						}
						return 0
					}(),
				}, nil
			}
			continue
		}

		choice := payload.Choices[0]

		if d.pendingToolCalls == nil {
			d.pendingToolCalls = make(map[int]*pendingToolCall)
		}
		for _, tcRaw := range choice.Delta.ToolCalls {
			var delta struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			}
			if err := json.Unmarshal(tcRaw, &delta); err != nil {
				continue
			}
			ptc, exists := d.pendingToolCalls[delta.Index]
			if !exists {
				ptc = &pendingToolCall{}
				d.pendingToolCalls[delta.Index] = ptc
			}
			if delta.ID != "" {
				ptc.id = delta.ID
			}
			if delta.Function.Name != "" {
				ptc.name = delta.Function.Name
			}
			if delta.Function.Arguments != "" {
				ptc.arguments.WriteString(delta.Function.Arguments)
			}
		}

		if choice.FinishReason == "tool_calls" {
			chunk := LLMChunk{
				ToolCalls: d.flushToolCalls(),
				Done:      true,
			}
			if payload.Usage != nil {
				chunk.InputTokens = payload.Usage.PromptTokens
				chunk.OutputTokens = payload.Usage.CompletionTokens
				chunk.TotalTokens = payload.Usage.TotalTokens
				if payload.Usage.PromptTokensDetails != nil {
					chunk.PromptCachedTokens = payload.Usage.PromptTokensDetails.CachedTokens
				}
			}
			return chunk, nil
		}

		if payload.Usage != nil {
			return LLMChunk{
				Done:         true,
				InputTokens:  payload.Usage.PromptTokens,
				OutputTokens: payload.Usage.CompletionTokens,
				TotalTokens:  payload.Usage.TotalTokens,
				PromptCachedTokens: func() int {
					if payload.Usage.PromptTokensDetails != nil {
						return payload.Usage.PromptTokensDetails.CachedTokens
					}
					return 0
				}(),
			}, nil
		}

		return LLMChunk{
			Content: choice.Delta.Content,
			Done:    choice.FinishReason == "stop",
		}, nil
	}

	if err := d.scanner.Err(); err != nil {
		return LLMChunk{}, fmt.Errorf("stream read: %w", err)
	}

	return LLMChunk{}, io.EOF
}

// AnthropicStreamDecoder decodes Anthropic SSE format.
type AnthropicStreamDecoder struct {
	scanner                  *bufio.Scanner
	inputTokens              int
	outputTokens             int
	cacheCreationInputTokens int
	cacheReadInputTokens     int
	pendingToolID            string
	pendingToolName          string
	pendingInput             strings.Builder
	pendingThinking          strings.Builder
	pendingSignature         string
	hasPendingThink          bool
}

func NewAnthropicDecoder(r io.Reader) *AnthropicStreamDecoder {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &AnthropicStreamDecoder{scanner: scanner}
}

func (d *AnthropicStreamDecoder) Decode() (LLMChunk, error) {
	for d.scanner.Scan() {
		line := d.scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &base); err != nil {
			continue
		}

		switch base.Type {
		case "message_start":
			var msg struct {
				Message struct {
					ID    string `json:"id"`
					Usage struct {
						InputTokens              int `json:"input_tokens"`
						OutputTokens             int `json:"output_tokens"`
						CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
						CacheReadInputTokens     int `json:"cache_read_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &msg); err == nil {
				d.inputTokens = msg.Message.Usage.InputTokens
				d.cacheCreationInputTokens = msg.Message.Usage.CacheCreationInputTokens
				d.cacheReadInputTokens = msg.Message.Usage.CacheReadInputTokens
			}
			continue

		case "content_block_start":
			var base struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type     string          `json:"type"`
					Text     string          `json:"text"`
					Thinking string          `json:"thinking"`
					ID       string          `json:"id"`
					Name     string          `json:"name"`
					Input    json.RawMessage `json:"input"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &base); err != nil {
				continue
			}
			switch base.ContentBlock.Type {
			case "text":
				if base.ContentBlock.Text != "" {
					return LLMChunk{Content: base.ContentBlock.Text}, nil
				}
			case "thinking":
				d.hasPendingThink = true
				d.pendingThinking.Reset()
				d.pendingSignature = ""
				d.pendingThinking.WriteString(base.ContentBlock.Thinking)
			case "tool_use":
				d.pendingToolID = base.ContentBlock.ID
				d.pendingToolName = base.ContentBlock.Name
				d.pendingInput.Reset()
				input := string(base.ContentBlock.Input)
				if input != "{}" && input != "" {
					d.pendingInput.WriteString(input)
				}
			}
			continue

		case "content_block_delta":
			var delta struct {
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Thinking    string `json:"thinking"`
					Signature   string `json:"signature"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				continue
			}
			// Default to text_delta when type is empty (backward compat).
			dt := delta.Delta.Type
			if dt == "" {
				dt = "text_delta"
			}
			switch dt {
			case "text_delta":
				return LLMChunk{Content: delta.Delta.Text}, nil
			case "thinking_delta":
				if d.hasPendingThink {
					content := delta.Delta.Thinking
					if content == "" {
						content = delta.Delta.Text
					}
					d.pendingThinking.WriteString(content)
				}
			case "signature_delta":
				if d.hasPendingThink && delta.Delta.Signature != "" {
					d.pendingSignature = delta.Delta.Signature
				}
			case "input_json_delta":
				if d.pendingToolName != "" {
					d.pendingInput.WriteString(delta.Delta.PartialJSON)
				}
			}
			continue

		case "content_block_stop":
			if d.hasPendingThink {
				thinking := d.pendingThinking.String()
				sig := d.pendingSignature
				d.hasPendingThink = false
				d.pendingThinking.Reset()
				d.pendingSignature = ""
				return LLMChunk{Thinking: thinking, ThinkingSignature: sig}, nil
			}
			if d.pendingToolName != "" {
				tc := types.ToolCall{
					ID:        d.pendingToolID,
					Name:      d.pendingToolName,
					Arguments: d.pendingInput.String(),
				}
				d.pendingToolName = ""
				d.pendingToolID = ""
				d.pendingInput.Reset()
				return LLMChunk{ToolCalls: []types.ToolCall{tc}}, nil
			}
			continue

		case "message_delta":
			var delta struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err == nil {
				d.outputTokens = delta.Usage.OutputTokens
				if delta.Delta.StopReason != "" {
					return LLMChunk{
						Done:                     true,
						InputTokens:              d.inputTokens,
						OutputTokens:             d.outputTokens,
						CacheCreationInputTokens: d.cacheCreationInputTokens,
						CacheReadInputTokens:     d.cacheReadInputTokens,
						TotalTokens:              d.inputTokens + d.outputTokens,
					}, nil
				}
			}

		case "message_stop":
			return LLMChunk{
				Done:                     true,
				InputTokens:              d.inputTokens,
				OutputTokens:             d.outputTokens,
				CacheCreationInputTokens: d.cacheCreationInputTokens,
				CacheReadInputTokens:     d.cacheReadInputTokens,
				TotalTokens:              d.inputTokens + d.outputTokens,
			}, nil

		case "ping":
			continue

		case "error":
			var errResp struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &errResp); err == nil {
				return LLMChunk{Error: fmt.Errorf("anthropic: %s", errResp.Error.Message)}, nil
			}
			return LLMChunk{Error: fmt.Errorf("anthropic: stream error")}, nil
		}
	}

	if err := d.scanner.Err(); err != nil {
		return LLMChunk{}, fmt.Errorf("stream read: %w", err)
	}

	return LLMChunk{}, io.EOF
}
