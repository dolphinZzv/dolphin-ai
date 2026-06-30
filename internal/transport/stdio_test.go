package transport

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/chzyer/readline"
	. "github.com/smartystreets/goconvey/convey"
)

func TestStdioGetters(t *testing.T) {
	Convey("Stdio getters", t, func() {
		s := NewStdio("testuser", "Dolphin")

		Convey("ID returns stdio", func() {
			So(s.ID(), ShouldEqual, "stdio")
		})

		Convey("Context returns empty", func() {
			So(s.Context(), ShouldEqual, "")
		})

		Convey("Tools returns nil", func() {
			So(s.Tools(), ShouldBeNil)
		})

		Convey("Start returns nil", func() {
			err := s.Start(context.Background())
			So(err, ShouldBeNil)
		})
	})
}

func TestNullTransportGetters(t *testing.T) {
	Convey("NullTransport getters", t, func() {
		n := NewNullTransport("test")

		Convey("ID returns given id", func() {
			So(n.ID(), ShouldEqual, "test")
		})

		Convey("Context returns empty", func() {
			So(n.Context(), ShouldEqual, "")
		})

		Convey("Tools returns nil", func() {
			So(n.Tools(), ShouldBeNil)
		})

		Convey("Start returns nil", func() {
			err := n.Start(context.Background())
			So(err, ShouldBeNil)
		})
	})
}

func TestNullTransportIO(t *testing.T) {
	Convey("NullTransport IO operations", t, func() {
		n := NewNullTransport("test")

		Convey("Write returns nil", func() {
			err := n.Write(context.Background(), "hello")
			So(err, ShouldBeNil)
		})

		Convey("Flush returns nil", func() {
			err := n.Flush()
			So(err, ShouldBeNil)
		})

		Convey("Read returns EOF on empty", func() {
			_, err := n.Read(context.Background())
			So(err, ShouldEqual, io.EOF)
		})

		Convey("RequestPermission returns PermissionDenied", func() {
			result, err := n.RequestPermission(context.Background(), "test")
			So(err, ShouldBeNil)
			So(result, ShouldEqual, PermissionDenied)
		})
	})
}

func TestNewStdio_EmptyUser(t *testing.T) {
	Convey("NewStdio with empty user", t, func() {
		s := NewStdio("", "Dolphin")
		So(s, ShouldNotBeNil)
		So(s.id, ShouldEqual, "stdio")
	})
}

