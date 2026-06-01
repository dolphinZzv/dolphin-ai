package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"dolphin/internal/agentio"
	"dolphin/internal/agentloop"
	"dolphin/internal/brain"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/scheduler"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/transport"
	"dolphin/internal/userio"

	"go.uber.org/zap"
)

type Pipeline struct {
	transports         []transport.IO
	userIO             *userio.UserIO
	agentIO            *agentio.AgentIO
	agentLoop          *agentloop.AgentLoop
	sessionMgr         *session.Manager
	brain              *brain.Brain
	scheduler          *scheduler.Scheduler
	signalBus          *signal.Bus
	eventBus           *event.Bus
	logger             *zap.Logger
	cancel             context.CancelFunc
	otelShutdown       func()
	dingtalkWebhookURL string
}

func New(cfg *config.Config) *Pipeline {
	return NewBuilder(cfg).
		StepLogger().
		StepBuses().
		StepSession().
		StepMemory().
		StepLLM().
		StepTools().
		StepBrain().
		StepScheduler().
		StepAgentIO().
		StepUserIO().
		StepObservability().
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

	go p.agentLoop.Run(ctx)

	p.agentLoop.SetOnResult(func(tr agentio.TurnResult) {
		p.agentIO.OnResult(&tr)
	})

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

	// Send startup notification via DingTalk webhook if configured.
	if p.dingtalkWebhookURL != "" {
		go sendStartupNotification(p.logger, p.dingtalkWebhookURL)
	}
}

func sendStartupNotification(logger *zap.Logger, webhookURL string) {
	payload := map[string]any{
		"msgtype": "text",
		"text": map[string]string{
			"content": "Dolphin AI assistant online ✓",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("startup notification marshal error", zap.Error(err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(data))
	if err != nil {
		logger.Warn("startup notification request error", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("startup notification send error", zap.Error(err))
		return
	}
	defer resp.Body.Close()
}

func (p *Pipeline) Shutdown() {
	p.logger.Info("pipeline shutting down")
	p.eventBus.Publish(context.Background(), event.Event{Type: event.EventPipelineShutdown})

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

	if p.otelShutdown != nil {
		p.otelShutdown()
	}

	if p.logger != nil {
		_ = p.logger.Sync()
	}
}
