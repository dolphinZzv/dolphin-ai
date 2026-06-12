package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"dolphin/internal/agentio"
	"dolphin/internal/common"
	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/permission"
	"dolphin/internal/signal"
	"dolphin/internal/skill"
	"dolphin/internal/session"
	"dolphin/internal/testhelper"
	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/types"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

func TestCompositor(t *testing.T) {
	Convey("Compositor", t, func() {
		logger, _ := zap.NewDevelopment()

		Convey("Execute runs init then loop stages", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)

			c := NewCompositor(
				[]Stage{
					&MemoryReadStage{Memory: mem},
				},
				[]Stage{
					&MemoryWriteStage{Memory: mem, EventBus: event.NewBus()},
				},
				10,
			)

			state := &State{
				SessionID: "test-session",
				Input:     "hello",
			}

			err := c.Execute(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.Done, ShouldBeTrue)
			So(len(state.Messages), ShouldBeGreaterThan, 0)
		})

		Convey("Compositor respects max rounds", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)

			rounds := 0
			c := NewCompositor(
				[]Stage{&MemoryReadStage{Memory: mem}},
				[]Stage{&incrementStage{count: &rounds}},
				3,
			)

			state := &State{
				SessionID: "test",
				Input:     "x",
			}

			err := c.Execute(context.Background(), state)
			So(err, ShouldBeNil)
			So(rounds, ShouldEqual, 3)
		})

		Convey("MemoryReadStage reads history", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)
			mem.Write(context.Background(), "sid", types.Message{
				Role: types.RoleUser, Content: "prev",
			})

			stage := &MemoryReadStage{Memory: mem}
			state := &State{SessionID: "sid", Input: "new"}

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(len(state.History), ShouldEqual, 1)
			So(len(state.Messages), ShouldEqual, 2)
			So(state.Messages[1].Content, ShouldEqual, "new")
		})

		Convey("ContextBuilderStage injects skills", func() {
			skStore := skill.NewFileStore(t.TempDir())
			skStore.Save(context.Background(), skill.Skill{
				Name:    "helper",
				Prompt:  "you are a helper",
				Enabled: true,
			})

			stage := &ContextBuilderStage{
				BaseSystemPrompt: "base prompt",
				SkillStore:       skStore,
			}
			state := &State{}

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.SystemPrompt, ShouldContainSubstring, "base prompt")
			So(state.SystemPrompt, ShouldContainSubstring, "helper")
		})

		Convey("MemoryWriteStage skips when tools were called", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)
			stage := &MemoryWriteStage{Memory: mem, EventBus: event.NewBus()}
			state := &State{
				SessionID:   "sid",
				Input:       "x",
				ToolsCalled: true,
			}

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.Done, ShouldBeFalse)
		})

		Convey("MemoryWriteStage writes messages", func() {
			mem := memory.NewFileMemory(t.TempDir(), 10)
			stage := &MemoryWriteStage{Memory: mem, EventBus: event.NewBus()}
			state := &State{
				SessionID: "sid",
				Input:     "hi",
				Messages: []types.Message{
					{Role: types.RoleUser, Content: "hi"},
					{Role: types.RoleAssistant, Content: "hello"},
				},
			}

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.Done, ShouldBeTrue)

			msgs, _ := mem.Read(context.Background(), "sid")
			So(len(msgs), ShouldEqual, 2)
		})

		Convey("LLMStage retries on error", func() {
			eb := event.NewBus()

			provider := llm.NewProvider(llm.Config{
				Provider:   "openai",
				Model:      "gpt-4o",
				APIKey:     "invalid",
				BaseURL:    "http://127.0.0.1:1",
				MaxRetries: 1,
				MaxTokens:  10,
			}, logger)

			stage := &LLMStage{
				Provider:   provider,
				Model:      "gpt-4o",
				MaxTokens:  10,
				MaxRetries: 1,
				EventBus:   eb,
				Logger:     logger,
			}

			state := &State{
				SessionID:    "sid",
				Input:        "hi",
				SystemPrompt: "be helpful",
				Messages:     []types.Message{{Role: types.RoleUser, Content: "hi"}},
			}

			err := stage.Process(context.Background(), state)
			So(err, ShouldNotBeNil)
		})
	})
}

