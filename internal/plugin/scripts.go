package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/event"
	"dolphin/internal/hook"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// scriptPluginMeta is the structure of a plugin.yaml file.
type scriptPluginMeta struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
}

// LoadScripts scans dir for plugin subdirectories and returns Plugin instances
// for each valid script plugin found. Non-existent dir is not an error.
func LoadScripts(dir string) ([]Plugin, error) {
	if dir == "" {
		return nil, nil
	}
	expanded, err := expandPath(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(expanded)
	if err != nil {
		if os.IsNotExist(err) {
				zap.S().Debugw("plugin: directory not found, skipping", "dir", expanded)
			return nil, nil
		}
		return nil, fmt.Errorf("read plugins dir: %w", err)
	}

	var plugins []Plugin
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		p, err := loadScriptPlugin(filepath.Join(expanded, entry.Name()))
		if err != nil {
			zap.S().Warnw("skipping invalid script plugin", "dir", entry.Name(), "error", err)
			continue
		}
		if p != nil {
			plugins = append(plugins, p)
		}
	}
	return plugins, nil
}

// loadScriptPlugin loads a single script plugin from dir.
func loadScriptPlugin(dir string) (Plugin, error) {
	// Parse plugin.yaml
	metaPath := filepath.Join(dir, "plugin.yaml")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read plugin.yaml: %w", err)
	}
	var meta scriptPluginMeta
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&meta); err != nil {
		return nil, fmt.Errorf("parse plugin.yaml: %w", err)
	}
	if meta.Name == "" {
		return nil, fmt.Errorf("plugin.yaml missing 'name'")
	}

	// Discover hook scripts: hooks/<point>.sh or hooks/<point>
	hooks := discoverHookScripts(filepath.Join(dir, "hooks"))

	// Discover event scripts: events/<type>.sh or events/<type>
	events := discoverEventScripts(filepath.Join(dir, "events"))

	if len(hooks) == 0 && len(events) == 0 {
		zap.S().Debugw("script plugin has no hooks or events, skipping", "name", meta.Name)
		return nil, nil
	}

	zap.S().Infow("script plugin loaded", "name", meta.Name, "version", meta.Version,
		"hooks", len(hooks), "events", len(events))

	return &scriptPlugin{
		name:        meta.Name,
		description: meta.Description,
		hooks:       hooks,
		events:      events,
	}, nil
}

// discoverHookScripts scans hooks/ for executable scripts named after hook points.
func discoverHookScripts(dir string) map[hook.Point]string {
	result := make(map[hook.Point]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := trimScriptExt(entry.Name())
		point := hook.Point(name)
		// Validate the point name
		switch point {
		case hook.PointSessionStart, hook.PointSessionEnd,
			hook.PointUserInput, hook.PointBeforeLLM, hook.PointAfterLLM,
			hook.PointBeforeTool, hook.PointAfterTool,
			hook.PointBeforeResponse, hook.PointOnError:
			result[point] = filepath.Join(dir, entry.Name())
		}
	}
	return result
}

// discoverEventScripts scans events/ for executable scripts named after event types.
func discoverEventScripts(dir string) map[event.Type]string {
	result := make(map[event.Type]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := trimScriptExt(entry.Name())
		evtType := event.Type(name)
		// Validate the event type name or wildcard
		if evtType == "*" || isValidEventType(evtType) {
			result[evtType] = filepath.Join(dir, entry.Name())
		}
	}
	return result
}

func isValidEventType(t event.Type) bool {
	for _, at := range event.AllTypes {
		if at == t {
			return true
		}
	}
	return false
}

// scriptPlugin implements Plugin for a directory of shell scripts.
type scriptPlugin struct {
	name        string
	description string
	hooks       map[hook.Point]string // point → script path
	events      map[event.Type]string // event type → script path
}

func (p *scriptPlugin) Name() string { return p.name }

func (p *scriptPlugin) Register(reg *Registry) {
	for point, scriptPath := range p.hooks {
		pt := point // capture
		sp := scriptPath
		reg.AddHook(pt, 0, func(ctx context.Context, hc *hook.Context) error {
			return runHookScript(ctx, sp, pt, hc)
		})
	}
	for evtType, scriptPath := range p.events {
		et := evtType
		sp := scriptPath
		reg.AddEvent(et, func(ctx context.Context, evt event.Event) {
			runEventScript(ctx, sp, et, evt)
		})
	}
}

// runHookScript executes a script for a hook point. stdin ← HookContext JSON.
// exit 0 = ok, exit non-zero = error (abort). stdout captured as Values entry.
func runHookScript(ctx context.Context, scriptPath string, point hook.Point, hc *hook.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Marshal a JSON object that combines HookContext fields + point metadata
	input := map[string]any{
		"hook_point": string(point),
		"session_id": hc.SessionID,
		"turn":       hc.Turn,
	}
	if hc.UserInput != "" {
		input["user_input"] = hc.UserInput
	}
	if hc.ToolName != "" {
		input["tool_name"] = hc.ToolName
	}
	if len(hc.ToolArgs) > 0 {
		var args any
		json.Unmarshal(hc.ToolArgs, &args)
		input["tool_args"] = args
	}
	if hc.Error != nil {
		input["error"] = hc.Error.Error()
	}

	stdin, _ := json.Marshal(input)

	cmd := shellCommand(ctx, scriptPath)
	cmd.Stdin = strings.NewReader(string(stdin))
	cmd.Env = append(os.Environ(),
		"DOLPHIN_SESSION_ID="+hc.SessionID,
		"DOLPHIN_HOOK_POINT="+string(point),
	)

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("hook script %s timed out after 3s", point)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			zap.S().Debugw("hook script rejected", "point", string(point),
				"script", scriptPath, "stderr", stderr)
			return fmt.Errorf("hook %s rejected by %s: %s", point, filepath.Base(scriptPath), strings.TrimSpace(stderr))
		}
		return fmt.Errorf("hook script %s: %w", point, err)
	}

	// Store stdout in Values for downstream hooks
	if len(output) > 0 {
		if hc.Values == nil {
			hc.Values = make(map[string]any)
		}
		hc.Values["plugin."+filepath.Base(filepath.Dir(scriptPath))+".output"] = strings.TrimSpace(string(output))
	}
	return nil
}

// runEventScript executes a script for an event. stdin ← Event JSON. Fire-and-forget.
func runEventScript(ctx context.Context, scriptPath string, evtType event.Type, evt event.Event) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	data, _ := json.Marshal(evt)

	cmd := shellCommand(ctx, scriptPath)
	cmd.Stdin = strings.NewReader(string(data))
	cmd.Env = append(os.Environ(),
		"DOLPHIN_SESSION_ID="+evt.SessionID,
		"DOLPHIN_EVENT_TYPE="+string(evtType),
	)

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == nil {
			zap.S().Debugw("event script failed", "type", string(evtType), "script", scriptPath, "error", err)
		}
		return
	}
	if len(output) > 0 {
		zap.S().Debugw("event script output", "type", string(evtType), "script", scriptPath, "output", strings.TrimSpace(string(output)))
	}
}

func expandPath(path string) (string, error) {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Clean(home + path[1:]), nil
	}
	return filepath.Clean(path), nil
}
