package command

import (
	"github.com/spf13/cobra"
)

// Version is set via ldflags at build time (e.g. -X dolphin/internal/command.Version={{.Version}}).
var Version = "dev"

// RegisterVersion registers the /version command.
func RegisterVersion(r *Registry) {
	r.Register(&cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("dolphin " + Version)
			return nil
		},
	})
}
