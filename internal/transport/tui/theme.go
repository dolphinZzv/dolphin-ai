package tui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// Adaptive colors that respond to the terminal's background.
var (
	adaptiveStatusBg     = lipgloss.Color("236")
	adaptiveSeparator    = lipgloss.Color("240")
	adaptiveFaint        = lipgloss.Color("237")
	adaptiveThinking     = lipgloss.Color("243")
	adaptiveToolResult   = lipgloss.Color("78")
	adaptiveError        = lipgloss.Color("203")
	adaptiveSystem       = lipgloss.Color("220")
	adaptiveUserLabel    = lipgloss.Color("78")
	adaptivePermBorder   = lipgloss.Color("220")
	adaptivePermPrompt   = lipgloss.Color("255")
	adaptivePermChoice   = lipgloss.Color("87")
	adaptivePermActiveFg = lipgloss.Color("0")
	adaptivePermActiveBg = lipgloss.Color("220")

	// Truly adaptive colors — resolved at init time based on terminal background.
	adaptiveInputBg    color.Color // input area background
	adaptiveInputFg    color.Color // input area text
	adaptiveInputPh    color.Color // input area placeholder
	adaptiveCursor     color.Color // input cursor
	adaptiveUserTextBg color.Color // user message background
	adaptiveToolIconError color.Color // ⏺ on tool error
	adaptiveToolIconOk    color.Color // ⏺ on tool success
)

func init() {
	adapt := lipgloss.LightDark(lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
	adaptiveInputBg = adapt(lipgloss.Color("254"), lipgloss.Color("236"))
	adaptiveInputFg = adapt(lipgloss.Color("0"), lipgloss.Color("255"))
	adaptiveInputPh = adapt(lipgloss.Color("240"), lipgloss.Color("249"))
	adaptiveCursor = adapt(lipgloss.Color("238"), lipgloss.Color("253"))
	adaptiveUserTextBg = adapt(lipgloss.Color("255"), lipgloss.Color("235"))
	adaptiveToolIconError = lipgloss.Color("196")
	adaptiveToolIconOk = lipgloss.Color("28")
}
