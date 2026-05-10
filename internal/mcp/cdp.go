package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"dolphinzZ/internal/config"

	"github.com/chromedp/chromedp"
)

// CDPTool implements browser automation via Chrome DevTools Protocol.
// The browser context is created once and reused across tool calls.
type CDPTool struct {
	cfg    *config.CDPConfig
	schema json.RawMessage

	mu            sync.Mutex
	allocCtx      context.Context
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	initialized   bool
	lastUsedAt    time.Time
}

func NewCDPTool(cfg *config.Config) *CDPTool {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"navigate", "click", "screenshot", "evaluate", "get_text"},
			},
			"url": map[string]any{
				"type":        "string",
				"description": "URL for navigate action",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector for click/get_text/screenshot actions",
			},
			"script": map[string]any{
				"type":        "string",
				"description": "JavaScript for evaluate action. Supports async/await.",
			},
		},
		"required": []string{"action"},
	})

	t := &CDPTool{
		cfg:    &cfg.MCP.CDP,
		schema: schema,
	}
	if t.cfg.IdleTimeout > 0 {
		t.startIdleWatcher(time.Duration(t.cfg.IdleTimeout) * time.Second)
	}
	return t
}

func (c *CDPTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "cdp",
		Description: "Control a browser using Chrome DevTools Protocol. Actions: navigate (goto a URL and wait for page load), click (click an element by CSS selector), screenshot (capture page or element as base64 PNG), evaluate (run JavaScript, supports async/await), get_text (extract visible text from element). Browser state persists across calls within the same session.",
		InputSchema: c.schema,
		Priority:    c.cfg.Priority,
	}
}

