package agentloop

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"dolphin/internal/common"
	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/permission"
	"dolphin/internal/skill"
	"dolphin/internal/testhelper"
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
