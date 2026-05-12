package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
	"dolphin/internal/transport"

	"go.uber.org/zap"
)

// Agent is the core agent that runs the agent loop.
type Agent struct {
	cfg        *config.Config
	sessMgr    *session.Manager
	toolReg    *mcp.Registry
	provider   Provider
	ctxBuilder *ContextBuilder
	compressor Compressor
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
}

func New(cfg *config.Config, sessMgr *session.Manager, toolReg *mcp.Registry) *Agent {
	var provider Provider
	zap.S().Infow("selecting provider", "type", cfg.LLM.Type, "model", cfg.LLM.Model, "base_url", cfg.LLM.BaseURL)
	switch cfg.LLM.Type {
	case "anthropic":
		provider = NewAnthropicProvider(&cfg.LLM)
	default:
		provider = NewOpenAIProvider(&cfg.LLM)
	}

	a := &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   provider,
		ctxBuilder: NewContextBuilder(),
	}
	switch cfg.LLM.CompressMode {
	case "segment":
		a.compressor = NewSegmentCompressor(cfg.LLM.SegmentMergeLimit)
	case "tiered":
		a.compressor = NewTieredCompressor(provider)
	case "incremental":
		a.compressor = NewIncrementalCompressor(provider)
	case "topic":
		a.compressor = NewTopicCompressor(provider)
	default:
		a.compressor = &DropCompressor{}
	}
	return a
}

// Run starts the agent loop with interactive I/O (stdio, SSH, etc.).
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
		sess.Close()
		a.sessMgr.Remove(sess.ID)
	}()

	// Build system prompt
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

	// Print welcome with MCP tools list
	io.WriteLine("dolphin Agent ready. Type /exit to quit, /help for help.")
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

		if line == "/exit" || line == "/quit" {
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
		if line == "/help" {
			io.WriteLine("Commands: /exit - quit, /help - this help, /mcp - list MCP tools")
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

		state.Turn++
		sess.Turn = state.Turn

		// Add user message
		userContent := TextContent(line)
		state.Messages = append(state.Messages, Message{Role: "user", Content: userContent})
		sess.LogMessage("user", userContent)

		// Run agent sub-loop (handles tool call feedback cycles)
		if err := a.runTurn(ctx, state, systemPrompt, io, a.toolReg); err != nil {
			zap.S().Errorw("turn failed", "turn", state.Turn, "error", err)
			io.WriteLine(fmt.Sprintf("\n[Error: %v]", err))
		}
	}
}

