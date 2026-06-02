package brain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dolphin/internal/event"
)

// Watcher monitors a directory for file changes via polling and publishes
// file.* events to the event bus.
type Watcher struct {
	dir      string
	eventBus *event.Bus
	interval time.Duration
	state    map[string]time.Time // relPath -> last modification time
	mu       sync.Mutex
	stopped  chan struct{}
	running  bool
}

// NewWatcher creates a new file watcher. dir is the directory to watch.
// interval controls the polling frequency (default 5s if zero).
func NewWatcher(dir string, eventBus *event.Bus, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Watcher{
		dir:      dir,
		eventBus: eventBus,
		interval: interval,
		state:    make(map[string]time.Time),
		stopped:  make(chan struct{}),
	}
}

// Start begins polling for file changes in a background goroutine.
func (w *Watcher) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	w.scan()

	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				w.scan()
			case <-ctx.Done():
				w.Stop()
				return
			case <-w.stopped:
				return
			}
		}
	}()
}

// Stop stops polling.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	w.running = false
	select {
	case <-w.stopped:
	default:
		close(w.stopped)
	}
}

// Dir returns the watched directory path.
func (w *Watcher) Dir() string { return w.dir }

// scan compares current filesystem state against the last snapshot and
// publishes events for any changes.
func (w *Watcher) scan() {
	current := make(map[string]time.Time)

	filepath.Walk(w.dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}
		if fi.IsDir() {
			if fi.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(w.dir, path)
		if err != nil {
			return nil
		}
		current[rel] = fi.ModTime()
		return nil
	})

	w.mu.Lock()
	prev := w.state
	w.state = current
	w.mu.Unlock()

	// Detect new and modified files.
	for rel, modTime := range current {
		prevMod, seen := prev[rel]
		if !seen {
			w.publish(event.EventFileCreate, rel, modTime)
		} else if !prevMod.Equal(modTime) {
			w.publish(event.EventFileUpdate, rel, modTime)
		}
	}

	// Detect deleted files.
	for rel := range prev {
		if _, exists := current[rel]; !exists {
			w.publish(event.EventFileDelete, rel, time.Time{})
		}
	}
}

func (w *Watcher) publish(et event.Type, path string, modTime time.Time) {
	payload := map[string]any{"path": path}
	if !modTime.IsZero() {
		payload["changed_at"] = modTime.Format(time.RFC3339)
	}
	if ext := filepath.Ext(path); ext != "" {
		payload["ext"] = strings.ToLower(ext)
	}
	w.eventBus.Publish(context.Background(), event.Event{
		Type:      et,
		Timestamp: time.Now(),
		Payload:   payload,
	})
}
