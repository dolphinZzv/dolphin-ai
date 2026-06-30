package userio

import (
	"context"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/brain"
	"dolphin/internal/command"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/transport"
)

type captureWriteTransport struct {
	transport.NullTransport
	written string
}

func (t *captureWriteTransport) Write(_ context.Context, text string) error {
	t.written += text
	return nil
}

func (t *captureWriteTransport) Flush() error { return nil }

type writeFailTransport struct {
	transport.NullTransport
}

func (t *writeFailTransport) Write(_ context.Context, _ string) error {
	return fmt.Errorf("write error")
}

func (t *writeFailTransport) Flush() error { return nil }

type flushFailTransport struct {
	transport.NullTransport
}

func (t *flushFailTransport) Write(_ context.Context, text string) error { return nil }

func (t *flushFailTransport) Flush() error { return fmt.Errorf("flush error") }

func TestUserIO(t *testing.T) {
	Convey("UserIO", t, func() {
		logger, _ := zap.NewDevelopment()
		sb := signal.NewBus()
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, sb)
		aio := agentio.NewAgentIO(10, mgr, sb, logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, nil, mgr, "per_transport")

		Convey("NewUserIO creates instance", func() {
			So(uio, ShouldNotBeNil)
		})

		Convey("Handle routes / commands to cobra", func() {
			tio := transport.NewNullTransport("test")
			ctx := context.Background()

			uio.Handle(ctx, tio, transport.Input{Text: "/version"})
			uio.Handle(ctx, tio, transport.Input{Text: "/session new"})

			sess := mgr.Active()
			So(sess, ShouldNotBeNil)
			So(sess.Active, ShouldBeTrue)
		})

		Convey("Handle sends normal input to agent IO", func() {
			tio := transport.NewNullTransport("test")
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test", Type: "stdio"})

			uio.Handle(ctx, tio, transport.Input{Text: "/session new"})
			uio.Handle(ctx, tio, transport.Input{Text: "hello world"})

			So(len(aio.Queue()), ShouldEqual, 1)
		})

		Convey("Handle creates detached session without affecting active", func() {
			tio := transport.NewNullTransport("test")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test", Type: "null"})

			So(mgr.Active(), ShouldBeNil)

			uio.Handle(ctx, tio, transport.Input{Text: "create detached session"})

			So(mgr.Active(), ShouldBeNil)
		})
	})
}

func TestUserIOSharedMode(t *testing.T) {
	Convey("UserIO shared mode", t, func() {
		logger, _ := zap.NewDevelopment()
		sb := signal.NewBus()
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, sb)
		aio := agentio.NewAgentIO(10, mgr, sb, logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, nil, mgr, "shared")

		Convey("Handle does not store user_id in session.Data in shared mode", func() {
			tio := transport.NewNullTransport("test")
			tio.SetSessionManager(mgr)
			tio.SetSessionMode(true)
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test", Type: "null"})

			uio.Handle(ctx, tio, transport.Input{Text: "hello"})

			sess := mgr.Active()
			So(sess.Get("user_id"), ShouldBeNil)
			So(sess.Get("user_nick"), ShouldBeNil)
			So(sess.Get("conversation_id"), ShouldBeNil)
		})

		Convey("Handle in shared mode still creates session and queues turn", func() {
			tio := transport.NewNullTransport("test")
			tio.SetSessionManager(mgr)
			tio.SetSessionMode(true)
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test", Type: "null"})

			uio.Handle(ctx, tio, transport.Input{Text: "hello"})

			So(len(aio.Queue()), ShouldEqual, 1)
		})
	})
}

func TestIsInteractiveCmd(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"top", nil, true},
		{"less", nil, true},
		{"vim", nil, true},
		{"ssh", nil, true},
		{"fzf", nil, true},
		{"watch", nil, true},
		{"python", nil, true},
		{"python", []string{"script.py"}, false},
		{"python3", nil, true},
		{"node", nil, true},
		{"bash", nil, true},
		{"bash", []string{"-c", "echo hi"}, false},
		{"python", []string{"-c", "print(1)"}, false},
		{"ls", nil, false},
		{"cat", nil, false},
		{"grep", nil, false},
		{"unknown-cmd", nil, false},
	}
	for _, tc := range tests {
		got := isInteractiveCmd(tc.name, tc.args)
		if got != tc.want {
			t.Errorf("isInteractiveCmd(%q, %v) = %v; want %v", tc.name, tc.args, got, tc.want)
		}
	}
}

func TestReadLine(t *testing.T) {
	Convey("ReadLine", t, func() {
		tio := transport.NewNullTransport("test")
		uio := &UserIO{}

		Convey("delegates to transport.Read and returns EOF when empty", func() {
			line, err := uio.ReadLine(context.Background(), tio)
			So(err, ShouldNotBeNil)
			So(line, ShouldEqual, "")
		})
	})
}

