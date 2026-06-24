package command

import (
	"github.com/spf13/cobra"

	"dolphin/internal/dream"
	"dolphin/internal/i18n"
)

// RegisterDream registers the /dream command and its subcommands.
func RegisterDream(r *Registry, d *dream.Dream) {
	cmd := &cobra.Command{
		Use:   "dream",
		Short: "Self-improvement via offline brain editing",
		Long:  "Dream scans recent conversation sessions and edits brain files to correct mistakes, merge duplicates, and deprecate unused content.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "now",
		Short: "Trigger a dream run immediately, skipping the idle timer",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := d.DreamNow(cmd.Context())
			if err != nil {
				cmd.Println(i18n.T("command.error_format"), err)
				return nil
			}
			if result == "" {
				cmd.Println("Dream skipped: insufficient signal or already in progress.")
				return nil
			}
			cmd.Println(result)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show dream state and metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			s := d.State()
			lastStr := s.LastDreamAt.Format("2006-01-02 15:04")
		if s.LastDreamAt.IsZero() {
			lastStr = "never"
		}
		cmd.Printf("Last dream: #%d at %s\n", s.LastDreamID, lastStr)
			cmd.Printf("Consecutive empty: %d\n", s.ConsecutiveEmpty)
			cmd.Printf("Totals: %d improved, %d deprecated, %d merged, %d created\n",
				s.Totals.FilesImproved, s.Totals.FilesDeprecated, s.Totals.FilesMerged, s.Totals.FilesCreated)
			if s.OpenBranch != "" {
				cmd.Printf("Open branch: %s (use /dream review)\n", s.OpenBranch)
			}
			if s.BackupStale {
				cmd.Println("⚠ dream state backup is stale")
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "preview",
		Short: "Show what dream would edit without applying",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Preview would need a separate entry point. For now, tell the user.
			cmd.Println("dream preview: run /dream now to trigger a full scan + apply.")
			cmd.Println("Use /dream status to see past results.")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "revert [dream_id]",
		Short: "Revert a dream's merge commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("dream revert: reversion is handled via git revert of the dream merge commit.")
			cmd.Println("Check /dream status for the merge SHA and use /brain git revert <sha>.")
			return nil
		},
	})

	// Default: show status.
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		s := d.State()
		lastStr := s.LastDreamAt.Format("2006-01-02 15:04")
		if s.LastDreamAt.IsZero() {
			lastStr = "never"
		}
		cmd.Printf("Dream: #%d runs completed. Last: %s\n", s.LastDreamID, lastStr)
		return nil
	}

	r.Register(cmd)
}
