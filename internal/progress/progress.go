// Package progress provides a context-attached feed point that long-running
// operations (tools, LLM streams, sub-tasks) use to signal "still alive" to
// an idle watchdog without taking a dependency on the watchdog implementation.
//
// The pattern: a watchdog owner calls progress.With(ctx, feeder) when
// wrapping a context; any code along the call chain can call
// progress.Feed(ctx) to reset the idle timer. Callers that don't care
// about feeding just call Feed and it's a nil-safe no-op when no
// feeder is attached.
package progress

import "context"

// Feeder is implemented by anything that can be fed as a heartbeat.
// The canonical implementation is agentloop.Watchdog.
type Feeder interface {
	Feed()
}

type ctxKey struct{}

// With returns a ctx carrying feeder. feeder may be nil, in which case
// With returns parent unchanged and Feed on the result is a no-op.
func With(parent context.Context, feeder Feeder) context.Context {
	if feeder == nil {
		return parent
	}
	return context.WithValue(parent, ctxKey{}, feeder)
}

// From returns the feeder attached to ctx, or nil.
func From(ctx context.Context) Feeder {
	if f, ok := ctx.Value(ctxKey{}).(Feeder); ok {
		return f
	}
	return nil
}

// Feed feeds the feeder attached to ctx. Nil-safe: if no feeder is
// attached (or ctx is nil), Feed is a no-op. Intended as the one-liner
// feed point inside tools and stages.
func Feed(ctx context.Context) {
	if ctx == nil {
		return
	}
	if f, ok := ctx.Value(ctxKey{}).(Feeder); ok && f != nil {
		f.Feed()
	}
}
