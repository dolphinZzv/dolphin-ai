package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	"dolphin/internal/types"
)

// eventDecoder decodes an Anthropic SSE event stream.
type eventDecoder struct {
	sse                      *proto.SSEReader
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

// NewChunkDecoder returns a proto.ChunkDecoder factory over r.
func NewChunkDecoder(r io.Reader) proto.ChunkDecoder {
	return &eventDecoder{sse: proto.NewSSEReader(r)}
}

// Decode implements proto.ChunkDecoder.
func (d *eventDecoder) Decode() (llm.LLMChunk, error) {
	for {
		data, _, err := d.sse.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return llm.LLMChunk{}, io.EOF
			}
			return llm.LLMChunk{}, fmt.Errorf("stream read: %w", err)
		}

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &base); err != nil {
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
			if err := json.Unmarshal(data, &msg); err == nil {
				d.inputTokens = msg.Message.Usage.InputTokens
				d.cacheCreationInputTokens = msg.Message.Usage.CacheCreationInputTokens
				d.cacheReadInputTokens = msg.Message.Usage.CacheReadInputTokens
			}
			continue

		case "content_block_start":
			var blk struct {
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
			if err := json.Unmarshal(data, &blk); err != nil {
				continue
			}
			switch blk.ContentBlock.Type {
			case "text":
				if blk.ContentBlock.Text != "" {
					return llm.LLMChunk{Content: blk.ContentBlock.Text}, nil
				}
			case "thinking":
				d.hasPendingThink = true
				d.pendingThinking.Reset()
				d.pendingSignature = ""
				d.pendingThinking.WriteString(blk.ContentBlock.Thinking)
			case "tool_use":
				d.pendingToolID = blk.ContentBlock.ID
				d.pendingToolName = blk.ContentBlock.Name
				d.pendingInput.Reset()
				input := string(blk.ContentBlock.Input)
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
			if err := json.Unmarshal(data, &delta); err != nil {
				continue
			}
			dt := delta.Delta.Type
			if dt == "" {
				dt = "text_delta"
			}
			switch dt {
			case "text_delta":
				return llm.LLMChunk{Content: delta.Delta.Text}, nil
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
				return llm.LLMChunk{Thinking: thinking, ThinkingSignature: sig}, nil
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
				return llm.LLMChunk{ToolCalls: []types.ToolCall{tc}}, nil
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
			if err := json.Unmarshal(data, &delta); err == nil {
				d.outputTokens = delta.Usage.OutputTokens
				if delta.Delta.StopReason != "" {
					return llm.LLMChunk{
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
			return llm.LLMChunk{
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
			if err := json.Unmarshal(data, &errResp); err == nil {
				return llm.LLMChunk{Error: fmt.Errorf("anthropic: %s", errResp.Error.Message)}, nil
			}
			return llm.LLMChunk{Error: fmt.Errorf("anthropic: stream error")}, nil
		}
	}
}

// nonStreamResponse is an Anthropic non-streaming messages response.
type nonStreamResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		Signature string          `json:"signature"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage,omitempty"`
}

// DecodeComplete parses a full non-streaming Anthropic response body.
func DecodeComplete(raw []byte) (llm.LLMChunk, error) {
	var cr nonStreamResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return llm.LLMChunk{}, fmt.Errorf("anthropic: decode response: %w", err)
	}

	chunk := llm.LLMChunk{}
	if cr.Usage != nil {
		chunk.InputTokens = cr.Usage.InputTokens
		chunk.OutputTokens = cr.Usage.OutputTokens
		chunk.CacheCreationInputTokens = cr.Usage.CacheCreationInputTokens
		chunk.CacheReadInputTokens = cr.Usage.CacheReadInputTokens
	}
	for _, block := range cr.Content {
		switch block.Type {
		case "text":
			chunk.Content += block.Text
		case "thinking":
			chunk.Thinking = block.Thinking
			chunk.ThinkingSignature = block.Signature
		case "tool_use":
			args := ""
			if block.Input != nil {
				args = string(block.Input)
			}
			chunk.ToolCalls = append(chunk.ToolCalls, types.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}
	return chunk, nil
}
