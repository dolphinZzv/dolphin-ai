package tui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
)

// Themeable color variables. They are populated by applyTheme() at startup
// (rather than init()) so they can be customized from config.yaml. Package
// code references these directly; applyTheme reassigns them when the theme
// is loaded.
var (
	adaptiveStatusBg     color.Color = lipgloss.Color("236")
	adaptiveSeparator    color.Color = lipgloss.Color("240")
	adaptiveFaint        color.Color = lipgloss.Color("237")
	adaptiveThinking     color.Color = lipgloss.Color("243")
	adaptiveToolResult   color.Color = lipgloss.Color("78")
	adaptiveError        color.Color = lipgloss.Color("203")
	adaptiveSystem       color.Color = lipgloss.Color("220")
	adaptivePermBorder   color.Color = lipgloss.Color("220")
	adaptivePermPrompt   color.Color = lipgloss.Color("255")
	adaptivePermChoice   color.Color = lipgloss.Color("87")
	adaptivePermActiveFg color.Color = lipgloss.Color("0")
	adaptivePermActiveBg color.Color = lipgloss.Color("220")

	adaptiveToolUse color.Color = lipgloss.Color("117")

	// Truly adaptive colors — resolved at applyTheme time based on terminal background.
	adaptiveInputBg       color.Color // input area background
	adaptiveInputFg       color.Color // input area text
	adaptiveInputPh       color.Color // input area placeholder
	adaptiveCursor        color.Color // input cursor
	adaptiveUserTextBg    color.Color // user message background
	adaptiveToolIconError color.Color // ⏺ on tool error
	adaptiveToolIconOk    color.Color // ⏺ on tool success

	// Themeable message colors.
	userMsgFG color.Color = lipgloss.Color("255")
	userMsgBG color.Color // user message background (resolved)
	toolUseBG color.Color // tool_use background (resolved, may be nil)
	toolResBG color.Color // tool_result background (resolved, may be nil)
	thinkBG   color.Color // thinking background (resolved, may be nil)
	respBG    color.Color // response background (resolved, may be nil)

	// viewportBG is the background color of the entire TUI. nil = transparent
	// (follows the terminal's current background).
	viewportBG color.Color
)

// themePalette is one light or dark color set for a theme.
type themePalette struct {
	UserMessageFG string
	UserMessageBG string
	ToolUseFG     string
	ToolUseBG     string
	ToolResultFG  string
	ToolResultBG  string
	ThinkingFG    string
	ThinkingBG    string
	ResponseFG    string
	ResponseBG    string
	Background    string // "default" or "" → transparent
}

// themeSet holds both palettes for a named theme.
type themeSet struct {
	Light themePalette
	Dark  themePalette
}

// defaultTheme returns the built-in default theme. Users may override any
// field; this is the fallback for any field they leave empty.
func defaultTheme() themeSet {
	return themeSet{
		Light: themePalette{
			UserMessageFG: "#1a1a1a",
			UserMessageBG: "#ffffff",
			ToolUseFG:     "blue",
			ToolResultFG:  "28",
			ThinkingFG:    "243",
			ResponseFG:    "#1a1a1a",
			Background:    "default",
		},
		Dark: themePalette{
			UserMessageFG: "78",
			UserMessageBG: "235",
			ToolUseFG:     "117",
			ToolResultFG:  "78",
			ThinkingFG:    "243",
			ResponseFG:    "255",
			Background:    "default",
		},
	}
}

// applyTheme loads the named theme from config (falling back to "default")
// and selects its light or dark palette based on the terminal background.
// It populates the package-level color variables and rebuilds derived styles.
func applyTheme(active string, custom map[string]any) {
	def := defaultTheme()

	set := def
	if active == "" {
		active = "default"
	}
	if ts, ok := extractThemeSet(custom, active, def); ok {
		set = ts
	}

	dark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	pal := set.Light
	if dark {
		pal = set.Dark
	}

	adaptiveThinking = resolveColor(pal.ThinkingFG, def.darkFG("ThinkingFG"))
	adaptiveToolResult = resolveColor(pal.ToolResultFG, def.darkFG("ToolResultFG"))
	adaptiveSystem = lipgloss.Color("220") // not user-themeable; keep default
	adaptiveToolUse = resolveColor(pal.ToolUseFG, lipgloss.Color("117"))
	userMsgFG = resolveColor(pal.UserMessageFG, lipgloss.Color("78"))

	// Backgrounds: empty/"default" → nil (transparent).
	userMsgBG = resolveBG(pal.UserMessageBG)
	toolUseBG = resolveBG(pal.ToolUseBG)
	toolResBG = resolveBG(pal.ToolResultBG)
	thinkBG = resolveBG(pal.ThinkingBG)
	respBG = resolveBG(pal.ResponseBG)
	viewportBG = resolveBG(pal.Background)

	// Terminal-background-adaptive UI colors (input area, cursor, icons).
	// These are not per-message themeable; keep the existing adaptive logic.
	adapt := lipgloss.LightDark(dark)
	adaptiveInputBg = adapt(lipgloss.Color("254"), lipgloss.Color("236"))
	adaptiveInputFg = adapt(lipgloss.Color("0"), lipgloss.Color("255"))
	adaptiveInputPh = adapt(lipgloss.Color("240"), lipgloss.Color("249"))
	adaptiveCursor = adapt(lipgloss.Color("238"), lipgloss.Color("253"))
	adaptiveUserTextBg = adaptiveUserTextBgOrDefault(userMsgBG, adapt(lipgloss.Color("255"), lipgloss.Color("235")))
	adaptiveToolIconError = lipgloss.Color("196")
	adaptiveToolIconOk = lipgloss.Color("28")

	// Status bar / separators stay at their defaults (not message-themeable).
	adaptiveStatusBg = lipgloss.Color("236")
	adaptiveSeparator = lipgloss.Color("240")
	adaptiveFaint = lipgloss.Color("237")

	rebuildStyles()
}

