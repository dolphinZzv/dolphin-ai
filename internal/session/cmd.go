package session

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

// SessionsCommand returns the cobra command tree for session management.
// The command supports list/show/log/rm/dump subcommands and writes to
// cmd.OutOrStdout() so it works in both CLI and REPL contexts.
func SessionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSessionsUse),
		Short: i18n.TL(i18n.KeyCmdSessionsShort),
		RunE:  sessionsList,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSessionsShowUse),
		Short: i18n.TL(i18n.KeyCmdSessionsShowShort),
		Args:  cobra.ExactArgs(1),
		RunE:  sessionsShow,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSessionsLogUse),
		Short: i18n.TL(i18n.KeyCmdSessionsLogShort),
		Args:  cobra.ExactArgs(1),
		RunE:  sessionsLog,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSessionsRmUse),
		Short: i18n.TL(i18n.KeyCmdSessionsRmShort),
		Args:  cobra.ExactArgs(1),
		RunE:  sessionsRemove,
	})

	cmd.AddCommand(sessionsDumpCmd())

	return cmd
}

func sessionsList(cmd *cobra.Command, _ []string) error {
	sessionDir := config.SessionsDir()

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeySessNoDir))
			return nil
		}
		return fmt.Errorf("read session directory: %w", err)
	}

	type sessInfo struct {
		id        string
		startedAt time.Time
		state     string
		path      string
		turns     int
		inTokens  int
		outTokens int
	}

	var sessions []sessInfo
	summaryIDs := make(map[string]bool)

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, "-summary.json") {
			sid := strings.TrimSuffix(name, "-summary.json")
			summaryIDs[sid] = true
			info, err := entry.Info()
			if err != nil {
				continue
			}
			var inTok, outTok int
			if data, err := os.ReadFile(filepath.Join(sessionDir, name)); err == nil {
				var sum Summary
				if json.Unmarshal(data, &sum) == nil {
					inTok = sum.InputTokens
					outTok = sum.OutputTokens
				}
			}
			sessions = append(sessions, sessInfo{
				id:        sid,
				startedAt: info.ModTime(),
				state:     "completed",
				path:      filepath.Join(sessionDir, name),
				inTokens:  inTok,
				outTokens: outTok,
			})
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sid := strings.TrimSuffix(name, ".jsonl")
		if summaryIDs[sid] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		sessPath := filepath.Join(sessionDir, name)
		turns, _ := CountTurns(sessPath)
		inTok, outTok, _ := CountTokens(sessPath)
		sessions = append(sessions, sessInfo{
			id:        sid,
			startedAt: info.ModTime(),
			state:     "active",
			path:      sessPath,
			turns:     turns,
			inTokens:  inTok,
			outTokens: outTok,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].startedAt.After(sessions[j].startedAt)
	})

	if len(sessions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeySessNone))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySessDirLabel)+"\n\n", sessionDir)
	for _, s := range sessions {
		age := time.Since(s.startedAt).Round(time.Second)
		label := s.id
		if len(label) > 20 {
			label = label[:20] + "..."
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %-24s  %s  %s  %s  turns=%-3d  in=%d out=%d\n",
			label, s.startedAt.Format("2006-01-02 15:04"), s.state, age, s.turns, s.inTokens, s.outTokens)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

func sessionsShow(cmd *cobra.Command, args []string) error {
	sid := args[0]
	sessionDir := config.SessionsDir()

	eventsPath := filepath.Join(sessionDir, sid+".jsonl")
	events, err := ReadEvents(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %q not found", sid)
		}
		return fmt.Errorf("read session: %w", err)
	}

	if len(events) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.TL(i18n.KeySessNoEvents))
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, i18n.TL(i18n.KeySessHeader)+"\n", sid)
	fmt.Fprintf(out, "Duration: %s — %s (%d events)\n",
		events[0].Timestamp.Format("2006-01-02 15:04:05"),
		events[len(events)-1].Timestamp.Format("15:04:05"),
		len(events),
	)
	fmt.Fprintln(out)

	type turnEvents struct {
		turn   int
		events []SessionEvent
	}
	turnMap := make(map[int]*turnEvents)
	var turnOrder []int
	for _, evt := range events {
		t := evt.Turn
		if _, ok := turnMap[t]; !ok {
			turnMap[t] = &turnEvents{turn: t}
			turnOrder = append(turnOrder, t)
		}
		turnMap[t].events = append(turnMap[t].events, evt)
	}

	for _, t := range turnOrder {
		te := turnMap[t]
		var inTok, outTok int
		for _, evt := range te.events {
			inTok += evt.InputTokens
			outTok += evt.OutputTokens
		}
		tokStr := ""
		if inTok > 0 || outTok > 0 {
			tokStr = fmt.Sprintf(" (tokens: %d in / %d out)", inTok, outTok)
		}
		fmt.Fprintf(out, "--- Turn %d%s ---\n", t, tokStr)
		for _, evt := range te.events {
			switch evt.Type {
			case EventMessage:
				role := evt.Role
				if role == "" {
					role = "unknown"
				}
				content := strings.TrimSpace(string(evt.Content))
				if content != "" {
					fmt.Fprintf(out, "  [%s] %s\n", role, content)
				}
			case EventToolCall:
				if evt.ToolName != "" {
					input := strings.TrimSpace(string(evt.ToolInput))
					if len(input) > 200 {
						input = input[:200] + "..."
					}
					fmt.Fprintf(out, "  [tool] %s(%s)\n", evt.ToolName, input)
				}
			case EventToolResult:
				result := extractTextContent(string(evt.ToolResult))
				if result == "" {
					result = "(empty)"
				}
				if len(result) > 300 {
					result = result[:300] + "..."
				}
				isErr := ""
				if evt.IsError {
					isErr = " ERROR"
				}
				fmt.Fprintf(out, "  [result%s] %s\n", isErr, result)
			case EventSystem:
				content := strings.TrimSpace(string(evt.Content))
				if content != "" {
					fmt.Fprintf(out, "  [system] %s\n", content)
				}
			case EventSummary:
				content := strings.TrimSpace(string(evt.Content))
				if content != "" {
					fmt.Fprintf(out, "  [summary] %s\n", content)
				}
			case EventCompression:
				content := strings.TrimSpace(string(evt.Content))
				if content != "" {
					fmt.Fprintf(out, "  [compress] %s\n", content)
				}
			}
		}
		fmt.Fprintln(out)
	}
	return nil
}

