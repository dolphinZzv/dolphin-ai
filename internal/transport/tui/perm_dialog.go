package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type permDialog struct {
	prompt  string
	choices []string
	active  int
}

type permResponseMsg struct {
	choice string
}

func renderPermDialog(d permDialog, width int) string {
	dialogWidth := min(width-4, 60)
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#ffd700")).
		Width(dialogWidth).
		Padding(0, 1)

	promptStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true)

	choiceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#87ceeb"))

	activeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#ffd700")).
		Bold(true)

	var b strings.Builder
	b.WriteString(promptStyle.Render(d.prompt))
	b.WriteString("\n\n")

	for i, c := range d.choices {
		if i == d.active {
			b.WriteString(activeStyle.Render(" " + c + " "))
		} else {
			b.WriteString(choiceStyle.Render(" " + c + " "))
		}
		if i < len(d.choices)-1 {
			b.WriteString("  ")
		}
	}

	return borderStyle.Render(b.String())
}
