package brain

import (
	"context"
	"strings"
	"testing"
)

func TestSeedSubscriptionsCreates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	SeedSubscriptions(ctx, b)

	// Verify watch-soul was created.
	sub, err := ReadSubscription(ctx, b, "watch-soul")
	if err != nil {
		t.Fatalf("ReadSubscription(watch-soul) failed: %v", err)
	}
	if sub.EventPattern != "file.*" {
		t.Errorf("expected event_pattern 'file.*', got %q", sub.EventPattern)
	}
	if !sub.Enabled {
		t.Error("expected watch-soul to be enabled")
	}
	if sub.Filters.Path != "SOUL.md" {
		t.Errorf("expected filter path 'SOUL.md', got %q", sub.Filters.Path)
	}
}

func TestSeedSubscriptionsIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	SeedSubscriptions(ctx, b)
	SeedSubscriptions(ctx, b)

	// Should still exist and only one copy.
	subs, err := ListSubscriptions(ctx, b)
	if err != nil {
		t.Fatalf("ListSubscriptions failed: %v", err)
	}
	count := 0
	for _, s := range subs {
		if s.Name == "watch-soul" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 watch-soul subscription, got %d", count)
	}
}

func TestSeedSubscriptionsAlreadyExists(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Pre-create with different content.
	WriteSubscription(ctx, b, Subscription{
		Name:         "watch-soul",
		EventPattern: "file.*",
		Content:      "custom content",
		Enabled:      true,
	})

	SeedSubscriptions(ctx, b)

	// Verify content wasn't overwritten.
	sub, err := ReadSubscription(ctx, b, "watch-soul")
	if err != nil {
		t.Fatalf("ReadSubscription failed: %v", err)
	}
	if strings.TrimSpace(sub.Content) != "custom content" {
		t.Errorf("expected existing content preserved, got %q", sub.Content)
	}
}
