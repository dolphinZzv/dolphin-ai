// Package registry provides a central command registry that unifies
// Cobra CLI commands, Console REPL commands, and LLM tools under a single
// definition. Each command is defined once as a *cobra.Command and
// registered with metadata; adapters generate console and tool bindings.
package registry

import (
	"github.com/spf13/cobra"
)

// Category groups commands for help display and filtering.
type Category string

const (
	CatAgent     Category = "Agent Management"
	CatConfig    Category = "Configuration"
	CatSession   Category = "Sessions"
	CatSkills    Category = "Skills"
	CatWorkflows Category = "Workflows"
	CatCommands  Category = "Commands"
	CatMCP       Category = "MCP Tools"
	CatCron      Category = "Scheduled Tasks"
	CatSystem    Category = "System"
)

// ConsoleSignal is returned from the console dispatcher to communicate
// flow-control requests (exit, new session, reload) back to the main loop.
type ConsoleSignal int

const (
	SignalNone       ConsoleSignal = iota
	SignalExit                     // /exit → terminate session
	SignalNewSession               // /new  → start new session
	SignalReload                   // /reload → reload agent
)

// CommandSpec wraps a *cobra.Command with registry metadata.
// The Cobra field is the single source of truth — all execution contexts
// (CLI, REPL, LLM tool) derive their behaviour from it.
type CommandSpec struct {
	// Cobra is the command definition shared by all contexts.
	Cobra *cobra.Command

	// Category groups this command for help output.
	Category Category

	// ToolSchema is the JSON Schema for the LLM tool version of this
	// command. When nil the adapter auto-infers a schema from the
	// cobra command's flags and Args validator.
	ToolSchema map[string]any

	// ToolName overrides the LLM tool name. When empty the adapter
	// uses Cobra.Use with spaces replaced by underscores.
	ToolName string

	// SelfEvolution marks a tool that should only be exposed as an LLM
	// tool when config SelfEvolution is enabled.
	SelfEvolution bool

	// Hidden hides this command from the REPL /help listing.
	Hidden bool

	// Signal communicates flow-control intent from the REPL dispatcher
	// back to the main loop (exit, new, reload).
	Signal ConsoleSignal
}
