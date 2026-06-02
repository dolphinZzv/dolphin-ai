package limit

import (
	"testing"
	"time"
)

func TestNewResetSchedulerInvalidExpr(t *testing.T) {
	if _, err := NewResetScheduler("not-a-cron", NewMemoryStore(), time.Time{}, newTestLogger(t), nil); err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestNewResetSchedulerRunsMissedReset(t *testing.T) {
	store := NewMemoryStore()
	store.Increment("llm.total_tokens", 100)
	store.Increment("llm.requests", 5)
	lastReset := time.Now().Add(-25 * time.Hour)
	rs, err := NewResetScheduler("0 0 * * *", store, lastReset, newTestLogger(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Stop()

	if v, _ := store.Get("llm.requests"); v != 0 {
		t.Fatalf("requests should be cleared by missed reset, got %d", v)
	}
	if v, _ := store.Get("llm.total_tokens"); v != 100 {
		t.Fatalf("total tokens should be preserved across reset, got %d", v)
	}
}

func TestNewResetSchedulerNoMissedReset(t *testing.T) {
	store := NewMemoryStore()
	store.Increment("llm.requests", 5)
	lastReset := time.Now().Add(-1 * time.Hour)
	rs, err := NewResetScheduler("0 0 * * *", store, lastReset, newTestLogger(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Stop()
	if v, _ := store.Get("llm.requests"); v != 5 {
		t.Fatalf("expected counters intact when no missed reset, got %d", v)
	}
}

func TestNewResetSchedulerZeroLastReset(t *testing.T) {
	store := NewMemoryStore()
	store.Increment("llm.requests", 5)
	rs, err := NewResetScheduler("0 0 * * *", store, time.Time{}, newTestLogger(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Stop()
	if v, _ := store.Get("llm.requests"); v != 5 {
		t.Fatalf("expected counters intact on zero lastReset, got %d", v)
	}
}

func TestResetSchedulerStop(t *testing.T) {
	rs, err := NewResetScheduler("0 0 * * *", NewMemoryStore(), time.Time{}, newTestLogger(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	rs.Stop()
}

func TestResetSchedulerOnResetNilSafe(t *testing.T) {
	store := NewMemoryStore()
	store.Increment("llm.total_tokens", 10)
	rs, err := NewResetScheduler("0 0 * * *", store, time.Now().Add(-25*time.Hour), newTestLogger(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	rs.OnReset = nil
	defer rs.Stop()
}

func TestNextResetAfter(t *testing.T) {
	now := time.Date(2025, 6, 2, 10, 0, 0, 0, time.UTC)
	got := nextResetAfter("0 0 * * *", now)
	if got.IsZero() {
		t.Fatal("expected non-zero next reset")
	}
	if !got.After(now) {
		t.Fatalf("next reset must be after now, got %v", got)
	}
}

func TestNextResetAfterInvalid(t *testing.T) {
	if got := nextResetAfter("not-a-cron", time.Now()); !got.IsZero() {
		t.Fatalf("expected zero time for invalid expression, got %v", got)
	}
}
