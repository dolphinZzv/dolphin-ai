package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/session"

	"github.com/spf13/cobra"
)

func NewSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List and manage agent sessions",
		RunE:  runSessionsList,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show <id>",
		Short: "Show session details as a readable conversation",
		Args:  cobra.ExactArgs(1),
		RunE:  runSessionsShow,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "log <id>",
		Short: "Show raw session event log",
		Args:  cobra.ExactArgs(1),
		RunE:  runSessionsLog,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "rm <id>",
		Short: "Remove a session file",
		Args:  cobra.ExactArgs(1),
		RunE:  runSessionsRemove,
	})

	dumpCmd := &cobra.Command{
		Use:   "dump <id>",
		Short: "Generate Mermaid sequence diagram for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			server, _ := cmd.Flags().GetBool("server")
			open, _ := cmd.Flags().GetBool("open")
			return runSessionsDumpDo(args[0], server, open)
		},
	}
	dumpCmd.Flags().Bool("server", false, "serve diagram as a local web page")
	dumpCmd.Flags().Bool("open", false, "open diagram in browser after generating")
	cmd.AddCommand(dumpCmd)

	return cmd
}

func runSessionsList(cmd *cobra.Command, args []string) error {
	sessionDir := config.SessionsDir()

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No sessions found (directory does not exist)")
			return nil
		}
		return fmt.Errorf("read session directory: %w", err)
	}

	type sessInfo struct {
		id        string
		startedAt time.Time
		state     string
		path      string
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
			sessions = append(sessions, sessInfo{
				id:        sid,
				startedAt: info.ModTime(),
				state:     "completed",
				path:      filepath.Join(sessionDir, name),
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
		sessions = append(sessions, sessInfo{
			id:        sid,
			startedAt: info.ModTime(),
			state:     "active",
			path:      filepath.Join(sessionDir, name),
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].startedAt.After(sessions[j].startedAt)
	})

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	fmt.Printf("Sessions in: %s\n\n", sessionDir)
	for _, s := range sessions {
		age := time.Since(s.startedAt).Round(time.Second)
		fmt.Printf("  %-24s  %s  %s  %s\n", s.id[:min(len(s.id), 20)]+"...", s.startedAt.Format("2006-01-02 15:04"), s.state, age)
	}
	fmt.Println()
	return nil
}

func runSessionsShow(cmd *cobra.Command, args []string) error {
	sid := args[0]
	sessionDir := config.SessionsDir()

	eventsPath := filepath.Join(sessionDir, sid+".jsonl")
	events, err := session.ReadEvents(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %q not found", sid)
		}
		return fmt.Errorf("read session: %w", err)
	}

	if len(events) == 0 {
		fmt.Println("No events in session.")
		return nil
	}

	fmt.Printf("Session: %s\n", sid)
	fmt.Printf("Duration: %s — %s (%d events)\n",
		events[0].Timestamp.Format("2006-01-02 15:04:05"),
		events[len(events)-1].Timestamp.Format("15:04:05"),
		len(events),
	)
	fmt.Println()

	type turnEvents struct {
		turn   int
		events []session.SessionEvent
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
		fmt.Printf("--- Turn %d%s ---\n", t, tokStr)
		for _, evt := range te.events {
			switch evt.Type {
			case session.EventMessage:
				role := evt.Role
				if role == "" {
					role = "unknown"
				}
				content := strings.TrimSpace(string(evt.Content))
				if content != "" {
					fmt.Printf("  [%s] %s\n", role, content)
				}
			case session.EventToolCall:
				if evt.ToolName != "" {
					input := strings.TrimSpace(string(evt.ToolInput))
					if len(input) > 200 {
						input = input[:200] + "..."
					}
					fmt.Printf("  [tool] %s(%s)\n", evt.ToolName, input)
				}
			case session.EventToolResult:
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
				fmt.Printf("  [result%s] %s\n", isErr, result)
			case session.EventSystem:
				content := strings.TrimSpace(string(evt.Content))
				if content != "" {
					fmt.Printf("  [system] %s\n", content)
				}
			case session.EventSummary:
				content := strings.TrimSpace(string(evt.Content))
				if content != "" {
					fmt.Printf("  [summary] %s\n", content)
				}
			case session.EventCompression:
				content := strings.TrimSpace(string(evt.Content))
				if content != "" {
					fmt.Printf("  [compress] %s\n", content)
				}
			}
		}
		fmt.Println()
	}
	return nil
}

