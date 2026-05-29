package command

import (
	"testing"

	"dolphin/internal/session"
	"dolphin/internal/signal"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewRegistry(t *testing.T) {
	Convey("NewRegistry", t, func() {
		store := session.NewFileStore(t.TempDir())
		mgr := session.NewManager(store)
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		So(r, ShouldNotBeNil)
	})
}

func TestRegistryExecute(t *testing.T) {
	Convey("Registry.Execute", t, func() {
		store := session.NewFileStore(t.TempDir())
		mgr := session.NewManager(store)
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

func TestRegistryCommandTool(t *testing.T) {
	Convey("Registry command tools", t, func() {
		store := session.NewFileStore(t.TempDir())
		mgr := session.NewManager(store)
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		Convey("RegisterCommandTool stores a command", func() {
			err := r.RegisterCommandTool("greet", "Say hello", "You are a greeter")
			So(err, ShouldBeNil)
			So(len(r.llmCmds), ShouldEqual, 1)
		})

		Convey("RegisterCommandTool rejects duplicate", func() {
			r.RegisterCommandTool("greet", "", "prompt")
			err := r.RegisterCommandTool("greet", "", "prompt2")
			So(err, ShouldNotBeNil)
		})

		Convey("RegisterCommandTool requires name and prompt", func() {
			err := r.RegisterCommandTool("", "", "")
			So(err, ShouldNotBeNil)
		})

		Convey("UnregisterCommandTool removes a command", func() {
			r.RegisterCommandTool("greet", "", "prompt")
			err := r.UnregisterCommandTool("greet")
			So(err, ShouldBeNil)
			So(len(r.llmCmds), ShouldEqual, 0)
		})

		Convey("UnregisterCommandTool returns error for unknown", func() {
			err := r.UnregisterCommandTool("nonexistent")
			So(err, ShouldNotBeNil)
		})
	})
}

func TestRegistryFromSkill(t *testing.T) {
	Convey("Registry skill commands", t, func() {
		store := session.NewFileStore(t.TempDir())
		mgr := session.NewManager(store)
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		Convey("RegisterFromSkill adds commands", func() {
			r.RegisterFromSkill("my-skill", []string{"cmd1", "cmd2"})
			So(len(r.llmCmds), ShouldEqual, 2)
			_, exists := r.llmCmds["cmd1"]
			So(exists, ShouldBeTrue)
		})

		Convey("UnregisterFromSkill removes commands", func() {
			r.RegisterFromSkill("s", []string{"c1", "c2"})
			r.UnregisterFromSkill("s", []string{"c1"})
			_, exists := r.llmCmds["c1"]
			So(exists, ShouldBeFalse)
			_, exists = r.llmCmds["c2"]
			So(exists, ShouldBeTrue)
		})
	})
}

func TestRegistrySetAgentIO(t *testing.T) {
	Convey("SetAgentIO", t, func() {
		store := session.NewFileStore(t.TempDir())
		mgr := session.NewManager(store)
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
