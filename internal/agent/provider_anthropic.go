package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/metrics"

	"go.uber.org/zap"
)

// AnthropicProvider implements Provider for Anthropic Messages API.
type AnthropicProvider struct {
	baseURL string
	apiKey  string
	model   string
	maxTok  int
	client  *http.Client
}

func NewAnthropicProvider(cfg *config.LLMConfig) *AnthropicProvider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	zap.S().Infow("anthropic provider created",
		"base_url", baseURL,
		"model", cfg.Model,
		"has_key", cfg.APIKey != "",
	)

	return &AnthropicProvider{
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		maxTok:  cfg.MaxTokens,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *AnthropicProvider) Type() ProviderType { return ProviderAnthropic }

// ---- API types ----

type anthroMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // []contentBlock
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`  // DeepSeek reasoning blocks
	Signature string          `json:"signature,omitempty"` // DeepSeek thinking signature
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthroTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthroReq struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []anthroMsg  `json:"messages"`
	Tools     []anthroTool `json:"tools,omitempty"`
	Stream    bool         `json:"stream,omitempty"`
}

type anthroResp struct {
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// ---- Provider interface ----

func (p *AnthropicProvider) Complete(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
	llmRequests.Inc()
	timer := metrics.StartTimer(llmDuration)
	defer timer.Stop()

	body, err := json.Marshal(p.buildReq(req, false))
	if err != nil {
		llmErrors.Inc()
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		llmErrors.Inc()
		return nil, fmt.Errorf("http req: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		llmErrors.Inc()
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		llmErrors.Inc()
		return nil, fmt.Errorf("anthropic error status=%d body=%s", resp.StatusCode, string(rawBody))
	}

	var anthResp anthroResp
	if err := json.Unmarshal(rawBody, &anthResp); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	pr := &ProviderResponse{
		StopReason: anthResp.StopReason,
	}
	if anthResp.Usage != nil {
		pr.Usage = &Usage{
			InputTokens:  anthResp.Usage.InputTokens,
			OutputTokens: anthResp.Usage.OutputTokens,
		}
		llmInputTokens.Add(int64(anthResp.Usage.InputTokens))
		llmOutputTokens.Add(int64(anthResp.Usage.OutputTokens))
	}

	// Preserve all content blocks (text, tool_use, thinking, etc.)
	// DeepSeek requires thinking blocks to be passed back
	var outBlocks []map[string]any
	for _, cb := range anthResp.Content {
		switch cb.Type {
		case "text":
			outBlocks = append(outBlocks, map[string]any{
				"type": "text",
				"text": cb.Text,
			})
		case "thinking":
			b := map[string]any{
				"type":     "thinking",
				"thinking": cb.Text,
			}
			if cb.Signature != "" {
				b["signature"] = cb.Signature
			}
			outBlocks = append(outBlocks, b)
		case "tool_use":
			outBlocks = append(outBlocks, map[string]any{
				"type":  "tool_use",
				"id":    cb.ID,
				"name":  cb.Name,
				"input": cb.Input,
			})
			pr.ToolCalls = append(pr.ToolCalls, ToolCall{
				ID:        cb.ID,
				Name:      cb.Name,
				Arguments: cb.Input,
			})
		}
	}

	if len(outBlocks) == 0 {
		pr.Content = TextContent("")
	} else {
		pr.Content, _ = json.Marshal(outBlocks)
	}

	return pr, nil
}

func (p *AnthropicProvider) CompleteStream(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error) {
	llmRequests.Inc()
	timer := metrics.StartTimer(llmDuration)

	body, err := json.Marshal(p.buildReq(req, true))
	if err != nil {
		llmErrors.Inc()
		timer.Stop()
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		llmErrors.Inc()
		timer.Stop()
		return nil, fmt.Errorf("http req: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		llmErrors.Inc()
		timer.Stop()
		return nil, fmt.Errorf("http: %w", err)
	}

	// Check HTTP status before starting the streaming goroutine so errors propagate to caller
	if resp.StatusCode != 200 {
		llmErrors.Inc()
		timer.Stop()
		rawBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic error status=%d body=%s", resp.StatusCode, string(rawBody))
	}

	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		defer timer.Stop()

		// Track the current content block type per index for thinking/text disambiguation
		blockTypes := make(map[int]string)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" || strings.HasPrefix(line, "event: ") {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- StreamChunk{Done: true}
				return
			}

			var evt struct {
				Type         string          `json:"type"`
				Index        int             `json:"index"`
				ContentBlock *contentBlock   `json:"content_block,omitempty"`
				Delta        json.RawMessage `json:"delta,omitempty"`
				Usage        *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "content_block_start":
				if evt.ContentBlock != nil {
					blockTypes[evt.Index] = evt.ContentBlock.Type
					if evt.ContentBlock.Type == "tool_use" {
						ch <- StreamChunk{
							ToolCallBegin: &ToolCallBegin{
								ID:   evt.ContentBlock.ID,
								Name: evt.ContentBlock.Name,
							},
						}
					}
				}
			case "content_block_delta":
				if evt.Delta != nil {
					var delta struct {
						Type        string `json:"type"`
						Text        string `json:"text,omitempty"`
						Thinking    string `json:"thinking,omitempty"`
						PartialJSON string `json:"partial_json,omitempty"`
					}
					json.Unmarshal(evt.Delta, &delta)
					switch delta.Type {
					case "text_delta":
						ch <- StreamChunk{Content: TextContent(delta.Text)}
					case "thinking_delta":
						ch <- StreamChunk{BlockDelta: delta.Thinking, DeltaType: "thinking"}
					case "input_json_delta":
						ch <- StreamChunk{ToolCallDelta: delta.PartialJSON}
					}
				}
			case "message_delta":
				if evt.Usage != nil {
					ch <- StreamChunk{
						Usage: &Usage{
							InputTokens:  evt.Usage.InputTokens,
							OutputTokens: evt.Usage.OutputTokens,
						},
					}
				}
				// Capture thinking signature from message_delta (DeepSeek requires passing it back)
				if evt.Delta != nil {
					var deltaMsg struct {
						Thinking *struct {
							Signature string `json:"signature"`
						} `json:"thinking"`
					}
					if err := json.Unmarshal(evt.Delta, &deltaMsg); err == nil && deltaMsg.Thinking != nil {
						ch <- StreamChunk{BlockSignature: deltaMsg.Thinking.Signature}
					}
				}
			case "message_stop":
				ch <- StreamChunk{Done: true}
				return
			}
		}
		ch <- StreamChunk{Done: true}
	}()
	return ch, nil
}

// ---- helpers ----

func (p *AnthropicProvider) buildReq(req ProviderRequest, stream bool) anthroReq {
	ar := anthroReq{
		Model:     p.model,
		MaxTokens: p.maxTok,
		System:    req.System,
		Stream:    stream,
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			ar.Messages = append(ar.Messages, anthroMsg{Role: "user", Content: msg.Content})
		case "assistant":
			ar.Messages = append(ar.Messages, anthroMsg{Role: "assistant", Content: msg.Content})
		case "tool":
			// Convert tool role to user with tool_result blocks
			ar.Messages = append(ar.Messages, anthroMsg{Role: "user", Content: msg.Content})
		}
	}

	for _, t := range req.Tools {
		ar.Tools = append(ar.Tools, anthroTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return ar
}

func (p *AnthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}
