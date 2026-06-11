package command

import (
	"context"

	"dolphin/internal/brain"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

// RegisterScripts registers the /script command for managing scripts.
func RegisterScripts(r *Registry, br *brain.Brain) {
	cmd := WithI18nShort(&cobra.Command{
		Use: "script",
	}, "command.scripts_manage")

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			scripts, err := brain.ListScripts(context.Background(), br)
			if err != nil {
				cmd.Printf(i18n.T("command.error_format"), err)
				return nil
			}
			if len(scripts) == 0 {
				cmd.Println(i18n.T("command.scripts_none"))
				return nil
			}
			if RenderModeFrom(cmd) == "markdown" {
				cmd.Print("**" + i18n.T("command.scripts_available") + "**\n\n")
				cmd.Println("| Name | Description | Status |")
				cmd.Println("|------|-------------|--------|")
				for _, s := range scripts {
					status := i18n.T("command.scripts_disabled")
					if s.Enabled {
						status = "✅ " + i18n.T("command.scripts_enabled")
					}
					cmd.Printf("| %s | %s | %s |\n", s.Name, s.Description, status)
				}
			} else {
				cmd.Println(i18n.T("command.scripts_available"))
				for _, s := range scripts {
					status := i18n.T("command.scripts_disabled")
					if s.Enabled {
						status = i18n.T("command.scripts_enabled")
					}
					cmd.Printf("  %s — %s (%s)\n", s.Name, s.Description, status)
				}
			}
			cmd.Printf(i18n.T("command.scripts_total"), len(scripts))
			return nil
		},
	}, "command.scripts_list"))

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "show [name]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := brain.ReadScript(context.Background(), br, args[0])
			if err != nil {
				cmd.Printf(i18n.T("command.scripts_not_found"), args[0])
				return nil
			}
			status := i18n.T("command.scripts_disabled")
			if s.Enabled {
				status = i18n.T("command.scripts_enabled")
			}
			cmd.Printf("%s — %s (%s)\n\n%s\n", s.Name, s.Description, status, s.Content)
			return nil
		},
	}, "command.scripts_show"))

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "delete [name]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := brain.DeleteScript(context.Background(), br, args[0]); err != nil {
				cmd.Printf(i18n.T("command.error_format"), err)
				return nil
			}
			cmd.Printf(i18n.T("command.scripts_deleted"), args[0])
			return nil
		},
	}, "command.scripts_delete"))

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		scripts, err := brain.ListScripts(context.Background(), br)
		if err != nil {
			cmd.Printf(i18n.T("command.error_format"), err)
			return nil
		}
		if len(scripts) == 0 {
			cmd.Println(i18n.T("command.scripts_none"))
			return nil
		}
		if RenderModeFrom(cmd) == "markdown" {
			cmd.Print("**" + i18n.T("command.scripts_available") + "**\n\n")
			cmd.Println("| Name | Description | Status |")
			cmd.Println("|------|-------------|--------|")
			for _, s := range scripts {
				status := i18n.T("command.scripts_disabled")
				if s.Enabled {
					status = "✅ " + i18n.T("command.scripts_enabled")
				}
				cmd.Printf("| %s | %s | %s |\n", s.Name, s.Description, status)
			}
		} else {
			cmd.Println(i18n.T("command.scripts_available"))
			for _, s := range scripts {
				status := i18n.T("command.scripts_disabled")
				if s.Enabled {
					status = i18n.T("command.scripts_enabled")
				}
				cmd.Printf("  %s — %s (%s)\n", s.Name, s.Description, status)
			}
		}
		cmd.Printf(i18n.T("command.scripts_total"), len(scripts))
		return nil
	}

	r.Register(cmd)
}
