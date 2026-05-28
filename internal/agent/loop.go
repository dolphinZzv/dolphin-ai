package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"dolphin/internal/agent/limits"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/i18n"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
	"dolphin/internal/transport"

	"dolphin/internal/agent/compressor"
	"dolphin/internal/agent/provider"
	ctxpkg "dolphin/internal/context"
	"dolphin/internal/subsystem"

	"go.uber.org/zap"
)

// Agent is the core agent that runs the agent loop.
type Agent struct {
	cfg               *config.Config
	sessMgr           *session.Manager
	toolReg           *mcp.Registry
	provider          provider.Provider
	ctxBuilder        *ctxpkg.Builder
	compressor        compressor.Compressor
	hooks             *hook.Registry
	events            *event.EventBus
	heartbeatInterval int
	version           string
	buildTime         string
	commitHash        string
	limitsManager     *limits.LimitsManager
	reloadFunc        func() error
}

// LoopState holds state for a single agent run.
type LoopState struct {
	Sess              *session.Session
	Messages          []provider.Message
	Turn              int
	StopReason        string
	ToolCallCount     int
	ErrorCount        int
	CompressionCount  int
	SummaryGenerated  bool
	SummaryTexts      []string
	TotalInputTokens  int // cumulative input tokens
	TotalOutputTokens int // cumulative output tokens
	TotalCachedTokens int // cumulative cached input tokens
	TotalMissedTokens int // cumulative missed input tokens
}

func New(cfg *config.Config, sessMgr *session.Manager, toolReg *mcp.Registry) *Agent {
	provider := selectProvider(cfg)

	a := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   provider,
		ctxBuilder: ctxpkg.NewBuilder(),
	}
	if cfg.LLM.Limits.Enabled {
		a.limitsManager = limits.NewLimitsManager(&cfg.LLM.Limits)
	}

	a.ctxBuilder.RegisterSectionProvider(ctxpkg.NewSectionProviderFunc("workspace",
		func(agentName string) string {
			if agentName != "" {
				return ""
			}
			return "## Workspace\n" + a.cfg.Workspace
		},
	), ctxpkg.PriorityWorkspace, "WORKSPACE")
	a.ctxBuilder.RegisterSectionProvider(ctxpkg.NewSectionProviderFunc("subsystems",
		func(agentName string) string { return subsystem.ContextMD() },
	), ctxpkg.PrioritySubSystems, "SUBSYSTEMS.md")
	a.ctxBuilder.SetToolLister(func() []ctxpkg.ToolInfo {
		defs := toolReg.List()
		info := make([]ctxpkg.ToolInfo, len(defs))
		for i, d := range defs {
			info[i] = ctxpkg.ToolInfo{
				Name:        d.Name,
				Description: d.Description,
				Priority:    d.Priority,
			}
		}
		return info
	})

	a.rebuildCompressor()
	return a
}

func (a *Agent) SetVersion(v string)           { a.version = v }
func (a *Agent) SetBuildTime(t string)         { a.buildTime = t }
func (a *Agent) SetCommitHash(h string)        { a.commitHash = h }
func (a *Agent) SetReloadFunc(fn func() error) { a.reloadFunc = fn }

