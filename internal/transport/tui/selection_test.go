package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// --- Coordinate mapping tests ---

func TestViewportStartY(t *testing.T) {
	m := newModel()

	// At bottom (default): viewport starts at row 0.
	if got := m.viewportStartY(); got != 0 {
		t.Errorf("viewportStartY at bottom = %d, want 0", got)
	}
}

func TestMouseInViewport(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 100
	m.height = 40
	m.viewport.Width = 80
	m.viewport.Height = 20
	m.viewport.SetContent(strings.Repeat("a\n", 30))

	// Within viewport.
	if !m.mouseInViewport(5, 0) {
		t.Error("mouseInViewport(5, 0) should be true")
	}
	if !m.mouseInViewport(40, 19) {
		t.Error("mouseInViewport(40, 19) should be true")
	}

	// Outside (Y too high).
	if m.mouseInViewport(5, 25) {
		t.Error("mouseInViewport(5, 25) should be false")
	}
	// Outside (X too high).
	if m.mouseInViewport(85, 5) {
		t.Error("mouseInViewport(85, 5) should be false")
	}
	// Outside (negative).
	if m.mouseInViewport(-1, 5) {
		t.Error("mouseInViewport(-1, 5) should be false")
	}
}

func TestMouseToContentLine(t *testing.T) {
	m := newModel()
	m.ready = true
	m.viewport.Width = 80
	m.viewport.Height = 10
	// renderedContent must be non-empty for mouseToContentLine.
	m.renderedContent = strings.Repeat("line\n", 20)
	m.viewport.SetContent(m.renderedContent)

	// YOffset = 0 → mouse at Y=0 maps to content line 0.
	if got := m.mouseToContentLine(0); got != 0 {
		t.Errorf("mouseToContentLine(0) = %d, want 0", got)
	}
	// mouse at Y=5 maps to content line 5.
	if got := m.mouseToContentLine(5); got != 5 {
		t.Errorf("mouseToContentLine(5) = %d, want 5", got)
	}

	// Scroll down.
	m.viewport.YOffset = 10
	if got := m.mouseToContentLine(0); got != 10 {
		t.Errorf("mouseToContentLine(0) after scroll = %d, want 10", got)
	}

	// Out of bounds.
	m.renderedContent = ""
	if got := m.mouseToContentLine(5); got != -1 {
		t.Errorf("mouseToContentLine with empty content = %d, want -1", got)
	}
}

func TestMouseToContentCol(t *testing.T) {
	if got := mouseToContentCol(10); got != 10 {
		t.Errorf("mouseToContentCol(10) = %d, want 10", got)
	}
}

// --- Selection state tests ---

func TestClearSelection(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 3
	m.sel.endCol = 10

	m.clearSelection()
	if m.sel.active {
		t.Error("clearSelection should set active=false")
	}
	if m.sel.startLine != 0 || m.sel.endCol != 0 {
		t.Error("clearSelection should zero all fields")
	}
}

func TestStartSelection(t *testing.T) {
	m := newModel()
	m.startSelection(5, 10)

	if !m.sel.active {
		t.Error("startSelection should set active=true")
	}
	if m.sel.startLine != 5 || m.sel.startCol != 10 {
		t.Errorf("startSelection start = (%d,%d), want (5,10)", m.sel.startLine, m.sel.startCol)
	}
	if m.sel.endLine != 5 || m.sel.endCol != 10 {
		t.Errorf("startSelection end = (%d,%d), want (5,10)", m.sel.endLine, m.sel.endCol)
	}
}

func TestUpdateSelection_Forward(t *testing.T) {
	m := newModel()
	m.startSelection(0, 0)
	m.updateSelection(3, 10)

	// Anchor should stay at (0,0), end moves to (3,10).
	if m.sel.startLine != 0 || m.sel.startCol != 0 {
		t.Errorf("anchor should stay at (0,0), got (%d,%d)", m.sel.startLine, m.sel.startCol)
	}
	if m.sel.endLine != 3 || m.sel.endCol != 10 {
		t.Errorf("end should be (3,10), got (%d,%d)", m.sel.endLine, m.sel.endCol)
	}
}

func TestUpdateSelection_Backward(t *testing.T) {
	m := newModel()
	m.startSelection(5, 10)
	m.updateSelection(2, 3)

	// Anchor should swap: new start is (2,3), end becomes (5,10).
	if m.sel.startLine != 2 || m.sel.startCol != 3 {
		t.Errorf("start should be (2,3), got (%d,%d)", m.sel.startLine, m.sel.startCol)
	}
	if m.sel.endLine != 5 || m.sel.endCol != 10 {
		t.Errorf("end should be (5,10), got (%d,%d)", m.sel.endLine, m.sel.endCol)
	}
}

