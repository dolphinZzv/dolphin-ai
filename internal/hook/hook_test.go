package hook

import (
	"context"
	"sync/atomic"
	"testing"

	"dolphin/internal/event"

	. "github.com/smartystreets/goconvey/convey"
)

type testHandler struct {
	name   string
	count  atomic.Int32
	events []event.Event
}

func (h *testHandler) Name() string { return h.name }
func (h *testHandler) Handle(ctx context.Context, e event.Event) error {
	h.count.Add(1)
	h.events = append(h.events, e)
	return nil
}

func TestNewRegistry(t *testing.T) {
	Convey("NewRegistry", t, func() {
		r := NewRegistry()
		So(r, ShouldNotBeNil)
	})
}

func TestRegistryRegister(t *testing.T) {
	Convey("Registry.Register", t, func() {
		r := NewRegistry()
		h := &testHandler{name: "test"}
		r.Register(h)
		So(len(r.handlers), ShouldEqual, 1)
	})
}

func TestRegistryDispatch(t *testing.T) {
	Convey("Registry.Dispatch", t, func() {
		r := NewRegistry()
		ctx := context.Background()

		Convey("dispatches to registered handlers", func() {
			h := &testHandler{name: "h1"}
			r.Register(h)

			r.Dispatch(ctx, event.Event{Type: event.EventPipelineStart})
			So(h.count.Load(), ShouldEqual, 1)
		})

		Convey("dispatches to all handlers", func() {
			h1 := &testHandler{name: "h1"}
			h2 := &testHandler{name: "h2"}
			r.Register(h1)
			r.Register(h2)

			r.Dispatch(ctx, event.Event{Type: event.EventTurnStart})
			So(h1.count.Load(), ShouldEqual, 1)
			So(h2.count.Load(), ShouldEqual, 1)
		})

		Convey("does not panic when no handlers registered", func() {
			So(func() { r.Dispatch(ctx, event.Event{}) }, ShouldNotPanic)
		})
	})
}
