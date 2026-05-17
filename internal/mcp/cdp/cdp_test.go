package cdp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
)

func TestCDPShutdownUninitialized(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	tool := New(cfg)
	tool.Shutdown()
}

func TestCDPTruncateShort(t *testing.T) {
	if s := truncate("hello", 10); s != "hello" {
		t.Errorf("got %q", s)
	}
}

func TestCDPTruncateLong(t *testing.T) {
	if s := truncate("hello world", 5); s != "hello..." {
		t.Errorf("got %q", s)
	}
}

func TestCDPTruncateEmpty(t *testing.T) {
	if s := truncate("", 5); s != "" {
		t.Errorf("got %q", s)
	}
}

func TestCDPNavigateNoURL(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	cfg.MCP.CDP.Enabled = false
	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"navigate"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for navigate without url")
	}
}

func TestCDPClickNoSelector(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"click"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for click without selector")
	}
}

func TestCDPUnknownAction(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"invalid"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestCDPInvalidInput(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MCP.CDP.IdleTimeout = 0
	tool := New(cfg)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{invalid json}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
}

func TestCDPIntegrationNavigateAndScreenshot(t *testing.T) {
	if os.Getenv("SKIP_CDP_INTEGRATION") != "" {
		t.Skip("skipping CDP integration test (SKIP_CDP_INTEGRATION set)")
	}

	cfg := config.DefaultConfig()
	cfg.MCP.CDP.Enabled = true
	cfg.MCP.CDP.Headless = true
	cfg.MCP.CDP.IdleTimeout = 0 // disable idle timeout for test
	cfg.MCP.CDP.StartupTimeout = 30
	cfg.MCP.CDP.Priority = 1000

	tool := New(cfg)
	defer tool.Shutdown()

	t.Run("navigate", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{
			"action": "navigate",
			"url":    "https://www.baidu.com",
		})
		start := time.Now()
		result, err := tool.Execute(context.Background(), input)
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			t.Fatalf("Execute error after %s: %v", elapsed, err)
		}
		if result.IsError {
			t.Fatalf("navigate failed after %s: %s", elapsed, result.Content)
		}
		if !strings.Contains(result.Content, "www.baidu.com") {
			t.Errorf("expected baidu.com in result, got: %s", result.Content)
		}
		t.Logf("navigate OK [%s]: %s", elapsed, result.Content)
	})

	t.Run("screenshot", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{
			"action": "screenshot",
		})
		start := time.Now()
		result, err := tool.Execute(context.Background(), input)
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			t.Fatalf("Execute error after %s: %v", elapsed, err)
		}
		if result.IsError {
			t.Fatalf("screenshot failed after %s: %s", elapsed, result.Content)
		}
		if !strings.Contains(result.Content, "Screenshot saved:") {
			t.Fatalf("expected 'Screenshot saved:' in result, got: %s", result.Content)
		}

		fields := strings.Fields(result.Content)
		if len(fields) >= 3 {
			filePath := fields[2]
			if data, err := os.ReadFile(filePath); err == nil {
				if len(data) < 100 {
					t.Fatalf("screenshot too small: %d bytes", len(data))
				}
				t.Logf("screenshot OK [%s]: %s (%d bytes)", elapsed, filePath, len(data))
			} else {
				t.Logf("screenshot file not readable (test env): %v", err)
			}
		}
	})

	// Element screenshot
	t.Run("element_screenshot", func(t *testing.T) {
		input, _ := json.Marshal(map[string]string{
			"action":   "screenshot",
			"selector": "body",
		})
		start := time.Now()
		result, err := tool.Execute(context.Background(), input)
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			t.Fatalf("Execute error after %s: %v", elapsed, err)
		}
		if result.IsError {
			t.Fatalf("element screenshot failed after %s: %s", elapsed, result.Content)
		}
		if !strings.Contains(result.Content, "Screenshot saved:") {
			t.Fatalf("expected 'Screenshot saved:' in result, got: %s", result.Content)
		}
		t.Logf("element screenshot OK [%s]: %s", elapsed, result.Content)
	})
}
