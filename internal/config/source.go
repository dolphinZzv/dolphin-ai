package config

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// Source is a config source that can be read and watched for changes.
type Source interface {
	Name() string
	Read() ([]byte, error)
	Watch(ctx context.Context, onChange func()) error
	Close() error
}

// FileSource watches a config file for changes via fsnotify, with a fallback
// to mtime-based polling for editors that use atomic saves (vim, etc.).
type FileSource struct {
	path    string
	watcher *fsnotify.Watcher

	// mtime poll fallback
	pollTick time.Duration
}

// NewFileSource creates a FileSource watching the given path.
func NewFileSource(path string) *FileSource {
	return &FileSource{
		path:     path,
		pollTick: 30 * time.Second,
	}
}

func (s *FileSource) Name() string { return "file:" + s.path }

func (s *FileSource) Read() ([]byte, error) {
	return os.ReadFile(filepath.Clean(s.path))
}

// Watch monitors the file for changes. It uses fsnotify for the primary
// watch and falls back to mtime polling every pollTick to catch edits from
// editors that use atomic saves (backup-and-replace pattern).
func (s *FileSource) Watch(ctx context.Context, onChange func()) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	s.watcher = w
	defer w.Close()

	// Watch the directory containing the file (fsnotify doesn't follow
	// renames of the watched file itself across atomic-save patterns).
	dir := filepath.Dir(s.path)
	if err := w.Add(dir); err != nil {
		zap.S().Warnw("file watch: cannot watch directory, falling back to poll", "dir", dir, "error", err)
		return s.pollLoop(ctx, onChange)
	}

	// Also watch the file directly if it exists.
	if fi, err := os.Stat(s.path); err == nil {
		if err := w.Add(s.path); err == nil {
			_ = fi
		}
	}

	// Debounce: coalesce rapid events with a reusable timer.
	const debounceDelay = 200 * time.Millisecond
	debounceTimer := time.NewTimer(debounceDelay)
	debounceTimer.Stop()
	defer debounceTimer.Stop()
	var debouncePending bool

	// mtime poll fallback ticker
	pollTicker := time.NewTicker(s.pollTick)
	defer pollTicker.Stop()

	// Track the last known modification time for poll-based detection.
	var lastMod time.Time
	if fi, err := os.Stat(s.path); err == nil {
		lastMod = fi.ModTime()
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			if event.Name != s.path {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Chmod) == 0 {
				continue
			}
			// Reset the debounce timer; first event starts it, subsequent
			// events within the window extend the window.
			if !debouncePending {
				debounceTimer.Reset(debounceDelay)
				debouncePending = true
			}

		case <-pollTicker.C:
			if fi, err := os.Stat(s.path); err == nil {
				if fi.ModTime().After(lastMod) {
					lastMod = fi.ModTime()
					onChange()
				}
			}

		case <-debounceTimer.C:
			debouncePending = false
			onChange()

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			zap.S().Warnw("file watch error", "path", s.path, "error", err)
		}
	}
}

// pollLoop is a fallback watcher that polls the file's mtime regularly.
// Used when fsnotify cannot watch the directory (e.g., permission denied).
func (s *FileSource) pollLoop(ctx context.Context, onChange func()) error {
	ticker := time.NewTicker(s.pollTick)
	defer ticker.Stop()

	var lastMod time.Time
	if fi, err := os.Stat(s.path); err == nil {
		lastMod = fi.ModTime()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if fi, err := os.Stat(s.path); err == nil {
				if fi.ModTime().After(lastMod) {
					lastMod = fi.ModTime()
					onChange()
				}
			}
		}
	}
}

func (s *FileSource) Close() error {
	if s.watcher != nil {
		return s.watcher.Close()
	}
	return nil
}