// darkFG returns the default dark-palette value for the named field, used as
// the fallback when the user leaves a field empty.
func (d themeSet) darkFG(field string) color.Color {
	switch field {
	case "ThinkingFG":
		return lipgloss.Color(d.Dark.ThinkingFG)
	case "ToolResultFG":
		return lipgloss.Color(d.Dark.ToolResultFG)
	default:
		return lipgloss.Color("255")
	}
}

// adaptiveUserTextBgOrDefault keeps backward compatibility: if the user set a
// user_message background, use it; otherwise keep the terminal-adaptive value.
func adaptiveUserTextBgOrDefault(custom color.Color, fallback color.Color) color.Color {
	if custom != nil {
		return custom
	}
	return fallback
}

// resolveColor parses a color string; empty → fallback.
func resolveColor(s string, fallback color.Color) color.Color {
	if s == "" {
		return fallback
	}
	return lipgloss.Color(s)
}

// resolveBG parses a background color; "", "default", "none" → nil (transparent).
func resolveBG(s string) color.Color {
	if s == "" || s == "default" || s == "none" || s == "transparent" {
		return nil
	}
	return lipgloss.Color(s)
}

// availableThemes returns the names of all themes the user can switch to:
// always "default", plus every named theme under tui.theme.themes.
func availableThemes(custom map[string]any) []string {
	names := []string{"default"}
	if custom == nil {
		return names
	}
	themes, ok := custom["themes"].(map[string]any)
	if !ok {
		return names
	}
	for name := range themes {
		if name == "default" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// currentThemeName returns the active theme name from the config map.
func currentThemeName(custom map[string]any) string {
	if custom == nil {
		return "default"
	}
	if a, ok := custom["active"].(string); ok && a != "" {
		return a
	}
	return "default"
}

func extractThemeSet(custom map[string]any, name string, def themeSet) (themeSet, bool) {
	if custom == nil {
		return def, false
	}
	themes, ok := custom["themes"].(map[string]any)
	if !ok {
		return def, false
	}
	raw, ok := themes[name].(map[string]any)
	if !ok {
		return def, false
	}
	light := mergePalette(extractPalette(raw["light"]), def.Light)
	dark := mergePalette(extractPalette(raw["dark"]), def.Dark)
	return themeSet{Light: light, Dark: dark}, true
}

// extractPalette reads a {fg,bg,...} map from config.
func extractPalette(v any) themePalette {
	var p themePalette
	m, ok := v.(map[string]any)
	if !ok {
		return p
	}
	p.UserMessageFG = strField(m, "user_message", "fg")
	p.UserMessageBG = strField(m, "user_message", "bg")
	p.ToolUseFG = strField(m, "tool_use", "fg")
	p.ToolUseBG = strField(m, "tool_use", "bg")
	p.ToolResultFG = strField(m, "tool_result", "fg")
	p.ToolResultBG = strField(m, "tool_result", "bg")
	p.ThinkingFG = strField(m, "thinking", "fg")
	p.ThinkingBG = strField(m, "thinking", "bg")
	p.ResponseFG = strField(m, "response", "fg")
	p.ResponseBG = strField(m, "response", "bg")
	p.Background = strVal(m, "background")
	return p
}

// mergePalette fills empty fields in src with values from def.
func mergePalette(src, def themePalette) themePalette {
	fill := func(s, d string) string {
		if s == "" {
			return d
		}
		return s
	}
	return themePalette{
		UserMessageFG: fill(src.UserMessageFG, def.UserMessageFG),
		UserMessageBG: fill(src.UserMessageBG, def.UserMessageBG),
		ToolUseFG:     fill(src.ToolUseFG, def.ToolUseFG),
		ToolUseBG:     fill(src.ToolUseBG, def.ToolUseBG),
		ToolResultFG:  fill(src.ToolResultFG, def.ToolResultFG),
		ToolResultBG:  fill(src.ToolResultBG, def.ToolResultBG),
		ThinkingFG:    fill(src.ThinkingFG, def.ThinkingFG),
		ThinkingBG:    fill(src.ThinkingBG, def.ThinkingBG),
		ResponseFG:    fill(src.ResponseFG, def.ResponseFG),
		ResponseBG:    fill(src.ResponseBG, def.ResponseBG),
		Background:    fill(src.Background, def.Background),
	}
}

// strField reads m[group][key] as a string.
func strField(m map[string]any, group, key string) string {
	sub, ok := m[group].(map[string]any)
	if !ok {
		return ""
	}
	return strVal(sub, key)
}

// strVal reads m[key] as a string.
func strVal(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}
