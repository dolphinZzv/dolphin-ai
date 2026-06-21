// Package dump records and persists the last turn's LLM request/response
// and tool call data for each session, enabling /dump to write a structured
// JSON file under dump_dir/<session_id>.json.
package dump

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"dolphin/internal/types"
)

// TurnDump holds the complete data from one turn.
type TurnDump struct {
	SessionID    string             `json:"session_id"`
	ModelName    string             `json:"model"`
	Input        string             `json:"input"`
	SystemPrompt string             `json:"system_prompt,omitempty"`
	Messages     []types.Message    `json:"messages"`
	ToolResults  []types.ToolResult `json:"tool_results"`
	Timestamp    time.Time          `json:"timestamp"`
}

// Recorder stores the most recent turn dump per session in memory.
// It is safe for concurrent use.
type Recorder struct {
	mu   sync.Mutex
	last map[string]*TurnDump
	dir  string
}

// NewRecorder creates a Recorder that writes dumps to the given directory.
func NewRecorder(dir string) *Recorder {
	return &Recorder{last: make(map[string]*TurnDump), dir: dir}
}

// Record stores a dump for the given session.
func (r *Recorder) Record(d *TurnDump) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last[d.SessionID] = d
}

// Last returns the most recent dump for a session, or nil if none exists.
func (r *Recorder) Last(sessionID string) *TurnDump {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last[sessionID]
}

// Write persists the last dump for sessionID to r.dir/<sessionID>.json.
// Returns the written file path, or an error if no dump exists or IO fails.
func (r *Recorder) Write(sessionID string) (string, error) {
	r.mu.Lock()
	d, ok := r.last[sessionID]
	r.mu.Unlock()
	if !ok || d == nil {
		return "", fmt.Errorf("no dump available for session %s", sessionID)
	}

	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return "", fmt.Errorf("dump: mkdir %s: %w", r.dir, err)
	}

	path := filepath.Join(r.dir, sessionID+".json")
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", fmt.Errorf("dump: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("dump: write %s: %w", path, err)
	}
	return path, nil
}

// Dir returns the configured dump directory.
func (r *Recorder) Dir() string { return r.dir }
