package agentloop

import (
	"context"
	"errors"
	"time"

	"dolphin/internal/event"
	"dolphin/internal/memory"
)

// Checkpoint flushes the not-yet-persisted tail of state.Messages to
// memory when a turn fails, so the next turn can resume from the
// failure point instead of from the last successful round.
//
// The checkpoint is partial: it may contain an assistant message that
// was being streamed when the failure occurred, plus tool results that
// were produced before the failure. Such messages are marked
// IsPartial=true so downstream code (and the LLM, via the system prompt)
// can distinguish "completed round output" from "aborted mid-flight".
//
// Design note: this is the B step toward durable execution. The
// API is shaped to extend to per-stage checkpointing (B+) without
// rewrite — Write takes a reason and publishes a checkpoint event,
// leaving room for periodic non-failure checkpoints later.
type Checkpoint struct {
	Memory   memory.Memory
	EventBus *event.Bus
}

// NewCheckpoint creates a Checkpoint wired to the given memory and
// event bus. Either may be nil; in that case Write is a no-op.
func NewCheckpoint(mem memory.Memory, bus *event.Bus) *Checkpoint {
	return &Checkpoint{Memory: mem, EventBus: bus}
}

// Write flushes state.Messages[state.PersistedIdx:] to memory. Each
// flushed message gets IsPartial=true so readers can tell it was
// produced by an aborted turn. Messages already marked IsError are
// left as-is (errors are not "partial" — they're terminal).
//
// reason is a short human-readable string describing why the checkpoint
// was taken (e.g. "watchdog idle timeout", "round timeout"). It is
// published as event metadata for observability; it is not written
// into message content.
//
// Write is best-effort: a write error is returned but does not block
// the turn's failure path from completing. Callers should log it.
//
// Concurrency: Checkpoint is shared across all worker compositor clones
// (Compositor.Clone copies the pointer). Write itself is stateless — it
// only reads the per-turn `state` and delegates to Memory.Write, which
// is concurrency-safe (FileMemory holds its own mutex). Per-session
// turns are serialized by AgentLoop's sessionLock, so two Write calls
// racing on the same session cannot happen; races across different
// sessions touch disjoint session state. No additional locking here.
func (c *Checkpoint) Write(ctx context.Context, state *State, reason string) error {
	if c == nil || c.Memory == nil || state == nil {
		return nil
	}
	if state.PersistedIdx > len(state.Messages) {
		state.PersistedIdx = len(state.Messages)
	}
	tail := state.Messages[state.PersistedIdx:]
	if len(tail) == 0 {
		return nil
	}

	if c.EventBus != nil {
		c.EventBus.Publish(ctx, event.Event{
			Type:      event.EventMemoryWriteStart,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
			Payload: map[string]any{
				"checkpoint": true,
				"reason":     reason,
				"messages":   len(tail),
			},
		})
	}

	for i := range tail {
		msg := tail[i]
		// Mark non-error messages as partial so the LLM on resume knows
		// this output was aborted mid-flight. Tool_result messages from
		// tools that did complete are also partial in the sense that the
		// round didn't finish — but their content is still accurate, so
		// we mark them too for consistency.
		if !msg.IsError {
			msg.IsPartial = true
		}
		if err := c.Memory.Write(ctx, state.SessionID, msg); err != nil {
			return err
		}
	}
	state.PersistedIdx = len(state.Messages)

	if c.EventBus != nil {
		c.EventBus.Publish(ctx, event.Event{
			Type:      event.EventMemoryWriteComplete,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
			Payload: map[string]any{
				"checkpoint": true,
				"reason":     reason,
			},
		})
	}
	return nil
}

// IsRecoverable reports whether err is a failure mode where checkpointing
// partial state is worthwhile. Currently: context cancellation (watchdog
// or upstream cancel) and deadline exceeded (round timeout). Other
// errors (permission denials, tool errors that already wrote their own
// tool_result) don't benefit from a partial checkpoint.
func IsRecoverable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
