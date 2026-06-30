package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	appctx "dolphin/internal/context"
	"dolphin/internal/event"
	"dolphin/internal/hook"
	"dolphin/internal/i18n"
	"dolphin/internal/llm"
	"dolphin/internal/llm/proto"
	"dolphin/internal/memory"
	"dolphin/internal/permission"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/skill"
	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/types"
)

type Stage interface {
	Name() string
	Process(ctx context.Context, state *State) error
	Clone() Stage
}

type State struct {
	SessionID        string
	Input            string
	Parts            []types.ContentPart
	TransportContext string
	TransportID      string
	ModelName        string
	History          []types.Message
	Messages         []types.Message
	SystemPrompt     string
	Tools            []types.ToolDef
	ToolCalls        []types.ToolCall
	ToolResults      []types.ToolResult
	Round            int
	Done             bool
	ToolsCalled      bool
	// PersistedIdx tracks the index in Messages up to which content has
	// been durably written by MemoryWriteStage. Checkpoint.Write uses it
	// to flush only the not-yet-persisted tail when a turn fails.
	PersistedIdx int

	OnChunk      func(text string)
	OnThinking   func(text string)
	OnToolCall   func(tc types.ToolCall)
	OnToolResult func(tr types.ToolResult)
}

type Compositor struct {
	initStages      []Stage
	loopStages      []Stage
	maxRounds       int
	turnTimeout     time.Duration // per-round hard cap, 0 = no cap
	idleTimeout     time.Duration // watchdog idle: cancels the whole turn if no Feed within this window, 0 = disabled
	feedMinInterval time.Duration // throttle for Feed calls, 0 = use watchdog default
	checkpoint      *Checkpoint   // optional: flushes partial state on recoverable failures
	eventBus        *event.Bus    // optional: used to publish max-rounds truncation notices
	signalBus       *signal.Bus   // optional: cancels the turn on signal.Interrupt
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

// SetEventBus wires the event bus used to publish turn-level notices
// (currently: max-rounds truncation). Optional — when nil, notices are
// only delivered via the state callbacks.
func (c *Compositor) SetEventBus(b *event.Bus) {
	c.eventBus = b
}

// SetSignalBus wires the signal bus used to watch for Interrupt during a
// turn. When set, Execute subscribes for the turn's session and cancels
// the turn context on signal.Interrupt — so any stage aborts promptly,
// including init stages (e.g. CompactionStage) that do not subscribe on
// their own. Pause/Resume are left to the per-stage handlers in LLMStage
// and ToolStage. Optional — nil disables turn-level interrupt cancellation.
func (c *Compositor) SetSignalBus(b *signal.Bus) {
	c.signalBus = b
}

// SetIdleTimeout configures the watchdog idle window. When > 0, the
// compositor wraps the turn context with a Watchdog that cancels the
// turn if no Feed occurs within d. Stages and tools feed the watchdog
// via agentloop.Feed(ctx) on meaningful progress (LLM chunks, tool
// results). 0 disables the watchdog.
func (c *Compositor) SetIdleTimeout(d time.Duration) {
	c.idleTimeout = d
}

// SetFeedMinInterval sets the throttle window applied to Feed calls
// inside the watchdog. 0 means use the watchdog's default (100ms).
func (c *Compositor) SetFeedMinInterval(d time.Duration) {
	c.feedMinInterval = d
}

// SetCheckpoint wires a Checkpoint used to flush partial state when a
// turn fails with a recoverable error (context cancellation / deadline).
// Pass nil to disable checkpointing (default).
func (c *Compositor) SetCheckpoint(cp *Checkpoint) {
	c.checkpoint = cp
}

// Clone creates a per-worker copy. Shared resources (providers, registries,
// event bus) are copied by pointer — these are concurrency-safe by design.
// Per-turn state (writeIdx, transportCtx) resets to zero values via Clone().
func (c *Compositor) Clone() *Compositor {
	initCopy := make([]Stage, len(c.initStages))
	for i, s := range c.initStages {
		initCopy[i] = s.Clone()
	}
	loopCopy := make([]Stage, len(c.loopStages))
	for i, s := range c.loopStages {
		loopCopy[i] = s.Clone()
	}
	return &Compositor{
		initStages:      initCopy,
		loopStages:      loopCopy,
		maxRounds:       c.maxRounds,
		turnTimeout:     c.turnTimeout,
		idleTimeout:     c.idleTimeout,
		feedMinInterval: c.feedMinInterval,
		checkpoint:      c.checkpoint,
		eventBus:        c.eventBus,
		signalBus:       c.signalBus,
	}
}

func (c *Compositor) Execute(ctx context.Context, state *State) error {
	// The outer context is cancellable but not deadline-bound: total
	// turn duration is bounded by the watchdog (no-feed idle) and the
	// per-round turnTimeout applied inside the loop, not by a single
	// wall-clock budget. This lets multi-round turns run as long as
	// each round stays within budget and continues making progress.
	execCtx, turnCancel := context.WithCancel(ctx)
	defer turnCancel()

	// Interrupt watcher: /session stop (signal.Interrupt) must abort the
	// turn no matter which stage is running. LLMStage and ToolStage
	// subscribe themselves, but init stages like CompactionStage do not —
	// so the compositor subscribes once for the whole turn and cancels
	// execCtx on Interrupt, which propagates ctx.Done() to every stage.
	// Pause/Resume are intentionally not handled here: those are
	// turn-pacing signals consumed by the LLM/tool stage subscribers.
	if c.signalBus != nil && state.SessionID != "" {
		sigCh := c.signalBus.Subscribe(state.SessionID)
		defer c.signalBus.Unsubscribe(state.SessionID, sigCh)
		go func() {
			for {
				select {
				case sig, ok := <-sigCh:
					if !ok {
						return
					}
					if sig == signal.Interrupt {
						turnCancel()
						return
					}
				case <-execCtx.Done():
					return
				}
			}
		}()
	}

	// Idle watchdog: cancels execCtx if no Feed occurs within idleTimeout.
	// Stages feed via agentloop.Feed(ctx) on LLM chunks and tool results.
	wdCtx, wd := New(execCtx, c.idleTimeout)
	if wd != nil && c.feedMinInterval > 0 {
		wd.SetMinFeedInterval(c.feedMinInterval)
	}
	defer wd.Stop()

	for _, stage := range c.initStages {
		if err := stage.Process(wdCtx, state); err != nil {
			c.checkpointOnFailure(wdCtx, state, "init stage "+stage.Name(), err)
			return fmt.Errorf("init stage %s: %w", stage.Name(), err)
		}
		Feed(wdCtx)
	}

	for !state.Done && state.Round < c.maxRounds {
		// Each round gets a fresh hard timeout so long-running tools in
		// previous rounds don't starve subsequent LLM calls.
		roundCtx := wdCtx
		var cancel func()
		if c.turnTimeout > 0 {
			roundCtx, cancel = context.WithTimeout(wdCtx, c.turnTimeout)
		}
		for _, stage := range c.loopStages {
			if err := stage.Process(roundCtx, state); err != nil {
				if cancel != nil {
					cancel()
				}
				c.checkpointOnFailure(wdCtx, state, "loop stage "+stage.Name(), err)
				return fmt.Errorf(i18n.T("agentloop.stage_loop_failed"), stage.Name(), err)
			}
		}
		if cancel != nil {
			cancel()
		}
		Feed(roundCtx)
		state.Round++
	}

	// If the loop exited because of the round cap rather than state.Done,
	// the model was still mid-task — surface that so the user/LLM isn't
	// left with a silently truncated response. Emit a visible notice via
	// the chunk callback and an observability event.
	if !state.Done && state.Round >= c.maxRounds {
		notice := i18n.T("agentloop.max_rounds_reached", c.maxRounds)
		if state.OnChunk != nil {
			state.OnChunk(notice)
		}
		if c.eventBus != nil {
			c.eventBus.Publish(ctx, event.Event{
				Type:      event.EventTurnTruncated,
				Timestamp: time.Now(),
				SessionID: state.SessionID,
				Payload: map[string]any{
					"max_rounds": c.maxRounds,
				},
			})
		}
	}
	return nil
}

// checkpointOnFailure flushes partial state when err is a recoverable
// failure (context cancellation or deadline). Non-recoverable errors
// (permission, tool errors that already wrote their own tool_result)
// skip the checkpoint. Write errors are logged but not returned — the
// turn is already failing, a checkpoint failure shouldn't mask the
// original cause.
//
// The flush runs on a context stripped of the turn's cancellation, so
// that a watchdog-fired ctx doesn't prevent the local memory write
// from completing.
func (c *Compositor) checkpointOnFailure(ctx context.Context, state *State, where string, err error) {
	if c.checkpoint == nil || !IsRecoverable(err) {
		return
	}
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_ = c.checkpoint.Write(flushCtx, state, where+": "+err.Error())
}

// MemoryReadStage reads history from memory.
type MemoryReadStage struct {
	Memory memory.Memory
}

func (s *MemoryReadStage) Name() string { return "memory_read" }

// Clone shares the Memory reference (concurrency-safe). No per-turn state.
func (s *MemoryReadStage) Clone() Stage {
	return &MemoryReadStage{Memory: s.Memory}
}

func (s *MemoryReadStage) Process(ctx context.Context, state *State) error {
	history, err := s.Memory.Read(ctx, state.SessionID)
	if err != nil {
		return err
	}
	state.History = history
	state.Messages = append([]types.Message{}, history...)
	state.Messages = append(state.Messages, types.Message{
		Role:      types.RoleUser,
		Parts:     append([]types.ContentPart{types.TextPart(state.Input)}, state.Parts...),
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
	Workmode         string
	EventBus         *event.Bus
	reg              *appctx.Registry
	transportCtx     string // set per-call in Process
}

// Registry returns the internal section registry, initializing it if needed.
func (s *ContextBuilderStage) Registry() *appctx.Registry {
	s.initRegistry()
	return s.reg
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
	s.reg.Register(&appctx.Agent{
		Workspace:   s.Workspace,
		DefaultText: s.BaseSystemPrompt,
	})
	s.reg.Register(&appctx.Transport{
		ContextFunc: func() string { return s.transportCtx },
	})
	s.reg.Register(&appctx.Workmode{Mode: s.Workmode})
	s.reg.Register(&appctx.Workspace{Dir: s.Workspace})
	s.reg.Register(&appctx.Brain{Reader: s.Brain})
	s.reg.Register(&appctx.Design{Workspace: s.Workspace})
	s.reg.Register(&appctx.Soul{Workspace: s.Workspace})
	s.reg.Register(&appctx.Skills{Store: s.SkillStore})
	s.reg.Register(&appctx.Workflow{})
}

func (s *ContextBuilderStage) Name() string { return "context_builder" }

// Clone shares registries and stores (concurrency-safe).
// Per-turn state intentionally not cloned: transportCtx.
func (s *ContextBuilderStage) Clone() Stage {
	return &ContextBuilderStage{
		BaseSystemPrompt: s.BaseSystemPrompt,
		SkillStore:       s.SkillStore,
		Brain:            s.Brain,
		Workspace:        s.Workspace,
		Workmode:         s.Workmode,
		EventBus:         s.EventBus,
	}
}

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
	SignalBus    *signal.Bus
	Logger       *zap.Logger
	HookReg      *hook.Registry
}

func (s *LLMStage) Clone() Stage {
	// All fields are shared resources (providers, registries, bus). No per-turn state.
	return &LLMStage{
		Provider:     s.Provider,
		Model:        s.Model,
		MaxTokens:    s.MaxTokens,
		MaxRetries:   s.MaxRetries,
		ToolRegistry: s.ToolRegistry,
		EventBus:     s.EventBus,
		SignalBus:    s.SignalBus,
		Logger:       s.Logger,
		HookReg:      s.HookReg,
	}
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

var ErrInterrupted = errors.New("llm: interrupted")
var ErrTurnAborted = errors.New("turn aborted by user")

// isRetryableLLMError reports whether err should be retried. Network and
// context errors are retryable; an HTTPStatusError is retryable only for
// 429/5xx (see HTTPStatusError.IsRetryable). Anything else defaults to
// retryable — a conservative choice that preserves the previous behavior
// for errors the LLM layer wraps generically (e.g. "llm: decode: ...").
func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	var hse *proto.HTTPStatusError
	if errors.As(err, &hse) {
		return hse.IsRetryable()
	}
	// Context cancellation is not something a retry can fix.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

// retryDelay computes the backoff before the next retry attempt. When the
// error carries a Retry-After hint (429 responses often do), honor it
// directly (capped at maxBackoff for safety). Otherwise use exponential
// backoff with jitter: base * 2^attempt, capped, plus up to 25% jitter so
// concurrent workers don't retry in lockstep.
//
// attempt is 0-based (the just-failed attempt index).
func retryDelay(err error, attempt int) time.Duration {
	var hse *proto.HTTPStatusError
	if errors.As(err, &hse) && hse.RetryAfter > 0 {
		if hse.RetryAfter > maxBackoff {
			return maxBackoff
		}
		return hse.RetryAfter
	}
	// Exponential backoff: 500ms, 1s, 2s, 4s, ... capped at 30s.
	d := baseBackoff << attempt
	if d <= 0 || d > maxBackoff {
		d = maxBackoff
	}
	// Jitter: add up to 25% of d to spread out concurrent retries.
	jitter := time.Duration(rand.Int63n(int64(d) / 4)) //nolint:gosec // G404: jitter does not need crypto-grade randomness.
	return d + jitter
}

const (
	baseBackoff = 500 * time.Millisecond
	maxBackoff  = 30 * time.Second
)

func (s *LLMStage) Process(ctx context.Context, state *State) error {
	// Pre-check limits via hook before retry loop.
	if s.HookReg != nil {
		if err := s.HookReg.DispatchCheck(ctx, event.Event{
			Type:      event.EventCheckLLM,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
			Payload:   map[string]any{"model": s.activeModel()},
		}); err != nil {
			return fmt.Errorf("llm limit exceeded: %w", err)
		}
	}

	// Subscribe to interrupt signals for this session.
	var sigCh <-chan signal.Signal
	if s.SignalBus != nil {
		sigCh = s.SignalBus.Subscribe(state.SessionID)
		defer s.SignalBus.Unsubscribe(state.SessionID, sigCh)
	}

	var lastErr error
	for i := 0; i <= s.MaxRetries; i++ {
		// Check for interrupt before each retry.
		select {
		case sig, ok := <-sigCh:
			if ok && sig == signal.Pause {
				if pauseOnSignal(sigCh) != signal.Resume {
					s.EventBus.Publish(ctx, event.Event{
						Type:      event.EventLLMInterrupt,
						Timestamp: time.Now(),
						SessionID: state.SessionID,
					})
					return ErrInterrupted
				}
			} else if ok && sig == signal.Interrupt {
				s.EventBus.Publish(ctx, event.Event{
					Type:      event.EventLLMInterrupt,
					Timestamp: time.Now(),
					SessionID: state.SessionID,
				})
				return ErrInterrupted
			}
		default:
		}

		// Track whether this attempt streamed anything to the user (text,
		// thinking, or tool calls). If it did, a retry would duplicate that
		// output in the UI — the partial response is already committed to
		// the user's view. In that case we suppress the retry and surface
		// the error instead. Wrapping the callbacks (rather than changing
		// tryComplete's signature) keeps emission detection local to the
		// retry policy that owns it.
		emitted := false
		mark := func() { emitted = true }
		origChunk, origThinking, origToolCall := state.OnChunk, state.OnThinking, state.OnToolCall
		if origChunk != nil {
			state.OnChunk = func(text string) {
				if text != "" {
					mark()
				}
				origChunk(text)
			}
		}
		if origThinking != nil {
			state.OnThinking = func(text string) {
				if text != "" {
					mark()
				}
				origThinking(text)
			}
		}
		if origToolCall != nil {
			state.OnToolCall = func(tc types.ToolCall) { mark(); origToolCall(tc) }
		}

		err := s.tryComplete(ctx, state, sigCh)

		// Restore the original callbacks before any return path.
		state.OnChunk, state.OnThinking, state.OnToolCall = origChunk, origThinking, origToolCall

		if err == nil {
			return nil
		}
		if errors.Is(err, ErrInterrupted) {
			return err
		}
		if emitted {
			// Content was already streamed — retrying would show the user
			// duplicated output. Publish the error and stop retrying.
			s.EventBus.Publish(ctx, event.Event{
				Type:      event.EventLLMError,
				Timestamp: time.Now(),
				SessionID: state.SessionID,
				Payload: map[string]any{
					"error":            err.Error(),
					"retry_suppressed": true,
					"attempt":          i,
				},
			})
			return err
		}
		// Classify the error before retrying. A non-retryable HTTP status
		// (4xx other than 429) means retrying is pointless — surface it now
		// instead of burning the retry budget.
		if !isRetryableLLMError(err) {
			s.EventBus.Publish(ctx, event.Event{
				Type:      event.EventLLMError,
				Timestamp: time.Now(),
				SessionID: state.SessionID,
				Payload: map[string]any{
					"error":         err.Error(),
					"non_retryable": true,
					"attempt":       i,
				},
			})
			s.Logger.Warn(fmt.Sprintf(i18n.T("agentloop.llm_non_retryable"), err))
			return err
		}
		// Back off before the next attempt. Honor Retry-After when the
		// provider gave one; otherwise exponential backoff with jitter.
		// This is what stops a 429 from turning into an immediate retry
		// storm that deepens the rate limit.
		delay := retryDelay(err, i)
		s.Logger.Info(fmt.Sprintf(i18n.T("agentloop.llm_backoff"), i+1, s.MaxRetries+1, delay, err))
		if backoffSleep(sigCh, delay) {
			s.EventBus.Publish(ctx, event.Event{
				Type:      event.EventLLMInterrupt,
				Timestamp: time.Now(),
				SessionID: state.SessionID,
			})
			return ErrInterrupted
		}
		lastErr = err
		s.EventBus.Publish(ctx, event.Event{
			Type:      event.EventLLMRetry,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
			Payload:   map[string]any{"error": err.Error(), "attempt": i, "backoff": delay.String()},
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

func (s *LLMStage) tryComplete(ctx context.Context, state *State, sigCh <-chan signal.Signal) error {
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

	state.ModelName = s.activeModel()

	s.EventBus.Publish(ctx, event.Event{
		Type:      event.EventLLMStart,
		Timestamp: time.Now(),
		SessionID: state.SessionID,
		Payload: map[string]any{
			"model": state.ModelName,
			"tools": toolNames,
		},
	})

	// Derive HTTP timeout from context deadline so the HTTP client has a
	// direct timeout even if context cancellation propagation lags.
	httpTimeout := 120 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d > 0 && d < httpTimeout {
			httpTimeout = d
		}
	}

	ch, err := s.Provider.CompleteStream(ctx, llm.LLMRequest{
		Messages:  msgs,
		System:    state.SystemPrompt,
		Model:     s.activeModel(),
		MaxTokens: s.MaxTokens,
		Tools:     tools,
		Stream:    true,
		Timeout:   httpTimeout,
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
	var promptCacheHitTokens, promptCacheMissTokens int

	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return nil
			}
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
			if chunk.Thinking != "" && state.OnThinking != nil {
				state.OnThinking(chunk.Thinking)
			}
			if chunk.ThinkingSignature != "" {
				thinkingSignature = chunk.ThinkingSignature
			}
			content.WriteString(chunk.Content)
			if len(chunk.ToolCalls) > 0 {
				toolCalls = append(toolCalls, chunk.ToolCalls...)
				if state.OnToolCall != nil {
					for _, tc := range chunk.ToolCalls {
						state.OnToolCall(tc)
					}
				}
			}

			// Any non-empty chunk is a heartbeat: feed the watchdog so a
			// slow-but-healthy stream doesn't get cancelled by idle.
			if chunk.Content != "" || chunk.Thinking != "" || len(chunk.ToolCalls) > 0 {
				Feed(ctx)
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
			if chunk.PromptCacheHitTokens > 0 {
				promptCacheHitTokens = chunk.PromptCacheHitTokens
			}
			if chunk.PromptCacheMissTokens > 0 {
				promptCacheMissTokens = chunk.PromptCacheMissTokens
			}

			if chunk.Content != "" && state.OnChunk != nil {
				state.OnChunk(chunk.Content)
			}

			if chunk.Done {
				goto done
			}

		case sig, ok := <-sigCh:
			if ok && sig == signal.Pause {
				if pauseOnSignal(sigCh) != signal.Resume {
					s.EventBus.Publish(ctx, event.Event{
						Type:      event.EventLLMInterrupt,
						Timestamp: time.Now(),
						SessionID: state.SessionID,
					})
					return ErrInterrupted
				}
			} else if ok && sig == signal.Interrupt {
				s.EventBus.Publish(ctx, event.Event{
					Type:      event.EventLLMInterrupt,
					Timestamp: time.Now(),
					SessionID: state.SessionID,
				})
				return ErrInterrupted
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}

done:

	s.Logger.Debug("llm chunk results",
		zap.Int("thinking_len", thinking.Len()),
		zap.Int("content_len", content.Len()),
		zap.Bool("has_signature", thinkingSignature != ""),
		zap.Int("tool_calls", len(toolCalls)),
	)
	msg := types.Message{
		Role:              types.RoleAssistant,
		Parts:             []types.ContentPart{types.TextPart(content.String())},
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
			"model":                       s.activeModel(),
			"input_tokens":                inputTokens,
			"output_tokens":               outputTokens,
			"total_tokens":                inputTokens + outputTokens,
			"cache_creation_input_tokens": cacheCreationInputTokens,
			"cache_read_input_tokens":     cacheReadInputTokens,
			"prompt_cached_tokens":        promptCachedTokens,
			"prompt_cache_hit_tokens":     promptCacheHitTokens,
			"prompt_cache_miss_tokens":    promptCacheMissTokens,
		},
	})

	return nil
}

// ToolStage executes tool calls with timeout and signal handling.
type ToolStage struct {
	ToolRegistry    *tool.Registry
	SignalBus       *signal.Bus
	Timeout         time.Duration
	Logger          *zap.Logger
	HookReg         *hook.Registry
	EventBus        *event.Bus
	PermissionStore *permission.Store
	GetTransport    func(id string) transport.IO
	Workmode        string
	MaxParallel     int
}

func (s *ToolStage) Clone() Stage {
	// All fields are shared resources. No per-turn state.
	return &ToolStage{
		ToolRegistry:    s.ToolRegistry,
		SignalBus:       s.SignalBus,
		Timeout:         s.Timeout,
		Logger:          s.Logger,
		HookReg:         s.HookReg,
		EventBus:        s.EventBus,
		PermissionStore: s.PermissionStore,
		GetTransport:    s.GetTransport,
		Workmode:        s.Workmode,
	}
}

func (s *ToolStage) Name() string { return "tool" }

func (s *ToolStage) Process(ctx context.Context, state *State) error {
	calls := state.ToolCalls
	state.ToolCalls = nil

	if len(calls) == 0 {
		return nil
	}

	state.ToolsCalled = true

	var sigCh <-chan signal.Signal
	if s.SignalBus != nil {
		sigCh = s.SignalBus.Subscribe(state.SessionID)
		defer s.SignalBus.Unsubscribe(state.SessionID, sigCh)
	}

	// Fast path: serial execution when parallelism is disabled.
	if s.MaxParallel <= 1 {
		for i, call := range calls {
			// Interrupt check before each call.
			if sigCh != nil {
				select {
				case sig := <-sigCh:
					if sig == signal.Pause {
						sig = pauseOnSignal(sigCh)
					}
					if sig == signal.Interrupt {
						s.EventBus.Publish(ctx, event.Event{
							Type:      event.EventTurnInterrupt,
							Timestamp: time.Now(),
							SessionID: state.SessionID,
							Payload:   map[string]any{"tool": call.Name},
						})
						// Append error results for remaining calls so every
						// tool_use has a matching tool_result.
						for _, c := range calls[i:] {
							msg, tr := s.interruptedToolResult(c)
							state.Messages = append(state.Messages, msg)
							state.ToolResults = append(state.ToolResults, tr)
						}
						return nil
					}
				default:
				}
			}

			if err := s.checkPermission(ctx, state, call); err != nil {
				msg, tr := s.deniedToolResult(call, err)
				s.EventBus.Publish(ctx, event.Event{
					Type:      event.EventToolError,
					Timestamp: time.Now(),
					SessionID: state.SessionID,
					Payload:   map[string]any{"error": err.Error(), "tool": call.Name, "input": call.Arguments},
				})
				state.Messages = append(state.Messages, msg)
				state.ToolResults = append(state.ToolResults, tr)
				if errors.Is(err, ErrTurnAborted) {
					// Abort the entire turn: append interrupted results for
					// remaining calls so every tool_use has a matching tool_result.
					for _, c := range calls[i+1:] {
						msg, tr := s.interruptedToolResult(c)
						state.Messages = append(state.Messages, msg)
						state.ToolResults = append(state.ToolResults, tr)
					}
					state.Done = true
					return nil
				}
				continue
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
			Feed(ctx)

			if err != nil {
				s.EventBus.Publish(ctx, event.Event{
					Type:      event.EventToolError,
					Timestamp: time.Now(),
					SessionID: state.SessionID,
					Payload:   map[string]any{"error": err.Error(), "tool": call.Name, "input": call.Arguments},
				})
				// Surface the real error to the LLM as an IsError tool_result
				// and continue to the next call. The agent loop then re-enters
				// the LLM stage, letting the model react to the failure rather
				// than aborting the turn. Only context cancellation/deadline
				// aborts the turn: those are recoverable failures that should
				// trigger a checkpoint flush, and the round's context is
				// already gone so continuing would be pointless.
				msg, tr := s.failedToolResult(call, err)
				state.Messages = append(state.Messages, msg)
				state.ToolResults = append(state.ToolResults, tr)
				if state.OnToolResult != nil {
					state.OnToolResult(tr)
				}
				if IsRecoverable(err) {
					return err
				}
				continue
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
				Parts:      []types.ContentPart{types.TextPart(result.Content)},
				IsError:    result.IsError,
				Timestamp:  time.Now(),
			})
			state.ToolResults = append(state.ToolResults, *result)
			if state.OnToolResult != nil {
				state.OnToolResult(*result)
			}
			Feed(ctx)
		}
		return nil
	}

	// Parallel path: permission checks run serially (may prompt), then
	// approved tools execute concurrently bounded by MaxParallel.
	return s.processParallel(ctx, state, calls, sigCh)
}

// deniedToolResult builds the error message and ToolResult for a
// permission-denied tool call.
func (s *ToolStage) deniedToolResult(call types.ToolCall, err error) (types.Message, types.ToolResult) {
	msg := types.Message{
		Role:       types.RoleTool,
		ToolCallID: call.ID,
		Parts:      []types.ContentPart{types.TextPart(fmt.Sprintf(i18n.T("agentloop.denied_message"), err.Error()))}, IsError: true,
		Timestamp: time.Now(),
	}
	tr := types.ToolResult{
		ToolCallID: call.ID,
		Content:    err.Error(),
		IsError:    true,
	}
	return msg, tr
}

// interruptedToolResult builds an error result for a tool call that was
// interrupted before execution. Ensures every tool_use has a matching
// tool_result in the message history.
func (s *ToolStage) interruptedToolResult(call types.ToolCall) (types.Message, types.ToolResult) {
	content := i18n.T("agentloop.tool_interrupted", call.Name)
	msg := types.Message{
		Role:       types.RoleTool,
		ToolCallID: call.ID,
		Parts:      []types.ContentPart{types.TextPart(content)},
		IsError:    true,
		Timestamp:  time.Now(),
	}
	tr := types.ToolResult{
		ToolCallID: call.ID,
		Content:    content,
		IsError:    true,
	}
	return msg, tr
}

// failedToolResult builds a tool_result carrying the real execution error
// returned by the tool. Unlike interruptedToolResult (which is for
// pre-execution interrupts and hides the cause), this surfaces the error
// message to the LLM so it can reason about the failure and recover on
// the next round — e.g. retry with different arguments, switch tools, or
// report the problem to the user.
func (s *ToolStage) failedToolResult(call types.ToolCall, err error) (types.Message, types.ToolResult) {
	content := fmt.Sprintf(i18n.T("agentloop.tool_failed"), call.Name, err.Error())
	msg := types.Message{
		Role:       types.RoleTool,
		ToolCallID: call.ID,
		Parts:      []types.ContentPart{types.TextPart(content)},
		IsError:    true,
		Timestamp:  time.Now(),
	}
	tr := types.ToolResult{
		ToolCallID: call.ID,
		Content:    content,
		IsError:    true,
	}
	return msg, tr
}

// processParallel executes tool calls concurrently, bounded by MaxParallel.
// Permission checks already passed for all calls.
func (s *ToolStage) processParallel(ctx context.Context, state *State, calls []types.ToolCall, sigCh <-chan signal.Signal) error {
	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	sem := make(chan struct{}, s.MaxParallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	completed := make(map[string]bool)

	// Collect denied calls and passing calls.
	type pending struct {
		call types.ToolCall
	}
	var pendingCalls []pending

	for i, call := range calls {
		if err := s.checkPermission(ctx, state, call); err != nil {
			msg, tr := s.deniedToolResult(call, err)
			s.EventBus.Publish(ctx, event.Event{
				Type:      event.EventToolError,
				Timestamp: time.Now(),
				SessionID: state.SessionID,
				Payload:   map[string]any{"error": err.Error(), "tool": call.Name, "input": call.Arguments},
			})
			state.Messages = append(state.Messages, msg)
			state.ToolResults = append(state.ToolResults, tr)
			if errors.Is(err, ErrTurnAborted) {
				// Abort the entire turn: append interrupted results for
				// remaining calls so every tool_use has a matching tool_result.
				for _, c := range calls[i+1:] {
					msg, tr := s.interruptedToolResult(c)
					state.Messages = append(state.Messages, msg)
					state.ToolResults = append(state.ToolResults, tr)
				}
				state.Done = true
				return nil
			}
			continue
		}
		pendingCalls = append(pendingCalls, pending{call})
	}

	// Watch for interrupt while tools are running.
	if sigCh != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case sig := <-sigCh:
					if sig == signal.Pause {
						sig = pauseOnSignal(sigCh)
					}
					if sig == signal.Interrupt {
						s.EventBus.Publish(ctx, event.Event{
							Type:      event.EventTurnInterrupt,
							Timestamp: time.Now(),
							SessionID: state.SessionID,
						})
						execCancel()
						return
					}
				case <-execCtx.Done():
					return
				}
			}
		}()
	}

	for _, p := range pendingCalls {
		call := p.call

		// Don't start new work if already cancelled.
		if execCtx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(tc types.ToolCall) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-execCtx.Done():
				return
			}
			defer func() { <-sem }()

			toolCtx := execCtx
			var cancel func()
			if s.Timeout > 0 {
				toolCtx, cancel = context.WithTimeout(execCtx, s.Timeout)
				defer cancel()
			}

			s.EventBus.Publish(ctx, event.Event{
				Type:      event.EventToolStart,
				Timestamp: time.Now(),
				SessionID: state.SessionID,
				Payload:   map[string]any{"tool": tc.Name, "input": tc.Arguments},
			})

			result, err := s.ToolRegistry.Execute(toolCtx, tc)
			Feed(ctx)

			mu.Lock()
			if err != nil {
				s.EventBus.Publish(ctx, event.Event{
					Type:      event.EventToolError,
					Timestamp: time.Now(),
					SessionID: state.SessionID,
					Payload:   map[string]any{"error": err.Error(), "tool": tc.Name, "input": tc.Arguments},
				})
				// Surface the real error to the LLM. Recoverable failures
				// (context cancellation / deadline — typically a round
				// timeout or watchdog) abort the batch: the exec context is
				// already gone, so sibling tools can't make progress anyway,
				// and the turn should checkpoint. Tool-specific errors do
				// NOT cancel siblings — let them finish so the LLM gets the
				// full set of results and can recover on the next round.
				if IsRecoverable(err) {
					if firstErr == nil {
						firstErr = err
						execCancel()
					}
				}
				msg, tr := s.failedToolResult(tc, err)
				state.Messages = append(state.Messages, msg)
				state.ToolResults = append(state.ToolResults, tr)
				completed[tc.ID] = true
				// Invoke the callback under the lock so OnToolResult order
				// matches the order results are appended (see success branch).
				if state.OnToolResult != nil {
					state.OnToolResult(tr)
				}
				mu.Unlock()
				return
			}

			s.EventBus.Publish(ctx, event.Event{
				Type:      event.EventToolComplete,
				Timestamp: time.Now(),
				SessionID: state.SessionID,
				Payload:   map[string]any{"tool": tc.Name, "output": result.Content, "is_error": result.IsError},
			})

			state.Messages = append(state.Messages, types.Message{
				Role:       types.RoleTool,
				ToolCallID: tc.ID,
				Parts:      []types.ContentPart{types.TextPart(result.Content)}, IsError: result.IsError,
				Timestamp: time.Now(),
			})
			state.ToolResults = append(state.ToolResults, *result)
			completed[tc.ID] = true
			// Invoke the callback under the lock so the order of
			// OnToolResult calls matches the order results are appended to
			// state.Messages/ToolResults. The callback forwards to the
			// transport/UI and does not re-enter this stage, so holding mu
			// briefly is safe and keeps the event stream consistent.
			if state.OnToolResult != nil {
				state.OnToolResult(*result)
			}
			mu.Unlock()
		}(call)
	}

	wg.Wait()

	// Ensure every tool_use has a matching tool_result, even for calls
	// that were skipped due to cancellation or interrupt.
	for _, p := range pendingCalls {
		if !completed[p.call.ID] {
			msg, tr := s.interruptedToolResult(p.call)
			state.Messages = append(state.Messages, msg)
			state.ToolResults = append(state.ToolResults, tr)
		}
	}

	return firstErr
}

// checkPermission evaluates whether a tool call is allowed under the current
// permission rules and work mode. Returns nil to allow, or an error describing
// why the call was denied.
func (s *ToolStage) checkPermission(ctx context.Context, state *State, call types.ToolCall) error {
	// request_permission and emit_event are always allowed — they are
	// meta-tools for requesting user permission and emitting events,
	// and must not require permission themselves.
	if call.Name == "request_permission" || call.Name == "emit_event" {
		return nil
	}

	if s.PermissionStore == nil {
		return nil
	}

	argsRaw := json.RawMessage(call.Arguments)
	result := s.PermissionStore.Check(call.Name, argsRaw)

	switch result {
	case permission.Deny:
		return fmt.Errorf(i18n.T("agentloop.tool_denied"), call.Name)
	case permission.Allow:
		return nil
	case permission.NoMatch:
		if isSafeShellCommand(call.Name, call.Arguments) {
			return nil
		}
		if s.Workmode == "yolo" {
			return nil
		}
		// Default mode: try to ask the user.
		if s.GetTransport == nil {
			return fmt.Errorf(i18n.T("agentloop.tool_requires_permission"), call.Name)
		}
		tio := s.GetTransport(state.TransportID)
		if tio == nil {
			return fmt.Errorf(i18n.T("agentloop.tool_requires_permission"), call.Name)
		}

		prompt := fmt.Sprintf(i18n.T("agentloop.tool_permission_request"), call.Name, call.Arguments)
		permResult, err := requestPermissionFeeding(ctx, tio, prompt)
		if err != nil {
			return fmt.Errorf(i18n.T("agentloop.tool_permission_failed"), call.Name, err)
		}

		switch permResult { //nolint:exhaustive // PermissionDenied falls through to default (denied).
		case transport.PermissionOnce:
			return nil
		case transport.PermissionAlways:
			if err := s.PermissionStore.AddAllowTool(call.Name); err != nil {
				s.Logger.Warn("failed to save permission rule", zap.Error(err))
			}
			return nil
		case transport.PermissionAbort:
			return fmt.Errorf("%w: %s", ErrTurnAborted, call.Name)
		default:
			return fmt.Errorf(i18n.T("agentloop.tool_denied_by_user"), call.Name)
		}
	}
	return nil
}

// permFeedInterval is how often requestPermissionFeeding strokes the idle
// watchdog while waiting for the user to answer a permission prompt. The
// wait is user-bound (not a stuck LLM), so we keep the watchdog alive until
// the user responds — otherwise a slow reader can exceed llm_idle_timeout
// (default 60s) and get the turn cancelled mid-prompt. A var (not const)
// so tests can shrink it.
var permFeedInterval = 5 * time.Second

// requestPermissionFeeding wraps transport.RequestPermission, feeding the
// idle watchdog on a ticker until the user answers (or the context is
// cancelled). Feed is nil-safe once the watchdog has fired, so a late tick
// after cancellation is harmless.

// safeShellCommands is the set of commands considered harmless when invoked as
// the first token (before any flags or args). They emit no side-effects and
// are auto-allowed without prompting.
//
// Policy: only genuinely read-only, side-effect-free commands belong here.
// Commands that can mutate the filesystem (sed -i, tee, find -delete/-exec,
// xargs <cmd>) or execute arbitrary code (awk via system(), find -exec) are
// intentionally excluded — even though their common forms are read-only, a
// flag-level allowlist cannot reliably distinguish safe from destructive
// invocations, so they must go through the normal permission flow.
var safeShellCommands = map[string]bool{
	"ls": true, "pwd": true, "cat": true, "echo": true, "head": true,
	"tail": true, "wc": true, "which": true, "date": true, "whoami": true,
	"hostname": true, "uname": true, "id": true, "env": true, "printenv": true,
	"ps": true, "df": true, "du": true, "free": true, "uptime": true,
	"dirname": true, "basename": true, "realpath": true, "readlink": true,
	"sort": true, "uniq": true, "tr": true, "cut": true,
	"grep": true,
	"file": true, "stat": true, "tree": true, "diff": true, "man": true,
	"pgrep": true, "history": true, "type": true, "command": true,
}

// isSafeShellCommand returns true if the call is a shell command whose first
// token is on the safe list. Compound commands (&&, ||, ;, |, >, <) and
// commands with flags that write/modify are NOT automatically safe.
func isSafeShellCommand(toolName, args string) bool {
	if toolName != "shell" {
		return false
	}
	// Parse first token from the command string.
	raw := args
	// Handle JSON {"command":"cmd ..."}
	if len(raw) > 0 && raw[0] == '{' {
		var m map[string]string
		_ = json.Unmarshal([]byte(raw), &m)
		if m != nil {
			raw = m["command"]
		}
	}
	if raw == "" {
		return false
	}
	// Split on whitespace; reject commands with shell metacharacters
	// that could chain unsafe operations (&&, ||, ;, |, >, <, $, `, etc.).
	for _, ch := range raw {
		switch ch {
		case '&', '|', ';', '>', '<', '$', '`', '(', ')':
			return false
		}
	}
	first := strings.Fields(raw)
	if len(first) == 0 {
		return false
	}
	return safeShellCommands[first[0]]
}

func requestPermissionFeeding(ctx context.Context, tio transport.IO, prompt string) (transport.PermissionResult, error) {
	Feed(ctx) // immediate feed so the prompt display itself doesn't trip the watchdog
	// Read the interval once here (in the caller's goroutine) rather than
	// inside the spawned goroutine below: permFeedInterval is a test-mutable
	// global, and reading it from a detached goroutine races with a later
	// test's write to it (no happens-before edge between the two). Capturing
	// the value keeps the global access on the synchronous call path.
	interval := permFeedInterval
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				Feed(ctx)
			}
		}
	}()
	result, err := tio.RequestPermission(ctx, prompt)
	close(done)
	return result, err
}

