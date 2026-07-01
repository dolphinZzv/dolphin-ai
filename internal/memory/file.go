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

func (m *FileMemory) Read(ctx context.Context, sessionID string, start, end int) ([]types.Message, error) {
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

	var msgs []types.Message
	switch v := raw.(type) {
	case []types.Message:
		msgs = v
	case []interface{}:
		var err error
		msgs, err = decodeMessages(v)
		if err != nil {
			return nil, err
		}
	default:
		return nil, nil
	}

	return sliceMessages(msgs, start, end), nil
}

// sliceMessages applies start/end slicing. Both 0 returns all.
// Negative start counts from the end. end <= 0 means to the end.
func sliceMessages(msgs []types.Message, start, end int) []types.Message {
	if start == 0 && end == 0 {
		return msgs
	}
	n := len(msgs)
	if n == 0 {
		return nil
	}
	if start < 0 {
		start = n + start
		if start < 0 {
			start = 0
		}
	}
	if end <= 0 || end > n {
		end = n
	}
	if start >= end {
		return nil
	}
	return msgs[start:end]
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
