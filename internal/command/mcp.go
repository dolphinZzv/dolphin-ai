package command

import (
	"context"
	"sort"
	"strings"

	"dolphin/internal/i18n"
	"dolphin/internal/tool"
	"dolphin/internal/types"

	"github.com/spf13/cobra"
)

type category struct {
	Name  string
	Match func(name string) bool
}

var toolCategories = []category{
	{Name: "Knowledge", Match: prefix("brain_")},
	{Name: "Commands", Match: prefix("command_")},
	{Name: "Scripts", Match: prefix("script_")},
	{Name: "Skills", Match: prefix("skill_")},
	{Name: "Sessions", Match: prefix("session_")},
	{Name: "Scheduled Tasks", Match: prefix("cron_")},
	{Name: "Subscriptions", Match: prefix("subscription_")},
	{Name: "MCP Services", Match: prefix("mcp_")},
	{Name: "Agent", Match: func(name string) bool {
		return name == "request_permission" || name == "emit_event"
	}},
	{Name: "System", Match: func(name string) bool { return name == "shell" }},
}

func prefix(p string) func(string) bool {
	return func(name string) bool { return strings.HasPrefix(name, p) }
}

// mcpManager is the subset of tool.Registry that the /mcp command needs.
type mcpManager interface {
	List(ctx context.Context) ([]types.ToolDef, error)
	ListActiveSources(ctx context.Context) []tool.SourceInfo
	DisableSource(name string) error
	EnableSource(name string) error
}

// RegisterMCP registers the /mcp command.
func RegisterMCP(r *Registry, mgr mcpManager) {
	mcpCmd := WithI18nShort(&cobra.Command{
		Use: "mcp",
		RunE: func(cmd *cobra.Command, args []string) error {
			defs, err := mgr.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(defs) == 0 {
				cmd.Println(i18n.T("command.mcp_none"))
				return nil
			}

			// Track categorized tools.
			categorized := make(map[string]bool, len(defs))

			cmd.Println(i18n.T("command.mcp_loaded"))

			for _, cat := range toolCategories {
				var matched []types.ToolDef
				for _, d := range defs {
					if cat.Match(d.Name) {
						matched = append(matched, d)
						categorized[d.Name] = true
					}
				}
				if len(matched) == 0 {
					continue
				}
				sort.Slice(matched, func(i, j int) bool {
					return matched[i].Name < matched[j].Name
				})
				cmd.Printf("\n  [%s]\n", cat.Name)
				for _, t := range matched {
					cmd.Printf("  %s — %s\n", t.Name, t.Description)
				}
			}

			// Uncategorized tools.
			var other []types.ToolDef
			for _, d := range defs {
				if !categorized[d.Name] {
					other = append(other, d)
				}
			}
			if len(other) > 0 {
				sort.Slice(other, func(i, j int) bool {
					return other[i].Name < other[j].Name
				})
				cmd.Printf("\n  [%s]\n", i18n.T("command.mcp_other"))
				for _, t := range other {
					cmd.Printf("  %s — %s\n", t.Name, t.Description)
				}
			}

			// Show source status.
			sources := mgr.ListActiveSources(cmd.Context())
			if len(sources) > 0 {
				cmd.Printf("\n%s\n", i18n.T("command.mcp_sources"))
				for _, s := range sources {
					status := i18n.T("command.mcp_enabled")
					if !s.Enabled {
						status = i18n.T("command.mcp_disabled")
					}
					cmd.Printf("  %s — %s\n", s.Name, status)
				}
			}

			return nil
		},
	}, "command.mcp_list_desc")

	mcpCmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "disable [source]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mgr.DisableSource(args[0]); err != nil {
				cmd.Printf(i18n.T("command.mcp_source_not_found"), args[0])
				return nil
			}
			cmd.Printf(i18n.T("command.mcp_disabled_source"), args[0])
			return nil
		},
	}, "command.mcp_disable_cmd"))

	mcpCmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "enable [source]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mgr.EnableSource(args[0]); err != nil {
				cmd.Printf(i18n.T("command.mcp_source_not_found"), args[0])
				return nil
			}
			cmd.Printf(i18n.T("command.mcp_enabled_source"), args[0])
			return nil
		},
	}, "command.mcp_enable_cmd"))

	r.Register(mcpCmd)
}
