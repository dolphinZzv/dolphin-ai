package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type permDialog struct {
	prompt     string
	choices    []string
	active     int
	confirmIdx int // -1 = no pending confirmation, >=0 = waiting for second press
}

type permResponseMsg struct {
	choice string
}

func renderPermDialog(d permDialog, width int) string {
	dialogWidth := min(width-4, 60)
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(adaptivePermBorder).
		Width(dialogWidth).
		Padding(0, 1)

	promptStyle := lipgloss.NewStyle().
		Foreground(adaptivePermPrompt).
		Bold(true)

	choiceStyle := lipgloss.NewStyle().
		Foreground(adaptivePermChoice)

	activeStyle := lipgloss.NewStyle().
		Foreground(adaptivePermActiveFg).
		Background(adaptivePermActiveBg).
		Bold(true)

	var b strings.Builder
	b.WriteString(promptStyle.Render(d.prompt))
	b.WriteString("\n\n")

	for i, c := range d.choices {
		if i == d.active {
			label := " " + c + " "
			if d.confirmIdx == i {
				label = " " + c + " -- press again "
			}
			b.WriteString(activeStyle.Render(label))
		} else {
			b.WriteString(choiceStyle.Render(" " + c + " "))
		}
		if i < len(d.choices)-1 {
			b.WriteString("  ")
		}
	}

	return borderStyle.Render(b.String())
}
