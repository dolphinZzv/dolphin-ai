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
		start := len(msgs) - m.window*2
		// Never orphan tool_result messages: walk back until we find a
		// non-tool message (user or assistant) so every tool_result in
		// the kept range has its corresponding tool_use.
		for start > 0 && msgs[start].Role == types.RoleTool {
			start--
		}
		msgs = msgs[start:]
	}
	return msgs, nil
}

func (m *DroppingMemory) Write(ctx context.Context, sessionID string, msg types.Message) error {
	return m.inner.Write(ctx, sessionID, msg)
}
