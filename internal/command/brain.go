package command

import (
	"github.com/spf13/cobra"

	"dolphin/internal/brain"
)

// RegisterBrain registers /brain push, /brain pull, /brain set, and the
// top-level aliases /push and /pull.
func RegisterBrain(r *Registry, br *brain.Brain) {
	if br == nil || !br.IsInitialized() {
		return
	}

	brainCmd := &cobra.Command{
		Use:   "brain",
		Short: "Manage brain git operations (push/pull/set)",
	}

	// /brain push
	brainCmd.AddCommand(&cobra.Command{
		Use:   "push",
		Short: "Push brain commits to the remote",
		Long:  "Pushes committed brain changes to the configured origin remote.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := br.Push(cmd.Context()); err != nil {
				cmd.Printf("brain push failed: %v\n", err)
			} else {
				cmd.Println("brain pushed.")
			}
			return nil
		},
	})

	// /brain pull
	brainCmd.AddCommand(&cobra.Command{
		Use:   "pull",
		Short: "Pull brain commits from the remote",
		Long:  "Fetches and merges brain changes from the configured origin remote.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := br.Pull(cmd.Context()); err != nil {
				cmd.Printf("brain pull failed: %v\n", err)
			} else {
				cmd.Println("brain pulled.")
			}
			return nil
		},
	})

	// /brain set url <url>
	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Configure brain settings",
	}
	setCmd.AddCommand(&cobra.Command{
		Use:   "url <url>",
		Short: "Set the brain remote URL",
		Long:  "Configures the origin remote URL for brain git operations (push/pull).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			if err := br.SetURL(cmd.Context(), url); err != nil {
				cmd.Printf("brain set url failed: %v\n", err)
			} else {
				cmd.Printf("brain remote URL set to %s\n", url)
			}
			return nil
		},
	})
	brainCmd.AddCommand(setCmd)

	r.Register(brainCmd)

	// Aliases: /push → /brain push, /pull → /brain pull
	r.Register(&cobra.Command{
		Use:   "push",
		Short: "Alias for /brain push",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := br.Push(cmd.Context()); err != nil {
				cmd.Printf("push failed: %v\n", err)
			} else {
				cmd.Println("pushed.")
			}
			return nil
		},
	})

	r.Register(&cobra.Command{
		Use:   "pull",
		Short: "Alias for /brain pull",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := br.Pull(cmd.Context()); err != nil {
				cmd.Printf("pull failed: %v\n", err)
			} else {
				cmd.Println("pulled.")
			}
			return nil
		},
	})
}