func TestUpdateSelection_SameLineBackward(t *testing.T) {
	m := newModel()
	m.startSelection(3, 10)
	m.updateSelection(3, 5)

	if m.sel.startLine != 3 || m.sel.startCol != 5 {
		t.Errorf("start should be (3,5), got (%d,%d)", m.sel.startLine, m.sel.startCol)
	}
	if m.sel.endLine != 3 || m.sel.endCol != 10 {
		t.Errorf("end should be (3,10), got (%d,%d)", m.sel.endLine, m.sel.endCol)
	}
}

func TestUpdateSelection_NotActive(t *testing.T) {
	m := newModel()
	// No active selection.
	m.updateSelection(3, 10)
	if m.sel.active {
		t.Error("updateSelection should not activate selection")
	}
}

// --- Rendering tests ---

func TestApplySelectionToLine_NoSelection(t *testing.T) {
	m := newModel()
	line := "hello world"
	result := m.applySelectionToLine(line, 0)
	if result != line {
		t.Errorf("expected unchanged line, got %q", result)
	}
}

func TestApplySelectionToLine_OutsideRange(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 5
	m.sel.startCol = 0
	m.sel.endLine = 5
	m.sel.endCol = 5

	// Line 3 is outside selection.
	result := m.applySelectionToLine("hello", 3)
	if result != "hello" {
		t.Errorf("line outside range should be unchanged, got %q", result)
	}
}

func TestApplySelectionToLine_FullLine(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 0
	m.sel.endLine = 0
	m.sel.endCol = 5

	result := m.applySelectionToLine("hello", 0)
	if !strings.Contains(result, "\x1b[7m") {
		t.Error("expected reverse video start escape")
	}
	if !strings.Contains(result, "\x1b[27m") {
		t.Error("expected reverse video end escape")
	}
	plain := ansi.Strip(result)
	if plain != "hello" {
		t.Errorf("plain text = %q, want 'hello'", plain)
	}
}

func TestApplySelectionToLine_Partial(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 2
	m.sel.endLine = 0
	m.sel.endCol = 4

	result := m.applySelectionToLine("abcdef", 0)
	if !strings.Contains(result, "\x1b[7m") {
		t.Error("expected reverse video")
	}
	plain := ansi.Strip(result)
	if plain != "abcdef" {
		t.Errorf("plain text = %q, want 'abcdef'", plain)
	}
}

func TestApplySelectionToLine_Suffix(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 0
	m.sel.endLine = 0
	m.sel.endCol = 3

	result := m.applySelectionToLine("abcdef", 0)
	if !strings.Contains(result, "\x1b[7m") {
		t.Error("expected reverse video at start")
	}
	if !strings.Contains(result, "\x1b[27m") {
		t.Error("expected reverse video end")
	}
	plain := ansi.Strip(result)
	if plain != "abcdef" {
		t.Errorf("plain text = %q, want 'abcdef'", plain)
	}
}

func TestApplySelectionToLine_EmptySelection(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 5
	m.sel.endLine = 0
	m.sel.endCol = 5 // same point = empty selection

	result := m.applySelectionToLine("hello", 0)
	if result != "hello" {
		t.Errorf("empty selection should not modify line, got %q", result)
	}
}

func TestApplySelectionToLine_PastEnd(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 0
	m.sel.endLine = 0
	m.sel.endCol = 100 // past end of line

	result := m.applySelectionToLine("hello", 0)
	if !strings.Contains(result, "\x1b[7m") {
		t.Error("expected reverse video")
	}
	plain := ansi.Strip(result)
	if plain != "hello" {
		t.Errorf("plain text = %q, want 'hello'", plain)
	}
}

func TestApplySelectionToLine_StartPastEnd(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 10
	m.sel.endLine = 0
	m.sel.endCol = 15

	result := m.applySelectionToLine("hello", 0)
	// Start is past the visual end of the line; line should be unchanged.
	if result != "hello" {
		t.Errorf("start past end: line should be unchanged, got %q", result)
	}
}