func TestWriteLine(t *testing.T) {
	Convey("WriteLine", t, func() {
		tio := transport.NewNullTransport("test")
		uio := &UserIO{}

		Convey("writes text and flushes", func() {
			err := uio.WriteLine(context.Background(), tio, "hello")
			So(err, ShouldBeNil)
		})
	})
}

func TestUserIOHandleSystemCmd(t *testing.T) {
	Convey("Handle with system command", t, func() {
		logger, _ := zap.NewDevelopment()
		sb := signal.NewBus()
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, sb)
		aio := agentio.NewAgentIO(10, mgr, sb, logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, nil, mgr, "per_transport")

		Convey("executes non-interactive system command", func() {
			tio := transport.NewNullTransport("test")
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test", Type: "stdio"})

			result := uio.Handle(ctx, tio, transport.Input{Text: "/echo hello"})
			So(result, ShouldBeFalse)
		})
	})
}

func TestUserIOHandleSetSession(t *testing.T) {
	Convey("Handle with SetSession transport", t, func() {
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, signal.NewBus())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), zap.NewNop(), "Dolphin")
		uio := NewUserIO(aio, cmdReg, nil, mgr, "per_transport")

		Convey("calls SetSession after session new", func() {
			tio := &setSessionTransport{NullTransport: *transport.NewNullTransport("test")}
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test"})

			uio.Handle(ctx, tio, transport.Input{Text: "/session new"})
			So(tio.calledSetSession, ShouldBeTrue)
		})

		Convey("calls SetSession after clear", func() {
			tio := &setSessionTransport{NullTransport: *transport.NewNullTransport("test")}
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test"})

			uio.Handle(ctx, tio, transport.Input{Text: "/clear"})
			So(tio.calledSetSession, ShouldBeTrue)
		})
	})
}

func TestUserIOHandleMetadata(t *testing.T) {
	Convey("Handle stores transport metadata in session", t, func() {
		dir := t.TempDir()
		mgr := session.NewManager(dir)
		cmdReg := command.NewRegistry(mgr, signal.NewBus())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), zap.NewNop(), "Dolphin")
		uio := NewUserIO(aio, cmdReg, nil, mgr, "per_transport")

		Convey("stores user_id, user_nick and conversation_id when transport exposes them", func() {
			tio := &metaTransport{
				NullTransport: *transport.NewNullTransport("test"),
				uid:           "u1", nick: "Alice", cid: "conv-1",
			}
			tio.SetSessionManager(mgr)
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test"})

			uio.Handle(ctx, tio, transport.Input{Text: "hello"})

			select {
			case turn := <-aio.Queue():
				sess := mgr.Get(turn.SessionID)
				So(sess, ShouldNotBeNil)
				So(sess.Get("user_id"), ShouldEqual, "u1")
				So(sess.Get("user_nick"), ShouldEqual, "Alice")
				So(sess.Get("conversation_id"), ShouldEqual, "conv-1")
			default:
				t.Error("expected a turn to be queued")
			}
		})
	})
}

func TestUserIOHandleUnknownCmd(t *testing.T) {
	Convey("Handle with unknown command", t, func() {
		logger, _ := zap.NewDevelopment()
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, signal.NewBus())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, nil, mgr, "per_transport")

		Convey("sends unknown /cmd to LLM with script_not_found message", func() {
			tio := transport.NewNullTransport("test")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test"})

			uio.Handle(ctx, tio, transport.Input{Text: "/totally_made_up_cmd_xyz"})

			select {
			case turn := <-aio.Queue():
				So(turn, ShouldNotBeNil)
				So(turn.Input, ShouldContainSubstring, "totally_made_up_cmd_xyz")
			default:
				t.Error("expected a turn to be queued")
			}
		})
	})
}

func TestUserIOHandleSystemCmdOutput(t *testing.T) {
	Convey("Handle with system command output", t, func() {
		logger, _ := zap.NewDevelopment()
		sb := signal.NewBus()
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, sb)
		aio := agentio.NewAgentIO(10, mgr, sb, logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, nil, mgr, "per_transport")

		Convey("captures non-interactive command output and writes it", func() {
			tio := &captureWriteTransport{NullTransport: *transport.NewNullTransport("test")}
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test", Type: "stdio"})

			result := uio.Handle(ctx, tio, transport.Input{Text: "/echo hello world"})
			So(result, ShouldBeFalse)
			So(tio.written, ShouldContainSubstring, "hello world")
		})
	})
}

