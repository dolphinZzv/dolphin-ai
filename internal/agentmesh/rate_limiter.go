package agentmesh

import (
	"sync"
	"time"
)

// tokenBucket is a minimal token-bucket limiter (rate + burst) implemented
// with stdlib only, to avoid pulling in golang.org/x/time/rate.
type tokenBucket struct {
	ratePerSec float64 // refill rate
	burst      float64 // capacity
	tokens     float64
	last       time.Time
}

func newTokenBucket(ratePerSec float64, burst int) *tokenBucket {
	return &tokenBucket{
		ratePerSec: ratePerSec,
		burst:      float64(burst),
		tokens:     float64(burst),
		// last left zero; set on first allow() so the bucket is not biased by
		// wall-clock skew between construction and the first request.
	}
}

func (b *tokenBucket) allow(now time.Time) bool {
	// First call seeds the clock without refilling (tokens already at burst).
	if b.last.IsZero() {
		b.last = now
	}
	// refill — clamp elapsed >= 0 so a slightly-earlier `now` (clock skew,
	// or a test passing a fixed time before construction) never drains tokens.
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.ratePerSec
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

// RateLimiter is a per-target-agent sender-side rate limiter.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket // keyed by agent addr
	rate    float64
	burst   int
}

// NewRateLimiter builds a limiter with the given per-agent rate (req/s) and
// burst.
func NewRateLimiter(ratePerSec float64, burst int) *RateLimiter {
	if ratePerSec <= 0 {
		ratePerSec = 2
	}
	if burst <= 0 {
		burst = 5
	}
	return &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    ratePerSec,
		burst:   burst,
	}
}

// Allow reports whether a delegation to the named agent may proceed now.
func (rl *RateLimiter) Allow(agentAddr string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[agentAddr]
	if !ok {
		b = newTokenBucket(rl.rate, rl.burst)
		rl.buckets[agentAddr] = b
	}
	return b.allow(time.Now())
}

// AllowAt is Allow with an injectable clock (tests).
func (rl *RateLimiter) AllowAt(agentAddr string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[agentAddr]
	if !ok {
		b = newTokenBucket(rl.rate, rl.burst)
		rl.buckets[agentAddr] = b
	}
	return b.allow(now)
}
