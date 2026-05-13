package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

// Summary holds a session's summary data.
type Summary struct {
	SessionID        SessionID `json:"session_id"`
	Transport        string    `json:"transport,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	EndedAt          time.Time `json:"ended_at"`
	Turns            int       `json:"turns"`
	MaxLoop          int       `json:"max_loop"`
	ToolCallCount    int       `json:"tool_call_count"`
	ErrorCount       int       `json:"error_count"`
	CompressionCount int       `json:"compression_count"`
	State            string    `json:"state"`
	Summary          string    `json:"summary,omitempty"`
}

// GenerateSummary creates a summary from session events and writes it to a JSON file.
func (s *Session) GenerateSummary(dir string, toolCalls int, errors int, compressions int, state string, summary string) error {
	sum := Summary{
		SessionID:        s.ID,
		StartedAt:        s.StartedAt,
		EndedAt:          time.Now(),
		Turns:            s.Turn,
		MaxLoop:          s.MaxLoop,
		ToolCallCount:    toolCalls,
		ErrorCount:       errors,
		CompressionCount: compressions,
		State:            state,
		Summary:          summary,
	}

	data, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}

	path := filepath.Join(dir, string(s.ID)+"-summary.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}

	zap.S().Infow("session summary written",
		"session_id", s.ID,
		"path", path,
		"turns", s.Turn,
		"state", state,
	)
	return nil
}
