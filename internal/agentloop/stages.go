package agentloop

import (
	"context"
	"fmt"
	"strings"
	"time"

	appctx "dolphin/internal/context"
	"dolphin/internal/event"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/signal"
	"dolphin/internal/skill"
	"dolphin/internal/tool"
	"dolphin/internal/types"

	"go.uber.org/zap"
)

type Stage interface {
	Name() string
	Process(ctx context.Context, state *State) error
}

type State struct {
	SessionID        string
	Input            string
	TransportContext string
	History          []types.Message
	Messages         []types.Message
	SystemPrompt     string
	Tools            []types.ToolDef
	ToolCalls        []types.ToolCall
	ToolResults      []types.ToolResult
	Round            int
	Done             bool
	ToolsCalled      bool

	OnChunk func(text string)
}

type Compositor struct {
	initStages  []Stage
	loopStages  []Stage
	maxRounds   int
	turnTimeout time.Duration // per-turn timeout, 0 = no timeout
}

func NewCompositor(init, loop []Stage, maxRounds int) *Compositor {
	return &Compositor{
		initStages: init,
		loopStages: loop,
		maxRounds:  maxRounds,
	}
}

func (c *Compositor) SetTurnTimeout(d time.Duration) {
	c.turnTimeout = d
}

func (c *Compositor) Execute(ctx context.Context, state *State) error {
	if c.turnTimeout > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, c.turnTimeout)
		defer cancel()
	}

	for _, stage := range c.initStages {
		if err := stage.Process(ctx, state); err != nil {
			return fmt.Errorf("init stage %s: %w", stage.Name(), err)
		}
	}

	for !state.Done && state.Round < c.maxRounds {
		for _, stage := range c.loopStages {
			if err := stage.Process(ctx, state); err != nil {
				return fmt.Errorf("loop stage %s: %w", stage.Name(), err)
			}
		}
		state.Round++
	}
	return nil
}

// MemoryReadStage reads history from memory.
type MemoryReadStage struct {
	Memory memory.Memory
}

func (s *MemoryReadStage) Name() string { return "memory_read" }

func (s *MemoryReadStage) Process(ctx context.Context, state *State) error {
	history, err := s.Memory.Read(ctx, state.SessionID)
	if err != nil {
		return err
	}
	state.History = history
	state.Messages = append(history, types.Message{
		Role:      types.RoleUser,
		Content:   state.Input,
		Timestamp: time.Now(),
	})
	return nil
}

// BrainIndexReader provides the brain index content to inject into system prompt.
type BrainIndexReader = appctx.BrainIndexReader

// ContextBuilderStage assembles the system prompt from registered sections.
type ContextBuilderStage struct {
	BaseSystemPrompt string
	SkillStore       skill.Store
	Brain            BrainIndexReader
	Workspace        string
	EventBus         *event.Bus

	reg          *appctx.Registry
	transportCtx string // set per-call in Process
}

// RegisterSection adds a prompt section to the registry.
func (s *ContextBuilderStage) RegisterSection(section appctx.Section) {
	s.initRegistry()
	s.reg.Register(section)
}

func (s *ContextBuilderStage) initRegistry() {
	if s.reg != nil {
		return
	}
	s.reg = appctx.NewRegistry()
	s.reg.Register(&appctx.Base{
		Workspace:   s.Workspace,
		DefaultText: s.BaseSystemPrompt,
	})
	s.reg.Register(&appctx.Transport{
		ContextFunc: func() string { return s.transportCtx },
	})
	s.reg.Register(&appctx.Workspace{Dir: s.Workspace})
	s.reg.Register(&appctx.Brain{Reader: s.Brain})
	s.reg.Register(&appctx.Design{Workspace: s.Workspace})
	s.reg.Register(&appctx.Soul{Workspace: s.Workspace})
	s.reg.Register(&appctx.Skills{Store: s.SkillStore})
}

func (s *ContextBuilderStage) Name() string { return "context_builder" }

func (s *ContextBuilderStage) Process(ctx context.Context, state *State) error {
	s.transportCtx = state.TransportContext

	if s.EventBus != nil {
		s.EventBus.Publish(ctx, event.Event{
			Type:      event.EventContextStart,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
		})
	}

	prompt, err := s.BuildSystemPrompt(ctx)
	if err != nil {
		return err
	}
	state.SystemPrompt = prompt

	if s.EventBus != nil {
		s.EventBus.Publish(ctx, event.Event{
			Type:      event.EventContextComplete,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
			Payload: map[string]any{
				"input": prompt,
			},
		})
	}
	return nil
}

