// Package session manages conversation session storage, retrieval, and lifecycle.
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/xid"
	"go.uber.org/zap"
)

type SessionID string

type EventType string

const (
	EventMessage     EventType = "message"
	EventToolCall    EventType = "tool_call"
	EventToolResult  EventType = "tool_result"
	EventSystem      EventType = "system"
	EventSummary     EventType = "summary"
	EventCompression EventType = "compression"
	EventAgentAction EventType = "agent_action"
)

type SessionEvent struct {
	Timestamp    time.Time       `json:"ts"`
	SessionID    SessionID       `json:"session_id"`
	ParentID     SessionID       `json:"parent_id,omitempty"`
	Type         EventType       `json:"type"`
	Turn         int             `json:"turn"`
	Role         string          `json:"role,omitempty"`
	Content      json.RawMessage `json:"content,omitempty"`
	ToolName     string          `json:"tool_name,omitempty"`
	ToolInput    json.RawMessage `json:"tool_input,omitempty"`
	ToolResult   json.RawMessage `json:"tool_result,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	DurationMs   int64           `json:"duration_ms,omitempty"`
	InputTokens  int             `json:"input_tokens,omitempty"`
	OutputTokens int             `json:"output_tokens,omitempty"`
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

// Dir returns the directory path for session files.
func (m *Manager) Dir() string { return m.dir }

func (m *Manager) EnsureDir() error {
	return os.MkdirAll(m.dir, 0700)
}

func (m *Manager) NewSession(maxLoop int) (*Session, error) {
	return m.NewSessionWithParent(maxLoop, "")
}

func (m *Manager) NewSessionWithParent(maxLoop int, parentID SessionID) (*Session, error) {
	id := SessionID(xid.New().String())
	path := filepath.Join(m.dir, string(id)+".jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
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
	zap.S().Debugw("session created", attrs...)

	if parentID != "" {
		if err := sess.LogSystem(fmt.Sprintf("child session of %s", parentID)); err != nil {
			zap.S().Warnw("log system failed", "error", err)
		}
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

// LogMessageWithUsage logs a message event with token usage.
func (s *Session) LogMessageWithUsage(role string, content json.RawMessage, inputTokens, outputTokens int) error {
	return s.LogEvent(SessionEvent{
		Type:         EventMessage,
		Role:         role,
		Content:      content,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
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

// CompressMeta holds metadata for a compression event.
type CompressMeta struct {
	Level        int    `json:"level"`
	CoveredCount int    `json:"covered_count"`
	Summary      string `json:"summary"`
	TokensSaved  int    `json:"tokens_saved,omitempty"`
}

func (s *Session) LogCompression(meta CompressMeta) error {
	encoded, _ := json.Marshal(meta)
	return s.LogEvent(SessionEvent{
		Type:    EventCompression,
		Content: json.RawMessage(encoded),
	})
}

func (s *Session) LogSystem(msg string) error {
	encoded, _ := json.Marshal(msg)
	return s.LogEvent(SessionEvent{
		Type:    EventSystem,
		Content: json.RawMessage(encoded),
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
		if err := s.Close(); err != nil {
			zap.S().Warnw("session close failed", "error", err)
		}
		delete(m.sessions, id)
	}
}

// LatestSession returns the ID, path, and last turn of the most recent session file.
// Returns ("", "", 0, nil) if no sessions exist.
func (m *Manager) LatestSession() (SessionID, string, int, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return "", "", 0, err
	}

	var latestPath string
	var latestID SessionID
	var latestMod time.Time
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, "-summary.json") {
			continue
		}
		//nolint:govet
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
	turns, err := CountTurns(latestPath)
	if err != nil {
		return latestID, latestPath, 0, err
	}
	return latestID, latestPath, turns, nil
}

// CountTurns counts the number of turns in a session file by finding the max turn value.
func CountTurns(path string) (int, error) {
	// Limit file size to 10MB to prevent OOM on large session files
	const maxSize = 10 * 1024 * 1024
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.Size() > maxSize {
		return 0, fmt.Errorf("session file too large (%d bytes), exceeds limit (%d)", info.Size(), maxSize)
	}
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

// CountTokens reads a session file and sums input/output token counts.
func CountTokens(path string) (inputTokens, outputTokens int, err error) {
	const maxSize = 10 * 1024 * 1024
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	if info.Size() > maxSize {
		return 0, 0, fmt.Errorf("session file too large (%d bytes), exceeds limit (%d)", info.Size(), maxSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt SessionEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		inputTokens += evt.InputTokens
		outputTokens += evt.OutputTokens
	}
	return inputTokens, outputTokens, nil
}

// ReadEvents reads all session events from a session file.
func ReadEvents(path string) ([]SessionEvent, error) {
	// Limit file size to 10MB to prevent OOM
	const maxSize = 10 * 1024 * 1024
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxSize {
		return nil, fmt.Errorf("session file too large (%d bytes), exceeds limit (%d)", info.Size(), maxSize)
	}
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
			zap.S().Warnw("session: skipping malformed event", "error", err)
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
		if err := s.Close(); err != nil {
			zap.S().Warnw("session close failed", "error", err)
		}
		delete(m.sessions, id)
	}
}

// StartReaper runs a background goroutine that periodically removes session files
// older than maxAge. Stop it by cancelling ctx.
func (m *Manager) StartReaper(ctx context.Context, maxAge time.Duration, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		zap.S().Infow("session reaper started", "max_age", maxAge, "interval", interval)
		for {
			select {
			case <-ctx.Done():
				zap.S().Infow("session reaper stopped")
				return
			case <-ticker.C:
				m.reapOldSessions(maxAge)
			}
		}
	}()
}

func (m *Manager) reapOldSessions(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

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
			// Skip files belonging to active sessions
			isActive := false
			for _, s := range m.sessions {
				if s.file.Name() == path {
					isActive = true
					break
				}
			}
			if isActive {
				continue
			}
			if err := os.Remove(path); err != nil {
				zap.S().Warnw("reaper: failed to remove session file", "path", path, "error", err)
			} else {
				zap.S().Debugw("reaper: removed old session file", "path", path, "age", now.Sub(info.ModTime()).Round(time.Second))
			}
		}
	}
}
