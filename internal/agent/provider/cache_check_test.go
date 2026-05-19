package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"

	"gopkg.in/yaml.v3"
)

func findConfigPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, ".dolphin", "config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			data, err := os.ReadFile(candidate)
			if err == nil && bytes.Contains(data, []byte("api_key:")) {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func testDeepSeekProvider(t *testing.T) (*OpenAIProvider, string) {
	t.Helper()
	cfgPath := findConfigPath(t)
	if cfgPath == "" {
		t.Skip(".dolphin/config.yaml not found (walked up from CWD)")
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Skipf("read config: %v", err)
	}

	var parsed struct {
		LLM struct {
			APIKey    string `yaml:"api_key"`
			BaseURL   string `yaml:"base_url"`
			Providers []struct {
				APIKey  string `yaml:"api_key"`
				BaseURL string `yaml:"base_url"`
			} `yaml:"providers"`
		} `yaml:"llm"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Skipf("parse config: %v", err)
	}

	apiKey := parsed.LLM.APIKey
	baseURL := parsed.LLM.BaseURL
	if apiKey == "" && len(parsed.LLM.Providers) > 0 && parsed.LLM.Providers[0].APIKey != "" {
		apiKey = parsed.LLM.Providers[0].APIKey
		baseURL = parsed.LLM.Providers[0].BaseURL
	}
	if apiKey == "" {
		apiKey = os.Getenv("DZ_LLM_API_KEY")
	}
	if apiKey == "" {
		t.Skip("no API key available")
	}

	prov := NewOpenAIProvider(&config.ProviderConfig{
		Name:      "cache-test",
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Model:     "deepseek-v4-pro",
		MaxTokens: 1024,
	})
	return prov, baseURL
}

func TestCacheTokensWithDeepSeekPro(t *testing.T) {
	prov, baseURL := testDeepSeekProvider(t)
	t.Logf("Testing model=deepseek-v4-pro baseURL=%s", baseURL)

	ctx := context.Background()
	prompt := "What is the capital of France? Answer in one word."

	for i := 0; i < 3; i++ {
		t.Logf("=== Request %d (non-streaming) ===", i+1)
		resp, err := prov.Complete(ctx, ProviderRequest{
			System: "You are a helpful assistant.",
			Messages: []Message{
				{Role: "user", Content: json.RawMessage(`"` + prompt + `"`)},
			},
		})
		if err != nil {
			if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
				t.Skipf("auth error: %v", err)
			}
			if strings.Contains(err.Error(), "404") {
				t.Skipf("model not found: %v", err)
			}
			t.Fatalf("Request %d error: %v", i+1, err)
		}

		t.Logf("Content: %s", string(resp.Content))
		if resp.Usage != nil {
			t.Logf("  Input=%d Cache=%d Miss=%d (cache+miss=%d) Output=%d",
				resp.Usage.InputTokens, resp.Usage.CachedInputTokens, resp.Usage.MissedInputTokens,
				resp.Usage.CachedInputTokens+resp.Usage.MissedInputTokens,
				resp.Usage.OutputTokens)
			if resp.Usage.CachedInputTokens+resp.Usage.MissedInputTokens != resp.Usage.InputTokens {
				t.Errorf("cache+miss (%d) != input (%d)", resp.Usage.CachedInputTokens+resp.Usage.MissedInputTokens, resp.Usage.InputTokens)
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func TestCacheGrowsWithConversation(t *testing.T) {
	prov, _ := testDeepSeekProvider(t)
	t.Logf("Testing cache growth with multi-turn conversation")

	// Build a long system prompt (~2000 tokens to trigger DeepSeek caching)
	var sb strings.Builder
	sb.WriteString("You are a helpful assistant. ")
	for i := 0; i < 200; i++ {
		sb.WriteString(fmt.Sprintf("Rule %d: Always be helpful. ", i))
	}
	system := sb.String()

	ctx := context.Background()

	// Turn 1: first request
	t.Log("=== Turn 1 ===")
	resp1, err := prov.Complete(ctx, ProviderRequest{
		System: system,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What is the capital of France? Answer in one word."`)},
		},
	})
	if err != nil {
		t.Fatalf("Turn 1 error: %v", err)
	}
	t.Logf("in=%d cache=%d miss=%d out=%d",
		resp1.Usage.InputTokens, resp1.Usage.CachedInputTokens, resp1.Usage.MissedInputTokens, resp1.Usage.OutputTokens)

	time.Sleep(1 * time.Second)

	// Turn 2: previous turn + new question
	t.Log("=== Turn 2 ===")
	resp2, err := prov.Complete(ctx, ProviderRequest{
		System: system,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What is the capital of France? Answer in one word."`)},
			{Role: "assistant", Content: resp1.Content},
			{Role: "user", Content: json.RawMessage(`"What is the capital of Germany? Answer in one word."`)},
		},
	})
	if err != nil {
		t.Fatalf("Turn 2 error: %v", err)
	}
	t.Logf("in=%d cache=%d miss=%d out=%d",
		resp2.Usage.InputTokens, resp2.Usage.CachedInputTokens, resp2.Usage.MissedInputTokens, resp2.Usage.OutputTokens)

	// Turn 3: more conversation
	t.Log("=== Turn 3 ===")
	resp3, err := prov.Complete(ctx, ProviderRequest{
		System: system,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What is the capital of France? Answer in one word."`)},
			{Role: "assistant", Content: resp1.Content},
			{Role: "user", Content: json.RawMessage(`"What is the capital of Germany? Answer in one word."`)},
			{Role: "assistant", Content: resp2.Content},
			{Role: "user", Content: json.RawMessage(`"What is the capital of Italy? Answer in one word."`)},
		},
	})
	if err != nil {
		t.Fatalf("Turn 3 error: %v", err)
	}
	t.Logf("in=%d cache=%d miss=%d out=%d",
		resp3.Usage.InputTokens, resp3.Usage.CachedInputTokens, resp3.Usage.MissedInputTokens, resp3.Usage.OutputTokens)

	// Verify: Turn 2 cache should cover Turn 1's system + user prompt
	// (cache may be 0 for turn 1 as it's the first request)
	if resp1.Usage.CachedInputTokens+resp1.Usage.MissedInputTokens != resp1.Usage.InputTokens {
		t.Errorf("Turn 1: cache+miss (%d) != input (%d)",
			resp1.Usage.CachedInputTokens+resp1.Usage.MissedInputTokens, resp1.Usage.InputTokens)
	}
	if resp2.Usage.CachedInputTokens+resp2.Usage.MissedInputTokens != resp2.Usage.InputTokens {
		t.Errorf("Turn 2: cache+miss (%d) != input (%d)",
			resp2.Usage.CachedInputTokens+resp2.Usage.MissedInputTokens, resp2.Usage.InputTokens)
	}
	if resp3.Usage.CachedInputTokens+resp3.Usage.MissedInputTokens != resp3.Usage.InputTokens {
		t.Errorf("Turn 3: cache+miss (%d) != input (%d)",
			resp3.Usage.CachedInputTokens+resp3.Usage.MissedInputTokens, resp3.Usage.InputTokens)
	}

	t.Log("=== Cache growth analysis ===")
	if resp2.Usage.CachedInputTokens > resp1.Usage.CachedInputTokens {
		t.Logf("Cache GREW from turn 1 to turn 2: %d → %d ✓",
			resp1.Usage.CachedInputTokens, resp2.Usage.CachedInputTokens)
	} else {
		t.Logf("Cache stayed same from turn 1 to turn 2: %d → %d",
			resp1.Usage.CachedInputTokens, resp2.Usage.CachedInputTokens)
	}
	if resp3.Usage.CachedInputTokens > resp2.Usage.CachedInputTokens {
		t.Logf("Cache GREW from turn 2 to turn 3: %d → %d ✓",
			resp2.Usage.CachedInputTokens, resp3.Usage.CachedInputTokens)
	} else {
		t.Logf("Cache stayed same from turn 2 to turn 3: %d → %d",
			resp2.Usage.CachedInputTokens, resp3.Usage.CachedInputTokens)
	}
}

