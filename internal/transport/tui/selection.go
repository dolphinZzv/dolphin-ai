package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/atotto/clipboard"
)

// viewportStartY returns the terminal row (0-indexed) where the viewport's
// first visible line is rendered. Row 0 is the optional floating bar
// (current-message or scroll indicator); the viewport starts at row 1 when
// scrolled up, otherwise row 0.
func (m model) viewportStartY() int {
	if m.currentMsg != "" && !m.viewport.AtBottom() {
		return 1
	}
	return 0
}

// mouseInViewport reports whether terminal coordinates (x, y) fall within
// the rendered viewport area.
func (m model) mouseInViewport(x, y int) bool {
	if y < m.viewportStartY() {
		return false
	}
	if y >= m.viewportStartY()+m.viewport.Height {
		return false
	}
	if x < 0 || x >= m.viewportWidth() {
		return false
	}
	return true
}

// mouseToContentLine maps a terminal mouse Y coordinate to a 0-indexed line
// number in renderedContent. Returns -1 if out of bounds.
func (m model) mouseToContentLine(mouseY int) int {
	relY := mouseY - m.viewportStartY()
	line := m.viewport.YOffset + relY
	contentLines := strings.Count(m.renderedContent, "\n") + 1
	if m.renderedContent == "" {
		return -1
	}
	if line < 0 || line >= contentLines {
		return -1
	}
	return line
}

// mouseToContentCol maps a terminal mouse X coordinate to a visual column
// within the content line. The viewport does not use horizontal scrolling
// (xOffset is always 0), so the mapping is 1:1.
func mouseToContentCol(mouseX int) int {
	return mouseX
}

// Selection state management -------------------------------------------------

// clearSelection resets all selection state.
func (m *model) clearSelection() {
	m.sel.active = false
	m.sel.startLine = 0
	m.sel.startCol = 0
	m.sel.endLine = 0
	m.sel.endCol = 0
}

// startSelection begins a new selection at the given content position.
func (m *model) startSelection(line, col int) {
	m.sel.active = true
	m.sel.startLine = line
	m.sel.startCol = col
	m.sel.endLine = line
	m.sel.endCol = col
}

// updateSelection extends the selection to the given content position.
// Normalises so startLine/startCol is always before endLine/endCol.
func (m *model) updateSelection(line, col int) {
	if !m.sel.active {
		return
	}
	if line < m.sel.startLine || (line == m.sel.startLine && col < m.sel.startCol) {
		// New position is before the anchor: it becomes the new start.
		m.sel.endLine = m.sel.startLine
		m.sel.endCol = m.sel.startCol
		m.sel.startLine = line
		m.sel.startCol = col
	} else {
		m.sel.endLine = line
		m.sel.endCol = col
	}
}

// Mouse event handling -------------------------------------------------------

// handleMouse processes mouse events for text selection. Called from
// model.Update() before mouse events reach the viewport/textarea.
// Returns an optional tea.Cmd for clipboard copy on mouse release.
func (m *model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	// Never initiate or modify selection while the permission dialog is open.
	if m.permDialog != nil {
		return nil
	}

	switch {
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		if !m.mouseInViewport(msg.X, msg.Y) {
			m.clearSelection()
			return nil
		}
		line := m.mouseToContentLine(msg.Y)
		if line < 0 {
			m.clearSelection()
			return nil
		}
		col := mouseToContentCol(msg.X)
		m.startSelection(line, col)

	case msg.Action == tea.MouseActionMotion && m.sel.active:
		line := m.mouseToContentLine(msg.Y)
		if line < 0 {
			return nil
		}
		col := mouseToContentCol(msg.X)
		m.updateSelection(line, col)

	case msg.Action == tea.MouseActionRelease:
		// Auto-copy to system clipboard on mouse release so Cmd+V works.
		// Match on Action alone (not Button): in SGR mouse mode — the
		// preferred mode bubbletea negotiates with iTerm2/Terminal.app/
		// Alacritty/kitty — a left-button release arrives with
		// Button == MouseButtonLeft, whereas X10 fallback reports
		// Button == MouseButtonNone. Requiring MouseButtonNone (the old
		// check) meant copy never fired under SGR, so dragging to select
		// never reached the clipboard.
		return m.copySelection()

	case msg.Action == tea.MouseActionPress && msg.Button != tea.MouseButtonLeft:
		// Non-left click clears selection.
		if m.mouseInViewport(msg.X, msg.Y) {
			m.clearSelection()
		}
	}
	return nil
}

