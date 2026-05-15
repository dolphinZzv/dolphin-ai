package main

import (
	"fmt"
	"os"

	"dolphin/cmd"
	"dolphin/internal/update"
)

func main() {
	if update.ApplyStagedUpdate(update.MustExecPath()) {
		os.Exit(0)
	}

	root := cmd.NewRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
