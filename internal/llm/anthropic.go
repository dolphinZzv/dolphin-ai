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
	RegisterProvider("anthropic", func(cfg Config, logger *zap.Logger) Provider {
		return &anthropicProvider{cfg: cfg, logger: logger}
	})
}

type anthropicProvider struct {
	cfg    Config
	logger *zap.Logger
}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) Models(ctx context.Context) ([]ModelConfig, error) {
	if len(p.cfg.Models) > 0 {
		return p.cfg.Models, nil
	}
	return []ModelConfig{
		{
			Name:        p.cfg.Model,
			Provider:    "anthropic",
			Model:       p.cfg.Model,
			MaxTokens:   p.cfg.MaxTokens,
			Temperature: p.cfg.Temperature,
		},
	}, nil
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	TopP        float64            `json:"top_p,omitempty"`
	Stream      bool               `json:"stream"`
	Stop        []string           `json:"stop_sequences,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicError struct {
	Message string `json:"message"`
}

type anthropicErrorBody struct {
	Error anthropicError `json:"error"`
}

func (p *anthropicProvider) CompleteStream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	ch := make(chan LLMChunk)

	baseURL := p.cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	msgs := p.buildAnthropicMessages(req)

	body := anthropicRequest{
		Model:       req.Model,
		Messages:    msgs,
		System:      req.System,
		MaxTokens:   req.MaxTokens,
		Temperature: p.cfg.Temperature,
		TopP:        req.TopP,
		Stream:      true,
		Stop:        req.Stop,
	}
	if len(req.Tools) > 0 {
		body.Tools = buildAnthropicTools(req.Tools)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	p.logger.Debug("anthropic request body",
		zap.String("body", string(data)),
	)

	go func() {
		defer close(ch)

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(data))
		if err != nil {
			ch <- LLMChunk{Error: fmt.Errorf("llm: create request: %w", err)}
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", p.cfg.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		for k, v := range p.cfg.Headers {
			httpReq.Header.Set(k, v)
		}
		p.logger.Debug("anthropic request",
			zap.String("url", baseURL+"/v1/messages"),
			zap.Bool("has_api_key", p.cfg.APIKey != ""),
			zap.Int("api_key_len", len(p.cfg.APIKey)),
			zap.String("model", req.Model),
		)

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
			var apiErr anthropicErrorBody
			if json.Unmarshal(errBody, &apiErr) == nil && apiErr.Error.Message != "" {
				bodyPreview := string(data)
				if len(bodyPreview) > 2000 {
					bodyPreview = bodyPreview[:2000] + "..."
				}
				ch <- LLMChunk{Error: fmt.Errorf("llm: %s (status %d)\nrequest: %s", apiErr.Error.Message, resp.StatusCode, bodyPreview)}
			} else {
				ch <- LLMChunk{Error: fmt.Errorf("llm: status %d", resp.StatusCode)}
			}
			return
		}

		dec := NewAnthropicDecoder(resp.Body)
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

func (p *anthropicProvider) buildAnthropicMessages(req LLMRequest) []anthropicMessage {
	var msgs []anthropicMessage
	for _, m := range req.Messages {
		switch m.Role {
		case types.RoleTool:
			blocks := []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				},
			}
			data, _ := json.Marshal(blocks)
			msgs = append(msgs, anthropicMessage{Role: "user", Content: data})

		case types.RoleAssistant:
			p.logger.Info("llm build assistant msg",
				zap.Bool("has_thinking", m.Thinking != ""),
				zap.Int("thinking_len", len(m.Thinking)),
				zap.Bool("has_signature", m.ThinkingSignature != ""),
				zap.Bool("has_content", m.Content != ""),
				zap.Int("tool_calls", len(m.ToolCalls)),
			)
			if m.Thinking != "" || len(m.ToolCalls) > 0 {
				var blocks []map[string]any
				if m.Thinking != "" {
					block := map[string]any{"type": "thinking", "thinking": m.Thinking}
					if m.ThinkingSignature != "" {
						block["signature"] = m.ThinkingSignature
					}
					blocks = append(blocks, block)
				}
				if m.Content != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
				}
				for _, tc := range m.ToolCalls {
					var input any = map[string]any{}
					if tc.Arguments != "" {
						json.Unmarshal([]byte(tc.Arguments), &input)
					}
					blocks = append(blocks, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": input,
					})
				}
				data, _ := json.Marshal(blocks)
				msgs = append(msgs, anthropicMessage{Role: "assistant", Content: data})
			} else {
				data, _ := json.Marshal(m.Content)
				msgs = append(msgs, anthropicMessage{Role: "assistant", Content: data})
			}

		default:
			data, _ := json.Marshal(m.Content)
			msgs = append(msgs, anthropicMessage{Role: string(m.Role), Content: data})
		}
	}
	return msgs
}

func buildAnthropicTools(tools []types.ToolDef) []anthropicTool {
	out := make([]anthropicTool, len(tools))
	for i, t := range tools {
		out[i] = anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: defaultSchema(t.Schema),
		}
	}
	return out
}
