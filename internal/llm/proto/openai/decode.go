package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/google/uuid"

	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	"dolphin/internal/types"
)

type pendingToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

// chunkDecoder decodes an OpenAI-flavored SSE stream. It owns an SSEReader and
// accumulates tool-call deltas across chunks.
type chunkDecoder struct {
	sse              *proto.SSEReader
	pendingToolCalls map[int]*pendingToolCall
}

// NewChunkDecoder returns a proto.ChunkDecoder factory over r.
func NewChunkDecoder(r io.Reader) proto.ChunkDecoder {
	return &chunkDecoder{sse: proto.NewSSEReader(r)}
}

func (d *chunkDecoder) flushToolCalls() []types.ToolCall {
	if len(d.pendingToolCalls) == 0 {
		return nil
	}
	tcs := make([]types.ToolCall, 0, len(d.pendingToolCalls))
	for i := 0; i < len(d.pendingToolCalls); i++ {
		ptc := d.pendingToolCalls[i]
		if ptc != nil {
			id := ptc.id
			// Some OpenAI-compatible endpoints never send a tool_call ID in
			// the streaming deltas. Without an ID, downstream pairing (tool
			// result → tool call) collapses onto the last call. Synthesize a
			// unique ID so each call/result pair stays matched.
			if id == "" {
				log.Printf("warning: llm/openai: tool_call %q arrived without an ID; synthesizing one for pairing", ptc.name)
				id = "gen_" + uuid.NewString()
			}
			tcs = append(tcs, types.ToolCall{
				ID:        id,
				Name:      ptc.name,
				Arguments: ptc.arguments.String(),
			})
		}
	}
	d.pendingToolCalls = nil
	return tcs
}

