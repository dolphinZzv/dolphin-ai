package tui

import (
	"regexp"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

var (
	styleThinking       lipgloss.Style
	styleToolCall       lipgloss.Style
	styleToolResult     lipgloss.Style
	styleToolError      lipgloss.Style
	styleSystem         lipgloss.Style
	styleSeparator      lipgloss.Style
	styleSeparatorFaint lipgloss.Style
	styleUserText       lipgloss.Style
	styleQueueActive    lipgloss.Style
	styleQueueWait      lipgloss.Style
	styleQueueTime      lipgloss.Style
	styleAttachment     lipgloss.Style

	mdRenderer   *glamour.TermRenderer
	mdRendererMu sync.Mutex
)

// viewportWidth is set by the model on each resize so that styled
// entries (e.g. user_text with a background) can render full-width.
var viewportWidth int

func init() {
	// Build the markdown renderer once. Style variables are populated by
	// rebuildStyles(), which is called from applyTheme() at startup.
	rebuildStyles()
	r, err := glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
	)
	if err == nil {
		mdRenderer = r
	}
}

// rebuildStyles (re)builds package style variables from the current theme
// colors. Called once at init and again whenever applyTheme runs.
func rebuildStyles() {
	styleThinking = lipgloss.NewStyle().
		Foreground(adaptiveThinking)
	if thinkBG != nil {
		styleThinking = styleThinking.Background(thinkBG)
	}

	styleToolCall = lipgloss.NewStyle().
		Foreground(adaptiveToolUse)
	if toolUseBG != nil {
		styleToolCall = styleToolCall.Background(toolUseBG)
	}

	styleToolResult = lipgloss.NewStyle().
		Foreground(adaptiveToolResult)
	if toolResBG != nil {
		styleToolResult = styleToolResult.Background(toolResBG)
	}

	styleToolError = lipgloss.NewStyle().
		Foreground(adaptiveError)

	styleSystem = lipgloss.NewStyle().
		Foreground(adaptiveSystem)

	styleSeparator = lipgloss.NewStyle().
		Foreground(adaptiveSeparator)

	styleSeparatorFaint = lipgloss.NewStyle().
		Foreground(adaptiveFaint)

	styleUserText = lipgloss.NewStyle().
		Foreground(userMsgFG)
	if userMsgBG != nil {
		styleUserText = styleUserText.Background(userMsgBG)
	}

	styleQueueActive = lipgloss.NewStyle().
		Foreground(lipgloss.Color("40"))
	styleQueueWait = lipgloss.NewStyle().
		Foreground(lipgloss.Color("178"))
	styleQueueTime = lipgloss.NewStyle().
		Foreground(adaptiveFaint)

	styleAttachment = lipgloss.NewStyle().
		Foreground(lipgloss.Color("215"))
}

func renderStyled(e renderEntry) string {
	switch e.style {
	case "separator":
		return styleSeparator.Render(e.content)
	case "separator_faint":
		return styleSeparatorFaint.Render(e.content)
	case "thinking":
		return styleThinking.Render("\n" + e.content)
	case "tool_call":
		return styleToolCall.Render("\n" + e.content)
	case "tool_result":
		return styleToolResult.Render("\n" + padLines(e.content, 3))
	case "tool_error":
		return styleToolError.Render("\n" + padLines(e.content, 3))
	case "system":
		return styleSystem.Render(e.content)
	case "user_text":
		// Prefix each line with "> " to mark the user's message; blank
		// lines get just the marker gutter so the block stays aligned.
		lines := strings.Split(e.content, "\n")
		for i, line := range lines {
			if line == "" {
				lines[i] = "> "
			} else {
				lines[i] = "> " + line
			}
		}
		content := strings.Join(lines, "\n")
		// Pad each line to viewportWidth so the background fills
		// the entire line, not just the text area.
		if viewportWidth > 0 {
			lines = strings.Split(content, "\n")
			for i, line := range lines {
				if w := lipgloss.Width(line); w < viewportWidth {
					lines[i] = line + strings.Repeat(" ", viewportWidth-w)
				}
			}
			content = strings.Join(lines, "\n")
		}
		// Leading \n is padding between entries — no background.
		return "\n" + styleUserText.
			Background(adaptiveUserTextBg).
			Render(content)
	default:
		return e.content
	}
}

func renderMarkdown(s string) string {
	if s == "" {
		return s
	}
	mdRendererMu.Lock()
	r := mdRenderer
	mdRendererMu.Unlock()
	if r == nil {
		return s
	}
	out, err := r.Render(s)
	if err != nil {
		return s
	}
	lines := strings.Split(out, "\n")
	var cleaned []string
	prevBlank := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " ")
		if ansiRe.ReplaceAllString(trimmed, "") == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		cleaned = append(cleaned, trimmed)
	}
	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	for len(cleaned) > 0 && cleaned[0] == "" {
		cleaned = cleaned[1:]
	}
	joined := padLines(strings.Join(cleaned, "\n"), 1)
	// Apply the theme response background (if set) so the assistant's
	// reply block sits on a colored field. Foreground is left to glamour.
	if respBG != nil {
		bgStyle := lipgloss.NewStyle().Background(respBG)
		lines := strings.Split(joined, "\n")
		for i, line := range lines {
			if line != "" {
				lines[i] = bgStyle.Render(line)
			}
		}
		joined = strings.Join(lines, "\n")
	}
	return joined
}

func renderSeparator(name string, width int) string {
	if name == "" {
		return ""
	}
	label := ""
	dashCount := (width - len(label)) / 2
	if dashCount < 2 {
		dashCount = 2
	}
	dashes := strings.Repeat("-", dashCount)
	return dashes + label + dashes
}

func padLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	pad := strings.Repeat(" ", n)
	for i, line := range lines {
		if line != "" {
			lines[i] = pad + line
		}
	}
	return strings.Join(lines, "\n")
}