// autoScrollDuringDrag scrolls the viewport by one line when the mouse is
// within one row of the top or bottom edge during a drag selection.
func (m *model) autoScrollDuringDrag(msg tea.MouseMsg) {
	if !m.sel.active || msg.Action != tea.MouseActionMotion {
		return
	}
	relY := msg.Y - m.viewportStartY()
	if relY <= 1 && !m.viewport.AtTop() {
		m.viewport.ScrollUp(1)
	} else if relY >= m.viewport.Height-2 && !m.viewport.AtBottom() {
		m.viewport.ScrollDown(1)
	}
}

// Selection rendering --------------------------------------------------------

// renderViewportContent returns the visible viewport content with selection
// highlighting applied when a selection is active. It replicates the
// viewport's own View() logic so that highlighting is injected.
func (m model) renderViewportContent() string {
	lines := strings.Split(m.renderedContent, "\n")
	h := m.viewport.Height
	w := m.viewport.Width

	top := m.viewport.YOffset
	if top < 0 {
		top = 0
	}
	if top >= len(lines) {
		top = max(0, len(lines)-1)
	}
	bottom := top + h
	if bottom > len(lines) {
		bottom = len(lines)
	}

	visible := lines[top:bottom]

	if m.sel.active {
		visible = m.applySelectionToLines(visible, top)
	}

	return lipgloss.NewStyle().
		Width(w).
		Height(h).
		MaxHeight(h).
		MaxWidth(w).
		Render(strings.Join(visible, "\n"))
}

// applySelectionToLines applies reverse-video highlighting to visible
// viewport lines that intersect the current selection.
func (m model) applySelectionToLines(visible []string, firstLineIdx int) []string {
	if !m.sel.active {
		return visible
	}
	result := make([]string, len(visible))
	for i, line := range visible {
		result[i] = m.applySelectionToLine(line, firstLineIdx+i)
	}
	return result
}

// applySelectionToLine applies reverse-video highlighting to a single line
// that intersects the selection. Uses ansi.Cut for ANSI-safe visual-column
// slicing so ANSI escape codes are preserved and wide characters handled.
func (m model) applySelectionToLine(line string, lineIdx int) string {
	totalWidth := ansi.StringWidth(line)

	if lineIdx < m.sel.startLine || lineIdx > m.sel.endLine {
		return line
	}

	selStart := 0
	selEnd := totalWidth

	if lineIdx == m.sel.startLine {
		selStart = m.sel.startCol
	}
	if lineIdx == m.sel.endLine {
		selEnd = m.sel.endCol
		if selEnd > totalWidth {
			selEnd = totalWidth
		}
	}

	if selStart >= totalWidth {
		return line
	}
	if selStart >= selEnd {
		return line
	}

	var b strings.Builder
	if selStart > 0 {
		b.WriteString(ansi.Cut(line, 0, selStart))
	}
	b.WriteString("\x1b[7m")
	b.WriteString(ansi.Cut(line, selStart, selEnd))
	b.WriteString("\x1b[27m")
	if selEnd < totalWidth {
		b.WriteString(ansi.Cut(line, selEnd, totalWidth))
	}
	return b.String()
}

// Copy to clipboard ----------------------------------------------------------

// copySelection copies the selected text (ANSI stripped) to the system
// clipboard. Returns a tea.Cmd so the UI does not block.
func (m model) copySelection() tea.Cmd {
	if !m.sel.active {
		return nil
	}
	text := m.selectedText()
	if text == "" {
		return nil
	}
	return func() tea.Msg {
		_ = clipboard.WriteAll(text)
		return nil
	}
}

// selectedText extracts the plain text (ANSI stripped) of the current
// selection from renderedContent.
func (m model) selectedText() string {
	if !m.sel.active {
		return ""
	}
	lines := strings.Split(m.renderedContent, "\n")
	if m.sel.startLine >= len(lines) {
		return ""
	}

	endLine := m.sel.endLine
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	var parts []string
	for i := m.sel.startLine; i <= endLine; i++ {
		line := lines[i]
		startCol := 0
		endCol := ansi.StringWidth(line)

		if i == m.sel.startLine {
			startCol = m.sel.startCol
		}
		if i == m.sel.endLine {
			endCol = m.sel.endCol
			if endCol > ansi.StringWidth(line) {
				endCol = ansi.StringWidth(line)
			}
		}
		if startCol < endCol {
			part := ansi.Cut(line, startCol, endCol)
			parts = append(parts, ansi.Strip(part))
		}
	}
	return strings.Join(parts, "\n")
}
