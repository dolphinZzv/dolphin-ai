package limits

import (
	"context"
	"sync"
)

type ConcurrencyLimiter struct {
	maxRunning int
	running    int
	ch         chan struct{}
	mu         sync.Mutex
}

func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
	if max <= 0 {
		max = 1
	}
	ch := make(chan struct{}, max)
	for i := 0; i < max; i++ {
		ch <- struct{}{}
	}
	return &ConcurrencyLimiter{
		maxRunning: max,
		ch:         ch,
	}
}

func (cl *ConcurrencyLimiter) Acquire(ctx context.Context) error {
	select {
	case <-cl.ch:
		cl.mu.Lock()
		cl.running++
		cl.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (cl *ConcurrencyLimiter) Release() {
	cl.mu.Lock()
	cl.running--
	cl.mu.Unlock()
	cl.ch <- struct{}{}
}

func (cl *ConcurrencyLimiter) Current() int {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.running
}