func runSessionsLog(cmd *cobra.Command, args []string) error {
	sid := args[0]
	sessionDir := config.SessionsDir()

	sumPath := filepath.Join(sessionDir, sid+"-summary.json")
	if data, err := os.ReadFile(sumPath); err == nil {
		fmt.Println(string(data))
		return nil
	}

	eventsPath := filepath.Join(sessionDir, sid+".jsonl")
	events, err := session.ReadEvents(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %q not found", sid)
		}
		return fmt.Errorf("read session: %w", err)
	}

	fmt.Printf("Session: %s\n", sid)
	fmt.Printf("Events:  %d\n", len(events))
	fmt.Println()
	for _, evt := range events {
		t := evt.Timestamp.Format("15:04:05")
		content := strings.TrimSpace(string(evt.Content))
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		fmt.Printf("  [%s] turn=%d %s", t, evt.Turn, evt.Type)
		if evt.Role != "" {
			fmt.Printf(" role=%s", evt.Role)
		}
		if evt.ToolName != "" {
			fmt.Printf(" tool=%s", evt.ToolName)
		}
		if content != "" {
			fmt.Printf(" %q", content)
		}
		fmt.Println()
	}
	return nil
}

func runSessionsRemove(cmd *cobra.Command, args []string) error {
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
	os.Remove(sumPath)

	fmt.Printf("Removed session %q\n", sid)
	return nil
}