func TestToolStage_CheckPermission(t *testing.T) {
	Convey("ToolStage checkPermission", t, func() {
		logger, _ := zap.NewDevelopment()

		Convey("nil PermissionStore allows all", func() {
			stage := &ToolStage{Logger: logger}
			state := &State{}
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"echo secret"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldBeNil)
		})

		Convey("deny rule blocks tool call", func() {
			stage := &ToolStage{
				Logger: logger,
				PermissionStore: newTestStore(map[string]permission.RuleSet{
					"shell": {Deny: []map[string]string{{"command": "echo *"}}},
				}),
				Workmode: "default",
			}
			state := &State{}
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"echo secret"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "denied")
		})

		Convey("deny rule applies in yolo mode too", func() {
			stage := &ToolStage{
				Logger: logger,
				PermissionStore: newTestStore(map[string]permission.RuleSet{
					"shell": {Deny: []map[string]string{{"command": "echo *"}}},
				}),
				Workmode: "yolo",
			}
			state := &State{}
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"echo secret"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldNotBeNil)
		})

		Convey("allow rule passes", func() {
			stage := &ToolStage{
				Logger: logger,
				PermissionStore: newTestStore(map[string]permission.RuleSet{
					"shell": {Allow: []map[string]string{{"command": "ls *"}}},
				}),
				Workmode: "default",
			}
			state := &State{}
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"ls -la"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldBeNil)
		})

		Convey("yolo mode allows NoMatch", func() {
			stage := &ToolStage{
				Logger:          logger,
				PermissionStore: newTestStore(map[string]permission.RuleSet{}),
				Workmode:        "yolo",
			}
			state := &State{}
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"ls"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldBeNil)
		})

		Convey("NoMatch + default without transport returns error", func() {
			stage := &ToolStage{
				Logger:          logger,
				PermissionStore: newTestStore(map[string]permission.RuleSet{}),
				Workmode:        "default",
			}
			state := &State{}
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"ls"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "requires permission")
		})

		Convey("NoMatch + default with transport calls RequestPermission", func() {
			stage := &ToolStage{
				Logger:          logger,
				PermissionStore: newTestStore(map[string]permission.RuleSet{}),
				Workmode:        "default",
				GetTransport: func(id string) transport.IO {
					return &mockTransport{permResult: transport.PermissionOnce}
				},
			}
			state := &State{TransportID: "test"}
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"ls"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldBeNil)
		})

		Convey("NoMatch + default transport returns denied", func() {
			stage := &ToolStage{
				Logger:          logger,
				PermissionStore: newTestStore(map[string]permission.RuleSet{}),
				Workmode:        "default",
				GetTransport: func(id string) transport.IO {
					return &mockTransport{permResult: transport.PermissionDenied}
				},
			}
			state := &State{TransportID: "test"}
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"ls"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "denied by the user")
		})
	})
}

// newTestStore creates a permission store with the given rules (no file).
func newTestStore(rules map[string]permission.RuleSet) *permission.Store {
	return permission.NewTestStore(rules)
}

// mockTransport implements transport.IO for testing.
type mockTransport struct {
	transport.SessionHolder
	permResult transport.PermissionResult
}

func (m *mockTransport) ID() string                           { return "mock" }
func (m *mockTransport) Context() string                      { return "" }
func (m *mockTransport) Start(context.Context) error          { return nil }
func (m *mockTransport) Tools() []common.ToolDesc             { return nil }
func (m *mockTransport) Read(context.Context) (string, error) { return "", nil }
func (m *mockTransport) Write(context.Context, string) error  { return nil }
func (m *mockTransport) Flush() error                         { return nil }
func (m *mockTransport) Close() error                         { return nil }
func (m *mockTransport) Capability() transport.Capability {
	return transport.Capability{Interactive: true}
}
func (m *mockTransport) RequestPermission(_ context.Context, _ string) (transport.PermissionResult, error) {
	return m.permResult, nil
}

