package memory

import (
	"context"

	"dolphin/internal/types"
)

// DroppingMemory wraps a Memory and drops oldest messages
// to keep at most window rounds (one user + one assistant per round).
type DroppingMemory struct {
	inner  Memory
	window int
}

func NewDroppingMemory(inner Memory, window int) *DroppingMemory {
	return &DroppingMemory{inner: inner, window: window}
}

func (m *DroppingMemory) Read(ctx context.Context, sessionID string) ([]types.Message, error) {
	msgs, err := m.inner.Read(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if m.window > 0 && len(msgs) > m.window*2 {
		msgs = msgs[len(msgs)-m.window*2:]
	}
	return msgs, nil
}

func (m *DroppingMemory) Write(ctx context.Context, sessionID string, msg types.Message) error {
	return m.inner.Write(ctx, sessionID, msg)
}
