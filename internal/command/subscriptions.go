package command

import (
	"context"

	"dolphin/internal/brain"
	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

// RegisterSubscriptionCmd registers the /subscription command for listing subscriptions.
func RegisterSubscriptionCmd(r *Registry, br *brain.Brain) {
	cmd := WithI18nShort(&cobra.Command{
		Use: "subscription",
	}, "command.subscription_manage")

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			subs, err := brain.ListSubscriptions(context.Background(), br)
			if err != nil {
				cmd.Printf(i18n.T("command.error_format"), err)
				return nil
			}
			if len(subs) == 0 {
				cmd.Println(i18n.T("command.subscription_none"))
				return nil
			}
			cmd.Println(i18n.T("command.subscription_list"))
			for _, s := range subs {
				status := i18n.T("command.subscription_disabled")
				if s.Enabled {
					status = i18n.T("command.subscription_enabled")
				}
				cmd.Printf("  %s — %s [%s] (%s)\n", s.Name, s.Description, s.EventPattern, status)
			}
			cmd.Printf(i18n.T("command.subscription_total"), len(subs))
			return nil
		},
	}, "command.subscription_list"))

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "show [name]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sub, err := brain.ReadSubscription(context.Background(), br, args[0])
			if err != nil {
				cmd.Printf(i18n.T("command.subscription_not_found"), args[0])
				return nil
			}
			status := i18n.T("command.subscription_disabled")
			if sub.Enabled {
				status = i18n.T("command.subscription_enabled")
			}
			cmd.Printf("%s — %s (%s)\n", sub.Name, sub.Description, status)
			cmd.Printf(i18n.T("command.subscription_event"), sub.EventPattern)
			if sub.Filters.Path != "" {
				cmd.Printf(i18n.T("command.subscription_filter"), sub.Filters.Path)
			}
			if sub.Content != "" {
				cmd.Printf("\n%s\n", sub.Content)
			}
			return nil
		},
	}, "command.subscription_show"))

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		subs, err := brain.ListSubscriptions(context.Background(), br)
		if err != nil {
			cmd.Printf(i18n.T("command.error_format"), err)
			return nil
		}
		if len(subs) == 0 {
			cmd.Println(i18n.T("command.subscription_none"))
			return nil
		}
		cmd.Println(i18n.T("command.subscription_list"))
		for _, s := range subs {
			status := i18n.T("command.subscription_disabled")
			if s.Enabled {
				status = i18n.T("command.subscription_enabled")
			}
			cmd.Printf("  %s — %s [%s] (%s)\n", s.Name, s.Description, s.EventPattern, status)
		}
		cmd.Printf(i18n.T("command.subscription_total"), len(subs))
		return nil
	}

	r.Register(cmd)
}
