package lifecycle

import (
	"context"
	"fmt"
	"os"

	"dolphin/internal/agentio"
	"dolphin/internal/agentloop"
	"dolphin/internal/brain"
	"dolphin/internal/command"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/llm"
	"dolphin/internal/logger"
	"dolphin/internal/mcp"
	"dolphin/internal/memory"
	"dolphin/internal/observability"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/skill"
	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/userio"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// Builder constructs a Pipeline with explicit, named steps.
type Builder struct {
	cfg      *config.Config
	pipeline *Pipeline

	// Intermediate products.
	logger       *zap.Logger
	eventBus     *event.Bus
	hookReg      *hook.Registry
	signalBus    *signal.Bus
	sessionMgr   *session.Manager
	mem          memory.Memory
	llmProvider  llm.Provider
	toolReg      *tool.Registry
	skillStore   skill.Store
	cmdReg       *command.Registry
	brain        *brain.Brain
	agentIO      *agentio.AgentIO
	agentLoop    *agentloop.AgentLoop
	userIO       *userio.UserIO
	transports   []transport.IO
	otelShutdown func()
}

func NewBuilder(cfg *config.Config) *Builder {
	return &Builder{cfg: cfg}
}

func (b *Builder) Build() *Pipeline {
	if b.pipeline == nil {
		panic("pipeline builder: Build() called without running steps")
	}
	return b.pipeline
}

// StepLogger creates the zap logger.
func (b *Builder) StepLogger() *Builder {
	if b.logger != nil {
		return b
	}
	b.logger = logger.New(b.cfg)
	return b
}

// StepBuses creates event bus, hook registry, signal bus.
func (b *Builder) StepBuses() *Builder {
	if b.eventBus != nil {
		return b
	}
	b.eventBus = event.NewBus()
	b.hookReg = hook.NewRegistry()
	b.signalBus = signal.NewBus()
	return b
}

// StepSession creates the session manager and loads the last active session.
func (b *Builder) StepSession() *Builder {
	if b.sessionMgr != nil {
		return b
	}
	store := session.NewFileStore(b.cfg.GetString("memory.dir") + "/sessions")
	mgr := session.NewManager(store)
	mgr.LoadActive(context.Background())
	b.sessionMgr = mgr
	return b
}

// StepMemory creates the memory layer with window.
func (b *Builder) StepMemory() *Builder {
	if b.mem != nil {
		return b
	}
	dir := b.cfg.GetString("memory.dir")
	window := b.cfg.GetInt("memory.window")
	b.mem = memory.NewDroppingMemory(
		memory.NewFileMemory(dir, window),
		window,
	)
	return b
}

// StepLLM creates the LLM provider from config.
func (b *Builder) StepLLM() *Builder {
	if b.llmProvider != nil {
		return b
	}
	providerName := b.cfg.GetString("llm.provider")
	b.llmProvider = llm.NewProvider(llm.Config{
		Provider:    providerName,
		Model:       b.cfg.GetString("llm.model"),
		APIKey:      b.cfg.GetString("llm." + providerName + ".api_key"),
		BaseURL:     b.cfg.GetString("llm." + providerName + ".base_url"),
		Temperature: b.cfg.GetFloat("llm.temperature"),
		MaxTokens:   b.cfg.GetInt("llm.max_tokens"),
		MaxRetries:  b.cfg.GetInt("llm.max_retries"),
		Timeout:     b.cfg.GetDuration("llm.timeout"),
	}, b.logger)
	return b
}

// StepTools creates the tool registry, MCP servers, skills, and commands.
func (b *Builder) StepTools() *Builder {
	if b.toolReg != nil {
		return b
	}

	b.toolReg = tool.NewRegistry()
	loadMCPServers(b.cfg, b.toolReg, b.logger)

	catalogEntries, _ := loadCatalogFromConfig(b.cfg)
	catalog := tool.NewCatalog(catalogEntries)
	for name, mt := range tool.MetaHandler(catalog, b.toolReg) {
		b.toolReg.RegisterBuiltin(name, "MCP server discovery", mt.Schema, mt.Handler)
	}

	b.skillStore = skill.NewFileStore(b.cfg.GetString("memory.dir") + "/skills")
	b.cmdReg = command.NewRegistry(b.sessionMgr, b.signalBus)
	tool.RegisterSkillTools(b.toolReg, tool.SkillAdapter{Store: b.skillStore}, b.cmdReg)
	tool.RegisterCommandTools(b.toolReg, b.cmdReg)
	tool.RegisterSessionTools(b.toolReg, b.sessionMgr)
	b.registerMCPCommand()
	b.registerSkillsCommand()
	b.registerContextCommand()

	return b
}

// StepBrain creates the brain (long-term knowledge directory with git).
func (b *Builder) StepBrain() *Builder {
	if b.brain != nil {
		return b
	}
	brainDir := b.cfg.GetString("brain.dir")
	br := brain.New(brainDir)
	if br.IsInitialized() {
		b.logger.Info("brain already initialized", zap.String("dir", brainDir))
	} else {
		b.logger.Info("brain not initialized, creating", zap.String("dir", brainDir))
	}
	if err := br.Init(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "brain: init failed: %v\n", err)
		b.logger.Fatal("brain init failed", zap.String("dir", brainDir), zap.Error(err))
	}
	fmt.Fprintf(os.Stdout, "brain: %s (git repo)\n", brainDir)
	tool.RegisterBrainTools(b.toolReg, br)
	b.brain = br
	return b
}

