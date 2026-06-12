package command

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"dolphin/internal/brain"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/limit"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/skill"
	"dolphin/internal/tool"
	transport "dolphin/internal/transport"

	"dolphin/internal/agentio"
	"dolphin/internal/types"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
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
			So(func() { r.Execute(context.Background(), "version", "none") }, ShouldNotPanic)
		})

		Convey("/session new creates session", func() {
			So(mgr.Active(), ShouldBeNil)
			r.Execute(context.Background(), "session new", "none")
			So(mgr.Active(), ShouldNotBeNil)
		})

		Convey("unknown command does not panic", func() {
			So(func() { r.Execute(context.Background(), "nonexistent", "none") }, ShouldNotPanic)
		})
	})
}

func TestRegistryExecuteContext(t *testing.T) {
	Convey("Registry.Execute context propagation", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		// Register a test command that captures the transport ID from context.
		var (
			mu         sync.Mutex
			capturedID string
		)
		whoami := &cobra.Command{
			Use: "whoami",
			RunE: func(cmd *cobra.Command, args []string) error {
				info := transport.GetInfo(cmd.Context())
				mu.Lock()
				if info != nil {
					capturedID = info.ID
				} else {
					capturedID = ""
				}
				mu.Unlock()
				return nil
			},
		}
		r.Register(whoami)
		So(r.HasCommand("whoami"), ShouldBeTrue)

		Convey("command sees transport info from context", func() {
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "wework"})
			r.Execute(ctx, "whoami", "none")
			mu.Lock()
			So(capturedID, ShouldEqual, "wework")
			mu.Unlock()
		})

		Convey("different transports get their own info sequentially", func() {
			ctxDing := transport.WithInfo(context.Background(), &transport.Info{ID: "dingtalk"})
			r.Execute(ctxDing, "whoami", "none")
			mu.Lock()
			So(capturedID, ShouldEqual, "dingtalk")
			mu.Unlock()

			ctxWe := transport.WithInfo(context.Background(), &transport.Info{ID: "wework"})
			r.Execute(ctxWe, "whoami", "none")
			mu.Lock()
			So(capturedID, ShouldEqual, "wework")
			mu.Unlock()
		})
	})
}

func TestRegistryExecuteContextConcurrent(t *testing.T) {
	Convey("Registry.Execute concurrent context isolation", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		results := make(map[string]int)
		var resMu sync.Mutex

		concCmd := &cobra.Command{
			Use: "concur",
			RunE: func(cmd *cobra.Command, args []string) error {
				info := transport.GetInfo(cmd.Context())
				id := "unknown"
				if info != nil {
					id = info.ID
				}
				resMu.Lock()
				results[id]++
				resMu.Unlock()
				return nil
			},
		}
		r.Register(concCmd)
		So(r.HasCommand("concur"), ShouldBeTrue)

		numGoroutines := 20
		var wg sync.WaitGroup
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				ctx := transport.WithInfo(context.Background(), &transport.Info{ID: id})
				r.Execute(ctx, "concur", "none")
			}(fmt.Sprintf("t_%d", i))
		}
		wg.Wait()

		So(len(results), ShouldEqual, numGoroutines)
		for id, count := range results {
			So(count, ShouldEqual, 1)
			So(id, ShouldStartWith, "t_")
		}
	})
}

func TestRegistrySetAgentIO(t *testing.T) {
	Convey("SetAgentIO", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		So(r.agentIO, ShouldBeNil)

		r.SetAgentIO(nil)
		So(r.agentIO, ShouldBeNil)
	})
}

