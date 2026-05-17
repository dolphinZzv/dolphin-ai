//go:build windows

package shell

import (
	"context"
	"os/exec"
	"path/filepath"
	"sync"
)

var (
	detectedShell string
	detectOnce    sync.Once
)

func detectShell() string {
	detectOnce.Do(func() {
		for _, sh := range []string{"pwsh", "powershell", "cmd"} {
			if p, err := exec.LookPath(sh); err == nil {
				detectedShell = p
				return
			}
		}
		detectedShell = "cmd"
	})
	return detectedShell
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	sh := detectShell()
	base := filepath.Base(sh)
	switch base {
	case "powershell", "pwsh":
		return exec.CommandContext(ctx, sh, "-Command", command)
	case "cmd":
		return exec.CommandContext(ctx, sh, "/C", command)
	default:
		return exec.CommandContext(ctx, sh, "-c", command)
	}
}
