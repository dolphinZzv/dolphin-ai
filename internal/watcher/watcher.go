package watcher

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
// file.* events to the event bus. Patterns from .dolphinignore (in the
// watched directory) are used to skip matching files.
type Watcher struct {
	dir            string
	eventBus       *event.Bus
	interval       time.Duration
	state          map[string]time.Time // relPath -> last modification time
	mu             sync.Mutex
	stopped        chan struct{}
	running        bool
	ignorePatterns []string // glob patterns from .dolphinignore
}

// NewWatcher creates a new file watcher. dir is the directory to watch.
// interval controls the polling frequency (default 5s if zero).
// If dir contains a .dolphinignore file, its patterns are loaded for
// exclusion (one glob per line, # for comments).
func NewWatcher(dir string, eventBus *event.Bus, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	w := &Watcher{
		dir:      dir,
		eventBus: eventBus,
		interval: interval,
		state:    make(map[string]time.Time),
		stopped:  make(chan struct{}),
	}
	w.loadIgnorePatterns()
	return w
}

// loadIgnorePatterns loads built-in defaults and reads .dolphinignore
// if it exists in the watched directory.
func (w *Watcher) loadIgnorePatterns() {
	// Built-in defaults: always ignore .git and .dolphin directories.
	w.ignorePatterns = append(w.ignorePatterns, ".git/", ".dolphin/")

	data, err := os.ReadFile(filepath.Join(w.dir, ".dolphinignore"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		w.ignorePatterns = append(w.ignorePatterns, line)
	}
}

// isIgnored checks whether a relative path matches any ignore pattern.
// isDir should be true when rel refers to a directory.
// Lines starting with "!" negate (un-ignore) a previously matched pattern;
// the last matching pattern wins, like .gitignore semantics.
func (w *Watcher) isIgnored(rel string, isDir bool) bool {
	ignored := false
	for _, pattern := range w.ignorePatterns {
		negate := strings.HasPrefix(pattern, "!")
		pat := strings.TrimPrefix(pattern, "!")

		dirPat := strings.HasSuffix(pat, "/")
		cleanPat := strings.TrimSuffix(pat, "/")

		match := false
		if dirPat {
			if isDir {
				if ok, _ := filepath.Match(cleanPat, filepath.Base(rel)); ok {
					match = true
				}
			}
			if strings.HasPrefix(rel, cleanPat+"/") {
				match = true
			}
		} else {
			if ok, _ := filepath.Match(cleanPat, filepath.Base(rel)); ok {
				match = true
			}
			if ok, _ := filepath.Match(cleanPat, rel); ok {
				match = true
			}
		}

		if match {
			ignored = !negate
		}
	}
	return ignored
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
// publishes events for any changes. It also reloads .dolphinignore before
// each scan so changes take effect without restart.
func (w *Watcher) scan() {
	// Reload ignore patterns so .dolphinignore changes are live.
	w.ignorePatterns = nil
	w.loadIgnorePatterns()

	current := make(map[string]time.Time)

	filepath.Walk(w.dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}
		rel, err := filepath.Rel(w.dir, path)
		if err != nil {
			return nil
		}
		if w.isIgnored(rel, fi.IsDir()) {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.IsDir() {
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
