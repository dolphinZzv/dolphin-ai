package command

import (
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"dolphin/internal/agentloop"
	"dolphin/internal/dump"
	"dolphin/internal/event"
	"dolphin/internal/i18n"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/session"
	"dolphin/internal/signal"
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
	// /session stop aborts the current turn: it sends signal.Interrupt,
	// which the compositor turns into a context cancellation that every
	// stage (including init-stage compaction) observes. This is a hard
	// stop — use /session pause for a resumable pause.
	sessionCmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "stop",
		RunE: func(cmd *cobra.Command, args []string) error {
			cur := sessMgr.Active()
			if cur == nil {
				cmd.Println("no active session")
				return nil
			}
			r.signalBus.Send(cur.ID, signal.Interrupt)
			cmd.Println(i18n.T("command.session_stopped_msg"))
			return nil
		},
	}, "command.session_stop"))

	// /session pause pauses the current turn. CompactionStage, LLMStage and
	// ToolStage all honor Pause, so it takes effect even mid-compaction.
	// Resume with /session continue.
	sessionCmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "pause",
		RunE: func(cmd *cobra.Command, args []string) error {
			cur := sessMgr.Active()
			if cur == nil {
				cmd.Println("no active session")
				return nil
			}
			r.signalBus.Send(cur.ID, signal.Pause)
			cmd.Println(i18n.T("command.session_paused_msg"))
			return nil
		},
	}, "command.session_pause"))

	// /session continue resumes a paused turn.
	sessionCmd.AddCommand(WithI18nShort(&cobra.Command{
		Use: "continue",
		RunE: func(cmd *cobra.Command, args []string) error {
			cur := sessMgr.Active()
			if cur == nil {
				cmd.Println("no active session")
				return nil
			}
			r.signalBus.Send(cur.ID, signal.Resume)
			cmd.Println(i18n.T("command.session_resumed_msg"))
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

// RegisterDump registers the /session dump subcommand and /dump alias.
func RegisterDump(r *Registry, recorder *dump.Recorder, sessMgr *session.Manager) {
	if recorder == nil || sessMgr == nil {
		return
	}

	runE := func(cmd *cobra.Command, args []string) error {
		sid := ""
		if len(args) > 0 {
			sid = args[0]
		} else {
			if s := sessMgr.Active(); s != nil {
				sid = s.ID
			}
		}
		if sid == "" {
			cmd.Println("no session specified and no active session")
			return nil
		}

		path, err := recorder.Write(sid)
		if err != nil {
			cmd.Printf("dump failed: %v\n", err)
			return nil
		}
		cmd.Printf("dumped to %s\n", path)
		return nil
	}

	// /session dump [session_id] — subcommand of the existing /session group.
	if parent, _, err := r.root.Find(strings.Fields("session")); err == nil {
		parent.AddCommand(&cobra.Command{
			Use:   "dump [session_id]",
			Short: "Write the last turn's LLM request/response and tool calls to a JSON file",
			Long:  "Writes the most recent turn's full data to <dump_dir>/<session_id>.json. If no session_id is given, the currently active session is used.",
			RunE:  runE,
		})
	}

	// /dump alias
	r.Register(&cobra.Command{
		Use:   "dump [session_id]",
		Short: "Alias for /session dump",
		RunE:  runE,
	})
}

// RegisterCompaction registers /session compaction and the /compaction alias
// for on-demand manual history compaction.
func RegisterCompaction(r *Registry, provider llm.Provider, mem memory.Memory, maxThreshold, keepRounds, summaryMaxTokens, tokenRatio int, model string, eventBus *event.Bus, logger *zap.Logger, sessMgr *session.Manager) {
	if provider == nil || mem == nil || maxThreshold <= 0 || keepRounds <= 0 {
		return
	}

	stage := &agentloop.CompactionStage{
		Provider:     provider,
		Memory:       mem,
		Model:        model,
		MaxTokens:    summaryMaxTokens,
		MaxThreshold: maxThreshold,
		KeepRounds:   keepRounds,
		TokenRatio:   tokenRatio,
		EventBus:     eventBus,
		Logger:       logger,
	}

	runE := func(cmd *cobra.Command, args []string) error {
		s := sessMgr.Active()
		if s == nil {
			cmd.Println("no active session")
			return nil
		}
		result, err := stage.ManualCompact(cmd.Context(), s.ID)
		if err != nil {
			cmd.Printf("compaction failed: %v\n", err)
			return nil
		}
		cmd.Println(result)
		return nil
	}

	// /session compaction — subcommand of the existing /session group.
	if parent, _, err := r.root.Find(strings.Fields("session")); err == nil {
		parent.AddCommand(&cobra.Command{
			Use:   "compaction",
			Short: "Manually compact the current session's history",
			Long:  "Summarizes the oldest messages in the current session, keeping the most recent turns verbatim. The compacted history replaces the original in memory.",
			RunE:  runE,
		})
	}

	// /compaction alias
	r.Register(&cobra.Command{
		Use:   "compaction",
		Short: "Alias for /session compaction",
		RunE:  runE,
	})
}
