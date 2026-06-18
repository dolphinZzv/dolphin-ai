package agentloop

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/event"
	"dolphin/internal/transport"
	"dolphin/internal/types"
)

type AgentLoop struct {
	queue      chan *agentio.Turn
	priority   chan *agentio.Turn
	onResult   func(agentio.TurnResult)
	compositor *Compositor
	logger     *zap.Logger
	eventBus   *event.Bus
	agentIO    *agentio.AgentIO
	poolSize   int

	sessionMu    sync.Mutex
	sessionLocks map[string]*sync.Mutex

	gcInterval           time.Duration
	maxPanicBackoff      time.Duration
	maxConsecutivePanics int
}

func NewAgentLoop(queue chan *agentio.Turn, compositor *Compositor, logger *zap.Logger, eventBus *event.Bus, agentIO *agentio.AgentIO, poolSize int) *AgentLoop {
	if poolSize <= 0 {
		poolSize = 1
	}
	pr := make(chan *agentio.Turn)
	if agentIO != nil {
		pr = agentIO.PriorityQueue()
	}
	return &AgentLoop{
		queue:                queue,
		priority:             pr,
		compositor:           compositor,
		logger:               logger,
		eventBus:             eventBus,
		agentIO:              agentIO,
		poolSize:             poolSize,
		sessionLocks:         make(map[string]*sync.Mutex),
		maxPanicBackoff:      30 * time.Second,
		maxConsecutivePanics: 5,
	}
}

func (a *AgentLoop) SetOnResult(fn func(agentio.TurnResult)) {
	a.onResult = fn
}

// SetSessionGcInterval sets the session lock GC interval.
func (a *AgentLoop) SetSessionGcInterval(d time.Duration) {
	a.gcInterval = d
}

func (a *AgentLoop) Run(ctx context.Context) {
	if a.poolSize > 1 {
		go a.startSessionLockGC(ctx)
	}

	var wg sync.WaitGroup
	for i := 0; i < a.poolSize; i++ {
		wg.Add(1)
		go func(id int) { //nolint:gosec // G118: worker publishes panic events on background ctx (run ctx may be cancelled)
			defer wg.Done()
			workerID := fmt.Sprintf("worker-%d", id+1)
			consecutivePanics := 0

			for {
				a.runWorker(ctx, workerID)
				select {
				case <-ctx.Done():
					return
				default:
				}

				consecutivePanics++
				backoff := time.Duration(1<<(consecutivePanics-1)) * time.Second
				if backoff > a.maxPanicBackoff {
					backoff = a.maxPanicBackoff
				}

				if a.eventBus != nil {
					a.eventBus.Publish(context.Background(), event.Event{
						Type:      event.EventWorkerPanic,
						Timestamp: time.Now(),
						Payload: map[string]any{
							"worker_id":          workerID,
							"consecutive_panics": consecutivePanics,
							"backoff_ms":         backoff.Milliseconds(),
						},
					})
				}

				if consecutivePanics >= a.maxConsecutivePanics {
					a.logger.Error("worker exceeded max consecutive panics, exiting",
						zap.String("worker_id", workerID),
						zap.Int("consecutive_panics", consecutivePanics),
					)
					return
				}

				a.logger.Warn("worker panicked, restarting",
					zap.String("worker_id", workerID),
					zap.Int("consecutive_panics", consecutivePanics),
					zap.Duration("backoff", backoff),
				)
				time.Sleep(backoff)
			}
		}(i)
	}
	wg.Wait()
	a.logger.Info("agent loop stopped")
}

func (a *AgentLoop) runWorker(ctx context.Context, id string) {
	wLogger := a.logger.With(zap.String("worker_id", id))
	compositor := a.compositor.Clone()

	defer func() {
		if r := recover(); r != nil {
			wLogger.Error("worker panic recovered", zap.Any("panic", r))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			wLogger.Info("worker stopped")
			return
		case turn := <-a.priority:
			if a.agentIO != nil {
				cancelled := a.agentIO.IsCancelled(turn.TurnID)
				a.agentIO.OnTurnDequeued(turn)
				if cancelled {
					continue
				}
			}
			a.processTurn(ctx, turn, compositor, id, wLogger)
		case turn := <-a.queue:
			if a.agentIO != nil {
				cancelled := a.agentIO.IsCancelled(turn.TurnID)
				a.agentIO.OnTurnDequeued(turn)
				if cancelled {
					continue
				}
			}
			a.processTurn(ctx, turn, compositor, id, wLogger)
		}
	}
}