func TestQueuePop(t *testing.T) {
	Convey("/queue pop", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		logger, _ := zap.NewDevelopment()
		aio := agentio.NewAgentIO(10, mgr, sb, logger, "test")
		r.SetAgentIO(aio)
		RegisterQueue(r)

		Convey("pops a turn by index", func() {
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "t1"})
			aio.SendTurn(ctx, &agentio.Turn{Input: "first"})
			aio.SendTurn(ctx, &agentio.Turn{Input: "second"})

			pending, _, _ := aio.QueueSnapshot()
			So(len(pending), ShouldEqual, 2)

			// Pop index 1 (first item)
			r.Execute(context.Background(), "queue pop 1", "none")

			pending, _, _ = aio.QueueSnapshot()
			So(len(pending), ShouldEqual, 1)
			So(pending[0].Input, ShouldEqual, "second")
		})

		Convey("pop with invalid index is rejected", func() {
			So(func() { r.Execute(context.Background(), "queue pop abc", "none") }, ShouldNotPanic)
			So(func() { r.Execute(context.Background(), "queue pop 0", "none") }, ShouldNotPanic)
			So(func() { r.Execute(context.Background(), "queue pop -1", "none") }, ShouldNotPanic)
		})

		Convey("pop out of bounds reports error", func() {
			r.Execute(context.Background(), "queue pop 99", "none") // queue is empty, should not panic
		})

		Convey("queue status shows cancelled items are gone", func() {
			ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "t1"})
			aio.SendTurn(ctx, &agentio.Turn{Input: "only"})

			pending, _, _ := aio.QueueSnapshot()
			So(len(pending), ShouldEqual, 1)

			r.Execute(context.Background(), "queue pop 1", "none")

			pending, _, _ = aio.QueueSnapshot()
			So(len(pending), ShouldEqual, 0)
		})
	})
}

func TestTokenVal(t *testing.T) {
	Convey("tokenVal", t, func() {
		Convey("returns 0 for nil", func() {
			So(tokenVal(nil), ShouldEqual, 0)
		})
		Convey("returns int value directly", func() {
			So(tokenVal(42), ShouldEqual, 42)
		})
		Convey("returns 0 for wrong type", func() {
			So(tokenVal("not-an-int"), ShouldEqual, 0)
		})
		Convey("returns 0 for zero int", func() {
			So(tokenVal(0), ShouldEqual, 0)
		})
	})
}

func TestComma(t *testing.T) {
	Convey("comma", t, func() {
		Convey("returns number as-is when 3 or fewer digits", func() {
			So(comma(0), ShouldEqual, "0")
			So(comma(1), ShouldEqual, "1")
			So(comma(999), ShouldEqual, "999")
		})
		Convey("adds thousand separators", func() {
			So(comma(1000), ShouldEqual, "1,000")
			So(comma(1234567), ShouldEqual, "1,234,567")
			So(comma(1000000), ShouldEqual, "1,000,000")
		})
		Convey("handles negative numbers", func() {
			So(comma(-1000), ShouldEqual, "-1,000")
		})
	})
}

func TestRegisterContext(t *testing.T) {
	Convey("RegisterContext", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		RegisterContext(r, func(ctx context.Context) (string, error) {
			return "custom context", nil
		})

		output := r.Execute(context.Background(), "context", "")
		So(output, ShouldEqual, "custom context")
	})

	Convey("RegisterContext error path", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		RegisterContext(r, func(ctx context.Context) (string, error) {
			return "", fmt.Errorf("build failed")
		})

		// Error is returned via RunE, output goes to stderr not stdout buffer.
		output := r.Execute(context.Background(), "context", "")
		So(output, ShouldBeBlank)
	})
}

func TestRegisterSkills(t *testing.T) {
	Convey("RegisterSkills", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		skStore := skill.NewFileStore(t.TempDir())
		ctx := context.Background()
		skStore.Save(ctx, skill.Skill{Name: "helper", Description: "helper tool", Enabled: true})
		skStore.Save(ctx, skill.Skill{Name: "disabled-one", Description: "not used", Enabled: false})

		RegisterSkills(r, skStore)

		Convey("list shows all skills with status", func() {
			output := r.Execute(context.Background(), "skills list", "")
			So(output, ShouldContainSubstring, "helper")
			So(output, ShouldContainSubstring, "disabled-one")
			So(output, ShouldContainSubstring, "(enabled)")
			So(output, ShouldContainSubstring, "(disabled)")
		})

		Convey("enable command", func() {
			output := r.Execute(context.Background(), "skills enable helper", "")
			So(output, ShouldContainSubstring, "enabled")
		})

		Convey("disable command", func() {
			output := r.Execute(context.Background(), "skills disable helper", "")
			So(output, ShouldContainSubstring, "disabled")
		})
	})
}