func TestRealLLMCompositor(t *testing.T) {
	testhelper.LoadEnv()
	if os.Getenv("DOLPHIN_LLM_ANTHROPIC_API_KEY") == "" {
		t.Skip("DOLPHIN_LLM_ANTHROPIC_API_KEY not set — real LLM test skipped")
	}

	Convey("Real LLM streaming", t, func() {
		logger, _ := zap.NewDevelopment()

		Convey("Provider returns first char of 123456", func() {
			provider := llm.NewProvider(llm.Config{
				Provider:   "anthropic",
				Model:      "deepseek-v4-flash",
				APIKey:     os.Getenv("DOLPHIN_LLM_ANTHROPIC_API_KEY"),
				BaseURL:    "https://api.deepseek.com/anthropic",
				MaxRetries: 1,
				MaxTokens:  300,
				Timeout:    60 * time.Second,
			}, logger)

			ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
				Model:     "deepseek-v4-flash",
				MaxTokens: 300,
				Messages: []types.Message{
					{Role: types.RoleUser, Content: "123456的第一个字是什么?只回答一个字符"},
				},
			})
			So(err, ShouldBeNil)

			var content string
			var gotDone bool
			for chunk := range ch {
				So(chunk.Error, ShouldBeNil)
				content += chunk.Content
				if chunk.Done {
					gotDone = true
				}
			}
			So(gotDone, ShouldBeTrue)
			So(content, ShouldNotBeBlank)
			So(strings.TrimSpace(content), ShouldEqual, "1")
		})

		Convey("Provider respects max tokens", func() {
			provider := llm.NewProvider(llm.Config{
				Provider:   "anthropic",
				Model:      "deepseek-v4-flash",
				APIKey:     os.Getenv("DOLPHIN_LLM_ANTHROPIC_API_KEY"),
				BaseURL:    "https://api.deepseek.com/anthropic",
				MaxRetries: 0,
				MaxTokens:  10,
				Timeout:    60 * time.Second,
			}, logger)

			ch, err := provider.CompleteStream(context.Background(), llm.LLMRequest{
				Model:     "deepseek-v4-flash",
				MaxTokens: 10,
				Messages: []types.Message{
					{Role: types.RoleUser, Content: "写一篇长文章"},
				},
			})
			So(err, ShouldBeNil)

			var content string
			for chunk := range ch {
				So(chunk.Error, ShouldBeNil)
				content += chunk.Content
			}
			/* max_tokens=10 on thinking model may produce zero text */
			So(len(content), ShouldBeLessThanOrEqualTo, 20)
		})
	})
}

func TestStageNameMethods(t *testing.T) {
	Convey("Stage Name() methods return correct names", t, func() {
		Convey("MemoryReadStage.Name", func() {
			s := &MemoryReadStage{}
			So(s.Name(), ShouldEqual, "memory_read")
		})
		Convey("ContextBuilderStage.Name", func() {
			s := &ContextBuilderStage{}
			So(s.Name(), ShouldEqual, "context_builder")
		})
		Convey("LLMStage.Name", func() {
			s := &LLMStage{}
			So(s.Name(), ShouldEqual, "llm")
		})
		Convey("ToolStage.Name", func() {
			s := &ToolStage{}
			So(s.Name(), ShouldEqual, "tool")
		})
		Convey("MemoryWriteStage.Name", func() {
			s := &MemoryWriteStage{}
			So(s.Name(), ShouldEqual, "memory_write")
		})
	})
}

