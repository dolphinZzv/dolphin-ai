package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"dolphin/internal/config"
	"dolphin/internal/mcp"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// configInput is the JSON-unmarshal shape for the config tool.
type configInput struct {
	Action string `json:"action"` // "list", "get", "set", "save"
	Path   string `json:"path"`
	Value  any    `json:"value"`
	File   string `json:"file"` // target file for save (or use name)
}

// configEntry defines a single configurable path with getter and setter.
type configEntry struct {
	path        string
	description string
	get         func(*config.Config) any
	set         func(*config.Config, any) error
	needsSync   bool // true => call rebuildCompressor after set
}

var configurablePaths = []configEntry{
	// LLM
	{
		path: "llm.temperature", description: "LLM temperature (0.0–2.0)",
		get: func(c *config.Config) any { return c.LLM.Temperature },
		set: func(c *config.Config, v any) error {
			f, err := toFloat(v)
			if err != nil {
				return err
			}
			c.LLM.Temperature = f
			return nil
		},
	},
	{
		path: "llm.max_tokens", description: "Max tokens per LLM response",
		get: func(c *config.Config) any { return c.LLM.MaxTokens },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.LLM.MaxTokens = n
			return nil
		},
	},
	{
		path: "llm.max_context_tokens", description: "Max context window before compression",
		get: func(c *config.Config) any { return c.LLM.MaxContextTokens },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.LLM.MaxContextTokens = n
			return nil
		},
	},
	{
		path: "llm.max_sub_turns", description: "Max tool-call feedback loops per turn",
		get: func(c *config.Config) any { return c.LLM.MaxSubTurns },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.LLM.MaxSubTurns = n
			return nil
		},
	},
	{
		path: "llm.compress_mode", description: "Context compression mode (e.g. drop, summarize)",
		get: func(c *config.Config) any { return c.LLM.CompressMode },
		set: func(c *config.Config, v any) error {
			s, err := toString(v)
			if err != nil {
				return err
			}
			c.LLM.CompressMode = s
			return nil
		},
		needsSync: true,
	},
	{
		path: "llm.segment_merge_limit", description: "Max segments before merge",
		get: func(c *config.Config) any { return c.LLM.SegmentMergeLimit },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.LLM.SegmentMergeLimit = n
			return nil
		},
		needsSync: true,
	},
	// MCP Shell
	{
		path: "mcp.shell.timeout_seconds", description: "Shell command timeout in seconds",
		get: func(c *config.Config) any { return c.MCP.Shell.TimeoutSeconds },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.MCP.Shell.TimeoutSeconds = n
			return nil
		},
	},
	{
		path: "mcp.shell.max_command_length", description: "Max shell command length",
		get: func(c *config.Config) any { return c.MCP.Shell.MaxCommandLength },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.MCP.Shell.MaxCommandLength = n
			return nil
		},
	},
	{
		path: "mcp.shell.allowed_commands", description: "Allowed shell commands (empty = allow all)",
		get: func(c *config.Config) any { return c.MCP.Shell.AllowedCommands },
		set: func(c *config.Config, v any) error {
			ss, err := toStrings(v)
			if err != nil {
				return err
			}
			c.MCP.Shell.AllowedCommands = ss
			return nil
		},
	},
	{
		path: "mcp.shell.enabled", description: "Enable shell tool",
		get: func(c *config.Config) any { return c.MCP.Shell.Enabled },
		set: func(c *config.Config, v any) error {
			b, err := toBool(v)
			if err != nil {
				return err
			}
			c.MCP.Shell.Enabled = b
			return nil
		},
	},
	// MCP CDP
	{
		path: "mcp.cdp.headless", description: "Run browser in headless mode",
		get: func(c *config.Config) any { return c.MCP.CDP.Headless },
		set: func(c *config.Config, v any) error {
			b, err := toBool(v)
			if err != nil {
				return err
			}
			c.MCP.CDP.Headless = b
			return nil
		},
	},
	{
		path: "mcp.cdp.idle_timeout", description: "Browser idle timeout in seconds",
		get: func(c *config.Config) any { return c.MCP.CDP.IdleTimeout },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.MCP.CDP.IdleTimeout = n
			return nil
		},
	},
	{
		path: "mcp.cdp.startup_timeout", description: "Browser startup verify timeout in seconds",
		get: func(c *config.Config) any { return c.MCP.CDP.StartupTimeout },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.MCP.CDP.StartupTimeout = n
			return nil
		},
	},
	{
		path: "mcp.cdp.enabled", description: "Enable CDP browser tool",
		get: func(c *config.Config) any { return c.MCP.CDP.Enabled },
		set: func(c *config.Config, v any) error {
			b, err := toBool(v)
			if err != nil {
				return err
			}
			c.MCP.CDP.Enabled = b
			return nil
		},
	},
	// MCP Email
	{
		path: "mcp.email.enabled", description: "Enable email MCP tool",
		get: func(c *config.Config) any { return c.MCP.Email.Enabled },
		set: func(c *config.Config, v any) error {
			b, err := toBool(v)
			if err != nil {
				return err
			}
			c.MCP.Email.Enabled = b
			return nil
		},
	},
	// MCP Webhook
	{
		path: "mcp.webhook.enabled", description: "Enable webhook MCP tool",
		get: func(c *config.Config) any { return c.MCP.Webhook.Enabled },
		set: func(c *config.Config, v any) error {
			b, err := toBool(v)
			if err != nil {
				return err
			}
			c.MCP.Webhook.Enabled = b
			return nil
		},
	},
	// Session
	{
		path: "session.max_loop", description: "Max turns per session before checkpoint",
		get: func(c *config.Config) any { return c.Session.MaxLoop },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.Session.MaxLoop = n
			return nil
		},
	},
	{
		path: "session.summary", description: "Auto-generate session summary",
		get: func(c *config.Config) any { return c.Session.Summary },
		set: func(c *config.Config, v any) error {
			b, err := toBool(v)
			if err != nil {
				return err
			}
			c.Session.Summary = b
			return nil
		},
	},
	// Agent Pool
	{
		path: "agent_pool.max_pending_results", description: "Max pending sub-agent results in context",
		get: func(c *config.Config) any { return c.Pool.MaxPendingResults },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.Pool.MaxPendingResults = n
			return nil
		},
	},
	{
		path: "agent_pool.max_pending_result_len", description: "Max chars per pending result in prompt (0 = no truncation)",
		get: func(c *config.Config) any { return c.Pool.MaxPendingResultLen },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.Pool.MaxPendingResultLen = n
			return nil
		},
	},
	{
		path: "mcp.shell.allow_unrestricted", description: "Allow unrestricted shell access when no command whitelist",
		get: func(c *config.Config) any { return c.MCP.Shell.AllowUnrestricted },
		set: func(c *config.Config, v any) error {
			b, err := toBool(v)
			if err != nil {
				return err
			}
			c.MCP.Shell.AllowUnrestricted = b
			return nil
		},
	},
	// Session
	{
		path: "session.max_age", description: "Session max age (e.g. 24h, 7d)",
		get: func(c *config.Config) any { return c.Session.MaxAge },
		set: func(c *config.Config, v any) error {
			s, err := toString(v)
			if err != nil {
				return err
			}
			c.Session.MaxAge = s
			return nil
		},
	},
	{
		path: "session.resume", description: "Auto-resume last session on interactive transports",
		get: func(c *config.Config) any { return c.Session.Resume },
		set: func(c *config.Config, v any) error {
			b, err := toBool(v)
			if err != nil {
				return err
			}
			c.Session.Resume = b
			return nil
		},
	},
	// Skills
	{
		path: "skills.max_top", description: "Max skills shown in system prompt (default: 10)",
		get: func(c *config.Config) any { return c.Skills.MaxTop },
		set: func(c *config.Config, v any) error {
			n, err := toInt(v)
			if err != nil {
				return err
			}
			c.Skills.MaxTop = n
			return nil
		},
	},
	// Resource monitor
	{
		path: "resource.enabled", description: "Enable periodic resource monitoring",
		get: func(c *config.Config) any { return c.Resource.Enabled },
		set: func(c *config.Config, v any) error {
			b, err := toBool(v)
			if err != nil {
				return err
			}
			c.Resource.Enabled = b
			return nil
		},
	},
	{
		path: "resource.interval", description: "Resource sampling interval (e.g. 30s, 1m)",
		get: func(c *config.Config) any { return c.Resource.Interval },
		set: func(c *config.Config, v any) error {
			s, err := toString(v)
			if err != nil {
				return err
			}
			c.Resource.Interval = s
			return nil
		},
	},
	// Logging
	{
		path: "log_level", description: "Log level (debug, info, warn, error)",
		get: func(c *config.Config) any { return c.LogLevel },
		set: func(c *config.Config, v any) error {
			s, err := toString(v)
			if err != nil {
				return err
			}
			c.LogLevel = s
			return nil
		},
	},
}

