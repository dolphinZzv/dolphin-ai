package agentloop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	"dolphin/internal/signal"
	"dolphin/internal/types"
)

func TestIsRetryableLLMError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"429", &proto.HTTPStatusError{Status: http.StatusTooManyRequests}, true},
		{"500", &proto.HTTPStatusError{Status: http.StatusInternalServerError}, true},
		{"503", &proto.HTTPStatusError{Status: http.StatusServiceUnavailable}, true},
		{"400", &proto.HTTPStatusError{Status: http.StatusBadRequest}, false},
		{"401", &proto.HTTPStatusError{Status: http.StatusUnauthorized}, false},
		{"403", &proto.HTTPStatusError{Status: http.StatusForbidden}, false},
		{"404", &proto.HTTPStatusError{Status: http.StatusNotFound}, false},
		{"generic network error", errors.New("connection reset"), true},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"wrapped 429", fmt.Errorf("wrap: %w", &proto.HTTPStatusError{Status: 429}), true},
		{"wrapped 400", fmt.Errorf("wrap: %w", &proto.HTTPStatusError{Status: 400}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableLLMError(tt.err); got != tt.want {
				t.Errorf("isRetryableLLMError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestRetryDelay_HonorsRetryAfter(t *testing.T) {
	err := &proto.HTTPStatusError{Status: 429, RetryAfter: 2 * time.Second}
	if d := retryDelay(err, 0); d != 2*time.Second {
		t.Errorf("retryDelay with Retry-After 2s = %v, want 2s", d)
	}
}

func TestRetryDelay_RetryAfterCapped(t *testing.T) {
	err := &proto.HTTPStatusError{Status: 429, RetryAfter: 5 * time.Minute}
	if d := retryDelay(err, 0); d != maxBackoff {
		t.Errorf("retryDelay with huge Retry-After = %v, want %v (cap)", d, maxBackoff)
	}
}

func TestRetryDelay_ExponentialGrowth(t *testing.T) {
	err := errors.New("network error")
	d0 := retryDelay(err, 0)
	d1 := retryDelay(err, 1)
	d2 := retryDelay(err, 2)
	// Base grows 500ms, 1s, 2s (plus jitter). The jitter-free component
	// must be at least the base.
	if d0 < baseBackoff {
		t.Errorf("attempt 0: %v < base %v", d0, baseBackoff)
	}
	if d1 < 2*baseBackoff {
		t.Errorf("attempt 1: %v < 2*base %v", d1, 2*baseBackoff)
	}
	if d2 < 4*baseBackoff {
		t.Errorf("attempt 2: %v < 4*base %v", d2, 4*baseBackoff)
	}
}

func TestRetryDelay_Capped(t *testing.T) {
	err := errors.New("network error")
	// High attempt index would overflow; ensure capped at maxBackoff + jitter.
	d := retryDelay(err, 20)
	if d > maxBackoff+maxBackoff/4 {
		t.Errorf("retryDelay at high attempt = %v, want <= max+jitter", d)
	}
}

func TestBackoffSleep_NilSigCh(t *testing.T) {
	start := time.Now()
	if backoffSleep(nil, 50*time.Millisecond) {
		t.Error("nil sigCh should not report interrupted")
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("backoffSleep returned too fast: %v", elapsed)
	}
}

func TestBackoffSleep_Interrupted(t *testing.T) {
	sigBus := signal.NewBus()
	sigCh := sigBus.Subscribe("s")
	go func() {
		time.Sleep(20 * time.Millisecond)
		sigBus.Send("s", signal.Interrupt)
	}()
	start := time.Now()
	if !backoffSleep(sigCh, 5*time.Second) {
		t.Error("expected interrupt to abort backoff")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("interrupt should abort quickly, took %v", elapsed)
	}
}

func TestBackoffSleep_PauseThenResume(t *testing.T) {
	sigBus := signal.NewBus()
	sigCh := sigBus.Subscribe("s")
	go func() {
		time.Sleep(20 * time.Millisecond)
		sigBus.Send("s", signal.Pause)
		time.Sleep(30 * time.Millisecond)
		sigBus.Send("s", signal.Resume)
	}()
	// Short backoff; the Pause suspends it, Resume lets it proceed.
	// Should NOT report interrupted.
	start := time.Now()
	if backoffSleep(sigCh, 100*time.Millisecond) {
		t.Error("Pause/Resume should not report interrupted")
	}
	// Wall clock must reflect the pause: at least 20ms (to pause) + 30ms
	// (held) = 50ms even though backoff was 100ms... actually backoff is
	// 100ms so total is ~100ms. Just ensure it didn't abort early.
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("backoff returned too fast: %v", elapsed)
	}
}

// TestLLMStageProcess_429Backoff verifies that a 429 error triggers a backoff
// (not an immediate retry storm) and that retry eventually succeeds when the
// provider recovers.
func TestLLMStageProcess_429Backoff(t *testing.T) {
	logger := zap.NewNop()
	eb := event.NewBus()
	provider := &errThenSuccessProvider{
		good: []llm.LLMChunk{{Content: "ok", Done: true}},
		err:  &proto.HTTPStatusError{Status: http.StatusTooManyRequests, RetryAfter: 1 * time.Millisecond},
	}
	stage := &LLMStage{
		Provider:   provider,
		Model:      "m",
		MaxRetries: 3,
		EventBus:   eb,
		Logger:     logger,
	}
	state := &State{
		SessionID: "s-429",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
	}
	start := time.Now()
	err := stage.Process(context.Background(), state)
	if err != nil {
		t.Fatalf("expected success after backoff, got %v", err)
	}
	// Must have backed off at least once (attempt 0 failed, attempt 1
	// succeeded) — the RetryAfter of 1ms keeps the test fast but the
	// backoff machinery still ran.
	if elapsed := time.Since(start); elapsed < 1*time.Millisecond {
		t.Errorf("expected backoff to add delay, elapsed %v", elapsed)
	}
}

// errThenSuccessProvider fails on the first CompleteStream call with an
// injectable error, then succeeds. Unlike errorThenSuccessProvider (which
// always returns a generic network error), this one lets the test inject a
// specific error type (e.g. an HTTPStatusError) to exercise the retry
// classification path.
type errThenSuccessProvider struct {
	calls int32
	good  []llm.LLMChunk
	err   error
}

func (p *errThenSuccessProvider) Name() string { return "err-then-success" }
func (p *errThenSuccessProvider) Models(_ context.Context) ([]llm.ModelConfig, error) {
	return nil, nil
}
func (p *errThenSuccessProvider) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	if atomic.AddInt32(&p.calls, 1) == 1 {
		return nil, p.err
	}
	ch := make(chan llm.LLMChunk, len(p.good))
	for _, c := range p.good {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// TestLLMStageProcess_NonRetryable400 verifies that a 400 (non-retryable)
// error aborts immediately without consuming the retry budget.
func TestLLMStageProcess_NonRetryable400(t *testing.T) {
	logger := zap.NewNop()
	eb := event.NewBus()
	provider := &alwaysErrorProvider{
		err: &proto.HTTPStatusError{Status: http.StatusBadRequest},
	}
	stage := &LLMStage{
		Provider:   provider,
		Model:      "m",
		MaxRetries: 5,
		EventBus:   eb,
		Logger:     logger,
	}
	state := &State{
		SessionID: "s-400",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
	}
	start := time.Now()
	err := stage.Process(context.Background(), state)
	if err == nil {
		t.Fatal("expected error for 400")
	}
	var hse *proto.HTTPStatusError
	if !errors.As(err, &hse) || hse.Status != http.StatusBadRequest {
		t.Errorf("expected 400 HTTPStatusError, got %v", err)
	}
	// Must not have retried 5 times — non-retryable aborts on first failure.
	if calls := atomic.LoadInt32(&provider.calls); calls != 1 {
		t.Errorf("expected 1 call (no retries), got %d", calls)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("non-retryable should abort fast, took %v", elapsed)
	}
}

// alwaysErrorProvider always returns the same error.
type alwaysErrorProvider struct {
	err   error
	calls int32
}

func (p *alwaysErrorProvider) Name() string { return "always-error" }
func (p *alwaysErrorProvider) Models(_ context.Context) ([]llm.ModelConfig, error) {
	return nil, nil
}
func (p *alwaysErrorProvider) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	atomic.AddInt32(&p.calls, 1)
	return nil, p.err
}
