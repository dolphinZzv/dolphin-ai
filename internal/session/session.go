package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rs/xid"
)

// SessionID is a unique session identifier.
type SessionID = string

// Session represents a single conversation session.
type Session struct {
	ID        string         `json:"id"`
	Active    bool           `json:"active"`
	CreatedAt time.Time      `json:"created_at"`
	Data      map[string]any `json:"data,omitempty"`
}

func (s *Session) Set(key string, value any) {
	if s.Data == nil {
		s.Data = make(map[string]any)
	}
	s.Data[key] = value
}

func (s *Session) Get(key string) any {
	if s.Data == nil {
		return nil
	}
	return s.Data[key]
}

// Manager manages session lifecycle.
// Sessions are kept in memory; .md files on disk are used only for listing
// sessions from previous runs.
type Manager struct {
	dir      string
	current  *Session
	known    map[string]*Session // sessions created this runtime
	mu       sync.Mutex
	onFliped func(ctx context.Context, sessionID string)
}

func NewManager(dir string) *Manager {
	return &Manager{dir: dir, known: make(map[string]*Session)}
}

func (m *Manager) Create(ctx context.Context) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil {
		m.current.Active = false
	}

	sess := &Session{
		ID:        xid.New().String(),
		Active:    true,
		CreatedAt: time.Now(),
	}
	m.current = sess
	m.known[sess.ID] = sess
	return sess
}

// NewSession creates a session without changing the active session.
func (m *Manager) NewSession(ctx context.Context) *Session {
	sess := &Session{
		ID:        xid.New().String(),
		Active:    false,
		CreatedAt: time.Now(),
	}
	m.known[sess.ID] = sess
	return sess
}

// LoadActive picks the most recently modified .md file as the active session.
func (m *Manager) LoadActive(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessions := m.listFilesLocked()
	if len(sessions) == 0 {
		return
	}
	m.current = sessions[len(sessions)-1]
}

func (m *Manager) Active() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

func (m *Manager) List(ctx context.Context) ([]*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listFilesLocked(), nil
}

func (m *Manager) SwitchTo(ctx context.Context, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check known (in-memory) sessions first.
	if found, ok := m.known[id]; ok {
		if m.current != nil {
			m.current.Active = false
		}
		found.Active = true
		m.current = found
		if m.onFliped != nil {
			m.onFliped(ctx, found.ID)
		}
		return found, nil
	}

	// Fall back to disk sessions.
	sessions := m.listFilesLocked()
	for _, s := range sessions {
		if s.ID == id {
			if m.current != nil {
				m.current.Active = false
			}
			s.Active = true
			m.current = s
			m.known[s.ID] = s
			if m.onFliped != nil {
				m.onFliped(ctx, s.ID)
			}
			return s, nil
		}
	}

	return nil, fmt.Errorf("session not found: %s", id)
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.known, id)
	return os.Remove(filepath.Join(m.dir, id+".md"))
}

func (m *Manager) OnFliped(fn func(ctx context.Context, sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onFliped = fn
}

// listFilesLocked scans .md files in dir and merges with known sessions.
// Caller must hold m.mu.
func (m *Manager) listFilesLocked() []*Session {
	byID := make(map[string]*Session)

	// Start with known sessions.
	for id, s := range m.known {
		byID[id] = s
	}

	// Add sessions from disk, potentially updating CreatedAt from file mod time.
	entries, err := os.ReadDir(m.dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".md" || e.Name() == "index.md" {
				continue
			}
			id := e.Name()[:len(e.Name())-3]
			if _, exists := byID[id]; !exists {
				byID[id] = &Session{ID: id, Active: false}
			}
			if info, err := e.Info(); err == nil {
				byID[id].CreatedAt = info.ModTime()
			}
		}
	}

	sessions := make([]*Session, 0, len(byID))
	for _, s := range byID {
		sessions = append(sessions, s)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})
	return sessions
}
