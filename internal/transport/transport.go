// Package transport provides user I/O transport implementations.
package transport

import (
	"context"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

// Transport represents a user connection transport.
// Each Transport instance corresponds to one user session.
type Transport interface {
	// Name returns a human-readable name ("stdio", "ssh", "mqtt").
	Name() string

	// Start initiates the transport and blocks until the session ends.
	Start(ctx context.Context) error

	// Close terminates the transport.
	Close() error
}

// UserIO provides readline-based interactive I/O for the agent loop.
type UserIO interface {
	ReadLine() (string, error)
	WriteLine(string) error
	WriteString(string) error
	Flush() error
	Capabilities() Capabilities
	Context() string // transport-specific context injected into system prompt
	Name() string    // transport name ("stdio", "email", "ssh")
}

// Capabilities describes a transport's write semantics and interaction features.
// Streaming transports send each write immediately (e.g. WebSocket, stdio).
// Block transports batch writes and flush periodically (e.g. MQTT, Email).
type Capabilities struct {
	Streaming       bool
	ConfirmExit     bool // if true, require confirmation before exiting the agent
	ShowToolDetails bool // if true, show tool call arguments/outputs to the user
}

// SessionTransport is a Transport that accepts incoming sessions and dispatches
// them to a handler. Only SSH implements this.
type SessionTransport interface {
	Transport
	SetSessionHandler(func(context.Context, UserIO))
}

// BannerProvider is an optional interface that transports can implement to
// provide a startup banner describing how to connect or use the transport.
type BannerProvider interface {
	Banner() string
}

// Truncate truncates a string to max characters, appending "..." if needed.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// NewMarkdownRenderer creates a glamour terminal renderer for the given style name.
// Supported styles: "auto" (auto-detect dark/light), "dark", "light", "ascii",
// "pink", "dracula", "tokyo-night". Falls back to auto on unknown style names.
// Returns nil on error.
func NewMarkdownRenderer(style string) *glamour.TermRenderer {
	switch style {
	case "", "auto":
		md, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(0))
		if err == nil {
			return md
		}
	default:
		if sc, ok := styles.DefaultStyles[style]; ok {
			md, err := glamour.NewTermRenderer(glamour.WithStyles(*sc), glamour.WithWordWrap(0))
			if err == nil {
				return md
			}
		}
	}
	return nil
}