func TestRegisterScheduler(t *testing.T) {
	Convey("RegisterScheduler", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		now := time.Now()
		tasks := []*scheduler.Task{
			{Name: "daily", ID: "1", Schedule: "0 9 * * *", Enabled: true, RunCount: 5, LastRunAt: &now},
			{Name: "off", ID: "2", Schedule: "0 0 * * *", Enabled: false},
			{Name: "delayed", ID: "3", Delay: "5m", Enabled: true, RunCount: 1, LastStatus: "success"},
		}
		RegisterScheduler(r, &mockSchedLister{tasks: tasks})

		output := r.Execute(context.Background(), "scheduler", "")
		So(output, ShouldContainSubstring, "daily")
		So(output, ShouldContainSubstring, "off")
		So(output, ShouldContainSubstring, "delayed")
		So(output, ShouldContainSubstring, "disabled")
		So(output, ShouldContainSubstring, "success")
	})

	Convey("RegisterScheduler empty", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		RegisterScheduler(r, &mockSchedLister{})
		output := r.Execute(context.Background(), "scheduler", "")
		So(output, ShouldContainSubstring, "No scheduled tasks")
	})
}

func TestRegisterMCP(t *testing.T) {
	Convey("RegisterMCP", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		mgrMock := &mockMCPSource{
			defs: []types.ToolDef{
				{Name: "brain_read", Description: "read brain"},
				{Name: "shell", Description: "run command"},
				{Name: "custom_tool", Description: "custom"},
			},
			sources: []tool.SourceInfo{
				{Name: "local", Enabled: true},
				{Name: "remote", Enabled: false},
			},
		}
		RegisterMCP(r, mgrMock)

		Convey("list shows categorized tools", func() {
			output := r.Execute(context.Background(), "mcp", "")
			So(output, ShouldContainSubstring, "brain_read")
			So(output, ShouldContainSubstring, "shell")
			So(output, ShouldContainSubstring, "custom_tool")
			So(output, ShouldContainSubstring, "local")
		})

		Convey("disable source", func() {
			output := r.Execute(context.Background(), "mcp disable remote", "")
			So(output, ShouldContainSubstring, "disabled")
		})

		Convey("enable source", func() {
			output := r.Execute(context.Background(), "mcp enable remote", "")
			So(output, ShouldContainSubstring, "enabled")
		})
	})
}

func TestRegisterLimit(t *testing.T) {
	Convey("RegisterLimit", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		cfg := config.LoadConfigFromMap(map[string]any{
			"llm.limit.max_requests.hard":                "100",
			"llm.limit.max_input_tokens":                 "50000",
			"llm.limit.max_output_tokens.hard":           "100000",
			"llm.limit.max_total_tokens":                 "150000",
			"llm.openai.api_key":                         "sk-test",
			"llm.openai.models.0.name":                   "deepseek-v4-flash",
			"llm.openai.models.0.limit.max_requests":     "50",
			"llm.openai.models.0.limit.max_total_tokens": "25000",
		})
		store := limit.NewMemoryStore()
		bus := event.NewBus()
		logger, _ := zap.NewDevelopment()
		limiter := limit.NewLimiter(store, cfg, bus, logger)

		limiter.RecordLLM("deepseek-v4-flash", 1000, 500)

		RegisterLimit(r, limiter)

		output := r.Execute(context.Background(), "limit", "")
		So(output, ShouldContainSubstring, "Global Limits")
		So(output, ShouldContainSubstring, "requests")
		So(output, ShouldContainSubstring, "input tokens")
		So(output, ShouldContainSubstring, "output tokens")
		So(output, ShouldContainSubstring, "total tokens")
		So(output, ShouldContainSubstring, "deepseek-v4-flash")
		So(output, ShouldContainSubstring, "50")
	})

	Convey("RegisterLimit with nil limiter", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		So(func() { RegisterLimit(r, nil) }, ShouldNotPanic)
	})
}