func TestApplySelectionToLine_MultiLineSelection_FirstLine(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 2
	m.sel.endLine = 2
	m.sel.endCol = 3

	// First line: selection from col 2 to end.
	result := m.applySelectionToLine("abcdef", 0)
	if !strings.Contains(result, "\x1b[7m") {
		t.Error("expected reverse video on first line")
	}
	if !strings.Contains(result, "\x1b[27m") {
		t.Error("expected reverse video end on first line")
	}
	plain := ansi.Strip(result)
	if plain != "abcdef" {
		t.Errorf("plain text = %q, want 'abcdef'", plain)
	}
}

func TestApplySelectionToLine_MultiLineSelection_MiddleLine(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 2
	m.sel.endLine = 2
	m.sel.endCol = 3

	// Middle line (line 1): full line selected.
	result := m.applySelectionToLine("abcdef", 1)
	if !strings.Contains(result, "\x1b[7m") {
		t.Error("expected reverse video on middle line")
	}
	if !strings.Contains(result, "\x1b[27m") {
		t.Error("expected reverse video end on middle line")
	}
	plain := ansi.Strip(result)
	if plain != "abcdef" {
		t.Errorf("plain text = %q, want 'abcdef'", plain)
	}
}

func TestApplySelectionToLine_MultiLineSelection_LastLine(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 2
	m.sel.endLine = 2
	m.sel.endCol = 3

	// Last line: selection from col 0 to col 3.
	result := m.applySelectionToLine("abcdef", 2)
	if !strings.Contains(result, "\x1b[7m") {
		t.Error("expected reverse video on last line")
	}
	if !strings.Contains(result, "\x1b[27m") {
		t.Error("expected reverse video end on last line")
	}
	plain := ansi.Strip(result)
	if plain != "abcdef" {
		t.Errorf("plain text = %q, want 'abcdef'", plain)
	}
}

func TestApplySelectionToLine_WithAnsiCodes(t *testing.T) {
	// Simulate glamour-markdown-rendered text (contains ANSI SGR codes).
	styledLine := "\x1b[1m\x1b[34mbold blue\x1b[0m text"

	m := newModel()
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 0
	m.sel.endLine = 0
	m.sel.endCol = ansi.StringWidth(styledLine)

	result := m.applySelectionToLine(styledLine, 0)
	if !strings.Contains(result, "\x1b[7m") {
		t.Error("expected reverse video on ANSI content")
	}
	// Visual content should be preserved.
	plain := ansi.Strip(result)
	if plain != ansi.Strip(styledLine) {
		t.Errorf("plain content mismatch: %q vs %q", plain, ansi.Strip(styledLine))
	}
}

func TestApplySelectionToLines(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 1
	m.sel.startCol = 2
	m.sel.endLine = 1
	m.sel.endCol = 5

	visible := []string{"line0", "line1", "line2"}
	result := m.applySelectionToLines(visible, 0)

	// Line 0 should be unchanged (outside selection).
	if result[0] != "line0" {
		t.Errorf("line 0 = %q, want 'line0'", result[0])
	}
	// Line 1 should have reverse codes (inside selection).
	if !strings.Contains(result[1], "\x1b[7m") {
		t.Error("line 1 should have reverse video")
	}
	// Line 2 should be unchanged (outside selection).
	if result[2] != "line2" {
		t.Errorf("line 2 = %q, want 'line2'", result[2])
	}
}

func TestApplySelectionToLines_NoSelection(t *testing.T) {
	m := newModel()
	visible := []string{"line0", "line1"}
	result := m.applySelectionToLines(visible, 0)
	if result[0] != "line0" || result[1] != "line1" {
		t.Error("lines should be unchanged when no selection")
	}
}

// --- Text extraction tests ---

func TestSelectedText_SingleLine(t *testing.T) {
	m := newModel()
	m.renderedContent = "hello world\n"
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 0
	m.sel.endLine = 0
	m.sel.endCol = 5

	text := m.selectedText()
	if text != "hello" {
		t.Errorf("selectedText = %q, want 'hello'", text)
	}
}

func TestSelectedText_MultiLine(t *testing.T) {
	m := newModel()
	m.renderedContent = "line one\nline two\nline three\n"
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 5
	m.sel.endLine = 2
	m.sel.endCol = 4

	text := m.selectedText()
	expected := "one\nline two\nline"
	if text != expected {
		t.Errorf("selectedText = %q, want %q", text, expected)
	}
}

func TestSelectedText_WithAnsiCodes(t *testing.T) {
	m := newModel()
	m.renderedContent = "\x1b[1mbold\x1b[0m text\nline\n"
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 0
	m.sel.endLine = 1
	m.sel.endCol = 4

	text := m.selectedText()
	// ANSI codes should be stripped.
	if strings.Contains(text, "\x1b") {
		t.Errorf("selectedText should not contain ANSI codes, got %q", text)
	}
	expected := "bold text\nline"
	if text != expected {
		t.Errorf("selectedText = %q, want %q", text, expected)
	}
}