func TestCacheStreamWithDeepSeekPro(t *testing.T) {
	prov, baseURL := testDeepSeekProvider(t)
	t.Logf("Testing streaming model=deepseek-v4-pro baseURL=%s", baseURL)

	ctx := context.Background()
	prompt := "What is the capital of France? Answer in one word."

	for i := 0; i < 3; i++ {
		t.Logf("=== Request %d (streaming) ===", i+1)
		ch, err := prov.CompleteStream(ctx, ProviderRequest{
			System: "You are a helpful assistant.",
			Messages: []Message{
				{Role: "user", Content: json.RawMessage(`"` + prompt + `"`)},
			},
		})
		if err != nil {
			if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
				t.Skipf("auth error: %v", err)
			}
			if strings.Contains(err.Error(), "404") {
				t.Skipf("model not found: %v", err)
			}
			t.Fatalf("Request %d error: %v", i+1, err)
		}

		var lastUsage *Usage
		var content strings.Builder
		for c := range ch {
			if c.Done {
				break
			}
			if c.Usage != nil {
				lastUsage = c.Usage
			}
			if txt := ExtractText(c.Content); txt != "" {
				content.WriteString(txt)
			}
		}

		t.Logf("Content: %s", content.String())
		if lastUsage != nil {
			t.Logf("  Input=%d Cache=%d Miss=%d (cache+miss=%d) Output=%d",
				lastUsage.InputTokens, lastUsage.CachedInputTokens, lastUsage.MissedInputTokens,
				lastUsage.CachedInputTokens+lastUsage.MissedInputTokens,
				lastUsage.OutputTokens)
			if lastUsage.CachedInputTokens+lastUsage.MissedInputTokens != lastUsage.InputTokens {
				t.Errorf("cache+miss (%d) != input (%d)", lastUsage.CachedInputTokens+lastUsage.MissedInputTokens, lastUsage.InputTokens)
			}
		}
		time.Sleep(1 * time.Second)
	}
}

