package tui

import "github.com/charmbracelet/lipgloss"

// Adaptive colors that respond to the terminal's background.
var (
	adaptiveStatusBg    = lipgloss.AdaptiveColor{Light: "248", Dark: "236"}
	adaptiveSeparator   = lipgloss.AdaptiveColor{Light: "244", Dark: "240"}
	adaptiveFaint       = lipgloss.AdaptiveColor{Light: "252", Dark: "237"}
	adaptiveThinking    = lipgloss.AdaptiveColor{Light: "245", Dark: "243"}
	adaptiveToolResult  = lipgloss.AdaptiveColor{Light: "28", Dark: "78"}
	adaptiveError       = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	adaptiveSystem      = lipgloss.AdaptiveColor{Light: "136", Dark: "220"}
	adaptiveUserLabel   = lipgloss.AdaptiveColor{Light: "28", Dark: "78"}
)
