package command

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"dolphin/internal/agentio"
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

			pending, capacity, _ := r.agentIO.QueueSnapshot()
			active := r.agentIO.ActiveSnapshot()

			if RenderModeFrom(cmd) == "markdown" {
				renderQueueMarkdown(cmd, active, pending, capacity)
			} else {
				renderQueuePlain(cmd, active, pending, capacity)
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

func renderQueuePlain(cmd *cobra.Command, active map[string]*agentio.TurnInfo, pending []*agentio.Turn, capacity int) {
	fmt.Fprintf(cmd.OutOrStdout(), "Agent Queue: %d worker(s) active, %d pending / %d capacity\n",
		len(active), len(pending), capacity)

	if len(active) > 0 {
		cmd.Println()
		for _, id := range sortedWorkerIDs(active) {
			t := active[id]
			elapsed := time.Since(t.StartedAt).Round(time.Second)
			input := truncateInput(t.Input, 80)
			fmt.Fprintf(cmd.OutOrStdout(), "  %s: [%s] %s — elapsed %s\n",
				id, t.TransportID, input, elapsed)
		}
	}

	if len(pending) > 0 {
		cmd.Println()
		for i, t := range pending {
			wait := time.Since(t.EnqueuedAt).Round(time.Second)
			input := truncateInput(t.Input, 80)
			fmt.Fprintf(cmd.OutOrStdout(), "  %d. [%s] %s — waiting %s\n",
				i+1, t.TransportID, input, wait)
		}
	}
}

func renderQueueMarkdown(cmd *cobra.Command, active map[string]*agentio.TurnInfo, pending []*agentio.Turn, capacity int) {
	fmt.Fprintf(cmd.OutOrStdout(), "**Agent Queue** — %d active, %d pending / %d capacity\n\n",
		len(active), len(pending), capacity)

	if len(active) > 0 {
		cmd.Println("| Worker | Transport | Session | Input | Elapsed |")
		cmd.Println("|--------|-----------|---------|-------|---------|")
		for _, id := range sortedWorkerIDs(active) {
			t := active[id]
			elapsed := time.Since(t.StartedAt).Round(time.Second)
			input := truncateInput(t.Input, 60)
			cmd.Printf("| %s | %s | %s | %s | %s |\n",
				id, t.TransportID, truncateInput(t.SessionID, 8), input, elapsed)
		}
		cmd.Println()
	}

	if len(pending) > 0 {
		cmd.Println("| # | Transport | Session | Input | Waiting |")
		cmd.Println("|---|-----------|---------|-------|---------|")
		for i, t := range pending {
			wait := time.Since(t.EnqueuedAt).Round(time.Second)
			input := truncateInput(t.Input, 60)
			cmd.Printf("| %d | %s | %s | %s | %s |\n",
				i+1, t.TransportID, truncateInput(t.SessionID, 8), input, wait)
		}
	}
}

func sortedWorkerIDs(active map[string]*agentio.TurnInfo) []string {
	ids := make([]string, 0, len(active))
	for id := range active {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func truncateInput(s string, n int) string {
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}

// Ensure agentio import is used (satisfies compiler when Turn is referenced indirectly).
var _ = (*agentio.Turn)(nil)
