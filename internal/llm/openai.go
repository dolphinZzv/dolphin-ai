package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"dolphin/internal/types"
	"go.uber.org/zap"
)

func init() {
	RegisterProvider("openai", func(cfg Config, logger *zap.Logger) Provider {
		return &openAIProvider{cfg: cfg, logger: logger}
	})
}

type openAIProvider struct {
	cfg    Config
	logger *zap.Logger
}

func (p *openAIProvider) Name() string { return "openai" }

func (p *openAIProvider) Models(ctx context.Context) ([]ModelConfig, error) {
	if len(p.cfg.Models) > 0 {
		return p.cfg.Models, nil
	}
	return []ModelConfig{
		{
			Name:        p.cfg.Model,
			Provider:    "openai",
			Model:       p.cfg.Model,
			MaxTokens:   p.cfg.MaxTokens,
			Temperature: p.cfg.Temperature,
		},
	}, nil
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	TopP        float64         `json:"top_p,omitempty"`
	MaxTokens   int             `json:"max_tokens"`
	Stream      bool            `json:"stream"`
	Stop        []string        `json:"stop,omitempty"`
	Tools       []openAITool    `json:"tools,omitempty"`
}

type openAIError struct {
	Message string `json:"message"`
}

type openAIErrorBody struct {
	Error openAIError `json:"error"`
}

func (p *openAIProvider) CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	ch := make(chan LLMChunk)

	baseURL := p.cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	msgs := p.buildOpenAIMessages(req)

	body := openAIRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: p.cfg.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
		Stop:        req.Stop,
	}
	if len(req.Tools) > 0 {
		body.Tools = buildOpenAITools(req.Tools)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	p.logger.Debug("openai request body",
		zap.String("body", string(data)),
	)

	go func() {
		defer close(ch)

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(data))
		if err != nil {
			ch <- LLMChunk{Error: fmt.Errorf("llm: create request: %w", err)}
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
		for k, v := range p.cfg.Headers {
			httpReq.Header.Set(k, v)
		}

		cl := &http.Client{Timeout: p.cfg.Timeout}
		resp, err := cl.Do(httpReq)
		if err != nil {
			ch <- LLMChunk{Error: fmt.Errorf("llm: request failed: %w", err)}
			return
		}
		defer resp.Body.Close()

		cleanup := context.AfterFunc(ctx, func() { resp.Body.Close() })
		defer cleanup()

		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(resp.Body)
			var apiErr openAIErrorBody
			if json.Unmarshal(errBody, &apiErr) == nil && apiErr.Error.Message != "" {
				ch <- LLMChunk{Error: fmt.Errorf("llm: %s (status %d)", apiErr.Error.Message, resp.StatusCode)}
			} else {
				ch <- LLMChunk{Error: fmt.Errorf("llm: status %d", resp.StatusCode)}
			}
			return
		}

		dec := NewStreamDecoder(resp.Body)
		for {
			chunk, err := dec.Decode()
			if err == io.EOF {
				ch <- LLMChunk{Done: true}
				return
			}
			if err != nil {
				ch <- LLMChunk{Error: fmt.Errorf("llm: decode: %w", err)}
				return
			}
			ch <- chunk
		}
	}()

	return ch, nil
}

func (p *openAIProvider) buildOpenAIMessages(req LLMRequest) []openAIMessage {
	var msgs []openAIMessage
	if req.System != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case types.RoleTool:
			msgs = append(msgs, openAIMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			})
		case types.RoleAssistant:
			p.logger.Info("llm build assistant msg",
				zap.Bool("has_thinking", m.Thinking != ""),
				zap.Int("thinking_len", len(m.Thinking)),
				zap.Bool("has_signature", m.ThinkingSignature != ""),
				zap.Bool("has_content", m.Content != ""),
				zap.Int("tool_calls", len(m.ToolCalls)),
			)
			if len(m.ToolCalls) > 0 {
				tcs := make([]openAIToolCall, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					tcs[i] = openAIToolCall{
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
				msgs = append(msgs, openAIMessage{
					Role:      "assistant",
					Content:   nil,
					ToolCalls: tcs,
				})
			} else {
				msgs = append(msgs, openAIMessage{Role: "assistant", Content: m.Content})
			}
		default:
			msgs = append(msgs, openAIMessage{Role: string(m.Role), Content: m.Content})
		}
	}
	return msgs
}

func defaultSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return schema
}

func buildOpenAITools(tools []types.ToolDef) []openAITool {
	out := make([]openAITool, len(tools))
	for i, t := range tools {
		out[i] = openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  defaultSchema(t.Schema),
			},
		}
	}
	return out
}
