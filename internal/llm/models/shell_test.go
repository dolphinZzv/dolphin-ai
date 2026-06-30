package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dolphin/internal/llm"
	"dolphin/internal/types"
)

func TestNewAnthropicProvider_CompleteStream(t *testing.T) {
	var gotHeaders http.Header
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":          "msg_test",
			"type":        "message",
			"role":        "assistant",
			"content":     []any{map[string]any{"type": "text", "text": "Hello from Anthropic"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := llm.Config{
		Provider: "test-vendor",
		APIType:  "anthropic",
		BaseURL:  server.URL,
		APIKey:   "sk-test",
		Models:   []llm.ModelConfig{{Name: "test-model"}},
	}

	factory := NewAnthropicProvider("test-model")
	p := factory(cfg, noopLogger())

	req := llm.LLMRequest{
		Messages: []types.Message{
			{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("Hello")}},
		},
	}
	ch, err := p.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var chunk llm.LLMChunk
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("chunk error: %v", c.Error)
		}
		chunk = c
	}

	if chunk.Content != "Hello from Anthropic" {
		t.Errorf("Content = %q, want %q", chunk.Content, "Hello from Anthropic")
	}
	if !chunk.Done {
		t.Error("chunk should be marked done")
	}

	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotHeaders.Get("x-api-key") != "sk-test" {
		t.Errorf("x-api-key = %q", gotHeaders.Get("x-api-key"))
	}
	if gotHeaders.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version = %q", gotHeaders.Get("anthropic-version"))
	}
}

func TestNewOpenAIProvider_CompleteStream(t *testing.T) {
	var gotHeaders http.Header
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{"content": "Hello from OpenAI"},
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := llm.Config{
		Provider: "test-vendor",
		APIType:  "openai",
		BaseURL:  server.URL,
		APIKey:   "sk-test",
		Models:   []llm.ModelConfig{{Name: "test-model"}},
	}

	p := NewOpenAIProvider("test-model")(cfg, noopLogger())
	req := llm.LLMRequest{
		Messages: []types.Message{
			{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("Hello")}},
		},
	}
	ch, err := p.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var chunk llm.LLMChunk
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("chunk error: %v", c.Error)
		}
		chunk = c
	}

	if chunk.Content != "Hello from OpenAI" {
		t.Errorf("Content = %q, want %q", chunk.Content, "Hello from OpenAI")
	}
	if !chunk.Done {
		t.Error("chunk should be marked done")
	}

	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotHeaders.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("Authorization = %q", gotHeaders.Get("Authorization"))
	}
}

func TestNewAnthropicProvider_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	cfg := llm.Config{
		Provider: "test-vendor",
		APIType:  "anthropic",
		BaseURL:  server.URL,
		APIKey:   "sk-test",
		Models:   []llm.ModelConfig{{Name: "test-model"}},
	}

	p := NewAnthropicProvider("test-model")(cfg, noopLogger())
	req := llm.LLMRequest{
		Messages: []types.Message{
			{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("Hello")}},
		},
	}
	ch, err := p.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var gotErr error
	for c := range ch {
		if c.Error != nil {
			gotErr = c.Error
		}
	}
	if gotErr == nil {
		t.Fatal("expected error from bad request")
	}
}

func TestNewOpenAIProvider_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	cfg := llm.Config{
		Provider: "test-vendor",
		APIType:  "openai",
		BaseURL:  server.URL,
		APIKey:   "sk-test",
		Models:   []llm.ModelConfig{{Name: "test-model"}},
	}

	p := NewOpenAIProvider("test-model")(cfg, noopLogger())
	req := llm.LLMRequest{
		Messages: []types.Message{
			{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("Hello")}},
		},
	}
	ch, err := p.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	var gotErr error
	for c := range ch {
		if c.Error != nil {
			gotErr = c.Error
		}
	}
	if gotErr == nil {
		t.Fatal("expected error from rate limited")
	}
}

func TestNewAnthropicProvider_CustomHeaders(t *testing.T) {
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":          "msg_h",
			"type":        "message",
			"role":        "assistant",
			"content":     []any{map[string]any{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := llm.Config{
		Provider: "test-vendor",
		APIType:  "anthropic",
		BaseURL:  server.URL,
		APIKey:   "sk-test",
		Headers:  map[string]string{"X-Custom": "section-val"},
		Models: []llm.ModelConfig{{
			Name:    "test-model",
			Headers: map[string]string{"X-Custom": "model-override"},
		}},
	}

	p := NewAnthropicProvider("test-model")(cfg, noopLogger())
	ch, err := p.CompleteStream(context.Background(), llm.LLMRequest{
		Messages: []types.Message{{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("hi")}}},
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}
	for range ch {
	}

	if gotHeaders.Get("X-Custom") != "model-override" {
		t.Errorf("X-Custom = %q, want model-override", gotHeaders.Get("X-Custom"))
	}
}
