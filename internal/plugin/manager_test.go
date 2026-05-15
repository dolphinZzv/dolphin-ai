package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"dolphin/internal/event"
	"dolphin/internal/hook"

	"gopkg.in/yaml.v3"
)

func TestManagerActivateEmpty(t *testing.T) {
	hooks := hook.NewRegistry()
	bus := event.NewEventBus(16)
	m := NewManager(hooks, bus)

	// Activate with no plugins should not crash
	m.Activate()
	if len(m.List()) != 0 {
		t.Errorf("expected no plugins, got %v", m.List())
	}
}

func TestManagerRegisterAndActivate(t *testing.T) {
	hooks := hook.NewRegistry()
	bus := event.NewEventBus(16)
	m := NewManager(hooks, bus)

	// Register a Go plugin
	p := &testPlugin{name: "test-1", hookPoint: hook.PointUserInput}
	m.Register(p)
	m.Activate()

	if len(m.List()) != 1 {
		t.Fatalf("expected 1 plugin, got %v", m.List())
	}

	// Verify hook is registered
	if !hooks.HasAny(hook.PointUserInput) {
		t.Error("hook should be registered for user:input")
	}

	// Verify event is registered
	var evtMu sync.Mutex
	var receivedEvents []event.Event
	bus.On(event.TypeError, func(ctx context.Context, evt event.Event) {
		evtMu.Lock()
		receivedEvents = append(receivedEvents, evt)
		evtMu.Unlock()
	})
}

func TestManagerActivateHookAbort(t *testing.T) {
	hooks := hook.NewRegistry()
	bus := event.NewEventBus(16)
	m := NewManager(hooks, bus)

	m.Register(&testPlugin{
		name:      "blocker",
		hookPoint: hook.PointBeforeTool,
		hookErr:   errors.New("blocked by test"),
	})
	m.Activate()

	hc := &hook.Context{SessionID: "s1", ToolName: "dangerous_tool"}
	err := hooks.Fire(context.Background(), hook.PointBeforeTool, hc)
	if err == nil || err.Error() != "blocked by test" {
		t.Fatalf("expected 'blocked by test' error, got: %v", err)
	}
}

func TestLoadScriptsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	plugins, err := LoadScripts(dir)
	if err != nil {
		t.Fatalf("LoadScripts: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins from empty dir, got %d", len(plugins))
	}
}

func TestLoadScriptsNonExistentDir(t *testing.T) {
	plugins, err := LoadScripts("/nonexistent/path/plugins")
	if err != nil {
		t.Fatalf("LoadScripts should not error on nonexistent dir: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins from nonexistent dir, got %d", len(plugins))
	}
}

func TestLoadScriptsValidPlugin(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "hello-plugin")
	os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0755)
	os.MkdirAll(filepath.Join(pluginDir, "events"), 0755)

	// plugin.yaml
	meta, _ := yaml.Marshal(scriptPluginMeta{
		Name:        "hello-plugin",
		Version:     "1.0",
		Description: "A test plugin",
	})
	os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), meta, 0644)

	// Create a hook script
	hookScript := `#!/bin/sh
# Reject empty input
input=$(cat)
user_input=$(echo "$input" | grep -o '"user_input":"[^"]*"' | cut -d'"' -f4)
if [ -z "$user_input" ]; then
  echo "empty input rejected" >&2
  exit 1
fi
exit 0
`
	os.WriteFile(filepath.Join(pluginDir, "hooks", "user:input.sh"), []byte(hookScript), 0755)

	// Create an event script
	eventScript := `#!/bin/sh
cat > /dev/null
exit 0
`
	os.WriteFile(filepath.Join(pluginDir, "events", "error.sh"), []byte(eventScript), 0755)

	plugins, err := LoadScripts(dir)
	if err != nil {
		t.Fatalf("LoadScripts: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Name() != "hello-plugin" {
		t.Errorf("expected name 'hello-plugin', got %q", plugins[0].Name())
	}
}

func TestLoadScriptsSkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	hiddenDir := filepath.Join(dir, ".hidden-plugin")
	os.MkdirAll(hiddenDir, 0755)

	plugins, err := LoadScripts(dir)
	if err != nil {
		t.Fatalf("LoadScripts: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins (hidden dir skipped), got %d", len(plugins))
	}
}

func TestLoadScriptsSkipsWithoutYAML(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "no-yaml")
	os.MkdirAll(pluginDir, 0755)

	plugins, err := LoadScripts(dir)
	if err != nil {
		t.Fatalf("LoadScripts: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins (no yaml), got %d", len(plugins))
	}
}

func TestRunHookScriptExitZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping Unix shell script test on Windows")
	}
	// Write a simple script that always passes
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "pass.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755)

	err := runHookScript(context.Background(), scriptPath, hook.PointUserInput, &hook.Context{
		SessionID: "s1",
		Turn:      1,
		UserInput: "hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestRunHookScriptExitNonZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping Unix shell script test on Windows")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fail.sh")
	script := `#!/bin/sh
echo "rejected for testing" >&2
exit 1
`
	os.WriteFile(scriptPath, []byte(script), 0755)

	err := runHookScript(context.Background(), scriptPath, hook.PointBeforeTool, &hook.Context{
		SessionID: "s1",
		ToolName:  "test_tool",
	})
	if err == nil {
		t.Fatal("expected error from exit 1")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("expected stderr in error, got: %v", err)
	}
}

func TestRunHookScriptOutputToValues(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping Unix shell script test on Windows")
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "echo.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\necho '{\"sanitized\": true}'\n"), 0755)

	hc := &hook.Context{
		SessionID: "s1",
		ToolArgs:  []byte(`{"input":"raw"}`),
		Values:    make(map[string]any),
	}
	err := runHookScript(context.Background(), scriptPath, hook.PointBeforeTool, hc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check Values has the plugin output
	key := "plugin." + filepath.Base(filepath.Dir(scriptPath)) + ".output"
	if v, ok := hc.Values[key]; !ok || v.(string) != `{"sanitized": true}` {
		t.Errorf("expected output in Values, got: %v", hc.Values)
	}
}

func TestRunEventScript(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "log.sh")
	os.WriteFile(scriptPath, []byte("#!/bin/sh\ncat > /dev/null\nexit 0\n"), 0755)

	// Should not panic or error — event scripts are fire-and-forget
	runEventScript(context.Background(), scriptPath, event.TypeError, event.Event{
		Type:      event.TypeError,
		SessionID: "s1",
	})
}

// testPlugin is a mock Go plugin for testing.
type testPlugin struct {
	name      string
	hookPoint hook.Point
	hookErr   error
}

func (p *testPlugin) Name() string { return p.name }

func (p *testPlugin) Register(reg *Registry) {
	reg.AddHook(p.hookPoint, 0, func(ctx context.Context, hc *hook.Context) error {
		if p.hookErr != nil {
			return p.hookErr
		}
		return nil
	})
	reg.AddEvent(event.TypeError, func(ctx context.Context, evt event.Event) {})
}
