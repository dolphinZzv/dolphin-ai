package limits

import (
	"context"
	"testing"
	"time"

	"dolphin/internal/config"
)

func TestConcurrencyLimiter(t *testing.T) {
	cl := NewConcurrencyLimiter(3)

	ctx := context.Background()

	if n := cl.Current(); n != 0 {
		t.Errorf("expected 0, got %d", n)
	}

	for i := 0; i < 3; i++ {
		if err := cl.Acquire(ctx); err != nil {
			t.Errorf("Acquire %d failed: %v", i, err)
		}
	}

	if n := cl.Current(); n != 3 {
		t.Errorf("expected 3, got %d", n)
	}

	done := make(chan error, 1)
	go func() {
		done <- cl.Acquire(ctx)
	}()

	select {
	case err := <-done:
		t.Errorf("expected blocking, got error: %v", err)
	case <-time.After(10 * time.Millisecond):
	}

	cl.Release()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("expected immediate acquisition after release")
	}
}

func TestConcurrencyLimiterZeroMax(t *testing.T) {
	cl := NewConcurrencyLimiter(0)
	if cl.maxRunning != 1 {
		t.Errorf("expected 1, got %d", cl.maxRunning)
	}
}

func TestConcurrencyLimiterCancel(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	cl.Acquire(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cl.Acquire(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestTokenCounterReset(t *testing.T) {
	cfg := &config.LimitsConfig{}
	tc := NewTokenCounter(cfg)

	tc.RequestsDaily = 100
	tc.ResetLevel("daily")

	if tc.RequestsDaily != 0 {
		t.Errorf("expected 0, got %d", tc.RequestsDaily)
	}
}

func TestLimitError(t *testing.T) {
	err := &LimitError{
		Type:    "requests",
		Level:   "daily",
		Current: 1000,
		Max:     1000,
	}

	expected := "llm limit exceeded: requests daily (1000/1000)"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestIsLimitError(t *testing.T) {
	if IsLimitError(nil) {
		t.Errorf("expected false for nil")
	}

	err := &LimitError{Type: "requests"}
	if !IsLimitError(err) {
		t.Errorf("expected true for LimitError")
	}

	if IsLimitError(err) {
	} else {
		t.Errorf("IsLimitError should return true for LimitError")
	}
}

func TestUsageStatPercent(t *testing.T) {
	s := UsageStat{Current: 50, Max: 100}
	if p := s.Percent(); p != 50.0 {
		t.Errorf("expected 50.0, got %f", p)
	}

	s = UsageStat{Current: 0, Max: 0}
	if p := s.Percent(); p != 0 {
		t.Errorf("expected 0, got %f", p)
	}
}
