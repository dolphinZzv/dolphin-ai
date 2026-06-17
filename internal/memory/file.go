package memory

import (
	"context"
	"encoding/json"
	"sync"

	"dolphin/internal/session"
	"dolphin/internal/types"
)

const memoryKey = "memory"

// SessionStore is the subset of session.Manager used by FileMemory.
type SessionStore interface {
	Get(id string) *session.Session
}

type FileMemory struct {
	sessions SessionStore
	mu       sync.RWMutex
}

func NewFileMemory(sessions SessionStore) *FileMemory {
	return &FileMemory{sessions: sessions}
}

func (m *FileMemory) Read(ctx context.Context, sessionID string) ([]types.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess := m.sessions.Get(sessionID)
	if sess == nil {
		return nil, nil
	}

	raw := sess.Get(memoryKey)
	if raw == nil {
		return nil, nil
	}

	// session.Data values survive JSON round-trips as []interface{}.
	switch v := raw.(type) {
	case []types.Message:
		return v, nil
	case []interface{}:
		return decodeMessages(v)
	default:
		return nil, nil
	}
}

func (m *FileMemory) Write(ctx context.Context, sessionID string, msg types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess := m.sessions.Get(sessionID)
	if sess == nil {
		return nil
	}

	var msgs []types.Message
	if raw := sess.Get(memoryKey); raw != nil {
		switch v := raw.(type) {
		case []types.Message:
			msgs = v
		case []interface{}:
			decoded, err := decodeMessages(v)
			if err != nil {
				return err
			}
			msgs = decoded
		}
	}

	msgs = append(msgs, msg)
	sess.Set(memoryKey, msgs)
	return nil
}

// Replace overwrites the entire message list for a session with msgs.
// Used by context compaction to atomically substitute a compacted
// [summary + tail] list for the prior full history.
func (m *FileMemory) Replace(ctx context.Context, sessionID string, msgs []types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess := m.sessions.Get(sessionID)
	if sess == nil {
		return nil
	}
	if msgs == nil {
		msgs = []types.Message{}
	}
	sess.Set(memoryKey, msgs)
	return nil
}

func decodeMessages(raw []interface{}) ([]types.Message, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var msgs []types.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}