// BuildSystemPrompt assembles the full system prompt from registered sections.
func (s *ContextBuilderStage) BuildSystemPrompt(ctx context.Context) (string, error) {
	s.initRegistry()

	if s.EventBus != nil {
		s.EventBus.Publish(ctx, event.Event{
			Type:      event.EventContextBuildStart,
			Timestamp: time.Now(),
		})
	}

	result, err := s.reg.Build(ctx)

	if s.EventBus != nil {
		payload := map[string]any{
			"error": err != nil,
		}
		if err == nil {
			payload["input"] = result
		}
		s.EventBus.Publish(ctx, event.Event{
			Type:      event.EventContextBuildComplete,
			Timestamp: time.Now(),
			Payload:   payload,
		})
	}

	return result, err
}

// LLMStage calls the LLM and processes streaming response.
type LLMStage struct {
	Provider     llm.Provider
	Model        string
	MaxTokens    int
	MaxRetries   int
	ToolRegistry *tool.Registry
	EventBus     *event.Bus
	Logger       *zap.Logger
}

func (s *LLMStage) Name() string { return "llm" }

// activeModel returns the current model name, preferring Manager.ActiveModel
// when the provider supports it.
func (s *LLMStage) activeModel() string {
	if s.Model != "" {
		return s.Model
	}
	if a, ok := s.Provider.(interface{ ActiveModel() string }); ok {
		return a.ActiveModel()
	}
	return ""
}

func (s *LLMStage) Process(ctx context.Context, state *State) error {
	var lastErr error
	for i := 0; i <= s.MaxRetries; i++ {
		err := s.tryComplete(ctx, state)
		if err == nil {
			return nil
		}
		lastErr = err
		s.EventBus.Publish(ctx, event.Event{
			Type:      event.EventLLMRetry,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
			Payload:   map[string]any{"error": err.Error(), "attempt": i},
		})
	}

	s.EventBus.Publish(ctx, event.Event{
		Type:      event.EventLLMError,
		Timestamp: time.Now(),
		SessionID: state.SessionID,
		Payload:   map[string]any{"error": lastErr.Error()},
	})
	return lastErr
}

func (s *LLMStage) tryComplete(ctx context.Context, state *State) error {
	msgs := state.Messages

	// Use per-turn tools if set, otherwise fall back to global registry.
	tools := state.Tools
	if len(tools) == 0 && s.ToolRegistry != nil {
		tools, _ = s.ToolRegistry.List(ctx)
	}

	toolNames := make([]string, len(tools))
	for i, t := range tools {
		toolNames[i] = t.Name
	}

	s.EventBus.Publish(ctx, event.Event{
		Type:      event.EventToolAssembly,
		Timestamp: time.Now(),
		SessionID: state.SessionID,
		Payload: map[string]any{
			"tools": toolNames,
		},
	})

	s.EventBus.Publish(ctx, event.Event{
		Type:      event.EventLLMStart,
		Timestamp: time.Now(),
		SessionID: state.SessionID,
		Payload: map[string]any{
			"model": s.activeModel(),
			"tools": toolNames,
		},
	})

	ch, err := s.Provider.CompleteStream(ctx, llm.LLMRequest{
		Messages:  msgs,
		System:    state.SystemPrompt,
		Model:     s.activeModel(),
		MaxTokens: s.MaxTokens,
		Tools:     tools,
	})
	if err != nil {
		return err
	}

	var content strings.Builder
	var thinking strings.Builder
	var thinkingSignature string
	var toolCalls []types.ToolCall
	var inputTokens, outputTokens int
	var cacheCreationInputTokens, cacheReadInputTokens, promptCachedTokens int

	for chunk := range ch {
		if chunk.Error != nil {
			s.EventBus.Publish(ctx, event.Event{
				Type:      event.EventLLMError,
				Timestamp: time.Now(),
				SessionID: state.SessionID,
				Payload:   map[string]any{"error": chunk.Error.Error()},
			})
			return chunk.Error
		}

		thinking.WriteString(chunk.Thinking)
		if chunk.ThinkingSignature != "" {
			thinkingSignature = chunk.ThinkingSignature
		}
		content.WriteString(chunk.Content)
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}

		if chunk.InputTokens > 0 {
			inputTokens = chunk.InputTokens
		}
		if chunk.OutputTokens > 0 {
			outputTokens = chunk.OutputTokens
		}
		if chunk.CacheCreationInputTokens > 0 {
			cacheCreationInputTokens = chunk.CacheCreationInputTokens
		}
		if chunk.CacheReadInputTokens > 0 {
			cacheReadInputTokens = chunk.CacheReadInputTokens
		}
		if chunk.PromptCachedTokens > 0 {
			promptCachedTokens = chunk.PromptCachedTokens
		}

		if chunk.Content != "" && state.OnChunk != nil {
			state.OnChunk(chunk.Content)
		}

		if chunk.Done {
			break
		}
	}

	s.Logger.Debug("llm chunk results",
		zap.Int("thinking_len", thinking.Len()),
		zap.Int("content_len", content.Len()),
		zap.Bool("has_signature", thinkingSignature != ""),
		zap.Int("tool_calls", len(toolCalls)),
	)
	msg := types.Message{
		Role:              types.RoleAssistant,
		Content:           content.String(),
		Thinking:          thinking.String(),
		ThinkingSignature: thinkingSignature,
		Timestamp:         time.Now(),
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		state.ToolCalls = toolCalls
	}
	state.Messages = append(state.Messages, msg)
	s.EventBus.Publish(ctx, event.Event{
		Type:      event.EventLLMComplete,
		Timestamp: time.Now(),
		SessionID: state.SessionID,
		Payload: map[string]any{
			"input_tokens":                inputTokens,
			"output_tokens":               outputTokens,
			"total_tokens":                inputTokens + outputTokens,
			"cache_creation_input_tokens": cacheCreationInputTokens,
			"cache_read_input_tokens":     cacheReadInputTokens,
			"prompt_cached_tokens":        promptCachedTokens,
		},
	})

	return nil
}

