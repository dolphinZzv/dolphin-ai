package brain

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSubscriptionParseAndSerialize(t *testing.T) {
	raw := `---
name: test-sub
description: A test subscription
event_pattern: file.*
filters:
    path: "*.md"
enabled: true
created_at: 2026-01-01T00:00:00Z
updated_at: 2026-01-01T00:00:00Z
---
Notify me about markdown file changes.
`

	sub, err := parseSubscription(raw)
	if err != nil {
		t.Fatalf("parseSubscription failed: %v", err)
	}
	if sub.Name != "test-sub" {
		t.Errorf("expected name 'test-sub', got %q", sub.Name)
	}
	if sub.EventPattern != "file.*" {
		t.Errorf("expected event_pattern 'file.*', got %q", sub.EventPattern)
	}
	if !sub.Enabled {
		t.Error("expected enabled=true")
	}
	if sub.Filters.Path != "*.md" {
		t.Errorf("expected filter path '*.md', got %q", sub.Filters.Path)
	}
	if strings.TrimSpace(sub.Content) != "Notify me about markdown file changes." {
		t.Errorf("unexpected content: %q", sub.Content)
	}
}

func TestSubscriptionParseNoContent(t *testing.T) {
	raw := `---
name: no-content
event_pattern: llm.*
enabled: true
---
`
	sub, err := parseSubscription(raw)
	if err != nil {
		t.Fatalf("parseSubscription failed: %v", err)
	}
	if sub.Name != "no-content" {
		t.Errorf("expected name 'no-content', got %q", sub.Name)
	}
	if sub.Content != "" {
		t.Errorf("expected empty content, got %q", sub.Content)
	}
}

func TestSubscriptionParseMissingName(t *testing.T) {
	raw := `---
event_pattern: file.*
enabled: true
---
`
	_, err := parseSubscription(raw)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestSubscriptionParseMissingEventPattern(t *testing.T) {
	raw := `---
name: no-pattern
enabled: true
---
`
	_, err := parseSubscription(raw)
	if err == nil {
		t.Fatal("expected error for missing event_pattern")
	}
}

func TestSubscriptionParseInvalidFrontmatter(t *testing.T) {
	raw := `no delimiter here`
	_, err := parseSubscription(raw)
	if err == nil {
		t.Fatal("expected error for missing frontmatter delimiter")
	}
}

func TestSubscriptionSerializeAndParse(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := Subscription{
		Name:         "roundtrip",
		Description:  "Round trip test",
		EventPattern: "llm.emit",
		Filters:      SubscriptionFilter{Path: ""},
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
		Content:      "Handle the event",
	}

	data, err := serializeSubscription(sub)
	if err != nil {
		t.Fatalf("serializeSubscription failed: %v", err)
	}

	parsed, err := parseSubscription(data)
	if err != nil {
		t.Fatalf("parseSubscription after serialize failed: %v", err)
	}
	if parsed.Name != sub.Name {
		t.Errorf("name: expected %q, got %q", sub.Name, parsed.Name)
	}
	if parsed.EventPattern != sub.EventPattern {
		t.Errorf("event_pattern: expected %q, got %q", sub.EventPattern, parsed.EventPattern)
	}
	if parsed.Enabled != sub.Enabled {
		t.Errorf("enabled: expected %v, got %v", sub.Enabled, parsed.Enabled)
	}
	if strings.TrimSpace(parsed.Content) != sub.Content {
		t.Errorf("content: expected %q, got %q", sub.Content, parsed.Content)
	}
}

func TestWriteAndReadSubscription(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	sub := Subscription{
		Name:         "my-sub",
		Description:  "My subscription",
		EventPattern: "file.*",
		Filters:      SubscriptionFilter{Path: "docs/*"},
		Enabled:      true,
		Content:      "File changed!",
	}

	if err := WriteSubscription(ctx, b, sub); err != nil {
		t.Fatalf("WriteSubscription failed: %v", err)
	}

	got, err := ReadSubscription(ctx, b, "my-sub")
	if err != nil {
		t.Fatalf("ReadSubscription failed: %v", err)
	}

	if got.Name != "my-sub" {
		t.Errorf("expected name 'my-sub', got %q", got.Name)
	}
	if got.EventPattern != "file.*" {
		t.Errorf("expected event_pattern 'file.*', got %q", got.EventPattern)
	}
	if got.Filters.Path != "docs/*" {
		t.Errorf("expected filter path 'docs/*', got %q", got.Filters.Path)
	}
	if strings.TrimSpace(got.Content) != "File changed!" {
		t.Errorf("expected content 'File changed!', got %q", got.Content)
	}
}

func TestReadSubscriptionNotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	_, err := ReadSubscription(ctx, b, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent subscription")
	}
}