// StepAgentIO creates the agent IO and agent loop with compositor.
func (b *Builder) StepAgentIO() *Builder {
	if b.agentIO != nil {
		return b
	}

	bufferSize := b.cfg.GetInt("agent.buffer_size")
	if bufferSize <= 0 {
		bufferSize = 1024
	}
	b.agentIO = agentio.NewAgentIO(bufferSize, b.sessionMgr, b.signalBus, b.logger, b.cfg.GetString("agent.name"))
	b.cmdReg.SetAgentIO(b.agentIO)

	maxRounds := b.cfg.GetInt("agent.max_rounds")
	if maxRounds <= 0 {
		maxRounds = 100
	}

	turnTimeout := b.cfg.GetDuration("agent.turn_timeout")
	compositor := agentloop.NewCompositor(
		[]agentloop.Stage{
			&agentloop.MemoryReadStage{Memory: b.mem},
			&agentloop.ContextBuilderStage{
				SkillStore: b.skillStore,
				Brain:      b.brain,
				Workspace:  b.cfg.GetString("agent.workspace"),
				EventBus:   b.eventBus,
			},
		},
		[]agentloop.Stage{
			&agentloop.LLMStage{
				Provider:     b.llmProvider,
				Model:        b.cfg.GetString("llm.model"),
				MaxTokens:    b.cfg.GetInt("llm.max_tokens"),
				MaxRetries:   b.cfg.GetInt("llm.max_retries"),
				ToolRegistry: b.toolReg,
				EventBus:     b.eventBus,
				Logger:       b.logger,
			},
			&agentloop.ToolStage{
				ToolRegistry: b.toolReg,
				SignalBus:    b.signalBus,
				Timeout:      b.cfg.GetDuration("tool.timeout"),
				Logger:       b.logger,
				EventBus:     b.eventBus,
			},
			&agentloop.MemoryWriteStage{Memory: b.mem, EventBus: b.eventBus},
		},
		maxRounds,
	)
	compositor.SetTurnTimeout(turnTimeout)

	b.agentLoop = agentloop.NewAgentLoop(b.agentIO.Queue(), compositor, b.logger, b.eventBus)
	b.agentLoop.SetOnResult(func(tr agentio.TurnResult) {
		b.agentIO.OnResult(&tr)
	})

	return b
}

// StepUserIO creates the user IO.
func (b *Builder) StepUserIO() *Builder {
	if b.userIO != nil {
		return b
	}
	b.userIO = userio.NewUserIO(b.agentIO, b.cmdReg, b.sessionMgr)
	return b
}

// StepObservability creates OTel and metrics.
func (b *Builder) StepObservability() *Builder {
	if b.otelShutdown != nil {
		return b
	}
	b.otelShutdown = observability.BuildObservability(b.cfg, b.hookReg, b.logger)

	b.eventBus.Subscribe(event.NewLogHandler(b.logger))
	b.eventBus.Subscribe(func(ctx context.Context, e event.Event) {
		b.hookReg.Dispatch(ctx, e)
	})

	return b
}

