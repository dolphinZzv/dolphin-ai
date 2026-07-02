package memory

import (
	"context"

	"dolphin/internal/types"
)

// TurnMarker is an optional interface implemented by memory backends
// that support recording turn metadata for time-machine inspection.
// WALMemory implements this; FileMemory and SQLiteMemory do not.
type TurnMarker interface {
	WriteTurn(ctx context.Context, sessionID string, tp TurnPayload) error
	TurnMarks(sessionID string) ([]TurnMark, error)
	RewindTo(sessionID string, seq uint64) error
}

type Memory interface {
	// Read returns messages[start:end] for a session.
	// Both 0 means all messages. A negative start counts from the end
	// (e.g. start=-5, end=0 returns the last 5 messages).
	Read(ctx context.Context, sessionID string, start, end int) ([]types.Message, error)
	Write(ctx context.Context, sessionID string, msg types.Message) error
	// Replace overwrites the entire message list for a session. It is used
	// by context compaction to substitute a compacted [summary + tail]
	// list for the prior full history in one atomic write.
	Replace(ctx context.Context, sessionID string, msgs []types.Message) error
}