func TestSelectedText_NotActive(t *testing.T) {
	m := newModel()
	m.renderedContent = "hello\n"
	text := m.selectedText()
	if text != "" {
		t.Errorf("selectedText should be empty when not active, got %q", text)
	}
}

func TestSelectedText_StartPastContent(t *testing.T) {
	m := newModel()
	m.renderedContent = "hello\n"
	m.sel.active = true
	m.sel.startLine = 10 // past content
	m.sel.startCol = 0
	m.sel.endLine = 15
	m.sel.endCol = 5

	text := m.selectedText()
	if text != "" {
		t.Errorf("selectedText should be empty when start past content, got %q", text)
	}
}

// --- Mouse handler tests ---

func TestHandleMouse_WheelUnaffected(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 100
	m.height = 40
	m.viewport.Width = 80
	m.viewport.Height = 20
	m.renderedContent = "line\n"
	m.viewport.SetContent(m.renderedContent)

	// Wheel up event should NOT trigger selection.
	msg := tea.MouseMsg{
		X:      10,
		Y:      5,
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	}
	m.handleMouse(msg)
	if m.sel.active {
		t.Error("wheel event should not activate selection")
	}
}

func TestHandleMouse_ClickOutsideViewport(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 100
	m.height = 40
	m.viewport.Width = 80
	m.viewport.Height = 20
	m.renderedContent = "line\n"
	m.viewport.SetContent(m.renderedContent)

	// First, create a selection.
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.endLine = 0

	// Click outside viewport (Y below viewport).
	msg := tea.MouseMsg{
		X:      10,
		Y:      25, // outside viewport (viewport height is 20)
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
	m.handleMouse(msg)
	if m.sel.active {
		t.Error("click outside viewport should clear selection")
	}
}

func TestHandleMouse_ClickInsideViewport(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 100
	m.height = 40
	m.viewport.Width = 80
	m.viewport.Height = 20
	m.renderedContent = "line one\nline two\n"
	m.viewport.SetContent(m.renderedContent)

	msg := tea.MouseMsg{
		X:      3,
		Y:      0, // inside viewport
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
	m.handleMouse(msg)
	if !m.sel.active {
		t.Error("left click inside viewport should start selection")
	}
	if m.sel.startLine != 0 || m.sel.startCol != 3 {
		t.Errorf("selection start = (%d,%d), want (0,3)", m.sel.startLine, m.sel.startCol)
	}
}

func TestHandleMouse_ClickWithEmptyContent(t *testing.T) {
	m := newModel()
	m.ready = true
	m.viewport.Width = 80
	m.viewport.Height = 20
	m.renderedContent = ""
	m.viewport.SetContent(m.renderedContent)

	msg := tea.MouseMsg{
		X:      3,
		Y:      0,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
	m.handleMouse(msg)
	if m.sel.active {
		t.Error("click with empty content should not start selection")
	}
}

func TestHandleMouse_DragMotion(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 100
	m.height = 40
	m.viewport.Width = 80
	m.viewport.Height = 20
	m.renderedContent = "line one\nline two\nline three\n"
	m.viewport.SetContent(m.renderedContent)

	// Start selection.
	m.handleMouse(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	// Drag to extend.
	m.handleMouse(tea.MouseMsg{X: 5, Y: 2, Action: tea.MouseActionMotion})
	// Selection should now span lines 0-2.
	if m.sel.startLine != 0 || m.sel.endLine != 2 {
		t.Errorf("drag selection: (%d,%d) to (%d,%d), want start=(0,0) end=(2,5)",
			m.sel.startLine, m.sel.startCol, m.sel.endLine, m.sel.endCol)
	}
	if m.sel.endCol != 5 {
		t.Errorf("drag endCol = %d, want 5", m.sel.endCol)
	}
}

func TestHandleMouse_ReleaseKeepsSelection(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 100
	m.height = 40
	m.viewport.Width = 80
	m.viewport.Height = 20
	m.renderedContent = "line\n"
	m.viewport.SetContent(m.renderedContent)

	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 0
	m.sel.endLine = 0
	m.sel.endCol = 3

	// Release event — selection should persist.
	m.handleMouse(tea.MouseMsg{X: 5, Y: 0, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
	if !m.sel.active {
		t.Error("selection should persist after release")
	}
}

func TestHandleMouse_NonLeftClickClears(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 100
	m.height = 40
	m.viewport.Width = 80
	m.viewport.Height = 20
	m.renderedContent = "line\n"
	m.viewport.SetContent(m.renderedContent)

	m.sel.active = true

	// Right click in viewport should clear selection.
	m.handleMouse(tea.MouseMsg{X: 5, Y: 0, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if m.sel.active {
		t.Error("right click should clear selection")
	}
}

func TestHandleMouse_PermDialogBlocks(t *testing.T) {
	m := newModel()
	m.ready = true
	m.width = 100
	m.height = 40
	m.viewport.Width = 80
	m.viewport.Height = 20
	m.renderedContent = "line\n"
	m.viewport.SetContent(m.renderedContent)
	m.permDialog = &permDialog{prompt: "test", choices: []string{"y", "n"}, active: 0}

	m.handleMouse(tea.MouseMsg{X: 3, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.sel.active {
		t.Error("mouse events should be blocked during permission dialog")
	}
}

// --- Auto-scroll tests ---

func TestAutoScrollDuringDrag_NearTop(t *testing.T) {
	m := newModel()
	m.ready = true
	m.viewport.Height = 20
	m.renderedContent = strings.Repeat("line\n", 50)
	m.viewport.SetContent(m.renderedContent)
	m.viewport.YOffset = 5 // scrolled away from top
	m.sel.active = true

	m.autoScrollDuringDrag(tea.MouseMsg{X: 10, Y: 1, Action: tea.MouseActionMotion})
	if m.viewport.YOffset != 4 {
		t.Errorf("should scroll up near top edge, YOffset = %d, want 4", m.viewport.YOffset)
	}
}

func TestAutoScrollDuringDrag_NearBottom(t *testing.T) {
	m := newModel()
	m.ready = true
	m.viewport.Height = 20
	m.renderedContent = strings.Repeat("line\n", 50)
	m.viewport.SetContent(m.renderedContent)
	m.sel.active = true

	m.autoScrollDuringDrag(tea.MouseMsg{X: 10, Y: 19, Action: tea.MouseActionMotion})
	if m.viewport.YOffset != 1 {
		t.Errorf("should scroll down near bottom edge, YOffset = %d, want 1", m.viewport.YOffset)
	}
}

func TestAutoScrollDuringDrag_NotActive(t *testing.T) {
	m := newModel()
	m.ready = true
	m.viewport.Height = 20
	m.renderedContent = strings.Repeat("line\n", 50)
	m.viewport.SetContent(m.renderedContent)

	m.autoScrollDuringDrag(tea.MouseMsg{X: 10, Y: 1, Action: tea.MouseActionMotion})
	if m.viewport.YOffset != 0 {
		t.Error("should not scroll when selection not active")
	}
}

// --- appendEntry clears selection ---

func TestAppendEntryClearsSelection(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 3
	m.sel.endLine = 5

	m.appendEntry(renderEntry{content: "new text", style: "text"})
	if m.sel.active {
		t.Error("appendEntry should clear selection")
	}
}

func TestAppendEntryClearsSelection_NonText(t *testing.T) {
	m := newModel()
	m.sel.active = true
	m.sel.startLine = 3

	m.appendEntry(renderEntry{content: "---", style: "separator"})
	if m.sel.active {
		t.Error("appendEntry (non-text) should clear selection")
	}
}

// --- renderViewportContent ---

func TestRenderViewportContent_Basic(t *testing.T) {
	m := newModel()
	m.viewport.Width = 20
	m.viewport.Height = 5
	m.renderedContent = "line0\nline1\nline2\nline3\nline4\n"
	m.viewport.SetContent(m.renderedContent)

	result := m.renderViewportContent()
	// Should show 5 lines within a 20x5 box.
	if !strings.Contains(result, "line0") {
		t.Error("viewport content should contain line0")
	}
	if !strings.Contains(result, "line4") {
		t.Error("viewport content should contain line4")
	}
}

func TestRenderViewportContent_WithSelection(t *testing.T) {
	m := newModel()
	m.viewport.Width = 20
	m.viewport.Height = 3
	m.renderedContent = "line0\nline1\nline2\n"
	m.viewport.SetContent(m.renderedContent)
	m.sel.active = true
	m.sel.startLine = 0
	m.sel.startCol = 0
	m.sel.endLine = 2
	m.sel.endCol = 5

	result := m.renderViewportContent()
	if !strings.Contains(result, "\x1b[7m") {
		t.Error("viewport content should have reverse video when selection active")
	}
}
