//go:build windows

package plugin

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	detectedInterpreter string
	interpreterOnce     sync.Once
)

func detectInterpreter() string {
	for _, name := range []string{"pwsh.exe", "powershell.exe", "cmd.exe", "bash.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return "cmd.exe"
}

func shellInterpreter() string {
	interpreterOnce.Do(func() {
		detectedInterpreter = detectInterpreter()
	})
	return detectedInterpreter
}

func shellCommand(ctx context.Context, scriptPath string) *exec.Cmd {
	interp := shellInterpreter()
	name := strings.ToLower(filepath.Base(interp))
	switch {
	case strings.Contains(name, "powershell") || strings.Contains(name, "pwsh"):
		return exec.CommandContext(ctx, interp, "-NoProfile", "-File", scriptPath)
	case strings.Contains(name, "cmd"):
		return exec.CommandContext(ctx, interp, "/C", scriptPath)
	default:
		return exec.CommandContext(ctx, interp, scriptPath)
	}
}
