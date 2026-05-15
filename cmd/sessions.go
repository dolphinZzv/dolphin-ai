package cmd

import (
	"fmt"
	"os"
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
		Short: "Show session details",
		Args:  cobra.ExactArgs(1),
		RunE:  runSessionsShow,
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "rm <id>",
		Short: "Remove a session file",
		Args:  cobra.ExactArgs(1),
		RunE:  runSessionsRemove,
	})

	return cmd
}

func runSessionsList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sessionDir := cfg.Session.Dir

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
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sid := args[0]
	sessionDir := cfg.Session.Dir

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
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	sid := args[0]
	sessionDir := cfg.Session.Dir

	removed := 0
	for _, suffix := range []string{".jsonl", "-summary.json"} {
		path := filepath.Join(sessionDir, sid+suffix)
		if err := os.Remove(path); err == nil {
			fmt.Printf("removed: %s\n", path)
			removed++
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	if removed == 0 {
		return fmt.Errorf("session %q not found", sid)
	}
	return nil
}
