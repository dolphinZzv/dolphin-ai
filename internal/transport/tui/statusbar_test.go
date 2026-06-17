package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
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

// renderStatusBar drops left parts from the right until they fit.
func TestRenderStatusBar_DropsParts(t *testing.T) {
	parts := []string{"🐬 Dolphin v1.0.0", "minimax-m3", "yolo", "turn:5", "tools:3", "/exit"}
	out := renderStatusBar(parts, nil, 40)
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
	out := renderStatusBar(parts, nil, 80)
	if !strings.Contains(out, "Dolphin") || !strings.Contains(out, "minimax-m3") || !strings.Contains(out, "/exit") {
		t.Errorf("expected all parts in output, got: %s", out)
	}
}

// The right-side parts (e.g. session id) should be pinned to the right
// edge of the bar.
func TestRenderStatusBar_RightPinned(t *testing.T) {
	left := []string{"Dolphin", "/exit"}
	right := []string{"abc12345"}
	out := renderStatusBar(left, right, 80)
	if !strings.Contains(out, "Dolphin") {
		t.Errorf("expected left part, got: %s", out)
	}
	if !strings.Contains(out, "abc12345") {
		t.Errorf("expected right part, got: %s", out)
	}
	// The session id should sit at the right edge (last visible chars).
	trimmed := strings.TrimRight(ansiRe.ReplaceAllString(out, ""), " ")
	if !strings.HasSuffix(trimmed, "abc12345") {
		t.Errorf("expected right part at right edge, got: %q", trimmed)
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

// When the viewport is scrolled up (and nothing is being processed), a
// scroll-position indicator should appear with a jump-to-bottom hint.
func TestView_ScrollIndicatorWhenScrolledUp(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 80
	m.height = 24
	m.agentName = "Dolphin"
	m.viewport = viewport.New(80, 5)
	// Fill enough content that scrolling is possible, then scroll up.
	m.viewport.SetContent(strings.Repeat("line\n", 50))
	m.viewport.GotoBottom()
	m.viewport.ScrollUp(10) // scroll away from the bottom

	out := m.View()
	if !strings.Contains(out, "scrolled to") {
		t.Errorf("expected scroll indicator, got: %s", out)
	}
	if !strings.Contains(out, "Ctrl+G") {
		t.Errorf("expected Ctrl+G hint, got: %s", out)
	}
}

// Ctrl+G jumps the viewport back to the bottom.
func TestCtrlG_JumpsToBottom(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 80
	m.height = 24
	m.viewport = viewport.New(80, 5)
	m.viewport.SetContent(strings.Repeat("line\n", 50))
	m.viewport.GotoBottom()
	m.viewport.ScrollUp(10)
	if m.viewport.AtBottom() {
		t.Fatal("expected to be scrolled up before Ctrl+G")
	}

	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = newM.(model)
	if !m.viewport.AtBottom() {
		t.Errorf("expected viewport at bottom after Ctrl+G")
	}
}

// While a turn is pending, the status bar should show a working spinner
// with elapsed time so the user gets live feedback.
func TestView_SpinnerShownWhilePending(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 80
	m.height = 24
	m.agentName = "Dolphin"
	m.viewport = viewport.New(80, 10)
	m.viewport.SetContent("hello")
	m.currentMsg = "do something"
	m.msgStatus = "pending"
	m.msgStartedAt = time.Now().Add(-3 * time.Second)

	out := m.View()
	plain := ansiRe.ReplaceAllString(out, "")
	// A spinner frame and an elapsed "3s" should appear in the status bar.
	matched := false
	for _, f := range spinnerFrames {
		if strings.Contains(plain, f) {
			matched = true
			break
		}
	}
	if !matched {
		t.Errorf("expected a spinner frame in status bar, got: %s", plain)
	}
	if !strings.Contains(plain, "3s") {
		t.Errorf("expected elapsed '3s' in status bar, got: %s", plain)
	}
}

// The spinner should not appear once the turn has finished.
func TestView_NoSpinnerWhenNotPending(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 80
	m.height = 24
	m.agentName = "Dolphin"
	m.viewport = viewport.New(80, 10)
	m.viewport.SetContent("hello")
	m.msgStatus = "success"

	out := m.View()
	plain := ansiRe.ReplaceAllString(out, "")
	for _, f := range spinnerFrames {
		if strings.Contains(plain, f) {
			t.Errorf("spinner should not show when not pending, got: %s", plain)
			break
		}
	}
}

// renderCurrentMsg should show the username, the status icon, and — when
// scrolled — a right-aligned scroll-position + jump hint.
func TestRenderCurrentMsg(t *testing.T) {
	out := renderCurrentMsg("doing work", "alice", "pending", 80, 0.42)
	plain := ansiRe.ReplaceAllString(out, "")
	if !strings.Contains(plain, "alice") {
		t.Errorf("expected username, got: %s", plain)
	}
	if !strings.Contains(plain, "doing work") {
		t.Errorf("expected message body, got: %s", plain)
	}
	if !strings.Contains(plain, "⏳") {
		t.Errorf("expected pending icon, got: %s", plain)
	}
	if !strings.Contains(plain, "42%") {
		t.Errorf("expected scroll percent, got: %s", plain)
	}
	if !strings.Contains(plain, "Ctrl+G") {
		t.Errorf("expected Ctrl+G hint, got: %s", plain)
	}
}

func TestRenderCurrentMsg_StatusIcons(t *testing.T) {
	cases := []struct{ status, icon string }{
		{"success", "✅"},
		{"error", "❌"},
		{"pending", "⏳"},
	}
	for _, c := range cases {
		out := renderCurrentMsg("x", "u", c.status, 80, -1)
		plain := ansiRe.ReplaceAllString(out, "")
		if !strings.Contains(plain, c.icon) {
			t.Errorf("status %q: expected icon %s, got: %s", c.status, c.icon, plain)
		}
	}
}

// renderScrollIndicator shows the position and the jump hint.
func TestRenderScrollIndicator(t *testing.T) {
	out := renderScrollIndicator(80, 0.1)
	plain := ansiRe.ReplaceAllString(out, "")
	if !strings.Contains(plain, "scrolled to 10%") {
		t.Errorf("expected scroll percent text, got: %s", plain)
	}
	if !strings.Contains(plain, "Ctrl+G") {
		t.Errorf("expected Ctrl+G hint, got: %s", plain)
	}
}
