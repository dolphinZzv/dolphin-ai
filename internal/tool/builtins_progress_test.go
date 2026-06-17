package tool

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dolphin/internal/progress"
)

// countingFeeder records every Feed call. Implements progress.Feeder.
type countingFeeder struct {
	count atomic.Int64
}

func (c *countingFeeder) Feed() { c.count.Add(1) }

// Shell handler should call progress.Feed on each stdout line so a
// long-running command with regular output keeps the watchdog alive.
func TestShellHandler_FeedsProgressPerLine(t *testing.T) {
	feeder := &countingFeeder{}
	ctx := progress.With(context.Background(), feeder)

	h := BuiltinMCPHandlers(nil)["shell"]
	// Emit 5 lines, 10ms apart. Should produce >= 5 feed calls.
	result, err := h(ctx, json.RawMessage(`{"command":"for i in 1 2 3 4 5; do echo line$i; sleep 0.01; done"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if got := feeder.count.Load(); got < 5 {
		t.Fatalf("feeder called %d times, want >= 5 (one per line)", got)
	}
	if !strings.Contains(result.Content, "line5") {
		t.Fatalf("expected line5 in output, got: %s", result.Content)
	}
}

// Shell handler should not feed when the command produces no output —
// that's the correct behavior, the watchdog is supposed to fire on
// silent stalls.
func TestShellHandler_NoFeedOnSilentCommand(t *testing.T) {
	feeder := &countingFeeder{}
	ctx := progress.With(context.Background(), feeder)

	h := BuiltinMCPHandlers(nil)["shell"]
	// sleep with no output — feeder should not be called.
	_, err := h(ctx, json.RawMessage(`{"command":"sleep 0.05"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := feeder.count.Load(); got != 0 {
		t.Fatalf("feeder called %d times on silent command, want 0", got)
	}
}

// Streaming output must be preserved in the result content even though
// we read it line-by-line for feeding.
func TestShellHandler_StreamingPreservesOutput(t *testing.T) {
	ctx := progress.With(context.Background(), &countingFeeder{})
	h := BuiltinMCPHandlers(nil)["shell"]
	result, err := h(ctx, json.RawMessage(`{"command":"printf 'a\\nb\\nc\\n'"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "a\nb\nc\n" {
		t.Fatalf("expected 'a\\nb\\nc\\n', got %q", result.Content)
	}
}

// Trailing partial line (no newline) must be preserved.
func TestShellHandler_PreservesPartialLastLine(t *testing.T) {
	ctx := progress.With(context.Background(), &countingFeeder{})
	h := BuiltinMCPHandlers(nil)["shell"]
	result, err := h(ctx, json.RawMessage(`{"command":"printf 'no-newline'"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "no-newline" {
		t.Fatalf("expected 'no-newline', got %q", result.Content)
	}
}

// Slow command should respect the watchdog's idle window when fed.
// This is a smoke test for the integration with progress.Feed — the
// real watchdog behavior is verified in internal/agentloop.
func TestShellHandler_FeedIntegratesWithProgress(t *testing.T) {
	// If progress.Feed is wired to a nil feeder (no watchdog attached),
	// the shell handler should still work normally.
	h := BuiltinMCPHandlers(nil)["shell"]
	result, err := h(context.Background(), json.RawMessage(`{"command":"echo ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "ok") {
		t.Fatalf("expected 'ok' in output, got: %s", result.Content)
	}
}

// Ensure shell handler doesn't hang if context is cancelled mid-stream.
func TestShellHandler_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	h := BuiltinMCPHandlers(nil)["shell"]
	result, _ := h(ctx, json.RawMessage(`{"command":"sleep 10"}`))
	// Should return (likely with an error result) rather than hanging.
	if result == nil {
		t.Fatal("expected non-nil result even on cancel")
	}
}
