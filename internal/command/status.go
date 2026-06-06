package command

import (
	"context"
	"fmt"
	"strings"

	"dolphin/internal/memory"
	"dolphin/internal/session"

	"github.com/spf13/cobra"
)

// RegisterSessionStatus registers the /session status subcommand.
func RegisterSessionStatus(r *Registry, sessMgr *session.Manager, mem memory.Memory, sessionMode string) {
	parent, _, err := r.root.Find(strings.Fields("session"))
	if err != nil || parent == r.root {
		return // session command not found
	}

	statusCmd := WithI18nShort(&cobra.Command{
		Use: "status",
		RunE: printSessionStatus(sessMgr, mem, sessionMode),
	}, "command.session_status")

	parent.AddCommand(statusCmd)

	// Top-level alias: /status
	statusAlias := &cobra.Command{
		Use:   "status",
		Short: "Show session status",
		RunE:  printSessionStatus(sessMgr, mem, sessionMode),
	}
	r.Register(statusAlias)
}

func printSessionStatus(sessMgr *session.Manager, mem memory.Memory, sessionMode string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		sess := sessMgr.Active()

		if sess != nil {
			cmd.Printf("Session ID:    %s\n", sess.ID)
		} else {
			cmd.Println("Session ID:    (none)")
		}

		cmd.Printf("Session Mode:  %s\n", sessionMode)
		cmd.Printf("System Ctx:    %s characters\n", comma(tokenVal(sess.Get("system_context"))))

		if sess != nil && mem != nil {
			msgs, err := mem.Read(context.Background(), sess.ID)
			if err == nil {
				totalChars := 0
				for _, m := range msgs {
					totalChars += len(m.Content)
				}
				cmd.Printf("Rounds:        %s\n", comma(tokenVal(sess.Get("rounds"))))
				cmd.Printf("Tool Calls:    %s\n", comma(tokenVal(sess.Get("tool_calls"))))
				cmd.Printf("Input Tokens:  %s\n", comma(tokenVal(sess.Get("input_tokens"))))
				cmd.Printf("Output Tokens: %s\n", comma(tokenVal(sess.Get("output_tokens"))))
				cmd.Printf("Last Input:    %s\n", comma(tokenVal(sess.Get("last_input_tokens"))))
				cmd.Printf("Last Output:   %s\n", comma(tokenVal(sess.Get("last_output_tokens"))))
				cmd.Printf("Context:       %d messages, %s characters\n",
					len(msgs), comma(totalChars))
			}
		}

		return nil
	}
}

// tokenVal extracts an int from the session.Data value (may be nil or wrong type).
func tokenVal(v any) int {
	if v == nil {
		return 0
	}
	n, _ := v.(int)
	return n
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
