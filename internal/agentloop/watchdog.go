package agentloop

import (
	"context"
	"sync"
	"time"

	"dolphin/internal/progress"
)

// Watchdog cancels its context if Feed is not called within idle.
// A zero idle disables the watchdog (New returns the parent ctx and nil).
//
// Feed is safe for concurrent use. Once the watchdog has fired or been
// stopped, Feed is a no-op.
//
// To make Feed cheap enough to call from hot paths (e.g. every stdout
// line of a streaming tool, every LLM chunk), Feed throttles itself
// internally: if the last effective feed was less than minFeedInterval
// ago, the call is a no-op (only an atomic-ish counter under lock
// increments). Default minFeedInterval is 100ms, so even a tool that
// emits 10000 lines/sec causes at most 10 actual timer resets/sec.
type Watchdog struct {
	mu              sync.Mutex
	timer           *time.Timer
	cancel          context.CancelFunc
	idle            time.Duration
	minFeedInterval time.Duration
	last            time.Time
	feeds           int64
	skippedFeeds    int64
	done            bool
}

// DefaultMinFeedInterval is the throttle window applied to Feed when
// the watchdog is constructed via New. Calls closer than this to the
// previous effective feed are no-ops.
const DefaultMinFeedInterval = 100 * time.Millisecond

// New wraps parent with a watchdog that cancels the returned ctx if
// Feed is not called within idle. If idle <= 0, the watchdog is
// disabled and New returns parent unchanged with a nil *Watchdog.
//
// The watchdog is attached to the returned ctx via progress.With, so
// any code along the call chain can call progress.Feed(ctx) (or
// agentloop.Feed(ctx)) without importing agentloop.
//
// The caller must call Stop on the returned *Watchdog when the work
// guarded by the watchdog is finished, so the underlying timer is
// released. Stop is nil-safe.
func New(parent context.Context, idle time.Duration) (context.Context, *Watchdog) {
	if idle <= 0 {
		return parent, nil
	}
	ctx, cancel := context.WithCancel(parent)
	w := &Watchdog{
		idle:            idle,
		minFeedInterval: DefaultMinFeedInterval,
		cancel:          cancel,
		last:            time.Now(),
	}
	w.timer = time.AfterFunc(idle, func() {
		w.mu.Lock()
		w.done = true
		w.mu.Unlock()
		cancel()
	})
	return progress.With(ctx, w), w
}

// SetMinFeedInterval adjusts the throttle window. Must be called before
// the watchdog is in use. Useful for tests that need sub-millisecond
// throttling. Set to 0 to disable throttling (every Feed resets the
// timer). Nil-safe.
func (w *Watchdog) SetMinFeedInterval(d time.Duration) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.minFeedInterval = d
}

// Feed resets the idle timer, unless it was reset less than
// minFeedInterval ago (throttled) or the watchdog has fired/stopped.
// Safe for concurrent use; nil-safe.
func (w *Watchdog) Feed() {
	if w == nil {
		return
	}
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return
	}
	w.feeds++
	if w.minFeedInterval > 0 && now.Sub(w.last) < w.minFeedInterval {
		w.skippedFeeds++
		return
	}
	w.timer.Stop()
	w.timer.Reset(w.idle)
	w.last = now
}

// Stop halts the watchdog and releases the timer. Idempotent; nil-safe.
func (w *Watchdog) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return
	}
	w.done = true
	w.timer.Stop()
}

// Stats returns a snapshot for diagnostics when a watchdog fires.
type WatchdogStats struct {
	Feeds        int64
	SkippedFeeds int64
	LastFeed     time.Time
	Fired        bool
}

func (w *Watchdog) Stats() WatchdogStats {
	if w == nil {
		return WatchdogStats{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return WatchdogStats{
		Feeds:        w.feeds,
		SkippedFeeds: w.skippedFeeds,
		LastFeed:     w.last,
		Fired:        w.done,
	}
}

// FromCtx returns the watchdog attached to ctx by New, or nil.
// Prefer progress.From(ctx) in packages that must not import agentloop.
func FromCtx(ctx context.Context) *Watchdog {
	if w, ok := progress.From(ctx).(*Watchdog); ok {
		return w
	}
	return nil
}

// Feed feeds the watchdog attached to ctx, if any. Nil-safe; intended
// as the one-liner feed point inside stages and tools:
//
//	agentloop.Feed(ctx)
//
// Code in packages that cannot import agentloop (e.g. internal/tool)
// should call progress.Feed(ctx) instead — same effect, no cycle.
func Feed(ctx context.Context) {
	progress.Feed(ctx)
}
