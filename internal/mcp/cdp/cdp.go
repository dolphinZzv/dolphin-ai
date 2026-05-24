// Package cdp provides Chrome DevTools Protocol browser automation tools.
package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/mcp"

	"github.com/chromedp/chromedp"
	"go.uber.org/zap"
)

// Tool implements browser automation via Chrome DevTools Protocol.
type Tool struct {
	cfg    *config.CDPConfig
	schema json.RawMessage

	mu            sync.Mutex
	allocCtx      context.Context
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	initialized   bool
	lastUsedAt    time.Time
	idleStop      chan struct{}
}

func New(cfg *config.Config) *Tool {
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

	t := &Tool{
		cfg:      &cfg.MCP.CDP,
		schema:   schema,
		idleStop: make(chan struct{}),
	}
	if t.cfg.IdleTimeout > 0 {
		t.startIdleWatcher(time.Duration(t.cfg.IdleTimeout) * time.Second)
	}
	return t
}

func (c *Tool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "cdp",
		Description: "Control a browser using Chrome DevTools Protocol. Actions: navigate (goto a URL and wait for page load), click (click an element by CSS selector), screenshot (capture page or element as base64 PNG), evaluate (run JavaScript, supports async/await), get_text (extract visible text from element). Browser state persists across calls within the same session.",
		InputSchema: c.schema,
		Priority:    c.cfg.Priority,
		Source:      "built-in",
	}
}

