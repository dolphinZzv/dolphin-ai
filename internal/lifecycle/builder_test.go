package lifecycle

import (
	"testing"

	"dolphin/internal/config"
	. "github.com/smartystreets/goconvey/convey"
)

func TestBuilder(t *testing.T) {
	Convey("Builder", t, func() {
		cfg := config.LoadConfigFromMap(map[string]any{
			"llm.provider":       "openai",
			"llm.model":          "gpt-4o",
			"llm.openai.api_key": "test-key",
			"llm.max_retries":    0,
			"llm.timeout":        "30s",
			"tool.timeout":       "30s",
			"agent.max_rounds":   10,
			"agent.buffer_size":  10,
			"memory.window":      10,
			"memory.dir":         t.TempDir(),
			"brain.dir":          t.TempDir(),
		})

		Convey("chains all steps without error", func() {
			b := NewBuilder(cfg)
			So(b, ShouldNotBeNil)

			p := b.
				StepLogger().
				StepBuses().
				StepSession().
				StepMemory().
				StepLLM().
				StepTools().
				StepBrain().
				StepAgentIO().
				StepUserIO().
				StepObservability().
				StepTransports().
				Assemble().
				Build()

			So(p, ShouldNotBeNil)
			So(p.logger, ShouldNotBeNil)
			So(p.agentIO, ShouldNotBeNil)
			So(p.agentLoop, ShouldNotBeNil)
			So(p.userIO, ShouldNotBeNil)
			So(p.sessionMgr, ShouldNotBeNil)
			So(p.eventBus, ShouldNotBeNil)
			So(p.signalBus, ShouldNotBeNil)
		})

		Convey("steps are idempotent", func() {
			b := NewBuilder(cfg)
			// Call each step twice — second call should be a no-op.
			b2 := b.
				StepLogger().
				StepLogger().
				StepBuses().
				StepBuses()

			So(b2, ShouldEqual, b)
		})

		Convey("Build without Assemble panics", func() {
			b := NewBuilder(cfg)
			So(func() { b.Build() }, ShouldPanic)
		})
	})
}

func TestBuilderCommands(t *testing.T) {
	Convey("Builder commands", t, func() {
		cfg := config.LoadConfigFromMap(map[string]any{
			"llm.provider":       "openai",
			"llm.model":          "gpt-4o",
			"llm.openai.api_key": "test-key",
			"llm.max_retries":    0,
			"llm.timeout":        "30s",
			"tool.timeout":       "30s",
			"agent.max_rounds":   10,
			"agent.buffer_size":  10,
			"memory.window":      10,
			"memory.dir":         t.TempDir(),
			"brain.dir":          t.TempDir(),
		})

		Convey("mcp command lists loaded tools", func() {
			b := NewBuilder(cfg).
				StepLogger().
				StepBuses().
				StepSession().
				StepMemory().
				StepLLM().
				StepTools()

			// Execute the mcp command directly.
			out := b.cmdReg.Execute("mcp", "none")
			So(out, ShouldContainSubstring, "Loaded tools")
		})

		Convey("skills command shows no skills when empty", func() {
			b := NewBuilder(cfg).
				StepLogger().
				StepBuses().
				StepSession().
				StepMemory().
				StepLLM().
				StepTools()

			out := b.cmdReg.Execute("skills list", "none")
			So(out, ShouldContainSubstring, "No skills available")
		})

		Convey("context command prints system prompt", func() {
			b := NewBuilder(cfg).
				StepLogger().
				StepBuses().
				StepSession().
				StepMemory().
				StepLLM().
				StepTools().
				StepBrain()

			out := b.cmdReg.Execute("context", "none")
			So(out, ShouldNotBeEmpty)
		})
	})
}
