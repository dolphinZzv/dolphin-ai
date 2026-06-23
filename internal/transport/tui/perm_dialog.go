package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type permDialog struct {
	prompt       string
	choices      []string
	active       int
	confirmIdx   int // -1 = no pending confirmation, >=0 = waiting for second press
	scrollOffset int // current scroll position for long prompts, 0 = top
}

type permResponseMsg struct {
	choice string
}

// wordWrap splits text into lines that fit within the given width, wrapping at
// word boundaries. Existing newlines are preserved as paragraph breaks.
func wordWrap(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		current := words[0]
		for _, word := range words[1:] {
			if lipgloss.Width(current)+1+lipgloss.Width(word) <= width {
				current += " " + word
			} else {
				lines = append(lines, current)
				current = word
			}
		}
		lines = append(lines, current)
	}
	return lines
}

func renderPermDialog(d permDialog, width int, maxHeight int) string {
	dialogWidth := min(width-4, 60)
	contentWidth := dialogWidth - 2 // account for Padding(0, 1)

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

	// Word-wrap the prompt to fit inside the dialog.
	promptLines := wordWrap(d.prompt, contentWidth)

	// Build the choices line.
	var choiceParts []string
	for i, c := range d.choices {
		if i == d.active {
			label := " " + c + " "
			if d.confirmIdx == i {
				label = " " + c + " -- press again "
			}
			choiceParts = append(choiceParts, activeStyle.Render(label))
		} else {
			choiceParts = append(choiceParts, choiceStyle.Render(" "+c+" "))
		}
	}
	choicesLine := strings.Join(choiceParts, "  ")

	// Full interior height (prompt + blank line + choices).
	fullHeight := len(promptLines) + 2

	if maxHeight <= 0 || fullHeight <= maxHeight {
		// No clipping needed — render everything.
		var b strings.Builder
		for _, line := range promptLines {
			b.WriteString(promptStyle.Render(line))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(choicesLine)
		return borderStyle.Render(b.String())
	}

	// Clipping needed: the dialog must fit within maxHeight interior lines.
	// Layout: [▲?] [prompt window] [▼?] [blank] [choices]
	scrollOff := d.scrollOffset
	if scrollOff < 0 {
		scrollOff = 0
	}

	// Reserve space for the fixed elements.
	availablePrompt := maxHeight - 2 // blank + choices
	if availablePrompt < 1 {
		availablePrompt = 1
	}

	hasAbove := scrollOff > 0
	hasBelow := false

	// Tentative end without indicators.
	end := scrollOff + availablePrompt
	if end > len(promptLines) {
		end = len(promptLines)
	}
	if end < len(promptLines) {
		hasBelow = true
	}

	// Deduct indicator lines from available prompt space.
	if hasAbove {
		availablePrompt--
	}
	if hasBelow {
		availablePrompt--
	}
	if availablePrompt < 1 {
		availablePrompt = 1
	}

	// Recompute end with adjusted available prompt.
	end = scrollOff + availablePrompt
	if end > len(promptLines) {
		end = len(promptLines)
	}
	// Re-compute hasBelow — it may have changed.
	hasBelow = end < len(promptLines)
	hasAbove = scrollOff > 0

	// Clamp scroll offset to valid range.
	maxScroll := len(promptLines) - availablePrompt
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollOff > maxScroll {
		scrollOff = maxScroll
	}
	end = scrollOff + availablePrompt
	if end > len(promptLines) {
		end = len(promptLines)
	}
	hasBelow = end < len(promptLines)
	hasAbove = scrollOff > 0

	indicatorStyle := lipgloss.NewStyle().
		Foreground(adaptivePermChoice).
		Align(lipgloss.Center).
		Width(contentWidth)

	var b strings.Builder
	if hasAbove {
		b.WriteString(indicatorStyle.Render("▲"))
		b.WriteString("\n")
	}
	for i := scrollOff; i < end; i++ {
		b.WriteString(promptStyle.Render(promptLines[i]))
		b.WriteString("\n")
	}
	if hasBelow {
		b.WriteString(indicatorStyle.Render("▼"))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(choicesLine)

	return borderStyle.Render(b.String())
}
