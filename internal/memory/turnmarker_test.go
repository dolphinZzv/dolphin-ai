package memory_test

import (
	"context"
	"testing"

	"dolphin/internal/memory"
	"dolphin/internal/types"
)

func TestTurnMarker_ThroughDroppingMemory(t *testing.T) {
	dir := t.TempDir()
	wm, err := memory.NewWALMemory(dir, 0, 0)
	if err != nil {
		t.Fatalf("NewWALMemory: %v", err)
	}
	defer wm.Close()

	// Simulate the real chain: WALMemory → DroppingMemory
	dm := memory.NewDroppingMemory(wm, 40)

	// Verify DroppingMemory satisfies TurnMarker via Memory interface.
	var mem memory.Memory = dm
	tm, ok := mem.(memory.TurnMarker)
	if !ok {
		t.Fatal("DroppingMemory does not implement TurnMarker — agentloop type assertion will fail silently!")
	}

	sid := "test-turnmarker"

	// Write initial compact and messages.
	dm.Replace(nil, sid, []types.Message{types.NewTextMessage(types.RoleSystem, "init")})
	dm.Write(nil, sid, types.NewTextMessage(types.RoleUser, "hello"))

	// Write a turn mark.
	err = tm.WriteTurn(context.Background(), sid, memory.TurnPayload{
		TurnID:       "t-1",
		Input:        "hello",
		SystemPrompt: "You are a helpful assistant. You have access to tools.",
		ModelName:    "test-model",
		InTokens:     100,
		OutTokens:    50,
		Rounds:       1,
	})
	if err != nil {
		t.Fatalf("WriteTurn through DroppingMemory: %v", err)
	}

	// Read turn marks through DroppingMemory.
	marks, err := tm.TurnMarks(sid)
	if err != nil {
		t.Fatalf("TurnMarks: %v", err)
	}
	if len(marks) != 1 {
		t.Fatalf("expected 1 turn mark, got %d", len(marks))
	}
	if marks[0].Input != "hello" {
		t.Errorf("input mismatch: %q", marks[0].Input)
	}
	if marks[0].SystemPrompt != "You are a helpful assistant. You have access to tools." {
		t.Errorf("SystemPrompt mismatch: got %q", marks[0].SystemPrompt)
	}
	t.Logf("TurnMark verified: input=%q SystemPrompt(len=%d)", marks[0].Input, len(marks[0].SystemPrompt))
}
