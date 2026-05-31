package command

import (
	"bytes"
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
}

func NewRegistry(sessMgr *session.Manager, sb *signal.Bus) *Registry {
	r := &Registry{
		sessMgr:   sessMgr,
		signalBus: sb,
		root:      &cobra.Command{Use: "dolphin"},
	}
	r.root.SilenceErrors = true
	r.root.SilenceUsage = true

	RegisterVersion(r)
	RegisterSession(r, sessMgr)

	return r
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
