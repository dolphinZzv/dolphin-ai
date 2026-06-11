package command

import (
	"context"
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
	"dolphin/internal/types"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/zap"
)

func TestRenderMarkdown_Lang(t *testing.T) {
	Convey("lang list in markdown mode", t, func() {
		r := NewRegistry(nil, nil)
		RegisterLang(r)
		output := r.Execute(context.Background(), "lang", "markdown")
		So(output, ShouldContainSubstring, "| Code | Name |")
		So(output, ShouldContainSubstring, "| en | English")
		So(output, ShouldContainSubstring, "| zh | 中文")
	})
}

func TestRenderMarkdown_SessionList(t *testing.T) {
	Convey("session list in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		RegisterSession(r, mgr)
		sess := mgr.Create(context.Background())

		output := r.Execute(context.Background(), "session list", "markdown")
		So(output, ShouldContainSubstring, "| ID | Status |")
		So(output, ShouldContainSubstring, sess.ID[:8])
	})
}

func TestRenderMarkdown_SessionStatus(t *testing.T) {
	Convey("session status in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		sess := mgr.Create(context.Background())
		sess.Set("rounds", 5)
		sess.Set("tool_calls", 12)
		sess.Set("system_context", 42)

		mem := memory.NewFileMemory(t.TempDir(), 100)
		err := mem.Write(context.Background(), sess.ID, types.Message{Role: types.RoleUser, Content: "hello"})
		So(err, ShouldBeNil)

		RegisterSessionStatus(r, mgr, mem, "per_transport", nil)

		output := r.Execute(context.Background(), "session status", "markdown")
		So(output, ShouldContainSubstring, "**Session Status**")
		So(output, ShouldContainSubstring, "| Key | Value |")
		So(output, ShouldContainSubstring, "per_transport")
		So(output, ShouldContainSubstring, "5")
	})
}

func TestRenderMarkdown_Skills(t *testing.T) {
	Convey("skills list in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		skStore := skill.NewFileStore(t.TempDir())
		err := skStore.Save(context.Background(), skill.Skill{Name: "helper", Description: "helper tool", Enabled: true})
		So(err, ShouldBeNil)
		RegisterSkills(r, skStore)

		output := r.Execute(context.Background(), "skills list", "markdown")
		So(output, ShouldContainSubstring, "**Available skills:**")
		So(output, ShouldContainSubstring, "| Name | Status |")
		So(output, ShouldContainSubstring, "✅ enabled")
	})
}

func TestRenderMarkdown_Scheduler(t *testing.T) {
	Convey("scheduler in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		now := time.Now()
		tasks := []*scheduler.Task{
			{Name: "daily", ID: "1", Schedule: "0 9 * * *", Enabled: true, RunCount: 5, LastRunAt: &now},
			{Name: "delayed", ID: "2", Delay: "5m", Enabled: true, RunCount: 1, LastStatus: "success", LastRunAt: &now},
			{Name: "cron-off", ID: "3", Schedule: "0 0 * * *", Enabled: false, LastStatus: "disabled"},
		}
		RegisterScheduler(r, &mockSchedLister{tasks: tasks})

		output := r.Execute(context.Background(), "scheduler", "markdown")
		So(output, ShouldContainSubstring, "**Scheduled tasks:**")
		So(output, ShouldContainSubstring, "| Name | ID | Status | Type | Schedule | Command | Last Run | Runs |")
		So(output, ShouldContainSubstring, "daily")
		So(output, ShouldContainSubstring, "delayed")
		So(output, ShouldContainSubstring, "disabled")
		So(output, ShouldContainSubstring, "success")
	})
}

func TestRenderMarkdown_MCP(t *testing.T) {
	Convey("mcp in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		mgrMock := &mockMCPSource{
			defs: []types.ToolDef{
				{Name: "brain_read", Description: "read brain"},
				{Name: "shell", Description: "run command"},
				{Name: "custom_tool", Description: "custom tool"},
			},
			sources: []tool.SourceInfo{
				{Name: "local", Enabled: true},
				{Name: "remote", Enabled: false},
			},
		}
		RegisterMCP(r, mgrMock)

		output := r.Execute(context.Background(), "mcp", "markdown")
		So(output, ShouldContainSubstring, "Knowledge")
		So(output, ShouldContainSubstring, "brain_read")
		So(output, ShouldContainSubstring, "local")
		So(output, ShouldContainSubstring, "remote")
	})
}

