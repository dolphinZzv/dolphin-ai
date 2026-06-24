package setup

import (
	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/agentloop"
	"dolphin/internal/brain"
	"dolphin/internal/command"
	"dolphin/internal/config"
	appctx "dolphin/internal/context"
	"dolphin/internal/dream"
	"dolphin/internal/dump"
	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/limit"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/skill"
	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/userio"
	"dolphin/internal/watcher"
	"dolphin/internal/workflow"
)

// Context holds all components produced during bootstrapping.
// Bootstrappers read from and write to this struct.
type Context struct {
	Config              *config.Config
	Logger              *zap.Logger
	EventBus            *event.Bus
	HookReg             *hook.Registry
	SignalBus           *signal.Bus
	SessionMgr          *session.Manager
	Mem                 memory.Memory
	LLMProvider         llm.Provider
	ToolReg             *tool.Registry
	SkillStore          skill.Store
	CmdReg              *command.Registry
	Brain               *brain.Brain
	Scheduler           *scheduler.Scheduler
	SubscriptionEngine  *brain.SubscriptionEngine
	Watchers            []*watcher.Watcher
	AgentIO             *agentio.AgentIO
	AgentLoop           *agentloop.AgentLoop
	UserIO              *userio.UserIO
	Transports          []transport.IO
	OtelShutdown        func()
	PprofShutdown       func()
	ContextSections     []appctx.Section
	ContextReg          *appctx.Registry
	Limit               *limit.Limiter
	DumpRecorder        *dump.Recorder
	LimitResetScheduler *limit.ResetScheduler
	WorkflowEngine      *workflow.Engine
	Dream               *dream.Dream
}

func NewContext(cfg *config.Config) *Context {
	return &Context{Config: cfg}
}

// CreateDumpRecorder creates the dump recorder if not already created,
// using the configured dump_dir. Safe to call multiple times.
func (c *Context) CreateDumpRecorder() *dump.Recorder {
	if c.DumpRecorder != nil {
		return c.DumpRecorder
	}
	dir := c.Config.GetString("session.dump_dir")
	if dir == "" {
		dir = ".dolphin/dumps"
	}
	c.DumpRecorder = dump.NewRecorder(dir)
	return c.DumpRecorder
}
