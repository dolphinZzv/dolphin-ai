package userio

import (
	"context"
	"testing"

	"dolphin/internal/agentio"
	"dolphin/internal/command"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/transport"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

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

			uio.Handle(ctx, tio, "/version")
			uio.Handle(ctx, tio, "/session new")

			sess := mgr.Active()
			So(sess, ShouldNotBeNil)
			So(sess.Active, ShouldBeTrue)
		})

		Convey("Handle sends normal input to agent IO", func() {
			tio := transport.NewNullTransport("test")
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test", Type: "stdio"})

			uio.Handle(ctx, tio, "/session new")
			uio.Handle(ctx, tio, "hello world")

			So(len(aio.Queue()), ShouldEqual, 1)
		})

		Convey("Handle creates detached session without affecting active", func() {
			tio := transport.NewNullTransport("test")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test", Type: "null"})

			So(mgr.Active(), ShouldBeNil)

			uio.Handle(ctx, tio, "create detached session")

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

			uio.Handle(ctx, tio, "hello")

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

			uio.Handle(ctx, tio, "hello")

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

			result := uio.Handle(ctx, tio, "/echo hello")
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

			uio.Handle(ctx, tio, "/session new")
			So(tio.calledSetSession, ShouldBeTrue)
		})

		Convey("calls SetSession after clear", func() {
			tio := &setSessionTransport{NullTransport: *transport.NewNullTransport("test")}
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test"})

			uio.Handle(ctx, tio, "/clear")
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
			tio := &metaTransport{NullTransport: *transport.NewNullTransport("test"),
				uid: "u1", nick: "Alice", cid: "conv-1",
			}
			tio.SetSessionManager(mgr)
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test"})

			uio.Handle(ctx, tio, "hello")

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

			uio.Handle(ctx, tio, "/totally_made_up_cmd_xyz")

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

var _ interface{ SetSession(*session.Session) } = (*setSessionTransport)(nil)
var _ interface {
	UserID() string
	UserNick() string
} = (*metaTransport)(nil)
var _ interface{ ConversationID() string } = (*metaTransport)(nil)
