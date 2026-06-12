package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"dolphin/internal/types"
)

type FileMemory struct {
	dir    string
	window int // 0 = all
	mu     sync.RWMutex
}

func NewFileMemory(dir string, window int) *FileMemory {
	_ = os.MkdirAll(dir, 0755)
	return &FileMemory{dir: dir, window: window}
}

func (m *FileMemory) path(sessionID string) string {
	return filepath.Join(m.dir, sessionID+".json")
}

func (m *FileMemory) Read(ctx context.Context, sessionID string) ([]types.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.path(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var msgs []types.Message
	if len(data) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, err
	}

	if m.window > 0 && len(msgs) > m.window*2 {
		msgs = msgs[len(msgs)-m.window*2:]
	}
	return msgs, nil
}

func (m *FileMemory) Write(ctx context.Context, sessionID string, msg types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.path(sessionID)

	var msgs []types.Message
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &msgs)
	}
	msgs = append(msgs, msg)

	out, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}
