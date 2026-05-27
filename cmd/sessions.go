package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/i18n"
	"dolphin/internal/session"

	"github.com/spf13/cobra"
)

// NewSessionsCmd creates the sessions command tree for CLI use.
func NewSessionsCmd() *cobra.Command {
	cmd := session.SessionsCommand()

	// Replace dump subcommand with CLI-enhanced version (adds --server/--open)
	oldDump := findSubCommand(cmd, "dump")
	if oldDump != nil {
		cmd.RemoveCommand(oldDump)
	}
	dumpCmd := &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdSessionsDumpUse) + " [list|mermaid]",
		Short: i18n.TL(i18n.KeyCmdSessionsDumpShort),
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			server, _ := c.Flags().GetBool("server")
			open, _ := c.Flags().GetBool("open")
			if server || open {
				return runSessionsDumpDo(args[0], server, open)
			}
			// Delegate to the shared handler for normal dump
			return oldDump.RunE(c, args)
		},
	}
	dumpCmd.Flags().Bool("server", false, "serve diagram as a local web page")
	dumpCmd.Flags().Bool("open", false, "open diagram in browser after generating")
	cmd.AddCommand(dumpCmd)

	return cmd
}

// findSubCommand finds a subcommand by name.
func findSubCommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

// runSessionsDumpDo generates a Mermaid sequence diagram for the session and
// optionally serves it as a local web page.
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
		return fmt.Errorf("%s", i18n.TL(i18n.KeySessDumpNoEvents))
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
			_ = exec.Command("open", addr).Start()
		}()
	}

	fmt.Printf(i18n.TL(i18n.KeySessServing)+"\n", addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(htmlPage(sid, diagram)))
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(ln) }()

	fmt.Println(i18n.TL(i18n.KeySessStopHint))
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
  <pre>` + escapeHTML(diagram) + `</pre>
  <script>mermaid.initialize({ startOnLoad: true, theme: 'dark' });</script>
</body>
</html>`
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
