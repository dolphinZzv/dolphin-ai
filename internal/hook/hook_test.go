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

		Convey("continues when handler returns error", func() {
			errH := &errHandler{name: "error"}
			okH := &testHandler{name: "ok"}
			r.Register(errH)
			r.Register(okH)

			r.Dispatch(ctx, event.Event{})
			So(okH.count.Load(), ShouldEqual, 1)
		})

		Convey("does not panic when no handlers registered", func() {
			So(func() { r.Dispatch(ctx, event.Event{}) }, ShouldNotPanic)
		})
	})
}

func TestRegistryDispatchCheck(t *testing.T) {
	Convey("Registry.DispatchCheck", t, func() {
		r := NewRegistry()
		ctx := context.Background()

		Convey("returns nil when no handlers registered", func() {
			err := r.DispatchCheck(ctx, event.Event{})
			So(err, ShouldBeNil)
		})

		Convey("returns nil when all handlers succeed", func() {
			r.Register(&testHandler{name: "h1"})
			r.Register(&testHandler{name: "h2"})

			err := r.DispatchCheck(ctx, event.Event{})
			So(err, ShouldBeNil)
		})

		Convey("returns first error and stops dispatching", func() {
			r.Register(&errHandler{name: "e1"})
			called := false
			r.Register(handlerFunc(func(ctx context.Context, e event.Event) error {
				called = true
				return nil
			}))

			err := r.DispatchCheck(ctx, event.Event{})
			So(err, ShouldEqual, errHookFailed)
			So(called, ShouldBeFalse)
		})
	})
}

type errHandler struct{ name string }

func (h *errHandler) Name() string { return h.name }
func (h *errHandler) Handle(_ context.Context, _ event.Event) error {
	return errHookFailed
}

var errHookFailed = &errHookError{"hook failed"}

type errHookError struct{ msg string }

func (e *errHookError) Error() string { return e.msg }

type handlerFunc func(ctx context.Context, e event.Event) error

func (f handlerFunc) Name() string { return "func" }
func (f handlerFunc) Handle(ctx context.Context, e event.Event) error {
	return f(ctx, e)
}

func TestRegisterLLMRequestHook(t *testing.T) {
	Convey("RegisterLLMRequestHook", t, func() {
		// Reset hooks between tests.
		llmHooks = nil

		Convey("registers a hook", func() {
			called := 0
			RegisterLLMRequestHook(func(req any, model, apiType string) {
				called++
			})
			So(len(llmHooks), ShouldEqual, 1)
		})
	})
}

func TestDispatchLLMRequest(t *testing.T) {
	Convey("DispatchLLMRequest", t, func() {
		llmHooks = nil

		Convey("dispatches to all registered hooks", func() {
			var calls []string
			RegisterLLMRequestHook(func(req any, model, apiType string) {
				calls = append(calls, "hook1:"+model)
			})
			RegisterLLMRequestHook(func(req any, model, apiType string) {
				calls = append(calls, "hook2:"+model)
			})

			DispatchLLMRequest(nil, "gpt-4", "openai")
			So(len(calls), ShouldEqual, 2)
			So(calls[0], ShouldEqual, "hook1:gpt-4")
			So(calls[1], ShouldEqual, "hook2:gpt-4")
		})

		Convey("hook can modify request", func() {
			type fakeReq struct{ Model string }
			req := &fakeReq{Model: "original"}
			RegisterLLMRequestHook(func(req any, model, apiType string) {
				if r, ok := req.(*fakeReq); ok {
					r.Model = "modified"
				}
			})

			DispatchLLMRequest(req, "original", "openai")
			So(req.Model, ShouldEqual, "modified")
		})

		Convey("does not panic when no hooks registered", func() {
			So(func() { DispatchLLMRequest(nil, "m", "t") }, ShouldNotPanic)
		})
	})
}