// StepTransports creates and registers all transports.
func (b *Builder) StepTransports() *Builder {
	if b.transports != nil {
		return b
	}
	transportCfgs, _ := loadTransportConfigs(b.cfg, b.cfg.GetString("agent.name"))
	for _, tc := range transportCfgs {
		tc.Config["logger"] = b.logger
		tio, err := transport.Build(context.Background(), tc.Type, tc.Config)
		if err != nil {
			b.logger.Fatal("transport build failed", zap.String("type", tc.Type), zap.Error(err))
		}
		b.agentIO.RegisterTransport(tio.ID(), tio)
		b.transports = append(b.transports, tio)

		// Register transport-specific MCP tools.
		for _, td := range tio.Tools() {
			switch {
			case td.URL != "":
				client := mcp.NewClient(td.URL)
				b.toolReg.AddSource(client)
				b.logger.Info("registered transport MCP source",
					zap.String("transport", tio.ID()),
					zap.String("url", td.URL),
				)
			case td.Command != "":
				client, err := mcp.NewStdioClient(context.Background(), td.Command, td.Args)
				if err != nil {
					b.logger.Warn("transport MCP stdio client failed",
						zap.String("transport", tio.ID()),
						zap.String("command", td.Command),
						zap.Error(err),
					)
					continue
				}
				b.toolReg.AddSource(client)
				b.logger.Info("registered transport MCP source",
					zap.String("transport", tio.ID()),
					zap.String("command", td.Command),
				)
			}
		}
	}
	return b
}

// Assemble builds the final Pipeline from all constructed components.
func (b *Builder) Assemble() *Builder {
	if b.pipeline != nil {
		return b
	}
	b.pipeline = &Pipeline{
		transports:         b.transports,
		userIO:             b.userIO,
		agentIO:            b.agentIO,
		agentLoop:          b.agentLoop,
		sessionMgr:         b.sessionMgr,
		signalBus:          b.signalBus,
		eventBus:           b.eventBus,
		logger:             b.logger,
		otelShutdown:       b.otelShutdown,
		dingtalkWebhookURL: b.cfg.GetString("dingtalk.webhook_url"),
	}
	return b
}

// ---------------------------------------------------------------------------
// Builder internal helpers (mirrors pipeline.go commands).
// ---------------------------------------------------------------------------

func (b *Builder) registerMCPCommand() {
	b.cmdReg.Register(&cobra.Command{
		Use:   "mcp",
		Short: "List loaded MCP tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			defs, err := b.toolReg.List(context.Background())
			if err != nil {
				return err
			}
			if len(defs) == 0 {
				cmd.Println("No MCP tools loaded")
				return nil
			}
			cmd.Println("Loaded tools:")
			for _, t := range defs {
				cmd.Printf("  %s — %s\n", t.Name, t.Description)
			}
			return nil
		},
	})
}

func (b *Builder) registerSkillsCommand() {
	b.cmdReg.Register(&cobra.Command{
		Use:   "skills",
		Short: "List available skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			skills, err := b.skillStore.List(context.Background())
			if err != nil {
				return err
			}
			if len(skills) == 0 {
				cmd.Println("No skills available")
				return nil
			}
			cmd.Println("Available skills:")
			for _, sk := range skills {
				enabled := "disabled"
				if sk.Enabled {
					enabled = "enabled"
				}
				cmd.Printf("  %s (%s)\n", sk.Name, enabled)
			}
			return nil
		},
	})
}

func (b *Builder) registerContextCommand() {
	contextCmd := &cobra.Command{
		Use:   "context",
		Short: "Show full system context (brain index, skills, etc.)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cbs := &agentloop.ContextBuilderStage{
				SkillStore: b.skillStore,
				Brain:      b.brain,
				Workspace:  b.cfg.GetString("agent.workspace"),
			}
			prompt, err := cbs.BuildSystemPrompt(context.Background())
			if err != nil {
				return err
			}
			cmd.Println(prompt)
			return nil
		},
	}
	b.cmdReg.Register(contextCmd)
}
