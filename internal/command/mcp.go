package command

import (
	"context"

	"dolphin/internal/types"

	"github.com/spf13/cobra"
)

// RegisterMCP registers the /mcp command.
// toolLister is any source that can list tool definitions (e.g. *tool.Registry).
func RegisterMCP(r *Registry, toolLister interface {
	List(ctx context.Context) ([]types.ToolDef, error)
}) {
	r.Register(&cobra.Command{
		Use:   "mcp",
		Short: "List loaded MCP tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			defs, err := toolLister.List(context.Background())
			if err != nil {
				return err
			}
			if len(defs) == 0 {
				cmd.Println("No MCP tools loaded")
				return nil
			}
			cmd.Println("Loaded tools:")
			for _, t := range defs {
				cmd.Printf("  %s — %s\n", t.Name, t.Description)
			}
			return nil
		},
	})
}