func TestTruncateStr(t *testing.T) {
	Convey("truncateStr", t, func() {
		Convey("returns string unchanged when within max", func() {
			So(truncateStr("hello", 10), ShouldEqual, "hello")
		})
		Convey("truncates when string exceeds max", func() {
			So(truncateStr("hello world", 5), ShouldEqual, "hello")
		})
		Convey("handles empty string", func() {
			So(truncateStr("", 5), ShouldEqual, "")
		})
		Convey("handles zero max", func() {
			So(truncateStr("hello", 0), ShouldEqual, "")
		})
	})
}

func TestCompositorSetTurnTimeout(t *testing.T) {
	Convey("Compositor.SetTurnTimeout", t, func() {
		c := NewCompositor(nil, nil, 10)
		c.SetTurnTimeout(5 * time.Second)
		So(c.turnTimeout, ShouldEqual, 5*time.Second)
	})
}

func TestContextBuilderStageRegisterSection(t *testing.T) {
	Convey("ContextBuilderStage RegisterSection", t, func() {
		Convey("registers a section that appears in build", func() {
			stage := &ContextBuilderStage{
				BaseSystemPrompt: "base",
			}
			stage.RegisterSection(&appctxSection{name: "custom", content: "custom content"})
			state := &State{}
			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.SystemPrompt, ShouldContainSubstring, "custom content")
		})
	})
}

func TestBuildSystemPromptWithEventBus(t *testing.T) {
	Convey("ContextBuilderStage.BuildSystemPrompt", t, func() {
		Convey("publishes events when EventBus is set", func() {
			eb := event.NewBus()
			stage := &ContextBuilderStage{
				BaseSystemPrompt: "test base",
				EventBus:         eb,
			}

			var events []event.Event
			eb.Subscribe(func(ctx context.Context, e event.Event) {
				events = append(events, e)
			})

			prompt, err := stage.BuildSystemPrompt(context.Background())
			So(err, ShouldBeNil)
			So(prompt, ShouldContainSubstring, "test base")
			So(len(events), ShouldEqual, 2)
			So(events[0].Type, ShouldEqual, event.EventContextBuildStart)
			So(events[1].Type, ShouldEqual, event.EventContextBuildComplete)
		})

		Convey("works without EventBus", func() {
			stage := &ContextBuilderStage{
				BaseSystemPrompt: "no events",
			}
			prompt, err := stage.BuildSystemPrompt(context.Background())
			So(err, ShouldBeNil)
			So(prompt, ShouldContainSubstring, "no events")
		})
	})
}

func TestLLMStageActiveModel(t *testing.T) {
	Convey("LLMStage activeModel", t, func() {
		Convey("returns Model when set", func() {
			s := &LLMStage{Model: "gpt-4"}
			So(s.activeModel(), ShouldEqual, "gpt-4")
		})
		Convey("returns empty when Model empty and Provider has no ActiveModel", func() {
			s := &LLMStage{}
			So(s.activeModel(), ShouldEqual, "")
		})
		Convey("calls Provider.ActiveModel when Model is empty and provider supports it", func() {
			s := &LLMStage{Provider: &mockActiveProvider{active: "claude-3"}}
			So(s.activeModel(), ShouldEqual, "claude-3")
		})
	})
}

func TestToolStageProcessEmptyCalls(t *testing.T) {
	Convey("ToolStage.Process with empty calls", t, func() {
		stage := &ToolStage{}
		state := &State{}
		err := stage.Process(context.Background(), state)
		So(err, ShouldBeNil)
	})
}