func (c *CDPTool) Execute(ctx context.Context, input json.RawMessage) (*ToolResult, error) {
	var params struct {
		Action   string `json:"action"`
		URL      string `json:"url,omitempty"`
		Selector string `json:"selector,omitempty"`
		Script   string `json:"script,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}

	// Get or create browser context (persistent across calls)
	browserCtx, err := c.getBrowser(ctx)
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("browser init failed: %v", err), IsError: true}, nil
	}

	c.mu.Lock()
	c.lastUsedAt = time.Now()
	c.mu.Unlock()

	slog.Debug("cdp executing", "action", params.Action, "headless", c.cfg.Headless)

	switch params.Action {
	case "navigate":
		return c.navigate(browserCtx, params.URL)
	case "click":
		return c.click(browserCtx, params.Selector)
	case "screenshot":
		return c.screenshot(browserCtx, params.Selector)
	case "evaluate":
		return c.evaluate(browserCtx, params.Script)
	case "get_text":
		return c.getText(browserCtx, params.Selector)
	default:
		return &ToolResult{
			Content: fmt.Sprintf("unknown action: %s (supported: navigate, click, screenshot, evaluate, get_text)", params.Action),
			IsError: true,
		}, nil
	}
}

// getBrowser returns a persistent browser context, creating one if needed.
func (c *CDPTool) getBrowser(ctx context.Context) (context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		// Health check: try a lightweight operation to see if browser is still alive
		healthCtx, cancel := context.WithTimeout(c.browserCtx, 5*time.Second)
		defer cancel()
		err := chromedp.Run(healthCtx, chromedp.Navigate("about:blank"))
		if err == nil {
			return c.browserCtx, nil
		}
		slog.Warn("cdp browser appears dead, reinitializing", "error", err)
		c.shutdownBrowser()
	}

	if c.cfg.WsURL != "" {
		// Connect to remote browser — use background context so request timeouts don't kill it
		allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), c.cfg.WsURL)
		browserCtx, browserCancel := chromedp.NewContext(allocCtx)
		c.allocCtx = allocCtx
		c.allocCancel = allocCancel
		c.browserCtx = browserCtx
		c.browserCancel = browserCancel
	} else {
		// Start local browser — use background context so request timeouts don't kill it
		allocOpts := []chromedp.ExecAllocatorOption{
			chromedp.Flag("headless", c.cfg.Headless),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
		}
		allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
		browserCtx, browserCancel := chromedp.NewContext(allocCtx)
		c.allocCtx = allocCtx
		c.allocCancel = allocCancel
		c.browserCtx = browserCtx
		c.browserCancel = browserCancel
	}

	c.initialized = true
	slog.Debug("cdp browser initialized", "headless", c.cfg.Headless, "remote", c.cfg.WsURL != "")

	// Verify browser is working
	initCtx, cancel := context.WithTimeout(c.browserCtx, 10*time.Second)
	defer cancel()
	if err := chromedp.Run(initCtx, chromedp.Navigate("about:blank")); err != nil {
		c.shutdownBrowser()
		return nil, fmt.Errorf("browser init verify failed: %w", err)
	}

	return c.browserCtx, nil
}

// shutdownBrowser cleans up browser resources without holding the lock
// (caller must hold c.mu).
func (c *CDPTool) shutdownBrowser() {
	if c.browserCancel != nil {
		c.browserCancel()
		c.browserCancel = nil
	}
	if c.allocCancel != nil {
		c.allocCancel()
		c.allocCancel = nil
	}
	c.initialized = false
}

// Shutdown cleans up browser resources. Safe to call from multiple goroutines.
func (c *CDPTool) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdownBrowser()
}

// startIdleWatcher runs a background goroutine that shuts down the browser
// if no Execute call has been made within the given timeout.
func (c *CDPTool) startIdleWatcher(timeout time.Duration) {
	go func() {
		for {
			time.Sleep(timeout / 2)
			c.mu.Lock()
			if c.initialized && time.Since(c.lastUsedAt) > timeout {
				slog.Warn("cdp: idle timeout, shutting down browser", "idle", time.Since(c.lastUsedAt).Round(time.Second))
				c.shutdownBrowser()
			}
			c.mu.Unlock()
		}
	}()
}

func (c *CDPTool) navigate(ctx context.Context, url string) (*ToolResult, error) {
	if url == "" {
		return &ToolResult{Content: "url is required for navigate action", IsError: true}, nil
	}

	// Add https:// if no scheme
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	var title string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Title(&title),
	)
	if err != nil {
		return &ToolResult{Content: fmt.Sprintf("navigate to '%s' failed: %v", url, err), IsError: true}, nil
	}

	return &ToolResult{Content: fmt.Sprintf("Navigated to %s\nPage title: %s", url, title)}, nil
}

func (c *CDPTool) click(ctx context.Context, selector string) (*ToolResult, error) {
	if selector == "" {
		return &ToolResult{Content: "selector is required for click action", IsError: true}, nil
	}

	if err := chromedp.Run(ctx, chromedp.Click(selector, chromedp.NodeVisible)); err != nil {
		return &ToolResult{Content: fmt.Sprintf("click failed on '%s': %v", selector, err), IsError: true}, nil
	}

	return &ToolResult{Content: fmt.Sprintf("Clicked element: %s", selector)}, nil
}

func (c *CDPTool) screenshot(ctx context.Context, selector string) (*ToolResult, error) {
	var buf []byte

	if selector != "" {
		if err := chromedp.Run(ctx, chromedp.Screenshot(selector, &buf, chromedp.NodeVisible)); err != nil {
			return &ToolResult{Content: fmt.Sprintf("element screenshot failed for '%s': %v", selector, err), IsError: true}, nil
		}
	} else {
		// Full page screenshot
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 100)); err != nil {
			return &ToolResult{Content: fmt.Sprintf("full page screenshot failed: %v", err), IsError: true}, nil
		}
	}

	result := fmt.Sprintf("data:image/png;base64,%s", bytesToBase64(buf))
	return &ToolResult{Content: result}, nil
}

func (c *CDPTool) evaluate(ctx context.Context, script string) (*ToolResult, error) {
	if script == "" {
		return &ToolResult{Content: "script is required for evaluate action", IsError: true}, nil
	}

	// Auto-wrap in async IIFE if the script uses await
	wrappedScript := script
	if strings.Contains(script, "await ") {
		wrappedScript = "(async () => { " + script + " })()"
	}

	var result string
	if err := chromedp.Run(ctx, chromedp.Evaluate(wrappedScript, &result)); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("evaluate failed:\nscript: %s\nerror: %v", truncate(script, 200), err),
			IsError: true,
		}, nil
	}

	if len(result) > 10000 {
		result = result[:10000] + "... [truncated]"
	}
	return &ToolResult{Content: result}, nil
}

func (c *CDPTool) getText(ctx context.Context, selector string) (*ToolResult, error) {
	if selector == "" {
		return &ToolResult{Content: "selector is required for get_text action", IsError: true}, nil
	}

	var text string
	if err := chromedp.Run(ctx, chromedp.Text(selector, &text, chromedp.NodeVisible)); err != nil {
		return &ToolResult{Content: fmt.Sprintf("get_text failed for '%s': %v", selector, err), IsError: true}, nil
	}

	text = strings.TrimSpace(text)
	if len(text) > 10000 {
		text = text[:10000] + "... [truncated]"
	}

	return &ToolResult{Content: text}, nil
}

// bytesToBase64 encodes byte data to base64 string.
func bytesToBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