func sessionsLog(cmd *cobra.Command, args []string) error {
	sid := args[0]
	sessionDir := config.SessionsDir()

	sumPath := filepath.Join(sessionDir, sid+"-summary.json")
	if data, err := os.ReadFile(sumPath); err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	eventsPath := filepath.Join(sessionDir, sid+".jsonl")
	events, err := ReadEvents(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %q not found", sid)
		}
		return fmt.Errorf("read session: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Session: %s\n", sid)
	fmt.Fprintf(out, "Events:  %d\n", len(events))
	fmt.Fprintln(out)
	for _, evt := range events {
		t := evt.Timestamp.Format("15:04:05")
		content := strings.TrimSpace(string(evt.Content))
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		fmt.Fprintf(out, "  [%s] turn=%d %s", t, evt.Turn, evt.Type)
		if evt.Role != "" {
			fmt.Fprintf(out, " role=%s", evt.Role)
		}
		if evt.ToolName != "" {
			fmt.Fprintf(out, " tool=%s", evt.ToolName)
		}
		if content != "" {
			fmt.Fprintf(out, " %q", content)
		}
		fmt.Fprintln(out)
	}
	return nil
}

func sessionsRemove(cmd *cobra.Command, args []string) error {
	sid := args[0]
	sessionDir := config.SessionsDir()

	remPath := filepath.Join(sessionDir, sid+".jsonl")
	if err := os.Remove(remPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %q not found", sid)
		}
		return fmt.Errorf("remove session: %w", err)
	}

	sumPath := filepath.Join(sessionDir, sid+"-summary.json")
	_ = os.Remove(sumPath)

	fmt.Fprintf(cmd.OutOrStdout(), i18n.TL(i18n.KeySessRemoved)+"\n", sid)
	return nil
}

func sessionsDumpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSessionsDumpUse) + " [list|mermaid]",
		Short: i18n.TL(i18n.KeyCmdSessionsDumpShort),
		Args:  cobra.RangeArgs(1, 2),
		RunE:  sessionsDump,
	}
}

