package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicCompleteCacheTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"content": [{"type":"text","text":"Paris"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 1500, "output_tokens": 50, "cache_read_input_tokens": 1000}
		}`)
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		baseURL: srv.URL,
		apiKey:  "sk-test",
		model:   "deepseek-v4-pro",
		maxTok:  1024,
		client:  http.DefaultClient,
	}

	resp, err := p.Complete(context.Background(), ProviderRequest{
		System: "You are helpful.",
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.InputTokens != 1500 {
		t.Errorf("InputTokens = %d, want 1500", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", resp.Usage.OutputTokens)
	}
	if resp.Usage.CachedInputTokens != 1000 {
		t.Errorf("CachedInputTokens = %d, want 1000", resp.Usage.CachedInputTokens)
	}
	if resp.Usage.MissedInputTokens != 500 {
		t.Errorf("MissedInputTokens = %d, want 500", resp.Usage.MissedInputTokens)
	}
}

func TestAnthropicCompleteCacheClamp(t *testing.T) {
	// DeepSeek's anthropic API may return cache_read_input_tokens > input_tokens
	// (representing total cached prefix size). The provider must clamp it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"content": [{"type":"text","text":"Paris"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 117, "output_tokens": 46, "cache_read_input_tokens": 4096}
		}`)
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		baseURL: srv.URL,
		apiKey:  "sk-test",
		model:   "deepseek-v4-pro",
		maxTok:  1024,
		client:  http.DefaultClient,
	}

	resp, err := p.Complete(context.Background(), ProviderRequest{
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	// cached must be clamped to input_tokens (117), not 4096
	if resp.Usage.CachedInputTokens != 117 {
		t.Errorf("CachedInputTokens = %d, want 117 (clamped)", resp.Usage.CachedInputTokens)
	}
	if resp.Usage.MissedInputTokens != 0 {
		t.Errorf("MissedInputTokens = %d, want 0", resp.Usage.MissedInputTokens)
	}
	if resp.Usage.CachedInputTokens+resp.Usage.MissedInputTokens != resp.Usage.InputTokens {
		t.Errorf("cache+miss (%d) != input (%d)",
			resp.Usage.CachedInputTokens+resp.Usage.MissedInputTokens, resp.Usage.InputTokens)
	}
}

func TestAnthropicCompleteCacheTokensZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"content": [{"type":"text","text":"Paris"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 500, "output_tokens": 10, "cache_read_input_tokens": 0}
		}`)
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		baseURL: srv.URL,
		apiKey:  "sk-test",
		model:   "deepseek-v4-pro",
		maxTok:  1024,
		client:  http.DefaultClient,
	}

	resp, err := p.Complete(context.Background(), ProviderRequest{
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if resp.Usage.CachedInputTokens != 0 {
		t.Errorf("CachedInputTokens = %d, want 0", resp.Usage.CachedInputTokens)
	}
	if resp.Usage.MissedInputTokens != 500 {
		t.Errorf("MissedInputTokens = %d, want 500", resp.Usage.MissedInputTokens)
	}
}

func TestAnthropicStreamMessageStartCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w, "event: message_start\ndata: ")
		fmt.Fprint(w, `{"type":"message_start","message":{"usage":{"input_tokens":2000,"output_tokens":0,"cache_read_input_tokens":1200}}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_start\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_delta\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Paris"}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: message_stop\ndata: ")
		fmt.Fprint(w, `{"type":"message_stop"}`)
		fmt.Fprint(w, "\n\n")
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		baseURL: srv.URL,
		apiKey:  "sk-test",
		model:   "deepseek-v4-pro",
		maxTok:  1024,
		client:  http.DefaultClient,
	}

	ch, err := p.CompleteStream(context.Background(), ProviderRequest{
		System: strings.Repeat("Rule: be helpful. ", 200),
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What is the capital of France?"`)},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	// Find the last Usage chunk from message_start (message_delta has no cache info)
	var foundUsage *Usage
	for c := range ch {
		if c.Done {
			break
		}
		if c.Usage != nil && c.Usage.InputTokens > 0 {
			foundUsage = c.Usage
		}
	}

	if foundUsage == nil {
		t.Fatal("no Usage found in stream")
	}
	if foundUsage.InputTokens != 2000 {
		t.Errorf("InputTokens = %d, want 2000", foundUsage.InputTokens)
	}
	if foundUsage.CachedInputTokens != 1200 {
		t.Errorf("CachedInputTokens = %d, want 1200", foundUsage.CachedInputTokens)
	}
	if foundUsage.MissedInputTokens != 800 {
		t.Errorf("MissedInputTokens = %d, want 800", foundUsage.MissedInputTokens)
	}
	if foundUsage.CachedInputTokens+foundUsage.MissedInputTokens != foundUsage.InputTokens {
		t.Errorf("cache+miss (%d) != input (%d)",
			foundUsage.CachedInputTokens+foundUsage.MissedInputTokens, foundUsage.InputTokens)
	}
}

func TestAnthropicStreamMessageStartCacheClamp(t *testing.T) {
	// DeepSeek may send cache_read_input_tokens > input_tokens in message_start too
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// cache_read_input_tokens (4096) > input_tokens (117)
		fmt.Fprint(w, "event: message_start\ndata: ")
		fmt.Fprint(w, `{"type":"message_start","message":{"usage":{"input_tokens":117,"output_tokens":0,"cache_read_input_tokens":4096}}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_start\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_delta\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Paris"}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: message_delta\ndata: ")
		fmt.Fprint(w, `{"type":"message_delta","usage":{"output_tokens":46}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: message_stop\ndata: ")
		fmt.Fprint(w, `{"type":"message_stop"}`)
		fmt.Fprint(w, "\n\n")
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		baseURL: srv.URL,
		apiKey:  "sk-test",
		model:   "deepseek-v4-pro",
		maxTok:  1024,
		client:  http.DefaultClient,
	}

	ch, err := p.CompleteStream(context.Background(), ProviderRequest{
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var msgStartUsage *Usage
	for c := range ch {
		if c.Done {
			break
		}
		if c.Usage != nil && c.Usage.InputTokens > 0 {
			msgStartUsage = c.Usage
		}
	}

	if msgStartUsage == nil {
		t.Fatal("no message_start Usage found")
	}
	// cached must be clamped to input_tokens
	if msgStartUsage.CachedInputTokens != 117 {
		t.Errorf("CachedInputTokens = %d, want 117 (clamped)", msgStartUsage.CachedInputTokens)
	}
	if msgStartUsage.MissedInputTokens != 0 {
		t.Errorf("MissedInputTokens = %d, want 0", msgStartUsage.MissedInputTokens)
	}
	if msgStartUsage.CachedInputTokens+msgStartUsage.MissedInputTokens != msgStartUsage.InputTokens {
		t.Errorf("cache+miss (%d) != input (%d)",
			msgStartUsage.CachedInputTokens+msgStartUsage.MissedInputTokens, msgStartUsage.InputTokens)
	}
}

func TestAnthropicStreamMessageDeltaOnlyOutput(t *testing.T) {
	// Verify message_delta only sends OutputTokens (per Anthropic spec),
	// and that merging message_start + message_delta gives correct final usage.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// message_start with full usage
		fmt.Fprint(w, "event: message_start\ndata: ")
		fmt.Fprint(w, `{"type":"message_start","message":{"usage":{"input_tokens":2000,"output_tokens":0,"cache_read_input_tokens":1200}}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_start\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_delta\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Berlin"}}`)
		fmt.Fprint(w, "\n\n")

		// message_delta only has output_tokens
		fmt.Fprint(w, "event: message_delta\ndata: ")
		fmt.Fprint(w, `{"type":"message_delta","usage":{"output_tokens":30}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: message_stop\ndata: ")
		fmt.Fprint(w, `{"type":"message_stop"}`)
		fmt.Fprint(w, "\n\n")
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		baseURL: srv.URL,
		apiKey:  "sk-test",
		model:   "deepseek-v4-pro",
		maxTok:  1024,
		client:  http.DefaultClient,
	}

	ch, err := p.CompleteStream(context.Background(), ProviderRequest{
		System: "Be helpful.",
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	// Collect all Usage chunks
	var usages []*Usage
	for c := range ch {
		if c.Done {
			break
		}
		if c.Usage != nil {
			usages = append(usages, c.Usage)
		}
	}

	if len(usages) == 0 {
		t.Fatal("no Usage chunks found")
	}

	// Should have at least 2: one from message_start, one from message_delta
	if len(usages) < 2 {
		t.Fatalf("expected >=2 Usage chunks, got %d", len(usages))
	}

	// message_start Usage: has input+cache, output=0
	startUsage := usages[0]
	if startUsage.InputTokens != 2000 {
		t.Errorf("start InputTokens = %d, want 2000", startUsage.InputTokens)
	}
	if startUsage.CachedInputTokens != 1200 {
		t.Errorf("start CachedInputTokens = %d, want 1200", startUsage.CachedInputTokens)
	}
	if startUsage.MissedInputTokens != 800 {
		t.Errorf("start MissedInputTokens = %d, want 800", startUsage.MissedInputTokens)
	}

	// message_delta Usage: only output
	deltaUsage := usages[len(usages)-1]
	if deltaUsage.InputTokens != 0 {
		t.Errorf("delta InputTokens = %d, want 0", deltaUsage.InputTokens)
	}
	if deltaUsage.OutputTokens != 30 {
		t.Errorf("delta OutputTokens = %d, want 30", deltaUsage.OutputTokens)
	}
	if deltaUsage.CachedInputTokens != 0 {
		t.Errorf("delta CachedInputTokens = %d, want 0", deltaUsage.CachedInputTokens)
	}
	if deltaUsage.MissedInputTokens != 0 {
		t.Errorf("delta MissedInputTokens = %d, want 0", deltaUsage.MissedInputTokens)
	}
}

func TestAnthropicStreamNoUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w, "event: message_start\ndata: ")
		fmt.Fprint(w, `{"type":"message_start","message":{"usage":null}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_start\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: message_stop\ndata: ")
		fmt.Fprint(w, `{"type":"message_stop"}`)
		fmt.Fprint(w, "\n\n")
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		baseURL: srv.URL,
		apiKey:  "sk-test",
		model:   "deepseek-v4-pro",
		maxTok:  1024,
		client:  http.DefaultClient,
	}

	ch, err := p.CompleteStream(context.Background(), ProviderRequest{
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hi"`)},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var usageCount int
	for c := range ch {
		if c.Done {
			break
		}
		if c.Usage != nil {
			usageCount++
		}
	}
	t.Logf("usage events: %d", usageCount)
}
