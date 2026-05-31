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
func (r *Registry) Execute(line string, renderMode string) string {
	orig := r.root.OutOrStdout()
	defer r.root.SetOut(orig)
	defer r.root.SetUsageTemplate(r.root.UsageTemplate())

	if renderMode == "markdown" {
		r.root.SetUsageTemplate(markdownUsageTemplate)
	} else {
		r.root.SetUsageTemplate(defaultUsageTemplate)
	}

	var buf bytes.Buffer
	r.root.SetOut(&buf)
	r.root.SetArgs(strings.Fields(line))
	_ = r.root.Execute()
	return strings.TrimRight(buf.String(), "\n")
}

const defaultUsageTemplate = `Usage: /{{.Use}}{{if .HasAvailableSubCommands}} [command]{{end}}

Available Commands:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

{{if .HasAvailableLocalFlags}}Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`

const markdownUsageTemplate = "## Usage\n\n`/{{.Use}}{{if .HasAvailableSubCommands}} [command]{{end}}`\n\n{{if .HasAvailableSubCommands}}**Available Commands:**{{range .Commands}}{{if .IsAvailableCommand}}\n- **`{{.Name}}`**: {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}\n\n**Flags:**\n```\n{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}\n```{{end}}\n"