// runTurn handles one user input turn with streaming LLM response and tool call feedback cycles.
func (a *Agent) runTurn(ctx context.Context, state *LoopState, systemPrompt string, io transport.UserIO, toolReg *mcp.Registry) error {
	maxSubTurns := a.cfg.LLM.MaxSubTurns
	if maxSubTurns <= 0 {
		maxSubTurns = 10
	}

	// Compress history if approaching context limit
	comp := a.compressor
	if comp == nil {
		comp = &DropCompressor{}
	}
	compressed, report := comp.Compress(state.Messages, a.cfg.LLM.MaxContextTokens)
	if report != nil && report.DroppedCount > 0 {
		state.Messages = compressed
		// Log each summary segment to the session directory
		for _, seg := range extractSummarySegments(compressed) {
			state.Sess.LogCompression(session.CompressMeta{
				Level:        seg.Level,
				CoveredCount: seg.CoveredCount,
				Summary:      seg.Content,
				TokensSaved:  report.TokensSaved,
			})
			state.CompressionCount++
		}
		zap.S().Debugw("context compressed",
			"dropped", report.DroppedCount,
			"remaining", len(compressed),
			"tokens_saved", report.TokensSaved,
			"turn", state.Turn,
		)
	}

	for i := 0; i < maxSubTurns; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Get tool definitions — progressive disclosure: top 10 most-used + search tool
		mcpDefs := toolReg.MostUsedTools(10)
		// Ensure search_mcp_tools is always available
		if searchTool, ok := toolReg.Get("search_mcp_tools"); ok {
			hasSearch := false
			for _, d := range mcpDefs {
				if d.Name == "search_mcp_tools" {
					hasSearch = true
					break
				}
			}
			if !hasSearch {
				mcpDefs = append(mcpDefs, searchTool.Definition())
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

		zap.S().Debugw("llm stream start",
			"turn", state.Turn,
			"sub_turn", i,
			"messages", len(state.Messages),
			"tools", len(toolDefs),
		)

		// Call LLM with retry and streaming
		llmStart := time.Now()
		streamCh, err := a.callLLMWithRetry(ctx, ProviderRequest{
			Messages:  state.Messages,
			System:    systemPrompt,
			Tools:     toolDefs,
			MaxTokens: a.cfg.LLM.MaxTokens,
			Model:     a.cfg.LLM.Model,
		})
		if err != nil {
			return fmt.Errorf("llm call: %w", err)
		}

		// Process stream: accumulate text (progressive output) and tool calls
		var textBuf strings.Builder
		var thinkingBuf strings.Builder
		var thinkingSignature string
		type buildingTool struct {
			ID      string
			Name    string
			ArgsBuf strings.Builder
		}
		var tools []buildingTool
		var toolIdx = -1
		var finalUsage *Usage

		// Determine write strategy based on transport capabilities
		caps := io.Capabilities()

		// For block transports, buffer output and only flush at threshold or stream end.
		// No periodic flush — each mqtt publish is a separate message, so we want
		// one message per complete turn response, not fragments.
		var blockBuf strings.Builder
		const blockFlushThreshold = 1024

		flushBlock := func() {
			if blockBuf.Len() > 0 {
				io.WriteString(blockBuf.String())
				blockBuf.Reset()
			}
		}

		if caps.Streaming {
			io.WriteLine("") // spacing before response (streaming: typewriter effect)
		}

		for chunk := range streamCh {
			select {
			case <-ctx.Done():
				flushBlock()
				return ctx.Err()
			default:
			}

			if chunk.Done {
				break
			}

			// Text content — write progressively for typewriter effect
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

			// Tool call start
			if chunk.ToolCallBegin != nil {
				tools = append(tools, buildingTool{
					ID:   chunk.ToolCallBegin.ID,
					Name: chunk.ToolCallBegin.Name,
				})
				toolIdx = len(tools) - 1
			}

			// Content block delta (text, thinking, tool args)
			if chunk.BlockDelta != "" && chunk.DeltaType == "thinking" {
				thinkingBuf.WriteString(chunk.BlockDelta)
			}

			// Thinking signature (message-level delta, passed back for thinking blocks)
			if chunk.BlockSignature != "" {
				thinkingSignature = chunk.BlockSignature
			}

			// Tool call argument delta
			if chunk.ToolCallDelta != "" && toolIdx >= 0 {
				tools[toolIdx].ArgsBuf.WriteString(chunk.ToolCallDelta)
			}

			// Usage info (arrives at end on some providers)
			if chunk.Usage != nil {
				finalUsage = chunk.Usage
			}

		}

		// Final flush: send any remaining buffered content as a single message.
		if !caps.Streaming && blockBuf.Len() > 0 {
			blockBuf.WriteString("\n")
			flushBlock()
		} else if caps.Streaming {
			io.WriteLine("") // trailing newline after streaming
		}

		llmDuration := time.Since(llmStart)

		// Build full assistant message content blocks
		var outBlocks []map[string]any
		// Thinking block (provider-agnostic, with optional signature)
		if thinkingBuf.Len() > 0 {
			thinkingBlock := map[string]any{
				"type":     "thinking",
				"thinking": thinkingBuf.String(),
			}
			if thinkingSignature != "" {
				thinkingBlock["signature"] = thinkingSignature
			}
			outBlocks = append(outBlocks, thinkingBlock)
		}
		if textBuf.Len() > 0 {
			outBlocks = append(outBlocks, map[string]any{"type": "text", "text": textBuf.String()})
		}
		var providerToolCalls []ToolCall
		for _, tc := range tools {
			argsJSON := json.RawMessage(tc.ArgsBuf.String())
			outBlocks = append(outBlocks, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Name,
				"input": argsJSON,
			})
			providerToolCalls = append(providerToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: argsJSON,
			})
		}

		content, _ := json.Marshal(outBlocks)
		state.Messages = append(state.Messages, Message{Role: "assistant", Content: content})

		// Log to session
		state.Sess.LogMessage("assistant", content)
		for _, tc := range providerToolCalls {
			state.Sess.LogToolCall(tc.Name, tc.Arguments)
		}
		// Log usage and timing
		if finalUsage != nil {
			zap.S().Debugw("llm response",
				"turn", state.Turn,
				"sub_turn", i,
				"duration", llmDuration,
				"input_tokens", finalUsage.InputTokens,
				"output_tokens", finalUsage.OutputTokens,
				"tool_calls", len(providerToolCalls),
			)
		}

		// No tool calls — final response, done
		if len(providerToolCalls) == 0 {
			return nil
		}

		// Execute tool calls
		state.ToolCallCount += len(providerToolCalls)
		for _, tc := range providerToolCalls {
			zap.S().Debugw("executing tool",
				"name", tc.Name,
				"turn", state.Turn,
				"arguments", string(tc.Arguments),
			)
			if a.cfg.LogLevel == "debug" && caps.Streaming {
				io.WriteLine(fmt.Sprintf("\n[Calling tool: %s]", tc.Name))
			}

			toolStart := time.Now()
			result, err := toolReg.Execute(ctx, tc.Name, tc.Arguments)
			toolDuration := time.Since(toolStart)

			resultContent := ""
			if err != nil {
				resultContent = fmt.Sprintf("Error: %v", err)
			} else if result != nil {
				resultContent = result.Content
			}

			// Truncate oversized tool results
			llmResultContent := resultContent
			const maxResultLen = 2000
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

			// Build tool result message block (Anthropic tool_result format)
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

			if isErr {
				zap.S().Debugw("tool error", "name", tc.Name, "error", resultContent)
			} else {
				zap.S().Debugw("tool completed", "name", tc.Name)
			}
		}
	}

	if a.cfg.LogLevel == "debug" {
		io.WriteLine("\n[Max tool call iterations reached. Ending turn.]")
	}
	return nil
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

// callLLMWithRetry calls CompleteStream with exponential backoff retry.
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
			"wait", wait,
			"error", err,
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return a.provider.CompleteStream(ctx, req) // last try
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

	sess.GenerateSummary(a.cfg.Session.Dir, state.ToolCallCount, state.ErrorCount, state.CompressionCount, stateStr)
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

	start := time.Now()
	taskErr := a.runTurn(ctx, state, systemPrompt, NewChannelIO(task), tools)

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

	// Cleanup session
	a.generateSummary(sess, state)
	sess.Close()
	a.sessMgr.Remove(sess.ID)

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