// ---- Anthropic (DeepSeek via Anthropic-compatible endpoint) integration tests ----

type anthroProviderConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
	Type    string `yaml:"type"`
}

func testAnthropicDeepSeekProvider(t *testing.T) *AnthropicProvider {
	t.Helper()
	cfgPath := findConfigPath(t)
	if cfgPath == "" {
		t.Skip(".dolphin/config.yaml not found (walked up from CWD)")
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Skipf("read config: %v", err)
	}

	var parsed struct {
		LLM struct {
			APIKey    string                `yaml:"api_key"`
			BaseURL   string                `yaml:"base_url"`
			Type      string                `yaml:"type"`
			Providers []anthroProviderConfig `yaml:"providers"`
		} `yaml:"llm"`
	}
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Skipf("parse config: %v", err)
	}

	// Try to find an anthropic-type provider first
	var apiKey, baseURL string
	for _, p := range parsed.LLM.Providers {
		if p.Type == "anthropic" {
			apiKey = p.APIKey
			baseURL = p.BaseURL
			break
		}
	}
	if apiKey == "" && parsed.LLM.Type == "anthropic" {
		apiKey = parsed.LLM.APIKey
		baseURL = parsed.LLM.BaseURL
	}
	if apiKey == "" {
		apiKey = os.Getenv("DZ_LLM_API_KEY")
		baseURL = os.Getenv("DZ_LLM_BASE_URL")
	}
	if apiKey == "" {
		t.Skip("no API key available for anthropic provider")
	}
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/anthropic"
	}

	return NewAnthropicProvider(&config.ProviderConfig{
		Type:      "anthropic",
		Name:      "cache-test-anthropic",
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Model:     "deepseek-v4-pro",
		MaxTokens: 1024,
	})
}

