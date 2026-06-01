package command

import (
	"context"

	"dolphin/internal/brain"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

// RegisterCommands registers the /commands command for listing and managing commands.
func RegisterCommands(r *Registry, br *brain.Brain) {
	cmd := WithI18nShort(&cobra.Command{
		Use: "commands",
	}, "command.commands_manage")

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmds, err := brain.ListCommands(context.Background(), br)
			if err != nil {
				cmd.Printf(i18n.T("command.error_format"), err)
				return nil
			}
			if len(cmds) == 0 {
				cmd.Println(i18n.T("command.commands_none"))
				return nil
			}
			cmd.Println(i18n.T("command.commands_available"))
			for _, c := range cmds {
				status := i18n.T("command.commands_disabled")
				if c.Enabled {
					status = i18n.T("command.commands_enabled")
				}
				cmd.Printf("  %s — %s (%s)\n", c.Name, c.Description, status)
			}
			cmd.Printf(i18n.T("command.commands_total"), len(cmds))
			return nil
		},
	}, "command.commands_list"))

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "show [name]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := brain.ReadCommand(context.Background(), br, args[0])
			if err != nil {
				cmd.Printf(i18n.T("command.commands_not_found"), args[0])
				return nil
			}
			status := i18n.T("command.commands_disabled")
			if c.Enabled {
				status = i18n.T("command.commands_enabled")
			}
			cmd.Printf("%s — %s (%s)\n\n%s\n", c.Name, c.Description, status, c.Content)
			return nil
		},
	}, "command.commands_show"))

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		// Default to list when no subcommand.
		cmds, err := brain.ListCommands(context.Background(), br)
		if err != nil {
			cmd.Printf(i18n.T("command.error_format"), err)
			return nil
		}
		if len(cmds) == 0 {
			cmd.Println(i18n.T("command.commands_none"))
			return nil
		}
		cmd.Println(i18n.T("command.commands_available"))
		for _, c := range cmds {
			status := i18n.T("command.commands_disabled")
			if c.Enabled {
				status = i18n.T("command.commands_enabled")
			}
			cmd.Printf("  %s — %s (%s)\n", c.Name, c.Description, status)
		}
		cmd.Printf(i18n.T("command.commands_total"), len(cmds))
		return nil
	}

	r.Register(cmd)
}
