package registry

import (
	"strings"

	"dolphin/internal/transport"

	"github.com/spf13/cobra"
)

// consoleWriter wraps transport.UserIO as an io.Writer so cobra commands
// can write their output through the REPL's I/O channel.
type consoleWriter struct {
	io transport.UserIO
}

func (w *consoleWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(string(p), "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			_ = w.io.WriteLine(line)
		}
	}
	return len(p), nil
}

// ConsoleDispatcher routes REPL "/cmd ..." lines through the registry and
// executes the matching cobra command with output redirected to UserIO.
type ConsoleDispatcher struct {
	reg *Registry
}

// NewConsoleDispatcher creates a dispatcher backed by the given registry.
func NewConsoleDispatcher(reg *Registry) *ConsoleDispatcher {
	return &ConsoleDispatcher{reg: reg}
}

// Execute parses a "/cmd [sub] [args...]" line, finds the matching cobra
// command in the registry, redirects its output to io, and runs it.
// It returns true if the command was recognised along with any signal.
func (d *ConsoleDispatcher) Execute(line string, io transport.UserIO) (handled bool, signal ConsoleSignal) {
	if !strings.HasPrefix(line, "/") {
		return false, SignalNone
	}
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false, SignalNone
	}
	name := parts[0][1:] // strip leading "/"
	args := parts[1:]

	spec := d.reg.Get(name)
	if spec == nil {
		return false, SignalNone
	}
	if spec.Hidden {
		return false, SignalNone
	}
	signal = spec.Signal

	cmd := spec.Cobra
	// Redirect output on the root command so all subcommands (including
	// cobra's built-in help) inherit the redirected writer.
	cmd.SetOut(&consoleWriter{io: io})
	cmd.SetErr(&consoleWriter{io: io})

	sub, remArgs, err := cmd.Find(args)
	if err != nil || sub == nil {
		sub = cmd
		remArgs = args
	}

	sub.SilenceErrors = true
	sub.SilenceUsage = true

	// Check for explicit help request before running the handler.
	for _, a := range remArgs {
		if a == "--help" || a == "-h" {
			_ = sub.Help()
			return true, signal
		}
	}

	if sub.RunE != nil {
		if err := sub.RunE(sub, remArgs); err != nil {
			_ = io.WriteLine("Error: " + err.Error())
		}
	} else if sub.Run != nil {
		sub.Run(sub, remArgs)
	} else if sub.HasSubCommands() && len(remArgs) == 0 {
		// No handler but has subcommands → show usage.
		_ = sub.Usage()
	}

	return true, signal
}

// -- cobra adapter helpers ---------------------------------------------------

// AsCobraCommand adapts a *CommandSpec into a plain cobra.Command (without
// any wrapping). This is the identity for cobra commands already stored in
// the spec. For subsystem-provided specs it returns spec.Cobra unchanged.
func AsCobraCommand(spec *CommandSpec) *cobra.Command {
	return spec.Cobra
}
