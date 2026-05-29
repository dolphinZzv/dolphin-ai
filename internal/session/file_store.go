package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type FileStore struct {
	dir string
	mu  sync.RWMutex
}

func NewFileStore(dir string) *FileStore {
	os.MkdirAll(dir, 0755)
	return &FileStore{dir: dir}
}

func (s *FileStore) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *FileStore) Save(ctx context.Context, session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(session.ID), data, 0644)
}

func (s *FileStore) Get(ctx context.Context, id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *FileStore) List(ctx context.Context) ([]*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []*Session
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
}

func (s *FileStore) Delete(ctx context.Context, id string) error {
	return os.Remove(s.path(id))
}
