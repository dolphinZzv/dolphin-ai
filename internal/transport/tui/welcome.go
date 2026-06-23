package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"dolphin/internal/i18n"
)

// showingWelcome reports whether the empty-state welcome banner should
// replace the viewport content: no messages have been rendered yet and no
// turn is in flight. Selection mode keeps the real (empty) content so the
// selection bookkeeping stays consistent.
func (m model) showingWelcome() bool {
	if m.sel.active {
		return false
	}
	if len(m.messages) > 0 {
		return false
	}
	if m.msgStatus == "pending" {
		return false
	}
	return true
}

// renderWelcome builds the empty-state banner shown in the message viewport
// before the first turn begins. It is a pure overlay — it never enters the
// message buffer, so it does not pollute incremental rendering, is never
// trimmed, and disappears the moment real content arrives or a turn starts.
func (m model) renderWelcome() string {
	width := m.viewportWidth()
	if width < 20 {
		width = 20
	}

	// Title: agent name + version, styled to stand out at the top.
	title := fmt.Sprintf("🐬 %s %s", m.agentName, m.version)
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "26", Dark: "75"}).
		Bold(true)
	taglineStyle := lipgloss.NewStyle().
		Foreground(adaptiveFaint).
		Italic(true)
	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "220"}).
		Bold(true)
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "241", Dark: "247"})
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "252"})

	// Key column width: the longest keybinding. Keeps descriptions aligned.
	// "Ctrl+↑↓" uses a double-width arrow, so pad generously.
	const keyCol = 14

	hintRow := func(key, desc string) string {
		return keyStyle.Width(keyCol).Render(key) + "  " + descStyle.Render(desc)
	}

	sectionHeader := func(title string) string {
		// Underline the header with faint dashes to fill the column.
		dashes := width - len(title) - 2
		if dashes < 2 {
			dashes = 2
		}
		return sectionStyle.Render(title) + " " + lipgloss.NewStyle().
			Foreground(adaptiveFaint).
			Render(strings.Repeat("─", dashes))
	}

	var lines []string
	lines = append(lines, titleStyle.Render(title))
	lines = append(lines, taglineStyle.Render(i18n.T("tui.welcome_tagline")))
	lines = append(lines, "")

	lines = append(lines, sectionHeader(i18n.T("tui.welcome_section_input")))
	lines = append(lines, hintRow("Enter", i18n.T("tui.welcome_send")))
	lines = append(lines, hintRow("Alt+Enter", i18n.T("tui.welcome_newline")))
	lines = append(lines, hintRow("Ctrl+P", i18n.T("tui.welcome_priority")))
	lines = append(lines, hintRow("Ctrl+G", i18n.T("tui.welcome_jump")))
	lines = append(lines, hintRow("↑ / ↓", i18n.T("tui.welcome_scroll")))
	lines = append(lines, hintRow("Ctrl+↑↓", i18n.T("tui.welcome_history")))
	lines = append(lines, hintRow("Tab", i18n.T("tui.welcome_complete")))
	lines = append(lines, hintRow("ESC", i18n.T("tui.welcome_pause")))
	lines = append(lines, "")

	lines = append(lines, sectionHeader(i18n.T("tui.welcome_section_commands")))
	lines = append(lines, hintRow("/tools", i18n.T("tui.welcome_tools")))
	lines = append(lines, hintRow("/thinking", i18n.T("tui.welcome_thinking")))
	lines = append(lines, hintRow("/windows", i18n.T("tui.welcome_sidepanel")))
	lines = append(lines, hintRow("/help", i18n.T("tui.welcome_help")))
	lines = append(lines, hintRow("/exit", i18n.T("tui.welcome_exit")))

	body := strings.Join(lines, "\n")
	// Indent the whole banner so it reads as a panel rather than hugging the
	// left edge of the viewport.
	return lipgloss.NewStyle().PaddingLeft(1).Render(body)
}
