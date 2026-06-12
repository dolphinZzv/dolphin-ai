package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"dolphin/internal/types"

	"github.com/rs/xid"
)

// SessionID is a unique session identifier.
type SessionID = string

// Session represents a single conversation session.
type Session struct {
	ID        string          `json:"id"`
	Active    bool            `json:"active"`
	CreatedAt time.Time       `json:"created_at"`
	Data      map[string]any  `json:"data,omitempty"`
	Messages  []types.Message `json:"messages,omitempty"`

	saveFn func(*Session) // set by Manager for auto-save
}

func (s *Session) Set(key string, value any) {
	if s.Data == nil {
		s.Data = make(map[string]any)
	}
	s.Data[key] = value
	if s.saveFn != nil {
		s.saveFn(s)
	}
}

func (s *Session) Get(key string) any {
	if s.Data == nil {
		return nil
	}
	return s.Data[key]
}

// AppendMessages appends messages to the session and auto-saves.
func (s *Session) AppendMessages(msgs []types.Message) {
	s.Messages = append(s.Messages, msgs...)
	if s.saveFn != nil {
		s.saveFn(s)
	}
}

// Manager manages session lifecycle.
// Sessions are persisted as JSON files on disk and loaded on startup.
type Manager struct {
	dir      string
	current  *Session
	known    map[string]*Session // sessions created or loaded this runtime
	mu       sync.Mutex
	onFliped func(ctx context.Context, sessionID string)
}

func NewManager(dir string) *Manager {
	m := &Manager{dir: dir, known: make(map[string]*Session)}
	return m
}

func (m *Manager) Create(ctx context.Context) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil {
		m.current.Active = false
		m.saveSessionLocked(m.current)
	}

	sess := &Session{
		ID:        xid.New().String(),
		Active:    true,
		CreatedAt: time.Now(),
	}
	sess.saveFn = m.saveSession
	m.current = sess
	m.known[sess.ID] = sess
	m.saveSessionLocked(sess)
	return sess
}

// NewSession creates a session without changing the active session.
func (m *Manager) NewSession(ctx context.Context) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess := &Session{
		ID:        xid.New().String(),
		Active:    false,
		CreatedAt: time.Now(),
	}
	sess.saveFn = m.saveSession
	m.known[sess.ID] = sess
	m.saveSessionLocked(sess)
	return sess
}

// SaveActive persists the current active session to disk.
func (m *Manager) SaveActive() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil {
		m.saveSessionLocked(m.current)
	}
}

// LoadActive picks the most recently created session as the active session.
func (m *Manager) LoadActive(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessions := m.listFilesLocked()
	if len(sessions) == 0 {
		return
	}
	m.current = sessions[len(sessions)-1]
	m.known[m.current.ID] = m.current
}

func (m *Manager) Active() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.known[id]
}

func (m *Manager) List(ctx context.Context) ([]*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listFilesLocked(), nil
}

func (m *Manager) SwitchTo(ctx context.Context, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Save current session before switching.
	if m.current != nil {
		m.current.Active = false
		m.saveSessionLocked(m.current)
	}

	// Check known (in-memory) sessions first.
	if found, ok := m.known[id]; ok {
		found.Active = true
		m.current = found
		m.saveSessionLocked(found)
		if m.onFliped != nil {
			m.onFliped(ctx, found.ID)
		}
		return found, nil
	}

	// Try loading from disk.
	sess, err := m.loadSessionLocked(id)
	if err != nil {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	sess.Active = true
	sess.saveFn = m.saveSession
	m.current = sess
	m.known[sess.ID] = sess
	m.saveSessionLocked(sess)
	if m.onFliped != nil {
		m.onFliped(ctx, sess.ID)
	}
	return sess, nil
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.known, id)
	return os.Remove(filepath.Join(m.dir, id+".json"))
}

func (m *Manager) OnFliped(fn func(ctx context.Context, sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onFliped = fn
}

// ---------------------------------------------------------------------------
// JSON persistence
// ---------------------------------------------------------------------------

func (m *Manager) jsonPath(id string) string {
	return filepath.Join(m.dir, id+".json")
}

func (m *Manager) saveSession(sess *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveSessionLocked(sess)
}

func (m *Manager) saveSessionLocked(sess *Session) {
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(m.dir, 0755)
	_ = os.WriteFile(m.jsonPath(sess.ID), data, 0644)
}

func (m *Manager) loadSessionLocked(id string) (*Session, error) {
	data, err := os.ReadFile(m.jsonPath(id))
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// ---------------------------------------------------------------------------
// File listing (scans .json files)
// ---------------------------------------------------------------------------

// listFilesLocked scans .json files in dir and merges with known sessions.
// Caller must hold m.mu.
func (m *Manager) listFilesLocked() []*Session {
	byID := make(map[string]*Session)

	for id, s := range m.known {
		byID[id] = s
	}

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		sessions := make([]*Session, 0, len(byID))
		for _, s := range byID {
			sessions = append(sessions, s)
		}
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
		})
		return sessions
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		if _, exists := byID[id]; exists {
			continue
		}
		sess, err := m.loadSessionLocked(id)
		if err != nil {
			continue
		}
		byID[id] = sess
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
