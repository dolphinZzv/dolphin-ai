package tui

import (
	"regexp"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

var (
	styleThinking       lipgloss.Style
	styleToolCall       lipgloss.Style
	styleToolResult     lipgloss.Style
	styleSystem         lipgloss.Style
	styleSeparator      lipgloss.Style
	styleSeparatorFaint lipgloss.Style
	styleUserText       lipgloss.Style

	mdRenderer   *glamour.TermRenderer
	mdRendererMu sync.Mutex
	mdStyle      string
)

func ApplyTheme(t Theme) {
	styleThinking = lipgloss.NewStyle().
		Foreground(t.ThinkingColor).
		Italic(true)

	styleToolCall = lipgloss.NewStyle().
		Foreground(t.ToolCallColor)

	styleToolResult = lipgloss.NewStyle().
		Foreground(t.ToolResultColor)

	styleSystem = lipgloss.NewStyle().
		Foreground(t.SystemColor)

	styleSeparator = lipgloss.NewStyle().
		Foreground(t.SeparatorColor)

	styleSeparatorFaint = lipgloss.NewStyle().
		Foreground(t.SeparatorFaintColor)

	styleUserText = lipgloss.NewStyle().
		Foreground(t.UserLabelColor).
		Background(t.UserTextBg)

	setMarkdownStyle(t.MarkdownStyle)
}

func markdownRenderer() *glamour.TermRenderer {
	mdRendererMu.Lock()
	defer mdRendererMu.Unlock()

	style := "dark"
	if mdStyle != "" {
		style = mdStyle
	}

	if mdRenderer != nil {
		return mdRenderer
	}

	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style))
	if err != nil {
		return nil
	}
	mdRenderer = r
	return mdRenderer
}

func setMarkdownStyle(style string) {
	mdRendererMu.Lock()
	defer mdRendererMu.Unlock()

	if mdStyle != style {
		mdStyle = style
		mdRenderer = nil
	}
}

func renderStyled(e renderEntry) string {
	switch e.style {
	case "separator":
		return styleSeparator.Render(e.content)
	case "separator_faint":
		return styleSeparatorFaint.Render(e.content)
	case "thinking":
		return styleThinking.Render(e.content)
	case "tool_call":
		return styleToolCall.Render(e.content)
	case "tool_result":
		return styleToolResult.Render(padLines(e.content, 3))
	case "system":
		return styleSystem.Render(e.content)
	case "user_text":
		return styleUserText.Render(padLines(e.content, 3))
	default:
		return e.content
	}
}

func renderMarkdown(s string) string {
	if s == "" {
		return s
	}
	r := markdownRenderer()
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
	// Trim trailing blank lines.
	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	// Trim leading blank lines.
	for len(cleaned) > 0 && cleaned[0] == "" {
		cleaned = cleaned[1:]
	}
	return padLines(strings.Join(cleaned, "\n"), 1)
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
