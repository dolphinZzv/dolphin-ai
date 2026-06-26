package graph

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	rl := newRateLimiter(3, time.Minute)

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		if !rl.Allow("test-key") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be blocked
	if rl.Allow("test-key") {
		t.Fatal("4th request should be blocked")
	}
}

func TestRateLimiter_DifferentKeys(t *testing.T) {
	rl := newRateLimiter(1, time.Minute)

	if !rl.Allow("key-a") {
		t.Fatal("first key-a request should be allowed")
	}
	if !rl.Allow("key-b") {
		t.Fatal("first key-b request should be allowed after key-a")
	}
	// key-a should now be blocked
	if rl.Allow("key-a") {
		t.Fatal("second key-a request should be blocked")
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	rl := newRateLimiter(1, 50*time.Millisecond)

	if !rl.Allow("key") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("key") {
		t.Fatal("second request should be blocked within window")
	}

	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("key") {
		t.Fatal("request after window expiry should be allowed")
	}
}

func TestRateLimiter_ConcurrentSafe(t *testing.T) {
	rl := newRateLimiter(100, time.Minute)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rl.Allow("key")
		}()
	}
	wg.Wait()

	// After 50 concurrent requests, the limiter should have tracked them all
	// Next request should be allowed since we're under 100
	if !rl.Allow("key") {
		t.Fatal("request after concurrent load should still be allowed")
	}
}

func TestRateLimiter_EmptyKey(t *testing.T) {
	rl := newRateLimiter(2, time.Minute)

	if !rl.Allow("") {
		t.Fatal("empty key request should be allowed")
	}
	if !rl.Allow("") {
		t.Fatal("second empty key request should be allowed")
	}
	if rl.Allow("") {
		t.Fatal("third empty key request should be blocked")
	}
}

func TestRateLimiter_ZeroLimit(t *testing.T) {
	rl := newRateLimiter(0, time.Minute)

	if rl.Allow("key") {
		t.Fatal("request with zero limit should be blocked")
	}
}

func TestRateLimiter_PruneOldEntries(t *testing.T) {
	rl := newRateLimiter(3, 20*time.Millisecond)

	// Fill up
	rl.Allow("key")
	rl.Allow("key")
	rl.Allow("key")

	// Wait for window to pass
	time.Sleep(30 * time.Millisecond)

	// Should be allowed again (old entries pruned)
	if !rl.Allow("key") {
		t.Fatal("should be allowed after old entries expire")
	}
}
