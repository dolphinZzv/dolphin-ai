package command

import (
	"context"
	"fmt"
	"strings"

	"dolphin/internal/memory"
	"dolphin/internal/session"
	"dolphin/internal/types"

	"github.com/spf13/cobra"
)

// RegisterSessionStatus registers the /session status subcommand.
func RegisterSessionStatus(r *Registry, sessMgr *session.Manager, mem memory.Memory, sessionMode string) {
	parent, _, err := r.root.Find(strings.Fields("session"))
	if err != nil || parent == r.root {
		return // session command not found
	}

	parent.AddCommand(WithI18nShort(&cobra.Command{
		Use: "status",
		RunE: func(cmd *cobra.Command, args []string) error {
			sess := sessMgr.Active()

			if sess != nil {
				cmd.Printf("Session ID:    %s\n", sess.ID)
			} else {
				cmd.Println("Session ID:    (none)")
			}

			cmd.Printf("Session Mode:  %s\n", sessionMode)

			if sess != nil && mem != nil {
				msgs, err := mem.Read(context.Background(), sess.ID)
				if err == nil {
					totalChars := 0
					rounds := 0
					toolCalls := 0
					for _, m := range msgs {
						totalChars += len(m.Content)
						if m.Role == types.RoleUser {
							rounds++
						}
						if m.Role == types.RoleTool {
							toolCalls++
						}
					}
					cmd.Printf("Rounds:        %d\n", rounds)
					cmd.Printf("Tool Calls:    %d\n", toolCalls)
					cmd.Printf("Context:       %d messages, %s characters\n",
						len(msgs), comma(totalChars))
				}
			}

			return nil
		},
	}, "command.session_status"))
}

// comma formats an integer with thousand separators.
func comma(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for i := len(s); i > 0; i -= 3 {
		start := i - 3
		if start < 0 {
			start = 0
		}
		parts = append([]string{s[start:i]}, parts...)
	}
	return strings.Join(parts, ",")
}
