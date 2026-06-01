package command

import (
	"bytes"
	"context"
	"strings"

	"dolphin/internal/agentio"
	"dolphin/internal/i18n"
	"dolphin/internal/session"
	"dolphin/internal/signal"

	"github.com/spf13/cobra"
)

// annotationI18nShort is the annotation key for a dynamic i18n Short description.
const annotationI18nShort = "i18n_short"

// WithI18nShort marks a cobra command's Short field as dynamically resolved
// from the given i18n key. The Short text updates when the language changes.
func WithI18nShort(cmd *cobra.Command, key string) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations[annotationI18nShort] = key
	cmd.Short = i18n.T(key)
	return cmd
}

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

// HasCommand reports whether a command with the given name is registered in the root.
func (r *Registry) HasCommand(name string) bool {
	_, _, err := r.root.Find(strings.Fields(name))
	return err == nil
}

// resolveI18nAnnotations refreshes all dynamic i18n Short fields on registered commands.
func (r *Registry) resolveI18nAnnotations() {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if key, ok := cmd.Annotations[annotationI18nShort]; ok {
			cmd.Short = i18n.T(key)
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(r.root)
}

// Execute parses and runs a slash command line (without the leading "/").
// Returns the command output as a string.
func (r *Registry) Execute(ctx context.Context, line string, renderMode string) string {
	r.resolveI18nAnnotations()

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
	r.root.SetContext(ctx)
	_, _ = r.root.ExecuteC()
	return strings.TrimRight(buf.String(), "\n")
}

const defaultUsageTemplate = `Usage: /{{.Use}}{{if .HasAvailableSubCommands}} [command]{{end}}

Available Commands:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

{{if .HasAvailableLocalFlags}}Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`

const markdownUsageTemplate = "## Usage\n\n`/{{.Use}}{{if .HasAvailableSubCommands}} [command]{{end}}`\n\n{{if .HasAvailableSubCommands}}**Available Commands:**{{range .Commands}}{{if .IsAvailableCommand}}\n- **`{{.Name}}`**: {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}\n\n**Flags:**\n```\n{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}\n```{{end}}\n"
