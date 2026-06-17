package memory

import (
	"context"

	"dolphin/internal/types"
)

type Memory interface {
	Read(ctx context.Context, sessionID string) ([]types.Message, error)
	Write(ctx context.Context, sessionID string, msg types.Message) error
	// Replace overwrites the entire message list for a session. It is used
	// by context compaction to substitute a compacted [summary + tail]
	// list for the prior full history in one atomic write.
	Replace(ctx context.Context, sessionID string, msgs []types.Message) error
}
