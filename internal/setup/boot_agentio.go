package setup

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/agentloop"
	"dolphin/internal/command"
	"dolphin/internal/permission"
	"dolphin/internal/tool"
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

	// Session idle-expiry: when > 0, AgentIO asks the user (via their
	// transport) whether to rotate to a new session if a turn arrives
	// after this much idle time on the active session. Default: 1h.
	expireAfter := c.Config.GetDuration("session.expire_after")
	if expireAfter <= 0 {
		expireAfter = time.Hour
	}
	c.AgentIO.SetExpireAfter(expireAfter)

	maxRounds := c.Config.GetInt("agent.max_rounds")
	if maxRounds <= 0 {
		maxRounds = 100
	}

	turnTimeout := c.Config.GetDuration("agent.turn_timeout")
	idleTimeout := c.Config.GetDuration("agent.llm_idle_timeout")
	feedMinInterval := c.Config.GetDuration("agent.feed_min_interval")

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

	// Load deny defaults from config — these act as a safety net that
	// blocks dangerous operations regardless of runtime allow grants.
	if denyDefaults := loadDenyDefaults(c.Config); len(denyDefaults) > 0 {
		permStore.AddDenyDefaults(denyDefaults)
	}

	if workmode != "yolo" {
		tool.RegisterPermissionTool(c.ToolReg, permStore, c.AgentIO.GetTransport)
	}
	tool.RegisterEmitEventTool(c.ToolReg, c.EventBus)

	ctxBuilder := &agentloop.ContextBuilderStage{
		SkillStore: c.SkillStore,
		Brain:      c.Brain,
		Workspace:  c.Config.GetString("agent.workspace"),
		Workmode:   c.Config.GetString("agent.workmode"),
		EventBus:   c.EventBus,
	}
	for _, sec := range c.ContextSections {
		ctxBuilder.RegisterSection(sec)
	}
	c.ContextReg = ctxBuilder.Registry()

	initStages := []agentloop.Stage{
		&agentloop.MemoryReadStage{Memory: c.Mem},
		ctxBuilder,
	}
	if c.Config.GetBool("compaction.enabled") {
		initStages = append(initStages, &agentloop.CompactionStage{
			Provider:     c.LLMProvider,
			Memory:       c.Mem,
			Model:        c.Config.GetString("compaction.model"),
			MaxTokens:    c.Config.GetInt("compaction.summary_max_tokens"),
			MaxThreshold: c.Config.GetInt("compaction.max_tokens"),
			KeepRounds:   c.Config.GetInt("compaction.keep_rounds"),
			TokenRatio:   c.Config.GetInt("compaction.token_ratio"),
			EventBus:     c.EventBus,
			Logger:       c.Logger,
			SessionMgr:   c.SessionMgr,
		})
	}

	compositor := agentloop.NewCompositor(
		initStages,
		[]agentloop.Stage{
			&agentloop.LLMStage{
				Provider:     c.LLMProvider,
				MaxTokens:    maxInt(c.Config.GetInt("llm.max_tokens"), 4096),
				MaxRetries:   c.Config.GetInt("llm.max_retries"),
				ToolRegistry: c.ToolReg,
				EventBus:     c.EventBus,
				SignalBus:    c.SignalBus,
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
				MaxParallel:     c.Config.GetInt("agent.tool_parallelism"),
			},
			&agentloop.MemoryWriteStage{
				Memory:   c.Mem,
				EventBus: c.EventBus,
			},
		},
		maxRounds,
	)
	compositor.SetTurnTimeout(turnTimeout)
	compositor.SetIdleTimeout(idleTimeout)
	compositor.SetFeedMinInterval(feedMinInterval)
	compositor.SetCheckpoint(agentloop.NewCheckpoint(c.Mem, c.EventBus))

	c.AgentLoop = agentloop.NewAgentLoop(c.AgentIO.Queue(), compositor, c.Logger, c.EventBus, c.AgentIO, c.Config.GetInt("agent.pool_size"))
	c.AgentLoop.SetSessionGcInterval(c.Config.GetDuration("agent.session_gc_interval"))

	recorder := c.CreateDumpRecorder()
	c.AgentLoop.SetDumpRecorder(recorder)
	command.RegisterDump(c.CmdReg, recorder, c.SessionMgr)

	c.CmdReg.SetAgentIO(c.AgentIO)

	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// loadDenyDefaults reads permission.deny rules from config and returns them
// in the format expected by permission.Store.AddDenyDefaults.
func loadDenyDefaults(cfg interface {
	Keys() []string
	GetString(string) string
}) map[string][]map[string]string {
	result := make(map[string][]map[string]string)
	prefix := "permission.deny."
	for _, key := range cfg.Keys() {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		// Expected format: <tool>.<index>.<arg>
		parts := strings.SplitN(rest, ".", 3)
		if len(parts) < 3 {
			continue
		}
		tool := parts[0]
		// parts[1] is the index, parts[2] is the arg name
		v := cfg.GetString(key)
		if v == "" {
			continue
		}
		result[tool] = append(result[tool], map[string]string{parts[2]: v})
	}
	return result
}
