package lifecycle

import (
	"context"
	"testing"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/event"
	. "github.com/smartystreets/goconvey/convey"
)

func TestPipeline(t *testing.T) {
	Convey("Pipeline", t, func() {
		Convey("New creates pipeline from config", func() {
			cfg, err := config.LoadConfig("../../config.yaml")
			if err != nil {
				cfg = config.LoadConfigFromMap(map[string]any{
					"llm.use":          "gpt-4o",
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
				"llm.use":          "gpt-4o",
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
			"llm.use":          "gpt-4o",
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

func TestPipelineTokenAccumulation(t *testing.T) {
	t.Parallel()

	cfg := config.LoadConfigFromMap(map[string]any{
		"llm.use":          "gpt-4o",
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
	defer p.Shutdown()

	// Create a session and publish an LLM complete event
	sess := p.sessionMgr.Create(ctx)

	p.eventBus.Publish(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: sess.ID,
		Payload: map[string]any{
			"input_tokens":  100,
			"output_tokens": 50,
		},
	})

	// Give the handlers time to run (synchronous on same goroutine)
	if v := sess.Get("input_tokens"); v == nil {
		t.Error("input_tokens should not be nil")
	} else if n, ok := v.(int); !ok {
		t.Errorf("input_tokens type = %T, want int", v)
	} else if n != 100 {
		t.Errorf("input_tokens = %d, want 100", n)
	}

	if v := sess.Get("output_tokens"); v == nil {
		t.Error("output_tokens should not be nil")
	} else if n, ok := v.(int); !ok {
		t.Errorf("output_tokens type = %T, want int", v)
	} else if n != 50 {
		t.Errorf("output_tokens = %d, want 50", n)
	}

	// Publish a second event to test accumulation
	p.eventBus.Publish(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: sess.ID,
		Payload: map[string]any{
			"input_tokens":  30,
			"output_tokens": 20,
		},
	})

	if n := sess.Get("input_tokens").(int); n != 130 {
		t.Errorf("input_tokens after accumulation = %d, want 130", n)
	}
	if n := sess.Get("output_tokens").(int); n != 70 {
		t.Errorf("output_tokens after accumulation = %d, want 70", n)
	}

	// Test system context tracking
	p.eventBus.Publish(ctx, event.Event{
		Type:      event.EventContextComplete,
		SessionID: sess.ID,
		Payload:   map[string]any{"input": "this is a system prompt"},
	})

	if n := sess.Get("system_context").(int); n != 23 {
		t.Errorf("system_context = %d, want 25", n)
	}

	// Test round accumulation
	p.eventBus.Publish(ctx, event.Event{
		Type:      event.EventTurnStart,
		SessionID: sess.ID,
	})
	p.eventBus.Publish(ctx, event.Event{
		Type:      event.EventTurnStart,
		SessionID: sess.ID,
	})

	if n := sess.Get("rounds").(int); n != 2 {
		t.Errorf("rounds = %d, want 2", n)
	}

	// Test tool call accumulation
	p.eventBus.Publish(ctx, event.Event{
		Type:      event.EventToolComplete,
		SessionID: sess.ID,
		Payload:   map[string]any{"tool": "test"},
	})

	if n := sess.Get("tool_calls").(int); n != 1 {
		t.Errorf("tool_calls = %d, want 1", n)
	}
}

func TestPipelineTokenAccumulationNewSession(t *testing.T) {
	t.Parallel()

	cfg := config.LoadConfigFromMap(map[string]any{
		"llm.use":          "gpt-4o",
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
	defer p.Shutdown()

	// NewSession is what the transport uses (not Create)
	sess := p.sessionMgr.NewSession(ctx)

	p.eventBus.Publish(ctx, event.Event{
		Type:      event.EventLLMComplete,
		SessionID: sess.ID,
		Payload: map[string]any{
			"input_tokens":  200,
			"output_tokens": 100,
		},
	})

	if v := sess.Get("input_tokens"); v == nil {
		t.Fatal("input_tokens should not be nil when session created via NewSession")
	} else if n := v.(int); n != 200 {
		t.Fatalf("input_tokens = %d, want 200", n)
	}
	if v := sess.Get("output_tokens"); v == nil {
		t.Fatal("output_tokens should not be nil when session created via NewSession")
	} else if n := v.(int); n != 100 {
		t.Fatalf("output_tokens = %d, want 100", n)
	}
}
