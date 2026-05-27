package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"dolphin/internal/config"
)

func newFailoverProviderWithMocks(mocks ...*mockProvider) *FailoverProvider {
	cfgs := make([]config.ProviderConfig, len(mocks))
	providers := make([]Provider, len(mocks))
	for i, m := range mocks {
		cfgs[i] = config.ProviderConfig{Name: m.name, Model: "test-model"}
		providers[i] = NewRetryProvider(m, 2, time.Millisecond)
	}
	return &FailoverProvider{
		providers:  providers,
		configs:    cfgs,
		currentIdx: 0,
		hcTimeout:  time.Second,
	}
}

func TestFailoverProvider_Current(t *testing.T) {
	p1 := &mockProvider{name: "p1", typ: ProviderOpenAI}
	p2 := &mockProvider{name: "p2", typ: ProviderAnthropic}
	fp := newFailoverProviderWithMocks(p1, p2)

	if fp.Name() != "p1" {
		t.Fatalf("expected p1, got %s", fp.Name())
	}
	if fp.CurrentIndex() != 0 {
		t.Fatalf("expected index 0, got %d", fp.CurrentIndex())
	}
}

func TestFailoverProvider_SwitchToNext(t *testing.T) {
	p1 := &mockProvider{
		name: "p1",
		healthFn: func(ctx context.Context) error {
			return errors.New("unhealthy")
		},
	}
	p2 := &mockProvider{name: "p2"}
	fp := newFailoverProviderWithMocks(p1, p2)

	if !fp.SwitchToNext() {
		t.Fatal("expected SwitchToNext to succeed")
	}
	if fp.Name() != "p2" {
		t.Fatalf("expected p2, got %s", fp.Name())
	}
}

func TestFailoverProvider_SwitchToNextAllFail(t *testing.T) {
	p1 := &mockProvider{
		name: "p1",
		healthFn: func(ctx context.Context) error {
			return errors.New("unhealthy")
		},
	}
	p2 := &mockProvider{
		name: "p2",
		healthFn: func(ctx context.Context) error {
			return errors.New("unhealthy")
		},
	}
	fp := newFailoverProviderWithMocks(p1, p2)

	if fp.SwitchToNext() {
		t.Fatal("expected SwitchToNext to fail")
	}
}

func TestFailoverProvider_SwitchTo(t *testing.T) {
	p1 := &mockProvider{name: "p1"}
	p2 := &mockProvider{name: "p2"}
	fp := newFailoverProviderWithMocks(p1, p2)

	if !fp.SwitchTo("p2") {
		t.Fatal("expected SwitchTo to succeed")
	}
	if fp.Name() != "p2" {
		t.Fatalf("expected p2, got %s", fp.Name())
	}
}

func TestFailoverProvider_SwitchToNotFound(t *testing.T) {
	p1 := &mockProvider{name: "p1"}
	fp := newFailoverProviderWithMocks(p1)

	if fp.SwitchTo("nonexistent") {
		t.Fatal("expected SwitchTo to fail")
	}
}

func TestFailoverProvider_SwitchToUnhealthy(t *testing.T) {
	p1 := &mockProvider{name: "p1"}
	p2 := &mockProvider{
		name: "p2",
		healthFn: func(ctx context.Context) error {
			return errors.New("unhealthy")
		},
	}
	fp := newFailoverProviderWithMocks(p1, p2)

	if fp.SwitchTo("p2") {
		t.Fatal("expected SwitchTo p2 to fail")
	}
	// Should still be on p1
	if fp.Name() != "p1" {
		t.Fatalf("expected still on p1, got %s", fp.Name())
	}
}

func TestFailoverProvider_CompleteWithFailover(t *testing.T) {
	p1 := &mockProvider{
		name: "p1",
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			return nil, errors.New("503 service unavailable")
		},
	}
	p2 := &mockProvider{
		name: "p2",
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			return &ProviderResponse{Content: TextContent("ok from p2")}, nil
		},
	}
	fp := newFailoverProviderWithMocks(p1, p2)

	resp, err := fp.Complete(context.Background(), ProviderRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	// Should have failed over to p2
	if fp.Name() != "p2" {
		t.Fatalf("expected p2 active after failover, got %s", fp.Name())
	}
}

func TestFailoverProvider_CompleteAllFail(t *testing.T) {
	p1 := &mockProvider{
		name: "p1",
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			return nil, errors.New("503 service unavailable")
		},
	}
	p2 := &mockProvider{
		name: "p2",
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			return nil, errors.New("503 service unavailable")
		},
	}
	fp := newFailoverProviderWithMocks(p1, p2)

	_, err := fp.Complete(context.Background(), ProviderRequest{})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestFailoverProvider_CompleteNonRetryableError(t *testing.T) {
	calls := 0
	p1 := &mockProvider{
		name: "p1",
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			calls++
			return nil, errors.New("400 bad request")
		},
	}
	p2 := &mockProvider{
		name: "p2",
		completeFn: func(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
			calls++
			return &ProviderResponse{Content: TextContent("ok")}, nil
		},
	}
	fp := newFailoverProviderWithMocks(p1, p2)

	_, err := fp.Complete(context.Background(), ProviderRequest{})
	if err == nil {
		t.Fatal("expected error for non-retryable error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no failover for non-retryable), got %d", calls)
	}
}

func TestFailoverProvider_SelectProvider(t *testing.T) {
	p1 := &mockProvider{name: "p1"}
	p2 := &mockProvider{name: "p2"}
	fp := newFailoverProviderWithMocks(p1, p2)

	fp.SelectProvider(1)
	if fp.CurrentIndex() != 1 {
		t.Fatalf("expected index 1, got %d", fp.CurrentIndex())
	}
	if fp.Name() != "p2" {
		t.Fatalf("expected p2, got %s", fp.Name())
	}
}

func TestFailoverProvider_Configs(t *testing.T) {
	p1 := &mockProvider{name: "p1"}
	p2 := &mockProvider{name: "p2"}
	fp := newFailoverProviderWithMocks(p1, p2)

	cfgs := fp.Configs()
	if len(cfgs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(cfgs))
	}
	cc := fp.CurrentConfig()
	if cc.Name != "p1" {
		t.Fatalf("expected current config name p1, got %s", cc.Name)
	}
}
