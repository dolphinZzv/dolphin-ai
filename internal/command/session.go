package command

import (
	"dolphin/internal/session"

	"github.com/spf13/cobra"
)

// RegisterSession registers the /session command group.
func RegisterSession(r *Registry, sessMgr *session.Manager) {
	sessionCmd := &cobra.Command{
		Use:   "session",
		Short: "Manage sessions",
	}

	sessionCmd.AddCommand(&cobra.Command{
		Use:   "new",
		Short: "Create a new session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sess := sessMgr.Create(cmd.Context())
			cmd.Printf("created session %s\n", sess.ID)
			return nil
		},
	})

	sessionCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessions, _ := sessMgr.List(cmd.Context())
			if len(sessions) == 0 {
				cmd.Println("no sessions")
				return nil
			}
			for _, s := range sessions {
				active := ""
				if s.Active {
					active = " [active]"
				}
				cmd.Printf("  %s%s\n", s.ID[:8], active)
			}
			return nil
		},
	})

	sessionCmd.AddCommand(&cobra.Command{
		Use:   "switch [id]",
		Short: "Switch to a session (deprecated: use /session new)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("use /session new to create and switch to a new session")
			return nil
		},
	})

	r.Register(sessionCmd)
}