func (a *AgentLoop) processTurn(ctx context.Context, turn *agentio.Turn, compositor *Compositor, workerID string, wLogger *zap.Logger) {
	mu := a.sessionLock(turn.SessionID)
	mu.Lock()
	defer mu.Unlock()

	if a.agentIO != nil {
		a.agentIO.SetActive(workerID, turn)
		defer a.agentIO.ClearActive(workerID)
	}

	// Create a root span — the span context propagates through ctx to stages,
	// so LLM and tool spans become children of this turn span.
	ctx, span := otel.Tracer("dolphin").Start(ctx, "turn."+turn.SessionID)
	span.SetAttributes(attribute.String("turnid", turn.TurnID))
	sid := validSessionID(turn.SessionID)
	if sid != "" {
		span.SetAttributes(attribute.String("sessionid", sid))
	}
	span.SetAttributes(attribute.String("input", turn.Input))
	span.SetAttributes(attribute.String("worker_id", workerID))
	start := time.Now()
	defer span.End()

	// Catch panics raised by compositor stages so the turn is not silently
	// dropped: convert the panic into an error result for this turn (so the
	// transport/UI receives a Done signal instead of hanging forever), then
	// re-panic so runWorker's recovery still drives exponential backoff and
	// publishes EventWorkerPanic. This defer is registered after span.End so
	// it runs first under LIFO unwinding, letting it mark the span as errored
	// before span.End executes; the remaining defers (ClearActive, Unlock)
	// still run during the re-panicked unwind.
	defer func() {
		if r := recover(); r != nil {
			span.SetAttributes(attribute.Bool("error", true))
			span.SetAttributes(attribute.String("panic", fmt.Sprintf("%v", r)))
			err := fmt.Errorf("worker panic: %v", r)
			if a.onResult != nil {
				a.onResult(agentio.TurnResult{
					TurnID:      turn.TurnID,
					TransportID: turn.TransportID,
					SessionID:   turn.SessionID,
					Text:        "Error: " + err.Error(),
					Done:        true,
				})
			}
			a.publishTurnEvent(ctx, event.EventTurnError, turn.TurnID, turn.SessionID, start, err)
			panic(r)
		}
	}()

	a.publishTurnEvent(ctx, event.EventTurnStart, turn.TurnID, turn.SessionID, start, nil)

	var output strings.Builder

	state := &State{
		SessionID:        turn.SessionID,
		Input:            turn.Input,
		TransportContext: turn.Context,
		TransportID:      turn.TransportID,
	}

	state.OnChunk = func(text string) {
		output.WriteString(text)
		if a.onResult != nil {
			a.onResult(agentio.TurnResult{
				TurnID:      turn.TurnID,
				TransportID: turn.TransportID,
				SessionID:   turn.SessionID,
				Text:        text,
			})
		}
	}

	state.OnThinking = func(text string) {
		if a.onResult != nil {
			a.onResult(agentio.TurnResult{
				TurnID:      turn.TurnID,
				TransportID: turn.TransportID,
				SessionID:   turn.SessionID,
				Thinking:    text,
			})
		}
	}

	state.OnToolCall = func(tc types.ToolCall) {
		if a.agentIO != nil {
			a.agentIO.SetWorkerActivity(workerID, "call "+tc.Name)
		}
		if a.onResult != nil {
			a.onResult(agentio.TurnResult{
				TurnID:      turn.TurnID,
				TransportID: turn.TransportID,
				SessionID:   turn.SessionID,
				ToolCall:    &tc,
			})
		}
	}

	state.OnToolResult = func(tr types.ToolResult) {
		if a.agentIO != nil {
			a.agentIO.SetWorkerActivity(workerID, "")
		}
		if a.onResult != nil {
			a.onResult(agentio.TurnResult{
				TurnID:      turn.TurnID,
				TransportID: turn.TransportID,
				SessionID:   turn.SessionID,
				ToolResult:  &tr,
			})
		}
	}

	if turn.TransportID != "" {
		ctx = transport.WithInfo(ctx, &transport.Info{ID: turn.TransportID})
	}

	if err := compositor.Execute(ctx, state); err != nil {
		span.SetAttributes(attribute.Bool("error", true))
		span.SetAttributes(attribute.String("output", output.String()))
		if a.onResult != nil {
			a.onResult(agentio.TurnResult{
				TurnID:      turn.TurnID,
				TransportID: turn.TransportID,
				SessionID:   turn.SessionID,
				Text:        "Error: " + err.Error(),
				Done:        true,
			})
		}
		a.publishTurnEvent(ctx, event.EventTurnError, turn.TurnID, turn.SessionID, start, err)
		return
	}

	span.SetAttributes(attribute.String("output", output.String()))

	if a.onResult != nil {
		a.onResult(agentio.TurnResult{
			TurnID:      turn.TurnID,
			TransportID: turn.TransportID,
			SessionID:   turn.SessionID,
			Done:        true,
		})
	}
	a.publishTurnEvent(ctx, event.EventTurnComplete, turn.TurnID, turn.SessionID, start, nil,
		"system_context_length", len(state.SystemPrompt),
		"tool_call_count", len(state.ToolResults),
	)
}

func (a *AgentLoop) sessionLock(sessionID string) *sync.Mutex {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	if mu, ok := a.sessionLocks[sessionID]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	a.sessionLocks[sessionID] = mu
	return mu
}

// startSessionLockGC periodically removes session locks that are no longer
// contended. There is a theoretical window where GC runs between two turns for
// the same session: TryLock succeeds because no turn currently holds it, the
// entry is deleted, and the next turn's sessionLock() allocates a new Mutex.
// This is harmless — no correctness impact, just an extra allocation.
func (a *AgentLoop) startSessionLockGC(ctx context.Context) {
	interval := a.gcInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sessionMu.Lock()
			for id, mu := range a.sessionLocks {
				if mu.TryLock() {
					mu.Unlock()
					delete(a.sessionLocks, id)
				}
			}
			a.sessionMu.Unlock()
		}
	}
}

func (a *AgentLoop) publishTurnEvent(ctx context.Context, et event.Type, turnID, sid string, start time.Time, err error, extraKV ...any) {
	if a.eventBus == nil {
		return
	}
	payload := map[string]any{
		"turn_id":     turnID,
		"duration_ms": float64(time.Since(start).Milliseconds()),
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	for i := 0; i+1 < len(extraKV); i += 2 {
		if k, ok := extraKV[i].(string); ok {
			payload[k] = extraKV[i+1]
		}
	}
	a.eventBus.Publish(ctx, event.Event{
		Type:      et,
		Timestamp: time.Now(),
		SessionID: sid,
		Payload:   payload,
	})
}

func validSessionID(sid string) string {
	if len(sid) > 200 {
		return ""
	}
	for _, r := range sid {
		if r > unicode.MaxASCII {
			return ""
		}
	}
	return sid
}