func TestToolStageProcessDenied(t *testing.T) {
	Convey("ToolStage.Process with denied permission", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		var gotEvents []event.Event
		eb.Subscribe(func(_ context.Context, e event.Event) {
			gotEvents = append(gotEvents, e)
		})

		permStore := newTestStore(map[string]permission.RuleSet{
			"shell": {Deny: []map[string]string{{"command": "*"}}},
		})
		reg := tool.NewRegistry()
		reg.RegisterBuiltin("shell", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{Content: "ok", IsError: false}, nil
		})

		stage := &ToolStage{
			ToolRegistry:    reg,
			Logger:          logger,
			EventBus:        eb,
			PermissionStore: permStore,
			Workmode:        "default",
		}
		state := &State{SessionID: "s1"}
		state.ToolCalls = []types.ToolCall{
			{ID: "call1", Name: "shell", Arguments: `{"command":"ls"}`},
		}

		err := stage.Process(context.Background(), state)
		So(err, ShouldBeNil)
		So(state.ToolCalls, ShouldBeEmpty)
		So(len(state.Messages), ShouldEqual, 1)
		So(state.Messages[0].Role, ShouldEqual, types.RoleTool)
		So(state.Messages[0].ToolCallID, ShouldEqual, "call1")
		So(state.ToolsCalled, ShouldBeTrue)
		So(len(state.ToolResults), ShouldEqual, 1)
		So(state.ToolResults[0].IsError, ShouldBeTrue)
		So(len(gotEvents), ShouldBeGreaterThan, 0)
		hasToolError := false
		for _, e := range gotEvents {
			if e.Type == event.EventToolError {
				hasToolError = true
				break
			}
		}
		So(hasToolError, ShouldBeTrue)
	})
}

func TestToolStageProcessSignalInterrupt(t *testing.T) {
	Convey("ToolStage.Process with signal interrupt between calls", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		sigBus := signal.NewBus()
		var gotEvents []event.Event
		eb.Subscribe(func(_ context.Context, e event.Event) {
			gotEvents = append(gotEvents, e)
		})

		sigBus.ForSession("s1")

		handlerRunning := make(chan struct{})
		handlerResume := make(chan struct{})
		reg := tool.NewRegistry()
		reg.RegisterBuiltin("first", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			close(handlerRunning)
			<-handlerResume
			return &types.ToolResult{Content: "ok", IsError: false}, nil
		})
		reg.RegisterBuiltin("second", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{Content: "ok", IsError: false}, nil
		})

		stage := &ToolStage{
			ToolRegistry: reg,
			Logger:       logger,
			EventBus:     eb,
			SignalBus:    sigBus,
		}
		state := &State{SessionID: "s1"}
		state.ToolCalls = []types.ToolCall{
			{ID: "c1", Name: "first", Arguments: `{}`},
			{ID: "c2", Name: "second", Arguments: `{}`},
		}

		go func() {
			<-handlerRunning
			sigBus.Send("s1", signal.Interrupt)
			close(handlerResume)
		}()

		err := stage.Process(context.Background(), state)
		So(err, ShouldBeNil)
		hasInterrupt := false
		for _, e := range gotEvents {
			if e.Type == event.EventTurnInterrupt {
				hasInterrupt = true
				break
			}
		}
		So(hasInterrupt, ShouldBeTrue)
	})
}

func TestToolStageProcessSuccess(t *testing.T) {
	Convey("ToolStage.Process with successful execution", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		var gotEvents []event.Event
		eb.Subscribe(func(_ context.Context, e event.Event) {
			gotEvents = append(gotEvents, e)
		})

		reg := tool.NewRegistry()
		reg.RegisterBuiltin("shell", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{Content: "done", IsError: false}, nil
		})

		stage := &ToolStage{
			ToolRegistry: reg,
			Logger:       logger,
			EventBus:     eb,
		}
		state := &State{SessionID: "s1"}
		state.ToolCalls = []types.ToolCall{
			{ID: "call1", Name: "shell", Arguments: `{"command":"ls"}`},
		}

		err := stage.Process(context.Background(), state)
		So(err, ShouldBeNil)
		So(len(state.Messages), ShouldEqual, 1)
		So(state.Messages[0].Content, ShouldEqual, "done")
		So(state.Messages[0].Role, ShouldEqual, types.RoleTool)
		So(state.Messages[0].ToolCallID, ShouldEqual, "call1")
		So(len(state.ToolResults), ShouldEqual, 1)
		So(state.ToolResults[0].Content, ShouldEqual, "done")
		So(state.ToolResults[0].IsError, ShouldBeFalse)

		hasStart := false
		hasComplete := false
		for _, e := range gotEvents {
			switch e.Type {
			case event.EventToolStart:
				hasStart = true
			case event.EventToolComplete:
				hasComplete = true
			}
		}
		So(hasStart, ShouldBeTrue)
		So(hasComplete, ShouldBeTrue)
	})
}

