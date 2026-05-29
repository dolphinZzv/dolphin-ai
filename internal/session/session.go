package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/xid"
)

// SessionID is a unique session identifier.
type SessionID = string

// Session represents a single conversation session.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionStore persists sessions.
type SessionStore interface {
	Save(ctx context.Context, session *Session) error
	Get(ctx context.Context, id string) (*Session, error)
	List(ctx context.Context) ([]*Session, error)
	Delete(ctx context.Context, id string) error
}

// Manager manages session lifecycle.
type Manager struct {
	store    SessionStore
	current  *Session
	mu       sync.Mutex
	onFliped func(ctx context.Context, sessionID string)
}

func NewManager(store SessionStore) *Manager {
	mgr := &Manager{store: store}
	return mgr
}

func (m *Manager) Create(ctx context.Context) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil {
		m.current.Active = false
		m.store.Save(ctx, m.current)
	}

	sess := &Session{
		ID:        xid.New().String(),
		Title:     "",
		Active:    true,
		CreatedAt: time.Now(),
	}
	m.current = sess
	m.store.Save(ctx, sess)
	return sess
}

// NewSession creates a session without changing the active session.
func (m *Manager) NewSession(ctx context.Context) *Session {
	sess := &Session{
		ID:        xid.New().String(),
		Title:     "",
		Active:    false,
		CreatedAt: time.Now(),
	}
	m.store.Save(ctx, sess)
	return sess
}

// LoadActive restores the last active session from the store, if any.
func (m *Manager) LoadActive(ctx context.Context) {
	sessions, err := m.store.List(ctx)
	if err != nil {
		return
	}
	for _, s := range sessions {
		if s.Active {
			m.mu.Lock()
			m.current = s
			m.mu.Unlock()
			return
		}
	}
}

func (m *Manager) Active() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

func (m *Manager) List(ctx context.Context) ([]*Session, error) {
	return m.store.List(ctx)
}

func (m *Manager) SwitchTo(ctx context.Context, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, err := m.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("session not found: %s", id)
	}

	if m.current != nil {
		m.current.Active = false
		m.store.Save(ctx, m.current)
	}

	sess.Active = true
	m.current = sess
	m.store.Save(ctx, sess)

	if m.onFliped != nil {
		m.onFliped(ctx, sess.ID)
	}
	return sess, nil
}

func (m *Manager) OnFliped(fn func(ctx context.Context, sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onFliped = fn
}
