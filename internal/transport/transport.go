package transport

import "context"

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
	Capabilities() Capabilities
	Context() string // transport-specific context injected into system prompt
}

// Capabilities describes a transport's write semantics and interaction features.
// Streaming transports send each write immediately (e.g. WebSocket, stdio).
// Block transports batch writes and flush periodically (e.g. MQTT, Email).
type Capabilities struct {
	Streaming   bool
	Flushable   bool
	ConfirmExit bool // if true, require confirmation before exiting the agent
}
