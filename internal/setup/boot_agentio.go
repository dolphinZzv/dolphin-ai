package setup

import (
	"context"

	"dolphin/internal/agentio"
	"dolphin/internal/agentloop"
	"dolphin/internal/permission"
	"dolphin/internal/tool"

	"go.uber.org/zap"
)

type AgentIOBootstrapper struct{}

func (b *AgentIOBootstrapper) Name() string { return "agentio" }
func (b *AgentIOBootstrapper) Index() int   { return 90 }
func (b *AgentIOBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.AgentIO != nil {
		return nil
	}

	bufferSize := c.Config.GetInt("agent.buffer_size")
	if bufferSize <= 0 {
		bufferSize = 1024
	}
	c.AgentIO = agentio.NewAgentIO(bufferSize, c.SessionMgr, c.SignalBus, c.Logger, c.Config.GetString("agent.name"))

	maxRounds := c.Config.GetInt("agent.max_rounds")
	if maxRounds <= 0 {
		maxRounds = 100
	}

	turnTimeout := c.Config.GetDuration("agent.turn_timeout")

	permFile := c.Config.GetString("permission.file")
	workmode := c.Config.GetString("agent.workmode")
	permStore, err := permission.Load(permFile)
	if err != nil {
		c.Logger.Warn("permissions file is malformed, using empty rules",
			zap.String("file", permFile),
			zap.Error(err),
		)
		permStore = permission.NewStore(permFile)
	}

	if workmode != "yolo" {
		tool.RegisterPermissionTool(c.ToolReg, permStore, c.AgentIO.GetTransport)
	}
	tool.RegisterEmitEventTool(c.ToolReg, c.EventBus)

	compositor := agentloop.NewCompositor(
		[]agentloop.Stage{
			&agentloop.MemoryReadStage{Memory: c.Mem},
			&agentloop.ContextBuilderStage{
				SkillStore: c.SkillStore,
				Brain:      c.Brain,
				Workspace:  c.Config.GetString("agent.workspace"),
				Workmode:   c.Config.GetString("agent.workmode"),
				EventBus:   c.EventBus,
			},
		},
		[]agentloop.Stage{
			&agentloop.LLMStage{
				Provider:     c.LLMProvider,
				MaxTokens:    c.Config.GetInt("llm.max_tokens"),
				MaxRetries:   c.Config.GetInt("llm.max_retries"),
				ToolRegistry: c.ToolReg,
				EventBus:     c.EventBus,
				Logger:       c.Logger,
				HookReg:      c.HookReg,
			},
			&agentloop.ToolStage{
				ToolRegistry:    c.ToolReg,
				SignalBus:       c.SignalBus,
				Timeout:         c.Config.GetDuration("tool.timeout"),
				Logger:          c.Logger,
				EventBus:        c.EventBus,
				PermissionStore: permStore,
				GetTransport:    c.AgentIO.GetTransport,
				Workmode:        workmode,
			},
			&agentloop.MemoryWriteStage{Memory: c.Mem, EventBus: c.EventBus},
		},
		maxRounds,
	)
	compositor.SetTurnTimeout(turnTimeout)

	c.AgentLoop = agentloop.NewAgentLoop(c.AgentIO.Queue(), compositor, c.Logger, c.EventBus)

	return nil
}
