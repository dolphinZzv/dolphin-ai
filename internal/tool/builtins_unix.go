//go:build unix

package tool

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the child process in its own session/process
// group (Setpgid) so the whole tree can be signalled together, and arranges
// for context cancellation to kill that group rather than just the direct
// child. Without this, `sh -c "sleep 2"` leaves `sleep` running as an
// orphan holding the stdout pipe open after `sh` is killed, blocking the
// reader goroutine until the grandchild exits on its own.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Negative PID signals the entire process group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
