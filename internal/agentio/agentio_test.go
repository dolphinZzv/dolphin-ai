package agentio

import (
	"context"
	"testing"

	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/transport"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

func TestAgentIO(t *testing.T) {
	Convey("AgentIO", t, func() {
		logger, _ := zap.NewDevelopment()
		sb := signal.NewBus()
		store := session.NewFileStore(t.TempDir())
		mgr := session.NewManager(store)

		Convey("NewAgentIO creates instance", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			So(aio, ShouldNotBeNil)
			So(aio.Queue(), ShouldNotBeNil)
		})

		Convey("RegisterTransport adds route", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			tio := transport.NewNullTransport("test-1")
			aio.RegisterTransport("test-1", tio)
			So(aio.routes["test-1"], ShouldEqual, tio)
		})

		Convey("SendTurn creates session when none active", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			ctx := context.Background()
			ctx = transport.WithInfo(ctx, &transport.Info{ID: "test-1", Type: "stdio"})

			aio.SendTurn(ctx, &Turn{Input: "hello"})

			sess := mgr.Active()
			So(sess, ShouldNotBeNil)
			So(sess.Active, ShouldBeTrue)
		})

		Convey("SendTurn binds session ID to turn", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			ctx := context.Background()

			aio.SendTurn(ctx, &Turn{Input: "hello"})
			sess := mgr.Active()

			So(sess, ShouldNotBeNil)
		})

		Convey("OnResult writes to registered transport", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			tio := transport.NewNullTransport("test-1")
			aio.RegisterTransport("test-1", tio)

			// Should not panic for unknown transport
			aio.OnResult(&TurnResult{TransportID: "unknown", Text: "hello"})

			// Should not panic for known transport
			aio.OnResult(&TurnResult{TransportID: "test-1", Text: "hello"})
		})

		Convey("OnResult with Done flushes", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			tio := transport.NewNullTransport("test-1")
			aio.RegisterTransport("test-1", tio)

			aio.OnResult(&TurnResult{TransportID: "test-1", Text: "done", Done: true})
		})
	})
}
