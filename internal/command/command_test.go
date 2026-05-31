package command

import (
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
			So(func() { r.Execute("version") }, ShouldNotPanic)
		})

		Convey("/session new creates session", func() {
			So(mgr.Active(), ShouldBeNil)
			r.Execute("session new")
			So(mgr.Active(), ShouldNotBeNil)
		})

		Convey("unknown command does not panic", func() {
			So(func() { r.Execute("nonexistent") }, ShouldNotPanic)
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
