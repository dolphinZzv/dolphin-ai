package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"dolphin/internal/config"
)

func TestWebhook_POSTWithBody(t *testing.T) {
	var gotMethod, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := json.Marshal(map[string]string{"message": "hello"})
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
	defer ts.Close()

	tool := NewWebhookTool(config.DefaultConfig())
	input, _ := json.Marshal(map[string]any{
		"url":  ts.URL,
		"body": `{"message":"hello"}`,
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if gotMethod != "POST" {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotBody != `{"message":"hello"}` {
		t.Errorf("expected body %q, got %q", `{"message":"hello"}`, gotBody)
	}
}

func TestWebhook_CustomHeaders(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tool := NewWebhookTool(config.DefaultConfig())
	input, _ := json.Marshal(map[string]any{
		"url":     ts.URL,
		"body":    "test",
		"headers": map[string]string{"Authorization": "Bearer token123"},
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if gotAuth != "Bearer token123" {
		t.Errorf("expected Authorization header, got %q", gotAuth)
	}
}

func TestWebhook_NamedTarget(t *testing.T) {
	var gotMethod, gotHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.MCP.Webhook.Targets = map[string]config.WebhookTarget{
		"my_bot": {
			URL:    ts.URL,
			Method: "PUT",
			Headers: map[string]string{
				"X-Custom": "from-target",
			},
		},
	}
	tool := NewWebhookTool(cfg)
	input, _ := json.Marshal(map[string]any{
		"target": "my_bot",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if gotMethod != "PUT" {
		t.Errorf("expected PUT, got %s", gotMethod)
	}
	if gotHeader != "from-target" {
		t.Errorf("expected X-Custom: from-target, got %q", gotHeader)
	}
}

func TestWebhook_GETMethod(t *testing.T) {
	var gotMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tool := NewWebhookTool(config.DefaultConfig())
	input, _ := json.Marshal(map[string]any{
		"url":    ts.URL,
		"method": "GET",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if gotMethod != "GET" {
		t.Errorf("expected GET, got %s", gotMethod)
	}
}

func TestWebhook_MergeTargetAndInlineHeaders(t *testing.T) {
	var gotXCustom, gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXCustom = r.Header.Get("X-Custom")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := config.DefaultConfig()
	cfg.MCP.Webhook.Targets = map[string]config.WebhookTarget{
		"my_bot": {
			URL: ts.URL,
			Headers: map[string]string{
				"X-Custom": "from-target",
			},
		},
	}
	tool := NewWebhookTool(cfg)
	input, _ := json.Marshal(map[string]any{
		"target":  "my_bot",
		"headers": map[string]string{"Authorization": "Bearer inline"},
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if gotXCustom != "from-target" {
		t.Errorf("expected X-Custom: from-target, got %q", gotXCustom)
	}
	if gotAuth != "Bearer inline" {
		t.Errorf("expected Authorization: Bearer inline, got %q", gotAuth)
	}
}

func TestWebhook_URLFromTargetWithInlineOverride(t *testing.T) {
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not reach ts1")
	}))
	defer ts1.Close()
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts2.Close()

	cfg := config.DefaultConfig()
	cfg.MCP.Webhook.Targets = map[string]config.WebhookTarget{
		"my_bot": {URL: ts1.URL},
	}
	tool := NewWebhookTool(cfg)
	// Inline URL overrides target URL
	input, _ := json.Marshal(map[string]any{
		"target": "my_bot",
		"url":    ts2.URL,
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestWebhook_ErrorMissingURL(t *testing.T) {
	tool := NewWebhookTool(config.DefaultConfig())
	input, _ := json.Marshal(map[string]any{
		"body": "test",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing URL")
	}
}

func TestWebhook_ErrorUnknownTarget(t *testing.T) {
	tool := NewWebhookTool(config.DefaultConfig())
	input, _ := json.Marshal(map[string]any{
		"target": "nonexistent",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error for unknown target")
	}
}

func TestWebhook_ErrorNetworkFailure(t *testing.T) {
	tool := NewWebhookTool(config.DefaultConfig())
	input, _ := json.Marshal(map[string]any{
		"url": "http://127.0.0.1:1",
	})
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestWebhook_ResponseContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": "msg_123"}`))
	}))
	defer ts.Close()

	tool := NewWebhookTool(config.DefaultConfig())
	input, _ := json.Marshal(map[string]any{
		"url":  ts.URL,
		"body": "test",
	})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	expected := fmt.Sprintf("HTTP %d\n\n%s", http.StatusCreated, `{"id": "msg_123"}`)
	if result.Content != expected {
		t.Errorf("expected %q, got %q", expected, result.Content)
	}
}

func TestWebhook_ContentTypeDefault(t *testing.T) {
	var gotCT string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tool := NewWebhookTool(config.DefaultConfig())
	input, _ := json.Marshal(map[string]any{
		"url":  ts.URL,
		"body": `{"key":"value"}`,
	})
	_, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if gotCT != "application/json" {
		t.Errorf("expected Content-Type: application/json, got %q", gotCT)
	}
}

func TestWebhook_NoContentTypeWithoutBody(t *testing.T) {
	var gotCT string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tool := NewWebhookTool(config.DefaultConfig())
	input, _ := json.Marshal(map[string]any{
		"url":    ts.URL,
		"method": "GET",
	})
	_, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if gotCT != "" {
		t.Errorf("expected no Content-Type for GET without body, got %q", gotCT)
	}
}


func TestBlockPrivateTarget(t *testing.T) {
	private := []string{
		"http://127.0.0.1:8080/test",
		"http://[::1]:8080/test",
		"http://10.0.0.1/api",
		"http://172.16.0.1/api",
		"http://192.168.1.1/api",
		"http://169.254.169.254/latest/meta-data/",
	}
	public := []string{
		"http://93.184.216.34/test",
		"http://8.8.8.8/",
		"http://example.com/",
	}
	for _, u := range private {
		if err := blockPrivateTarget(u); err == nil {
			t.Errorf("expected block for private URL: %s", u)
		}
	}
	for _, u := range public {
		if err := blockPrivateTarget(u); err != nil {
			t.Errorf("unexpected block for public URL %s: %v", u, err)
		}
	}
}
