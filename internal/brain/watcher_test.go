package brain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dolphin/internal/event"
)

func TestWatcherNew(t *testing.T) {
	bus := event.NewBus()
	w := NewWatcher(t.TempDir(), bus, 0)
	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
}

func TestWatcherStartStop(t *testing.T) {
	bus := event.NewBus()
	w := NewWatcher(t.TempDir(), bus, time.Hour)
	w.Start(context.Background())
	if !w.running {
		t.Error("expected running after Start")
	}

	// Start again — idempotent
	w.Start(context.Background())

	w.Stop()
	if w.running {
		t.Error("expected stopped after Stop")
	}

	w.Stop() // idempotent — should not panic
}

func TestWatcherDetectsNewFile(t *testing.T) {
	dir := t.TempDir()
	bus := event.NewBus()
	w := NewWatcher(dir, bus, 10*time.Millisecond)

	var (
		mu     sync.Mutex
		events []event.Event
	)
	bus.Subscribe(func(ctx context.Context, e event.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	w.Start(context.Background())
	defer w.Stop()

	// Write a new file and wait for the next scan
	time.Sleep(30 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "new.md"), []byte("hello"), 0644)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	createCount := 0
	for _, e := range events {
		if e.Type != event.EventFileCreate {
			continue
		}
		createCount++
		if p, ok := e.Payload["path"].(string); ok && p == "new.md" {
			if ext, ok := e.Payload["ext"].(string); !ok || ext != ".md" {
				t.Errorf("expected ext '.md', got %v", e.Payload["ext"])
			}
			mu.Unlock()
			return
		}
	}
	mu.Unlock()
	t.Errorf("expected EventFileCreate for 'new.md', got %d other events (createCount=%d)", len(events), createCount)
}

func TestWatcherDetectsUpdate(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "file.md")
	os.WriteFile(filePath, []byte("v1"), 0644)

	bus := event.NewBus()
	w := NewWatcher(dir, bus, 10*time.Millisecond)

	var (
		mu     sync.Mutex
		events []event.Event
	)
	bus.Subscribe(func(ctx context.Context, e event.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	w.Start(context.Background())
	defer w.Stop()

	// Wait for initial scan, then modify
	time.Sleep(30 * time.Millisecond)
	os.WriteFile(filePath, []byte("v2"), 0644)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	for _, e := range events {
		if e.Type == event.EventFileUpdate {
			return
		}
	}
	t.Errorf("expected EventFileUpdate, got events: %v", events)
}

func TestWatcherDetectsDelete(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "delete.md")
	os.WriteFile(filePath, []byte("bye"), 0644)

	bus := event.NewBus()
	w := NewWatcher(dir, bus, 10*time.Millisecond)

	var (
		mu     sync.Mutex
		events []event.Event
	)
	bus.Subscribe(func(ctx context.Context, e event.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	w.Start(context.Background())
	defer w.Stop()

	time.Sleep(30 * time.Millisecond)
	os.Remove(filePath)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	for _, e := range events {
		if e.Type == event.EventFileDelete {
			return
		}
	}
	t.Errorf("expected EventFileDelete, got events: %v", events)
}

func TestWatcherSkipsGit(t *testing.T) {
	dir := t.TempDir()
	// Create a .git directory with tracked content
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref"), 0644)

	bus := event.NewBus()
	w := NewWatcher(dir, bus, 10*time.Millisecond)

	var events []event.Event
	bus.Subscribe(func(ctx context.Context, e event.Event) {
		events = append(events, e)
	})

	w.Start(context.Background())
	defer w.Stop()

	time.Sleep(50 * time.Millisecond)

	// .git changes should not produce events
	for _, e := range events {
		if p, ok := e.Payload["path"].(string); ok {
			if strings.HasPrefix(p, ".git") {
				t.Errorf("unexpected event for .git path: %s", p)
			}
		}
	}
}

func TestWatcherDirProperty(t *testing.T) {
	dir := t.TempDir()
	bus := event.NewBus()
	w := NewWatcher(dir, bus, 0)
	if w.Dir() != dir {
		t.Errorf("expected dir %q, got %q", dir, w.Dir())
	}
}

func TestWatcherMultipleChanges(t *testing.T) {
	dir := t.TempDir()
	bus := event.NewBus()
	w := NewWatcher(dir, bus, 10*time.Millisecond)

	var events []event.Event
	bus.Subscribe(func(ctx context.Context, e event.Event) {
		events = append(events, e)
	})

	w.Start(context.Background())
	defer w.Stop()

	time.Sleep(30 * time.Millisecond)

	// Create two files
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("b"), 0644)

	time.Sleep(50 * time.Millisecond)

	createCount := 0
	for _, e := range events {
		if e.Type == event.EventFileCreate {
			createCount++
		}
	}
	if createCount != 2 {
		t.Errorf("expected 2 create events, got %d", createCount)
	}
}