func TestRegisterModels(t *testing.T) {
	Convey("RegisterModels", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		Convey("list models with data", func() {
			mockProv := &mockLister{
				models: []llm.ModelConfig{
					{Name: "gpt-4", Provider: "openai", Vendor: "OpenAI", APIType: "openai", Model: "gpt-4"},
					{Name: "claude-3", Provider: "anthropic", Vendor: "Anthropic", APIType: "anthropic", Model: "claude-3"},
				},
			}
			RegisterModels(r, mockProv)

			output := r.Execute(context.Background(), "models", "")
			So(output, ShouldContainSubstring, "gpt-4")
			So(output, ShouldContainSubstring, "claude-3")
			So(output, ShouldContainSubstring, "OpenAI")
			So(output, ShouldContainSubstring, "Anthropic")
		})

		Convey("list models when none available", func() {
			mockProv := &mockLister{}
			RegisterModels(r, mockProv)
			output := r.Execute(context.Background(), "models", "")
			So(output, ShouldContainSubstring, "No models available")
		})

		Convey("list subcommand works", func() {
			mockProv := &mockLister{
				models: []llm.ModelConfig{
					{Name: "model-a", Vendor: "test", APIType: "openai", Model: "gpt-4"},
				},
			}
			RegisterModels(r, mockProv)
			output := r.Execute(context.Background(), "models list", "")
			So(output, ShouldContainSubstring, "model-a")
		})

		Convey("models use switches active model", func() {
			mockProv := &mockLister{
				models: []llm.ModelConfig{
					{Name: "model-a", Vendor: "test", APIType: "openai", Model: "gpt-4"},
				},
			}
			RegisterModels(r, mockProv)
			output := r.Execute(context.Background(), "models use model-a", "")
			So(output, ShouldContainSubstring, "switched to model-a")
		})
	})
}

func TestRegisterSessionStatus(t *testing.T) {
	Convey("RegisterSessionStatus", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		sess := mgr.Create(context.Background())
		sess.Set("rounds", 5)
		sess.Set("tool_calls", 12)
		sess.Set("system_context", 42)
		sess.Set("input_tokens", 1000)
		sess.Set("output_tokens", 500)

		mem := memory.NewFileMemory(t.TempDir(), 100)
		mem.Write(context.Background(), sess.ID, types.Message{Role: types.RoleUser, Content: "hello"})

		RegisterSessionStatus(r, mgr, mem, "shared", nil)

		Convey("status shows session info", func() {
			output := r.Execute(context.Background(), "session status", "")
			So(output, ShouldContainSubstring, sess.ID[:8])
			So(output, ShouldContainSubstring, "shared")
			So(output, ShouldContainSubstring, "5")
			So(output, ShouldContainSubstring, "12")
		})

		Convey("/status alias works", func() {
			output := r.Execute(context.Background(), "status", "")
			So(output, ShouldContainSubstring, "Session ID")
		})
	})
}

func TestRegisterSessionStatusWithMode(t *testing.T) {
	Convey("RegisterSessionStatus with session mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		mem := memory.NewFileMemory(t.TempDir(), 100)

		sess := mgr.Create(context.Background())
		sess.Set("rounds", 3)

		RegisterSessionStatus(r, mgr, mem, "per_transport", nil)
		output := r.Execute(context.Background(), "session status", "")
		So(output, ShouldContainSubstring, sess.ID[:8])
		So(output, ShouldContainSubstring, "per_transport")
		So(output, ShouldContainSubstring, "3")
	})
}

