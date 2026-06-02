package lifecycle

import (
	"context"

	"dolphin/internal/agentio"
	"dolphin/internal/command"
	"dolphin/internal/config"
	"dolphin/internal/setup"
	_ "dolphin/internal/transport/dingtalk"
	_ "dolphin/internal/transport/wework"
)

// Builder constructs a Pipeline with explicit, named steps.
// Each step delegates to the corresponding setup.Bootstrapper.
type Builder struct {
	cfg      *config.Config
	ctx      *setup.Context
	pipeline *Pipeline

	// cmdReg is set during StepTools for test compatibility.
	cmdReg *command.Registry
}

func NewBuilder(cfg *config.Config) *Builder {
	return &Builder{
		cfg: cfg,
		ctx: setup.NewContext(cfg),
	}
}

func (b *Builder) StepLogger() *Builder {
	if b.ctx.Logger != nil {
		return b
	}
	(&setup.LoggerBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepBuses() *Builder {
	if b.ctx.EventBus != nil {
		return b
	}
	(&setup.BusesBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepSession() *Builder {
	if b.ctx.SessionMgr != nil {
		return b
	}
	(&setup.SessionBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepMemory() *Builder {
	if b.ctx.Mem != nil {
		return b
	}
	(&setup.MemoryBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepLLM() *Builder {
	if b.ctx.LLMProvider != nil {
		return b
	}
	(&setup.LLMBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepTools() *Builder {
	if b.ctx.ToolReg != nil {
		return b
	}
	(&setup.ToolsBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	b.cmdReg = b.ctx.CmdReg
	return b
}

func (b *Builder) StepBrain() *Builder {
	if b.ctx.Brain != nil {
		return b
	}
	(&setup.BrainBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepScheduler() *Builder {
	if b.ctx.Scheduler != nil {
		return b
	}
	(&setup.SchedulerBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepAgentIO() *Builder {
	if b.ctx.AgentIO != nil {
		return b
	}
	(&setup.AgentIOBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepUserIO() *Builder {
	if b.ctx.UserIO != nil {
		return b
	}
	(&setup.UserIOBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepObservability() *Builder {
	if b.ctx.OtelShutdown != nil {
		return b
	}
	(&setup.ObservabilityBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

func (b *Builder) StepTransports() *Builder {
	if b.ctx.Transports != nil {
		return b
	}
	(&setup.TransportsBootstrapper{}).Bootstrap(context.Background(), b.ctx)
	return b
}

// Assemble builds the final Pipeline from all constructed components.
func (b *Builder) Assemble() *Builder {
	if b.pipeline != nil {
		return b
	}

	// Wire subscription engine to send triggered content to the agent loop.
	if b.ctx.SubscriptionEngine != nil && b.ctx.AgentIO != nil {
		agentIO := b.ctx.AgentIO
		b.ctx.SubscriptionEngine.SendTurn = func(ctx context.Context, input string) {
			agentIO.SendTurn(ctx, &agentio.Turn{Input: input})
		}
	}
	b.pipeline = &Pipeline{
		transports:         b.ctx.Transports,
		userIO:             b.ctx.UserIO,
		agentIO:            b.ctx.AgentIO,
		agentLoop:          b.ctx.AgentLoop,
		sessionMgr:         b.ctx.SessionMgr,
		brain:              b.ctx.Brain,
		scheduler:          b.ctx.Scheduler,
		signalBus:          b.ctx.SignalBus,
		eventBus:           b.ctx.EventBus,
		logger:             b.ctx.Logger,
		otelShutdown:       b.ctx.OtelShutdown,
		watchers:           b.ctx.Watchers,
		subscriptionEngine: b.ctx.SubscriptionEngine,
	}
	return b
}

// Build returns the assembled Pipeline. Panics if Assemble wasn't called.
func (b *Builder) Build() *Pipeline {
	if b.pipeline == nil {
		panic("pipeline builder: Build() called without running Assemble()")
	}
	return b.pipeline
}
