package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"dolphinzZ/internal/config"
	"dolphinzZ/internal/mcp"
	"dolphinzZ/internal/session"
	"dolphinzZ/internal/transport"
)

// Agent is the core agent that runs the agent loop.
type Agent struct {
	cfg        *config.Config
	sessMgr    *session.Manager
	toolReg    *mcp.Registry
	provider   Provider
	ctxBuilder *ContextBuilder
}

// LoopState holds state for a single agent run.
type LoopState struct {
	Sess             *session.Session
	Messages         []Message
	Turn             int
	StopReason       string
	ToolCallCount    int
	ErrorCount       int
	SummaryGenerated bool
}

func New(cfg *config.Config, sessMgr *session.Manager, toolReg *mcp.Registry) *Agent {
	var provider Provider
	slog.Info("selecting provider", "type", cfg.LLM.Type, "model", cfg.LLM.Model, "base_url", cfg.LLM.BaseURL)
	switch cfg.LLM.Type {
	case "anthropic":
		provider = NewAnthropicProvider(&cfg.LLM)
	default:
		provider = NewOpenAIProvider(&cfg.LLM)
	}

	return &Agent{
		cfg:        cfg,
		sessMgr:    sessMgr,
		toolReg:    toolReg,
		provider:   provider,
		ctxBuilder: NewContextBuilder(),
	}
}

// Run starts the agent loop with interactive I/O (stdio, SSH, etc.).
func (a *Agent) Run(ctx context.Context, io transport.UserIO) {
	slog.Info("agent starting")

	// Create session
	sess, err := a.sessMgr.NewSession(a.cfg.Session.MaxLoop)
	if err != nil {
		slog.Error("create session failed", "error", err)
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
		slog.Error("build context failed", "error", err)
		return
	}

	slog.Debug("session started",
		"session_id", sess.ID,
		"max_loop", a.cfg.Session.MaxLoop,
		"model", a.cfg.LLM.Model,
		"provider", a.provider.Type(),
	)

	// Print welcome with MCP tools list
	io.WriteLine("DolphinzZ Agent ready. Type /exit to quit, /help for help.")
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
			slog.Info("max loop reached, generating summary", "turns", state.Turn)
			a.generateSummary(sess, state)
			io.WriteLine("\n[Session checkpoint: summary saved, continuing...]\n")
		}

		// Handle commands
		line, err := io.ReadLine()
		if err != nil {
			slog.Debug("read line error", "error", err)
			return
		}

		if line == "/exit" || line == "/quit" {
			state.StopReason = "user_exit"
			return
		}
		if line == "/help" {
			io.WriteLine("Commands: /exit - quit, /help - this help")
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
			slog.Error("turn failed", "turn", state.Turn, "error", err)
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
	a.compressHistory(state)

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

		slog.Debug("llm stream start",
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

		io.WriteLine("") // spacing before response

		for chunk := range streamCh {
			select {
			case <-ctx.Done():
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
					io.WriteString(text)
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

		io.WriteLine("") // trailing newline after streaming

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
		if len(providerToolCalls) > 0 {
			for _, tc := range providerToolCalls {
				state.Sess.LogToolCall(tc.Name, tc.Arguments)
			}
		} else {
			state.Sess.LogMessage("assistant", content)
		}

		// Log usage and timing
		if finalUsage != nil {
			slog.Debug("llm response",
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
			slog.Debug("executing tool",
				"name", tc.Name,
				"turn", state.Turn,
				"arguments", string(tc.Arguments),
			)
			if a.cfg.LogLevel == "debug" {
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

			slog.Debug("tool result",
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
				slog.Debug("tool error", "name", tc.Name, "error", resultContent)
			} else {
				slog.Debug("tool completed", "name", tc.Name)
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
	for _, b := range blocks {
		if t, ok := b["text"].(string); ok {
			return t
		}
	}
	return ""
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
		slog.Warn("llm call failed, retrying",
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

	stateStr := "completed"
	switch state.StopReason {
	case "interrupted":
		stateStr = "interrupted"
	case "user_exit":
		stateStr = "user_exit"
	case "max_loop":
		stateStr = "max_loop"
	}

	sess.GenerateSummary(a.cfg.Session.Dir, state.ToolCallCount, state.ErrorCount, stateStr)
	slog.Info("session summary",
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

// compressHistory checks if messages exceed the context window limit and drops
// older exchanges, keeping at least the most recent turns.
func (a *Agent) compressHistory(state *LoopState) {
	limit := a.cfg.LLM.MaxContextTokens
	if limit <= 0 {
		return
	}

	// Rough token estimate: count bytes of marshaled messages / 4
	est := 0
	for _, m := range state.Messages {
		est += len(m.Content) / 4
		if m.Role == "assistant" {
			est += 20 // overhead per message
		}
	}

	// Add system prompt overhead
	threshold := int(float64(limit) * 0.7)
	if est <= threshold {
		return
	}

	// Count messages to drop: keep at least last 6 messages (last ~3 turns)
	// We drop from start, skipping the first user message
	if len(state.Messages) <= 6 {
		return
	}

	dropped := 0
	// Drop complete turns (user + all following assistant/tool messages up to next user).
	// Keep at least the last user message and everything after it (current turn).
	// If no user found, keep at least 2 messages.
	findKeepIdx := func() int {
		for j := len(state.Messages) - 1; j >= 0; j-- {
			if state.Messages[j].Role == "user" {
				return j
			}
		}
		if len(state.Messages) > 2 {
			return len(state.Messages) - 2
		}
		return 0
	}

	for i := 0; ; {
		keepIdx := findKeepIdx()
		if i >= keepIdx {
			break
		}
		if state.Messages[i].Role != "user" {
			i++
			continue
		}
		// Find end of this turn: next user message or end
		end := i + 1
		for end < len(state.Messages) && state.Messages[end].Role != "user" {
			end++
		}
		// Don't drop if it overlaps with the keep zone
		if end > keepIdx {
			break
		}
		// Drop [i, end) as a complete turn
		for j := i; j < end; j++ {
			est -= len(state.Messages[j].Content) / 4
		}
		dropped += end - i
		state.Messages = append(state.Messages[:i], state.Messages[end:]...)
	}

	if dropped > 0 {
		slog.Info("context compressed",
			"dropped_messages", dropped,
			"remaining", len(state.Messages),
			"estimated_tokens_before", est,
			"turn", state.Turn,
		)
		state.Sess.LogSystem(fmt.Sprintf("context compressed: dropped %d old messages, %d remaining", dropped, len(state.Messages)))
	}
}
