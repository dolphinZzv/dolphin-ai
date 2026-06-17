package agentloop

import (
	"context"
	"testing"
	"time"
)

// Watchdog should cancel its ctx when Feed is never called within idle.
func TestWatchdog_FiresWhenNotFed(t *testing.T) {
	ctx, wd := New(context.Background(), 30*time.Millisecond)
	defer wd.Stop()

	select {
	case <-ctx.Done():
		// expected
	case <-time.After(200 * time.Millisecond):
		t.Fatal("watchdog did not fire within idle window")
	}
	if err := ctx.Err(); err != context.Canceled {
		t.Fatalf("ctx.Err() = %v, want Canceled", err)
	}
	if st := wd.Stats(); !st.Fired {
		t.Fatal("Stats.Fired = false, want true after firing")
	}
}

// Feeding before idle elapses keeps the ctx alive.
func TestWatchdog_FeedKeepsAlive(t *testing.T) {
	ctx, wd := New(context.Background(), 40*time.Millisecond)
	defer wd.Stop()
	// Disable throttle for this test so every Feed is effective; the
	// default 100ms throttle would skip feeds fired every 15ms.
	wd.SetMinFeedInterval(0)

	// Feed every 15ms for ~120ms — well under the 40ms idle.
	stop := time.After(120 * time.Millisecond)
loop:
	for {
		select {
		case <-stop:
			break loop
		case <-time.After(15 * time.Millisecond):
			Feed(ctx)
		}
	}

	select {
	case <-ctx.Done():
		t.Fatalf("ctx cancelled while being fed: %v", ctx.Err())
	default:
		// expected: still alive
	}
	if st := wd.Stats(); st.Feeds < 4 {
		t.Fatalf("Stats.Feeds = %d, want >= 4", st.Feeds)
	}
}

// Feed throttling: calls closer than minFeedInterval to the previous
// effective feed are skipped. Effective feeds still reset the timer.
func TestWatchdog_FeedThrottled(t *testing.T) {
	// idle=200ms, throttle=50ms. Feed every 10ms for 100ms.
	// Effective feeds should be ~2-3 (at t=0, t=50, t=100), rest skipped.
	// All feeds keep the ctx alive since effective feed gap < idle.
	ctx, wd := New(context.Background(), 200*time.Millisecond)
	defer wd.Stop()
	wd.SetMinFeedInterval(50 * time.Millisecond)

	for range 10 {
		Feed(ctx)
		time.Sleep(10 * time.Millisecond)
	}

	st := wd.Stats()
	if st.Feeds != 10 {
		t.Errorf("total Feeds = %d, want 10", st.Feeds)
	}
	if st.SkippedFeeds < 5 {
		t.Errorf("SkippedFeeds = %d, want >= 5", st.SkippedFeeds)
	}
	// ctx should still be alive (effective feeds within idle).
	select {
	case <-ctx.Done():
		t.Fatalf("ctx cancelled despite effective feeds: %v", ctx.Err())
	default:
	}
}

// Throttle must not cause a missed heartbeat when the gap between
// effective feeds is just under idle. This guards against off-by-one
// in the throttle comparison.
func TestWatchdog_ThrottleDoesNotStarveIdle(t *testing.T) {
	// idle=80ms, throttle=30ms. Feed every 30ms.
	// Effective feeds at t=0, 30, 60, 90, ... — gap 30ms < 80ms idle, OK.
	ctx, wd := New(context.Background(), 80*time.Millisecond)
	defer wd.Stop()
	wd.SetMinFeedInterval(30 * time.Millisecond)

	stop := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case <-stop:
			break loop
		case <-time.After(30 * time.Millisecond):
			Feed(ctx)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatalf("ctx cancelled despite steady effective feeds: %v", ctx.Err())
	default:
	}
}

// After Stop is called, the watchdog should not fire even if idle elapses.
func TestWatchdog_StopDisarms(t *testing.T) {
	ctx, wd := New(context.Background(), 20*time.Millisecond)
	wd.Stop()

	time.Sleep(60 * time.Millisecond)
	select {
	case <-ctx.Done():
		t.Fatalf("ctx cancelled after Stop: %v", ctx.Err())
	default:
	}
}

// After firing, Feed is a no-op (does not panic, does not resurrect).
func TestWatchdog_FeedAfterFireIsNoop(t *testing.T) {
	ctx, wd := New(context.Background(), 10*time.Millisecond)
	<-ctx.Done()

	// Should not panic and should not resurrect the ctx.
	wd.Feed()
	wd.Feed()
	select {
	case <-ctx.Done():
		// still done, good
	default:
		t.Fatal("ctx resurrected after firing")
	}
}

// idle <= 0 disables the watchdog: New returns parent and nil.
func TestWatchdog_DisabledWhenIdleZero(t *testing.T) {
	parent := context.Background()
	ctx, wd := New(parent, 0)
	if wd != nil {
		t.Fatal("idle=0 should return nil watchdog")
	}
	if ctx != parent {
		t.Fatal("idle=0 should return parent ctx unchanged")
	}
	// Feed on a context without a watchdog is a no-op.
	Feed(ctx)
	FromCtx(ctx)
}

// Feed is safe for concurrent callers.
func TestWatchdog_ConcurrentFeed(t *testing.T) {
	ctx, wd := New(context.Background(), 100*time.Millisecond)
	defer wd.Stop()

	done := make(chan struct{})
	for range 8 {
		go func() {
			for range 100 {
				Feed(ctx)
			}
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
	select {
	case <-ctx.Done():
		t.Fatalf("ctx cancelled under concurrent feed: %v", ctx.Err())
	default:
	}
}
