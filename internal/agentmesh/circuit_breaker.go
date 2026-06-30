package agentmesh

import (
	"sync"
	"time"
)

// cbState is the internal state of a circuit breaker.
type cbState int

const (
	cbClosed cbState = iota
	cbOpen
	cbHalfOpen
)

// CircuitBreaker is a per-agent circuit breaker.
//
// State machine:
//
//	CLOSED --consecutive failures>=threshold--> OPEN
//	CLOSED <-trial success-- HALF_OPEN <-cooldown elapsed-- OPEN
//	                 |
//	          trial failure --> OPEN
type CircuitBreaker struct {
	mu               sync.Mutex
	state            cbState
	failures         int           // consecutive failures (CLOSED)
	threshold        int           // failures before opening
	cooldown         time.Duration // OPEN → wait this long before HALF_OPEN
	halfOpenMax      int           // max trial requests in HALF_OPEN
	halfOpenInflight int           // trials currently in flight in HALF_OPEN
	openedAt         time.Time     // when OPEN began
	now              func() time.Time
}

// NewCircuitBreaker builds a breaker with the given config.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.CooldownPeriod <= 0 {
		cfg.CooldownPeriod = 60 * time.Second
	}
	if cfg.HalfOpenMax <= 0 {
		cfg.HalfOpenMax = 1
	}
	return &CircuitBreaker{
		threshold:   cfg.FailureThreshold,
		cooldown:    cfg.CooldownPeriod,
		halfOpenMax: cfg.HalfOpenMax,
		now:         func() time.Time { return time.Now() },
	}
}

// Allow reports whether a request may proceed. When allowed, the caller must
// report the outcome via RecordSuccess or RecordFailure.
func (c *CircuitBreaker) Allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.state {
	case cbClosed:
		return true
	case cbOpen:
		if c.now().Sub(c.openedAt) >= c.cooldown {
			c.state = cbHalfOpen
			c.halfOpenInflight = 1
			return true
		}
		return false
	case cbHalfOpen:
		if c.halfOpenInflight < c.halfOpenMax {
			c.halfOpenInflight++
			return true
		}
		return false
	}
	return false
}

// RecordSuccess resets the breaker to CLOSED.
func (c *CircuitBreaker) RecordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = 0
	c.halfOpenInflight = 0
	c.state = cbClosed
}

// RecordFailure records a failure and may trip the breaker.
func (c *CircuitBreaker) RecordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.state {
	case cbClosed:
		c.failures++
		if c.failures >= c.threshold {
			c.state = cbOpen
			c.openedAt = c.now()
		}
	case cbHalfOpen:
		// trial failed → back to OPEN
		c.state = cbOpen
		c.openedAt = c.now()
		c.halfOpenInflight = 0
	}
}

// IsOpen reports whether the breaker is currently tripped (OPEN). Mostly for
// observability/tests.
func (c *CircuitBreaker) IsOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Reflect cooldown expiry so tests reading IsOpen see the transition.
	if c.state == cbOpen && c.now().Sub(c.openedAt) >= c.cooldown {
		return false
	}
	return c.state == cbOpen
}

// withClock injects a clock (tests).
func (c *CircuitBreaker) withClock(now func() time.Time) *CircuitBreaker {
	c.now = now
	return c
}
