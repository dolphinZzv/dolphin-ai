package command

import (
	"bytes"
	"fmt"
	"strings"

	"dolphin/internal/agentio"
	"dolphin/internal/session"
	"dolphin/internal/signal"

	"github.com/spf13/cobra"
)

// Registry manages slash commands (/command) via cobra.
type Registry struct {
	sessMgr   *session.Manager
	signalBus *signal.Bus
	agentIO   *agentio.AgentIO
	root      *cobra.Command

	// LLM-managed command definitions
	llmCmds map[string]llmCmd
}

type llmCmd struct {
	Name        string
	Description string
	Prompt      string
}

func NewRegistry(sessMgr *session.Manager, sb *signal.Bus) *Registry {
	r := &Registry{
		sessMgr:   sessMgr,
		signalBus: sb,
		root:      &cobra.Command{Use: "dolphin"},
		llmCmds:   make(map[string]llmCmd),
	}
	r.root.SilenceErrors = true
	r.root.SilenceUsage = true

	r.registerBuiltins()
	return r
}

func (r *Registry) registerBuiltins() {
	r.root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("dolphin v2.0.0")
			return nil
		},
	})

	sessionCmd := &cobra.Command{
		Use:   "session",
		Short: "Manage sessions",
	}
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "new",
		Short: "Create a new session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sess := r.sessMgr.Create(cmd.Context())
			cmd.Printf("created session %s\n", sess.ID)
			return nil
		},
	})
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessions, _ := r.sessMgr.List(cmd.Context())
			if len(sessions) == 0 {
				cmd.Println("no sessions")
				return nil
			}
			for _, s := range sessions {
				active := ""
				if s.Active {
					active = " [active]"
				}
				title := s.Title
				if title == "" {
					title = "(untitled)"
				}
				cmd.Printf("  %s: %s%s\n", s.ID[:8], title, active)
			}
			return nil
		},
	})
	sessionCmd.AddCommand(&cobra.Command{
		Use:   "switch [id]",
		Short: "Switch to a session (deprecated: use /session new)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("use /session new to create and switch to a new session")
			return nil
		},
	})
	r.root.AddCommand(sessionCmd)
}

func (r *Registry) Register(cmd *cobra.Command) {
	r.root.AddCommand(cmd)
}

func (r *Registry) SetAgentIO(aio *agentio.AgentIO) {
	r.agentIO = aio
}

// Execute parses and runs a slash command line (without the leading "/").
// Returns the command output as a string.
func (r *Registry) Execute(line string) string {
	orig := r.root.OutOrStdout()
	defer r.root.SetOut(orig)

	var buf bytes.Buffer
	r.root.SetOut(&buf)
	r.root.SetArgs(strings.Fields(line))
	_ = r.root.Execute()
	return strings.TrimRight(buf.String(), "\n")
}

// RegisterCommandTool stores an LLM-managed command definition.
func (r *Registry) RegisterCommandTool(name, description, prompt string) error {
	if name == "" || prompt == "" {
		return fmt.Errorf("command name and prompt are required")
	}
	if _, exists := r.llmCmds[name]; exists {
		return fmt.Errorf("command %q already registered", name)
	}
	r.llmCmds[name] = llmCmd{
		Name:        name,
		Description: description,
		Prompt:      prompt,
	}
	return nil
}

// UnregisterCommandTool removes an LLM-managed command.
func (r *Registry) UnregisterCommandTool(name string) error {
	if _, exists := r.llmCmds[name]; !exists {
		return fmt.Errorf("command %q not found", name)
	}
	delete(r.llmCmds, name)
	return nil
}

// RegisterFromSkill registers commands from a skill definition.
func (r *Registry) RegisterFromSkill(skillName string, commands []string) {
	for _, cmd := range commands {
		r.llmCmds[cmd] = llmCmd{
			Name:        cmd,
			Description: fmt.Sprintf("from skill %s", skillName),
			Prompt:      fmt.Sprintf("skill: %s", skillName),
		}
	}
}

// UnregisterFromSkill removes commands from a skill.
func (r *Registry) UnregisterFromSkill(skillName string, commands []string) {
	for _, cmd := range commands {
		delete(r.llmCmds, cmd)
	}
}