func TestRenderMarkdown_Limit(t *testing.T) {
	Convey("limit in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		cfg := config.LoadConfigFromMap(map[string]any{
			"llm.limit.max_requests.hard":      "100",
			"llm.limit.max_input_tokens":       "50000",
			"llm.limit.max_output_tokens.hard": "100000",
			"llm.limit.max_total_tokens":       "150000",
		})
		store := limit.NewMemoryStore()
		bus := event.NewBus()
		logger, _ := zap.NewDevelopment()
		limiter := limit.NewLimiter(store, cfg, bus, logger)
		limiter.RecordLLM("deepseek-v4-flash", 1000, 500)
		RegisterLimit(r, limiter)

		output := r.Execute(context.Background(), "limit", "markdown")
		So(output, ShouldContainSubstring, "### Global Limits")
		So(output, ShouldContainSubstring, "| Limit | Hard | Soft | Current |")
		So(output, ShouldContainSubstring, "requests")
	})
}

func TestRenderMarkdown_Models(t *testing.T) {
	Convey("models list in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		mockProv := &mockLister{
			models: []llm.ModelConfig{
				{Name: "gpt-4", Provider: "openai", Vendor: "OpenAI", APIType: "openai", Model: "gpt-4"},
				{Name: "claude-3", Provider: "anthropic", Vendor: "Anthropic", APIType: "anthropic", Model: "claude-3"},
			},
		}
		RegisterModels(r, mockProv)

		output := r.Execute(context.Background(), "models", "markdown")
		So(output, ShouldContainSubstring, "**Available models:**")
		So(output, ShouldContainSubstring, "| Name | Vendor | API Type | Model |")
		So(output, ShouldContainSubstring, "gpt-4")
		So(output, ShouldContainSubstring, "claude-3")
	})

	Convey("models use subcommand in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)

		mockProv := &mockLister{
			models: []llm.ModelConfig{
				{Name: "model-a", Vendor: "test", APIType: "openai", Model: "gpt-4"},
			},
		}
		RegisterModels(r, mockProv)

		output := r.Execute(context.Background(), "models use model-a", "markdown")
		So(output, ShouldContainSubstring, "switched to model-a")
	})
}

func TestRenderMarkdown_Scripts(t *testing.T) {
	Convey("scripts in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		br := brain.New(t.TempDir())
		err := br.Init(context.Background())
		So(err, ShouldBeNil)
		RegisterScripts(r, br)

		err = brain.WriteScript(context.Background(), br, brain.Script{
			Name: "greet", Description: "say hello", Enabled: true, Content: "echo hello",
		})
		So(err, ShouldBeNil)

		output := r.Execute(context.Background(), "script", "markdown")
		So(output, ShouldContainSubstring, "| Name | Description | Status |")
		So(output, ShouldContainSubstring, "greet")
		So(output, ShouldContainSubstring, "say hello")
	})
}

func TestRenderMarkdown_Subscriptions(t *testing.T) {
	Convey("subscriptions in markdown mode", t, func() {
		mgr := session.NewManager(t.TempDir())
		sb := signal.NewBus()
		r := NewRegistry(mgr, sb)
		br := brain.New(t.TempDir())
		err := br.Init(context.Background())
		So(err, ShouldBeNil)
		RegisterSubscriptionCmd(r, br)

		err = brain.WriteSubscription(context.Background(), br, brain.Subscription{
			Name: "my-sub", Description: "my subscription", EventPattern: "file.*", Enabled: true,
		})
		So(err, ShouldBeNil)

		output := r.Execute(context.Background(), "subscription", "markdown")
		So(output, ShouldContainSubstring, "| Name | Description | Event | Status |")
		So(output, ShouldContainSubstring, "my-sub")
		So(output, ShouldContainSubstring, "file.*")
	})
}
