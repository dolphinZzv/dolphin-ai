package lifecycle

import (
	"context"
	"testing"
	"time"

	"dolphin/internal/config"
	. "github.com/smartystreets/goconvey/convey"
)

func TestPipeline(t *testing.T) {
	Convey("Pipeline", t, func() {
		Convey("New creates pipeline from config", func() {
			cfg, err := config.LoadConfig("../../config.yaml")
			if err != nil {
				cfg = config.LoadConfigFromMap(map[string]any{
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
				})
			}
			cfg.Set("brain.dir", t.TempDir())

			p := New(cfg)
			So(p, ShouldNotBeNil)
			So(p.logger, ShouldNotBeNil)
			So(p.agentIO, ShouldNotBeNil)
			So(p.agentLoop, ShouldNotBeNil)
			So(p.userIO, ShouldNotBeNil)
			So(p.sessionMgr, ShouldNotBeNil)
		})

		Convey("Start and Shutdown work", func() {
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

			p := New(cfg)
			ctx := context.Background()

			p.Start(ctx)
			So(p.cancel, ShouldNotBeNil)

			time.Sleep(50 * time.Millisecond)

			p.Shutdown()
		})
	})
}

func TestPipelineSharedSession(t *testing.T) {
	Convey("Pipeline with session.mode=shared", t, func() {
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
			"session.mode":       "shared",
		})

		Convey("New creates pipeline with shared session mode", func() {
			p := New(cfg)
			So(p, ShouldNotBeNil)
			So(p.userIO, ShouldNotBeNil)
		})

		Convey("Start and Shutdown work with shared mode", func() {
			p := New(cfg)
			ctx := context.Background()

			p.Start(ctx)
			So(p.cancel, ShouldNotBeNil)

			time.Sleep(50 * time.Millisecond)

			p.Shutdown()
		})
	})
}
