package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("dolphin %s %s/%s built %s\n", Version, runtime.GOOS, runtime.Version(), BuildTime)
		},
	}
}
