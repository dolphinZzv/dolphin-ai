package command

import (
	"dolphin/internal/session"
	"dolphin/internal/signal"

	"github.com/spf13/cobra"
)

// RegisterSession registers the /session command group.
func RegisterSession(r *Registry, sessMgr *session.Manager) {
	sessionCmd := WithI18nShort(&cobra.Command{
		Use: "session",
	}, "command.session_manage")

	sessionCmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "new",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Cancel all running operations in the current session before switching.
			if cur := sessMgr.Active(); cur != nil {
				r.signalBus.Send(cur.ID, signal.Interrupt)
			}
			sess := sessMgr.Create(cmd.Context())
			cmd.Printf("created session %s\n", sess.ID)
			return nil
		},
	}, "command.session_create"))

	sessionCmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessions, _ := sessMgr.List(cmd.Context())
			if len(sessions) == 0 {
				cmd.Println("no sessions")
				return nil
			}
			if RenderModeFrom(cmd) == "markdown" {
				cmd.Println("| ID | Status |")
				cmd.Println("|----|--------|")
				for _, s := range sessions {
					status := ""
					if s.Active {
						status = " 🟢 active"
					}
					cmd.Printf("| %s%s |\n", s.ID[:8], status)
				}
			} else {
				for _, s := range sessions {
					active := ""
					if s.Active {
						active = " [active]"
					}
					cmd.Printf("  %s%s\n", s.ID[:8], active)
				}
			}
			return nil
		},
	}, "command.session_list"))

	sessionCmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "switch [id]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			// Cancel all running operations in the current session before switching.
			if cur := sessMgr.Active(); cur != nil && cur.ID != id {
				r.signalBus.Send(cur.ID, signal.Interrupt)
			}
			sess, err := sessMgr.SwitchTo(cmd.Context(), id)
			if err != nil {
				cmd.Printf("error: %v\n", err)
				return nil
			}
			cmd.Printf("switched to session %s\n", sess.ID)
			return nil
		},
	}, "command.session_switch"))
		sessionCmd.AddCommand(WithI18nShort(&cobra.Command{
			Use: "stop",
			RunE: func(cmd *cobra.Command, args []string) error {
				cur := sessMgr.Active()
				if cur == nil {
					cmd.Println("no active session")
					return nil
				}
				r.signalBus.Send(cur.ID, signal.Pause)
				cmd.Printf("session %s paused\n", cur.ID[:8])
				return nil
			},
		}, "command.session_pause"))

	sessionCmd.AddCommand(WithI18nShort(&cobra.Command{
			Use: "continue",
			RunE: func(cmd *cobra.Command, args []string) error {
				cur := sessMgr.Active()
				if cur == nil {
					cmd.Println("no active session")
					return nil
				}
				r.signalBus.Send(cur.ID, signal.Resume)
				cmd.Printf("session %s resumed\n", cur.ID[:8])
				return nil
			},
		}, "command.session_resume"))


	r.Register(sessionCmd)

	// Aliases: /new and /clear both create a new session.
	alias := func(name string) {
		r.Register(WithI18nShort(&cobra.Command{
			Use: name,
			RunE: func(cmd *cobra.Command, args []string) error {
				// Cancel all running operations in the current session before switching.
				if cur := sessMgr.Active(); cur != nil {
					r.signalBus.Send(cur.ID, signal.Interrupt)
				}
				sess := sessMgr.Create(cmd.Context())
				cmd.Printf("created session %s\n", sess.ID)
				return nil
			},
		}, "command.session_create"))
	}
	alias("new")
	alias("clear")
}