func TestStdioClose_WithoutReadline(t *testing.T) {
	Convey("Close without readline does not panic", t, func() {
		s := &Stdio{
			SessionHolder: NewSessionHolder(nil),
			id:            "stdio",
			user:          "test",
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.ctx = ctx
		s.cancel = cancel

		err := s.Close()
		So(err, ShouldBeNil)
	})
}

func TestStdioClose_WithReadline(t *testing.T) {
	if isRace {
		t.Skip("skipping: readline has a known race under -race")
	}
	Convey("Close with readline returns no error", t, func() {
		s := NewStdio("testuser", "Dolphin")
		So(s.rl, ShouldNotBeNil)

		err := s.Close()
		So(err, ShouldBeNil)
	})
}

func TestStdioMaybeExit_NoMatch(t *testing.T) {
	Convey("maybeExit returns input unchanged for non-exit commands", t, func() {
		s := NewStdio("test", "Dolphin")
		result := s.maybeExit("hello")
		So(result, ShouldEqual, "hello")

		result = s.maybeExit("/help")
		So(result, ShouldEqual, "/help")

		result = s.maybeExit("")
		So(result, ShouldEqual, "")
	})
}

func TestStdioMaybeExit_Cancel(t *testing.T) {
	Convey("maybeExit returns empty when user cancels exit", t, func() {
		r, w, err := os.Pipe()
		So(err, ShouldBeNil)

		oldStdin := os.Stdin
		t.Cleanup(func() { os.Stdin = oldStdin })
		os.Stdin = r

		s := NewStdio("test", "Dolphin")

		go func() {
			fmt.Fprint(w, "n\n")
			w.Close()
		}()

		result := s.maybeExit("exit")
		So(result, ShouldEqual, "")
	})
}

func TestStdioMaybeExit_Variants(t *testing.T) {
	Convey("maybeExit handles all exit variants", t, func() {
		variants := []string{"exit", "quit", "/exit", "/quit"}
		for _, v := range variants {
			r, w, err := os.Pipe()
			So(err, ShouldBeNil)

			oldStdin := os.Stdin
			os.Stdin = r
			t.Cleanup(func() { os.Stdin = oldStdin })

			s := NewStdio("test", "Dolphin")

			go func() {
				fmt.Fprint(w, "n\n")
				w.Close()
			}()

			result := s.maybeExit(v)
			So(result, ShouldEqual, "")
		}
	})
}

func TestStdioWriteReturnsNil(t *testing.T) {
	Convey("Stdio.Write returns nil", t, func() {
		s := NewStdio("test", "Dolphin")
		err := s.Write(context.Background(), "hello")
		So(err, ShouldBeNil)
	})
}

func TestStdioCapability(t *testing.T) {
	Convey("Stdio.Capability returns interactive+streamable+nestRead", t, func() {
		s := NewStdio("test", "Dolphin")
		c := s.Capability()
		So(c.Interactive, ShouldBeTrue)
		So(c.Streamable, ShouldBeTrue)
		So(c.NestRead, ShouldBeTrue)
		So(c.RenderTextMarkdown, ShouldEqual, "none")
	})
}

func TestStdioRunInteractive_NoReadline(t *testing.T) {
	Convey("RunInteractive with no readline does not panic and returns error for non-existent cmd", t, func() {
		s := &Stdio{
			SessionHolder: NewSessionHolder(nil),
			id:            "stdio",
			ctx:           context.Background(),
			user:          "test",
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := s.RunInteractive(ctx, "nonexistent_cmd_xyz")
		So(err, ShouldNotBeNil)
	})
}

func TestStdioInit_AgentNameConfig(t *testing.T) {
	Convey("init with agent_name config", t, func() {
		io, err := Build(context.Background(), "stdio", map[string]any{
			"agent_name": "CustomBot",
		})
		So(err, ShouldBeNil)
		So(io, ShouldNotBeNil)
		So(io.ID(), ShouldEqual, "stdio")
	})
}

func TestStdioInit_NoAgentNameConfig(t *testing.T) {
	Convey("init without agent_name config", t, func() {
		io, err := Build(context.Background(), "stdio", nil)
		So(err, ShouldBeNil)
		So(io, ShouldNotBeNil)
	})
}

func TestStdioRequestPermission_FallbackReader(t *testing.T) {
	Convey("RequestPermission with fallback reader (no readline)", t, func() {
		Convey("returns PermissionOnce for '1'", func() {
			r, w, err := os.Pipe()
			So(err, ShouldBeNil)
			oldStdin := os.Stdin
			t.Cleanup(func() { os.Stdin = oldStdin })
			os.Stdin = r

			s := NewStdio("test", "Dolphin")
			s.rl = nil

			go func() {
				_, _ = w.WriteString("1\n")
			}()

			result, err := s.RequestPermission(context.Background(), "test")
			So(err, ShouldBeNil)
			So(result, ShouldEqual, PermissionOnce)
			_ = w.Close()
		})

		Convey("returns PermissionAlways for '2'", func() {
			r, w, err := os.Pipe()
			So(err, ShouldBeNil)
			oldStdin := os.Stdin
			t.Cleanup(func() { os.Stdin = oldStdin })
			os.Stdin = r

			s := NewStdio("test", "Dolphin")
			s.rl = nil

			go func() {
				_, _ = w.WriteString("2\n")
			}()

			result, err := s.RequestPermission(context.Background(), "test")
			So(err, ShouldBeNil)
			So(result, ShouldEqual, PermissionAlways)
			_ = w.Close()
		})

		Convey("returns PermissionDenied for unrecognized input", func() {
			r, w, err := os.Pipe()
			So(err, ShouldBeNil)
			oldStdin := os.Stdin
			t.Cleanup(func() { os.Stdin = oldStdin })
			os.Stdin = r

			s := NewStdio("test", "Dolphin")
			s.rl = nil

			go func() {
				_, _ = w.WriteString("xyz\n")
			}()

			result, err := s.RequestPermission(context.Background(), "test")
			So(err, ShouldBeNil)
			So(result, ShouldEqual, PermissionDenied)
			_ = w.Close()
		})

		Convey("returns PermissionDenied when pipe is closed", func() {
			r, w, err := os.Pipe()
			So(err, ShouldBeNil)
			oldStdin := os.Stdin
			t.Cleanup(func() { os.Stdin = oldStdin })
			os.Stdin = r

			s := NewStdio("test", "Dolphin")
			s.rl = nil

			_ = w.Close()

			result, err := s.RequestPermission(context.Background(), "test")
			So(err, ShouldBeNil)
			So(result, ShouldEqual, PermissionDenied)
		})
	})
}

func TestStdioRead_ReadlineCtxCancel(t *testing.T) {
	if isRace {
		t.Skip("skipping: readline has a known race under -race")
	}
	Convey("Read with readline and cancelled context returns error", t, func() {
		s := NewStdio("test", "Dolphin")
		if s.rl == nil {
			t.Skip("skipping: readline not available (no terminal)")
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := s.Read(ctx)
		So(err, ShouldNotBeNil)
	})
}

func TestStdioRunInteractive_WithReadline(t *testing.T) {
	if isRace {
		t.Skip("skipping: readline has a known race under -race")
	}
	Convey("RunInteractive with readline runs command and returns nil for 'true'", t, func() {
		s := NewStdio("test", "Dolphin")
		if s.rl == nil {
			t.Skip("skipping: readline not available (no terminal)")
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := s.RunInteractive(ctx, "true")
		So(err, ShouldBeNil)
		// readline may or may not be re-created depending on terminal availability
	})
}

func TestStdioMaybeExit_Yes(t *testing.T) {
	Convey("maybeExit calls osExit when user confirms with yes", t, func() {
		r, w, err := os.Pipe()
		So(err, ShouldBeNil)

		oldStdin := os.Stdin
		t.Cleanup(func() { os.Stdin = oldStdin })
		os.Stdin = r

		s := NewStdio("test", "Dolphin")

		exited := make(chan int, 1)
		oldOsExit := osExit
		osExit = func(code int) { exited <- code }
		t.Cleanup(func() { osExit = oldOsExit })

		go func() {
			_, _ = fmt.Fprint(w, "y\n")
		}()

		result := s.maybeExit("exit")
		So(result, ShouldEqual, "")

		code := <-exited
		So(code, ShouldEqual, 0)
		_ = w.Close()
	})
}

func TestStdioInit_AgentNameNotString(t *testing.T) {
	Convey("init with agent_name as non-string type", t, func() {
		io, err := Build(context.Background(), "stdio", map[string]any{
			"agent_name": 123,
		})
		So(err, ShouldBeNil)
		So(io, ShouldNotBeNil)
		So(io.ID(), ShouldEqual, "stdio")
	})
}

func TestStdioRead_FallbackErrCh(t *testing.T) {
	Convey("Read fallback path returns error when stdin is closed", t, func() {
		r, w, err := os.Pipe()
		So(err, ShouldBeNil)

		oldStdin := os.Stdin
		t.Cleanup(func() { os.Stdin = oldStdin })
		os.Stdin = r

		s := NewStdio("test", "Dolphin")
		s.rl = nil // force fallback reader

		_ = w.Close() // close without writing → ReadString returns io.EOF

		_, err = s.Read(context.Background())
		So(err, ShouldNotBeNil)
	})
}

func TestNewStdio_NonEmptyUser(t *testing.T) {
	Convey("NewStdio with non-empty user", t, func() {
		s := NewStdio("nonemptyuser", "Dolphin")
		So(s, ShouldNotBeNil)
		So(s.user, ShouldEqual, "nonemptyuser")
	})
}

func TestStdioInit_AgentNameEmptyString(t *testing.T) {
	Convey("init with agent_name as empty string", t, func() {
		io, err := Build(context.Background(), "stdio", map[string]any{
			"agent_name": "",
		})
		So(err, ShouldBeNil)
		So(io, ShouldNotBeNil)
		So(io.ID(), ShouldEqual, "stdio")
	})
}

func TestStdioRead_FallbackEmptyLine(t *testing.T) {
	Convey("Read fallback skips empty lines", t, func() {
		r, w, err := os.Pipe()
		So(err, ShouldBeNil)

		oldStdin := os.Stdin
		t.Cleanup(func() { os.Stdin = oldStdin })
		os.Stdin = r

		s := NewStdio("test", "Dolphin")
		s.rl = nil // force fallback reader

		go func() {
			_, _ = w.WriteString("\nworld\n")
			_ = w.Close()
		}()

		result, err := s.Read(context.Background())
		So(err, ShouldBeNil)
		So(result.Text, ShouldEqual, "world")
	})
}

func TestStdioRunInteractive_EmptyUser(t *testing.T) {
	Convey("RunInteractive with empty user does not panic", t, func() {
		s := &Stdio{
			SessionHolder: NewSessionHolder(nil),
			id:            "stdio",
			ctx:           context.Background(),
			user:          "",
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := s.RunInteractive(ctx, "true")
		So(err, ShouldBeNil)
	})
}

func TestStdioRead_ReadlineSuccess(t *testing.T) {
	if isRace {
		t.Skip("skipping: readline has a known race under -race")
	}
	Convey("Read with pipe-backed readline reads a line", t, func() {
		pr, pw, err := os.Pipe()
		So(err, ShouldBeNil)

		rl, err := readline.NewEx(&readline.Config{
			Prompt:       "",
			Stdin:        pr,
			Stdout:       io.Discard,
			Stderr:       io.Discard,
			FuncGetWidth: func() int { return 80 },
		})
		if err != nil {
			t.Skip("readline not available: " + err.Error())
		}
		defer func() { _ = rl.Close() }()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s := &Stdio{
			SessionHolder: NewSessionHolder(nil),
			id:            "stdio",
			user:          "test",
			ctx:           ctx,
			cancel:        cancel,
			rl:            rl,
		}

		go func() {
			_, _ = pw.WriteString("hello\n")
			_ = pw.Close()
		}()

		result, err := s.Read(context.Background())
		So(err, ShouldBeNil)
		So(result.Text, ShouldEqual, "hello")
	})
}

func TestStdioRead_ReadlineEmptyLine(t *testing.T) {
	if isRace {
		t.Skip("skipping: readline has a known race under -race")
	}
	Convey("Read with pipe-backed readline skips empty lines", t, func() {
		pr, pw, err := os.Pipe()
		So(err, ShouldBeNil)

		rl, err := readline.NewEx(&readline.Config{
			Prompt:       "",
			Stdin:        pr,
			Stdout:       io.Discard,
			Stderr:       io.Discard,
			FuncGetWidth: func() int { return 80 },
		})
		if err != nil {
			t.Skip("readline not available: " + err.Error())
		}
		defer func() { _ = rl.Close() }()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s := &Stdio{
			SessionHolder: NewSessionHolder(nil),
			id:            "stdio",
			user:          "test",
			ctx:           ctx,
			cancel:        cancel,
			rl:            rl,
		}

		go func() {
			_, _ = pw.WriteString("\nworld\n")
			_ = pw.Close()
		}()

		result, err := s.Read(context.Background())
		So(err, ShouldBeNil)
		So(result.Text, ShouldEqual, "world")
	})
}

func TestStdioRequestPermission_ReadlineCtxDone(t *testing.T) {
	if isRace {
		t.Skip("skipping: readline has a known race under -race")
	}
	Convey("RequestPermission with readline returns Denied when ctx is done", t, func() {
		pr, pw, err := os.Pipe()
		So(err, ShouldBeNil)
		defer func() { _ = pw.Close() }()

		rl, err := readline.NewEx(&readline.Config{
			Prompt:       "",
			Stdin:        pr,
			Stdout:       io.Discard,
			Stderr:       io.Discard,
			FuncGetWidth: func() int { return 80 },
		})
		if err != nil {
			t.Skip("readline not available: " + err.Error())
		}
		defer func() { _ = rl.Close() }()

		ctx, cancel := context.WithCancel(context.Background())
		s := &Stdio{
			SessionHolder: NewSessionHolder(nil),
			id:            "stdio",
			user:          "test",
			ctx:           ctx,
			cancel:        cancel,
			rl:            rl,
		}
		cancel() // cancel before calling → ctx.Done() should fire

		result, err := s.RequestPermission(context.Background(), "test")
		So(err, ShouldNotBeNil)
		So(result, ShouldEqual, PermissionDenied)
	})
}
