package transport

import (
	"strings"
	"sync"
)

// BufferedIO wraps a UserIO and buffers output for non-streaming transports.
// When Streaming is true, WriteString/WriteLine pass through immediately.
// When Streaming is false, writes are buffered and only sent on Flush().
// ReadLine() auto-flushes before reading to ensure buffered output is delivered.
type BufferedIO struct {
	inner UserIO
	mu    sync.Mutex
	buf   strings.Builder
	caps  Capabilities
}

// NewBufferedIO creates a BufferedIO wrapper.
func NewBufferedIO(inner UserIO) *BufferedIO {
	return &BufferedIO{inner: inner, caps: inner.Capabilities()}
}

func (b *BufferedIO) ReadLine() (string, error) {
	b.Flush()
	return b.inner.ReadLine()
}

func (b *BufferedIO) WriteLine(s string) error {
	if b.caps.Streaming {
		return b.inner.WriteLine(s)
	}
	b.mu.Lock()
	b.buf.WriteString(s)
	b.buf.WriteString("\n")
	b.mu.Unlock()
	return nil
}

func (b *BufferedIO) WriteString(s string) error {
	if b.caps.Streaming {
		return b.inner.WriteString(s)
	}
	b.mu.Lock()
	b.buf.WriteString(s)
	b.mu.Unlock()
	return nil
}

// Flush sends all buffered content to the underlying transport.
func (b *BufferedIO) Flush() error {
	b.mu.Lock()
	content := b.buf.String()
	b.buf.Reset()
	b.mu.Unlock()
	if content == "" {
		return nil
	}
	return b.inner.WriteString(content)
}

func (b *BufferedIO) Capabilities() Capabilities { return b.caps }

func (b *BufferedIO) Context() string { return b.inner.Context() }

func (b *BufferedIO) Name() string { return b.inner.Name() }