func findConfigEntry(path string) *configEntry {
	for i := range configurablePaths {
		if configurablePaths[i].path == path {
			return &configurablePaths[i]
		}
	}
	return nil
}

// findConfigChildren returns all entries whose path starts with prefix + "."
func findConfigChildren(prefix string) []configEntry {
	var children []configEntry
	p := prefix + "."
	for _, entry := range configurablePaths {
		if strings.HasPrefix(entry.path, p) {
			children = append(children, entry)
		}
	}
	return children
}

func (c *Coordinator) handleConfig(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params configInput
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}

	switch params.Action {
	case "list":
		return c.handleConfigList()
	case "get":
		return c.handleConfigGet(params.Path)
	case "set":
		if !c.agent.cfg.Flags.SelfEvolution {
			return configWriteErr(), nil
		}
		return c.handleConfigSet(params.Path, params.Value)
	case "save":
		if !c.agent.cfg.Flags.SelfEvolution {
			return configWriteErr(), nil
		}
		return c.handleConfigSave(params.File)
	case "delete":
		if !c.agent.cfg.Flags.SelfEvolution {
			return configWriteErr(), nil
		}
		return c.handleConfigDelete(params.Path)
	default:
		return &mcp.ToolResult{
			Content: fmt.Sprintf("unknown action %q — use list, get, set, save, or delete", params.Action),
			IsError: true,
		}, nil
	}
}

