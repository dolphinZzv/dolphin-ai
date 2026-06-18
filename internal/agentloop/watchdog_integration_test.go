package agentloop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"dolphin/internal/tool"
)

// End-to-end: a long-running shell command that emits output regularly
// should keep the watchdog alive. Without per-line feeding this would
// fire the 80ms idle watchdog; with feeding it survives.
func TestWatchdog_ShellToolStaysAliveWithOutput(t *testing.T) {
	// idle=80ms, throttle=10ms. Shell emits a line every 20ms.
	ctx, wd := New(context.Background(), 80*time.Millisecond)
	defer wd.Stop()
	wd.SetMinFeedInterval(10 * time.Millisecond)

	h := tool.BuiltinMCPHandlers(nil)["shell"]
	result, err := h(ctx, json.RawMessage(`{"command":"for i in 1 2 3 4 5 6 7 8; do echo line$i; sleep 0.02; done"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("shell failed: %s", result.Content)
	}
	// If we get here, the watchdog didn't fire — success.
	st := wd.Stats()
	if st.Feeds < 5 {
		t.Fatalf("expected >= 5 feeds, got %d (skipped %d)", st.Feeds, st.SkippedFeeds)
	}
}

// End-to-end: a silent shell command should trigger the watchdog.
// This verifies that we don't over-feed — silent stalls are still caught.
func TestWatchdog_ShellToolFiresOnSilentStall(t *testing.T) {
	ctx, wd := New(context.Background(), 50*time.Millisecond)
	defer wd.Stop()
	wd.SetMinFeedInterval(0)

	h := tool.BuiltinMCPHandlers(nil)["shell"]
	// sleep 2s with no output — watchdog should fire at ~50ms.
	// CommandContext tied to ctx will be killed when ctx cancels.
	start := time.Now()
	_, _ = h(ctx, json.RawMessage(`{"command":"sleep 2"}`))
	elapsed := time.Since(start)

	// Should have been killed well under 2s — somewhere around 50-200ms on a
	// fast machine. Allow generous headroom for CI runners under -race.
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("watchdog didn't fire in time, elapsed=%v", elapsed)
	}
	if !wd.Stats().Fired {
		t.Fatal("watchdog should have fired")
	}
}

// Benchmark: Feed call overhead under high frequency with throttling.
// A tool emitting 10000 lines/sec should not spend measurable time in Feed.
func BenchmarkFeed_Throttled(b *testing.B) {
	ctx, wd := New(context.Background(), 10*time.Second)
	defer wd.Stop()
	wd.SetMinFeedInterval(100 * time.Millisecond) // default throttle

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Feed(ctx)
	}
}

// Benchmark: Feed with throttle disabled (every call resets timer).
// This is the worst case — measures raw Feed overhead.
func BenchmarkFeed_Unthrottled(b *testing.B) {
	ctx, wd := New(context.Background(), 10*time.Second)
	defer wd.Stop()
	wd.SetMinFeedInterval(0) // no throttle

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Feed(ctx)
	}
}

// Benchmark: Feed on a context with no feeder attached (no-op path).
// Measures the cost of the ctx.Value lookup when called from code that
// doesn't know whether a feeder exists.
func BenchmarkFeed_NoFeeder(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		Feed(ctx)
	}
}
