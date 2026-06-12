package context

import (
	stdctx "context"
	"strings"

	"dolphin/internal/cli"

	"go.uber.org/zap"
)

// CliSection injects available CLI tool descriptions into the system prompt.
type CliSection struct {
	CLIs   []cli.CLI
	Logger *zap.Logger
}

func (s *CliSection) Name() string { return "cli" }
func (s *CliSection) Index() int   { return 15 }

func (s *CliSection) BuildContent(ctx stdctx.Context) (string, error) {
	if len(s.CLIs) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("## Available CLI tools\n\n")
	sb.WriteString("The following CLI tools are available in PATH. Use the `shell` tool to invoke them.\n")

	for i := range s.CLIs {
		cli.FetchHelp(&s.CLIs[i], s.Logger)

		desc := firstLine(s.CLIs[i].Help)
		if desc == "" {
			desc = "(no help available)"
		}

		sb.WriteString("- **")
		sb.WriteString(s.CLIs[i].Name)
		sb.WriteString("** — ")
		sb.WriteString(desc)
		sb.WriteString("\n")
	}

	sb.WriteString("\nUse `shell <name> --help` to see full usage for any CLI.")
	return sb.String(), nil
}

func firstLine(s string) string {
	if idx := strings.IndexAny(s, "\n\r"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	const maxLen = 200
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}
