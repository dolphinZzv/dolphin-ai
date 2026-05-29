package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dolphin/internal/types"
)

const timeFormat = "2006-01-02 15:04:05"

type FileMemory struct {
	dir    string
	window int // 0 = all
	mu     sync.RWMutex
}

func NewFileMemory(dir string, window int) *FileMemory {
	os.MkdirAll(dir, 0755)
	return &FileMemory{dir: dir, window: window}
}

func (m *FileMemory) path(sessionID string) string {
	return filepath.Join(m.dir, sessionID+".md")
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

	msgs := parseMarkdown(string(data))
	if m.window > 0 && len(msgs) > m.window*2 {
		msgs = msgs[len(msgs)-m.window*2:]
	}
	return msgs, nil
}

func (m *FileMemory) Write(ctx context.Context, sessionID string, msg types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	line := formatMarkdown(msg)
	f, err := os.OpenFile(m.path(sessionID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

func formatMarkdown(msg types.Message) string {
	ts := msg.Timestamp.Format(timeFormat)
	switch msg.Role {
	case types.RoleUser:
		return fmt.Sprintf("## user (%s)\n%s", ts, msg.Content)
	case types.RoleAssistant:
		if msg.Thinking != "" || len(msg.ToolCalls) > 0 {
			raw, _ := json.Marshal(msg)
			return fmt.Sprintf("## assistant (%s)\n_raw: %s", ts, string(raw))
		}
		return fmt.Sprintf("## assistant (%s)\n%s", ts, msg.Content)
	case types.RoleTool:
		raw, _ := json.Marshal(msg)
		return fmt.Sprintf("## tool (%s)\n_raw: %s", ts, string(raw))
	case types.RoleSystem:
		return fmt.Sprintf("## system (%s)\n%s", ts, msg.Content)
	}
	return ""
}

func parseMarkdown(data string) []types.Message {
	var msgs []types.Message
	lines := strings.Split(data, "\n")

	var current types.Message
	inContent := false

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if inContent {
				msgs = append(msgs, current)
				current = types.Message{}
			}

			header := line[3:]
			roleEnd := strings.Index(header, " (")
			if roleEnd < 0 {
				continue
			}
			role := header[:roleEnd]
			timeStr := header[roleEnd+2 : len(header)-1]
			t, _ := time.Parse(timeFormat, timeStr)

			current.Role = types.Role(role)
			current.Timestamp = t
			inContent = true
		} else if inContent {
			if strings.HasPrefix(line, "_raw: ") {
				raw := strings.TrimPrefix(line, "_raw: ")
				json.Unmarshal([]byte(raw), &current)
				continue
			}
			if strings.HasPrefix(line, "_") && strings.HasSuffix(line, "_") && current.Role == types.RoleTool {
				current.ToolCallID = strings.Trim(line, "_")
				continue
			}
			if current.Content != "" {
				current.Content += "\n"
			}
			current.Content += line
		}
	}

	if inContent {
		msgs = append(msgs, current)
	}

	return msgs
}