func TestAnthropicCacheTokensWithDeepSeekPro(t *testing.T) {
	prov := testAnthropicDeepSeekProvider(t)
	t.Logf("Testing anthropic model=deepseek-v4-pro baseURL=%s", prov.baseURL)

	ctx := context.Background()
	prompt := "What is the capital of France? Answer in one word."

	for i := 0; i < 3; i++ {
		t.Logf("=== Request %d (non-streaming) ===", i+1)
		resp, err := prov.Complete(ctx, ProviderRequest{
			System: "You are a helpful assistant.",
			Messages: []Message{
				{Role: "user", Content: json.RawMessage(`"` + prompt + `"`)},
			},
		})
		if err != nil {
			if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
				t.Skipf("auth error: %v", err)
			}
			if strings.Contains(err.Error(), "404") {
				t.Skipf("model not found: %v", err)
			}
			t.Fatalf("Request %d error: %v", i+1, err)
		}

		t.Logf("Content: %s", string(resp.Content))
		if resp.Usage != nil {
			t.Logf("  Input=%d Cache=%d Miss=%d (cache+miss=%d) Output=%d",
				resp.Usage.InputTokens, resp.Usage.CachedInputTokens, resp.Usage.MissedInputTokens,
				resp.Usage.CachedInputTokens+resp.Usage.MissedInputTokens,
				resp.Usage.OutputTokens)
			if resp.Usage.CachedInputTokens+resp.Usage.MissedInputTokens != resp.Usage.InputTokens {
				t.Errorf("cache+miss (%d) != input (%d)", resp.Usage.CachedInputTokens+resp.Usage.MissedInputTokens, resp.Usage.InputTokens)
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func TestAnthropicCacheGrowsWithConversation(t *testing.T) {
	prov := testAnthropicDeepSeekProvider(t)
	t.Logf("Testing anthropic cache growth with multi-turn conversation")

	// Build a long system prompt (~2000 tokens to trigger DeepSeek caching)
	var sb strings.Builder
	sb.WriteString("You are a helpful assistant. ")
	for i := 0; i < 200; i++ {
		sb.WriteString(fmt.Sprintf("Rule %d: Always be helpful. ", i))
	}
	system := sb.String()

	ctx := context.Background()

	// Turn 1: first request
	t.Log("=== Turn 1 ===")
	resp1, err := prov.Complete(ctx, ProviderRequest{
		System: system,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What is the capital of France? Answer in one word."`)},
		},
	})
	if err != nil {
		t.Fatalf("Turn 1 error: %v", err)
	}
	t.Logf("in=%d cache=%d miss=%d out=%d",
		resp1.Usage.InputTokens, resp1.Usage.CachedInputTokens, resp1.Usage.MissedInputTokens, resp1.Usage.OutputTokens)

	time.Sleep(1 * time.Second)

	// Turn 2: previous turn + new question
	t.Log("=== Turn 2 ===")
	resp2, err := prov.Complete(ctx, ProviderRequest{
		System: system,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What is the capital of France? Answer in one word."`)},
			{Role: "assistant", Content: resp1.Content},
			{Role: "user", Content: json.RawMessage(`"What is the capital of Germany? Answer in one word."`)},
		},
	})
	if err != nil {
		t.Fatalf("Turn 2 error: %v", err)
	}
	t.Logf("in=%d cache=%d miss=%d out=%d",
		resp2.Usage.InputTokens, resp2.Usage.CachedInputTokens, resp2.Usage.MissedInputTokens, resp2.Usage.OutputTokens)

	// Turn 3: more conversation
	t.Log("=== Turn 3 ===")
	resp3, err := prov.Complete(ctx, ProviderRequest{
		System: system,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What is the capital of France? Answer in one word."`)},
			{Role: "assistant", Content: resp1.Content},
			{Role: "user", Content: json.RawMessage(`"What is the capital of Germany? Answer in one word."`)},
			{Role: "assistant", Content: resp2.Content},
			{Role: "user", Content: json.RawMessage(`"What is the capital of Italy? Answer in one word."`)},
		},
	})
	if err != nil {
		t.Fatalf("Turn 3 error: %v", err)
	}
	t.Logf("in=%d cache=%d miss=%d out=%d",
		resp3.Usage.InputTokens, resp3.Usage.CachedInputTokens, resp3.Usage.MissedInputTokens, resp3.Usage.OutputTokens)

	// Verify: cache+miss == input invariant
	if resp1.Usage.CachedInputTokens+resp1.Usage.MissedInputTokens != resp1.Usage.InputTokens {
		t.Errorf("Turn 1: cache+miss (%d) != input (%d)",
			resp1.Usage.CachedInputTokens+resp1.Usage.MissedInputTokens, resp1.Usage.InputTokens)
	}
	if resp2.Usage.CachedInputTokens+resp2.Usage.MissedInputTokens != resp2.Usage.InputTokens {
		t.Errorf("Turn 2: cache+miss (%d) != input (%d)",
			resp2.Usage.CachedInputTokens+resp2.Usage.MissedInputTokens, resp2.Usage.InputTokens)
	}
	if resp3.Usage.CachedInputTokens+resp3.Usage.MissedInputTokens != resp3.Usage.InputTokens {
		t.Errorf("Turn 3: cache+miss (%d) != input (%d)",
			resp3.Usage.CachedInputTokens+resp3.Usage.MissedInputTokens, resp3.Usage.InputTokens)
	}

	t.Log("=== Cache growth analysis ===")
	if resp2.Usage.CachedInputTokens > resp1.Usage.CachedInputTokens {
		t.Logf("Cache GREW from turn 1 to turn 2: %d → %d ✓",
			resp1.Usage.CachedInputTokens, resp2.Usage.CachedInputTokens)
	} else {
		t.Logf("Cache stayed same from turn 1 to turn 2: %d → %d",
			resp1.Usage.CachedInputTokens, resp2.Usage.CachedInputTokens)
	}
	if resp3.Usage.CachedInputTokens > resp2.Usage.CachedInputTokens {
		t.Logf("Cache GREW from turn 2 to turn 3: %d → %d ✓",
			resp2.Usage.CachedInputTokens, resp3.Usage.CachedInputTokens)
	} else {
		t.Logf("Cache stayed same from turn 2 to turn 3: %d → %d",
			resp2.Usage.CachedInputTokens, resp3.Usage.CachedInputTokens)
	}
}

