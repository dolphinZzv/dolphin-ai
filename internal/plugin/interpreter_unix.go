//go:build !windows

package plugin

import (
	"context"
	"os/exec"
)

func shellInterpreter() string {
	return "sh"
}

func shellCommand(ctx context.Context, scriptPath string) *exec.Cmd {
	return exec.CommandContext(ctx, shellInterpreter(), scriptPath)
}
