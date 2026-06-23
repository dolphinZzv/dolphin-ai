package command

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"dolphin/internal/session"
	"dolphin/internal/signal"
)

func TestHelpCommand(t *testing.T) {
	Convey("/help command", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		Convey("/help shows available commands", func() {
			output := r.Execute(context.Background(), "help", "none")
			So(output, ShouldNotBeBlank)
			So(output, ShouldContainSubstring, "session")
			So(output, ShouldContainSubstring, "version")
		})

		Convey("/help session shows session commands", func() {
			output := r.Execute(context.Background(), "help session", "none")
			So(output, ShouldNotBeBlank)
			So(output, ShouldContainSubstring, "new")
			So(output, ShouldContainSubstring, "list")
			So(output, ShouldContainSubstring, "stop")
		})

		Convey("HasCommand returns true for help", func() {
			So(r.HasCommand("help"), ShouldBeTrue)
		})
	})
}
