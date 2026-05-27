package registry

import (
	"strings"
	"testing"

	"dolphin/internal/transport"

	"github.com/spf13/cobra"
)

// testIO implements transport.UserIO for testing.
type testIO struct {
	lines []string
}

func (t *testIO) ReadLine() (string, error) { return "", nil }
func (t *testIO) WriteLine(s string) error {
	t.lines = append(t.lines, s)
	return nil
}
func (t *testIO) WriteString(s string) error {
	t.lines = append(t.lines, s)
	return nil
}
func (t *testIO) Flush() error                         { return nil }
func (t *testIO) Capabilities() transport.Capabilities { return transport.Capabilities{} }
func (t *testIO) Context() string                      { return "" }
func (t *testIO) Name() string                         { return "test" }

func TestConsoleDispatcher_Execute_UnknownCommand(t *testing.T) {
	r := New()
	d := NewConsoleDispatcher(r)
	io := &testIO{}

	handled, sig := d.Execute("/nonexistent", io)
	if handled {
		t.Error("expected false for unknown command")
	}
	if sig != SignalNone {
		t.Errorf("expected SignalNone, got %d", sig)
	}
}

func TestConsoleDispatcher_Execute_Simple(t *testing.T) {
	r := New()
	var ran bool
	cmd := &cobra.Command{
		Use: "hello",
		RunE: func(c *cobra.Command, args []string) error {
			ran = true
			fmt := c.OutOrStdout()
			_, _ = fmt.Write([]byte("hello world\n"))
			return nil
		},
	}
	r.Register(&CommandSpec{Cobra: cmd, Category: CatSystem})
	d := NewConsoleDispatcher(r)
	io := &testIO{}

	handled, sig := d.Execute("/hello", io)
	if !handled {
		t.Error("expected handled")
	}
	if sig != SignalNone {
		t.Error("expected no signal")
	}
	if !ran {
		t.Error("command was not executed")
	}
	if len(io.lines) == 0 || io.lines[0] != "hello world" {
		t.Errorf("expected 'hello world', got %v", io.lines)
	}
}

func TestConsoleDispatcher_Execute_WithSignal(t *testing.T) {
	r := New()
	cmd := &cobra.Command{
		Use: "exit",
		RunE: func(c *cobra.Command, args []string) error {
			return nil
		},
	}
	r.Register(&CommandSpec{Cobra: cmd, Signal: SignalExit})
	d := NewConsoleDispatcher(r)
	io := &testIO{}

	handled, sig := d.Execute("/exit", io)
	if !handled {
		t.Error("expected handled")
	}
	if sig != SignalExit {
		t.Errorf("expected SignalExit, got %d", sig)
	}
}

func TestConsoleDispatcher_Execute_Subcommand(t *testing.T) {
	r := New()
	var ran bool
	sub := &cobra.Command{
		Use: "list",
		RunE: func(c *cobra.Command, args []string) error {
			ran = true
			_, _ = c.OutOrStdout().Write([]byte("item1\nitem2\n"))
			return nil
		},
	}
	root := &cobra.Command{
		Use: "skills",
	}
	root.AddCommand(sub)
	r.Register(&CommandSpec{Cobra: root, Category: CatSkills})
	d := NewConsoleDispatcher(r)
	io := &testIO{}

	handled, _ := d.Execute("/skills list", io)
	if !handled {
		t.Error("expected handled")
	}
	if !ran {
		t.Error("subcommand was not executed")
	}
	if len(io.lines) != 2 || strings.Join(io.lines, ",") != "item1,item2" {
		t.Errorf("unexpected output: %v", io.lines)
	}
}

func TestConsoleDispatcher_Execute_Error(t *testing.T) {
	r := New()
	cmd := &cobra.Command{
		Use: "fail",
		RunE: func(c *cobra.Command, args []string) error {
			return errFail
		},
	}
	r.Register(&CommandSpec{Cobra: cmd})
	d := NewConsoleDispatcher(r)
	io := &testIO{}

	handled, _ := d.Execute("/fail", io)
	if !handled {
		t.Error("expected handled even on error")
	}
	if len(io.lines) == 0 || !strings.Contains(io.lines[0], "Error") {
		t.Errorf("expected error message, got %v", io.lines)
	}
}

func TestConsoleDispatcher_Hidden(t *testing.T) {
	r := New()
	cmd := &cobra.Command{Use: "secret"}
	r.Register(&CommandSpec{Cobra: cmd, Hidden: true})
	d := NewConsoleDispatcher(r)
	io := &testIO{}

	handled, _ := d.Execute("/secret", io)
	if handled {
		t.Error("hidden command should not be dispatched")
	}
}

func TestConsoleDispatcher_NonSlash(t *testing.T) {
	r := New()
	d := NewConsoleDispatcher(r)
	io := &testIO{}

	handled, sig := d.Execute("not a command", io)
	if handled {
		t.Error("non-slash line should not be handled")
	}
	if sig != SignalNone {
		t.Error("expected SignalNone")
	}
}

var errFail = &testError{"command failed"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
