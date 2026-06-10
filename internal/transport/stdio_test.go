package transport

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestStdioGetters(t *testing.T) {
	Convey("Stdio getters", t, func() {
		s := NewStdio("testuser")

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
		s := NewStdio("")
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
	Convey("Close with readline returns no error", t, func() {
		s := NewStdio("testuser")
		So(s.rl, ShouldNotBeNil)

		err := s.Close()
		So(err, ShouldBeNil)
	})
}

func TestStdioMaybeExit_NoMatch(t *testing.T) {
	Convey("maybeExit returns input unchanged for non-exit commands", t, func() {
		s := NewStdio("test")
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

		s := NewStdio("test")

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

			s := NewStdio("test")

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
		s := NewStdio("test")
		err := s.Write(context.Background(), "hello")
		So(err, ShouldBeNil)
	})
}

func TestStdioCapability(t *testing.T) {
	Convey("Stdio.Capability returns interactive+streamable+nestRead", t, func() {
		s := NewStdio("test")
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