func TestUserIOHandleInteractiveCmdOnNullTransport(t *testing.T) {
	Convey("Handle with interactive cmd on non-interactive transport", t, func() {
		logger, _ := zap.NewDevelopment()
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, signal.NewBus())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, nil, mgr, "per_transport")

		Convey("falls through to LLM when transport cannot run interactive", func() {
			tio := transport.NewNullTransport("test")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test"})

			uio.Handle(ctx, tio, transport.Input{Text: "/vim"})

			select {
			case turn := <-aio.Queue():
				So(turn, ShouldNotBeNil)
				So(turn.Input, ShouldContainSubstring, "vim")
			default:
			}
		})
	})
}

func TestWriteLine_WriteError(t *testing.T) {
	Convey("WriteLine with Write error", t, func() {
		uio := &UserIO{}
		tio := &writeFailTransport{}
		err := uio.WriteLine(context.Background(), tio, "hello")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "write error")
	})
}

func TestWriteLine_FlushError(t *testing.T) {
	Convey("WriteLine with Flush error", t, func() {
		uio := &UserIO{}
		tio := &flushFailTransport{}
		err := uio.WriteLine(context.Background(), tio, "hello")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "flush error")
	})
}

func TestUserIOHandleBrainScriptDisabled(t *testing.T) {
	Convey("Handle with disabled brain script", t, func() {
		ctx := context.Background()
		dir := t.TempDir()
		b := brain.New(dir)
		So(b.Init(ctx), ShouldBeNil)

		err := brain.WriteScript(ctx, b, brain.Script{
			Name:    "mytest",
			Enabled: false,
			Content: "echo hello",
		})
		So(err, ShouldBeNil)

		logger, _ := zap.NewDevelopment()
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, signal.NewBus())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, b, mgr, "per_transport")

		tio := &captureWriteTransport{NullTransport: *transport.NewNullTransport("test")}
		ctx = transport.WithInfo(ctx, &transport.Info{ID: "test"})

		result := uio.Handle(ctx, tio, transport.Input{Text: "/mytest"})

		So(result, ShouldBeFalse)
		So(tio.written, ShouldContainSubstring, `script "mytest" is disabled`)
	})
}

func TestUserIOHandleBrainScriptNoContent(t *testing.T) {
	Convey("Handle with enabled brain script but no content", t, func() {
		ctx := context.Background()
		dir := t.TempDir()
		b := brain.New(dir)
		So(b.Init(ctx), ShouldBeNil)

		err := brain.WriteScript(ctx, b, brain.Script{
			Name:    "myempty",
			Enabled: true,
			Content: "",
		})
		So(err, ShouldBeNil)

		logger, _ := zap.NewDevelopment()
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, signal.NewBus())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, b, mgr, "per_transport")

		tio := &captureWriteTransport{NullTransport: *transport.NewNullTransport("test")}
		ctx = transport.WithInfo(ctx, &transport.Info{ID: "test"})

		result := uio.Handle(ctx, tio, transport.Input{Text: "/myempty"})

		So(result, ShouldBeFalse)
		So(tio.written, ShouldContainSubstring, `script "myempty" has no content`)
	})
}

func TestUserIOHandleBrainScriptEnabled(t *testing.T) {
	Convey("Handle with enabled brain script sends turn with script content", t, func() {
		ctx := context.Background()
		dir := t.TempDir()
		b := brain.New(dir)
		So(b.Init(ctx), ShouldBeNil)

		err := brain.WriteScript(ctx, b, brain.Script{
			Name:    "chat",
			Enabled: true,
			Content: "You are a helpful assistant.",
		})
		So(err, ShouldBeNil)

		logger, _ := zap.NewDevelopment()
		mgr := session.NewManager(t.TempDir())
		cmdReg := command.NewRegistry(mgr, signal.NewBus())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "Dolphin")
		uio := NewUserIO(aio, cmdReg, b, mgr, "per_transport")

		tio := transport.NewNullTransport("test")
		ctx = transport.WithInfo(ctx, &transport.Info{ID: "test"})

		result := uio.Handle(ctx, tio, transport.Input{Text: "/chat"})

		So(result, ShouldBeTrue)

		select {
		case turn := <-aio.Queue():
			So(turn.Input, ShouldContainSubstring, "helpful assistant")
		default:
			t.Error("expected a turn to be queued")
		}
	})
}

type setSessionTransport struct {
	transport.NullTransport
	calledSetSession bool
}

func (t *setSessionTransport) SetSession(_ *session.Session) {
	t.calledSetSession = true
}

type metaTransport struct {
	transport.NullTransport
	uid  string
	nick string
	cid  string
}

func (t *metaTransport) UserID() string         { return t.uid }
func (t *metaTransport) UserNick() string       { return t.nick }
func (t *metaTransport) ConversationID() string { return t.cid }

var (
	_ interface{ SetSession(*session.Session) } = (*setSessionTransport)(nil)
	_ interface {
		UserID() string
		UserNick() string
	} = (*metaTransport)(nil)
)
var _ interface{ ConversationID() string } = (*metaTransport)(nil)
