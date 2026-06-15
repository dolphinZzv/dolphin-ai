package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	StatusForeground    lipgloss.Color
	StatusBackground    lipgloss.Color
	SeparatorColor      lipgloss.Color
	SeparatorFaintColor lipgloss.Color
	UserTextBg          lipgloss.Color
	UserLabelColor      lipgloss.Color
	ThinkingColor       lipgloss.Color
	ToolCallColor       lipgloss.Color
	ToolResultColor     lipgloss.Color
	ErrorColor          lipgloss.Color
	SystemColor         lipgloss.Color
	CodeColor           lipgloss.Color
	TimelineBarColor    lipgloss.Color
	TextBold            lipgloss.Style
	TextItalic          lipgloss.Style
	MarkdownStyle       string
}

var ThemeDark = Theme{
	StatusForeground:    "#626262",
	StatusBackground:    "#d4d4d4",
	SeparatorColor:      "#626262",
	SeparatorFaintColor: "#4a4a4a",
	UserTextBg:          "#5c5c5c",
	UserLabelColor:      "#4ec94e",
	ThinkingColor:       "#626262",
	ToolCallColor:       "#87ceeb",
	ToolResultColor:     "#4ec94e",
	ErrorColor:          "#ff6b6b",
	SystemColor:         "#ffd700",
	CodeColor:           "#87ceeb",
	TimelineBarColor:    "#5a5a5a",
	MarkdownStyle:       "dark",
}

var ThemeLight = Theme{
	StatusForeground:    "#4a4a4a",
	StatusBackground:    "#e0e0e0",
	SeparatorColor:      "#888888",
	SeparatorFaintColor: "#cccccc",
	UserTextBg:          "#f0f0f0",
	UserLabelColor:      "#2d8a2d",
	ThinkingColor:       "#888888",
	ToolCallColor:       "#1e6f9f",
	ToolResultColor:     "#2d8a2d",
	ErrorColor:          "#cc3333",
	SystemColor:         "#b8860b",
	CodeColor:           "#1e6f9f",
	TimelineBarColor:    "#b0b0b0",
	MarkdownStyle:       "light",
}

func ThemeFromString(name string) Theme {
	switch strings.ToLower(name) {
	case "light":
		return ThemeLight
	case "auto":
		if isLightTerminal() {
			return ThemeLight
		}
		return ThemeDark
	default:
		return ThemeDark
	}
}

func isLightTerminal() bool {
	// Check common env vars that indicate light terminal background.
	if v := os.Getenv("COLORFGRD"); v != "" {
		return true
	}
	if bg := os.Getenv("TERM_BG"); strings.ToLower(bg) == "light" {
		return true
	}
	return false
}

// TextBold and TextItalic are computed once since they depend only on the
// base style, not on theme colors.
func init() {
	ThemeDark.TextBold = lipgloss.NewStyle().Bold(true)
	ThemeDark.TextItalic = lipgloss.NewStyle().Italic(true)
	ThemeLight.TextBold = lipgloss.NewStyle().Bold(true)
	ThemeLight.TextItalic = lipgloss.NewStyle().Italic(true)
}
