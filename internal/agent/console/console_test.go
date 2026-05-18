package console

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"dolphin/internal/transport"
)

type mockIO struct {
	writes bytes.Buffer
}

func (m *mockIO) ReadLine() (string, error)           { return "", io.EOF }
func (m *mockIO) WriteLine(s string) error             { m.writes.WriteString(s + "\n"); return nil }
func (m *mockIO) WriteString(s string) error            { m.writes.WriteString(s); return nil }
func (m *mockIO) Capabilities() transport.Capabilities  { return transport.Capabilities{} }
func (m *mockIO) Context() string                       { return "" }
func (m *mockIO) Name() string                          { return "mock" }

func TestExecuteNonCommand(t *testing.T) {
	c := New()
	io := &mockIO{}

	if got := c.Execute("hello", io); got {
		t.Error("Execute('hello') = true, want false")
	}
}

func TestExecuteEmptyLine(t *testing.T) {
	c := New()
	io := &mockIO{}

	if got := c.Execute("", io); got {
		t.Error("Execute('') = true, want false")
	}
}

func TestExecuteSlashOnly(t *testing.T) {
	c := New()
	io := &mockIO{}

	if got := c.Execute("/", io); got {
		t.Error("Execute('/') = true, want false")
	}
}

func TestExecuteUnknownRoot(t *testing.T) {
	c := New()
	io := &mockIO{}

	if got := c.Execute("/unknown", io); got {
		t.Error("Execute('/unknown') = true, want false")
	}
}

func TestExecuteBasicCommand(t *testing.T) {
	c := New()
	io := &mockIO{}
	var called bool
	c.Add(&Command{
		Name: "test",
		Desc: "test command",
		Handler: func(_ []string, _ transport.UserIO) {
			called = true
		},
	})

	if got := c.Execute("/test", io); !got {
		t.Error("Execute('/test') = false, want true")
	}
	if !called {
		t.Error("handler was not called")
	}
}

func TestExecuteWithArgs(t *testing.T) {
	c := New()
	io := &mockIO{}
	var gotArgs []string
	c.Add(&Command{
		Name: "test",
		Handler: func(args []string, _ transport.UserIO) {
			gotArgs = args
		},
	})

	c.Execute("/test arg1 arg2", io)
	if len(gotArgs) != 2 || gotArgs[0] != "arg1" || gotArgs[1] != "arg2" {
		t.Errorf("handler got args = %v, want [arg1 arg2]", gotArgs)
	}
}

func TestExecuteSubcommand(t *testing.T) {
	c := New()
	io := &mockIO{}
	var called bool
	c.Add(&Command{
		Name: "skills",
		Desc: "skills management",
		Children: []*Command{
			{
				Name: "list",
				Desc: "list skills",
				Handler: func(_ []string, _ transport.UserIO) {
					called = true
				},
			},
		},
		Handler: func(_ []string, _ transport.UserIO) {
			t.Error("parent handler should not be called")
		},
	})

	if got := c.Execute("/skills list", io); !got {
		t.Error("Execute('/skills list') = false, want true")
	}
	if !called {
		t.Error("subcommand handler was not called")
	}
}

func TestExecuteSubcommandWithArgs(t *testing.T) {
	c := New()
	io := &mockIO{}
	var gotArgs []string
	c.Add(&Command{
		Name: "skills",
		Children: []*Command{
			{
				Name: "show",
				Handler: func(args []string, _ transport.UserIO) {
					gotArgs = args
				},
			},
		},
	})

	c.Execute("/skills show my-skill", io)
	if len(gotArgs) != 1 || gotArgs[0] != "my-skill" {
		t.Errorf("handler got args = %v, want [my-skill]", gotArgs)
	}
}

func TestExecuteUnknownSubcommand(t *testing.T) {
	c := New()
	io := &mockIO{}
	c.Add(&Command{
		Name: "skills",
		Desc: "skills management",
		Children: []*Command{
			{Name: "list", Desc: "list", Handler: func(_ []string, _ transport.UserIO) {}},
		},
		Handler: func(_ []string, _ transport.UserIO) {},
	})

	if got := c.Execute("/skills bad", io); !got {
		t.Error("Execute('/skills bad') = false, want true")
	}
	output := io.writes.String()
	if !strings.Contains(output, "Unknown subcommand") {
		t.Errorf("expected 'Unknown subcommand' in output, got: %s", output)
	}
	if !strings.Contains(output, "Available: list") {
		t.Errorf("expected 'Available: list' in output, got: %s", output)
	}
}

func TestExecuteNoHandlerButHasChildren(t *testing.T) {
	c := New()
	io := &mockIO{}
	c.Add(&Command{
		Name: "skills",
		Desc: "skills management",
		Children: []*Command{
			{Name: "list", Desc: "List all skills", Handler: func(_ []string, _ transport.UserIO) {}},
			{Name: "show", Desc: "Show a skill", Handler: func(_ []string, _ transport.UserIO) {}},
		},
	})

	if got := c.Execute("/skills", io); !got {
		t.Error("Execute('/skills') = false, want true")
	}
	output := io.writes.String()
	if !strings.Contains(output, "Usage: /skills <command>") {
		t.Errorf("expected usage line in output, got: %s", output)
	}
	if !strings.Contains(output, "list") || !strings.Contains(output, "show") {
		t.Errorf("expected subcommands in output, got: %s", output)
	}
}

func TestExecuteNoHandlerNoChildren(t *testing.T) {
	c := New()
	io := &mockIO{}
	c.Add(&Command{
		Name: "empty",
		Desc: "empty command",
	})

	if got := c.Execute("/empty", io); got {
		t.Error("Execute('/empty') = true, want false")
	}
}

func TestExecuteDeepSubcommand(t *testing.T) {
	c := New()
	io := &mockIO{}
	var called bool
	c.Add(&Command{
		Name: "config",
		Children: []*Command{
			{
				Name: "tools",
				Children: []*Command{
					{
						Name: "list",
						Handler: func(_ []string, _ transport.UserIO) {
							called = true
						},
					},
				},
			},
		},
	})

	if got := c.Execute("/config tools list", io); !got {
		t.Error("Execute('/config tools list') = false, want true")
	}
	if !called {
		t.Error("deep subcommand handler was not called")
	}
}

func TestMultipleRootCommands(t *testing.T) {
	c := New()
	io := &mockIO{}
	var helpCalled, exitCalled bool

	c.Add(&Command{
		Name: "help",
		Handler: func(_ []string, _ transport.UserIO) {
			helpCalled = true
		},
	})
	c.Add(&Command{
		Name: "exit",
		Handler: func(_ []string, _ transport.UserIO) {
			exitCalled = true
		},
	})

	c.Execute("/help", io)
	if !helpCalled {
		t.Error("help handler was not called")
	}
	if exitCalled {
		t.Error("exit handler should not have been called")
	}
}
