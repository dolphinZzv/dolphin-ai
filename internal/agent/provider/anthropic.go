package provider

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
	name    string
	client  *http.Client
}

func NewAnthropicProvider(cfg *config.ProviderConfig) *AnthropicProvider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	zap.S().Infow("anthropic provider created",
		"name", cfg.Name,
		"base_url", baseURL,
		"model", cfg.Model,
		"has_key", cfg.APIKey != "",
	)

	return &AnthropicProvider{
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		maxTok:  cfg.MaxTokens,
		name:    cfg.Name,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *AnthropicProvider) Type() ProviderType { return ProviderAnthropic }
func (p *AnthropicProvider) Name() string       { return p.name }

func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	body := map[string]any{
		"model":      p.model,
		"max_tokens": 1,
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	p.setHeaders(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status=%d", resp.StatusCode)
	}
	return nil
}

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
		InputTokens          int `json:"input_tokens"`
		OutputTokens         int `json:"output_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// ---- Provider interface ----

func (p *AnthropicProvider) Complete(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
	llmRequests.With("anthropic").Inc()
	timer := metrics.StartTimer(llmDuration.With("anthropic"))
	defer timer.Stop()

	body, err := json.Marshal(p.buildReq(req, false))
	if err != nil {
		llmErrors.With("anthropic").Inc()
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		llmErrors.With("anthropic").Inc()
		return nil, fmt.Errorf("http req: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		llmErrors.With("anthropic").Inc()
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		llmErrors.With("anthropic").Inc()
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
		cached := anthResp.Usage.CacheReadInputTokens
		llmCacheHitTokens.With("anthropic").Add(int64(cached))
		llmCacheMissTokens.With("anthropic").Add(int64(anthResp.Usage.InputTokens - cached))
		pr.Usage = &Usage{
			InputTokens:       anthResp.Usage.InputTokens,
			OutputTokens:      anthResp.Usage.OutputTokens,
			CachedInputTokens: cached,
			MissedInputTokens: anthResp.Usage.InputTokens - cached,
		}
		llmInputTokens.With("anthropic").Add(int64(anthResp.Usage.InputTokens))
		llmOutputTokens.With("anthropic").Add(int64(anthResp.Usage.OutputTokens))
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
	llmRequests.With("anthropic").Inc()
	timer := metrics.StartTimer(llmDuration.With("anthropic"))

	body, err := json.Marshal(p.buildReq(req, true))
	if err != nil {
		llmErrors.With("anthropic").Inc()
		timer.Stop()
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		llmErrors.With("anthropic").Inc()
		timer.Stop()
		return nil, fmt.Errorf("http req: %w", err)
	}
	p.setHeaders(httpReq)

	resp, err := p.client.Do(httpReq) //nolint:bodyclose
	if err != nil {
		llmErrors.With("anthropic").Inc()
		timer.Stop()
		if resp != nil {
			resp.Body.Close()
		}
		return nil, fmt.Errorf("http: %w", err)
	}

	// Check HTTP status before starting the streaming goroutine so errors propagate to caller
	if resp.StatusCode != 200 {
		llmErrors.With("anthropic").Inc()
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
					InputTokens          int `json:"input_tokens"`
					OutputTokens         int `json:"output_tokens"`
					CacheReadInputTokens int `json:"cache_read_input_tokens"`
				} `json:"usage,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "message_start":
				// message_start carries input_tokens inside message.usage
				var msgStart struct {
					Message *struct {
						Usage *struct {
							InputTokens          int `json:"input_tokens"`
							OutputTokens         int `json:"output_tokens"`
							CacheReadInputTokens int `json:"cache_read_input_tokens"`
						} `json:"usage"`
					} `json:"message"`
				}
				if err := json.Unmarshal([]byte(data), &msgStart); err == nil && msgStart.Message != nil && msgStart.Message.Usage != nil {
					ch <- StreamChunk{
						Usage: &Usage{
							InputTokens:       msgStart.Message.Usage.InputTokens,
							OutputTokens:      msgStart.Message.Usage.OutputTokens,
							CachedInputTokens: msgStart.Message.Usage.CacheReadInputTokens,
							MissedInputTokens: msgStart.Message.Usage.InputTokens - msgStart.Message.Usage.CacheReadInputTokens,
						},
					}
				}
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
					if err := json.Unmarshal(evt.Delta, &delta); err != nil {
						zap.S().Debugw("failed to unmarshal streaming delta", "error", err)
						continue
					}
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
							InputTokens:       evt.Usage.InputTokens,
							OutputTokens:      evt.Usage.OutputTokens,
							CachedInputTokens: evt.Usage.CacheReadInputTokens,
							MissedInputTokens: evt.Usage.InputTokens - evt.Usage.CacheReadInputTokens,
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

	// Defense: ensure tool_use/tool_result pairing before building the request.
	// Session replay can introduce orphaned tool_use blocks when a previous
	// session was interrupted mid-tool-execution.
	msgs := SanitizeToolPairing(req.Messages)

	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		switch msg.Role {
		case "user":
			ar.Messages = append(ar.Messages, anthroMsg{Role: "user", Content: msg.Content})
		case "assistant":
			ar.Messages = append(ar.Messages, anthroMsg{Role: "assistant", Content: msg.Content})
		case "tool":
			// Collect all consecutive tool results into a single user message.
			// The Anthropic API requires each tool_use to have a corresponding
			// tool_result in the very next message, and all tool_results for a
			// turn must be in one user message — consecutive user messages are invalid.
			var blocks []json.RawMessage
			for i < len(msgs) && msgs[i].Role == "tool" {
				var toolBlocks []json.RawMessage
				if err := json.Unmarshal(msgs[i].Content, &toolBlocks); err == nil {
					blocks = append(blocks, toolBlocks...)
				}
				i++
			}
			i-- // outer loop will advance past last tool message
			merged, _ := json.Marshal(blocks)
			ar.Messages = append(ar.Messages, anthroMsg{Role: "user", Content: merged})
		}
	}

	for _, t := range req.Tools {
		ar.Tools = append(ar.Tools, anthroTool(t))
	}
	return ar
}

func (p *AnthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}