func TestReadSubscriptionEmptyName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	_, err := ReadSubscription(ctx, b, "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestWriteSubscriptionEmptyName(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	err := WriteSubscription(ctx, b, Subscription{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestListSubscriptions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create two subscriptions
	WriteSubscription(ctx, b, Subscription{Name: "sub-a", EventPattern: "file.*", Content: "a"})
	WriteSubscription(ctx, b, Subscription{Name: "sub-b", EventPattern: "llm.*", Content: "b"})

	subs, err := ListSubscriptions(ctx, b)
	if err != nil {
		t.Fatalf("ListSubscriptions failed: %v", err)
	}

	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}

	names := make(map[string]bool)
	for _, s := range subs {
		names[s.Name] = true
	}
	if !names["sub-a"] {
		t.Error("missing sub-a")
	}
	if !names["sub-b"] {
		t.Error("missing sub-b")
	}
}

func TestListSubscriptionsEmpty(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	subs, err := ListSubscriptions(ctx, b)
	if err != nil {
		t.Fatalf("ListSubscriptions failed: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(subs))
	}
}

func TestListSubscriptionsSkipsIndex(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create the subscriptions/index.md seed file
	b.Write(ctx, "subscriptions/index.md", "", "# Subscriptions\n\nIndex file for subscriptions directory.")

	WriteSubscription(ctx, b, Subscription{Name: "real-sub", EventPattern: "file.*", Content: "real"})

	subs, err := ListSubscriptions(ctx, b)
	if err != nil {
		t.Fatalf("ListSubscriptions failed: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("expected 1 subscription (index.md skipped), got %d", len(subs))
	}
	if subs[0].Name != "real-sub" {
		t.Errorf("expected 'real-sub', got %q", subs[0].Name)
	}
}

func TestDeleteSubscription(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	WriteSubscription(ctx, b, Subscription{Name: "to-delete", EventPattern: "file.*", Content: "bye"})

	if err := DeleteSubscription(ctx, b, "to-delete"); err != nil {
		t.Fatalf("DeleteSubscription failed: %v", err)
	}

	_, err := ReadSubscription(ctx, b, "to-delete")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteSubscriptionNotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	err := DeleteSubscription(ctx, b, "nonexistent")
	if err == nil {
		t.Fatal("expected error for deleting nonexistent subscription")
	}
}

func TestWriteSubscriptionUpdatesTimestamps(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(ctx); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	createdAt := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := Subscription{
		Name:         "timestamp-test",
		EventPattern: "file.*",
		CreatedAt:    createdAt,
		Content:      "test",
	}

	if err := WriteSubscription(ctx, b, sub); err != nil {
		t.Fatalf("WriteSubscription failed: %v", err)
	}

	got, err := ReadSubscription(ctx, b, "timestamp-test")
	if err != nil {
		t.Fatalf("ReadSubscription failed: %v", err)
	}

	// CreatedAt should be preserved from the original
	if !got.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt: expected %v, got %v", createdAt, got.CreatedAt)
	}

	// UpdatedAt should be set to a recent time
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}
