// Package console provides a lightweight command router for agent console commands.
package console

import (
	"fmt"
	"strings"

	"dolphin/internal/transport"
)

// Handler processes a console command with parsed arguments.
type Handler func(args []string, io transport.UserIO)

// Command represents a console command with optional subcommands.
type Command struct {
	Name     string
	Desc     string
	Handler  Handler
	Children []*Command
}

// Console routes "/cmd [sub] [args...]" lines to registered handlers.
type Console struct {
	roots map[string]*Command
}

// New creates an empty console.
func New() *Console {
	return &Console{roots: make(map[string]*Command)}
}

// Add registers a root-level command.
func (c *Console) Add(cmd *Command) {
	c.roots[cmd.Name] = cmd
}

// Execute parses a "/cmd [sub] [args...]" line and dispatches to the matching
// handler. Returns true if the command was recognized and handled.
func (c *Console) Execute(line string, io transport.UserIO) bool {
	if !strings.HasPrefix(line, "/") {
		return false
	}
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false
	}
	name := parts[0][1:] // strip leading "/"
	args := parts[1:]

	cmd, ok := c.roots[name]
	if !ok {
		return false
	}

	// Walk subcommands
	for len(args) > 0 && len(cmd.Children) > 0 {
		found := false
		for _, child := range cmd.Children {
			if child.Name == args[0] {
				cmd = child
				args = args[1:]
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	// If the current command has children but user typed an unknown subcommand
	if len(args) > 0 && len(cmd.Children) > 0 {
		_ = io.WriteLine(fmt.Sprintf("Unknown subcommand: /%s %s", name, args[0]))
		_ = io.WriteLine("Available: " + joinNames(cmd.Children))
		return true
	}

	if cmd.Handler != nil {
		cmd.Handler(args, io)
		return true
	}

	// No handler but has children → list them
	if len(cmd.Children) > 0 {
		printSubcommands(cmd, name, io)
		return true
	}

	return false
}

func joinNames(cmds []*Command) string {
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

func printSubcommands(cmd *Command, path string, io transport.UserIO) {
	io.WriteLine(fmt.Sprintf("Usage: /%s <command>", path))
	for _, child := range cmd.Children {
		desc := child.Desc
		if desc == "" {
			desc = child.Name
		}
		io.WriteLine(fmt.Sprintf("  %s  %s", child.Name, desc))
	}
}