func TestLLMStageTryComplete(t *testing.T) {
	Convey("LLMStage.tryComplete", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		Convey("processes chunks and builds response", func() {
			provider := &chunkProvider{
				chunks: []llm.LLMChunk{
					{Content: "Hello", InputTokens: 10},
					{Content: " world", OutputTokens: 5},
					{Content: "", Done: true},
				},
			}
			stage := &LLMStage{
				Provider: provider,
				Model:    "test-model",
				EventBus: eb,
				Logger:   logger,
			}
			state := &State{
				Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
			}

			var events []event.Event
			eb.Subscribe(func(ctx context.Context, e event.Event) {
				events = append(events, e)
			})

			err := stage.tryComplete(context.Background(), state)
			So(err, ShouldBeNil)
			So(len(state.Messages), ShouldEqual, 2)
			So(state.Messages[1].Role, ShouldEqual, types.RoleAssistant)
			So(state.Messages[1].Content, ShouldEqual, "Hello world")

			So(len(events), ShouldBeGreaterThanOrEqualTo, 3)
		})

		Convey("handles chunk error", func() {
			provider := &chunkProvider{
				chunks: []llm.LLMChunk{
					{Error: fmt.Errorf("api error")},
				},
			}
			stage := &LLMStage{
				Provider: provider,
				Model:    "test-model",
				EventBus: eb,
				Logger:   logger,
			}
			state := &State{
				Messages: []types.Message{{Role: types.RoleUser, Content: "hi"}},
			}

			err := stage.tryComplete(context.Background(), state)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldEqual, "api error")
		})

		Convey("handles tool calls in chunks", func() {
			provider := &chunkProvider{
				chunks: []llm.LLMChunk{
					{Content: "let me check"},
					{ToolCalls: []types.ToolCall{{ID: "tc-1", Name: "shell", Arguments: `{"cmd":"ls"}`}}},
					{Content: "", Done: true},
				},
			}
			stage := &LLMStage{
				Provider: provider,
				Model:    "test-model",
				EventBus: eb,
				Logger:   logger,
			}
			state := &State{
				Messages: []types.Message{{Role: types.RoleUser, Content: "do it"}},
			}

			err := stage.tryComplete(context.Background(), state)
			So(err, ShouldBeNil)
			So(len(state.ToolCalls), ShouldEqual, 1)
			So(state.ToolCalls[0].Name, ShouldEqual, "shell")
		})

		Convey("calls OnChunk callback for each content chunk", func() {
			provider := &chunkProvider{
				chunks: []llm.LLMChunk{
					{Content: "a"},
					{Content: "b"},
					{Content: "", Done: true},
				},
			}
			var chunks []string
			stage := &LLMStage{
				Provider: provider,
				Model:    "test-model",
				EventBus: eb,
				Logger:   logger,
			}
			state := &State{
				Messages: []types.Message{{Role: types.RoleUser, Content: "x"}},
				OnChunk: func(text string) {
					chunks = append(chunks, text)
				},
			}

			err := stage.tryComplete(context.Background(), state)
			So(err, ShouldBeNil)
			So(chunks, ShouldResemble, []string{"a", "b"})
		})
	})
}

type chunkProvider struct {
	chunks []llm.LLMChunk
}

func (c *chunkProvider) Name() string { return "chunk-provider" }
func (c *chunkProvider) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	ch := make(chan llm.LLMChunk, len(c.chunks))
	for _, chunk := range c.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}
