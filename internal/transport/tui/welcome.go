package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
// The banner shows a figlet ASCII art logo and a brief "type /help" hint.
// The content is both vertically and horizontally centered in the viewport.
func (m model) renderWelcome() string {
	vpWidth := m.viewportWidth()

	// ASCII art banner generated via figlet standard font "DOLPHIN-AI".
	dolphinArt := `  ____   ___  _     ____  _   _ ___ _   _         _    ___
 |  _ \ / _ \| |   |  _ \| | | |_ _| \ | |       / \  |_ _|
 | | | | | | | |   | |_) | |_| || ||  \| |_____ / _ \  | |
 | |_| | |_| | |___|  __/|  _  || || |\  |_____/ ___ \ | |
 |____/ \___/|_____|_|   |_| |_|___|_| \_|    /_/   \_\___|`
	artStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "26", Dark: "75"})

	// Version line: shown at the bottom in a subdued style.
	versionLine := fmt.Sprintf("🐬 %s %s", m.agentName, m.version)
	faintStyle := lipgloss.NewStyle().
		Foreground(adaptiveFaint).
		Italic(true)

	// Tips line: brief hint pointing to /help.
	tipLine := lipgloss.NewStyle().
		Foreground(adaptiveFaint).
		Render("Type /help to explore commands")

	// Find the widest line in the ASCII art for horizontal centering.
	artLines := strings.Split(dolphinArt, "\n")
	artWidth := 0
	for _, l := range artLines {
		if w := lipgloss.Width(l); w > artWidth {
			artWidth = w
		}
	}

	var lines []string
	for _, l := range artLines {
		lines = append(lines, artStyle.Render(l))
	}
	lines = append(lines, "")
	lines = append(lines, tipLine)
	lines = append(lines, "")
	lines = append(lines, faintStyle.Render(versionLine))

	body := strings.Join(lines, "\n")

	// Horizontal centering: pad every line so the block sits in the middle.
	if vpWidth > artWidth {
		leftPad := (vpWidth - artWidth) / 2
		padStr := strings.Repeat(" ", leftPad)
		padded := make([]string, len(lines))
		for i, l := range lines {
			padded[i] = padStr + l
		}
		body = strings.Join(padded, "\n")
	}

	// Fill the viewport height so the input area stays at the bottom.
	// Pad both top and bottom to vertically center the content.
	contentHeight := strings.Count(body, "\n") + 1
	if pad := m.viewport.Height - contentHeight; pad > 0 {
		top := pad / 2
		bottom := pad - top
		body = strings.Repeat("\n", top) + body + strings.Repeat("\n", bottom)
	}

	return body
}
