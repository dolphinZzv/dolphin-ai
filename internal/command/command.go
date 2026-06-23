package command

import (
	"bytes"
	"context"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"dolphin/internal/agentio"
	"dolphin/internal/i18n"
	"dolphin/internal/session"
	"dolphin/internal/signal"
)

// renderModeKey is the context key for the output render mode ("none" or "markdown").
type renderModeKey struct{}

// RenderModeFrom returns the render mode from the command's context.
// Returns "none" if not set.
// RenderModeFrom returns the render mode from the command's context.
// Returns "none" if not set.
func RenderModeFrom(cmd *cobra.Command) string {
	if v := cmd.Context().Value(renderModeKey{}); v != nil {
		return v.(string)
	}
	return "none"
}

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
	execMu    sync.Mutex // guards SetContext+ExecuteC across transport goroutines
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
	// Register cobra's built-in help command eagerly so HasCommand("help")
	// works before Execute is called. InitDefaultHelpCmd only adds the help
	// subcommand once the root has other children, so call it after the
	// domain commands are registered.
	r.root.InitDefaultHelpCmd()

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

// Completions returns registered command paths that match the given prefix,
// ordered by prefix match priority for tab completion. Prefix can be empty
// (return all), or a path with optional leading "/" like "ses" or "/session".
func (r *Registry) Completions(prefix string) []string {
	// Normalize: strip leading "/" for cobra-internal path matching.
	prefix = strings.TrimPrefix(prefix, "/")
	var matches []string
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		path := cmd.CommandPath()
		// Skip the root "dolphin" prefix.
		path = strings.TrimPrefix(path, "dolphin ")
		if path != "dolphin" && path != "" && strings.HasPrefix(path, prefix) {
			if !slicesContains(matches, path) {
				matches = append(matches, "/"+path)
			}
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(r.root)
	// Dedup: exact prefix matches first.
	var result []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			result = append(result, m)
		}
	}
	return result
}

func slicesContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
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
	r.execMu.Lock()
	defer r.execMu.Unlock()

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
	fields := strings.Fields(line)
	r.root.SetArgs(fields)
	r.resolveI18nAnnotations()
	renderCtx := context.WithValue(ctx, renderModeKey{}, renderMode)
	r.root.SetContext(renderCtx)
	// Cobra only propagates root.ctx to subcommands when cmd.ctx is nil
	// (first call). Walk the tree to set ctx on every node so subsequent
	// calls also get the correct transport info.
	walkSetContext(r.root, renderCtx)
	_, _ = r.root.ExecuteC()
	return strings.TrimRight(buf.String(), "\n")
}

// walkSetContext recursively sets ctx on cmd and all its subcommands.
func walkSetContext(cmd *cobra.Command, ctx context.Context) {
	cmd.SetContext(ctx)
	for _, sub := range cmd.Commands() {
		walkSetContext(sub, ctx)
	}
}

const defaultUsageTemplate = `Usage: /{{.Use}}{{if .HasAvailableSubCommands}} [command]{{end}}

Available Commands:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}

{{if .HasAvailableLocalFlags}}Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`

const markdownUsageTemplate = "## Usage\n\n`/{{.Use}}{{if .HasAvailableSubCommands}} [command]{{end}}`\n\n{{if .HasAvailableSubCommands}}**Available Commands:**{{range .Commands}}{{if .IsAvailableCommand}}\n- **`{{.Name}}`**: {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}\n\n**Flags:**\n```\n{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}\n```{{end}}\n"
