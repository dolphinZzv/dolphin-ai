package lifecycle

import (
	"context"
	"errors"
	"time"

	"dolphin/internal/agentio"
	"dolphin/internal/agentloop"
	"dolphin/internal/brain"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/limit"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/transport"
	"dolphin/internal/userio"
	"dolphin/internal/watcher"

	"go.uber.org/zap"
)

type Pipeline struct {
	transports          []transport.IO
	userIO              *userio.UserIO
	agentIO             *agentio.AgentIO
	agentLoop           *agentloop.AgentLoop
	sessionMgr          *session.Manager
	brain               *brain.Brain
	scheduler           *scheduler.Scheduler
	signalBus           *signal.Bus
	eventBus            *event.Bus
	logger              *zap.Logger
	cancel              context.CancelFunc
	otelShutdown        func()
	pprofShutdown       func()
	watchers            []*watcher.Watcher
	subscriptionEngine  *brain.SubscriptionEngine
	limitResetScheduler *limit.ResetScheduler
}

func New(cfg *config.Config) *Pipeline {
	return NewBuilder(cfg).
		StepLogger().
		StepBuses().
		StepLimit().
		StepSession().
		StepMemory().
		StepLLM().
		StepTools().
		StepBrain().
		StepScheduler().
		StepAgentIO().
		StepUserIO().
		StepObservability().
		StepPprof().
		StepTransports().
		Assemble().
		Build()
}

func (p *Pipeline) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	p.logger.Info("pipeline starting", zap.Int("transports", len(p.transports)))
	p.eventBus.Publish(ctx, event.Event{Type: event.EventPipelineStart})

	if p.scheduler != nil {
		p.scheduler.Start(ctx)
	}

	// Start subscription engine and file watchers.
	if p.subscriptionEngine != nil {
		p.subscriptionEngine.Start()
	}
	for _, w := range p.watchers {
		w.Start(ctx)
	}

	// Idle monitor: 20s after last user input, auto-commit brain changes.
	userActive := make(chan struct{}, 64)
	if p.brain != nil {
		go func() {
			timer := time.NewTimer(20 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-userActive:
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(20 * time.Second)
				case <-timer.C:
					p.brain.AutoCommit(ctx, "")
					timer.Reset(20 * time.Second)
				}
			}
		}()
	}

	// Accumulate token usage and tool call count per-session from events.
	p.eventBus.Subscribe(func(ctx context.Context, e event.Event) {
		if e.SessionID == "" {
			return
		}
		sess := p.sessionMgr.Get(e.SessionID)
		if sess == nil {
			return
		}
		switch e.Type {
		case event.EventLLMComplete:
			if v, ok := e.Payload["input_tokens"].(int); ok && v > 0 {
				sess.Set("last_input_tokens", v)
				acc := 0
				if cur := sess.Get("input_tokens"); cur != nil {
					acc, _ = cur.(int)
				}
				sess.Set("input_tokens", acc+v)
			}
			if v, ok := e.Payload["output_tokens"].(int); ok && v > 0 {
				sess.Set("last_output_tokens", v)
				acc := 0
				if cur := sess.Get("output_tokens"); cur != nil {
					acc, _ = cur.(int)
				}
				sess.Set("output_tokens", acc+v)
			}
		case event.EventContextComplete:
			if prompt, ok := e.Payload["input"].(string); ok {
				sess.Set("system_context", len(prompt))
			}
		case event.EventTurnStart:
			acc := 0
			if cur := sess.Get("rounds"); cur != nil {
				acc, _ = cur.(int)
			}
			sess.Set("rounds", acc+1)
		case event.EventToolComplete:
			acc := 0
			if cur := sess.Get("tool_calls"); cur != nil {
				acc, _ = cur.(int)
			}
			sess.Set("tool_calls", acc+1)
		}
	})

	go p.agentLoop.Run(ctx)

	p.agentLoop.SetOnResult(func(tr agentio.TurnResult) {
		p.agentIO.OnResult(&tr)
	})

	for _, tio := range p.transports {
		if err := tio.Start(ctx); err != nil {
			p.logger.Warn("transport start failed",
				zap.String("transport_id", tio.ID()),
				zap.Error(err),
			)
		}
	}

	for _, tio := range p.transports {
		t := tio
		go func() {
			for {
				input, err := t.Read(ctx)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						p.logger.Info("transport read stopped",
							zap.String("transport_id", t.ID()),
							zap.Error(err),
						)
						return
					}
					p.logger.Warn("transport read error, retrying in 5s",
						zap.String("transport_id", t.ID()),
						zap.Error(err),
					)
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Second):
					}
					continue
				}
				select {
				case userActive <- struct{}{}:
				default:
				}
				if !p.userIO.Handle(ctx, t, input) {
					continue
				}
			}
		}()
	}

}

func (p *Pipeline) Shutdown() {
	p.logger.Info("pipeline shutting down")
	p.eventBus.Publish(context.Background(), event.Event{Type: event.EventPipelineShutdown})

	// Stop subscription engine and file watchers.
	if p.subscriptionEngine != nil {
		p.subscriptionEngine.Stop()
	}
	for _, w := range p.watchers {
		w.Stop()
	}

	for _, tio := range p.transports {
		if err := tio.Close(); err != nil {
			p.logger.Warn("transport close error",
				zap.String("transport_id", tio.ID()),
				zap.Error(err),
			)
		}
	}

	if p.cancel != nil {
		p.cancel()
	}

	if p.scheduler != nil {
		p.scheduler.Stop()
	}

	if p.limitResetScheduler != nil {
		p.limitResetScheduler.Stop()
	}

	if p.otelShutdown != nil {
		p.otelShutdown()
	}

	if p.pprofShutdown != nil {
		p.pprofShutdown()
	}

	if p.logger != nil {
		_ = p.logger.Sync()
	}
}
