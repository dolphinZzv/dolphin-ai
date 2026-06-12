package setup

import (
	"dolphin/internal/agentio"
	"dolphin/internal/agentloop"
	"dolphin/internal/brain"
	"dolphin/internal/command"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/watcher"
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

	"go.uber.org/zap"
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
	Limit               *limit.Limiter
	LimitResetScheduler *limit.ResetScheduler
}

func NewContext(cfg *config.Config) *Context {
	return &Context{Config: cfg}
}
