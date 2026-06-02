package event

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

func TestNewBus(t *testing.T) {
	Convey("NewBus", t, func() {
		bus := NewBus()
		So(bus, ShouldNotBeNil)
	})
}

func TestBusPublishSubscribe(t *testing.T) {
	Convey("Bus publish and subscribe", t, func() {
		bus := NewBus()
		ctx := context.Background()

		Convey("subscriber receives published event", func() {
			var received atomic.Int32
			bus.Subscribe(func(ctx context.Context, e Event) {
				received.Add(1)
			})

			bus.Publish(ctx, Event{
				Type:      EventPipelineStart,
				Timestamp: time.Now(),
				Payload:   map[string]any{"key": "value"},
			})

			So(received.Load(), ShouldEqual, 1)
		})

		Convey("multiple subscribers all receive events", func() {
			var count atomic.Int32
			bus.Subscribe(func(ctx context.Context, e Event) {
				count.Add(1)
			})
			bus.Subscribe(func(ctx context.Context, e Event) {
				count.Add(1)
			})

			bus.Publish(ctx, Event{Type: EventTurnStart})
			So(count.Load(), ShouldEqual, 2)
		})

		Convey("subscriber sees correct event type", func() {
			var eType Type
			bus.Subscribe(func(ctx context.Context, e Event) {
				eType = e.Type
			})

			bus.Publish(ctx, Event{Type: EventLLMStart})
			So(eType, ShouldEqual, EventLLMStart)
		})

		Convey("subscriber sees session ID", func() {
			var sid string
			bus.Subscribe(func(ctx context.Context, e Event) {
				sid = e.SessionID
			})

			bus.Publish(ctx, Event{Type: EventTurnComplete, SessionID: "sess-1"})
			So(sid, ShouldEqual, "sess-1")
		})
	})
}

func TestBusConcurrentPublish(t *testing.T) {
	Convey("Bus handles concurrent publishes safely", t, func() {
		bus := NewBus()
		ctx := context.Background()

		var count atomic.Int32
		bus.Subscribe(func(ctx context.Context, e Event) {
			count.Add(1)
		})

		for range 10 {
			go bus.Publish(ctx, Event{Type: EventPipelineStart})
		}
		time.Sleep(50 * time.Millisecond)
		So(count.Load(), ShouldEqual, 10)
	})
}

func TestEventTypes(t *testing.T) {
	Convey("Event type constants", t, func() {
		Convey("pipeline events", func() {
			So(EventPipelineStart, ShouldEqual, Type("pipeline.start"))
			So(EventPipelineShutdown, ShouldEqual, Type("pipeline.shutdown"))
		})
		Convey("turn events", func() {
			So(EventTurnStart, ShouldEqual, Type("turn.start"))
			So(EventTurnComplete, ShouldEqual, Type("turn.complete"))
			So(EventTurnError, ShouldEqual, Type("turn.error"))
			So(EventTurnInterrupt, ShouldEqual, Type("turn.interrupt"))
		})
		Convey("LLM events", func() {
			So(EventLLMStart, ShouldEqual, Type("llm.start"))
			So(EventLLMComplete, ShouldEqual, Type("llm.complete"))
			So(EventLLMError, ShouldEqual, Type("llm.error"))
			So(EventLLMRetry, ShouldEqual, Type("llm.retry"))
		})
		Convey("tool events", func() {
			So(EventToolStart, ShouldEqual, Type("tool.start"))
			So(EventToolComplete, ShouldEqual, Type("tool.complete"))
			So(EventToolError, ShouldEqual, Type("tool.error"))
		})
	})
}

func TestBusSetLogger(t *testing.T) {
	Convey("SetLogger attaches a logger", t, func() {
		bus := NewBus()
		bus.SetLogger(zap.NewNop())
		bus.Publish(context.Background(), Event{Type: EventPipelineStart})
	})
}

func TestBusSetLoggerSubscribe(t *testing.T) {
	Convey("Subscribe logs when logger is set", t, func() {
		bus := NewBus()
		bus.SetLogger(zap.NewNop())
		bus.Subscribe(func(ctx context.Context, e Event) {})
		bus.Publish(context.Background(), Event{Type: EventPipelineStart})
	})
}
