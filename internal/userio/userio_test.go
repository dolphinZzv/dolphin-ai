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
		uio := NewUserIO(aio, cmdReg, nil, mgr)

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