func sessionsDump(cmd *cobra.Command, args []string) error {
	sid := args[0]
	format := "mermaid"
	if len(args) > 1 {
		format = args[1]
	}

	sessionDir := config.SessionsDir()
	eventsPath := filepath.Join(sessionDir, sid+".jsonl")
	events, err := ReadEvents(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %q not found", sid)
		}
		return fmt.Errorf("read session: %w", err)
	}

	if len(events) == 0 {
		return fmt.Errorf("%s", i18n.TL(i18n.KeySessDumpNoEvents))
	}

	out := cmd.OutOrStdout()
	switch format {
	case "list":
		dumpSessionList(out, events, sid)
	default:
		dumpSessionMermaid(out, events)
	}
	return nil
}

func dumpSessionList(out io.Writer, events []SessionEvent, id string) {
	fmt.Fprintf(out, "Session: %s (%d events)\n", id, len(events))
	for _, evt := range events {
		line := fmt.Sprintf("[T%d %s] %s", evt.Turn, evt.Timestamp.Format("15:04:05"), evt.Type)
		if evt.Role != "" {
			line += fmt.Sprintf(" (%s)", evt.Role)
		}
		if evt.ToolName != "" {
			line += fmt.Sprintf(" tool=%s", evt.ToolName)
		}
		if len(evt.Content) > 0 {
			content := string(evt.Content)
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			line += ": " + content
		}
		fmt.Fprintln(out, line)
	}
}

func dumpSessionMermaid(out io.Writer, events []SessionEvent) {
	fmt.Fprintln(out, "sequenceDiagram")
	fmt.Fprintln(out, "    participant User as User")
	fmt.Fprintln(out, "    participant Agent as Agent")
	toolNames := make(map[string]bool)
	for _, evt := range events {
		if evt.ToolName != "" {
			toolNames[evt.ToolName] = true
		}
	}
	for name := range toolNames {
		fmt.Fprintf(out, "    participant %s as %s\n", strings.ReplaceAll(name, "-", "_"), name)
	}
	fmt.Fprintln(out)
	var lastTurn int
	for _, evt := range events {
		if evt.Turn != lastTurn {
			fmt.Fprintf(out, "    Note over User,Agent: Turn %d\n", evt.Turn)
			lastTurn = evt.Turn
		}
		switch evt.Role {
		case "user":
			content := string(evt.Content)
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			fmt.Fprintf(out, "    User->>Agent: %s\n", content)
		case "assistant":
			if evt.ToolName != "" {
				toolName := strings.ReplaceAll(evt.ToolName, "-", "_")
				input := string(evt.ToolInput)
				if len(input) > 60 {
					input = input[:60] + "..."
				}
				fmt.Fprintf(out, "    Agent->>+%s: %s\n", toolName, input)
			} else {
				content := string(evt.Content)
				if len(content) > 80 {
					content = content[:80] + "..."
				}
				fmt.Fprintf(out, "    Agent-->>User: %s\n", content)
			}
		default:
			if evt.ToolName != "" && evt.Type == "tool_result" {
				toolName := strings.ReplaceAll(evt.ToolName, "-", "_")
				result := string(evt.Content)
				if len(result) > 80 {
					result = result[:80] + "..."
				}
				prefix := "-->>-"
				if evt.IsError {
					prefix = "-x->-"
				}
				fmt.Fprintf(out, "    %s%sAgent: %s\n", toolName, prefix, result)
			}
		}
	}
}

// extractTextContent tries to extract readable text from the nested JSON
// block structure used by LLM providers.
func extractTextContent(raw string) string {
	// Try nested structure: [{"type":"tool_result","content":[{"type":"text","text":"..."}]}]
	var nestedBlocks []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &nestedBlocks); err == nil {
		var parts []string
		for _, b := range nestedBlocks {
			for _, c := range b.Content {
				if c.Type == "text" && c.Text != "" {
					parts = append(parts, c.Text)
				}
			}
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}

	var msgBlocks []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	}
	if err := json.Unmarshal([]byte(raw), &msgBlocks); err != nil {
		return strings.TrimSpace(raw)
	}
	var parts []string
	for _, b := range msgBlocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}