// Decode implements proto.ChunkDecoder.
func (d *chunkDecoder) Decode() (llm.LLMChunk, error) {
	for {
		data, done, err := d.sse.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return llm.LLMChunk{}, io.EOF
			}
			return llm.LLMChunk{}, fmt.Errorf("stream read: %w", err)
		}
		if done {
			// Some providers append usage JSON after [DONE] (e.g.
			// "data: [DONE]{"usage":{...}}"). Parse it when present.
			if len(data) > 0 {
				var usagePayload struct {
					Usage *struct {
						PromptTokens          int `json:"prompt_tokens"`
						CompletionTokens      int `json:"completion_tokens"`
						TotalTokens           int `json:"total_tokens"`
						PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
						PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
						PromptTokensDetails   *struct {
							CachedTokens int `json:"cached_tokens"`
						} `json:"prompt_tokens_details"`
					} `json:"usage,omitempty"`
				}
				if json.Unmarshal(data, &usagePayload) == nil && usagePayload.Usage != nil {
					tc := d.flushToolCalls()
					return llm.LLMChunk{
						ToolCalls:             tc,
						Done:                  true,
						InputTokens:           usagePayload.Usage.PromptTokens,
						OutputTokens:          usagePayload.Usage.CompletionTokens,
						TotalTokens:           usagePayload.Usage.TotalTokens,
						PromptCacheHitTokens:  usagePayload.Usage.PromptCacheHitTokens,
						PromptCacheMissTokens: usagePayload.Usage.PromptCacheMissTokens,
						PromptCachedTokens: func() int {
							if usagePayload.Usage.PromptTokensDetails != nil {
								return usagePayload.Usage.PromptTokensDetails.CachedTokens
							}
							return 0
						}(),
					}, nil
				}
			}
			if tc := d.flushToolCalls(); tc != nil {
				return llm.LLMChunk{ToolCalls: tc, Done: true}, nil
			}
			return llm.LLMChunk{Done: true}, nil
		}

		var payload struct {
			Choices []struct {
				Delta struct {
					Content          string            `json:"content"`
					ReasoningContent string            `json:"reasoning_content"`
					ToolCalls        []json.RawMessage `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens          int `json:"prompt_tokens"`
				CompletionTokens      int `json:"completion_tokens"`
				TotalTokens           int `json:"total_tokens"`
				PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
				PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
				PromptTokensDetails   *struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage,omitempty"`
		}

		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}

		// Final chunk may carry usage with empty choices.
		if len(payload.Choices) == 0 {
			if payload.Usage != nil {
				return llm.LLMChunk{
					Done:                  true,
					InputTokens:           payload.Usage.PromptTokens,
					OutputTokens:          payload.Usage.CompletionTokens,
					TotalTokens:           payload.Usage.TotalTokens,
					PromptCacheHitTokens:  payload.Usage.PromptCacheHitTokens,
					PromptCacheMissTokens: payload.Usage.PromptCacheMissTokens,
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
			chunk := llm.LLMChunk{
				ToolCalls: d.flushToolCalls(),
				Done:      true,
			}
			if payload.Usage != nil {
				chunk.InputTokens = payload.Usage.PromptTokens
				chunk.OutputTokens = payload.Usage.CompletionTokens
				chunk.TotalTokens = payload.Usage.TotalTokens
				chunk.PromptCacheHitTokens = payload.Usage.PromptCacheHitTokens
				chunk.PromptCacheMissTokens = payload.Usage.PromptCacheMissTokens
				if payload.Usage.PromptTokensDetails != nil {
					chunk.PromptCachedTokens = payload.Usage.PromptTokensDetails.CachedTokens
				}
			}
			return chunk, nil
		}

		if payload.Usage != nil {
			return llm.LLMChunk{
				Done:                  true,
				InputTokens:           payload.Usage.PromptTokens,
				OutputTokens:          payload.Usage.CompletionTokens,
				TotalTokens:           payload.Usage.TotalTokens,
				PromptCacheHitTokens:  payload.Usage.PromptCacheHitTokens,
				PromptCacheMissTokens: payload.Usage.PromptCacheMissTokens,
				PromptCachedTokens: func() int {
					if payload.Usage.PromptTokensDetails != nil {
						return payload.Usage.PromptTokensDetails.CachedTokens
					}
					return 0
				}(),
			}, nil
		}

		thinking := ""
		if choice.Delta.ReasoningContent != "" {
			thinking = choice.Delta.ReasoningContent
		}
		return llm.LLMChunk{
			Content:  choice.Delta.Content,
			Thinking: thinking,
			Done:     choice.FinishReason == "stop",
		}, nil
	}
}

// completionResponse is an OpenAI non-streaming chat completion response.
type completionResponse struct {
	Choices []struct {
		Message struct {
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens          int `json:"prompt_tokens"`
		CompletionTokens      int `json:"completion_tokens"`
		TotalTokens           int `json:"total_tokens"`
		PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
		PromptTokensDetails   *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage,omitempty"`
}

// DecodeComplete parses a full non-streaming OpenAI response body into one chunk.
func DecodeComplete(raw []byte) (llm.LLMChunk, error) {
	var cr completionResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return llm.LLMChunk{}, fmt.Errorf("openai: decode response: %w", err)
	}

	chunk := llm.LLMChunk{}
	if cr.Usage != nil {
		chunk.InputTokens = cr.Usage.PromptTokens
		chunk.OutputTokens = cr.Usage.CompletionTokens
		chunk.TotalTokens = cr.Usage.TotalTokens
		chunk.PromptCacheHitTokens = cr.Usage.PromptCacheHitTokens
		chunk.PromptCacheMissTokens = cr.Usage.PromptCacheMissTokens
		if cr.Usage.PromptTokensDetails != nil {
			chunk.PromptCachedTokens = cr.Usage.PromptTokensDetails.CachedTokens
		}
	}
	if len(cr.Choices) > 0 {
		msg := cr.Choices[0].Message
		chunk.Content = msg.Content
		chunk.Thinking = msg.ReasoningContent
		if len(msg.ToolCalls) > 0 {
			tcs := make([]types.ToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				tcs[i] = types.ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				}
			}
			chunk.ToolCalls = tcs
		}
	}
	return chunk, nil
}
