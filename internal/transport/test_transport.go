package transport

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"dolphin/internal/common"
)

// TestTransport is a transport for testing the full agent loop.
// It provides a pre-loaded input and captures all output.
type TestTransport struct {
	*SessionHolder
	id     string
	input  chan string
	output strings.Builder
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
}

func NewTestTransport(id string) *TestTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &TestTransport{
		SessionHolder: NewSessionHolder(nil),
		id:            id,
		input:         make(chan string, 1),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// SendInput injects a user message. Safe to call from any goroutine.
func (t *TestTransport) SendInput(msg string) {
	t.input <- msg
}

// Output returns all captured output so far.
func (t *TestTransport) Output() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.output.String()
}

// Contains checks if the captured output contains the given substring.
func (t *TestTransport) Contains(sub string) bool {
	return strings.Contains(t.Output(), sub)
}

func (t *TestTransport) ID() string               { return t.id }
func (t *TestTransport) Context() string          { return "" }
func (t *TestTransport) Tools() []common.ToolDesc { return nil }

func (t *TestTransport) Start(ctx context.Context) error { return nil }

func (t *TestTransport) Read(ctx context.Context) (string, error) {
	select {
	case msg := <-t.input:
		return msg, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-t.ctx.Done():
		return "", io.EOF
	}
}

func (t *TestTransport) Write(ctx context.Context, text string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.output.WriteString(text)
	return nil
}

func (t *TestTransport) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.output.WriteString("\n")
	return nil
}

func (t *TestTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	t.cancel()
	return nil
}

func (t *TestTransport) RequestPermission(_ context.Context, msg string) (PermissionResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(&t.output, "[PERMISSION:%s]", msg)
	return PermissionOnce, nil
}

func (t *TestTransport) Confirm(_ context.Context, msg string) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(&t.output, "[CONFIRM:%s]", msg)
	return true, nil
}

func (t *TestTransport) Capability() Capability {
	return Capability{
		Interactive:        true,
		Streamable:         true,
		NestRead:           false,
		RenderTextMarkdown: "none",
	}
}

var _ IO = (*TestTransport)(nil)
