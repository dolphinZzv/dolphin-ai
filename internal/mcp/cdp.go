package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"

	"github.com/chromedp/chromedp"
	"go.uber.org/zap"
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
	idleStop      chan struct{} // closed in Shutdown to stop the idle watcher
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
		cfg:      &cfg.MCP.CDP,
		schema:   schema,
		idleStop: make(chan struct{}),
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
		Source:      "built-in",
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
		return &ToolResult{Content: err.Error(), IsError: true}, nil
	}

	c.mu.Lock()
	c.lastUsedAt = time.Now()
	c.mu.Unlock()

	zap.S().Debugw("cdp executing", "action", params.Action, "headless", c.cfg.Headless)

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
		// Health check: try a lightweight operation to see if browser is still alive.
		// Discard cancel — calling it would trigger chromedp cleanup and kill the
		// browserCtx's WebSocket connection. The timeout expires harmlessly in ~10s.
		healthCtx, _ := context.WithTimeout(c.browserCtx, 10*time.Second)
		if err := chromedp.Run(healthCtx, chromedp.Navigate("about:blank")); err == nil {
			return c.browserCtx, nil
		} else {
			zap.S().Warnw("cdp browser appears dead, reinitializing", "error", err)
			c.shutdownBrowser()
		}
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
		// Pre-flight: check that a browser executable is available
		if _, err := findBrowser(); err != nil {
			return nil, err
		}

		// Start local browser — use background context so request timeouts don't kill it
		allocOpts := []chromedp.ExecAllocatorOption{
			chromedp.Flag("headless", c.cfg.Headless),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-dev-shm-usage", true),
			chromedp.Flag("disable-extensions", true),
			chromedp.Flag("disable-background-networking", true),
			chromedp.Flag("disable-sync", true),
			chromedp.Flag("disable-default-apps", true),
			chromedp.Flag("disable-translate", true),
			chromedp.Flag("no-first-run", true),
			chromedp.Flag("no-default-browser-check", true),
		}
		allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
		browserCtx, browserCancel := chromedp.NewContext(allocCtx)
		c.allocCtx = allocCtx
		c.allocCancel = allocCancel
		c.browserCtx = browserCtx
		c.browserCancel = browserCancel
	}

	c.initialized = true
	zap.S().Debugw("cdp browser initialized", "headless", c.cfg.Headless, "remote", c.cfg.WsURL != "")

	// Verify browser is working — cold start can be slow (esp. macOS).
	// Discard cancel to avoid triggering chromedp's context cleanup which
	// would kill the browserCtx's WebSocket connection.
	startupTimeout := time.Duration(c.cfg.StartupTimeout) * time.Second
	if startupTimeout <= 0 {
		startupTimeout = 30 * time.Second
	}
	initCtx, _ := context.WithTimeout(c.browserCtx, startupTimeout)
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

// Shutdown cleans up browser resources and stops the idle watcher.
// Safe to call from multiple goroutines.
func (c *CDPTool) Shutdown() {
	c.mu.Lock()
	if c.idleStop != nil {
		select {
		case <-c.idleStop:
			// already closed
		default:
			close(c.idleStop)
		}
	}
	c.shutdownBrowser()
	c.mu.Unlock()
}

// startIdleWatcher runs a background goroutine that shuts down the browser
// if no Execute call has been made within the given timeout.
// The goroutine exits when c.idleStop is closed (via Shutdown).
func (c *CDPTool) startIdleWatcher(timeout time.Duration) {
	go func() {
		ticker := time.NewTicker(timeout / 2)
		defer ticker.Stop()
		for {
			select {
			case <-c.idleStop:
				return
			case <-ticker.C:
				c.mu.Lock()
				if c.initialized && time.Since(c.lastUsedAt) > timeout {
					zap.S().Warnw("cdp: idle timeout, shutting down browser", "idle", time.Since(c.lastUsedAt).Round(time.Second))
					c.shutdownBrowser()
				}
				c.mu.Unlock()
			}
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
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 100)); err != nil {
			return &ToolResult{Content: fmt.Sprintf("full page screenshot failed: %v", err), IsError: true}, nil
		}
	}

	// Save to file — don't send base64 to the LLM (wastes tokens, gets truncated)
	screenshotDir := filepath.Join(config.ProjectConfigDir, "screenshots")
	if err := os.MkdirAll(screenshotDir, 0700); err != nil {
		return &ToolResult{Content: fmt.Sprintf("create screenshot dir: %v", err), IsError: true}, nil
	}
	filename := fmt.Sprintf("screenshot_%s.png", time.Now().Format("20060102_150405"))
	filePath := filepath.Join(screenshotDir, filename)
	if err := os.WriteFile(filePath, buf, 0600); err != nil {
		return &ToolResult{Content: fmt.Sprintf("write screenshot: %v", err), IsError: true}, nil
	}

	return &ToolResult{
		Content: fmt.Sprintf("Screenshot saved: %s (%d bytes)", filePath, len(buf)),
	}, nil
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// findBrowser locates a Chrome/Chromium executable on the system.
// Returns the path and a user-friendly error if none is found.
func findBrowser() (string, error) {
	candidates := []string{
		"google-chrome-stable",
		"google-chrome",
		"chromium-browser",
		"chromium",
		"chrome",
		"microsoft-edge-stable",
		"microsoft-edge",
		"msedge",
	}

	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		)
	}

	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		)
	}

	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			zap.S().Debugw("cdp browser found", "path", path)
			return path, nil
		}
	}

	return "", fmt.Errorf("no Chrome/Chromium browser found — install Chrome or set mcp.cdp.ws_url to a remote browser")
}
