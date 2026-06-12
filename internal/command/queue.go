package command

import (
	"fmt"
	"strconv"
	"time"

	"dolphin/internal/agentio"

	"github.com/spf13/cobra"
)

// RegisterQueue registers the /queue command for viewing and managing the agent turn queue.
func RegisterQueue(r *Registry) {
	cmd := WithI18nShort(&cobra.Command{
		Use: "queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			if r.agentIO == nil {
				cmd.Println("queue status unavailable")
				return nil
			}

			pending, capacity, processing := r.agentIO.QueueSnapshot()

			if RenderModeFrom(cmd) == "markdown" {
				cmd.Print("**Agent Queue**\n\n")
				if processing {
					cmd.Println("- Processing: 🔄")
				}
				cmd.Printf("- Pending: %d / %d capacity\n\n", len(pending), capacity)
				if len(pending) > 0 {
					cmd.Println("| # | Transport | Session | Input | Waiting |")
					cmd.Println("|---|-----------|---------|-------|---------|")
					for i, t := range pending {
						wait := time.Since(t.EnqueuedAt).Round(time.Second)
						input := truncateForMarkdown(t.Input, 60)
						cmd.Printf("| %d | %s | %s | %s | %s |\n",
							i+1, t.TransportID, truncateForMarkdown(t.SessionID, 8), input, wait)
					}
				}
			} else {
				if processing {
					cmd.Println("Agent Queue: (processing)")
				}
				cmd.Printf("Agent Queue: %d pending / %d capacity\n", len(pending), capacity)
				for i, t := range pending {
					wait := time.Since(t.EnqueuedAt).Round(time.Second)
					input := t.Input
					if len(input) > 80 {
						input = input[:77] + "..."
					}
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %d. [%s] %s — %s\n", i+1, t.TransportID, input, wait)
				}
			}
			return nil
		},
	}, "command.queue")

	cmd.AddCommand(WithI18nShort(&cobra.Command{
		Use:  "pop [index]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if r.agentIO == nil {
				cmd.Println("queue status unavailable")
				return nil
			}

			index, err := strconv.Atoi(args[0])
			if err != nil || index < 1 {
				cmd.Printf("invalid index: %s (must be a positive number)\n", args[0])
				return nil
			}

			turn := r.agentIO.PopIndex(index - 1)
			if turn == nil {
				cmd.Printf("no turn at index %d\n", index)
				return nil
			}
			cmd.Printf("popped turn %d (%s)\n", index, turn.TurnID)
			return nil
		},
	}, "command.queue_pop"))

	r.Register(cmd)
}

func truncateForMarkdown(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// Ensure agentio import is used (satisfies compiler when Turn is referenced indirectly).
var _ = (*agentio.Turn)(nil)
