package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// findConfigPath looks for .dolphin/config.yaml by walking up from CWD.
// It checks the CWD first, then walks up to parent directories.
func findConfigPath() string {
	wd, _ := os.Getwd()
	dir := wd
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, ".dolphin", "config.yaml")
		// Skip if found in CWD's own .dolphin (could be a test artifact).
		// Only accept it if the file has real provider configs.
		if _, err := os.Stat(candidate); err == nil {
			data, err := os.ReadFile(candidate)
			if err == nil && (bytes.Contains(data, []byte("api_key:")) || bytes.Contains(data, []byte("api_key: "))) {
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

func readDeepSeekConfig(t *testing.T) (apiKey, baseURL, model string) {
	t.Helper()
	cfgPath := findConfigPath()
	if cfgPath == "" {
		t.Skip("config.yaml not found (walked up from CWD)")
	}
	if v := os.Getenv("DOLPHIN_CONFIG"); v != "" {
		cfgPath = v
	}
	t.Logf("config: %s", cfgPath)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Skipf("read config: %v", err)
	}

	var raw struct {
		LLM struct {
			Type    string `yaml:"type"`
			BaseURL string `yaml:"base_url"`
			Model   string `yaml:"model"`
			APIKey  string `yaml:"api_key"`
		} `yaml:"llm"`
		Providers []struct {
			Type   string `yaml:"type"`
			APIKey string `yaml:"api_key"`
			BaseURL string `yaml:"base_url"`
			Model  string `yaml:"model"`
		} `yaml:"providers"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Skipf("parse config: %v", err)
	}

	for _, p := range raw.Providers {
		if p.Type == "openai" && p.APIKey != "" {
			return p.APIKey, p.BaseURL, p.Model
		}
	}
	if raw.LLM.Type == "openai" && raw.LLM.APIKey != "" {
		return raw.LLM.APIKey, raw.LLM.BaseURL, raw.LLM.Model
	}
	t.Logf("found LLM config: type=%q model=%q", raw.LLM.Type, raw.LLM.Model)
	return "", "", ""
}

func testProvider(t *testing.T) (*OpenAIProvider, string) {
	t.Helper()
	apiKey, baseURL, model := readDeepSeekConfig(t)
	if apiKey == "" {
		t.Skip("no openai-type provider with API key configured")
	}
	return &OpenAIProvider{
		model:    model,
		maxTok:   2048,
		name:     "integration-test",
		temp:     0.7,
		baseURL:  baseURL,
		apiKey:   apiKey,
		httpDoer: http.DefaultClient,
	}, model
}

func TestDeepSeekIntegration(t *testing.T) {
	provider, model := testProvider(t)
	t.Logf("model: %s", model)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := provider.CompleteStream(ctx, ProviderRequest{
		System: "You are a helpful assistant. Keep responses very brief.",
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"say hello in one word"`)},
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			t.Skipf("auth error: %v", err)
		}
		t.Fatalf("CompleteStream failed: %v", err)
	}

	var content strings.Builder
	for c := range ch {
		if c.Done {
			break
		}
		if txt := extractText(c.Content); txt != "" {
			content.WriteString(txt)
		}
	}
	result := strings.TrimSpace(content.String())
	if result == "" {
		t.Fatal("no content received")
	}
	t.Logf("response: %s", result)
}

func TestDeepSeekIntegrationWithRoundTrip(t *testing.T) {
	provider, model := testProvider(t)
	t.Logf("model: %s", model)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Turn 1
	t.Log("--- turn 1 ---")
	ch1, err := provider.CompleteStream(ctx, ProviderRequest{
		System: "You are a helpful assistant. Keep responses very brief.",
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"what is 2+2?"`)},
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			t.Skipf("auth error: %v", err)
		}
		t.Fatalf("turn 1: %v", err)
	}

	var blocks []map[string]any
	var textBuf strings.Builder
	for c := range ch1 {
		if c.Done {
			break
		}
		if c.DeltaType == "thinking" {
			blocks = append(blocks, map[string]any{
				"type":     "thinking",
				"thinking": c.BlockDelta,
			})
		}
		if txt := extractText(c.Content); txt != "" {
			textBuf.WriteString(txt)
		}
	}
	if textBuf.Len() > 0 {
		blocks = append(blocks, map[string]any{
			"type": "text",
			"text": textBuf.String(),
		})
		t.Logf("turn 1: %s", strings.TrimSpace(textBuf.String()))
	}
	if len(blocks) == 0 {
		t.Fatal("no response from turn 1")
	}

	// Turn 2 — includes assistant message (with possible thinking) to test
	// reasoning_content round-trip.
	blocksJSON, _ := json.Marshal(blocks)
	t.Log("--- turn 2 (reasoning_content round-trip) ---")

	ch2, err := provider.CompleteStream(ctx, ProviderRequest{
		System: "You are a helpful assistant. Keep responses very brief.",
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"what is 2+2?"`)},
			{Role: "assistant", Content: json.RawMessage(blocksJSON)},
			{Role: "user", Content: json.RawMessage(`"and 3+3?"`)},
		},
	})
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}

	var result strings.Builder
	for c := range ch2 {
		if c.Done {
			break
		}
		if txt := extractText(c.Content); txt != "" {
			result.WriteString(txt)
		}
	}
	final := strings.TrimSpace(result.String())
	if final == "" {
		t.Error("no content from turn 2")
	} else {
		t.Logf("turn 2: %s", final)
	}
}