func (c *Coordinator) handleConfigList() (*mcp.ToolResult, error) {
	var b strings.Builder
	b.WriteString("=== Configurable Settings ===\n\n")

	var lastSection string
	for _, entry := range configurablePaths {
		section := sectionOf(entry.path)
		if section != lastSection {
			fmt.Fprintf(&b, "\n── %s ──\n", section)
			lastSection = section
		}
		val := entry.get(c.agent.cfg)
		fmt.Fprintf(&b, "  %s = %v\n", entry.path, formatValue(val))
		fmt.Fprintf(&b, "    %s\n", entry.description)
	}

	b.WriteString("\nUse `get <path>` to read a single value, `set <path> <value>` to modify, `save` to persist to disk.")
	return &mcp.ToolResult{Content: b.String()}, nil
}

func (c *Coordinator) handleConfigGet(path string) (*mcp.ToolResult, error) {
	if path == "" {
		return &mcp.ToolResult{Content: "path is required", IsError: true}, nil
	}
	entry := findConfigEntry(path)
	if entry == nil {
		// Check if it's a group prefix with children
		children := findConfigChildren(path)
		if len(children) == 0 {
			return &mcp.ToolResult{Content: fmt.Sprintf("unknown path %q — use list to see all paths", path), IsError: true}, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "=== %s ===\n\n", path)
		for _, ch := range children {
			val := ch.get(c.agent.cfg)
			fmt.Fprintf(&b, "%s = %v\n  %s\n\n", ch.path, formatValue(val), ch.description)
		}
		return &mcp.ToolResult{Content: b.String()}, nil
	}
	val := entry.get(c.agent.cfg)
	return &mcp.ToolResult{
		Content: fmt.Sprintf("%s = %v\n%s", entry.path, formatValue(val), entry.description),
	}, nil
}

func (c *Coordinator) handleConfigSet(path string, value any) (*mcp.ToolResult, error) {
	if path == "" {
		return &mcp.ToolResult{Content: "path is required", IsError: true}, nil
	}
	entry := findConfigEntry(path)
	if entry == nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("unknown path %q — use list to see all paths", path), IsError: true}, nil
	}
	oldVal := entry.get(c.agent.cfg)
	if err := entry.set(c.agent.cfg, value); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("set %s: %v", path, err), IsError: true}, nil
	}

	if entry.needsSync {
		c.agent.rebuildCompressor()
		zap.S().Infow("config changed: compressor rebuilt", "path", path)
	}

	zap.S().Infow("config changed", "path", path, "old", oldVal, "new", value)
	return &mcp.ToolResult{
		Content: fmt.Sprintf("Set %s = %v (was: %v). Changes are in-memory only. Use `save` to persist to disk, or ask the user if they want to save.", path, formatValue(value), formatValue(oldVal)),
	}, nil
}

