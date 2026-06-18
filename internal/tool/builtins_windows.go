//go:build windows

package tool

import "os/exec"

// configureProcessGroup is a no-op on Windows; the pipe-close on context
// cancellation (see shellHandler) is what unblocks the reader goroutine
// there. Windows process-tree kills would require a job object.
func configureProcessGroup(cmd *exec.Cmd) {}
