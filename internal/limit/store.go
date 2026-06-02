package limit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store persists limit counters.
type Store interface {
	Get(key string) (int64, error)
	Increment(key string, delta int64) (int64, error)
	Reset(prefix string) error
	GetAll() (map[string]int64, error)
}

// ---------------------------------------------------------------------------
// MemoryStore — in-memory, for tests
// ---------------------------------------------------------------------------

type MemoryStore struct {
	mu     sync.Mutex
	data   map[string]int64
	resets map[string]time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data:   make(map[string]int64),
		resets: make(map[string]time.Time),
	}
}

func (s *MemoryStore) Get(key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key], nil
}

func (s *MemoryStore) Increment(key string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] += delta
	return s.data[key], nil
}

func (s *MemoryStore) Reset(prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.data {
		if hasPrefix(k, prefix) {
			delete(s.data, k)
		}
	}
	s.resets[prefix] = time.Now()
	return nil
}

func (s *MemoryStore) GetAll() (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, nil
}

// LastReset returns when the given prefix was last reset (for testing).
func (s *MemoryStore) LastReset(prefix string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resets[prefix]
}

// ---------------------------------------------------------------------------
// FileStore — JSON file, for production
// ---------------------------------------------------------------------------

type counterData struct {
	Counters  map[string]int64 `json:"counters"`
	LastReset string           `json:"last_reset"` // RFC3339
}

type FileStore struct {
	dir       string
	mu        sync.Mutex
	lastReset time.Time
}

func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	s := &FileStore{dir: dir}
	if err := s.load(); err != nil {
		// Start fresh if file doesn't exist yet.
		s.lastReset = time.Now()
	}
	return s, nil
}

func (s *FileStore) filePath() string {
	return filepath.Join(s.dir, "counters.json")
}

func (s *FileStore) load() error {
	data, err := os.ReadFile(s.filePath())
	if err != nil {
		return err
	}
	var cd counterData
	if err := json.Unmarshal(data, &cd); err != nil {
		return err
	}
	s.lastReset, _ = time.Parse(time.RFC3339, cd.LastReset)
	return nil
}

func (s *FileStore) writeFile(cd counterData) error {
	data, err := json.MarshalIndent(cd, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath())
}

func (s *FileStore) Get(key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cd, err := s.read()
	if err != nil {
		return 0, nil
	}
	return cd.Counters[key], nil
}

func (s *FileStore) read() (counterData, error) {
	data, err := os.ReadFile(s.filePath())
	if err != nil {
		return counterData{Counters: make(map[string]int64)}, nil
	}
	var cd counterData
	if err := json.Unmarshal(data, &cd); err != nil {
		// Corrupt file detected — back it up before returning empty.
		backup := s.filePath() + ".corrupt." + time.Now().Format("20060102T150405")
		os.Rename(s.filePath(), backup)
		return counterData{Counters: make(map[string]int64)}, nil
	}
	if cd.Counters == nil {
		cd.Counters = make(map[string]int64)
	}
	return cd, nil
}

func (s *FileStore) Increment(key string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cd, err := s.read()
	if err != nil {
		return 0, err
	}
	if cd.Counters == nil {
		cd.Counters = make(map[string]int64)
	}
	cd.Counters[key] += delta
	cd.LastReset = s.lastReset.Format(time.RFC3339)
	if err := s.writeFile(cd); err != nil {
		return 0, err
	}
	return cd.Counters[key], nil
}

func (s *FileStore) Reset(prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cd, err := s.read()
	if err != nil {
		return err
	}
	for k := range cd.Counters {
		if hasPrefix(k, prefix) {
			delete(cd.Counters, k)
		}
	}
	s.lastReset = time.Now()
	cd.LastReset = s.lastReset.Format(time.RFC3339)
	return s.writeFile(cd)
}

func (s *FileStore) GetAll() (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cd, err := s.read()
	if err != nil {
		return make(map[string]int64), nil
	}
	out := make(map[string]int64, len(cd.Counters))
	for k, v := range cd.Counters {
		out[k] = v
	}
	return out, nil
}

// LastReset returns the last reset timestamp.
func (s *FileStore) LastReset() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastReset
}

// SetLastReset updates the last reset timestamp (used on startup recovery).
func (s *FileStore) SetLastReset(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastReset = t
	cd, err := s.read()
	if err != nil {
		cd = counterData{Counters: make(map[string]int64)}
	}
	cd.LastReset = t.Format(time.RFC3339)
	return s.writeFile(cd)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func hasPrefix(s, prefix string) bool {
	if prefix == "" {
		return true
	}
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
