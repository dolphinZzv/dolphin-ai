package setup

import (
	"context"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/event"
	"dolphin/internal/limit"
)

type LimitBootstrapper struct{}

func (b *LimitBootstrapper) Name() string { return "limit" }
func (b *LimitBootstrapper) Index() int   { return 45 }

func (b *LimitBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.Limit != nil {
		return nil
	}

	// Check if any limit is configured.
	enabled := c.Config.GetBool("llm.limit.enabled")
	if !enabled {
		hasLimit := c.Config.GetInt("llm.limit.max_requests") > 0 ||
			c.Config.GetInt("llm.limit.max_requests.hard") > 0 ||
			c.Config.GetInt("llm.limit.max_total_tokens") > 0 ||
			c.Config.GetInt("llm.limit.max_total_tokens.hard") > 0
		if !hasLimit {
			c.Logger.Debug("limit: no limits configured, skipping")
			return nil
		}
	}

	// Create store.
	storeDir := c.Config.GetString("limit.dir")
	if storeDir == "" {
		storeDir = ".dolphin/limits"
	}
	var store limit.Store
	fs, err := limit.NewFileStore(storeDir)
	if err != nil {
		c.Logger.Warn("limit: failed to create store, using memory fallback", zap.Error(err))
		store = limit.NewMemoryStore()
	} else {
		store = fs
	}

	// Create limiter.
	limiter := limit.NewLimiter(store, c.Config, c.EventBus, c.Logger)
	c.Limit = limiter

	// Register as hook handler for synchronous pre-check.
	c.HookReg.Register(limiter)

	// Subscribe to EventLLMComplete for async recording.
	c.EventBus.Subscribe(func(ctx context.Context, e event.Event) {
		if e.Type != event.EventLLMComplete {
			return
		}
		model, _ := e.Payload["model"].(string)
		inputTokens, _ := e.Payload["input_tokens"].(int)
		outputTokens, _ := e.Payload["output_tokens"].(int)
		limiter.RecordLLM(model, inputTokens, outputTokens)
	})

	// Start reset scheduler if cron expression configured.
	if expr := c.Config.GetString("llm.limit.reset_cron"); expr != "" {
		var lastReset time.Time
		if fs != nil {
			lastReset = fs.LastReset()
		}
		rs, err := limit.NewResetScheduler(expr, store, lastReset, c.Logger, limiter.ClearAlerted)
		if err != nil {
			c.Logger.Warn("limit: invalid reset_cron, skipping", zap.String("expr", expr), zap.Error(err))
		} else {
			c.LimitResetScheduler = rs
			c.Logger.Info("limit: reset scheduler started", zap.String("cron", expr))
		}
	}

	// Start webhook notifier if configured.
	if webhookURL := c.Config.GetString("agent.webhook.url"); webhookURL != "" {
		webhookType := limit.WebhookHTTP
		if t := c.Config.GetString("agent.webhook.type"); t != "" {
			webhookType = limit.WebhookType(t)
		}
		wn := limit.NewWebhookNotifier(webhookType, webhookURL, c.Logger)
		c.EventBus.Subscribe(func(ctx context.Context, e event.Event) {
			wn.Handle(ctx, e)
		})
		c.Logger.Info("limit: webhook notifier started",
			zap.String("type", string(webhookType)),
			zap.String("url", webhookURL),
		)
	}

	c.Logger.Info("limit: module initialized")
	return nil
}
