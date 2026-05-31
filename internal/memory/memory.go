package memory

import (
	"context"

	"dolphin/internal/types"
)

type Memory interface {
	Read(ctx context.Context, sessionID string) ([]types.Message, error)
	Write(ctx context.Context, sessionID string, msg types.Message) error
}
