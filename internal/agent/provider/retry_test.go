package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name       string
	typ        ProviderType
	completeFn func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error)
	streamFn   func(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error)
	healthFn   func(ctx context.Context) error
}

func (m *mockProvider) Type() ProviderType { return m.typ }
func (m *mockProvider) Name() string       { return m.name }
func (m *mockProvider) Complete(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
	return m.completeFn(ctx, req)
}
func (m *mockProvider) CompleteStream(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error) {
	return m.streamFn(ctx, req)
}
func (m *mockProvider) HealthCheck(ctx context.Context) error {
	if m.healthFn != nil {
		return m.healthFn(ctx)
	}
	return nil
}

func TestRetryProvider_SuccessFirstAttempt(t *testing.T) {
	calls := 0
	mock := &mockProvider{
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			calls++
			return &ProviderResponse{Content: TextContent("ok")}, nil
		},
		streamFn: func(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error) {
			calls++
			ch := make(chan StreamChunk, 1)
			ch <- StreamChunk{Done: true}
			return ch, nil
		},
	}
	rp := NewRetryProvider(mock, 3, time.Millisecond)

	// Complete — should succeed on first try
	resp, err := rp.Complete(context.Background(), ProviderRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}

	// CompleteStream — should succeed on first try
	prevCalls := calls
	ch, err := rp.CompleteStream(context.Background(), ProviderRequest{})
	if err != nil {
		t.Fatalf("unexpected stream error: %v", err)
	}
	<-ch
	if calls != prevCalls+1 {
		t.Fatalf("expected %d calls, got %d", prevCalls+1, calls)
	}
}

func TestRetryProvider_RetryThenSuccess(t *testing.T) {
	attempt := 0
	mock := &mockProvider{
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			attempt++
			if attempt < 3 {
				return nil, errors.New("503 service unavailable")
			}
			return &ProviderResponse{Content: TextContent("ok")}, nil
		},
	}
	rp := NewRetryProvider(mock, 5, time.Millisecond)

	resp, err := rp.Complete(context.Background(), ProviderRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if attempt != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempt)
	}
}

func TestRetryProvider_AllRetriesExhausted(t *testing.T) {
	mock := &mockProvider{
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			return nil, errors.New("503 service unavailable")
		},
	}
	rp := NewRetryProvider(mock, 2, time.Millisecond)

	_, err := rp.Complete(context.Background(), ProviderRequest{})
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}
}

func TestRetryProvider_NonRetryableError(t *testing.T) {
	calls := 0
	mock := &mockProvider{
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			calls++
			return nil, errors.New("400 bad request")
		},
	}
	rp := NewRetryProvider(mock, 3, time.Millisecond)

	_, err := rp.Complete(context.Background(), ProviderRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected only 1 call (non-retryable), got %d", calls)
	}
}

func TestRetryProvider_StreamRetryThenSuccess(t *testing.T) {
	attempt := 0
	mock := &mockProvider{
		streamFn: func(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error) {
			attempt++
			if attempt < 2 {
				return nil, errors.New("connection reset")
			}
			ch := make(chan StreamChunk, 1)
			ch <- StreamChunk{Done: true}
			return ch, nil
		},
	}
	rp := NewRetryProvider(mock, 3, time.Millisecond)

	ch, err := rp.CompleteStream(context.Background(), ProviderRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-ch
	if attempt != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempt)
	}
}

func TestRetryProvider_ContextCancellation(t *testing.T) {
	mock := &mockProvider{
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			return nil, errors.New("timeout")
		},
	}
	rp := NewRetryProvider(mock, 5, time.Hour) // long backoff so cancel triggers

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := rp.Complete(ctx, ProviderRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		err    error
		result bool
	}{
		{errors.New("429 too many requests"), true},
		{errors.New("500 internal error"), true},
		{errors.New("502 bad gateway"), true},
		{errors.New("503 service unavailable"), true},
		{errors.New("connection refused"), true},
		{errors.New("timeout exceeded"), true},
		{errors.New("EOF"), true},
		{errors.New("400 bad request"), false},
		{errors.New("401 unauthorized"), false},
		{errors.New("notimeoutmate"), true}, // "timeout" as substring
		{nil, false},
	}
	for _, tc := range tests {
		got := isRetryable(tc.err)
		if got != tc.result {
			t.Errorf("isRetryable(%v) = %v, want %v", tc.err, got, tc.result)
		}
	}
}

func TestIsPlaceholderKey(t *testing.T) {
	tests := []struct {
		key    string
		result bool
	}{
		{"", true},
		{"test-key", true},
		{"sk-placeholder", true},
		{"test-abc", true},
		{"sk-real-abc123", false},
		{"sk-ant-abcd1234", false},
	}
	for _, tc := range tests {
		got := IsPlaceholderKey(tc.key)
		if got != tc.result {
			t.Errorf("IsPlaceholderKey(%q) = %v, want %v", tc.key, got, tc.result)
		}
	}
}
