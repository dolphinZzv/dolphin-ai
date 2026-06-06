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
