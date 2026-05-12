package agent

import (
	"io"

	"dolphin/internal/transport"
)

// ChannelIO implements transport.UserIO using channels and buffers.
// Used for headless sub-agent execution where ReadLine returns the
// task once, and Write/WriteLine are discarded (response extracted
// from LoopState.Messages after runTurn completes).
type ChannelIO struct {
	input chan string
}

func NewChannelIO(task string) *ChannelIO {
	ch := make(chan string, 1)
	ch <- task
	return &ChannelIO{input: ch}
}

func (c *ChannelIO) ReadLine() (string, error) {
	msg, ok := <-c.input
	if !ok {
		return "", io.EOF
	}
	return msg, nil
}

func (c *ChannelIO) WriteLine(string) error   { return nil }
func (c *ChannelIO) WriteString(string) error { return nil }

func (c *ChannelIO) Context() string { return "" }

func (c *ChannelIO) Capabilities() transport.Capabilities {
	return transport.Capabilities{} // streaming=false by default, writes are no-ops anyway
}
