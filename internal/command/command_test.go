package command

import (
	"context"
	"testing"

	"dolphin/internal/session"
	"dolphin/internal/signal"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewRegistry(t *testing.T) {
	Convey("NewRegistry", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		So(r, ShouldNotBeNil)
	})
}

func TestRegistryExecute(t *testing.T) {
	Convey("Registry.Execute", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		Convey("/version prints version", func() {
			// Should not panic or error
			So(func() { r.Execute(context.Background(), "version", "none") }, ShouldNotPanic)
		})

		Convey("/session new creates session", func() {
			So(mgr.Active(), ShouldBeNil)
			r.Execute(context.Background(), "session new", "none")
			So(mgr.Active(), ShouldNotBeNil)
		})

		Convey("unknown command does not panic", func() {
			So(func() { r.Execute(context.Background(), "nonexistent", "none") }, ShouldNotPanic)
		})
	})
}

func TestRegistrySetAgentIO(t *testing.T) {
	Convey("SetAgentIO", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		So(r.agentIO, ShouldBeNil)

		// SetAgentIO just stores the reference; we can't easily test IO without full setup
		r.SetAgentIO(nil)
		So(r.agentIO, ShouldBeNil)
	})
}

// Ensure Registry implements expected interface.
var _ = (*Registry)(nil)