func (a *Agent) rebuildCompressor() {
	timeout := time.Duration(a.cfg.LLM.CompressTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	switch a.cfg.LLM.CompressMode {
	case "segment":
		a.compressor = compressor.NewSegmentCompressor(a.cfg.LLM.SegmentMergeLimit)
	case "tiered":
		a.compressor = compressor.NewTieredCompressor(a.provider, timeout)
	case "incremental":
		a.compressor = compressor.NewIncrementalCompressor(a.provider, timeout)
	case "topic":
		a.compressor = compressor.NewTopicCompressor(a.provider, timeout)
	default:
		a.compressor = &compressor.DropCompressor{}
	}
}

// OnConfigChange handles config hot-reload for the Agent.
// It re-points the config pointer and rebuilds provider/compressor/limits if
// their respective sections changed.
func (a *Agent) OnConfigChange(oldCfg, newCfg *config.Config) {
	a.cfg = newCfg

	if !reflect.DeepEqual(oldCfg.LLM.Providers, newCfg.LLM.Providers) ||
		oldCfg.LLM.Type != newCfg.LLM.Type ||
		oldCfg.LLM.BaseURL != newCfg.LLM.BaseURL {
		a.provider = selectProvider(newCfg)
	}

	if oldCfg.LLM.CompressMode != newCfg.LLM.CompressMode ||
		oldCfg.LLM.CompressTimeoutSeconds != newCfg.LLM.CompressTimeoutSeconds {
		a.rebuildCompressor()
	}

	if !reflect.DeepEqual(oldCfg.LLM.Limits, newCfg.LLM.Limits) {
		if newCfg.LLM.Limits.Enabled {
			a.limitsManager = limits.NewLimitsManager(&newCfg.LLM.Limits)
		} else {
			a.limitsManager = nil
		}
	}
}

// selectProvider creates a FailoverProvider with all configured providers,
// runs concurrent health checks, and selects the first healthy one.
// Each provider is wrapped in a RetryProvider for automatic retry.
func selectProvider(cfg *config.Config) provider.Provider {
	providerCfgs := cfg.LLM.EffectiveProviders()
	if len(providerCfgs) == 0 {
		zap.S().Fatal("no LLM providers configured")
	}

	maxAttempts := cfg.LLM.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	backoffBase := llmBackoffBase(cfg.LLM.Retry.BackoffBase)
	hcTimeout := time.Duration(cfg.LLM.HealthCheckTimeoutSeconds) * time.Second
	if hcTimeout <= 0 {
		hcTimeout = 10 * time.Second
	}

	failover := provider.NewFailoverProvider(providerCfgs, maxAttempts, backoffBase, hcTimeout)

	type jobResult struct {
		idx int
		pc  config.ProviderConfig
		ok  bool
		ms  int64
		err string
	}

	n := len(providerCfgs)
	ch := make(chan *jobResult, n)
	var wg sync.WaitGroup

	for i, pc := range providerCfgs {
		wg.Add(1)
		go func(idx int, pcfg config.ProviderConfig) {
			defer wg.Done()

			// Skip health check for placeholder/test API keys.
			if provider.IsPlaceholderKey(pcfg.APIKey) {
				ch <- &jobResult{idx, pcfg, true, 0, ""}
				return
			}

			p := provider.NewProviderFromConfig(&pcfg)
			start := time.Now()
			healthTO := time.Duration(cfg.LLM.HealthCheckTimeoutSeconds) * time.Second
			if healthTO <= 0 {
				healthTO = 10 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), healthTO)
			err := p.HealthCheck(ctx)
			cancel()

			// Retry once on network jitter.
			if err != nil {
				start = time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), healthTO)
				err = p.HealthCheck(ctx)
				cancel()
			}

			ms := time.Since(start).Milliseconds()
			if err != nil {
				ch <- &jobResult{idx, pcfg, false, ms, err.Error()}
			} else {
				ch <- &jobResult{idx, pcfg, true, ms, ""}
			}
		}(i, pc)
	}

	wg.Wait()
	close(ch)

	var results []*jobResult
	for r := range ch {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].idx < results[j].idx })

	// Build banner and select first healthy.
	{
		var buf strings.Builder
		buf.WriteString(i18n.TL(i18n.KeyLLMProvidersHeader))
		selected := false
		for _, r := range results {
			if r.ok {
				fmt.Fprintf(&buf, i18n.TL(i18n.KeyLLMProviderOK), r.pc.Name, r.pc.Model, r.ms)
			} else {
				fmt.Fprintf(&buf, i18n.TL(i18n.KeyLLMProviderFail), r.pc.Name, r.pc.Model, r.ms, r.err)
			}
		}
		for _, r := range results {
			if r.ok {
				fmt.Fprintf(&buf, i18n.TL(i18n.KeyLLMUsing), r.pc.Name)
				fmt.Fprint(os.Stderr, buf.String())

				failover.SelectProvider(r.idx)

				// Copy back to legacy fields for downstream compat.
				cfg.LLM.Type = r.pc.Type
				cfg.LLM.BaseURL = r.pc.BaseURL
				cfg.LLM.APIKey = r.pc.APIKey
				cfg.LLM.Model = r.pc.Model
				cfg.LLM.MaxTokens = r.pc.MaxTokens

				zap.S().Infow("selected LLM provider",
					"name", r.pc.Name,
					"type", r.pc.Type,
					"model", r.pc.Model,
					"base_url", r.pc.BaseURL,
					"ms", r.ms,
				)
				selected = true
				break
			}
		}
		if !selected {
			buf.WriteString(i18n.TL(i18n.KeyNoAvailableProvider))
			fmt.Fprint(os.Stderr, buf.String())
		}
	}

	// All failed — print help.
	if len(results) > 0 && !results[0].ok {
		printProviderHelp(providerCfgs)
	}

	return failover
}

func printProviderHelp(providers []config.ProviderConfig) {
	var buf strings.Builder
	buf.WriteString("\n")
	buf.WriteString("╔════════════════════════════════════════════════════════════╗\n")
	buf.WriteString("║  All LLM providers are unavailable.                     ║\n")
	buf.WriteString("║  Check your API keys and network connection.            ║\n")
	buf.WriteString("╚════════════════════════════════════════════════════════════╝\n")
	buf.WriteString("\nConfigured providers:\n")
	for _, pc := range providers {
		link := providerLink(pc.Name)
		fmt.Fprintf(&buf, "  - %s (%s)", pc.Name, pc.BaseURL)
		if link != "" {
			fmt.Fprintf(&buf, "\n    Get API key: %s", link)
		}
		buf.WriteString("\n")
	}
	fmt.Fprint(os.Stderr, buf.String())
}

func providerLink(name string) string {
	links := map[string]string{
		"deepseek": "https://platform.deepseek.com/api_keys",
		"minimax":  "https://platform.minimaxi.com/",
		"glm":      "https://open.bigmodel.cn/usercenter/apikeys",
		"qwen":     "https://help.aliyun.com/zh/model-studio/getting-started/first-api-call-to-qwen",
		"kimi":     "https://kimi.moonshot.cn/",
	}
	lower := strings.ToLower(name)
	for key, link := range links {
		if strings.Contains(lower, key) {
			return link
		}
	}
	return ""
}

// SetHooks sets the hook registry for this agent.
func (a *Agent) SetHooks(h *hook.Registry) { a.hooks = h }

// SetEventBus sets the event bus for this agent.
func (a *Agent) SetEventBus(b *event.EventBus) { a.events = b }

// SetHeartbeatInterval configures heartbeat event emission every N turns. 0 disables.
func (a *Agent) SetHeartbeatInterval(n int) { a.heartbeatInterval = n }

