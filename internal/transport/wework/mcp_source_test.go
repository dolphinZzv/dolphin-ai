package wework

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

func TestWeWorkTools(t *testing.T) {
	Convey("WeWork Tools()", t, func() {
		Convey("returns MCP executor when configured", func() {
			w := NewWeWork(WeWorkConfig{BotID: "bot", Secret: "secret"}, zap.NewNop(), "")
			tools := w.Tools()
			So(tools, ShouldHaveLength, 1)
			So(tools[0].Name, ShouldEqual, "wework_mcp")
			So(tools[0].Executor, ShouldNotBeNil)
		})

		Convey("returns nil when BotID is empty", func() {
			w := NewWeWork(WeWorkConfig{BotID: "", Secret: "secret"}, zap.NewNop(), "")
			tools := w.Tools()
			So(tools, ShouldBeNil)
		})

		Convey("returns nil when Secret is empty", func() {
			w := NewWeWork(WeWorkConfig{BotID: "bot", Secret: ""}, zap.NewNop(), "")
			tools := w.Tools()
			So(tools, ShouldBeNil)
		})
	})
}
