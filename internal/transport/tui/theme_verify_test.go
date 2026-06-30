package tui

import (
	"image/color"
	"testing"
)

// hexRGBA returns the RGBA of a color.Color, or false if it is not an
// RGB/hex color (e.g. an ANSI256 indexed color). Used to assert resolved
// theme colors deterministically.
func hexRGBA(c color.Color) (r, g, b uint8, ok bool) {
	rgba, isRGBA := c.(color.RGBA)
	if !isRGBA {
		// noColor / indexed colors won't match; report not-rgb.
		return 0, 0, 0, false
	}
	return rgba.R, rgba.G, rgba.B, true
}

// paletteBoth builds a theme config map where the given flat palette is applied
// to both light and dark, so applyTheme's terminal-background selection does
// not make the test nondeterministic.
func paletteBoth(palette map[string]any) map[string]any {
	return map[string]any{
		"active": "default",
		"themes": map[string]any{
			"default": map[string]any{
				"light": palette,
				"dark":  palette,
			},
		},
	}
}

// TestApplyTheme_UserMessageBG verifies checklist item 3: setting
// user_message.bg paints the user message block background.
func TestApplyTheme_UserMessageBG(t *testing.T) {
	cfg := paletteBoth(map[string]any{
		"user_message": map[string]any{"fg": "#ffffff", "bg": "#ff0000"},
	})
	applyTheme("default", cfg)
	r, g, b, ok := hexRGBA(userMsgBG)
	if !ok || r != 255 || g != 0 || b != 0 {
		t.Fatalf("userMsgBG = (%d,%d,%d, ok=%v), want red #ff0000", r, g, b, ok)
	}
}

// TestApplyTheme_ViewportBG verifies checklist item 4: setting background
// paints the whole TUI viewport.
func TestApplyTheme_ViewportBG(t *testing.T) {
	cfg := paletteBoth(map[string]any{
		"background": "#1a1a2e",
	})
	applyTheme("default", cfg)
	r, g, b, ok := hexRGBA(viewportBG)
	if !ok || r != 0x1a || g != 0x1a || b != 0x2e {
		t.Fatalf("viewportBG = (%d,%d,%d, ok=%v), want #1a1a2e", r, g, b, ok)
	}
}

// TestApplyTheme_CustomNamedTheme verifies checklist item 5: a user-defined
// named theme is selected via `active` and its colors are applied.
func TestApplyTheme_CustomNamedTheme(t *testing.T) {
	cfg := map[string]any{
		"active": "cyberpunk",
		"themes": map[string]any{
			"cyberpunk": map[string]any{
				"light": map[string]any{"tool_use": map[string]any{"fg": "#00ffff"}},
				"dark":  map[string]any{"tool_use": map[string]any{"fg": "#00ffff"}},
			},
		},
	}
	applyTheme("cyberpunk", cfg)
	r, g, b, ok := hexRGBA(adaptiveToolUse)
	if !ok || r != 0 || g != 0xff || b != 0xff {
		t.Fatalf("adaptiveToolUse = (%d,%d,%d, ok=%v), want cyan #00ffff", r, g, b, ok)
	}
}

// TestApplyTheme_EmptyFallsBack verifies checklist item 6: an empty field
// falls back to the built-in default theme rather than producing no color.
func TestApplyTheme_EmptyFallsBack(t *testing.T) {
	// Only set tool_use; thinking.fg is left empty → must still resolve to a
	// non-nil color (the default dark palette value).
	cfg := paletteBoth(map[string]any{
		"tool_use": map[string]any{"fg": "#00ffff"},
	})
	applyTheme("default", cfg)
	if adaptiveThinking == nil {
		t.Fatal("adaptiveThinking is nil; empty field should fall back to default, not unset")
	}
}

// TestApplyTheme_BackgroundDefault verifies checklist item 7: background
// "default" (or empty) yields a transparent (nil) viewport background.
func TestApplyTheme_BackgroundDefault(t *testing.T) {
	for _, val := range []string{"default", "", "none", "transparent"} {
		cfg := paletteBoth(map[string]any{"background": val})
		applyTheme("default", cfg)
		if viewportBG != nil {
			t.Fatalf("background=%q: viewportBG = %v, want nil (transparent)", val, viewportBG)
		}
	}
}

// TestApplyTheme_UnknownActiveFallsBack verifies that an unknown active theme
// name falls back to the default theme without error.
func TestApplyTheme_UnknownActiveFallsBack(t *testing.T) {
	applyTheme("does-not-exist", map[string]any{
		"active": "does-not-exist",
		"themes": map[string]any{},
	})
	// Default theme keeps viewport transparent.
	if viewportBG != nil {
		t.Fatalf("unknown active: viewportBG = %v, want nil (default theme)", viewportBG)
	}
}