func (a *Agent) Run(ctx context.Context, io transport.UserIO) {
	zap.S().Infow("agent starting")

	// Create session
	sess, err := a.sessMgr.NewSession(a.cfg.Session.MaxLoop)
	if err != nil {
		zap.S().Errorw("create session failed", "error", err)
		return
	}

	state := &LoopState{Sess: sess}

	defer func() {
		a.generateSummary(sess, state)
		sid := string(sess.ID)
		// Fire session:end hook + event
		if a.hooks != nil {
			a.hooks.Fire(ctx, hook.PointSessionEnd, &hook.Context{
				SessionID: sid,
				Turn:      state.Turn,
				Values:    map[string]any{"stop_reason": state.StopReason},
			})
		}
		if a.events != nil {
			a.events.Emit(ctx, event.Event{
				Type:      event.TypeSessionEnded,
				SessionID: sid,
				Turn:      state.Turn,
				Data:      map[string]any{"stop_reason": state.StopReason},
			})
		}
		sess.Close()
		a.sessMgr.Remove(sess.ID)
	}()

	// Build system prompt
	a.ctxBuilder.SetRenderData(ctxpkg.NewRenderData(a.cfg))
	systemPrompt, err := a.ctxBuilder.Build()
	if err != nil {
		zap.S().Errorw("build context failed", "error", err)
		return
	}

	// Inject transport-specific context
	if tc := io.Context(); tc != "" {
		systemPrompt += "\n\n## Transport\n" + tc
	}

	zap.S().Debugw("session started",
		"session_id", sess.ID,
		"max_loop", a.cfg.Session.MaxLoop,
		"model", a.cfg.LLM.Model,
		"provider", a.provider.Type(),
	)

	// Print welcome with MCP tools list.
	// For email transport: only send the startup notification the first time
	// email is configured, not on every startup.
	skipWelcome := io.Name() == "email" && config.IsEmailConfigured()
	if !skipWelcome {
		io.WriteLine(fmt.Sprintf("dolphin %s (%s/%s) %s — Agent ready. Type /help for available commands.", a.version, runtime.GOOS, runtime.Version(), a.commitHash))
		toolDefs := a.toolReg.List()
		if len(toolDefs) > 0 {
			io.WriteString("Loaded MCP tools: ")
			for i, t := range toolDefs {
				if i > 0 {
					io.WriteString(", ")
				}
				io.WriteString(t.Name)
			}
			io.WriteLine("")
		}
		io.WriteLine("")
		if io.Name() == "email" {
			config.MarkEmailConfigured()
		}
	}

	// Fire session:start hook + event
	sid := string(sess.ID)
	hc := &hook.Context{SessionID: sid, Turn: 0, Values: make(map[string]any)}
	if a.hooks != nil {
		a.hooks.Fire(ctx, hook.PointSessionStart, hc)
	}
	if a.events != nil {
		a.events.Emit(ctx, event.Event{
			Type:      event.TypeSessionCreated,
			SessionID: sid,
		})
	}

	for {
		select {
		case <-ctx.Done():
			state.StopReason = "interrupted"
			return
		default:
		}

		// Check max loop — generate summary once and continue
		if state.Turn >= a.cfg.Session.MaxLoop && !state.SummaryGenerated {
			state.SummaryGenerated = true
			zap.S().Infow("max loop reached, generating summary", "turns", state.Turn)
			a.generateSummary(sess, state)
			io.WriteLine("\n[Session checkpoint: summary saved, continuing...]\n")
		}

		// Handle commands
		line, err := io.ReadLine()
		if err != nil {
			zap.S().Debugw("read line error", "error", err)
			state.StopReason = "transport_error"
			return
		}

		if line == "/exit" || line == "exit" || line == "quit" {
			if io.Capabilities().ConfirmExit {
				io.WriteLine("Are you sure you want to exit? [y/N] ")
				confirm, err := io.ReadLine()
				if err != nil {
					zap.S().Debugw("read confirm error", "error", err)
					state.StopReason = "transport_error"
					return
				}
				if strings.ToLower(strings.TrimSpace(confirm)) != "y" && strings.ToLower(strings.TrimSpace(confirm)) != "yes" {
					io.WriteLine("Exit cancelled.")
					continue
				}
			}
			state.StopReason = "user_exit"
			return
		}
		if line == "/mcp" {
			toolDefs := a.toolReg.List()
			if len(toolDefs) == 0 {
				io.WriteLine("No MCP tools loaded.")
			} else {
				io.WriteLine(fmt.Sprintf("MCP tools (%d):", len(toolDefs)))
				for _, t := range toolDefs {
					src := ""
					if t.Source != "" {
						src = fmt.Sprintf(" [%s]", t.Source)
					}
					io.WriteLine(fmt.Sprintf("  %s%s - %s", t.Name, src, t.Description))
				}
			}
			continue
		}
		if line == "/provider" || strings.HasPrefix(line, "/provider ") {
			a.handleProviderCommand(strings.TrimSpace(line), io)
			continue
		}
		if line == "/status" {
			a.handleStatusCommand(state, io)
			continue
		}
		if line == "/help" {
			var sb strings.Builder
			sb.WriteString(i18n.TL(i18n.KeyHelpHeader))
			sb.WriteString("\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpExit))
			sb.WriteString("\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpHelp))
			sb.WriteString("\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpStatus))
			sb.WriteString("\n\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpAgents))
			sb.WriteString("\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpSkills))
			sb.WriteString("\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpCommands))
			sb.WriteString("\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpSessions))
			sb.WriteString("\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpCron))
			sb.WriteString("\n\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpMCP))
			sb.WriteString("\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpModel))
			sb.WriteString("\n")
			sb.WriteString(i18n.TL(i18n.KeyHelpReload))
			sb.WriteString("\n\n")
			toolDefs := a.toolReg.List()
			if len(toolDefs) > 0 {
				sb.WriteString(i18n.TL(i18n.KeyHelpTopMCP))
				sb.WriteString("\n")
				for _, t := range toolDefs {
					sb.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
				}
			}
			io.WriteLine(sb.String())
			continue
		}
		if line == "/reload" {
			if a.reloadFunc != nil {
				io.WriteLine("Reloading configuration...")
				if err := a.reloadFunc(); err != nil {
					io.WriteLine(fmt.Sprintf("[Reload error: %v]", err))
					zap.S().Errorw("config reload failed", "error", err)
				} else {
					io.WriteLine("Configuration reloaded.")
					zap.S().Infow("config reloaded via /reload command")
				}
			} else {
				io.WriteLine("Reload not available.")
			}
			continue
		}
		if line == "" {
			continue
		}

		// Hook: user:input — can rewrite or reject input
		if a.hooks != nil {
			hc := &hook.Context{
				SessionID: string(sess.ID),
				Turn:      state.Turn + 1,
				UserInput: line,
				Values:    make(map[string]any),
			}
			if err := a.hooks.Fire(ctx, hook.PointUserInput, hc); err != nil {
				io.WriteLine(fmt.Sprintf("[Rejected: %v]", err))
				continue
			}
			line = hc.UserInput
		}

		state.Turn++
		sess.Turn = state.Turn

		// Add user message
		userContent := provider.TextContent(line)
		state.Messages = append(state.Messages, provider.Message{Role: "user", Content: userContent})
		sess.LogMessage("user", userContent)

		// Emit user:message event
		if a.events != nil {
			a.events.Emit(ctx, event.Event{
				Type:      event.TypeUserMessage,
				SessionID: string(sess.ID),
				Turn:      state.Turn,
				Data:      map[string]any{"content": line},
			})
		}

		// Run agent sub-loop (handles tool call feedback cycles)
		if err := a.runTurn(ctx, state, systemPrompt, io, a.toolReg, nil); err != nil {
			zap.S().Errorw("turn failed", "turn", state.Turn, "error", err)
			io.WriteLine(fmt.Sprintf("\n[Error: %v]", err))
			if a.hooks != nil {
				hc := &hook.Context{SessionID: string(sess.ID), Turn: state.Turn, Error: err}
				a.hooks.Fire(ctx, hook.PointOnError, hc)
			}
			if a.events != nil {
				a.events.Emit(ctx, event.Event{
					Type:      event.TypeError,
					SessionID: string(sess.ID),
					Turn:      state.Turn,
					Data:      map[string]any{"error": err.Error()},
				})
			}
		}
	}
}

// handleProviderCommand processes /provider and /provider switch <name> commands.
func (a *Agent) handleProviderCommand(line string, io transport.UserIO) {
	fp, ok := a.provider.(*provider.FailoverProvider)
	if !ok {
		io.WriteLine("Provider does not support failover.")
		return
	}

	if line == "/provider" {
		cc := fp.CurrentConfig()
		io.WriteLine(fmt.Sprintf("Current: %s (%s)", fp.Current().Name(), cc.Model))
		io.WriteLine(fmt.Sprintf("Available providers (%d):", len(fp.Configs())))
		for i, pc := range fp.Configs() {
			mark := " "
			if i == fp.CurrentIndex() {
				mark = "→"
			}
			io.WriteLine(fmt.Sprintf("  %s %s (%s)", mark, pc.Name, pc.Model))
		}
		io.WriteLine("Usage: /provider switch <name> to switch, /provider switch to auto-switch")
		return
	}

	// /provider switch <name> or /provider switch
	rest := strings.TrimPrefix(line, "/provider ")
	if rest == "switch" {
		if fp.SwitchToNext() {
			cc := fp.CurrentConfig()
			a.cfg.LLM.Type = cc.Type
			a.cfg.LLM.BaseURL = cc.BaseURL
			a.cfg.LLM.APIKey = cc.APIKey
			a.cfg.LLM.Model = cc.Model
			a.cfg.LLM.MaxTokens = cc.MaxTokens
			a.rebuildCompressor()
			io.WriteLine(fmt.Sprintf("Switched to %s (%s)", cc.Name, cc.Model))
		} else {
			io.WriteLine("No other providers available.")
		}
		return
	}
	if strings.HasPrefix(rest, "switch ") {
		target := strings.TrimSpace(strings.TrimPrefix(rest, "switch "))
		if fp.CurrentIndex() >= 0 && fp.CurrentIndex() < len(fp.Configs()) &&
			strings.EqualFold(fp.Configs()[fp.CurrentIndex()].Name, target) {
			io.WriteLine(fmt.Sprintf("Already using %s.", fp.Configs()[fp.CurrentIndex()].Name))
			return
		}
		if fp.SwitchTo(target) {
			cc := fp.CurrentConfig()
			a.cfg.LLM.Type = cc.Type
			a.cfg.LLM.BaseURL = cc.BaseURL
			a.cfg.LLM.APIKey = cc.APIKey
			a.cfg.LLM.Model = cc.Model
			a.cfg.LLM.MaxTokens = cc.MaxTokens
			a.rebuildCompressor()
			io.WriteLine(fmt.Sprintf("Switched to %s (%s)", cc.Name, cc.Model))
		} else {
			// Check if the provider exists at all (vs health check failure)
			found := false
			for _, pc := range fp.Configs() {
				if strings.EqualFold(pc.Name, target) {
					found = true
					io.WriteLine(fmt.Sprintf("Provider %s is not available: health check failed.", pc.Name))
					break
				}
			}
			if !found {
				io.WriteLine(fmt.Sprintf("Provider %q not found. Use /provider to see available providers.", target))
			}
		}
	}
}

// handleStatusCommand displays current session and agent status.
func (a *Agent) handleStatusCommand(state *LoopState, io transport.UserIO) {
	io.WriteLine("=== Session Status ===")
	io.WriteLine(fmt.Sprintf("Provider:   %s (%s)", a.provider.Name(), a.cfg.LLM.Model))
	io.WriteLine(fmt.Sprintf("Base URL:   %s", a.cfg.LLM.BaseURL))
	io.WriteLine(fmt.Sprintf("Turn:       %d / %d", state.Turn, a.cfg.Session.MaxLoop))
	io.WriteLine(fmt.Sprintf("Tool Calls: %d", state.ToolCallCount))
	io.WriteLine(fmt.Sprintf("Errors:     %d", state.ErrorCount))
	io.WriteLine(fmt.Sprintf("Compressions: %d", state.CompressionCount))
	cachePct := 0.0
	if state.TotalInputTokens > 0 {
		cachePct = float64(state.TotalCachedTokens) / float64(state.TotalInputTokens) * 100
	}
	io.WriteLine(fmt.Sprintf("Cache Hit:   %d / %d (%.1f%%)", state.TotalCachedTokens, state.TotalInputTokens, cachePct))
}

// runTurn handles one user input turn with streaming LLM response and tool call feedback cycles.
// extraTools specifies MCP tool names that should be available to the LLM (beyond the always-available search_mcp_tools).
func (a *Agent) runTurn(ctx context.Context, state *LoopState, systemPrompt string, io transport.UserIO, toolReg *mcp.Registry, extraTools map[string]bool) error {
	a.emitHeartbeat(ctx, state)

	maxSubTurns := a.cfg.LLM.MaxSubTurns
	if maxSubTurns <= 0 {
		maxSubTurns = 10
	}

	a.compressHistory(ctx, state)

	for i := 0; i < maxSubTurns; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		toolDefs := a.buildTurnToolDefs(extraTools, toolReg)

		zap.S().Debugw("llm stream start",
			"turn", state.Turn,
			"sub_turn", i,
			"messages", len(state.Messages),
			"tools", len(toolDefs),
		)

		// Build LLM request
		req := &provider.ProviderRequest{
			Messages:  state.Messages,
			System:    systemPrompt,
			Tools:     toolDefs,
			MaxTokens: a.cfg.LLM.MaxTokens,
			Model:     a.cfg.LLM.Model,
		}

		// Limits check before LLM call
		if a.limitsManager != nil {
			checkReq := &limits.CheckRequest{
				Model:    a.cfg.LLM.Model,
				Provider: a.provider.Name(),
			}
			if err := a.limitsManager.Check(ctx, checkReq); err != nil {
				io.WriteLine(fmt.Sprintf("\n[LLM limit exceeded: %s]\n", err.Error()))
				return err
			}
		}

		// Hook: llm:before — plugins can modify the request or abort
		if err := a.fireBeforeLLM(ctx, state, req); err != nil {
			return err
		}

		// Call LLM with streaming (retry/failover handled by provider decorators)
		llmStart := time.Now()
		streamCh, err := a.provider.CompleteStream(ctx, *req)
		if err != nil {
			return fmt.Errorf("llm call: %w", err)
		}

		// Process stream and build assistant message
		caps := io.Capabilities()
		result := a.processStream(ctx, io, streamCh, caps, state.Turn, string(state.Sess.ID))
		if result.err != nil {
			return result.err
		}
		llmDuration := time.Since(llmStart)

		// Build assistant message content blocks from stream result
		content, toolCalls := a.buildAssistantMessage(result, state)
		state.Messages = append(state.Messages, provider.Message{Role: "assistant", Content: content})

		// Logging, hooks, events
		a.logLLMResponse(ctx, io, state, i, llmDuration, content, toolCalls, result.usage)

		if a.limitsManager != nil && result.usage != nil {
			a.limitsManager.UpdateUsage(&limits.Usage{
				InputTokens:  result.usage.InputTokens,
				OutputTokens: result.usage.OutputTokens,
			})
		}

		if len(toolCalls) == 0 {
			return nil
		}

		// Execute tool calls
		a.executeToolCalls(ctx, io, state, toolCalls, toolReg, caps, i, maxSubTurns)
	}

	if a.cfg.Log.Level == "debug" {
		io.WriteLine("\n[Max tool call iterations reached. Ending turn.]")
	}
	return nil
}

type streamResult struct {
	text              string
	thinking          string
	thinkingSignature string
	toolCalls         []provider.ToolCall
	usage             *provider.Usage
	err               error
}

func (a *Agent) emitHeartbeat(ctx context.Context, state *LoopState) {
	if a.heartbeatInterval > 0 && state.Turn > 0 && state.Turn%a.heartbeatInterval == 0 && a.events != nil {
		a.events.Emit(ctx, event.Event{
			Type:      event.TypeHeartbeat,
			SessionID: string(state.Sess.ID),
			Turn:      state.Turn,
		})
	}
}

func (a *Agent) compressHistory(ctx context.Context, state *LoopState) {
	comp := a.compressor
	if comp == nil {
		comp = &compressor.DropCompressor{}
	}
	compressed, report := comp.Compress(state.Messages, a.cfg.LLM.MaxContextTokens)
	if report == nil || report.DroppedCount == 0 {
		return
	}

	// Compression actually happened — fire telemetry and create span.
	if TelemetryCallbacks.OnCompression != nil {
		TelemetryCallbacks.OnCompression()
	}
	if TelemetryCallbacks.OnCompressionSpan != nil {
		end := TelemetryCallbacks.OnCompressionSpan(ctx, string(state.Sess.ID), state.Turn)
		if end != nil {
			defer end()
		}
	}

	state.Messages = compressed
	for _, seg := range compressor.ExtractSummarySegments(compressed) {
		state.Sess.LogCompression(session.CompressMeta{
			Level:        seg.Level,
			CoveredCount: seg.CoveredCount,
			Summary:      seg.Content,
			TokensSaved:  report.TokensSaved,
		})
		state.CompressionCount++
		state.SummaryTexts = append(state.SummaryTexts, fmt.Sprintf("[L%d] %s", seg.Level, seg.Content))
	}
	zap.S().Debugw("context compressed",
		"dropped", report.DroppedCount,
		"remaining", len(compressed),
		"tokens_saved", report.TokensSaved,
		"turn", state.Turn,
	)
	if a.events != nil {
		a.events.Emit(ctx, event.Event{
			Type:      event.TypeCompression,
			SessionID: string(state.Sess.ID),
			Turn:      state.Turn,
			Data: map[string]any{
				"dropped":      report.DroppedCount,
				"tokens_saved": report.TokensSaved,
			},
		})
	}
}

func (a *Agent) buildTurnToolDefs(extraTools map[string]bool, toolReg *mcp.Registry) []provider.ToolDef {
	var mcpDefs []mcp.ToolDefinition
	nameSet := map[string]bool{"search_mcp_tools": true}
	for name := range extraTools {
		nameSet[name] = true
	}
	for name := range nameSet {
		if t, ok := toolReg.Get(name); ok {
			mcpDefs = append(mcpDefs, t.Definition())
		}
	}
	toolDefs := make([]provider.ToolDef, len(mcpDefs))
	for j, d := range mcpDefs {
		toolDefs[j] = provider.ToolDef{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
		}
	}
	sort.Slice(toolDefs, func(i, j int) bool { return toolDefs[i].Name < toolDefs[j].Name })
	return toolDefs
}

// toolNames extracts all tool names from a registry for use as extraTools.
func toolNames(toolReg *mcp.Registry) map[string]bool {
	names := make(map[string]bool)
	for _, def := range toolReg.List() {
		names[def.Name] = true
	}
	return names
}

func (a *Agent) fireBeforeLLM(ctx context.Context, state *LoopState, req *provider.ProviderRequest) error {
	if a.hooks == nil {
		return nil
	}
	hc := &hook.Context{
		SessionID: string(state.Sess.ID),
		Turn:      state.Turn,
		Request:   req,
		Values:    make(map[string]any),
	}
	if err := a.hooks.Fire(ctx, hook.PointBeforeLLM, hc); err != nil {
		return fmt.Errorf("llm call blocked: %w", err)
	}
	if modified, ok := hc.Request.(*provider.ProviderRequest); ok {
		state.Messages = modified.Messages
	}
	return nil
}

func (a *Agent) processStream(ctx context.Context, io transport.UserIO, streamCh <-chan provider.StreamChunk, caps transport.Capabilities, turn int, sessionID string) *streamResult {
	var textBuf strings.Builder
	var thinkingBuf strings.Builder
	var thinkingSignature string
	type buildingTool struct {
		ID      string
		Name    string
		ArgsBuf strings.Builder
	}
	var tools []buildingTool
	toolIdx := -1
	var finalUsage *provider.Usage

	// Hook: response:before — last chance to abort before output
	if a.hooks != nil {
		hc := &hook.Context{
			SessionID: sessionID,
			Turn:      turn,
			Values:    make(map[string]any),
		}
		if err := a.hooks.Fire(ctx, hook.PointBeforeResponse, hc); err != nil {
			// Hook: transport:send — fires after response content is ready
			if a.hooks != nil {
				a.hooks.Fire(ctx, hook.PointTransportSend, &hook.Context{
					SessionID:     sessionID,
					Turn:          turn,
					TransportName: io.Name(),
					UserOutput:    textBuf.String(),
				})
			}

			return &streamResult{err: fmt.Errorf("response blocked: %w", err)}
		}
	}

	for chunk := range streamCh {
		select {
		case <-ctx.Done():
			return &streamResult{err: ctx.Err()}
		default:
		}

		if chunk.Done {
			break
		}

		if len(chunk.Content) > 0 {
			text := extractText(chunk.Content)
			if text != "" {
				textBuf.WriteString(text)
				io.WriteString(text)
			}
		}

		if chunk.ToolCallBegin != nil {
			tools = append(tools, buildingTool{
				ID:   chunk.ToolCallBegin.ID,
				Name: chunk.ToolCallBegin.Name,
			})
			toolIdx = len(tools) - 1
		}

		if chunk.BlockDelta != "" && chunk.DeltaType == "thinking" {
			thinkingBuf.WriteString(chunk.BlockDelta)
		}

		if chunk.BlockSignature != "" {
			thinkingSignature = chunk.BlockSignature
		}

		if chunk.ToolCallDelta != "" && toolIdx >= 0 {
			tools[toolIdx].ArgsBuf.WriteString(chunk.ToolCallDelta)
		}

		if chunk.Usage != nil {
			if finalUsage == nil {
				finalUsage = chunk.Usage
			} else {
				if chunk.Usage.OutputTokens > 0 {
					finalUsage.OutputTokens = chunk.Usage.OutputTokens
				}
			}
		}
	}

	// Build tool calls from accumulated buffers
	var providerToolCalls []provider.ToolCall
	for _, tc := range tools {
		providerToolCalls = append(providerToolCalls, provider.ToolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: json.RawMessage(tc.ArgsBuf.String()),
		})
	}

	return &streamResult{
		text:              textBuf.String(),
		thinking:          thinkingBuf.String(),
		thinkingSignature: thinkingSignature,
		toolCalls:         providerToolCalls,
		usage:             finalUsage,
	}
}

func (a *Agent) buildAssistantMessage(result *streamResult, state *LoopState) (json.RawMessage, []provider.ToolCall) {
	var outBlocks []map[string]any
	if result.thinking != "" {
		thinkingBlock := map[string]any{
			"type":     "thinking",
			"thinking": result.thinking,
		}
		if result.thinkingSignature != "" {
			thinkingBlock["signature"] = result.thinkingSignature
		}
		outBlocks = append(outBlocks, thinkingBlock)
	}
	if result.text != "" {
		outBlocks = append(outBlocks, map[string]any{"type": "text", "text": result.text})
	}
	for _, tc := range result.toolCalls {
		args := tc.Arguments
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		outBlocks = append(outBlocks, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Name,
			"input": args,
		})
	}
	content, err := json.Marshal(outBlocks)
	if err != nil {
		zap.S().Errorw("failed to marshal assistant message content", "error", err)
		content = json.RawMessage(`[{"type":"text","text":""}]`)
	}
	if len(outBlocks) == 0 {
		content = json.RawMessage(`[{"type":"text","text":""}]`)
	}
	return content, result.toolCalls
}

func (a *Agent) logLLMResponse(ctx context.Context, io transport.UserIO, state *LoopState, subTurn int, llmDuration time.Duration, content json.RawMessage, toolCalls []provider.ToolCall, usage *provider.Usage) {
	if usage != nil {
		state.Sess.LogMessageWithUsage("assistant", content, usage.InputTokens, usage.OutputTokens)
	} else {
		state.Sess.LogMessage("assistant", content)
	}
	for _, tc := range toolCalls {
		state.Sess.LogToolCall(tc.Name, tc.Arguments)
	}
	if usage != nil {
		state.TotalInputTokens += usage.InputTokens
		state.TotalOutputTokens += usage.OutputTokens
		state.TotalCachedTokens += usage.CachedInputTokens
		state.TotalMissedTokens += usage.MissedInputTokens
		session.SetSessionTokens(string(state.Sess.ID), state.TotalInputTokens, state.TotalOutputTokens)
		zap.S().Debugw("llm response",
			"turn", state.Turn,
			"sub_turn", subTurn,
			"duration", llmDuration,
			"input_tokens", usage.InputTokens,
			"output_tokens", usage.OutputTokens,
			"cached_tokens", usage.CachedInputTokens,
			"tool_calls", len(toolCalls),
		)
		if a.cfg.Log.Level == "debug" && io != nil {
			io.WriteLine(fmt.Sprintf("turn: %d model: %s sess: %s tokens: in=%d [cache=%d miss=%d] out=%d (total: in=%d out=%d cache=%d miss=%d)",
				state.Sess.Turn, a.cfg.LLM.Model, state.Sess.ID, usage.InputTokens, usage.CachedInputTokens, usage.MissedInputTokens, usage.OutputTokens,
				state.TotalInputTokens, state.TotalOutputTokens, state.TotalCachedTokens, state.TotalMissedTokens))
		}
	} else if a.cfg.Log.Level == "debug" && io != nil {
		io.WriteLine("  tokens: -")
	}
	if a.hooks != nil {
		a.hooks.Fire(ctx, hook.PointAfterLLM, &hook.Context{
			SessionID: string(state.Sess.ID),
			Turn:      state.Turn,
			Response:  &provider.ProviderResponse{Content: content, ToolCalls: toolCalls, Usage: usage},
		})
	}
	if a.events != nil {
		evtData := map[string]any{"tool_calls": len(toolCalls)}
		if usage != nil {
			evtData["input_tokens"] = usage.InputTokens
			evtData["output_tokens"] = usage.OutputTokens
		}
		a.events.Emit(ctx, event.Event{
			Type:      event.TypeLLMResponse,
			SessionID: string(state.Sess.ID),
			Turn:      state.Turn,
			Data:      evtData,
		})
	}
}

func (a *Agent) executeToolCalls(ctx context.Context, io transport.UserIO, state *LoopState, toolCalls []provider.ToolCall, toolReg *mcp.Registry, caps transport.Capabilities, subTurn, maxSubTurns int) {
	state.ToolCallCount += len(toolCalls)
	for _, tc := range toolCalls {
		zap.S().Debugw("executing tool",
			"name", tc.Name,
			"turn", state.Turn,
			"arguments", string(tc.Arguments),
		)

		effectiveArgs := tc.Arguments
		if a.hooks != nil {
			hc := &hook.Context{
				SessionID: string(state.Sess.ID),
				Turn:      state.Turn,
				ToolName:  tc.Name,
				ToolArgs:  tc.Arguments,
				Values:    make(map[string]any),
			}
			if err := a.hooks.Fire(ctx, hook.PointBeforeTool, hc); err != nil {
				zap.S().Errorw("tool blocked", "name", tc.Name, "error", err)
				continue
			}
			effectiveArgs = hc.ToolArgs
		}
		if a.events != nil {
			a.events.Emit(ctx, event.Event{
				Type:      event.TypeToolCalled,
				SessionID: string(state.Sess.ID),
				Turn:      state.Turn,
				Data:      map[string]any{"tool": tc.Name},
			})
		}

		toolStart := time.Now()
		result, err := toolReg.Execute(ctx, tc.Name, effectiveArgs)
		toolDuration := time.Since(toolStart)

		if a.hooks != nil {
			a.hooks.Fire(ctx, hook.PointAfterTool, &hook.Context{
				SessionID:  string(state.Sess.ID),
				Turn:       state.Turn,
				ToolName:   tc.Name,
				ToolResult: result,
			})
		}
		if a.events != nil {
			a.events.Emit(ctx, event.Event{
				Type:      event.TypeToolCompleted,
				SessionID: string(state.Sess.ID),
				Turn:      state.Turn,
				Data: map[string]any{
					"tool":     tc.Name,
					"duration": toolDuration.String(),
				},
			})
		}

		resultContent := ""
		if err != nil {
			resultContent = fmt.Sprintf("Error: %v", err)
		} else if result != nil {
			resultContent = result.Content
		}

		const maxResultLen = 2000
		llmResultContent := resultContent
		if len(llmResultContent) > maxResultLen {
			llmResultContent = llmResultContent[:maxResultLen] +
				fmt.Sprintf("\n... [result truncated, %d bytes total]", len(resultContent))
		}

		zap.S().Debugw("tool result",
			"name", tc.Name,
			"turn", state.Turn,
			"duration", toolDuration,
			"is_error", err != nil || (result != nil && result.IsError),
			"result_len", len(resultContent),
		)
		if a.cfg.Log.Level == "debug" && caps.ShowToolDetails && resultContent != "" {
			argsCompact := compactJSON(tc.Arguments, 200)
			io.WriteLine(fmt.Sprintf("[%s](%d/%d) %s", tc.Name, subTurn+1, maxSubTurns, argsCompact))
		}

		innerContent, _ := json.Marshal([]map[string]any{
			{"type": "text", "text": llmResultContent},
		})
		resultBlock, _ := json.Marshal([]map[string]any{
			{
				"type":        "tool_result",
				"tool_use_id": tc.ID,
				"content":     json.RawMessage(innerContent),
			},
		})
		state.Messages = append(state.Messages, provider.Message{
			Role:    "tool",
			Content: resultBlock,
		})

		isErr := err != nil || (result != nil && result.IsError)
		if isErr {
			state.ErrorCount++
		}
		state.Sess.LogToolResult(tc.Name, resultBlock, isErr)
	}
}

// extractText extracts the text portion from a content block array.
func extractText(content json.RawMessage) string {
	var blocks []map[string]any
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}
	var buf strings.Builder
	for _, b := range blocks {
		if t, ok := b["text"].(string); ok {
			buf.WriteString(t)
		}
	}
	return buf.String()
}

// compactJSON returns a compact single-line representation of JSON data,
// truncated to maxLen characters if longer.
func compactJSON(raw json.RawMessage, maxLen int) string {
	if len(raw) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		s := string(raw)
		if len(s) > maxLen {
			s = s[:maxLen] + "..."
		}
		return s
	}
	s := buf.String()
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}

func llmBackoffBase(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return time.Second
	}
	return d
}

func (a *Agent) generateSummary(sess *session.Session, state *LoopState) {
	if !a.cfg.Session.Summary {
		return
	}

	// Skip summary for transport errors with no activity — an unused transport
	// that disconnected immediately has nothing worth recording.
	if state.StopReason == "transport_error" && sess.Turn == 0 && state.ToolCallCount == 0 {
		return
	}

	stateStr := "completed"
	switch state.StopReason {
	case "interrupted":
		stateStr = "interrupted"
	case "user_exit":
		stateStr = "user_exit"
	case "max_loop":
		stateStr = "max_loop"
	case "transport_error":
		stateStr = "transport_error"
	}

	summaryText := strings.Join(state.SummaryTexts, "\n")
	sess.GenerateSummary(config.SessionsDir(), state.ToolCallCount, state.ErrorCount, state.CompressionCount, stateStr, summaryText, state.TotalInputTokens, state.TotalOutputTokens, state.TotalCachedTokens, state.TotalMissedTokens)
	zap.S().Infow("session summary",
		"session_id", sess.ID,
		"turns", sess.Turn,
		"tool_calls", state.ToolCallCount,
		"errors", state.ErrorCount,
		"state", stateStr,
	)
}

// RunTask executes a single task as a sub-agent with the given system prompt
// and tool registry. It creates a new session, runs one turn, and returns the
// final assistant response text. parentSessionID links this task to the parent
// conversation for audit tracing (empty string means no link).
func (a *Agent) RunTask(ctx context.Context, task string, systemPrompt string, tools *mcp.Registry, parentSessionID session.SessionID, io transport.UserIO) (TaskResult, error) {
	var sess *session.Session
	var err error
	if parentSessionID != "" {
		sess, err = a.sessMgr.NewSessionWithParent(a.cfg.Session.MaxLoop, parentSessionID)
	} else {
		sess, err = a.sessMgr.NewSession(a.cfg.Session.MaxLoop)
	}
	if err != nil {
		return TaskResult{Status: "error", Error: fmt.Sprintf("create session: %v", err)}, err
	}

	state := &LoopState{Sess: sess}
	state.Messages = append(state.Messages, provider.Message{Role: "user", Content: provider.TextContent(task)})

	// Ensure cleanup even on panic (recovered by pool.workerLoop).
	defer func() {
		a.generateSummary(sess, state)
		sess.Close()
		a.sessMgr.Remove(sess.ID)
	}()

	start := time.Now()
	// Use provided io if available, otherwise create ChannelIO
	taskIO := io
	if taskIO == nil {
		taskIO = NewChannelIO(task)
	}
	// Pass all tool names as extraTools so the LLM can see and call them
	taskErr := a.runTurn(ctx, state, systemPrompt, taskIO, tools, toolNames(tools))

	result := TaskResult{
		TaskID:     string(sess.ID),
		AgentName:  "", // set by caller
		DurationMs: time.Since(start).Milliseconds(),
	}

	if taskErr != nil {
		result.Success = false
		result.Error = taskErr.Error()
		switch {
		case strings.Contains(result.Error, "cancel"):
			result.Status = "cancelled"
		case strings.Contains(result.Error, "deadline") || strings.Contains(result.Error, "timeout"):
			result.Status = "timeout"
		default:
			result.Status = "error"
		}
	} else {
		result.Output = extractFinalResponse(state.Messages)
		result.Success = true
		result.Status = "completed"
	}

	return result, nil
}

// extractFinalResponse returns the text content of the last assistant message.
func extractFinalResponse(messages []provider.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return extractText(messages[i].Content)
		}
	}
	return ""
}

// estimateTokens returns a rough token count for mixed ASCII+CJK content.
// Uses byte/3.5 for non-CJK and ~1 token per CJK character, which is closer
// to reality than bytes/4 for the project's Chinese-language audience.
func estimateTokens(content string) int {
	cjk := 0
	for _, r := range content {
		if r >= 0x2E80 && r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0xFE30 && r <= 0xFE4F {
			cjk++
		}
	}
	// CJK: ~1 token each (conservative). Non-CJK: bytes / 3.5.
	nonCJKTokens := 0
	if nonCJKBytes := len(content) - cjk*3; nonCJKBytes > 0 {
		nonCJKTokens = nonCJKBytes * 10 / 35
	}
	return cjk + nonCJKTokens
}
