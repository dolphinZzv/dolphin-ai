package command

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// RegisterContext registers the /context command.
// buildSystemPrompt is a function that assembles and returns the full system prompt.
func RegisterContext(r *Registry, buildSystemPrompt func(ctx context.Context) (string, error)) {
	r.Register(&cobra.Command{
		Use:   "context",
		Short: "Show full system context (brain index, skills, etc.)",
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt, err := buildSystemPrompt(context.Background())
			if err != nil {
				return fmt.Errorf("context: %w", err)
			}
			cmd.Println(prompt)
			return nil
		},
	})
}
