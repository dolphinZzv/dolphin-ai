//go:build !windows

package shell

import (
	"context"
	"testing"
)

func TestShellCommandUnix(t *testing.T) {
	cmd := shellCommand(context.Background(), "echo hello")
	if cmd == nil {
		t.Fatal("shellCommand() returned nil")
	}
	args := cmd.Args
	if len(args) < 3 || args[0] != "sh" || args[1] != "-c" || args[2] != "echo hello" {
		t.Errorf("shellCommand args = %v, want [sh -c echo hello]", args)
	}
}
