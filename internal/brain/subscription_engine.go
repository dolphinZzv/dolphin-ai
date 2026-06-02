package brain

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dolphin/internal/event"
	"go.uber.org/zap"
)

// SubscriptionEngine matches events against stored subscriptions and sends
// trigger content to the agent loop for LLM processing.
type SubscriptionEngine struct {
	brain    *Brain
	eventBus *event.Bus
	logger   *zap.Logger
	handler  event.Handler
	running  bool

	// SendTurn enqueues a new turn for the agent loop. If nil, triggers are
	// silently dropped.
	SendTurn func(ctx context.Context, input string)

	cooldownMu      sync.Mutex
	lastTriggeredAt map[string]time.Time // sub name -> last trigger time
	cooldownPeriod  time.Duration        // minimum interval between triggers of the same sub
}

const defaultCooldownPeriod = 30 * time.Second

// NewSubscriptionEngine creates a new subscription engine.
func NewSubscriptionEngine(brain *Brain, eventBus *event.Bus, logger *zap.Logger) *SubscriptionEngine {
	return &SubscriptionEngine{
		brain:           brain,
		eventBus:        eventBus,
		logger:          logger,
		lastTriggeredAt: make(map[string]time.Time),
		cooldownPeriod:  defaultCooldownPeriod,
	}
}

// Start subscribes to the event bus and begins processing events.
func (e *SubscriptionEngine) Start() {
	if e.running {
		return
	}
	e.running = true
	e.handler = e.handleEvent
	e.eventBus.Subscribe(e.handler)
	if e.logger != nil {
		e.logger.Info("subscription engine started")
	}
}

// Stop unsubscribes from the event bus.
func (e *SubscriptionEngine) Stop() {
	e.running = false
	if e.logger != nil {
		e.logger.Info("subscription engine stopped")
	}
}

// handleEvent is called for every event on the bus.
func (e *SubscriptionEngine) handleEvent(ctx context.Context, evt event.Event) {
	if !e.running {
		return
	}

	subs, err := ListSubscriptions(ctx, e.brain)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("subscription engine: failed to list subscriptions", zap.Error(err))
		}
		return
	}

	for i := range subs {
		if !subs[i].Enabled {
			continue
		}
		if !e.matches(&subs[i], evt) {
			continue
		}
		e.trigger(ctx, &subs[i], evt)
	}
}

// matches checks whether an event matches a subscription's pattern and filters.
func (e *SubscriptionEngine) matches(sub *Subscription, evt event.Event) bool {
	matched, err := path.Match(sub.EventPattern, string(evt.Type))
	if err != nil || !matched {
		return false
	}

	if sub.Filters.Path != "" {
		p, ok := evt.Payload["path"].(string)
		if !ok {
			return false
		}
		matched, err := filepath.Match(sub.Filters.Path, p)
		if err != nil || !matched {
			return false
		}
	}

	return true
}

// trigger sends the subscription content to the agent loop as a new turn,
// enriched with event context so the LLM knows what happened.
// It enforces a cooldown per subscription to prevent infinite trigger loops.
func (e *SubscriptionEngine) trigger(ctx context.Context, sub *Subscription, evt event.Event) {
	if sub.Content == "" {
		return
	}
	if e.SendTurn == nil {
		return
	}

	e.cooldownMu.Lock()
	last, ok := e.lastTriggeredAt[sub.Name]
	now := time.Now()
	if ok && now.Sub(last) < e.cooldownPeriod {
		e.cooldownMu.Unlock()
		if e.logger != nil {
			e.logger.Debug("subscription suppressed by cooldown",
				zap.String("subscription", sub.Name),
				zap.Duration("cooldown", e.cooldownPeriod),
			)
		}
		return
	}
	e.lastTriggeredAt[sub.Name] = now
	e.cooldownMu.Unlock()

	// Build enriched input with event context so the LLM knows what it's responding to.
	input := e.buildTriggerInput(sub, evt)
	e.SendTurn(ctx, input)

	if e.logger != nil {
		e.logger.Info("subscription triggered",
			zap.String("subscription", sub.Name),
			zap.String("event", string(evt.Type)),
		)
	}
}

// buildTriggerInput wraps the subscription content with event context metadata.
func (e *SubscriptionEngine) buildTriggerInput(sub *Subscription, evt event.Event) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[Event: %s]\n", evt.Type))
	// Append payload key-value pairs for context.
	for k, v := range evt.Payload {
		b.WriteString(fmt.Sprintf("[%s: %v]\n", k, v))
	}
	b.WriteString("\n---\n\n")
	b.WriteString(sub.Content)
	return b.String()
}
