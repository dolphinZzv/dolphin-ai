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

func (m *DroppingMemory) Read(ctx context.Context, sessionID string, start, end int) ([]types.Message, error) {
	msgs, err := m.inner.Read(ctx, sessionID, start, end)
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
		// Keep a leading summary message (produced by compaction) even if
		// it falls before the window start: it carries the compressed
		// history and must not be dropped.
		if start > 0 && msgs[0].IsSummary {
			msgs = append([]types.Message{msgs[0]}, msgs[start:]...)
		} else {
			msgs = msgs[start:]
		}
	}
	return msgs, nil
}

func (m *DroppingMemory) Write(ctx context.Context, sessionID string, msg types.Message) error {
	return m.inner.Write(ctx, sessionID, msg)
}

// Replace delegates to the inner memory so compaction's atomic overwrite
// reaches the durable store.
func (m *DroppingMemory) Replace(ctx context.Context, sessionID string, msgs []types.Message) error {
	return m.inner.Replace(ctx, sessionID, msgs)
}

// WriteTurn forwards the TurnMarker interface to the inner memory if supported.
func (m *DroppingMemory) WriteTurn(ctx context.Context, sessionID string, tp TurnPayload) error {
	if tm, ok := m.inner.(TurnMarker); ok {
		return tm.WriteTurn(ctx, sessionID, tp)
	}
	return nil
}

// TurnMarks forwards the TurnMarker interface to the inner memory if supported.
func (m *DroppingMemory) TurnMarks(sessionID string) ([]TurnMark, error) {
	if tm, ok := m.inner.(TurnMarker); ok {
		return tm.TurnMarks(sessionID)
	}
	return nil, nil
}

// RewindTo forwards the TurnMarker interface to the inner memory if supported.
func (m *DroppingMemory) RewindTo(sessionID string, seq uint64) error {
	if tm, ok := m.inner.(TurnMarker); ok {
		return tm.RewindTo(sessionID, seq)
	}
	return nil
}
