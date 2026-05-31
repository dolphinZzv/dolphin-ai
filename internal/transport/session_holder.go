package transport

import (
	"context"
	"sync"
	"time"

	"dolphin/internal/session"
	"github.com/rs/xid"
)

// SessionManager is the interface for creating sessions.
type SessionManager interface {
	NewSession(ctx context.Context) *session.Session
}

// SessionHolder provides NewSession/Session methods for transports.
// Embed *SessionHolder in a transport to satisfy the IO interface.
type SessionHolder struct {
	mgr     SessionManager
	current *session.Session
	mu      sync.Mutex
}

func NewSessionHolder(mgr SessionManager) *SessionHolder {
	return &SessionHolder{mgr: mgr}
}

func (h *SessionHolder) SetSessionManager(mgr SessionManager) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.mgr = mgr
}

func (h *SessionHolder) NewSession(ctx context.Context) *session.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current != nil {
		return h.current
	}
	if h.mgr != nil {
		h.current = h.mgr.NewSession(ctx)
	} else {
		h.current = &session.Session{
			ID:        xid.New().String(),
			Active:    false,
			CreatedAt: time.Now(),
		}
	}
	return h.current
}

func (h *SessionHolder) Session() *session.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current
}

func (h *SessionHolder) SetSession(s *session.Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.current = s
}

func (h *SessionHolder) ResetSession() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.current = nil
}
