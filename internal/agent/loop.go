package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/i18n"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
	"dolphin/internal/transport"

	"go.uber.org/zap"
)

// Agent is the core agent that runs the agent loop.
type Agent struct {
	cfg                *config.Config
	sessMgr            *session.Manager
	toolReg            *mcp.Registry
	provider           Provider
	ctxBuilder         *ContextBuilder
	compressor         Compressor
	hooks              *hook.Registry
	events             *event.EventBus
	heartbeatInterval  int // emit heartbeat event every N turns, 0=off
	availableProviders []config.ProviderConfig
	providerIndex      int
	version            string
	buildTime          string
}

// LoopState holds state for a single agent run.
type LoopState struct {
	Sess             *session.Session
	Messages         []Message
	Turn             int
	StopReason       string
	ToolCallCount    int
	ErrorCount       int
	CompressionCount int
	SummaryGenerated bool
	SummaryTexts     []string
}

func New(cfg *config.Config, sessMgr *session.Manager, toolReg *mcp.Registry) *Agent {
	providers := cfg.LLM.EffectiveProviders()
	provider, selIdx := selectProvider(cfg)

	a := &Agent{
		cfg:                cfg,
		sessMgr:            sessMgr,
		toolReg:            toolReg,
		provider:           provider,
		ctxBuilder:         NewContextBuilder(),
		availableProviders: providers,
		providerIndex:      selIdx,
	}
	a.rebuildCompressor()
	return a
}

// switchToNextProvider switches to the next available provider.
// Returns true on success, false if no more providers to try.
func (a *Agent) switchToNextProvider() bool {
	for i := a.providerIndex + 1; i < len(a.availableProviders); i++ {
		pc := a.availableProviders[i]
		p := NewProviderFromConfig(&pc)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := p.HealthCheck(ctx)
		cancel()

		if err == nil {
			a.providerIndex = i
			a.provider = p
			// Copy back to legacy fields for downstream compat.
			a.cfg.LLM.Type = pc.Type
			a.cfg.LLM.BaseURL = pc.BaseURL
			a.cfg.LLM.APIKey = pc.APIKey
			a.cfg.LLM.Model = pc.Model
			a.cfg.LLM.MaxTokens = pc.MaxTokens
			a.rebuildCompressor()
			zap.S().Infow("failed over to provider", "name", pc.Name, "model", pc.Model)
			return true
		}
		zap.S().Warnw("failover health check failed", "name", pc.Name, "error", err)
	}
	return false
}

// switchToProvider switches to the named provider (by config name).
// Performs a health check before switching. Returns true on success.
func (a *Agent) switchToProvider(name string) bool {
	for i, pc := range a.availableProviders {
		if pc.Name != name {
			continue
		}
		p := NewProviderFromConfig(&pc)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := p.HealthCheck(ctx)
		cancel()
		if err != nil {
			zap.S().Warnw("switch to provider: health check failed", "name", name, "error", err)
			return false
		}
		a.providerIndex = i
		a.provider = p
		a.cfg.LLM.Type = pc.Type
		a.cfg.LLM.BaseURL = pc.BaseURL
		a.cfg.LLM.APIKey = pc.APIKey
		a.cfg.LLM.Model = pc.Model
		a.cfg.LLM.MaxTokens = pc.MaxTokens
		a.rebuildCompressor()
		zap.S().Infow("switched to provider", "name", name, "model", pc.Model)
		return true
	}
	zap.S().Warnw("switch to provider: not found", "name", name)
	return false
}

func (a *Agent) SetVersion(v string)   { a.version = v }
func (a *Agent) SetBuildTime(t string) { a.buildTime = t }

func (a *Agent) rebuildCompressor() {
	switch a.cfg.LLM.CompressMode {
	case "segment":
		a.compressor = NewSegmentCompressor(a.cfg.LLM.SegmentMergeLimit)
	case "tiered":
		a.compressor = NewTieredCompressor(a.provider)
	case "incremental":
		a.compressor = NewIncrementalCompressor(a.provider)
	case "topic":
		a.compressor = NewTopicCompressor(a.provider)
	default:
		a.compressor = &DropCompressor{}
	}
}

