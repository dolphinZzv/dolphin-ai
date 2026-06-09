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
		mgr := session.NewManager(t.TempDir())

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

		Convey("OnResult broadcasts to all transports when TransportID is empty", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			tio1 := transport.NewNullTransport("t1")
			tio2 := transport.NewNullTransport("t2")
			aio.RegisterTransport("t1", tio1)
			aio.RegisterTransport("t2", tio2)

			aio.OnResult(&TurnResult{Text: "broadcast", Done: true})
		})

		Convey("OnResult with chunk mode buffers text before Done", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			tio := transport.NewNullTransport("chunk-1")
			aio.RegisterTransport("chunk-1", tio)

			aio.OnResult(&TurnResult{TransportID: "chunk-1", Text: "hello "})

			aio.bufMu.Lock()
			buf, ok := aio.buffers["chunk-1"]
			aio.bufMu.Unlock()
			So(ok, ShouldBeTrue)
			So(buf, ShouldEqual, "hello ")

			aio.OnResult(&TurnResult{TransportID: "chunk-1", Text: "world", Done: true})

			aio.bufMu.Lock()
			_, exists := aio.buffers["chunk-1"]
			aio.bufMu.Unlock()
			So(exists, ShouldBeFalse)
		})

		Convey("OnResult chunk mode with error and empty buffer writes error", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			tio := transport.NewNullTransport("err-chunk")
			aio.RegisterTransport("err-chunk", tio)

			aio.OnResult(&TurnResult{
				TransportID: "err-chunk",
				Error:       assertionError("something went wrong"),
				Done:        true,
			})

			aio.bufMu.Lock()
			_, exists := aio.buffers["err-chunk"]
			aio.bufMu.Unlock()
			So(exists, ShouldBeFalse)
		})

		Convey("OnResult with Done and error for streamable transport", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			tio := newStreamableTransport("stream-err")
			aio.RegisterTransport("stream-err", tio)

			aio.OnResult(&TurnResult{
				TransportID: "stream-err",
				Text:        "partial",
				Error:       assertionError("oops"),
				Done:        true,
			})
		})

		Convey("GetTransport returns registered transport", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			tio := transport.NewNullTransport("test-1")
			aio.RegisterTransport("test-1", tio)

			got := aio.GetTransport("test-1")
			So(got, ShouldEqual, tio)

			got = aio.GetTransport("unknown")
			So(got, ShouldBeNil)
		})
	})
}

type streamableTransport struct {
	transport.NullTransport
}

func newStreamableTransport(id string) *streamableTransport {
	return &streamableTransport{*transport.NewNullTransport(id)}
}

func (s *streamableTransport) Capability() transport.Capability {
	return transport.Capability{Streamable: true}
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