func TestAnthropicCacheStreamWithDeepSeekPro(t *testing.T) {
	prov := testAnthropicDeepSeekProvider(t)
	t.Logf("Testing anthropic streaming model=deepseek-v4-pro baseURL=%s", prov.baseURL)

	ctx := context.Background()
	prompt := "What is the capital of France? Answer in one word."

	for i := 0; i < 3; i++ {
		t.Logf("=== Request %d (streaming) ===", i+1)
		ch, err := prov.CompleteStream(ctx, ProviderRequest{
			System: "You are a helpful assistant.",
			Messages: []Message{
				{Role: "user", Content: json.RawMessage(`"` + prompt + `"`)},
			},
		})
		if err != nil {
			if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
				t.Skipf("auth error: %v", err)
			}
			if strings.Contains(err.Error(), "404") {
				t.Skipf("model not found: %v", err)
			}
			t.Fatalf("Request %d error: %v", i+1, err)
		}

		var lastUsage *Usage
		var content strings.Builder
		for c := range ch {
			if c.Done {
				break
			}
			if c.Usage != nil {
				lastUsage = c.Usage
			}
			if txt := ExtractText(c.Content); txt != "" {
				content.WriteString(txt)
			}
		}

		t.Logf("Content: %s", content.String())
		if lastUsage != nil {
			t.Logf("  Input=%d Cache=%d Miss=%d (cache+miss=%d) Output=%d",
				lastUsage.InputTokens, lastUsage.CachedInputTokens, lastUsage.MissedInputTokens,
				lastUsage.CachedInputTokens+lastUsage.MissedInputTokens,
				lastUsage.OutputTokens)
			if lastUsage.CachedInputTokens+lastUsage.MissedInputTokens != lastUsage.InputTokens {
				t.Errorf("cache+miss (%d) != input (%d)", lastUsage.CachedInputTokens+lastUsage.MissedInputTokens, lastUsage.InputTokens)
			}
		}
		time.Sleep(1 * time.Second)
	}
}
