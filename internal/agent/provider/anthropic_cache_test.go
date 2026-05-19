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
	// Mock server returning a non-streaming response with cache tokens
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

func TestAnthropicCompleteCacheTokensZero(t *testing.T) {
	// No cache tokens in response
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
	// Mock SSE stream where message_start carries cache tokens
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// message_start with cache tokens
		fmt.Fprint(w, "event: message_start\ndata: ")
		fmt.Fprint(w, `{"type":"message_start","message":{"usage":{"input_tokens":2000,"output_tokens":0,"cache_read_input_tokens":1200}}}`)
		fmt.Fprint(w, "\n\n")

		// content_block_start, delta, and stop
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

	var foundUsage *Usage
	for c := range ch {
		if c.Done {
			break
		}
		if c.Usage != nil {
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

func TestAnthropicStreamMessageDeltaCache(t *testing.T) {
	// Mock SSE stream where message_start has no usage struct and
	// message_delta carries the final usage with cache tokens
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// message_start with NO usage (anthropic proxy may omit it)
		fmt.Fprint(w, "event: message_start\ndata: ")
		fmt.Fprint(w, `{"type":"message_start","message":{"usage":{"input_tokens":0,"output_tokens":0}}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_start\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_delta\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Berlin"}}`)
		fmt.Fprint(w, "\n\n")

		// message_delta carries the real usage with cache tokens
		fmt.Fprint(w, "event: message_delta\ndata: ")
		fmt.Fprint(w, `{"type":"message_delta","usage":{"input_tokens":1800,"output_tokens":30,"cache_read_input_tokens":900}}`)
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
			{Role: "user", Content: json.RawMessage(`"What is the capital of Germany?"`)},
		},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var foundUsage *Usage
	for c := range ch {
		if c.Done {
			break
		}
		if c.Usage != nil {
			foundUsage = c.Usage
		}
	}

	if foundUsage == nil {
		t.Fatal("no Usage found in stream")
	}
	if foundUsage.InputTokens != 1800 {
		t.Errorf("InputTokens = %d, want 1800", foundUsage.InputTokens)
	}
	if foundUsage.CachedInputTokens != 900 {
		t.Errorf("CachedInputTokens = %d, want 900", foundUsage.CachedInputTokens)
	}
	if foundUsage.MissedInputTokens != 900 {
		t.Errorf("MissedInputTokens = %d, want 900", foundUsage.MissedInputTokens)
	}
	if foundUsage.CachedInputTokens+foundUsage.MissedInputTokens != foundUsage.InputTokens {
		t.Errorf("cache+miss (%d) != input (%d)",
			foundUsage.CachedInputTokens+foundUsage.MissedInputTokens, foundUsage.InputTokens)
	}
}

func TestAnthropicStreamCacheGrowsAcrossTurns(t *testing.T) {
	// Simulate a multi-turn conversation. Turn 1 has no cache (first request).
	// Turn 2 should have growing cache as the prefix is established.
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		turn++

		var cached, missed, output int
		switch turn {
		case 1:
			cached, missed, output = 0, 2000, 20
		case 2:
			cached, missed, output = 1200, 800, 20
		case 3:
			cached, missed, output = 1800, 400, 20
		default:
			cached, missed, output = 2000, 200, 20
		}

		fmt.Fprint(w, "event: message_start\ndata: ")
		fmt.Fprintf(w, `{"type":"message_start","message":{"usage":{"input_tokens":%d,"output_tokens":0,"cache_read_input_tokens":%d}}}`, cached+missed, cached)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_start\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: content_block_delta\ndata: ")
		fmt.Fprint(w, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`)
		fmt.Fprint(w, "\n\n")

		fmt.Fprint(w, "event: message_delta\ndata: ")
		fmt.Fprintf(w, `{"type":"message_delta","usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d}}`, cached+missed, output, cached)
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

	ctx := context.Background()
	system := strings.Repeat("Rule: be helpful. ", 200)
	var lastUsage *Usage

	for i := 0; i < 3; i++ {
		ch, err := p.CompleteStream(ctx, ProviderRequest{
			System: system,
			Messages: []Message{
				{Role: "user", Content: json.RawMessage(`"question"`)},
			},
		})
		if err != nil {
			t.Fatalf("Turn %d CompleteStream error: %v", i+1, err)
		}

		for c := range ch {
			if c.Done {
				break
			}
			if c.Usage != nil {
				lastUsage = c.Usage
			}
		}
	}

	// After 3 turns, cache tokens should have grown
	if lastUsage == nil {
		t.Fatal("no Usage found")
	}
	t.Logf("Final turn: in=%d cache=%d miss=%d",
		lastUsage.InputTokens, lastUsage.CachedInputTokens, lastUsage.MissedInputTokens)
	if lastUsage.CachedInputTokens <= 0 {
		t.Error("expected cache > 0 after 3 turns, got 0")
	}
	if lastUsage.CachedInputTokens+lastUsage.MissedInputTokens != lastUsage.InputTokens {
		t.Errorf("cache+miss (%d) != input (%d)",
			lastUsage.CachedInputTokens+lastUsage.MissedInputTokens, lastUsage.InputTokens)
	}
}

func TestAnthropicStreamNoUsage(t *testing.T) {
	// Edge case: no usage at all in the stream (should not panic)
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
	// Should not panic; usage may or may not appear depending on parsing
	t.Logf("usage events: %d", usageCount)
}
