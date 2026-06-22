package progress

import (
	"context"
	"testing"
)

type mockFeeder struct {
	called bool
}

func (m *mockFeeder) Feed() {
	m.called = true
}

func TestWith(t *testing.T) {
	feeder := &mockFeeder{}
	ctx := With(context.Background(), feeder)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	// Extract feeder and verify
	f := From(ctx)
	if f == nil {
		t.Fatal("expected non-nil feeder from context")
	}

	// Feed should call the mock
	f.Feed()
	if !feeder.called {
		t.Error("expected feeder.Feed() to be called")
	}
}

func TestWithNilFeeder(t *testing.T) {
	parent := context.Background()
	ctx := With(parent, nil)
	if ctx != parent {
		t.Error("expected same context when feeder is nil")
	}
}

func TestFromNoFeeder(t *testing.T) {
	f := From(context.Background())
	if f != nil {
		t.Error("expected nil when no feeder attached")
	}
}

func TestFeed(t *testing.T) {
	feeder := &mockFeeder{}
	ctx := With(context.Background(), feeder)

	Feed(ctx)
	if !feeder.called {
		t.Error("expected feeder.Feed() to be called via Feed()")
	}
}

func TestFeedNoFeeder(t *testing.T) {
	// Should not panic
	Feed(context.Background())
}

func TestFeedNilFeeder(t *testing.T) {
	ctx := With(context.Background(), nil)
	// Should not panic
	Feed(ctx)
}

func TestFeedNilContext(t *testing.T) {
	// Should not panic
	Feed(nil)
}

func TestWithPreservesParentValues(t *testing.T) {
	type key struct{}
	parent := context.WithValue(context.Background(), key{}, "value")
	ctx := With(parent, &mockFeeder{})

	if v := ctx.Value(key{}); v != "value" {
		t.Errorf("expected parent value preserved, got %v", v)
	}
}