func (c *Tool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Action   string `json:"action"`
		URL      string `json:"url,omitempty"`
		Selector string `json:"selector,omitempty"`
		Script   string `json:"script,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "invalid input: " + err.Error(), IsError: true}, nil //nolint:nilerr
	}

	browserCtx, err := c.getBrowser(ctx)
	if err != nil {
		return &mcp.ToolResult{Content: err.Error(), IsError: true}, nil //nolint:nilerr
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
		return &mcp.ToolResult{
			Content: fmt.Sprintf("unknown action: %s (supported: navigate, click, screenshot, evaluate, get_text)", params.Action),
			IsError: true,
		}, nil
	}
}

func (c *Tool) getBrowser(ctx context.Context) (context.Context, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		healthTimeout := c.cfg.HealthCheckTimeout
		if healthTimeout <= 0 {
			healthTimeout = 10
		}
		healthCtx, healthCancel := context.WithTimeout(c.browserCtx, time.Duration(healthTimeout)*time.Second)
		defer func() { healthCancel() }()
		var ok bool
		if err := chromedp.Run(healthCtx, chromedp.Evaluate("!!window.chrome", &ok)); err == nil && ok {
			healthCancel = func() {}
			return c.browserCtx, nil
		} else {
			zap.S().Warnw("cdp browser appears dead, reinitializing", "error", err)
			c.shutdownBrowser()
		}
	}

	if c.cfg.WsURL != "" {
		allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), c.cfg.WsURL)
		browserCtx, browserCancel := chromedp.NewContext(allocCtx)
		c.allocCtx = allocCtx
		c.allocCancel = allocCancel
		c.browserCtx = browserCtx
		c.browserCancel = browserCancel
	} else {
		if _, err := findBrowser(); err != nil {
			return nil, err
		}

		allocOpts := []chromedp.ExecAllocatorOption{
			chromedp.Flag("headless", c.cfg.Headless),
		}
		// Apply chrome flags from config (sorted for deterministic order).
		if len(c.cfg.ChromeFlags) > 0 {
			keys := make([]string, 0, len(c.cfg.ChromeFlags))
			for k := range c.cfg.ChromeFlags {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				allocOpts = append(allocOpts, chromedp.Flag(k, c.cfg.ChromeFlags[k]))
			}
		}
		if c.cfg.UserAgent != "" {
			allocOpts = append(allocOpts, chromedp.UserAgent(c.cfg.UserAgent))
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

	return c.browserCtx, nil
}

func (c *Tool) shutdownBrowser() {
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

func (c *Tool) Shutdown() {
	c.mu.Lock()
	if c.idleStop != nil {
		select {
		case <-c.idleStop:
		default:
			close(c.idleStop)
		}
	}
	c.shutdownBrowser()
	c.mu.Unlock()
}

func (c *Tool) startIdleWatcher(timeout time.Duration) {
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

// blockPrivateBrowserTarget prevents navigation to private/internal IPs (SSRF protection).
func blockPrivateBrowserTarget(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}
	host := parsed.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %v", host, err)
	}
	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}
		if parsedIP.IsLoopback() || parsedIP.IsPrivate() || parsedIP.IsLinkLocalUnicast() {
			return fmt.Errorf("SSRF blocked: %q resolves to private IP %q", host, ip)
		}
	}
	return nil
}

func (c *Tool) navigate(ctx context.Context, url string) (*mcp.ToolResult, error) {
	if url == "" {
		return &mcp.ToolResult{Content: "url is required for navigate action", IsError: true}, nil
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	if err := blockPrivateBrowserTarget(url); err != nil {
		return &mcp.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	var title string
	navActions := []chromedp.Action{
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
	}
	if c.cfg.NavigationWait != "" {
		if d, err := time.ParseDuration(c.cfg.NavigationWait); err == nil && d > 0 {
			navActions = append(navActions, chromedp.Sleep(d))
		}
	}
	navActions = append(navActions, chromedp.Title(&title))
	err := chromedp.Run(ctx, navActions...)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("navigate to '%s' failed: %v", url, err), IsError: true}, nil
	}

	return &mcp.ToolResult{Content: fmt.Sprintf("Navigated to %s\nPage title: %s", url, title)}, nil
}

func (c *Tool) click(ctx context.Context, selector string) (*mcp.ToolResult, error) {
	if selector == "" {
		return &mcp.ToolResult{Content: "selector is required for click action", IsError: true}, nil
	}

	if err := chromedp.Run(ctx, chromedp.Click(selector, chromedp.NodeVisible)); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("click failed on '%s': %v", selector, err), IsError: true}, nil
	}

	return &mcp.ToolResult{Content: fmt.Sprintf("Clicked element: %s", selector)}, nil
}

func (c *Tool) screenshot(ctx context.Context, selector string) (*mcp.ToolResult, error) {
	var currentURL string
	if err := chromedp.Run(ctx, chromedp.Evaluate("window.location.href", &currentURL)); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("screenshot failed: cannot get current URL: %v", err), IsError: true}, nil
	}

	if currentURL == "about:blank" || currentURL == "" {
		return &mcp.ToolResult{Content: "screenshot failed: browser is on blank page, please navigate first", IsError: true}, nil
	}

	var buf []byte

	if selector != "" {
		if err := chromedp.Run(ctx, chromedp.Screenshot(selector, &buf, chromedp.NodeVisible)); err != nil {
			return &mcp.ToolResult{Content: fmt.Sprintf("element screenshot failed for '%s': %v", selector, err), IsError: true}, nil
		}
	} else {
		quality := c.cfg.ScreenshotQuality
		if quality <= 0 {
			quality = 100
		}
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, quality)); err != nil {
			return &mcp.ToolResult{Content: fmt.Sprintf("full page screenshot failed: %v", err), IsError: true}, nil
		}
	}

	screenshotDir := c.cfg.ScreenshotDir
	if screenshotDir == "" {
		screenshotDir = filepath.Join(config.ProjectConfigDir, "screenshots")
	}
	if err := os.MkdirAll(screenshotDir, 0700); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("create screenshot dir: %v", err), IsError: true}, nil
	}
	filename := fmt.Sprintf("screenshot_%s.png", time.Now().Format("20060102_150405"))
	filePath := filepath.Join(screenshotDir, filename)
	if err := os.WriteFile(filePath, buf, 0600); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("write screenshot: %v", err), IsError: true}, nil
	}

	return &mcp.ToolResult{
		Content: fmt.Sprintf("Screenshot saved: %s (%d bytes)", filePath, len(buf)),
	}, nil
}

func (c *Tool) evaluate(ctx context.Context, script string) (*mcp.ToolResult, error) {
	if script == "" {
		return &mcp.ToolResult{Content: "script is required for evaluate action", IsError: true}, nil
	}

	wrappedScript := script
	if strings.Contains(script, "await ") {
		wrappedScript = "(async () => { " + script + " })()"
	}

	var result string
	if err := chromedp.Run(ctx, chromedp.Evaluate(wrappedScript, &result)); err != nil {
		return &mcp.ToolResult{
			Content: fmt.Sprintf("evaluate failed:\nscript: %s\nerror: %v", truncate(script, 200), err),
			IsError: true,
		}, nil
	}

	if len(result) > 10000 {
		result = result[:10000] + "... [truncated]"
	}
	return &mcp.ToolResult{Content: result}, nil
}

func (c *Tool) getText(ctx context.Context, selector string) (*mcp.ToolResult, error) {
	if selector == "" {
		return &mcp.ToolResult{Content: "selector is required for get_text action", IsError: true}, nil
	}

	var text string
	if err := chromedp.Run(ctx, chromedp.Text(selector, &text, chromedp.NodeVisible)); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("get_text failed for '%s': %v", selector, err), IsError: true}, nil
	}

	text = strings.TrimSpace(text)
	if len(text) > 10000 {
		text = text[:10000] + "... [truncated]"
	}

	return &mcp.ToolResult{Content: text}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

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