// selectProvider runs health checks on all configured providers concurrently,
// then picks the first available one in configured order.
// Returns the selected Provider and its index in the providers list.
func selectProvider(cfg *config.Config) (Provider, int) {
	providers := cfg.LLM.EffectiveProviders()
	if len(providers) == 0 {
		zap.S().Fatal("no LLM providers configured")
	}

	type jobResult struct {
		idx      int
		provider Provider
		pc       config.ProviderConfig
		ok       bool
		ms       int64
		err      string
	}

	n := len(providers)
	ch := make(chan *jobResult, n)
	var wg sync.WaitGroup

	for i, pc := range providers {
		wg.Add(1)
		go func(idx int, pcfg config.ProviderConfig) {
			defer wg.Done()
			p := NewProviderFromConfig(&pcfg)

			// Skip health check for placeholder/test API keys.
			if isPlaceholderKey(pcfg.APIKey) {
				ch <- &jobResult{idx, p, pcfg, true, 0, ""}
				return
			}

			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := p.HealthCheck(ctx)
			cancel()

			// Retry once on network jitter.
			if err != nil {
				start = time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err = p.HealthCheck(ctx)
				cancel()
			}

			ms := time.Since(start).Milliseconds()
			if err != nil {
				ch <- &jobResult{idx, p, pcfg, false, ms, err.Error()}
			} else {
				ch <- &jobResult{idx, p, pcfg, true, ms, ""}
			}
		}(i, pc)
	}

	wg.Wait()
	close(ch)

	// Collect all results.
	var results []*jobResult
	for r := range ch {
		results = append(results, r)
	}
	// Restore config order.
	sort.Slice(results, func(i, j int) bool { return results[i].idx < results[j].idx })

	// Build banner.
	{
		var buf strings.Builder
		buf.WriteString(i18n.TL(i18n.KeyLLMProvidersHeader))
		for _, r := range results {
			if r.ok {
				buf.WriteString(fmt.Sprintf(i18n.TL(i18n.KeyLLMProviderOK), r.pc.Name, r.pc.Model, r.ms))
			} else {
				buf.WriteString(fmt.Sprintf(i18n.TL(i18n.KeyLLMProviderFail), r.pc.Name, r.pc.Model, r.ms, r.err))
			}
		}
		// Pick first available in config order.
		selected := false
		for _, r := range results {
			if r.ok {
				buf.WriteString(fmt.Sprintf(i18n.TL(i18n.KeyLLMUsing), r.pc.Name))
				selected = true
				fmt.Fprint(os.Stderr, buf.String())

				zap.S().Infow("selected LLM provider",
					"name", r.pc.Name,
					"type", r.pc.Type,
					"model", r.pc.Model,
					"base_url", r.pc.BaseURL,
					"ms", r.ms,
				)
				// Copy back to legacy fields for downstream compat.
				cfg.LLM.Type = r.pc.Type
				cfg.LLM.BaseURL = r.pc.BaseURL
				cfg.LLM.APIKey = r.pc.APIKey
				cfg.LLM.Model = r.pc.Model
				cfg.LLM.MaxTokens = r.pc.MaxTokens
				return r.provider, r.idx
			}
		}
		if !selected {
			buf.WriteString(i18n.TL(i18n.KeyNoAvailableProvider))
			fmt.Fprint(os.Stderr, buf.String())
		}
	}

	// All failed.
	printProviderHelp(providers)
	return NewProviderFromConfig(&providers[0]), 0 // fall back to first
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
		buf.WriteString(fmt.Sprintf("  - %s (%s)", pc.Name, pc.BaseURL))
		if link != "" {
			buf.WriteString(fmt.Sprintf("\n    Get API key: %s", link))
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

// Run starts the agent loop with interactive I/O (stdio, SSH, etc.).
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
	a.ctxBuilder.SetRenderData(a.cfg)
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
		io.WriteLine(fmt.Sprintf("dolphin %s (%s/%s) built %s — Agent ready. Type /exit to quit, /help for help.", a.version, runtime.GOOS, runtime.Version(), a.buildTime))
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
				state.StopReason = "user_exit"
				return
			}
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
			io.WriteLine("Commands: /exit - quit, /help - this help, /mcp - list MCP tools, /provider - list/switch LLM provider, /status - session info")
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
			io.WriteLine("Otherwise, enter your message for the agent.")
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
		userContent := TextContent(line)
		state.Messages = append(state.Messages, Message{Role: "user", Content: userContent})
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
	if line == "/provider" {
		io.WriteLine(fmt.Sprintf("Current: %s (%s)", a.provider.Name(), a.cfg.LLM.Model))
		io.WriteLine(fmt.Sprintf("Available providers (%d):", len(a.availableProviders)))
		for i, pc := range a.availableProviders {
			mark := " "
			if i == a.providerIndex {
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
		// Auto-switch to next available
		if a.switchToNextProvider() {
			io.WriteLine(fmt.Sprintf("Switched to %s (%s)", a.provider.Name(), a.cfg.LLM.Model))
		} else {
			io.WriteLine("No other providers available.")
		}
		return
	}
	if strings.HasPrefix(rest, "switch ") {
		target := strings.TrimSpace(strings.TrimPrefix(rest, "switch "))
		for i, pc := range a.availableProviders {
			if strings.EqualFold(pc.Name, target) {
				if i == a.providerIndex {
					io.WriteLine(fmt.Sprintf("Already using %s.", pc.Name))
					return
				}
				p := NewProviderFromConfig(&pc)
				checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := p.HealthCheck(checkCtx)
				cancel()
				if err != nil {
					io.WriteLine(fmt.Sprintf("Provider %s is not available: %v", pc.Name, err))
					return
				}
				a.providerIndex = i
				a.provider = p
				a.cfg.LLM.Type = pc.Type
				a.cfg.LLM.BaseURL = pc.BaseURL
				a.cfg.LLM.APIKey = pc.APIKey
				a.cfg.LLM.Model = pc.Model
				a.cfg.LLM.MaxTokens = pc.MaxTokens
				a.rebuildCompressor()
				io.WriteLine(fmt.Sprintf("Switched to %s (%s)", pc.Name, pc.Model))
				return
			}
		}
		io.WriteLine(fmt.Sprintf("Provider %q not found. Use /provider to see available providers.", target))
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
		req := &ProviderRequest{
			Messages:  state.Messages,
			System:    systemPrompt,
			Tools:     toolDefs,
			MaxTokens: a.cfg.LLM.MaxTokens,
			Model:     a.cfg.LLM.Model,
		}

		// Hook: llm:before — plugins can modify the request or abort
		if err := a.fireBeforeLLM(ctx, state, req); err != nil {
			return err
		}

		// Call LLM with retry and streaming
		llmStart := time.Now()
		streamCh, err := a.callLLMWithRetry(ctx, *req)
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
		state.Messages = append(state.Messages, Message{Role: "assistant", Content: content})

		// Logging, hooks, events
		a.logLLMResponse(ctx, state, i, llmDuration, content, toolCalls, result.usage)

		if len(toolCalls) == 0 {
			return nil
		}

		// Execute tool calls
		a.executeToolCalls(ctx, io, state, toolCalls, toolReg, caps)
	}

	if a.cfg.LogLevel == "debug" {
		io.WriteLine("\n[Max tool call iterations reached. Ending turn.]")
	}
	return nil
}

type streamResult struct {
	text              string
	thinking          string
	thinkingSignature string
	toolCalls         []ToolCall
	usage             *Usage
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
		comp = &DropCompressor{}
	}
	compressed, report := comp.Compress(state.Messages, a.cfg.LLM.MaxContextTokens)
	if report == nil || report.DroppedCount == 0 {
		return
	}
	state.Messages = compressed
	for _, seg := range extractSummarySegments(compressed) {
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

func (a *Agent) buildTurnToolDefs(extraTools map[string]bool, toolReg *mcp.Registry) []ToolDef {
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
	toolDefs := make([]ToolDef, len(mcpDefs))
	for j, d := range mcpDefs {
		toolDefs[j] = ToolDef{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
		}
	}
	return toolDefs
}

func (a *Agent) fireBeforeLLM(ctx context.Context, state *LoopState, req *ProviderRequest) error {
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
	if modified, ok := hc.Request.(*ProviderRequest); ok {
		state.Messages = modified.Messages
	}
	return nil
}

func (a *Agent) processStream(ctx context.Context, io transport.UserIO, streamCh <-chan StreamChunk, caps transport.Capabilities, turn int, sessionID string) *streamResult {
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
	var finalUsage *Usage

	var blockBuf strings.Builder
	const blockFlushThreshold = 1024

	flushBlock := func() {
		if blockBuf.Len() > 0 {
			io.WriteString(blockBuf.String())
			blockBuf.Reset()
		}
	}

	// Hook: response:before — last chance to abort before output
	if a.hooks != nil {
		hc := &hook.Context{
			SessionID: sessionID,
			Turn:      turn,
			Values:    make(map[string]any),
		}
		if err := a.hooks.Fire(ctx, hook.PointBeforeResponse, hc); err != nil {
			return &streamResult{err: fmt.Errorf("response blocked: %w", err)}
		}
	}

	if caps.Streaming {
		io.WriteLine("")
	}

	for chunk := range streamCh {
		select {
		case <-ctx.Done():
			flushBlock()
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
				if caps.Streaming {
					io.WriteString(text)
				} else {
					blockBuf.WriteString(text)
					if blockBuf.Len() >= blockFlushThreshold {
						flushBlock()
					}
				}
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
			finalUsage = chunk.Usage
		}
	}

	if !caps.Streaming && blockBuf.Len() > 0 {
		blockBuf.WriteString("\n")
		flushBlock()
	} else if caps.Streaming {
		io.WriteLine("")
	}

	// Build tool calls from accumulated buffers
	var providerToolCalls []ToolCall
	for _, tc := range tools {
		providerToolCalls = append(providerToolCalls, ToolCall{
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

func (a *Agent) buildAssistantMessage(result *streamResult, state *LoopState) (json.RawMessage, []ToolCall) {
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
		outBlocks = append(outBlocks, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Name,
			"input": tc.Arguments,
		})
	}
	content, _ := json.Marshal(outBlocks)
	return content, result.toolCalls
}

func (a *Agent) logLLMResponse(ctx context.Context, state *LoopState, subTurn int, llmDuration time.Duration, content json.RawMessage, toolCalls []ToolCall, usage *Usage) {
	state.Sess.LogMessage("assistant", content)
	for _, tc := range toolCalls {
		state.Sess.LogToolCall(tc.Name, tc.Arguments)
	}
	if usage != nil {
		zap.S().Debugw("llm response",
			"turn", state.Turn,
			"sub_turn", subTurn,
			"duration", llmDuration,
			"input_tokens", usage.InputTokens,
			"output_tokens", usage.OutputTokens,
			"tool_calls", len(toolCalls),
		)
	}
	if a.hooks != nil {
		a.hooks.Fire(ctx, hook.PointAfterLLM, &hook.Context{
			SessionID: string(state.Sess.ID),
			Turn:      state.Turn,
			Response:  &ProviderResponse{Content: content, ToolCalls: toolCalls, Usage: usage},
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

func (a *Agent) executeToolCalls(ctx context.Context, io transport.UserIO, state *LoopState, toolCalls []ToolCall, toolReg *mcp.Registry, caps transport.Capabilities) {
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
		if a.cfg.LogLevel == "debug" && caps.ShowToolDetails && resultContent != "" {
			argsCompact := compactJSON(tc.Arguments, 200)
			io.WriteLine(fmt.Sprintf("[Calling tool: %s] %s", tc.Name, argsCompact))
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
		state.Messages = append(state.Messages, Message{
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

func isPlaceholderKey(key string) bool {
	return key == "" || key == "test-key" || key == "sk-placeholder" || strings.HasPrefix(key, "test-")
}

// callLLMWithRetry calls CompleteStream with exponential backoff retry.
// On exhaustion, it fails over to the next available provider if any.
func (a *Agent) callLLMWithRetry(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error) {
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		ch, err := a.provider.CompleteStream(ctx, req)
		if err == nil {
			return ch, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		wait := time.Duration(1<<uint(attempt)) * time.Second
		zap.S().Warnw("llm call failed, retrying",
			"attempt", attempt+1,
			"max", maxRetries,
			"error", err,
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	// Retries exhausted — try failover to next provider
	origName := a.provider.Name()
	if a.switchToNextProvider() {
		zap.S().Warnw("failed over to next provider after retry exhaustion",
			"from", origName,
			"to", a.provider.Name(),
		)
		// Re-retry on new provider
		for attempt := 0; attempt < maxRetries; attempt++ {
			ch, err := a.provider.CompleteStream(ctx, req)
			if err == nil {
				return ch, nil
			}
			if !isRetryable(err) {
				return nil, err
			}
			wait := time.Duration(1<<uint(attempt)) * time.Second
			zap.S().Warnw("failover llm call failed, retrying",
				"attempt", attempt+1,
				"max", maxRetries,
				"error", err,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
	}

	return a.provider.CompleteStream(ctx, req) // last try on current (or failover) provider
}

func isRetryable(err error) bool {
	s := err.Error()
	return strings.Contains(s, "429") ||
		strings.Contains(s, "500") ||
		strings.Contains(s, "502") ||
		strings.Contains(s, "503") ||
		strings.Contains(s, "connection") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "EOF")
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
	sess.GenerateSummary(a.cfg.Session.Dir, state.ToolCallCount, state.ErrorCount, state.CompressionCount, stateStr, summaryText)
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
func (a *Agent) RunTask(ctx context.Context, task string, systemPrompt string, tools *mcp.Registry, parentSessionID session.SessionID) (TaskResult, error) {
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
	state.Messages = append(state.Messages, Message{Role: "user", Content: TextContent(task)})

	// Ensure cleanup even on panic (recovered by pool.workerLoop).
	defer func() {
		a.generateSummary(sess, state)
		sess.Close()
		a.sessMgr.Remove(sess.ID)
	}()

	start := time.Now()
	taskErr := a.runTurn(ctx, state, systemPrompt, NewChannelIO(task), tools, nil)

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
func extractFinalResponse(messages []Message) string {
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
	runes := []rune(content)
	cjk := 0
	for _, r := range runes {
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