func runSessionsDumpDo(sid string, serve, openBrowser bool) error {
	sessionDir := config.SessionsDir()

	eventsPath := filepath.Join(sessionDir, sid+".jsonl")
	events, err := session.ReadEvents(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %q not found", sid)
		}
		return fmt.Errorf("read session: %w", err)
	}

	if len(events) == 0 {
		return fmt.Errorf("no events in session")
	}

	participants := []string{"User", "LLM", "Agent"}
	participantSet := make(map[string]bool)
	participantSet["User"] = true
	participantSet["LLM"] = true
	participantSet["Agent"] = true

	var lines []string
	lines = append(lines, "sequenceDiagram")
	lines = append(lines, "    autoNumber")

	prevTurn := -1
	var turnStart time.Time
	var turnInTokens, turnOutTokens int
	for _, evt := range events {
		if evt.Turn != prevTurn {
			if !turnStart.IsZero() && prevTurn >= 0 {
				elapsed := evt.Timestamp.Sub(turnStart)
				note := fmt.Sprintf("Turn %d duration: %s", prevTurn, elapsed)
				if turnInTokens > 0 || turnOutTokens > 0 {
					note += fmt.Sprintf(", tokens: %d in / %d out", turnInTokens, turnOutTokens)
				}
				lines = append(lines, fmt.Sprintf("    Note over User,Agent: %s", note))
			}
			turnInTokens = 0
			turnOutTokens = 0
			turnStart = evt.Timestamp
			lines = append(lines, fmt.Sprintf("    Note over User,Agent: Turn %d @ %s", evt.Turn, evt.Timestamp.Format("15:04:05")))
			prevTurn = evt.Turn
		}
		turnInTokens += evt.InputTokens
		turnOutTokens += evt.OutputTokens

		switch evt.Type {
		case session.EventMessage:
			role := evt.Role
			if role == "" {
				continue
			}

			if role == "user" {
				text := extractTextContent(string(evt.Content))
				if text == "" {
					continue
				}
				text = truncate(text, 100)
				label := text
				if evt.DurationMs > 0 {
					label += fmt.Sprintf(" [%.0fms]", float64(evt.DurationMs))
				}
				lines = append(lines, fmt.Sprintf("    User->>Agent: %s", label))
			} else if role == "assistant" {
				text := extractTextContent(string(evt.Content))
				text = truncate(text, 100)
				label := text
				if evt.DurationMs > 0 {
					label += fmt.Sprintf(" [%.0fms]", float64(evt.DurationMs))
				}
				lines = append(lines, fmt.Sprintf("    Agent->>LLM: %s", label))
				lines = append(lines, fmt.Sprintf("    LLM-->>Agent: %s", label))
			}

		case session.EventToolCall:
			if evt.ToolName == "" {
				continue
			}
			if !participantSet[evt.ToolName] {
				participantSet[evt.ToolName] = true
				participants = append(participants, evt.ToolName)
			}
			input := extractTextContent(string(evt.ToolInput))
			if input == "" {
				input = "{}"
			}
			input = truncate(input, 60)
			label := fmt.Sprintf("%s(%s)", evt.ToolName, input)
			if evt.DurationMs > 0 {
				label += fmt.Sprintf(" [%.0fms]", float64(evt.DurationMs))
			}
			lines = append(lines, fmt.Sprintf("    Agent->>%s: %s", evt.ToolName, label))

		case session.EventToolResult:
			result := extractTextContent(string(evt.ToolResult))
			if result == "" {
				result = "(empty)"
			}
			result = truncate(result, 80)
			if evt.DurationMs > 0 {
				result += fmt.Sprintf(" [%.0fms]", float64(evt.DurationMs))
			}
			if evt.IsError {
				lines = append(lines, fmt.Sprintf("    %s-->>Agent: ERROR: %s", evt.ToolName, result))
			} else {
				lines = append(lines, fmt.Sprintf("    %s-->>Agent: %s", evt.ToolName, result))
			}
		}
	}

	lines = append(lines, "")
	lines = append(lines, "    participant User")
	lines = append(lines, "    participant Agent")
	lines = append(lines, "    participant LLM")
	for _, p := range participants {
		if p != "User" && p != "Agent" && p != "LLM" {
			lines = append(lines, fmt.Sprintf("    participant %s", p))
		}
	}

	diagram := strings.Join(lines, "\n")

	if serve {
		return serveDiagram(sid, diagram, openBrowser)
	}

	fmt.Println(diagram)
	return nil
}

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

	// Try flat structure: [{"type":"text","text":"..."}]
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

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func serveDiagram(sid, diagram string, openBrowser bool) error {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	addr := fmt.Sprintf("http://localhost:%d", port)

	if openBrowser {
		go func() {
			time.Sleep(500 * time.Millisecond)
			exec.Command("open", addr).Start()
		}()
	}

	fmt.Printf("Serving at %s\n", addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(htmlPage(sid, diagram)))
	})

	server := &http.Server{Handler: mux}
	go server.Serve(ln)

	fmt.Println("Press Ctrl+C to stop.")
	select {}
}

func htmlPage(sid, diagram string) string {
	return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Session ` + sid + `</title>
  <script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
  <style>
    body { margin: 0; padding: 20px; background: #1e1e1e; color: #d4d4d4; font-family: monospace; }
    h2 { color: #fff; }
    .mermaid { background: #2d2d2d; padding: 16px; border-radius: 8px; }
    pre { background: #2d2d2d; padding: 16px; border-radius: 8px; overflow-x: auto; }
  </style>
</head>
<body>
  <h2>Session Diagram — ` + sid + `</h2>
  <div class="mermaid">` + diagram + `</div>
  <h3>Raw Mermaid</h3>
  <pre>` + escapeHtml(diagram) + `</pre>
  <script>mermaid.initialize({ startOnLoad: true, theme: 'dark' });</script>
</body>
</html>`
}

func escapeHtml(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
