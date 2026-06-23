package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"dolphin/internal/i18n"
)

// completionsPageSize is the number of completion rows shown at once in the
// popup. Tab cycles within the full list; the view pages in blocks of this
// size around the active index.
const completionsPageSize = 8

// renderCompletions builds the slash-command autocomplete popup shown below
// the queue. Returns "" when there are no completions.
//
// Layout: a dashed separator, the visible page of completions (active row
// marked with ▸), an optional "+N more" indicator, and a footer hint. The
// popup is full-width; the caller stacks it into the main vertical layout.
func renderCompletions(completions []string, idx, width int) string {
	if len(completions) == 0 {
		return ""
	}

	compStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "252"}).
		Background(lipgloss.AdaptiveColor{Light: "189", Dark: "236"}).
		Padding(0, 1)
	compSep := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "244", Dark: "238"}).
		Render(strings.Repeat("─", width))
	header := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "241", Dark: "241"}).
		Render("Tab to cycle, type to filter")

	// Show a page of completions around the active index; highlight the
	// active one with ▸.
	start := idx / completionsPageSize * completionsPageSize
	end := start + completionsPageSize
	if end > len(completions) {
		end = len(completions)
	}
	var compLines []string
	for j := start; j < end; j++ {
		line := completions[j]
		if j == idx {
			line = "▸ " + line
		} else {
			line = "  " + line
		}
		compLines = append(compLines, compStyle.Render(line))
	}

	var elements []string
	elements = append(elements, compSep)
	if len(compLines) > 0 {
		elements = append(elements, lipgloss.JoinVertical(lipgloss.Left, compLines...))
	}
	if len(completions) > completionsPageSize {
		elements = append(elements, compStyle.Render(fmt.Sprintf(i18n.T("tui.completions_total"), len(completions))))
	}
	elements = append(elements, lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "136", Dark: "220"}).
		Render(header))
	return lipgloss.JoinVertical(lipgloss.Left, elements...)
}
