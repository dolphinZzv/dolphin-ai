package agentloop

import (
	"context"
	"errors"
	"testing"
	"time"

	"dolphin/internal/event"
	"dolphin/internal/memory"
	"dolphin/internal/types"
)

// Checkpoint.Write should flush only the not-yet-persisted tail of
// state.Messages, mark non-error messages as IsPartial, and advance
// PersistedIdx so a second Write is a no-op.
func TestCheckpoint_WriteFlushesTailAsPartial(t *testing.T) {
	mem := memory.NewFileMemory(&testSessionStore{})
	bus := event.NewBus()
	cp := NewCheckpoint(mem, bus)

	history := []types.Message{
		{Role: types.RoleUser, Content: "prior turn", Timestamp: time.Now()},
	}
	state := &State{
		SessionID:    "sess-cp-1",
		History:      history,
		PersistedIdx: len(history),
		Messages: append(history,
			types.Message{Role: types.RoleAssistant, Content: "partial stream output", Timestamp: time.Now()},
			types.Message{Role: types.RoleTool, ToolCallID: "tc1", Content: "tool result", Timestamp: time.Now()},
		),
	}

	if err := cp.Write(context.Background(), state, "watchdog idle timeout"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if state.PersistedIdx != len(state.Messages) {
		t.Fatalf("PersistedIdx = %d, want %d", state.PersistedIdx, len(state.Messages))
	}

	got, err := mem.Read(context.Background(), "sess-cp-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Checkpoint writes only the tail (Messages[PersistedIdx:]) — history
	// was already in memory or not, but the checkpoint doesn't re-write it.
	// Here history was never written, so memory contains exactly the 2
	// partial messages from the tail.
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	// Assistant message should be marked partial.
	if got[0].Role != types.RoleAssistant || !got[0].IsPartial {
		t.Errorf("got[0] = %+v, want partial assistant", got[0])
	}
	// Tool result is non-error, so it's also partial (round didn't complete).
	if got[1].Role != types.RoleTool || !got[1].IsPartial {
		t.Errorf("got[1] = %+v, want partial tool_result", got[1])
	}

	// Second Write with no new messages is a no-op.
	if err := cp.Write(context.Background(), state, "second"); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	got2, _ := mem.Read(context.Background(), "sess-cp-1")
	if len(got2) != 2 {
		t.Fatalf("after second Write, len = %d, want 2", len(got2))
	}
}

// Error messages (IsError=true) should not be re-marked as partial.
func TestCheckpoint_PreservesErrorMessages(t *testing.T) {
	mem := memory.NewFileMemory(&testSessionStore{})
	cp := NewCheckpoint(mem, event.NewBus())

	state := &State{
		SessionID:    "sess-cp-err",
		PersistedIdx: 0,
		Messages: []types.Message{
			{Role: types.RoleTool, Content: "boom", IsError: true, Timestamp: time.Now()},
		},
	}
	if err := cp.Write(context.Background(), state, "test"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, _ := mem.Read(context.Background(), "sess-cp-err")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].IsPartial {
		t.Error("error message should not be marked IsPartial")
	}
	if !got[0].IsError {
		t.Error("IsError flag lost")
	}
}

// Nil checkpoint / nil memory should be safe no-ops.
func TestCheckpoint_NilSafe(t *testing.T) {
	var cp *Checkpoint
	if err := cp.Write(context.Background(), &State{}, "x"); err != nil {
		t.Fatalf("nil checkpoint Write: %v", err)
	}
	cp = NewCheckpoint(nil, nil)
	if err := cp.Write(context.Background(), &State{Messages: []types.Message{{}}}, "x"); err != nil {
		t.Fatalf("nil memory Write: %v", err)
	}
}

// IsRecoverable should identify cancellation and deadline as recoverable,
// other errors as not.
func TestCheckpoint_IsRecoverable(t *testing.T) {
	if IsRecoverable(nil) {
		t.Error("nil should not be recoverable")
	}
	if !IsRecoverable(context.Canceled) {
		t.Error("context.Canceled should be recoverable")
	}
	if !IsRecoverable(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded should be recoverable")
	}
	if IsRecoverable(errors.New("permission denied")) {
		t.Error("permission denied should not be recoverable")
	}
}

// End-to-end: Compositor.Execute on a cancelled ctx with a Checkpoint
// wired should flush partial state to memory. Verifies the integration
// of checkpointOnFailure into Execute's error path.
func TestCompositor_CheckpointOnFailure(t *testing.T) {
	mem := memory.NewFileMemory(&testSessionStore{})
	bus := event.NewBus()

	// A loop stage that appends a partial assistant message then returns
	// context.Canceled, simulating an LLM stage interrupted mid-stream.
	interruptStage := &fakeStage{
		name: "llm_fake",
		process: func(ctx context.Context, state *State) error {
			state.Messages = append(state.Messages, types.Message{
				Role:      types.RoleAssistant,
				Content:   "half-generated output",
				Timestamp: time.Now(),
			})
			return context.Canceled
		},
	}

	c := NewCompositor(nil, []Stage{interruptStage}, 10)
	c.SetCheckpoint(NewCheckpoint(mem, bus))

	// Seed history so PersistedIdx baseline is non-zero.
	state := &State{
		SessionID:    "sess-int",
		History:      []types.Message{{Role: types.RoleUser, Content: "hi", Timestamp: time.Now()}},
		PersistedIdx: 1,
	}
	// Mirror the MemoryReadStage seeding of Messages = history + user input.
	state.Messages = append([]types.Message{}, state.History...)
	state.Messages = append(state.Messages, types.Message{
		Role:      types.RoleUser,
		Content:   "continue",
		Timestamp: time.Now(),
	})
	state.PersistedIdx = len(state.Messages) // user input was already "persisted" by prior turn

	err := c.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from cancelled stage")
	}

	got, _ := mem.Read(context.Background(), "sess-int")
	// The partial assistant message should have been flushed and marked.
	var found bool
	for _, m := range got {
		if m.Role == types.RoleAssistant && m.Content == "half-generated output" && m.IsPartial {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("partial assistant message not flushed to memory; got %d messages: %+v", len(got), got)
	}
}

// fakeStage is a test Stage with injectable behavior.
type fakeStage struct {
	name    string
	process func(ctx context.Context, state *State) error
}

func (s *fakeStage) Name() string                                 { return s.name }
func (s *fakeStage) Process(ctx context.Context, st *State) error { return s.process(ctx, st) }
func (s *fakeStage) Clone() Stage                                 { return s }
