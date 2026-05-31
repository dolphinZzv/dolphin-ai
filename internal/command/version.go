package command

import (
	"github.com/spf13/cobra"
)

// RegisterVersion registers the /version command.
func RegisterVersion(r *Registry) {
	r.Register(&cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("dolphin v2.0.0")
			return nil
		},
	})
}
