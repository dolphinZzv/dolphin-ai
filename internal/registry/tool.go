package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"dolphin/internal/mcp"
	"dolphin/internal/subsystem"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// BuildToolDef converts a CommandSpec into a subsystem.ToolDef suitable for
// LLM tool registration. Returns nil when the spec has no RunE/Run handler.
func BuildToolDef(spec *CommandSpec) *subsystem.ToolDef {
	cmd := spec.Cobra
	if cmd == nil || cmd.Hidden {
		return nil
	}
	if cmd.RunE == nil && cmd.Run == nil {
		return nil
	}

	schema := spec.ToolSchema
	if schema == nil {
		schema = inferSchema(cmd)
	}

	name := spec.ToolName
	if name == "" {
		name = strings.ReplaceAll(cmd.Name(), "-", "_")
	}

	return &subsystem.ToolDef{
		Name:          name,
		Description:   cmd.Short,
		Schema:        schema,
		Handler:       wrapRunE(cmd),
		SelfEvolution: spec.SelfEvolution,
	}
}

// BuildToolDefs converts all matching specs into ToolDefs, respecting the
// selfEvolution flag.
func BuildToolDefs(specs []*CommandSpec, selfEvolution bool) []subsystem.ToolDef {
	var out []subsystem.ToolDef
	for _, s := range specs {
		if s.SelfEvolution && !selfEvolution {
			continue
		}
		if td := BuildToolDef(s); td != nil {
			out = append(out, *td)
		}
	}
	return out
}

// wrapRunE adapts a cobra RunE/Run into a tool handler that captures
// OutOrStdout output and returns it as the ToolResult content.
func wrapRunE(cmd *cobra.Command) func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	return func(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
		var args []string
		if err := json.Unmarshal(input, &args); err != nil {
			// Try as object with "args" key.
			var obj struct {
				Args []string `json:"args"`
			}
			if json.Unmarshal(input, &obj) == nil {
				args = obj.Args
			} else {
				// Single string arg.
				var s string
				if json.Unmarshal(input, &s) == nil {
					args = []string{s}
				}
			}
		}

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true

		var runErr error
		if cmd.RunE != nil {
			runErr = cmd.RunE(cmd, args)
		} else if cmd.Run != nil {
			cmd.Run(cmd, args)
		}

		content := strings.TrimRight(buf.String(), "\n")
		if runErr != nil {
			if content != "" {
				content += "\n"
			}
			content += runErr.Error()
			return &mcp.ToolResult{Content: content, IsError: true}, nil
		}
		return &mcp.ToolResult{Content: content}, nil
	}
}

// inferSchema builds a minimal JSON Schema from a cobra command's flags and
// positional args.
func inferSchema(cmd *cobra.Command) map[string]any {
	props := make(map[string]any)
	required := make([]string, 0)

	// If the command has an Args validator and no subcommands, it accepts
	// positional arguments.
	if cmd.Args != nil && !cmd.HasSubCommands() {
		props["args"] = map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": fmt.Sprintf("Positional arguments for %s", cmd.Name()),
		}
		required = append(required, "args")
	}

	// Add flags.
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		fSchema := flagToSchema(f)
		if fSchema != nil {
			props[f.Name] = fSchema
		}
	})

	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func flagToSchema(f *pflag.Flag) map[string]any {
	s := make(map[string]any)
	switch f.Value.Type() {
	case "bool":
		s["type"] = "boolean"
	case "int", "int64", "uint", "uint64":
		s["type"] = "integer"
	case "float64", "float32":
		s["type"] = "number"
	default:
		s["type"] = "string"
	}
	if f.Usage != "" {
		s["description"] = f.Usage
	}
	return s
}