// ToolStage executes tool calls with timeout and signal handling.
type ToolStage struct {
	ToolRegistry *tool.Registry
	SignalBus    *signal.Bus
	Timeout      time.Duration
	Logger       *zap.Logger
	EventBus     *event.Bus
}

func (s *ToolStage) Name() string { return "tool" }

func (s *ToolStage) Process(ctx context.Context, state *State) error {
	calls := state.ToolCalls
	state.ToolCalls = nil

	if len(calls) == 0 {
		return nil
	}

	state.ToolsCalled = true

	// Subscribe to signal bus once and clean up when done.
	var sigCh <-chan signal.Signal
	if s.SignalBus != nil {
		sigCh = s.SignalBus.Subscribe(state.SessionID)
		defer s.SignalBus.Unsubscribe(state.SessionID, sigCh)
	}

	for _, call := range calls {
		if sigCh != nil {
			select {
			case sig := <-sigCh:
				switch sig {
				case signal.Interrupt:
					s.EventBus.Publish(ctx, event.Event{
						Type:      event.EventTurnInterrupt,
						Timestamp: time.Now(),
						SessionID: state.SessionID,
						Payload:   map[string]any{"tool": call.Name},
					})
					return nil
				case signal.Continue:
					// continue execution
				}
			default:
			}
		}

		s.EventBus.Publish(ctx, event.Event{
			Type:      event.EventToolStart,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
			Payload:   map[string]any{"tool": call.Name, "input": call.Arguments},
		})

		execCtx := ctx
		var cancel func()
		if s.Timeout > 0 {
			execCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		}

		result, err := s.ToolRegistry.Execute(execCtx, call)
		if cancel != nil {
			cancel()
		}

		if err != nil {

			s.EventBus.Publish(ctx, event.Event{
				Type:      event.EventToolError,
				Timestamp: time.Now(),
				SessionID: state.SessionID,
				Payload:   map[string]any{"error": err.Error(), "tool": call.Name, "input": call.Arguments},
			})
			return err
		}

		s.EventBus.Publish(ctx, event.Event{
			Type:      event.EventToolComplete,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
			Payload:   map[string]any{"tool": call.Name, "output": result.Content, "is_error": result.IsError},
		})

		state.Messages = append(state.Messages, types.Message{
			Role:       types.RoleTool,
			ToolCallID: call.ID,
			Content:    result.Content,
		})
		state.ToolResults = append(state.ToolResults, *result)
	}
	return nil
}

// MemoryWriteStage writes the completed turn to memory.
type MemoryWriteStage struct {
	Memory   memory.Memory
	EventBus *event.Bus
}

func (s *MemoryWriteStage) Name() string { return "memory_write" }

func (s *MemoryWriteStage) Process(ctx context.Context, state *State) error {
	if state.ToolsCalled {
		state.ToolsCalled = false
		return nil
	}

	s.EventBus.Publish(ctx, event.Event{
		Type:      event.EventMemoryWriteStart,
		Timestamp: time.Now(),
		SessionID: state.SessionID,
	})

	for _, msg := range state.Messages[len(state.History):] {
		// Skip tool-related messages from completed turns — they add token
		// overhead on multi-turn without benefitting the model (the final
		// assistant text already captures the result).
		if msg.Role == types.RoleTool {
			continue
		}
		if msg.Role == types.RoleAssistant && len(msg.ToolCalls) > 0 {
			continue
		}
		if err := s.Memory.Write(ctx, state.SessionID, msg); err != nil {
			return err
		}
	}
	state.Done = true

	s.EventBus.Publish(ctx, event.Event{
		Type:      event.EventMemoryWriteComplete,
		Timestamp: time.Now(),
		SessionID: state.SessionID,
	})
	return nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