func (c *chunkProvider) Models(_ context.Context) ([]llm.ModelConfig, error) { return nil, nil }
func (c *chunkProvider) ActiveModel() string                                 { return "" }

type appctxSection struct {
	name    string
	content string
}

func (s *appctxSection) Name() string                                   { return s.name }
func (s *appctxSection) Index() int                                     { return 10 }
func (s *appctxSection) BuildContent(_ context.Context) (string, error) { return s.content, nil }

type mockActiveProvider struct {
	active string
}

func (m *mockActiveProvider) ActiveModel() string { return m.active }
func (m *mockActiveProvider) Name() string        { return "mock" }
func (m *mockActiveProvider) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	ch := make(chan llm.LLMChunk)
	close(ch)
	return ch, nil
}
func (m *mockActiveProvider) Models(_ context.Context) ([]llm.ModelConfig, error) { return nil, nil }

type countingMemory struct {
	inner memory.Memory
}

func (m *countingMemory) Read(ctx context.Context, sid string) ([]types.Message, error) {
	return m.inner.Read(ctx, sid)
}
func (m *countingMemory) Write(ctx context.Context, sid string, msg types.Message) error {
	return m.inner.Write(ctx, sid, msg)
}

type incrementStage struct {
	count *int
}

func (s *incrementStage) Name() string { return "increment" }
func (s *incrementStage) Process(ctx context.Context, state *State) error {
	*s.count++
	return nil
}

func TestNewAgentLoop(t *testing.T) {
	Convey("NewAgentLoop", t, func() {
		q := make(chan *agentio.Turn)
		c := NewCompositor(nil, nil, 10)
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		a := NewAgentLoop(q, c, logger, eb, nil)
		So(a, ShouldNotBeNil)
		So(a.queue, ShouldEqual, q)
		So(a.compositor, ShouldEqual, c)
		So(a.logger, ShouldEqual, logger)
		So(a.eventBus, ShouldEqual, eb)
	})
}

func TestSetOnResult(t *testing.T) {
	Convey("SetOnResult", t, func() {
		q := make(chan *agentio.Turn)
		c := NewCompositor(nil, nil, 10)
		logger, _ := zap.NewDevelopment()
		a := NewAgentLoop(q, c, logger, nil, nil)

		called := false
		a.SetOnResult(func(result agentio.TurnResult) {
			called = true
		})
		So(a.onResult, ShouldNotBeNil)

		a.onResult(agentio.TurnResult{})
		So(called, ShouldBeTrue)
	})
}

func TestValidSessionID(t *testing.T) {
	Convey("validSessionID", t, func() {
		Convey("returns normal session ID as-is", func() {
			So(validSessionID("session-123"), ShouldEqual, "session-123")
		})
		Convey("returns empty for too long session ID", func() {
			long := make([]byte, 201)
			for i := range long {
				long[i] = 'a'
			}
			So(validSessionID(string(long)), ShouldEqual, "")
		})
		Convey("returns empty for non-ASCII session ID", func() {
			So(validSessionID("会话"), ShouldEqual, "")
		})
		Convey("handles empty session ID", func() {
			So(validSessionID(""), ShouldEqual, "")
		})
		Convey("handles exactly 200 chars", func() {
			str := make([]byte, 200)
			for i := range str {
				str[i] = 'a'
			}
			So(validSessionID(string(str)), ShouldEqual, string(str))
		})
	})
}

