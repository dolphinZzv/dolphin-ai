package cmd

import (
	"fmt"
	"runtime"

	"dolphin/internal/i18n"

	"github.com/spf13/cobra"
)

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   i18n.TL(i18n.KeyCmdVersionUse),
		Short: i18n.TL(i18n.KeyCmdVersionShort),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("dolphin %s %s/%s built %s\n", Version, runtime.GOOS, runtime.Version(), BuildTime)
		},
	}
}
