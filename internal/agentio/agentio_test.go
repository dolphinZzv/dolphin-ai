package agentio

import (
	"context"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"

	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/transport"
)

type writeRecorderTransport struct {
	transport.NullTransport
	mu      sync.Mutex
	writes  []string
	flushes int
}

func newWriteRecorderTransport(id string) *writeRecorderTransport {
	return &writeRecorderTransport{NullTransport: *transport.NewNullTransport(id)}
}

func (w *writeRecorderTransport) Write(_ context.Context, msg string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, msg)
	return nil
}

func (w *writeRecorderTransport) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushes++
	return nil
}

func (w *writeRecorderTransport) Written() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := make([]string, len(w.writes))
	copy(r, w.writes)
	return r
}

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

		Convey("session flip broadcasts to all transports", func() {
			mgr2 := session.NewManager(t.TempDir())
			aio := NewAgentIO(10, mgr2, sb, logger, "Dolphin")
			t1 := newWriteRecorderTransport("t1")
			t2 := newWriteRecorderTransport("t2")
			aio.RegisterTransport("t1", t1)
			aio.RegisterTransport("t2", t2)

			s1 := mgr2.Create(context.Background())
			_, _ = mgr2.SwitchTo(context.Background(), s1.ID)

			w1 := t1.Written()
			w2 := t2.Written()
			So(len(w1), ShouldBeGreaterThan, 0)
			So(len(w2), ShouldBeGreaterThan, 0)
			So(w1[0], ShouldContainSubstring, s1.ID)
			So(w2[0], ShouldContainSubstring, s1.ID)
		})

		Convey("PopIndex removes turn from pending and marks cancelled", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test-1"})

			aio.SendTurn(ctx, &Turn{Input: "first"})
			aio.SendTurn(ctx, &Turn{Input: "second"})

			pending, _, _ := aio.QueueSnapshot()
			So(len(pending), ShouldEqual, 2)

			popped := aio.PopIndex(0)
			So(popped, ShouldNotBeNil)
			So(popped.Input, ShouldEqual, "first")
			So(aio.IsCancelled(popped.TurnID), ShouldBeTrue)

			pending2, _, _ := aio.QueueSnapshot()
			So(len(pending2), ShouldEqual, 1)
			So(pending2[0].Input, ShouldEqual, "second")
		})

		Convey("PopIndex out of bounds returns nil", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			So(aio.PopIndex(-1), ShouldBeNil)
			So(aio.PopIndex(0), ShouldBeNil)
			So(aio.PopIndex(100), ShouldBeNil)
		})

		Convey("IsCancelled returns false for unknown turn", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			So(aio.IsCancelled("nonexistent"), ShouldBeFalse)
		})

		Convey("OnTurnDequeued cleans cancelled state", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test-1"})

			aio.SendTurn(ctx, &Turn{Input: "hello"})
			pending, _, _ := aio.QueueSnapshot()
			turn := pending[0]
			aio.cancelled[turn.TurnID] = true

			aio.OnTurnDequeued(turn)
			So(aio.IsCancelled(turn.TurnID), ShouldBeFalse)
		})

		Convey("QueueSnapshot returns capacity and processing state", func() {
			aio := NewAgentIO(42, mgr, sb, logger, "Dolphin")
			_, cap, proc := aio.QueueSnapshot()
			So(cap, ShouldEqual, 42)
			So(proc, ShouldBeFalse)

			aio.SetActive("worker-1", &Turn{TurnID: "t1", SessionID: "s1", Input: "hi"})
			_, _, proc = aio.QueueSnapshot()
			So(proc, ShouldBeTrue)
		})

		Convey("ClearActive removes worker from active turns", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			aio.SetActive("worker-1", &Turn{TurnID: "t1", SessionID: "s1", Input: "hi"})
			So(aio.Processing(), ShouldBeTrue)

			aio.ClearActive("worker-1")
			So(aio.Processing(), ShouldBeFalse)
		})

		Convey("PriorityQueue returns priority channel", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			So(aio.PriorityQueue(), ShouldNotBeNil)
			So(aio.PriorityQueue(), ShouldEqual, aio.priority)
		})

		Convey("SendTurnPriority creates session when none active", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test-1", Type: "stdio"})

			aio.SendTurnPriority(ctx, &Turn{Input: "priority hello"})

			sess := mgr.Active()
			So(sess, ShouldNotBeNil)
			So(sess.Active, ShouldBeTrue)
		})

		Convey("SendTurnPriority prepends to pending queue", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test-1"})

			aio.SendTurn(ctx, &Turn{Input: "regular"})
			aio.SendTurnPriority(ctx, &Turn{Input: "priority"})

			pending, _, _ := aio.QueueSnapshot()
			So(len(pending), ShouldEqual, 2)
			So(pending[0].Input, ShouldEqual, "priority")
			So(pending[1].Input, ShouldEqual, "regular")
		})

		Convey("SendTurnPriority falls back to last transport when ctx has no info", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "test-1"})

			aio.SendTurn(ctx, &Turn{Input: "first"})
			aio.SendTurnPriority(context.Background(), &Turn{Input: "second"})

			pending, _, _ := aio.QueueSnapshot()
			So(len(pending), ShouldEqual, 2)
			So(pending[0].TransportID, ShouldEqual, "test-1")
		})

		Convey("SendTurnPriority uses existing transport ID when set", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")

			aio.SendTurnPriority(context.Background(), &Turn{Input: "hi", TransportID: "explicit-id"})

			pending, _, _ := aio.QueueSnapshot()
			So(len(pending), ShouldEqual, 1)
			So(pending[0].TransportID, ShouldEqual, "explicit-id")
		})

		Convey("ActiveSnapshot returns a copy of active turns", func() {
			aio := NewAgentIO(10, mgr, sb, logger, "Dolphin")
			aio.SetActive("worker-1", &Turn{TurnID: "t1", SessionID: "s1", Input: "hello"})
			aio.SetActive("worker-2", &Turn{TurnID: "t2", SessionID: "s2", Input: "world"})

			snap := aio.ActiveSnapshot()
			So(len(snap), ShouldEqual, 2)
			So(snap["worker-1"].TurnID, ShouldEqual, "t1")
			So(snap["worker-1"].Input, ShouldEqual, "hello")
			So(snap["worker-2"].TurnID, ShouldEqual, "t2")

			// Verify it's a copy, not the original map
			snap["worker-3"] = &TurnInfo{TurnID: "t3"}
			_, _, proc := aio.QueueSnapshot()
			So(proc, ShouldBeTrue) // still only 2 active
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