func TestPublishTurnEvent(t *testing.T) {
	Convey("publishTurnEvent", t, func() {
		Convey("skips when eventBus is nil", func() {
			q := make(chan *agentio.Turn)
			c := NewCompositor(nil, nil, 10)
			logger, _ := zap.NewDevelopment()
			a := NewAgentLoop(q, c, logger, nil, nil)

			So(func() {
				a.publishTurnEvent(context.Background(), event.EventTurnStart, "tid", "sid", time.Now(), nil)
			}, ShouldNotPanic)
		})

		Convey("publishes event with correct type", func() {
			eb := event.NewBus()
			q := make(chan *agentio.Turn)
			c := NewCompositor(nil, nil, 10)
			logger, _ := zap.NewDevelopment()
			a := NewAgentLoop(q, c, logger, eb, nil)

			var receivedType event.Type
			eb.Subscribe(func(ctx context.Context, e event.Event) {
				receivedType = e.Type
			})

			a.publishTurnEvent(context.Background(), event.EventTurnComplete, "tid", "sid", time.Now(), nil)
			So(receivedType, ShouldEqual, event.EventTurnComplete)
		})

		Convey("publishes event with error payload", func() {
			eb := event.NewBus()
			q := make(chan *agentio.Turn)
			c := NewCompositor(nil, nil, 10)
			logger, _ := zap.NewDevelopment()
			a := NewAgentLoop(q, c, logger, eb, nil)

			var payload map[string]any
			eb.Subscribe(func(ctx context.Context, e event.Event) {
				payload = e.Payload
			})

			a.publishTurnEvent(context.Background(), event.EventTurnError, "tid", "sid", time.Now(), fmt.Errorf("oops"))
			So(payload, ShouldNotBeNil)
			So(payload["error"], ShouldEqual, "oops")
			So(payload["duration_ms"], ShouldNotBeNil)
		})
	})
}

func TestAgentLoopRunAndProcess(t *testing.T) {
	Convey("AgentLoop Run and processTurn", t, func() {
		q := make(chan *agentio.Turn, 1)
		mem := memory.NewFileMemory(t.TempDir(), 10)
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		compositor := NewCompositor(
			[]Stage{&MemoryReadStage{Memory: mem}},
			[]Stage{&MemoryWriteStage{Memory: mem, EventBus: eb}},
			1,
		)

		a := NewAgentLoop(q, compositor, logger, eb, nil)

		var resultCallCount int
		a.SetOnResult(func(result agentio.TurnResult) {
			resultCallCount++
		})

		ctx, cancel := context.WithCancel(context.Background())
		go a.Run(ctx)

		q <- &agentio.Turn{
			SessionID:   "test-session",
			Input:       "hello",
			Context:     "test-ctx",
			TransportID: "test-transport",
		}

		time.Sleep(100 * time.Millisecond)
		cancel()
		time.Sleep(50 * time.Millisecond)

		So(resultCallCount, ShouldBeGreaterThan, 0)
	})
}

func TestAgentLoopCanceledTurn(t *testing.T) {
	Convey("AgentLoop skips cancelled turns", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		mgr := session.NewManager(t.TempDir())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "test")
		mem := memory.NewFileMemory(t.TempDir(), 10)
		compositor := NewCompositor(
			[]Stage{&MemoryReadStage{Memory: mem}},
			[]Stage{&MemoryWriteStage{Memory: mem, EventBus: eb}},
			1,
		)
		a := NewAgentLoop(aio.Queue(), compositor, logger, eb, aio)

		var resultCalls int
		a.SetOnResult(func(result agentio.TurnResult) {
			resultCalls++
		})

		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID:      "t1",
			SessionID:   "test-session",
			Input:       "hello",
			TransportID: "test-transport",
		})
		aio.PopIndex(0) // mark cancelled

		ctx, cancel := context.WithCancel(context.Background())
		go a.Run(ctx)

		time.Sleep(100 * time.Millisecond)
		cancel()
		time.Sleep(50 * time.Millisecond)

		So(resultCalls, ShouldEqual, 0)
	})
}

func TestAgentLoopRunContextDone(t *testing.T) {
	Convey("AgentLoop.Run exits on context done", t, func() {
		q := make(chan *agentio.Turn)
		c := NewCompositor(nil, nil, 10)
		logger, _ := zap.NewDevelopment()
		a := NewAgentLoop(q, c, logger, nil, nil)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		So(func() { a.Run(ctx) }, ShouldNotPanic)
	})
}
