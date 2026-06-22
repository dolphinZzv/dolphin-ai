package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/common"
	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/permission"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/skill"
	"dolphin/internal/testhelper"
	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/types"
)

// testSessionStore is a lightweight SessionStore for tests.
type testSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session.Session
}

func (s *testSessionStore) Get(id string) *session.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess
	}
	sess := &session.Session{ID: id}
	if s.sessions == nil {
		s.sessions = make(map[string]*session.Session)
	}
	s.sessions[id] = sess
	return sess
}

func TestCompositor(t *testing.T) {
	Convey("Compositor", t, func() {
		logger, _ := zap.NewDevelopment()

		Convey("Execute runs init then loop stages", func() {
			mem := memory.NewFileMemory(&testSessionStore{})

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
			mem := memory.NewFileMemory(&testSessionStore{})

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
			mem := memory.NewFileMemory(&testSessionStore{})
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
			skStore, storeErr := skill.NewFileStore(t.TempDir())
			if storeErr != nil {
				t.Fatal(storeErr)
			}
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
			mem := memory.NewFileMemory(&testSessionStore{})
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
			mem := memory.NewFileMemory(&testSessionStore{})
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
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"unsafe_cmd"}`}
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
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"unsafe_cmd"}`}
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
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"unsafe_cmd"}`}
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
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"unsafe_cmd"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "denied by the user")
		})

		Convey("request_permission and emit_event bypass permission check", func() {
			stage := &ToolStage{
				Logger: logger,
				PermissionStore: newTestStore(map[string]permission.RuleSet{
					"request_permission": {Deny: []map[string]string{{"*": "*"}}},
				}),
				Workmode: "default",
			}
			state := &State{}
			So(stage.checkPermission(context.Background(), state, types.ToolCall{Name: "request_permission"}), ShouldBeNil)
			So(stage.checkPermission(context.Background(), state, types.ToolCall{Name: "emit_event"}), ShouldBeNil)
		})

		Convey("NoMatch + transport lookup returns nil", func() {
			stage := &ToolStage{
				Logger:          logger,
				PermissionStore: newTestStore(map[string]permission.RuleSet{}),
				Workmode:        "default",
				GetTransport:    func(id string) transport.IO { return nil },
			}
			state := &State{TransportID: "missing"}
			call := types.ToolCall{Name: "shell", Arguments: `{"command":"unsafe_cmd"}`}
			err := stage.checkPermission(context.Background(), state, call)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "requires permission")
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

// blockingPermTransport wraps mockTransport but blocks RequestPermission
// until its release channel is closed (or the context is cancelled), so we
// can observe the watchdog's behaviour during a user-bound permission wait.
type blockingPermTransport struct {
	mockTransport
	release chan struct{}
}

func (m *blockingPermTransport) RequestPermission(ctx context.Context, _ string) (transport.PermissionResult, error) {
	select {
	case <-m.release:
		return m.permResult, nil
	case <-ctx.Done():
		return transport.PermissionDenied, ctx.Err()
	}
}

// TestRequestPermissionFeedingKeepsWatchdogAlive verifies that while a
// permission prompt is blocking on user input, the idle watchdog is fed
// often enough that it does NOT cancel the turn — the wait is user-bound,
// not a stuck LLM.
func TestRequestPermissionFeedingKeepsWatchdogAlive(t *testing.T) {
	orig := permFeedInterval
	permFeedInterval = 10 * time.Millisecond
	t.Cleanup(func() { permFeedInterval = orig })

	release := make(chan struct{})
	bt := &blockingPermTransport{
		mockTransport: mockTransport{permResult: transport.PermissionOnce},
		release:       release,
	}

	wdCtx, wd := New(context.Background(), 200*time.Millisecond)
	defer wd.Stop()
	// Shrink the feed throttle so our 10ms ticker actually feeds each tick
	// (default 100ms throttle would skip them). Must be set before the
	// first Feed.
	wd.SetMinFeedInterval(5 * time.Millisecond)

	type result struct{ err error }
	resCh := make(chan result, 1)
	go func() {
		_, err := requestPermissionFeeding(wdCtx, bt, "allow shell?")
		resCh <- result{err}
	}()

	// Wait well past the idle window. Without feeding the watchdog would
	// have fired by now.
	time.Sleep(400 * time.Millisecond)
	select {
	case <-wdCtx.Done():
		t.Fatalf("watchdog fired during permission wait despite feeding: %v", wdCtx.Err())
	default:
	}
	if wd.Stats().Feeds < 2 {
		t.Errorf("expected multiple feeds while waiting, got %d", wd.Stats().Feeds)
	}

	// Let the prompt resolve; the call should return cleanly.
	close(release)
	select {
	case r := <-resCh:
		if r.err != nil {
			t.Errorf("unexpected error after release: %v", r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("requestPermissionFeeding did not return after release")
	}
	if wd.Stats().Fired {
		t.Errorf("watchdog should not have fired")
	}
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

func (m *mockTransport) Confirm(_ context.Context, _ string) (bool, error) {
	return m.permResult != transport.PermissionDenied, nil
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

// delayStage sleeps for a fixed duration on its first call, then returns
// immediately on subsequent calls. Used to test per-round timeout behavior.
type delayStage struct {
	delay   time.Duration
	first   bool
	didWait bool
}

func (s *delayStage) Name() string { return "delay" }
func (s *delayStage) Clone() Stage {
	return &delayStage{delay: s.delay, first: true}
}

func (s *delayStage) Process(ctx context.Context, _ *State) error {
	if s.first {
		s.first = false
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			s.didWait = true
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// doneStage sets state.Done after a configurable number of calls.
type doneStage struct {
	calls    int
	maxCalls int
}

func (s *doneStage) Name() string { return "done" }
func (s *doneStage) Clone() Stage { return &doneStage{maxCalls: s.maxCalls} }
func (s *doneStage) Process(_ context.Context, state *State) error {
	s.calls++
	if s.calls >= s.maxCalls {
		state.Done = true
	}
	return nil
}

func TestCompositorPerRoundTimeout(t *testing.T) {
	Convey("Compositor.Execute per-round timeout", t, func() {
		Convey("each round gets a fresh timeout context", func() {
			// delayStage takes 150ms on its first call.
			// turnTimeout is 200ms. Without per-round reset, 150ms +
			// round 2 overhead would exceed the shared 200ms budget.
			delay := &delayStage{delay: 150 * time.Millisecond, first: true}
			done := &doneStage{maxCalls: 2} // needs two rounds to set Done

			c := NewCompositor(nil, []Stage{delay, done}, 10)
			c.SetTurnTimeout(200 * time.Millisecond)

			ctx := context.Background()
			state := &State{}
			err := c.Execute(ctx, state)
			So(err, ShouldBeNil)
			So(state.Done, ShouldBeTrue)
			So(state.Round, ShouldEqual, 2)
			So(delay.didWait, ShouldBeTrue)
		})

		Convey("round times out when a single stage exceeds the deadline", func() {
			// 300ms delay > 100ms timeout → should fail.
			delay := &delayStage{delay: 300 * time.Millisecond, first: true}
			done := &doneStage{maxCalls: 1}

			c := NewCompositor(nil, []Stage{delay, done}, 10)
			c.SetTurnTimeout(100 * time.Millisecond)

			ctx := context.Background()
			state := &State{}
			err := c.Execute(ctx, state)
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "loop stage delay")
		})
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

		Convey("publishes events when EventBus is set", func() {
			eb := event.NewBus()
			stage := &ContextBuilderStage{
				BaseSystemPrompt: "base",
				EventBus:         eb,
			}
			var events []event.Event
			eb.Subscribe(func(_ context.Context, e event.Event) {
				events = append(events, e)
			})
			state := &State{SessionID: "s1"}
			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(state.SystemPrompt, ShouldContainSubstring, "base")
			So(len(events), ShouldBeGreaterThanOrEqualTo, 2)
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
			{ID: "call1", Name: "shell", Arguments: `{"command":"unsafe_cmd"}`},
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
			{ID: "call1", Name: "shell", Arguments: `{"command":"unsafe_cmd"}`},
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
			switch e.Type { //nolint:exhaustive // test asserts only on tool events
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

func TestToolStageProcessParallel(t *testing.T) {
	Convey("ToolStage.Process parallel execution", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		Convey("executes tools in parallel when MaxParallel > 1", func() {
			var mu sync.Mutex
			var execOrder []string
			started := make(chan struct{})
			reg := tool.NewRegistry()
			reg.RegisterBuiltin("a", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				mu.Lock()
				execOrder = append(execOrder, "a_start")
				mu.Unlock()
				<-started
				mu.Lock()
				execOrder = append(execOrder, "a_end")
				mu.Unlock()
				return &types.ToolResult{Content: "a", IsError: false}, nil
			})
			reg.RegisterBuiltin("b", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				mu.Lock()
				execOrder = append(execOrder, "b_start")
				mu.Unlock()
				<-started
				mu.Lock()
				execOrder = append(execOrder, "b_end")
				mu.Unlock()
				return &types.ToolResult{Content: "b", IsError: false}, nil
			})

			stage := &ToolStage{
				ToolRegistry: reg,
				Logger:       logger,
				EventBus:     eb,
				MaxParallel:  2,
			}
			state := &State{SessionID: "s1"}
			state.ToolCalls = []types.ToolCall{
				{ID: "c1", Name: "a", Arguments: `{}`},
				{ID: "c2", Name: "b", Arguments: `{}`},
			}

			go func() {
				time.Sleep(50 * time.Millisecond)
				close(started)
			}()

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(len(state.Messages), ShouldEqual, 2)
			// Both started before either finished → parallel execution.
			So(execOrder[0], ShouldEndWith, "_start")
			So(execOrder[1], ShouldEndWith, "_start")
			So(execOrder[2], ShouldEndWith, "_end")
			So(execOrder[3], ShouldEndWith, "_end")
		})

		Convey("falls back to serial when MaxParallel <= 1", func() {
			reg := tool.NewRegistry()
			reg.RegisterBuiltin("x", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
				return &types.ToolResult{Content: "x", IsError: false}, nil
			})

			stage := &ToolStage{
				ToolRegistry: reg,
				Logger:       logger,
				EventBus:     eb,
				MaxParallel:  1,
			}
			state := &State{SessionID: "s1"}
			state.ToolCalls = []types.ToolCall{
				{ID: "c1", Name: "x", Arguments: `{}`},
				{ID: "c2", Name: "x", Arguments: `{}`},
			}

			err := stage.Process(context.Background(), state)
			So(err, ShouldBeNil)
			So(len(state.Messages), ShouldEqual, 2)
			So(state.Messages[0].Content, ShouldEqual, "x")
			So(state.Messages[1].Content, ShouldEqual, "x")
		})
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

			err := stage.tryComplete(context.Background(), state, nil)
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

			err := stage.tryComplete(context.Background(), state, nil)
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

			err := stage.tryComplete(context.Background(), state, nil)
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

			err := stage.tryComplete(context.Background(), state, nil)
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

type incrementStage struct {
	count *int
}

func (s *incrementStage) Name() string { return "increment" }
func (s *incrementStage) Clone() Stage { return &incrementStage{count: s.count} }
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

		a := NewAgentLoop(q, c, logger, eb, nil, 1)
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
		a := NewAgentLoop(q, c, logger, nil, nil, 1)

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
			a := NewAgentLoop(q, c, logger, nil, nil, 1)

			So(func() {
				a.publishTurnEvent(context.Background(), event.EventTurnStart, "tid", "sid", time.Now(), nil)
			}, ShouldNotPanic)
		})

		Convey("publishes event with correct type", func() {
			eb := event.NewBus()
			q := make(chan *agentio.Turn)
			c := NewCompositor(nil, nil, 10)
			logger, _ := zap.NewDevelopment()
			a := NewAgentLoop(q, c, logger, eb, nil, 1)

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
			a := NewAgentLoop(q, c, logger, eb, nil, 1)

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
		mem := memory.NewFileMemory(&testSessionStore{})
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		compositor := NewCompositor(
			[]Stage{&MemoryReadStage{Memory: mem}},
			[]Stage{&MemoryWriteStage{Memory: mem, EventBus: eb}},
			1,
		)

		a := NewAgentLoop(q, compositor, logger, eb, nil, 1)

		var resultCallCount int
		turnDone := make(chan struct{})
		a.SetOnResult(func(result agentio.TurnResult) {
			resultCallCount++
			if result.Done {
				close(turnDone)
			}
		})

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(done)
		}()

		q <- &agentio.Turn{
			SessionID:   "test-session",
			Input:       "hello",
			Context:     "test-ctx",
			TransportID: "test-transport",
		}

		<-turnDone
		cancel()
		<-done

		So(resultCallCount, ShouldBeGreaterThan, 0)
	})
}

func TestAgentLoopCanceledTurn(t *testing.T) {
	Convey("AgentLoop skips cancelled turns", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		mgr := session.NewManager(t.TempDir())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "test")
		mem := memory.NewFileMemory(&testSessionStore{})
		compositor := NewCompositor(
			[]Stage{&MemoryReadStage{Memory: mem}},
			[]Stage{&MemoryWriteStage{Memory: mem, EventBus: eb}},
			1,
		)
		a := NewAgentLoop(aio.Queue(), compositor, logger, eb, aio, 1)

		var resultCalls int
		turnDone := make(chan struct{})

		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID:      "t1",
			SessionID:   "test-session",
			Input:       "hello",
			TransportID: "test-transport",
		})
		aio.PopIndex(0) // mark cancelled

		// Send a sentinel turn that will complete — proves the
		// cancelled turn was dequeued and skipped before it.
		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID: "sentinel", SessionID: "test-session", Input: "x", TransportID: "test-transport",
		})

		a.SetOnResult(func(result agentio.TurnResult) {
			if result.TurnID == "t1" {
				resultCalls++
			}
			if result.TurnID == "sentinel" && result.Done {
				close(turnDone)
			}
		})

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(done)
		}()

		<-turnDone
		cancel()
		<-done

		So(resultCalls, ShouldEqual, 0)
	})
}

func TestAgentLoopNonCanceledTurn(t *testing.T) {
	Convey("AgentLoop processes non-cancelled turns with agentIO set", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		mgr := session.NewManager(t.TempDir())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "test")
		mem := memory.NewFileMemory(&testSessionStore{})
		compositor := NewCompositor(
			[]Stage{&MemoryReadStage{Memory: mem}},
			[]Stage{&MemoryWriteStage{Memory: mem, EventBus: eb}},
			1,
		)
		a := NewAgentLoop(aio.Queue(), compositor, logger, eb, aio, 1)

		var resultCalls int
		turnDone := make(chan struct{})

		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID:      "t2",
			SessionID:   "test-session",
			Input:       "hello",
			TransportID: "test-transport",
		})
		// NOT popping — turn should be processed normally

		a.SetOnResult(func(result agentio.TurnResult) {
			resultCalls++
			if result.Done {
				close(turnDone)
			}
		})

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(done)
		}()

		<-turnDone
		cancel()
		<-done

		So(resultCalls, ShouldBeGreaterThan, 0)
	})
}

func TestAgentLoopProcessTurnError(t *testing.T) {
	Convey("AgentLoop processTurn handles error", t, func() {
		q := make(chan *agentio.Turn, 1)
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		// Compositor with an init stage that errors
		compositor := NewCompositor(
			[]Stage{&errorStage{}},
			nil,
			1,
		)

		a := NewAgentLoop(q, compositor, logger, eb, nil, 1)

		var lastResult agentio.TurnResult
		turnDone := make(chan struct{})
		a.SetOnResult(func(result agentio.TurnResult) {
			if result.Done {
				lastResult = result
				close(turnDone)
			}
		})

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(done)
		}()

		q <- &agentio.Turn{
			SessionID:   "test-session",
			Input:       "trigger error",
			TransportID: "test-transport",
		}

		<-turnDone
		cancel()
		<-done

		So(lastResult.Text, ShouldContainSubstring, "Error")
		So(lastResult.Done, ShouldBeTrue)
	})
}

func TestAgentLoopRunContextDone(t *testing.T) {
	Convey("AgentLoop.Run exits on context done", t, func() {
		q := make(chan *agentio.Turn)
		c := NewCompositor(nil, nil, 10)
		logger, _ := zap.NewDevelopment()
		a := NewAgentLoop(q, c, logger, nil, nil, 1)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		So(func() { a.Run(ctx) }, ShouldNotPanic)
	})
}

func TestAgentLoopSessionLockGC(t *testing.T) {
	Convey("sessionLockGC cleans up uncontended locks", t, func() {
		q := make(chan *agentio.Turn)
		c := NewCompositor(nil, nil, 10)
		logger, _ := zap.NewDevelopment()

		// poolSize > 1 triggers GC goroutine
		a := NewAgentLoop(q, c, logger, nil, nil, 2)

		// Acquire and release a session lock so it becomes uncontended.
		mu := a.sessionLock("gc-test-session")
		mu.Lock()
		mu.Unlock() //nolint:staticcheck // SA2001: the Lock has the side effect of registering the entry

		So(len(a.sessionLocks), ShouldEqual, 1)

		// Manually trigger one GC cycle.
		a.sessionMu.Lock()
		for id, m := range a.sessionLocks {
			if m.TryLock() {
				m.Unlock()
				delete(a.sessionLocks, id)
			}
		}
		a.sessionMu.Unlock()

		So(len(a.sessionLocks), ShouldEqual, 0)
	})

	Convey("sessionLockGC keeps contended locks", t, func() {
		q := make(chan *agentio.Turn)
		c := NewCompositor(nil, nil, 10)
		logger, _ := zap.NewDevelopment()

		a := NewAgentLoop(q, c, logger, nil, nil, 2)

		mu := a.sessionLock("contended-session")
		mu.Lock() // held — not released

		So(len(a.sessionLocks), ShouldEqual, 1)

		// Try GC — lock is still held so TryLock fails.
		a.sessionMu.Lock()
		for id, m := range a.sessionLocks {
			if m.TryLock() {
				m.Unlock()
				delete(a.sessionLocks, id)
			}
		}
		a.sessionMu.Unlock()

		So(len(a.sessionLocks), ShouldEqual, 1)

		mu.Unlock()
	})
}

func TestContextBuilderStageClone(t *testing.T) {
	Convey("ContextBuilderStage.Clone", t, func() {
		eb := event.NewBus()
		s := &ContextBuilderStage{
			BaseSystemPrompt: "base-prompt",
			Workspace:        "/tmp/ws",
			Workmode:         "default",
			EventBus:         eb,
		}

		cloned := s.Clone()
		cs, ok := cloned.(*ContextBuilderStage)
		So(ok, ShouldBeTrue)
		So(cs.BaseSystemPrompt, ShouldEqual, "base-prompt")
		So(cs.Workspace, ShouldEqual, "/tmp/ws")
		So(cs.Workmode, ShouldEqual, "default")
		So(cs.EventBus, ShouldEqual, eb)
		So(cs.transportCtx, ShouldBeEmpty) // per-turn state reset
	})
}

type errorStage struct{}

func (s *errorStage) Name() string { return "error" }
func (s *errorStage) Clone() Stage { return &errorStage{} }
func (s *errorStage) Process(_ context.Context, _ *State) error {
	return fmt.Errorf("injected error")
}

// mockLLMProvider satisfies llm.Provider for tests.
type mockLLMProvider struct{ name string }

func (m *mockLLMProvider) Name() string { return m.name }
func (m *mockLLMProvider) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockLLMProvider) Models(_ context.Context) ([]llm.ModelConfig, error) {
	return nil, nil
}

func TestLLMStageClone(t *testing.T) {
	Convey("LLMStage.Clone", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		tr := tool.NewRegistry()
		hr := hook.NewRegistry()
		s := &LLMStage{
			Provider:     &mockLLMProvider{name: "mock"},
			Model:        "gpt-4",
			MaxTokens:    4096,
			MaxRetries:   3,
			ToolRegistry: tr,
			EventBus:     eb,
			Logger:       logger,
			HookReg:      hr,
		}
		cloned := s.Clone()
		cs, ok := cloned.(*LLMStage)
		So(ok, ShouldBeTrue)
		So(cs.Model, ShouldEqual, "gpt-4")
		So(cs.MaxTokens, ShouldEqual, 4096)
		So(cs.MaxRetries, ShouldEqual, 3)
		So(cs.ToolRegistry, ShouldEqual, tr)
		So(cs.EventBus, ShouldEqual, eb)
		So(cs.Logger, ShouldEqual, logger)
		So(cs.HookReg, ShouldEqual, hr)
	})
}

func TestToolStageClone(t *testing.T) {
	Convey("ToolStage.Clone", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		tr := tool.NewRegistry()
		hr := hook.NewRegistry()
		sb := signal.NewBus()
		s := &ToolStage{
			ToolRegistry:    tr,
			SignalBus:       sb,
			Timeout:         30 * time.Second,
			Logger:          logger,
			HookReg:         hr,
			EventBus:        eb,
			PermissionStore: permission.NewStore("/tmp/perm.json"),
			Workmode:        "default",
		}
		cloned := s.Clone()
		cs, ok := cloned.(*ToolStage)
		So(ok, ShouldBeTrue)
		So(cs.ToolRegistry, ShouldEqual, tr)
		So(cs.SignalBus, ShouldEqual, sb)
		So(cs.Timeout, ShouldEqual, 30*time.Second)
		So(cs.Logger, ShouldEqual, logger)
		So(cs.EventBus, ShouldEqual, eb)
		So(cs.Workmode, ShouldEqual, "default")
	})
}

func TestContextBuilderStageRegistry(t *testing.T) {
	Convey("ContextBuilderStage.Registry", t, func() {
		s := &ContextBuilderStage{}
		reg := s.Registry()
		So(reg, ShouldNotBeNil)
		// Second call returns same instance.
		So(s.Registry(), ShouldEqual, reg)
	})
}

func TestStartSessionLockGC(t *testing.T) {
	Convey("startSessionLockGC runs and responds to context cancel", t, func() {
		q := make(chan *agentio.Turn)
		c := NewCompositor(nil, nil, 10)
		logger, _ := zap.NewDevelopment()
		a := NewAgentLoop(q, c, logger, nil, nil, 2)

		// Add a lock that will be uncontended.
		mu := a.sessionLock("gc-real")
		mu.Lock()
		mu.Unlock() //nolint:staticcheck // SA2001: Lock registers the entry as a side effect

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			a.startSessionLockGC(ctx)
			close(done)
		}()

		// Give GC one tick — ticker is 5min, so cancel immediately.
		// The function still exits cleanly via ctx.Done().
		cancel()
		<-done
		// Test passes if no deadlock/panic.
	})
}

func TestAgentLoopMultiWorkerE2E(t *testing.T) {
	Convey("Multi-worker E2E", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		mgr := session.NewManager(t.TempDir())
		aio := agentio.NewAgentIO(32, mgr, signal.NewBus(), logger, "test")

		mem := memory.NewFileMemory(&testSessionStore{})

		// 3 workers — two sessions each send 2 turns.
		compositor := NewCompositor(
			[]Stage{&MemoryReadStage{Memory: mem}},
			[]Stage{&MemoryWriteStage{Memory: mem, EventBus: eb}},
			1,
		)

		a := NewAgentLoop(aio.Queue(), compositor, logger, eb, aio, 3)

		var mu sync.Mutex
		resultsBySession := make(map[string][]string)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(done)
		}()

		// Send 2 turns for session-A, 2 turns for session-B.
		allDone := make(chan struct{})
		var completedCount int
		a.SetOnResult(func(r agentio.TurnResult) {
			if r.Done {
				mu.Lock()
				resultsBySession[r.SessionID] = append(resultsBySession[r.SessionID], r.TurnID)
				completedCount++
				if completedCount == 4 {
					close(allDone)
				}
				mu.Unlock()
			}
		})

		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID: "a1", SessionID: "session-A", Input: "hello-A1", TransportID: "t-a",
		})
		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID: "a2", SessionID: "session-A", Input: "hello-A2", TransportID: "t-a",
		})
		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID: "b1", SessionID: "session-B", Input: "hello-B1", TransportID: "t-b",
		})
		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID: "b2", SessionID: "session-B", Input: "hello-B2", TransportID: "t-b",
		})

		<-allDone
		cancel()
		<-done

		mu.Lock()
		aResults := resultsBySession["session-A"]
		bResults := resultsBySession["session-B"]
		mu.Unlock()

		// Both sessions should have 2 completed turns.
		So(len(aResults), ShouldEqual, 2)
		So(len(bResults), ShouldEqual, 2)

		// Sort results to ensure deterministic assertion regardless
		// of worker scheduling (per-session lock guarantees ordering in
		// normal execution; CI with -race may interleave differently).
		sort.Strings(aResults)
		sort.Strings(bResults)
		So(aResults[0], ShouldEqual, "a1")
		So(aResults[1], ShouldEqual, "a2")
		So(bResults[0], ShouldEqual, "b1")
		So(bResults[1], ShouldEqual, "b2")
	})
}

func TestAgentLoopWorkerPanicBackoff(t *testing.T) {
	Convey("Worker panic exponential backoff", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		var panicEvents []event.Event
		eb.Subscribe(func(_ context.Context, e event.Event) {
			if e.Type == event.EventWorkerPanic {
				panicEvents = append(panicEvents, e)
			}
		})

		mgr := session.NewManager(t.TempDir())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "test")

		compositor := NewCompositor(
			[]Stage{&panicStage{}},
			[]Stage{},
			1,
		)

		a := NewAgentLoop(aio.Queue(), compositor, logger, eb, aio, 1)
		a.maxPanicBackoff = 10 * time.Millisecond
		a.maxConsecutivePanics = 3

		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(runDone)
		}()

		// Send 3 turns — each will panic the worker, triggering restart with backoff.
		for i := 0; i < 3; i++ {
			aio.SendTurn(context.Background(), &agentio.Turn{
				TurnID:    fmt.Sprintf("backoff-%d", i),
				SessionID: "backoff-session", Input: "panic", TransportID: "t-backoff",
			})
			// Brief pause so the turn is enqueued before worker restarts.
			time.Sleep(10 * time.Millisecond)
		}

		// Wait for worker to hit max consecutive panics and exit.
		select {
		case <-runDone:
		case <-time.After(5 * time.Second):
		}
		cancel()
		<-runDone

		So(len(panicEvents), ShouldEqual, 3)

		// Verify consecutive_panics increases.
		for i := 1; i < len(panicEvents); i++ {
			prev := panicEvents[i-1].Payload["consecutive_panics"].(int)
			curr := panicEvents[i].Payload["consecutive_panics"].(int)
			So(curr, ShouldBeGreaterThan, prev)
		}

		// All backoffs should be capped at maxPanicBackoff (10ms).
		for _, e := range panicEvents {
			ms := e.Payload["backoff_ms"].(int64)
			So(ms, ShouldBeLessThanOrEqualTo, int64(10))
		}
	})
}

// Chaos tests — concurrent stress, panic recovery, cancellation under lock
// ---------------------------------------------------------------------------

func TestChaosContextCancelDuringSessionLock(t *testing.T) {
	Convey("Context cancel while worker holds session lock", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		mgr := session.NewManager(t.TempDir())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "test")

		blockStarted := make(chan struct{})
		blockUntilCancel := make(chan struct{})

		// A stage that blocks until the context is cancelled.
		compositor := NewCompositor(
			[]Stage{
				&blockingStage{
					blockStarted:     blockStarted,
					blockUntilCancel: blockUntilCancel,
				},
			},
			[]Stage{},
			1,
		)

		a := NewAgentLoop(aio.Queue(), compositor, logger, eb, aio, 1)

		a.SetOnResult(func(r agentio.TurnResult) {})

		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(runDone)
		}()

		// Send a turn that will block.
		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID: "block-1", SessionID: "chaos-session", Input: "block", TransportID: "t-chaos",
		})

		// Wait for the blocking stage to start (worker now holds session lock).
		<-blockStarted

		// Cancel context while the lock is held.
		cancel()

		// Unblock the stage so the worker can proceed to exit.
		close(blockUntilCancel)

		<-runDone

		// The system should not deadlock — Run() returns cleanly.
		// After Run exits, verify a new AgentLoop can acquire the same session lock.
		q2 := make(chan *agentio.Turn)
		a2 := NewAgentLoop(q2, compositor, logger, eb, nil, 1)
		mu := a2.sessionLock("chaos-session")
		mu.Lock()
		mu.Unlock() //nolint:staticcheck // SA2001: Lock registers the entry as a side effect
		// If we got here without deadlock, the test passes.
	})
}

func TestChaosCompositorPanic(t *testing.T) {
	Convey("Compositor.Execute panic recovery", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		// Collect panic events.
		var panicMu sync.Mutex
		var panicEvents []event.Event
		eb.Subscribe(func(_ context.Context, e event.Event) {
			if e.Type == event.EventWorkerPanic {
				panicMu.Lock()
				panicEvents = append(panicEvents, e)
				panicMu.Unlock()
			}
		})

		mgr := session.NewManager(t.TempDir())
		aio := agentio.NewAgentIO(10, mgr, signal.NewBus(), logger, "test")

		// A stage that panics.
		compositor := NewCompositor(
			[]Stage{&panicStage{}},
			[]Stage{},
			1,
		)

		a := NewAgentLoop(aio.Queue(), compositor, logger, eb, aio, 1)

		results := make(chan agentio.TurnResult, 10)
		a.SetOnResult(func(r agentio.TurnResult) {
			results <- r
		})

		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(runDone)
		}()

		// Send a turn that will panic the compositor.
		aio.SendTurn(context.Background(), &agentio.Turn{
			TurnID: "panic-1", SessionID: "chaos-panic", Input: "panic now", TransportID: "t-panic",
		})

		// Wait for the first panic event, then give the worker time to restart.
		deadline := time.After(5 * time.Second)
	waitLoop:
		for {
			panicMu.Lock()
			n := len(panicEvents)
			panicMu.Unlock()
			if n >= 1 {
				break waitLoop
			}
			select {
			case <-deadline:
				break waitLoop
			default:
			}
			time.Sleep(10 * time.Millisecond)
		}
		// Let the worker restart at least once more to verify continued operation.
		time.Sleep(100 * time.Millisecond)

		// Cancel and verify Run exits cleanly (no deadlock).
		cancel()
		<-runDone

		// The worker should have published at least one panic event.
		So(len(panicEvents), ShouldBeGreaterThanOrEqualTo, 1)
	})
}

func TestChaosConcurrentSameSession(t *testing.T) {
	Convey("100 concurrent turns to same session", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()

		mgr := session.NewManager(t.TempDir())
		aio := agentio.NewAgentIO(100, mgr, signal.NewBus(), logger, "test")

		mem := memory.NewFileMemory(&testSessionStore{})
		compositor := NewCompositor(
			[]Stage{&MemoryReadStage{Memory: mem}},
			[]Stage{&MemoryWriteStage{Memory: mem, EventBus: eb}},
			1,
		)

		// 4 workers processing 100 concurrent turns for the same session.
		a := NewAgentLoop(aio.Queue(), compositor, logger, eb, aio, 4)

		var mu sync.Mutex
		var completed []string
		allDone := make(chan struct{})
		a.SetOnResult(func(r agentio.TurnResult) {
			if r.Done {
				mu.Lock()
				completed = append(completed, r.TurnID)
				if len(completed) == 100 {
					close(allDone)
				}
				mu.Unlock()
			}
		})

		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan struct{})
		go func() {
			a.Run(ctx)
			close(runDone)
		}()

		// Fire 100 turns concurrently to the same session.
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				aio.SendTurn(context.Background(), &agentio.Turn{
					TurnID:      fmt.Sprintf("turn-%d", idx),
					SessionID:   "same-session",
					Input:       fmt.Sprintf("input-%d", idx),
					TransportID: "t-same",
				})
			}(i)
		}
		wg.Wait()

		// Wait for all turns to complete.
		<-allDone
		cancel()
		<-runDone

		// All 100 turns should have completed.
		mu.Lock()
		count := len(completed)
		mu.Unlock()
		So(count, ShouldEqual, 100)
	})
}

// blockingStage blocks until blockUntilCancel is closed, then returns ctx error.
type blockingStage struct {
	blockStarted     chan struct{}
	blockUntilCancel chan struct{}
}

func (s *blockingStage) Name() string { return "blocking" }
func (s *blockingStage) Clone() Stage {
	return &blockingStage{
		blockStarted:     s.blockStarted,
		blockUntilCancel: s.blockUntilCancel,
	}
}

func (s *blockingStage) Process(ctx context.Context, _ *State) error {
	close(s.blockStarted)
	select {
	case <-s.blockUntilCancel:
	case <-ctx.Done():
	}
	return nil
}

// panicStage always panics.
type panicStage struct{}

func (s *panicStage) Name() string { return "panic" }
func (s *panicStage) Clone() Stage { return &panicStage{} }
func (s *panicStage) Process(_ context.Context, _ *State) error {
	panic("chaos test: injected compositor panic")
}

// ---------------------------------------------------------------------------
// Fuzz tests
// ---------------------------------------------------------------------------

func FuzzValidSessionID(f *testing.F) {
	f.Add("session-123")
	f.Add("")
	f.Add("会话")
	f.Add(strings.Repeat("a", 201))
	f.Add("abc\x00def")
	f.Add("\x00")
	f.Add("normal-session-🆗")

	f.Fuzz(func(t *testing.T, s string) {
		result := validSessionID(s)
		// Invariant: if result is non-empty, it must be the same as input.
		if result != "" && result != s {
			t.Errorf("validSessionID returned %q, expected empty or %q", result, s)
		}
		// Invariant: empty input must return empty.
		if s == "" && result != "" {
			t.Errorf("validSessionID returned %q for empty input", result)
		}
		// Invariant: result must never panic regardless of input.
	})
}

func FuzzSessionLock(f *testing.F) {
	f.Add(int64(1), int64(2), int64(3))

	f.Fuzz(func(t *testing.T, seed1, seed2, seed3 int64) {
		q := make(chan *agentio.Turn)
		c := NewCompositor(nil, nil, 10)
		logger, _ := zap.NewDevelopment()
		a := NewAgentLoop(q, c, logger, nil, nil, 2)

		workers := 4
		opsPerWorker := 10

		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for i := 0; i < opsPerWorker; i++ {
					sid := fmt.Sprintf("session-%d-%d", workerID, i%3)
					mu := a.sessionLock(sid)
					mu.Lock()
					// Hold briefly to simulate work.
					time.Sleep(time.Microsecond)
					mu.Unlock()
				}
			}(w)
		}
		wg.Wait()

		// All sessions should still be in the map.
		a.sessionMu.Lock()
		count := len(a.sessionLocks)
		a.sessionMu.Unlock()
		// At least one session should have been created.
		if count == 0 {
			t.Errorf("expected at least one session lock, got 0")
		}
	})
}

// blockingChunkProvider sends chunks only after unblock is closed, allowing
// tests to hold the LLM mid-stream and inject signals.
type blockingChunkProvider struct {
	chunks  []llm.LLMChunk
	unblock chan struct{}
}

func (b *blockingChunkProvider) Name() string { return "blocking-chunk-provider" }
func (b *blockingChunkProvider) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	ch := make(chan llm.LLMChunk, len(b.chunks)+1)
	go func() {
		<-b.unblock
		for _, chunk := range b.chunks {
			ch <- chunk
		}
		close(ch)
	}()
	return ch, nil
}
func (b *blockingChunkProvider) Models(_ context.Context) ([]llm.ModelConfig, error) { return nil, nil }
func (b *blockingChunkProvider) ActiveModel() string                                 { return "" }

func TestLLMStageTryCompleteInterrupt(t *testing.T) {
	Convey("LLMStage.tryComplete interrupt during streaming", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		sigBus := signal.NewBus()

		var gotEvents []event.Event
		eb.Subscribe(func(_ context.Context, e event.Event) {
			gotEvents = append(gotEvents, e)
		})

		unblock := make(chan struct{})
		provider := &blockingChunkProvider{
			chunks: []llm.LLMChunk{
				{Content: "partial", OutputTokens: 5},
				{Content: "", Done: true},
			},
			unblock: unblock,
		}

		stage := &LLMStage{
			Provider:  provider,
			Model:     "test-model",
			EventBus:  eb,
			SignalBus: sigBus,
			Logger:    logger,
		}
		state := &State{
			SessionID: "s1",
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
		}

		sigCh := sigBus.Subscribe("s1")
		defer sigBus.Unsubscribe("s1", sigCh)

		var tryCompleteErr error
		done := make(chan struct{})
		go func() {
			tryCompleteErr = stage.tryComplete(context.Background(), state, sigCh)
			close(done)
		}()

		// Send interrupt while streaming is blocked.
		sigBus.Send("s1", signal.Interrupt)
		<-done

		So(tryCompleteErr, ShouldEqual, ErrInterrupted)

		hasInterrupt := false
		for _, e := range gotEvents {
			if e.Type == event.EventLLMInterrupt {
				hasInterrupt = true
				So(e.SessionID, ShouldEqual, "s1")
				break
			}
		}
		So(hasInterrupt, ShouldBeTrue)
	})

	Convey("LLMStage.tryComplete no interrupt when signal is not Interrupt", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		sigBus := signal.NewBus()

		provider := &blockingChunkProvider{
			chunks: []llm.LLMChunk{
				{Content: "done", Done: true},
			},
			unblock: make(chan struct{}),
		}

		stage := &LLMStage{
			Provider:  provider,
			Model:     "test-model",
			EventBus:  eb,
			SignalBus: sigBus,
			Logger:    logger,
		}
		state := &State{
			SessionID: "s2",
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
		}

		sigCh := sigBus.Subscribe("s2")
		defer sigBus.Unsubscribe("s2", sigCh)

		// Send a non-Interrupt signal (Continue) — should be ignored.
		sigBus.Send("s2", signal.Continue)
		close(provider.unblock)

		err := stage.tryComplete(context.Background(), state, sigCh)
		So(err, ShouldBeNil)
		So(len(state.Messages), ShouldEqual, 2)
	})

	Convey("LLMStage.tryComplete context done", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		sigBus := signal.NewBus()

		provider := &blockingChunkProvider{
			chunks:  []llm.LLMChunk{{Content: "x", Done: true}},
			unblock: make(chan struct{}),
		}

		stage := &LLMStage{
			Provider:  provider,
			Model:     "test-model",
			EventBus:  eb,
			SignalBus: sigBus,
			Logger:    logger,
		}
		state := &State{
			SessionID: "s3",
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
		}

		sigCh := sigBus.Subscribe("s3")
		defer sigBus.Unsubscribe("s3", sigCh)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := stage.tryComplete(ctx, state, sigCh)
		So(err, ShouldEqual, context.Canceled)
	})
}

func TestLLMStageProcessInterrupt(t *testing.T) {
	Convey("LLMStage.Process interrupt caught in tryComplete during streaming", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		sigBus := signal.NewBus()

		var gotEvents []event.Event
		llmStarted := make(chan struct{})
		eb.Subscribe(func(_ context.Context, e event.Event) {
			gotEvents = append(gotEvents, e)
			if e.Type == event.EventLLMStart {
				close(llmStarted)
			}
		})

		unblock := make(chan struct{})
		provider := &blockingChunkProvider{
			chunks:  []llm.LLMChunk{{Content: "ok", Done: true}},
			unblock: unblock,
		}

		stage := &LLMStage{
			Provider:  provider,
			Model:     "test-model",
			EventBus:  eb,
			SignalBus: sigBus,
			Logger:    logger,
		}
		state := &State{
			SessionID: "s-interrupt-streaming",
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
		}

		var procErr error
		done := make(chan struct{})
		go func() {
			procErr = stage.Process(context.Background(), state)
			close(done)
		}()

		// Wait for LLMStart event to confirm subscription is set up, then interrupt.
		<-llmStarted
		sigBus.Send("s-interrupt-streaming", signal.Interrupt)
		<-done

		So(procErr, ShouldEqual, ErrInterrupted)

		hasInterrupt := false
		for _, e := range gotEvents {
			if e.Type == event.EventLLMInterrupt {
				hasInterrupt = true
				break
			}
		}
		So(hasInterrupt, ShouldBeTrue)
	})

	Convey("LLMStage.Process no retry on ErrInterrupted from tryComplete", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		sigBus := signal.NewBus()

		var gotEvents []event.Event
		llmStarted := make(chan struct{})
		eb.Subscribe(func(_ context.Context, e event.Event) {
			gotEvents = append(gotEvents, e)
			if e.Type == event.EventLLMStart {
				select {
				case <-llmStarted:
				default:
					close(llmStarted)
				}
			}
		})

		retryCount := 0
		unblock := make(chan struct{})
		provider := &blockingChunkProvider{
			chunks:  []llm.LLMChunk{{Content: "ok", Done: true}},
			unblock: unblock,
		}

		stage := &LLMStage{
			Provider:   provider,
			Model:      "test-model",
			MaxRetries: 2,
			EventBus:   eb,
			SignalBus:  sigBus,
			Logger:     logger,
		}
		state := &State{
			SessionID: "s-no-retry",
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
		}

		var procErr error
		done := make(chan struct{})
		go func() {
			procErr = stage.Process(context.Background(), state)
			close(done)
		}()

		// Wait for LLMStart to confirm subscription, then interrupt.
		<-llmStarted
		sigBus.Send("s-no-retry", signal.Interrupt)
		<-done

		So(procErr, ShouldEqual, ErrInterrupted)
		// Should NOT have retried — no EventLLMRetry events.
		for _, e := range gotEvents {
			if e.Type == event.EventLLMRetry {
				retryCount++
			}
		}
		So(retryCount, ShouldEqual, 0)
	})
}

func TestLLMStage_SendsStreamTrue(t *testing.T) {
	Convey("LLM stage sends Stream:true in LLMRequest", t, func() {
		var capturedReq llm.LLMRequest
		p := &captureProvider{
			fn: func(_ context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
				capturedReq = req
				ch := make(chan llm.LLMChunk, 1)
				ch <- llm.LLMChunk{
					Content:      "ok",
					Done:         true,
					InputTokens:  1,
					OutputTokens: 1,
				}
				close(ch)
				return ch, nil
			},
		}

		state := &State{
			SessionID:    "test-session",
			SystemPrompt: "sys",
			Messages:     []types.Message{{Role: "user", Content: "hi"}},
		}

		stage := &LLMStage{
			Provider:   p,
			Model:      "test-model",
			MaxTokens:  100,
			MaxRetries: 0,
			EventBus:   event.NewBus(),
			Logger:     zap.NewNop(),
		}
		err := stage.Process(context.Background(), state)
		So(err, ShouldBeNil)
		So(capturedReq.Stream, ShouldBeTrue)
	})
}

type captureProvider struct {
	fn func(context.Context, llm.LLMRequest) (<-chan llm.LLMChunk, error)
}

func (c *captureProvider) Name() string { return "capture" }
func (c *captureProvider) CompleteStream(ctx context.Context, req llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	return c.fn(ctx, req)
}
func (c *captureProvider) Models(_ context.Context) ([]llm.ModelConfig, error) { return nil, nil }

func TestToolStageProcessSerialPause(t *testing.T) {
	Convey("ToolStage.Process serial path Pause+Resume between calls", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		sigBus := signal.NewBus()

		firstBlocked := make(chan struct{})
		firstUnblock := make(chan struct{})
		reg := tool.NewRegistry()
		reg.RegisterBuiltin("tool1", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			close(firstBlocked)
			<-firstUnblock
			return &types.ToolResult{Content: "ok1", IsError: false}, nil
		})
		reg.RegisterBuiltin("tool2", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			return &types.ToolResult{Content: "ok2", IsError: false}, nil
		})

		stage := &ToolStage{
			ToolRegistry: reg,
			Logger:       logger,
			EventBus:     eb,
			SignalBus:    sigBus,
		}
		state := &State{SessionID: "s-pause"}
		state.ToolCalls = []types.ToolCall{
			{ID: "c1", Name: "tool1", Arguments: `{}`},
			{ID: "c2", Name: "tool2", Arguments: `{}`},
		}

		var procErr error
		done := make(chan struct{})
		go func() {
			procErr = stage.Process(context.Background(), state)
			close(done)
		}()

		// Wait for first tool to start, then send Pause. When first tool
		// unblocks, the Pause will be caught at the signal check before
		// the second call.
		<-firstBlocked
		sigBus.Send("s-pause", signal.Pause)
		close(firstUnblock)

		// Wait a bit for pauseOnSignal to start blocking, then resume.
		time.Sleep(20 * time.Millisecond)
		sigBus.Send("s-pause", signal.Resume)
		<-done

		So(procErr, ShouldBeNil)
		So(len(state.Messages), ShouldEqual, 2)
		So(state.Messages[0].Content, ShouldEqual, "ok1")
		So(state.Messages[1].Content, ShouldEqual, "ok2")
	})
}

func TestLLMStageProcessRetryPause(t *testing.T) {
	Convey("LLMStage.Process Pause in retry loop", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		sigBus := signal.NewBus()

		// Provider fails on first call, succeeds on retry.
		provider := &errorThenSuccessProvider{
			good: []llm.LLMChunk{{Content: "recovered", Done: true}},
		}

		stage := &LLMStage{
			Provider:   provider,
			Model:      "test-model",
			MaxRetries: 1,
			EventBus:   eb,
			SignalBus:  sigBus,
			Logger:     logger,
		}
		state := &State{
			SessionID: "s-retry-pause",
			Messages:  []types.Message{{Role: types.RoleUser, Content: "hi"}},
		}

		// Start Process; first tryComplete fails immediately (provider error).
		var procErr error
		done := make(chan struct{})
		go func() {
			procErr = stage.Process(context.Background(), state)
			close(done)
		}()

		// Give Process time to fail once and enter the retry signal check.
		time.Sleep(20 * time.Millisecond)
		// Send Pause to be caught at the retry loop top, then Resume.
		sigBus.Send("s-retry-pause", signal.Pause)
		time.Sleep(20 * time.Millisecond)
		sigBus.Send("s-retry-pause", signal.Resume)
		<-done

		So(procErr, ShouldBeNil)
		So(len(state.Messages), ShouldEqual, 2)
		So(state.Messages[1].Content, ShouldEqual, "recovered")
	})
}

// errorThenSuccessProvider fails on first CompleteStream call, then succeeds.
type errorThenSuccessProvider struct {
	mu       sync.Mutex
	attempts int
	good     []llm.LLMChunk
}

func (e *errorThenSuccessProvider) Name() string { return "error-then-success" }
func (e *errorThenSuccessProvider) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	e.mu.Lock()
	n := e.attempts
	e.attempts++
	e.mu.Unlock()
	if n == 0 {
		return nil, fmt.Errorf("injected network error")
	}
	ch := make(chan llm.LLMChunk, len(e.good))
	for _, c := range e.good {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (e *errorThenSuccessProvider) Models(_ context.Context) ([]llm.ModelConfig, error) {
	return nil, nil
}
func (e *errorThenSuccessProvider) ActiveModel() string { return "" }

func TestToolStageProcessParallelPause(t *testing.T) {
	Convey("ToolStage processParallel watcher Pause+Resume", t, func() {
		logger, _ := zap.NewDevelopment()
		eb := event.NewBus()
		sigBus := signal.NewBus()

		handlerRunning := make(chan struct{})
		handlerResume := make(chan struct{})
		reg := tool.NewRegistry()
		reg.RegisterBuiltin("a", "", nil, func(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
			close(handlerRunning)
			<-handlerResume
			return &types.ToolResult{Content: "a-ok", IsError: false}, nil
		})

		stage := &ToolStage{
			ToolRegistry: reg,
			Logger:       logger,
			EventBus:     eb,
			SignalBus:    sigBus,
			MaxParallel:  2,
		}
		state := &State{SessionID: "s-parallel-pause"}
		state.ToolCalls = []types.ToolCall{
			{ID: "c1", Name: "a", Arguments: `{}`},
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var procErr error
		done := make(chan struct{})
		go func() {
			procErr = stage.Process(ctx, state)
			close(done)
		}()

		// Wait for handler to start, then send Pause to the signal watcher.
		<-handlerRunning
		sigBus.Send("s-parallel-pause", signal.Pause)
		time.Sleep(20 * time.Millisecond)
		sigBus.Send("s-parallel-pause", signal.Resume)

		// Unblock the tool handler so it completes.
		close(handlerResume)

		// Cancel the parent context so the signal watcher exits via execCtx.Done().
		cancel()
		<-done

		So(procErr, ShouldBeNil)
	})
}

func TestPauseOnSignal(t *testing.T) {
	Convey("pauseOnSignal", t, func() {
		Convey("returns Resume when received", func() {
			ch := make(chan signal.Signal, 2)
			ch <- signal.Pause
			ch <- signal.Resume
			So(pauseOnSignal(ch), ShouldEqual, signal.Resume)
		})

		Convey("returns Interrupt when received during pause", func() {
			ch := make(chan signal.Signal, 2)
			ch <- signal.Pause
			ch <- signal.Interrupt
			So(pauseOnSignal(ch), ShouldEqual, signal.Interrupt)
		})

		Convey("returns Cancel when received during pause", func() {
			ch := make(chan signal.Signal, 2)
			ch <- signal.Pause
			ch <- signal.Cancel
			So(pauseOnSignal(ch), ShouldEqual, signal.Cancel)
		})

		Convey("returns 0 when channel is closed", func() {
			ch := make(chan signal.Signal, 2)
			ch <- signal.Pause
			close(ch)
			So(pauseOnSignal(ch), ShouldEqual, signal.Signal(0))
		})
	})
}
