package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"dolphin/internal/brain"
	"dolphin/internal/i18n"
)

// RegisterCommands registers the /commands command for listing and managing commands.
func RegisterCommands(r *Registry, br *brain.Brain) {
	cmd := WithI18nShort(&cobra.Command{
		Use: "commands",
	}, "command.commands_manage")

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			printCommandList(cmd, br)
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
		printCommandList(cmd, br)
		return nil
	}

	r.Register(cmd)
}

func printCommandList(cmd *cobra.Command, br *brain.Brain) {
	cmds, err := brain.ListCommands(context.Background(), br)
	if err != nil {
		cmd.Printf(i18n.T("command.error_format"), err)
		return
	}
	if len(cmds) == 0 {
		cmd.Println(i18n.T("command.commands_none"))
		return
	}

	// Compute max name width for column alignment.
	nameW := len("command")
	statusW := len("status")
	enabledLabel := i18n.T("command.commands_enabled")
	disabledLabel := i18n.T("command.commands_disabled")
	for _, c := range cmds {
		if n := len(c.Name); n > nameW {
			nameW = n
		}
	}
	if n := len(enabledLabel); n > statusW {
		statusW = n
	}
	if n := len(disabledLabel); n > statusW {
		statusW = n
	}

	pfmt := fmt.Sprintf("  %%-%ds  %%-%ds  %%s\n", nameW, statusW)

	cmd.Printf(pfmt, "command", "status", "description")
	cmd.Println("  " + strings.Repeat("-", nameW) + "  " + strings.Repeat("-", statusW) + "  " + strings.Repeat("-", len("description")))
	for _, c := range cmds {
		status := disabledLabel
		if c.Enabled {
			status = enabledLabel
		}
		cmd.Printf(pfmt, c.Name, status, c.Description)
	}
	cmd.Printf(i18n.T("command.commands_total"), len(cmds))
}