func TestRegisterCommands(t *testing.T) {
	Convey("RegisterCommands", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		br := brain.New(t.TempDir())
		br.Init(context.Background())
		RegisterCommands(r, br)

		Convey("list commands when none", func() {
			output := r.Execute(context.Background(), "commands", "")
			So(output, ShouldContainSubstring, "No commands")
		})

		Convey("commands list subcommand when none", func() {
			output := r.Execute(context.Background(), "commands list", "")
			So(output, ShouldContainSubstring, "No commands")
		})

		Convey("list shows created commands", func() {
			brain.WriteCommand(context.Background(), br, brain.Command{
				Name: "test-cmd", Description: "a test", Enabled: true, Content: "run this",
			})
			output := r.Execute(context.Background(), "commands", "")
			So(output, ShouldContainSubstring, "test-cmd")
			So(output, ShouldContainSubstring, "enabled")
		})

		Convey("show command by name", func() {
			brain.WriteCommand(context.Background(), br, brain.Command{
				Name: "show-cmd", Description: "show test", Enabled: true, Content: "content here",
			})
			output := r.Execute(context.Background(), "commands show show-cmd", "")
			So(output, ShouldContainSubstring, "show-cmd")
			So(output, ShouldContainSubstring, "content here")
		})

		Convey("show command not found", func() {
			output := r.Execute(context.Background(), "commands show nonexistent", "")
			So(output, ShouldContainSubstring, "not found")
		})
	})
}

func TestRegisterScripts(t *testing.T) {
	Convey("RegisterScripts", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		br := brain.New(t.TempDir())
		br.Init(context.Background())
		RegisterScripts(r, br)

		Convey("list scripts when none", func() {
			output := r.Execute(context.Background(), "script", "")
			So(output, ShouldContainSubstring, "No scripts")
		})

		Convey("script list subcommand when none", func() {
			output := r.Execute(context.Background(), "script list", "")
			So(output, ShouldContainSubstring, "No scripts")
		})

		Convey("list shows created scripts", func() {
			brain.WriteScript(context.Background(), br, brain.Script{
				Name: "greet", Description: "say hello", Enabled: true, Content: "echo hello",
			})
			output := r.Execute(context.Background(), "script", "")
			So(output, ShouldContainSubstring, "greet")
			So(output, ShouldContainSubstring, "enabled")
		})

		Convey("show script by name", func() {
			brain.WriteScript(context.Background(), br, brain.Script{
				Name: "hello-script", Description: "a script", Enabled: true, Content: "echo hi",
			})
			output := r.Execute(context.Background(), "script show hello-script", "")
			So(output, ShouldContainSubstring, "hello-script")
			So(output, ShouldContainSubstring, "echo hi")
		})

		Convey("show script not found", func() {
			output := r.Execute(context.Background(), "script show nonexistent", "")
			So(output, ShouldContainSubstring, "not found")
		})

		Convey("delete script", func() {
			brain.WriteScript(context.Background(), br, brain.Script{
				Name: "del-script", Description: "to delete", Enabled: true, Content: "bye",
			})
			output := r.Execute(context.Background(), "script delete del-script", "")
			So(output, ShouldContainSubstring, "deleted")

			_, err := brain.ReadScript(context.Background(), br, "del-script")
			So(err, ShouldNotBeNil)
		})

		Convey("delete script error", func() {
			output := r.Execute(context.Background(), "script delete nonexistent", "")
			So(output, ShouldContainSubstring, "error")
		})
	})
}

func TestRegisterSubscriptionCmd(t *testing.T) {
	Convey("RegisterSubscriptionCmd", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		br := brain.New(t.TempDir())
		br.Init(context.Background())
		RegisterSubscriptionCmd(r, br)

		Convey("list subscriptions when none", func() {
			output := r.Execute(context.Background(), "subscription", "")
			So(output, ShouldContainSubstring, "No subscriptions")
		})

		Convey("subscription list subcommand when none", func() {
			output := r.Execute(context.Background(), "subscription list", "")
			So(output, ShouldContainSubstring, "No subscriptions")
		})

		Convey("list shows created subscriptions", func() {
			brain.WriteSubscription(context.Background(), br, brain.Subscription{
				Name: "my-sub", Description: "my sub", EventPattern: "file.*", Enabled: true,
			})
			output := r.Execute(context.Background(), "subscription", "")
			So(output, ShouldContainSubstring, "my-sub")
			So(output, ShouldContainSubstring, "enabled")
			So(output, ShouldContainSubstring, "file.*")
		})

		Convey("show subscription by name", func() {
			brain.WriteSubscription(context.Background(), br, brain.Subscription{
				Name: "show-sub", Description: "show sub", EventPattern: "llm.*", Enabled: true,
				Content: "an event occurred",
			})
			output := r.Execute(context.Background(), "subscription show show-sub", "")
			So(output, ShouldContainSubstring, "show-sub")
			So(output, ShouldContainSubstring, "an event occurred")
		})

		Convey("show subscription not found", func() {
			output := r.Execute(context.Background(), "subscription show nonexistent", "")
			So(output, ShouldContainSubstring, "not found")
		})
	})
}