// MemoryWriteStage writes the completed turn to memory.
type MemoryWriteStage struct {
	Memory   memory.Memory
	EventBus *event.Bus
	writeIdx int // tracks position in state.Messages already persisted, avoids re-writing across rounds
}

func (s *MemoryWriteStage) Clone() Stage {
	// Memory and EventBus are shared (concurrency-safe).
	// Per-turn state intentionally not cloned: writeIdx (resets to 0).
	return &MemoryWriteStage{
		Memory:   s.Memory,
		EventBus: s.EventBus,
	}
}

func (s *MemoryWriteStage) Name() string { return "memory_write" }

func (s *MemoryWriteStage) Process(ctx context.Context, state *State) error {
	s.EventBus.Publish(ctx, event.Event{
		Type:      event.EventMemoryWriteStart,
		Timestamp: time.Now(),
		SessionID: state.SessionID,
	})

	// On the first round, seed writeIdx from the history boundary.
	// On subsequent rounds (tool call loops), only write messages that
	// were added since the last round, avoiding duplicates in data.memory.
	if s.writeIdx == 0 {
		s.writeIdx = len(state.History)
	}
	for _, msg := range state.Messages[s.writeIdx:] {
		if err := s.Memory.Write(ctx, state.SessionID, msg); err != nil {
			return err
		}
	}
	s.writeIdx = len(state.Messages)
	state.PersistedIdx = s.writeIdx

	if state.ToolsCalled {
		state.ToolsCalled = false
		return nil
	}

	state.Done = true
	s.writeIdx = 0

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

// CompactionStage summarizes the oldest messages when the conversation
// approaches the model's context window, replacing them with a single
// summary message kept at the head of Messages. It runs once per turn
// (an init stage), after MemoryReadStage has assembled state.Messages
// from history plus the new user input.
//
// Design:
//   - Synchronous: compaction finishes before the main LLM call so the
//     current request already uses a trimmed context.
//   - Durable: the compacted [summary + tail] list is written back via
//     Memory.Replace so subsequent turns (and restarts) read the
//     compacted history without re-summarizing.
//   - Tail-preserving: the most recent keepRounds rounds are kept
//     verbatim; only older messages are summarized.
//
// The summary is emitted as a user-role message flagged IsSummary so the
// provider adapters send it unchanged while application code can tell it
// apart from real user input.
type CompactionStage struct {
	Provider     llm.Provider // used to generate the summary (typically the Manager)
	Memory       memory.Memory
	Model        string // optional: model for the summary call; empty = active
	MaxTokens    int    // summary output cap (e.g. 512)
	MaxThreshold int    // estimated-token trigger threshold
	KeepRounds   int    // recent rounds preserved verbatim (1 round = user+assistant)
	TokenRatio   int    // runes per estimated token
	EventBus     *event.Bus
	Logger       *zap.Logger
	// SessionMgr, when set, supplies the previous turn's real input-token
	// count (last_input_tokens, as reported by the provider). Compaction
	// triggers when that exceeds MaxThreshold — far more accurate than the
	// rune-based estimate, which misses system prompts and tool schemas.
	// Falls back to estimateTokens when nil or unset.
	SessionMgr *session.Manager
	// SignalBus, when set, lets the summary stream honor Pause (block until
	// Resume) and Interrupt (abort) so /session pause and ESC take effect
	// during compaction instead of being silently dropped.
	SignalBus *signal.Bus
}

func (s *CompactionStage) Name() string { return "compaction" }

// Clone shares all fields (providers/memory/bus are concurrency-safe).
func (s *CompactionStage) Clone() Stage {
	return &CompactionStage{
		Provider:     s.Provider,
		Memory:       s.Memory,
		Model:        s.Model,
		MaxTokens:    s.MaxTokens,
		MaxThreshold: s.MaxThreshold,
		KeepRounds:   s.KeepRounds,
		TokenRatio:   s.TokenRatio,
		EventBus:     s.EventBus,
		Logger:       s.Logger,
		SessionMgr:   s.SessionMgr,
		SignalBus:    s.SignalBus,
	}
}

func (s *CompactionStage) activeModel() string {
	if s.Model != "" {
		return s.Model
	}
	if a, ok := s.Provider.(interface{ ActiveModel() string }); ok {
		return a.ActiveModel()
	}
	return ""
}

// estimateTokens gives a rough token count for a message slice. It uses
// rune length divided by TokenRatio — imprecise but sufficient for
// triggering compaction with a safety margin. Thinking and tool content
// are included since they consume context too.
func (s *CompactionStage) estimateTokens(msgs []types.Message) int {
	ratio := s.TokenRatio
	if ratio <= 0 {
		ratio = 4
	}
	var runes int
	for _, m := range msgs {
		runes += len([]rune(m.Text()))
		// Images carry meaningful token cost; account for it so compaction
		// triggers earlier when attachments are present.
		if m.HasImage() {
			runes += 1000 * len(m.ImageFilenames())
		}
		runes += len([]rune(m.Thinking))
		for _, tc := range m.ToolCalls {
			runes += len([]rune(tc.Arguments))
		}
	}
	return runes / ratio
}

// estimateTokensReal returns the best available estimate of the current
// request's input-token size for threshold comparison. It prefers the
// previous turn's real input-token count (last_input_tokens, reported by
// the provider and accumulated in the session), since that captures the
// system prompt, tool schemas, and full history — none of which the
// rune-based estimate sees. The current turn's input is at least as large
// as the previous one, so using last_input_tokens as a floor is safe.
// estimateTokens is taken as a secondary floor so a fresh session with no
// prior LLM call still compacts on the rune signal.
func (s *CompactionStage) estimateTokensReal(state *State) int {
	est := s.estimateTokens(state.Messages)
	if s.SessionMgr == nil || state.SessionID == "" {
		return est
	}
	sess := s.SessionMgr.Get(state.SessionID)
	if sess == nil {
		return est
	}
	if v, ok := sess.Get("last_input_tokens").(int); ok && v > est {
		return v
	}
	return est
}

func (s *CompactionStage) Process(ctx context.Context, state *State) error {
	if s == nil || s.Provider == nil || s.Memory == nil {
		return nil
	}
	if s.MaxThreshold <= 0 || s.KeepRounds <= 0 {
		return nil
	}
	// Need at least keepRounds*2 + 1 messages (history + new input) for
	// compaction to make sense; otherwise there's nothing old to summarize.
	minNeeded := s.KeepRounds*2 + 1
	if len(state.Messages) < minNeeded {
		return nil
	}
	if s.estimateTokensReal(state) < s.MaxThreshold {
		return nil
	}

	// Subscribe for the session so the summary stream can honor Pause
	// (block until Resume) and Interrupt (abort). nil when no signal bus —
	// summarize treats a nil sigCh as "no signals".
	var sigCh <-chan signal.Signal
	if s.SignalBus != nil && state.SessionID != "" {
		sigCh = s.SignalBus.Subscribe(state.SessionID)
		defer s.SignalBus.Unsubscribe(state.SessionID, sigCh)
	}

	compacted, err := s.Compact(ctx, state.Messages, sigCh)
	if err != nil {
		// Compaction is best-effort: on failure, fall back to the
		// un-compacted messages so the turn can still proceed. Log and
		// continue rather than blocking the conversation.
		if s.Logger != nil {
			s.Logger.Warn("compaction failed, proceeding with full context",
				zap.Error(err))
		}
		return nil
	}

	// state.Messages is [history... + current user input]. After
	// compaction it becomes [summary + tail... + current user input].
	// The current user input is the last message and is always kept.
	state.Messages = compacted
	// Re-align state.History so MemoryWriteStage's writeIdx boundary
	// (len(state.History)) reflects the compacted list. Otherwise the
	// write stage would re-append already-compacted history.
	// History is everything except the current user input (last msg).
	state.History = compacted[:len(compacted)-1]

	// Persist the compacted history (excluding the not-yet-sent current
	// user input) so future turns read the compacted version.
	if err := s.Memory.Replace(ctx, state.SessionID, state.History); err != nil {
		if s.Logger != nil {
			s.Logger.Warn("compaction persist failed",
				zap.Error(err))
		}
	}

	if s.EventBus != nil {
		s.EventBus.Publish(ctx, event.Event{
			Type:      event.EventCompaction,
			Timestamp: time.Now(),
			SessionID: state.SessionID,
			Payload: map[string]any{
				"summary_tokens": s.estimateTokens([]types.Message{compacted[0]}),
				"kept_messages":  len(compacted),
			},
		})
	}
	return nil
}

// compact splits msgs into [old...] and [tail...] where tail holds the
// most recent keepRounds rounds (plus the trailing current user input),
// summarizes old into a single IsSummary message, and returns
// [summary + tail]. It never orphans a tool_result: the split point
// walks back past RoleTool messages so every tool_result in tail has
// its matching tool_use.
func (s *CompactionStage) Compact(ctx context.Context, msgs []types.Message, sigCh <-chan signal.Signal) ([]types.Message, error) {
	// tail keeps keepRounds rounds (user+assistant pairs) from the end,
	// but the very last message (current user input) is always part of
	// tail regardless. Work back from the end counting rounds.
	keepMsgs := s.KeepRounds * 2
	// Reserve room for the trailing current input (1 message).
	split := len(msgs) - keepMsgs - 1
	if split < 0 {
		split = 0
	}
	// Don't orphan tool_results: walk the split point back while it
	// lands on a tool message, so tail begins at a user/assistant msg.
	for split > 0 && msgs[split].Role == types.RoleTool {
		split--
	}
	if split <= 0 {
		// Nothing old enough to summarize; leave as-is.
		return msgs, nil
	}

	oldMsgs := msgs[:split]
	tail := msgs[split:]

	summaryText, err := s.summarize(ctx, oldMsgs, sigCh)
	if err != nil {
		return nil, err
	}

	summary := types.Message{
		Role:      types.RoleUser,
		Parts:     []types.ContentPart{types.TextPart("[Conversation summary of earlier turns]\n\n" + summaryText)},
		IsSummary: true,
		// Timestamp earlier than the tail so ordering is unambiguous.
		Timestamp: tail[0].Timestamp,
	}

	out := make([]types.Message, 0, len(tail)+1)
	out = append(out, summary)
	out = append(out, tail...)
	return out, nil
}

// ManualCompact reads the session history, compacts it, and persists the
// result. It is used by /session compaction (and /compaction alias) for
// on-demand compaction. Skips the token threshold and always compacts.
// Temporarily reduces keepRounds if needed so there is always at least
// one round of old messages to summarize.
func (s *CompactionStage) ManualCompact(ctx context.Context, sessionID string) (string, error) {
	if s == nil || s.Provider == nil || s.Memory == nil {
		return "", fmt.Errorf("compaction: not configured")
	}
	if s.MaxThreshold <= 0 || s.KeepRounds <= 0 {
		return "", fmt.Errorf("compaction: disabled (max_tokens or keep_rounds is 0)")
	}

	msgs, err := s.Memory.Read(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("compaction: read history: %w", err)
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("compaction: session has no messages")
	}

	// Strip any existing IsSummary message at the head.
	filtered := msgs[:0]
	for _, m := range msgs {
		if !m.IsSummary {
			filtered = append(filtered, m)
		}
	}
	msgs = filtered
	before := len(msgs)

	// Temporarily reduce keepRounds if it would keep everything so
	// there is always at least one round to summarize.
	savedKeep := s.KeepRounds
	needed := s.KeepRounds*2 + 1
	if needed >= len(msgs) {
		s.KeepRounds = len(msgs) / 2
		if s.KeepRounds < 1 {
			s.KeepRounds = 1
		}
	}

	// Compact expects a trailing user-input marker.
	marker := types.Message{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart("[manual compaction]")}, Timestamp: time.Now()}
	compacted, err := s.Compact(ctx, append(msgs, marker), nil)
	s.KeepRounds = savedKeep
	if err != nil {
		return "", fmt.Errorf("compaction: %w", err)
	}

	// Strip the synthetic marker — always the last message.
	if len(compacted) > 0 && compacted[len(compacted)-1].Text() == "[manual compaction]" {
		compacted = compacted[:len(compacted)-1]
	}

	if err := s.Memory.Replace(ctx, sessionID, compacted); err != nil {
		return "", fmt.Errorf("compaction: persist: %w", err)
	}

	after := len(compacted)
	return fmt.Sprintf("compacted %d messages → %d messages (summary + %d kept)", before, after, after-1), nil
}

