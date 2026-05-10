package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/xid"
)

type SessionID string

type EventType string

const (
	EventMessage    EventType = "message"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventSystem     EventType = "system"
	EventSummary    EventType = "summary"
)

type SessionEvent struct {
	Timestamp  time.Time       `json:"ts"`
	SessionID  SessionID       `json:"session_id"`
	ParentID   SessionID       `json:"parent_id,omitempty"`
	Type       EventType       `json:"type"`
	Turn       int             `json:"turn"`
	Role       string          `json:"role,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`
	ToolResult json.RawMessage `json:"tool_result,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`
}

type Session struct {
	ID        SessionID
	ParentID  SessionID
	file      *os.File
	encoder   *json.Encoder
	mu        sync.Mutex
	closed    bool
	StartedAt time.Time
	Turn      int
	MaxLoop   int
}

type Manager struct {
	dir      string
	mu       sync.Mutex
	sessions map[SessionID]*Session
}

func NewManager(dir string) *Manager {
	return &Manager{
		dir:      dir,
		sessions: make(map[SessionID]*Session),
	}
}

func (m *Manager) EnsureDir() error {
	return os.MkdirAll(m.dir, 0755)
}

func (m *Manager) NewSession(maxLoop int) (*Session, error) {
	return m.NewSessionWithParent(maxLoop, "")
}

func (m *Manager) NewSessionWithParent(maxLoop int, parentID SessionID) (*Session, error) {
	id := SessionID(xid.New().String())
	path := filepath.Join(m.dir, string(id)+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("create session file: %w", err)
	}

	sess := &Session{
		ID:        id,
		ParentID:  parentID,
		file:      f,
		encoder:   json.NewEncoder(f),
		StartedAt: time.Now(),
		MaxLoop:   maxLoop,
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	attrs := []any{"id", id, "path", path}
	if parentID != "" {
		attrs = append(attrs, "parent_id", parentID)
	}
	slog.Debug("session created", attrs...)

	if parentID != "" {
		sess.LogSystem(fmt.Sprintf("child session of %s", parentID))
	}
	return sess, nil
}

func (s *Session) LogEvent(evt SessionEvent) error {
	evt.Timestamp = time.Now()
	evt.SessionID = s.ID
	evt.Turn = s.Turn

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.encoder.Encode(evt)
}

func (s *Session) LogMessage(role string, content json.RawMessage) error {
	return s.LogEvent(SessionEvent{
		Type:    EventMessage,
		Role:    role,
		Content: content,
	})
}

func (s *Session) LogToolCall(name string, input json.RawMessage) error {
	return s.LogEvent(SessionEvent{
		Type:      EventToolCall,
		ToolName:  name,
		ToolInput: input,
	})
}

func (s *Session) LogToolResult(name string, result json.RawMessage, isErr bool) error {
	return s.LogEvent(SessionEvent{
		Type:       EventToolResult,
		ToolName:   name,
		ToolResult: result,
		IsError:    isErr,
	})
}

func (s *Session) LogSystem(msg string) error {
	return s.LogEvent(SessionEvent{
		Type:    EventSystem,
		Content: json.RawMessage(fmt.Sprintf(`"%s"`, msg)),
	})
}

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.file.Close()
}

func (m *Manager) Get(id SessionID) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) Remove(id SessionID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Close()
		delete(m.sessions, id)
	}
}

// LatestSession returns the ID, path, and last turn of the most recent session file.
// Returns ("", "", 0, nil) if no sessions exist.
func (m *Manager) LatestSession() (SessionID, string, int, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return "", "", 0, nil
	}

	var latestPath string
	var latestID SessionID
	var latestMod time.Time
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, "-summary.json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		mod := info.ModTime()
		if mod.After(latestMod) {
			latestMod = mod
			latestPath = filepath.Join(m.dir, name)
			latestID = SessionID(strings.TrimSuffix(name, ".jsonl"))
		}
	}

	if latestPath == "" {
		return "", "", 0, nil
	}

	// Count turns from the session file
	turns, err := countTurns(latestPath)
	if err != nil {
		return latestID, latestPath, 0, nil
	}
	return latestID, latestPath, turns, nil
}

// countTurns counts the number of turns in a session file by counting unique turn values.
func countTurns(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	maxTurn := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt SessionEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		if evt.Turn > maxTurn {
			maxTurn = evt.Turn
		}
	}
	return maxTurn, nil
}

// ReadEvents reads all session events from a session file.
func ReadEvents(path string) ([]SessionEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var events []SessionEvent
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt SessionEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			slog.Warn("session: skipping malformed event", "error", err)
			continue
		}
		events = append(events, evt)
	}
	return events, nil
}

func (m *Manager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		s.Close()
		delete(m.sessions, id)
	}
}

// StartReaper runs a background goroutine that periodically removes session files
// older than maxAge. Stop it by cancelling ctx.
func (m *Manager) StartReaper(ctx context.Context, maxAge time.Duration, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		slog.Info("session reaper started", "max_age", maxAge, "interval", interval)
		for {
			select {
			case <-ctx.Done():
				slog.Info("session reaper stopped")
				return
			case <-ticker.C:
				m.reapOldSessions(maxAge)
			}
		}
	}()
}

func (m *Manager) reapOldSessions(maxAge time.Duration) {
	// Build set of active session files under lock (P0#3)
	m.mu.Lock()
	activeFiles := make(map[string]bool, len(m.sessions))
	for _, s := range m.sessions {
		activeFiles[s.file.Name()] = true
	}
	m.mu.Unlock()

	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, "-summary.json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			path := filepath.Join(m.dir, name)
			if activeFiles[path] {
				continue
			}
			if err := os.Remove(path); err != nil {
				slog.Warn("reaper: failed to remove session file", "path", path, "error", err)
			} else {
				slog.Debug("reaper: removed old session file", "path", path, "age", now.Sub(info.ModTime()).Round(time.Second))
			}
		}
	}
}