func (c *Coordinator) handleConfigSave(filePath string) (*mcp.ToolResult, error) {
	if filePath == "" {
		filePath = filepath.Join(config.ProjectConfigDir, config.ConfigFileName+".yaml")
	}

	// Security: restrict save path to config directory
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("cannot resolve path %q: %v", filePath, err), IsError: true}, nil
	}
	cfgDir, _ := filepath.Abs(config.ProjectConfigDir)
	if !strings.HasPrefix(absPath, cfgDir) {
		return &mcp.ToolResult{
			Content: fmt.Sprintf("security: save path %q is outside config directory %q", filePath, cfgDir),
			IsError: true,
		}, nil
	}

	// Load existing file if present (to preserve un-tracked settings)
	existing := make(map[string]any)
	//nolint:govet
	if data, err := os.ReadFile(filePath); err == nil {
		if err := yaml.Unmarshal(data, &existing); err != nil {
			zap.S().Warnw("failed to parse existing config, starting fresh", "path", filePath, "error", err)
		}
	}

	// Overlay all tracked config values onto the map
	overlayConfig(existing, c.agent.cfg)

	// Marshal and write
	data, err := yaml.Marshal(existing)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("marshal config: %v", err), IsError: true}, nil
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("create config dir: %v", err), IsError: true}, nil
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("write config: %v", err), IsError: true}, nil
	}

	zap.S().Infow("config saved to disk", "file", filePath)
	return &mcp.ToolResult{Content: fmt.Sprintf("Config persisted to %s", filePath)}, nil
}

func configWriteErr() *mcp.ToolResult {
	return &mcp.ToolResult{
		Content: "modifying config requires self_evolution to be enabled (set flags.self_evolution = true in config.yaml)",
		IsError: true,
	}
}

func (c *Coordinator) handleConfigDelete(path string) (*mcp.ToolResult, error) {
	if path == "" {
		return &mcp.ToolResult{Content: "path is required", IsError: true}, nil
	}
	entry := findConfigEntry(path)
	if entry == nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("unknown path %q — use list to see all paths", path), IsError: true}, nil
	}
	oldVal := entry.get(c.agent.cfg)
	var zero any
	switch oldVal.(type) {
	case string:
		zero = ""
	case int:
		zero = 0
	case float64:
		zero = float64(0)
	case bool:
		zero = false
	case []string:
		zero = []string{}
	default:
		zero = nil
	}
	if err := entry.set(c.agent.cfg, zero); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("delete %s: %v", path, err), IsError: true}, nil
	}
	if entry.needsSync {
		c.agent.rebuildCompressor()
	}
	zap.S().Infow("config deleted (reset to zero)", "path", path, "old", oldVal)
	return &mcp.ToolResult{
		Content: fmt.Sprintf("Reset %s to default (was: %v). Use `save` to persist to disk.", path, formatValue(oldVal)),
	}, nil
}

// ---- Value coercion helpers ----

func toFloat(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	case json.Number:
		return val.Float64()
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

func toInt(v any) (int, error) {
	switch val := v.(type) {
	case float64:
		return int(val), nil
	case int:
		return val, nil
	case string:
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("invalid integer %q", val)
		}
		return n, nil
	case json.Number:
		n, err := val.Int64()
		if err != nil {
			return 0, err
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

func toBool(v any) (bool, error) {
	switch val := v.(type) {
	case bool:
		return val, nil
	case string:
		return strconv.ParseBool(val)
	default:
		return false, fmt.Errorf("expected boolean, got %T", v)
	}
}

func toString(v any) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	default:
		return "", fmt.Errorf("expected string, got %T", v)
	}
}

func toStrings(v any) ([]string, error) {
	switch val := v.(type) {
	case []any:
		ss := make([]string, len(val))
		for i, item := range val {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected string at index %d, got %T", i, item)
			}
			ss[i] = s
		}
		return ss, nil
	case []string:
		return val, nil
	case string:
		if val == "" {
			return []string{}, nil
		}
		return strings.Split(val, ","), nil
	default:
		return nil, fmt.Errorf("expected array of strings, got %T", v)
	}
}

// ---- Formatting helpers ----

func sectionOf(path string) string {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func formatValue(v any) string {
	if v == nil {
		return "<nil>"
	}
	switch val := v.(type) {
	case []string:
		if len(val) == 0 {
			return "[]"
		}
		return "[" + strings.Join(val, ", ") + "]"
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ---- Config overlay for save ----
// overlayConfig applies all tracked config values onto a generic map,
// so the written YAML preserves existing settings and only overrides
// the paths managed by this tool.

func overlayConfig(m map[string]any, cfg *config.Config) {
	for _, entry := range configurablePaths {
		val := entry.get(cfg)
		deepSet(m, entry.path, val)
	}
}

func deepSet(m map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	for i, part := range parts {
		if i == len(parts)-1 {
			m[part] = value
			return
		}
		inner, ok := m[part].(map[string]any)
		if !ok {
			inner = make(map[string]any)
			m[part] = inner
		}
		m = inner
	}
}