// summarize calls the LLM to produce a concise summary of oldMsgs. Any
// existing IsSummary message in oldMsgs is folded in as prior context so
// earlier summaries are not lost (summarize-on-summarize).
func (s *CompactionStage) summarize(ctx context.Context, oldMsgs []types.Message, sigCh <-chan signal.Signal) (string, error) {
	var b strings.Builder
	b.WriteString("Summarize the conversation below. Capture: key facts, decisions made, pending tasks, and any important context the assistant needs to continue. Be concise and factual; do not invent details. If a prior summary is included, integrate it.\n\n")

	for _, m := range oldMsgs {
		//nolint:exhaustive // RoleSystem never appears in Messages (it's a separate field); only user/assistant/tool reach here.
		switch m.Role {
		case types.RoleUser:
			if m.IsSummary {
				b.WriteString("[Prior summary]\n")
			}
			b.WriteString("User: ")
			b.WriteString(m.Text())
			if m.HasImage() {
				b.WriteString(" [attachments: " + strings.Join(m.ImageFilenames(), ", ") + "]")
			}
			b.WriteString("\n\n")
		case types.RoleAssistant:
			b.WriteString("Assistant: ")
			b.WriteString(m.Text())
			if m.Thinking != "" {
				b.WriteString("\n(thought: ")
				b.WriteString(truncateStr(m.Thinking, 500))
				b.WriteString(")")
			}
			b.WriteString("\n\n")
		case types.RoleTool:
			b.WriteString("[Tool result ")
			b.WriteString(m.ToolCallID)
			b.WriteString(": ")
			b.WriteString(truncateStr(m.Text(), 300))
			b.WriteString("]\n\n")
		}
	}

	maxTokens := s.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 512
	}

	req := llm.LLMRequest{
		Messages: []types.Message{
			{Role: types.RoleUser, Parts: []types.ContentPart{types.TextPart(b.String())}, Timestamp: time.Now()},
		},
		Model:     s.activeModel(),
		MaxTokens: maxTokens,
		Stream:    true,
		Timeout:   60 * time.Second,
	}

	ch, err := s.Provider.CompleteStream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("compaction: start summary: %w", err)
	}
	var content strings.Builder
	// Watch ctx and sigCh alongside the stream. sigCh (when non-nil) lets
	// /session pause and ESC take effect mid-compaction: Pause blocks here
	// until Resume (the stream stays open, mirroring LLMStage), Interrupt
	// aborts. ctx.Done() covers the compositor's Interrupt cancellation
	// for stages that don't subscribe. A nil sigCh makes the signal case
	// inert (nil channel never fires in select).
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				goto summarizeDone
			}
			if chunk.Error != nil {
				return "", fmt.Errorf("compaction: summary stream: %w", chunk.Error)
			}
			content.WriteString(chunk.Content)
		case sig, ok := <-sigCh:
			if !ok {
				continue
			}
			switch sig { //nolint:exhaustive // only Pause/Interrupt need action; other signals ignored.
			case signal.Pause:
				// Block until Resume/Interrupt/Cancel. On Resume, keep
				// streaming the summary; on anything else, abort.
				if pauseOnSignal(sigCh) != signal.Resume {
					return "", fmt.Errorf("compaction: summary stream: %w", ErrInterrupted)
				}
			case signal.Interrupt:
				return "", fmt.Errorf("compaction: summary stream: %w", ErrInterrupted)
			}
		case <-ctx.Done():
			return "", fmt.Errorf("compaction: summary stream: %w", ctx.Err())
		}
	}

summarizeDone:
	out := strings.TrimSpace(content.String())
	if out == "" {
		return "", fmt.Errorf("compaction: summary produced empty output")
	}
	return out, nil
}