func TestAllRegistrationCommandsNoPanic(t *testing.T) {
	Convey("All registration commands execute without panic", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		mgr.Create(context.Background())

		RegisterContext(r, func(ctx context.Context) (string, error) { return "ctx", nil })
		RegisterLimit(r, nil)
		RegisterScheduler(r, &mockSchedLister{})
		RegisterSkills(r, skill.NewFileStore(t.TempDir()))
		RegisterSessionStatus(r, mgr, memory.NewFileMemory(t.TempDir(), 10), "shared", nil)

		So(func() { r.Execute(context.Background(), "context", "") }, ShouldNotPanic)
		So(func() { r.Execute(context.Background(), "scheduler", "") }, ShouldNotPanic)
		So(func() { r.Execute(context.Background(), "skills", "") }, ShouldNotPanic)
		So(func() { r.Execute(context.Background(), "session status", "") }, ShouldNotPanic)
	})
}

type mockSchedLister struct {
	tasks []*scheduler.Task
}

func (m *mockSchedLister) List() []*scheduler.Task {
	return m.tasks
}

type mockMCPSource struct {
	defs    []types.ToolDef
	sources []tool.SourceInfo
}

func (m *mockMCPSource) List(_ context.Context) ([]types.ToolDef, error) {
	return m.defs, nil
}
func (m *mockMCPSource) ListActiveSources(_ context.Context) []tool.SourceInfo {
	return m.sources
}
func (m *mockMCPSource) DisableSource(name string) error {
	for i, s := range m.sources {
		if s.Name == name {
			m.sources[i].Enabled = false
			return nil
		}
	}
	return fmt.Errorf("not found")
}
func (m *mockMCPSource) EnableSource(name string) error {
	for i, s := range m.sources {
		if s.Name == name {
			m.sources[i].Enabled = true
			return nil
		}
	}
	return fmt.Errorf("not found")
}

type mockLister struct {
	models []llm.ModelConfig
}

func (m *mockLister) Models(_ context.Context) ([]llm.ModelConfig, error) { return m.models, nil }
func (m *mockLister) ActiveModel() string                                 { return "" }
func (m *mockLister) SetActiveModel(_ string) error                       { return nil }
func (m *mockLister) Name() string                                        { return "mock" }
func (m *mockLister) CompleteStream(_ context.Context, _ llm.LLMRequest) (<-chan llm.LLMChunk, error) {
	ch := make(chan llm.LLMChunk)
	close(ch)
	return ch, nil
}

func TestRegisterSession(t *testing.T) {
	Convey("RegisterSession", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		RegisterSession(r, mgr)

		Convey("session new creates a session", func() {
			output := r.Execute(context.Background(), "session new", "")
			So(output, ShouldContainSubstring, "created session")
		})

		Convey("session list shows sessions", func() {
			sess := mgr.Create(context.Background())
			output := r.Execute(context.Background(), "session list", "")
			So(output, ShouldContainSubstring, sess.ID[:8])
		})

		Convey("session list when none", func() {
			output := r.Execute(context.Background(), "session list", "")
			So(output, ShouldContainSubstring, "no sessions")
		})

		Convey("session switch prints message", func() {
			output := r.Execute(context.Background(), "session switch abc123", "")
			So(output, ShouldContainSubstring, "use /session new")
		})

		Convey("/new alias creates session", func() {
			output := r.Execute(context.Background(), "new", "")
			So(output, ShouldContainSubstring, "created session")
		})

		Convey("/clear alias creates session", func() {
			output := r.Execute(context.Background(), "clear", "")
			So(output, ShouldContainSubstring, "created session")
		})
	})
}

// Ensure Registry implements expected interface.
var _ = (*Registry)(nil)
