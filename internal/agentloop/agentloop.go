package agentloop

import (
	"context"
	"strings"
	"time"
	"unicode"

	"dolphin/internal/agentio"
	"dolphin/internal/event"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

type AgentLoop struct {
	queue      chan *agentio.Turn
	onResult   func(agentio.TurnResult)
	compositor *Compositor
	logger     *zap.Logger
	eventBus   *event.Bus
}

func NewAgentLoop(queue chan *agentio.Turn, compositor *Compositor, logger *zap.Logger, eventBus *event.Bus) *AgentLoop {
	return &AgentLoop{
		queue:      queue,
		compositor: compositor,
		logger:     logger,
		eventBus:   eventBus,
	}
}

func (a *AgentLoop) SetOnResult(fn func(agentio.TurnResult)) {
	a.onResult = fn
}

func (a *AgentLoop) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			a.logger.Info("agent loop stopped")
			return
		case turn := <-a.queue:
			a.processTurn(ctx, turn)
		}
	}
}

func (a *AgentLoop) processTurn(ctx context.Context, turn *agentio.Turn) {
	// Create a root span — the span context propagates through ctx to stages,
	// so LLM and tool spans become children of this turn span.
	ctx, span := otel.Tracer("dolphin").Start(ctx, "turn."+turn.SessionID)
	sid := validSessionID(turn.SessionID)
	if sid != "" {
		span.SetAttributes(attribute.String("sessionid", sid))
	}
	span.SetAttributes(attribute.String("input", turn.Input))
	start := time.Now()
	defer span.End()

	a.publishTurnEvent(ctx, event.EventTurnStart, turn.SessionID, start, nil)

	var output strings.Builder

	state := &State{
		SessionID:        turn.SessionID,
		Input:            turn.Input,
		TransportContext: turn.Context,
	}

	state.OnChunk = func(text string) {
		output.WriteString(text)
		if a.onResult != nil {
			a.onResult(agentio.TurnResult{
				TransportID: turn.TransportID,
				SessionID:   turn.SessionID,
				Text:        text,
			})
		}
	}

	if err := a.compositor.Execute(ctx, state); err != nil {
		span.SetAttributes(attribute.Bool("error", true))
		span.SetAttributes(attribute.String("output", output.String()))
		if a.onResult != nil {
			a.onResult(agentio.TurnResult{
				TransportID: turn.TransportID,
				SessionID:   turn.SessionID,
				Text:        "Error: " + err.Error(),
				Done:        true,
			})
		}
		a.publishTurnEvent(ctx, event.EventTurnError, turn.SessionID, start, err)
		return
	}

	span.SetAttributes(attribute.String("output", output.String()))

	if a.onResult != nil {
		a.onResult(agentio.TurnResult{
			TransportID: turn.TransportID,
			SessionID:   turn.SessionID,
			Done:        true,
		})
	}
	a.publishTurnEvent(ctx, event.EventTurnComplete, turn.SessionID, start, nil)
}

func (a *AgentLoop) publishTurnEvent(ctx context.Context, et event.Type, sid string, start time.Time, err error) {
	if a.eventBus == nil {
		return
	}
	payload := map[string]any{
		"duration_ms": float64(time.Since(start).Milliseconds()),
	}
	if err != nil {
		payload["error"] = err.Error()
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
