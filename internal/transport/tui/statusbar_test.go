package tui

import (
	"strings"
	"testing"
)

func TestFormatCount(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{1200, "1.2k"},
		{999999, "1000.0k"},
		{1_000_000, "1.0m"},
		{1_500_000, "1.5m"},
		{1_000_000_000, "1.0b"},
		{2_300_000_000, "2.3b"},
		{1_000_000_000_000, "1.0t"},
		{4_000_000_000_000, "4.0t"},
	}
	for _, c := range cases {
		got := formatCount(c.in)
		if got != c.want {
			t.Errorf("formatCount(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// renderStatusBar drops parts from the right until they fit.
func TestRenderStatusBar_DropsParts(t *testing.T) {
	parts := []string{"🐬 Dolphin v1.0.0", "minimax-m3", "yolo", "turn:5", "tools:3", "/exit"}
	out := renderStatusBar(parts, 40)
	// Should render something non-empty even if it can't fit all parts.
	if out == "" {
		t.Fatal("expected non-empty status bar")
	}
	// At minimum, the first part (agent name) should be present.
	if !strings.Contains(out, "Dolphin") {
		t.Errorf("expected Dolphin in output, got: %s", out)
	}
}

func TestRenderStatusBar_FitsAll(t *testing.T) {
	parts := []string{"Dolphin", "minimax-m3", "/exit"}
	out := renderStatusBar(parts, 80)
	if !strings.Contains(out, "Dolphin") || !strings.Contains(out, "minimax-m3") || !strings.Contains(out, "/exit") {
		t.Errorf("expected all parts in output, got: %s", out)
	}
}

// Side status panel should render temperature and pool size.
func TestRenderSideStatus_ShowsTempAndPool(t *testing.T) {
	m := newModel()
	m.width = 160
	m.modelName = "minimax-m3"
	m.temperature = 1.0
	m.poolSize = 2
	m.showTools = true
	m.showThinking = false

	out := m.renderSideStatus()
	if out == "" {
		t.Fatal("expected non-empty side status at width 160")
	}
	if !strings.Contains(out, "minimax-m3") {
		t.Errorf("expected model name in side status, got: %s", out)
	}
	if !strings.Contains(out, "temp") {
		t.Errorf("expected temp label, got: %s", out)
	}
	if !strings.Contains(out, "1.0") {
		t.Errorf("expected temperature value 1.0, got: %s", out)
	}
	if !strings.Contains(out, "pool") {
		t.Errorf("expected pool label, got: %s", out)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("expected pool value 2, got: %s", out)
	}
}

// Side status should include turn/usage when set.
func TestRenderSideStatus_ShowsUsage(t *testing.T) {
	m := newModel()
	m.width = 160
	m.rounds = 5
	m.hardReqs = 1000
	m.reqs = 49
	m.hardTokens = 1000000
	m.tokens = 49000
	m.toolCalls = 3

	out := m.renderSideStatus()
	if !strings.Contains(out, "turn") {
		t.Errorf("expected turn label, got: %s", out)
	}
	if !strings.Contains(out, "req") {
		t.Errorf("expected req label, got: %s", out)
	}
	if !strings.Contains(out, "tok") {
		t.Errorf("expected tok label, got: %s", out)
	}
	if !strings.Contains(out, "49/4.9%") {
		t.Errorf("expected req value 49/4.9%%, got: %s", out)
	}
}

// viewportWidth should shrink when the side panel is visible.
func TestViewportWidth_NarrowHidesSidePanel(t *testing.T) {
	m := newModel()
	m.width = 50
	// 20% of 50 = 10, below minSideStatusWidth (16) → panel hidden,
	// viewport takes full width.
	if got := m.viewportWidth(); got != 50 {
		t.Errorf("narrow viewportWidth = %d, want 50", got)
	}

	m.width = 100
	// 20% of 100 = 20, viewport = 100 - 20 - 1 = 79.
	want := 100 - sideStatusWidth(100) - 1
	if got := m.viewportWidth(); got != want {
		t.Errorf("wide viewportWidth = %d, want %d", got, want)
	}
}

// Full View() should produce a renderable string with the side panel
// when the terminal is wide enough.
func TestView_WithSidePanel(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 160
	m.height = 30
	m.agentName = "Dolphin"
	m.version = "v1"
	m.modelName = "minimax-m3"
	m.temperature = 1.0
	m.poolSize = 1
	m.viewport.Width = m.viewportWidth()
	m.viewport.Height = 20
	m.viewport.SetContent("hello")

	out := m.View()
	if !strings.Contains(out, "Dolphin") {
		t.Errorf("expected agent name, got: %s", out)
	}
	if !strings.Contains(out, "Status") {
		t.Errorf("expected side panel header 'Status', got: %s", out)
	}
	if !strings.Contains(out, "temp") {
		t.Errorf("expected temp in side panel, got: %s", out)
	}
}
