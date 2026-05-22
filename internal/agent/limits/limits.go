package limits

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dolphin/internal/config"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

type LimitsManager struct {
	config    *config.LimitsConfig
	counter   *TokenCounter
	semaphore *ConcurrencyLimiter
	scheduler *cron.Cron
	logger    *zap.Logger
	mu        sync.RWMutex
}

func NewLimitsManager(cfg *config.LimitsConfig) *LimitsManager {
	lm := &LimitsManager{
		config:    cfg,
		counter:   NewTokenCounter(cfg),
		semaphore: NewConcurrencyLimiter(cfg.Concurrency.MaxRunning),
	}

	if cfg.SchedulerEnabled {
		lm.scheduler = cron.New()
		lm.registerCronJobs()
		lm.scheduler.Start()
	}

	return lm
}

func (lm *LimitsManager) registerCronJobs() {
	if lm.scheduler == nil {
		return
	}

	levels := []struct {
		name  string
		cron  string
		reset func()
	}{
		{"requests_daily", lm.config.Requests.Daily.ResetCron, func() { lm.resetLevel("daily", "requests") }},
		{"requests_weekly", lm.config.Requests.Weekly.ResetCron, func() { lm.resetLevel("weekly", "requests") }},
		{"requests_monthly", lm.config.Requests.Monthly.ResetCron, func() { lm.resetLevel("monthly", "requests") }},
		{"tokens_daily", lm.config.Tokens.Daily.ResetCron, func() { lm.resetLevel("daily", "tokens") }},
		{"tokens_weekly", lm.config.Tokens.Weekly.ResetCron, func() { lm.resetLevel("weekly", "tokens") }},
		{"tokens_monthly", lm.config.Tokens.Monthly.ResetCron, func() { lm.resetLevel("monthly", "tokens") }},
	}

	for _, l := range levels {
		if l.cron != "" {
			lm.scheduler.AddFunc(l.cron, l.reset)
		}
	}
}

func (lm *LimitsManager) resetLevel(level, resetType string) {
	lm.mu.Lock()
	before := lm.counter.GetSnapshot()
	lm.counter.ResetLevel(level)
	lm.mu.Unlock()

	if lm.logger != nil {
		lm.logger.Info("limits reset",
			zap.String("level", level),
			zap.String("type", resetType),
			zap.Int("previous_value", before[level+"_"+resetType]),
			zap.Int("current_value", 0),
		)
	}
}

func (lm *LimitsManager) Stop() {
	if lm.scheduler != nil {
		lm.scheduler.Stop()
	}
}

func (lm *LimitsManager) Check(ctx context.Context, req *CheckRequest) error {
	if !lm.config.Enabled {
		return nil
	}

	if lm.config.Exempt.Enabled && lm.isExempt(req) {
		return nil
	}

	if err := lm.semaphore.Acquire(ctx); err != nil {
		err := &LimitError{
			Type:        "concurrency",
			Current:     lm.semaphore.Current(),
			Max:         lm.config.Concurrency.MaxRunning,
			Enforcement: lm.config.Enforcement,
		}
		lm.logBlocked(err)
		return err
	}
	defer lm.semaphore.Release()

	if lm.config.Requests.Daily.Max > 0 && lm.counter.RequestsDaily >= lm.config.Requests.Daily.Max {
		err := &LimitError{
			Type:        "requests",
			Level:       "daily",
			Current:     lm.counter.RequestsDaily,
			Max:         lm.config.Requests.Daily.Max,
			Enforcement: lm.config.Enforcement,
		}
		lm.logBlocked(err)
		return err
	}

	if lm.config.Requests.Weekly.Max > 0 && lm.counter.RequestsWeekly >= lm.config.Requests.Weekly.Max {
		err := &LimitError{
			Type:        "requests",
			Level:       "weekly",
			Current:     lm.counter.RequestsWeekly,
			Max:         lm.config.Requests.Weekly.Max,
			Enforcement: lm.config.Enforcement,
		}
		lm.logBlocked(err)
		return err
	}

	if lm.config.Requests.Monthly.Max > 0 && lm.counter.RequestsMonthly >= lm.config.Requests.Monthly.Max {
		err := &LimitError{
			Type:        "requests",
			Level:       "monthly",
			Current:     lm.counter.RequestsMonthly,
			Max:         lm.config.Requests.Monthly.Max,
			Enforcement: lm.config.Enforcement,
		}
		lm.logBlocked(err)
		return err
	}

	if lm.config.Tokens.Daily.InputMax > 0 && lm.counter.InputTokensDaily >= lm.config.Tokens.Daily.InputMax {
		err := &LimitError{
			Type:        "tokens_input",
			Level:       "daily",
			Current:     lm.counter.InputTokensDaily,
			Max:         lm.config.Tokens.Daily.InputMax,
			Enforcement: lm.config.Enforcement,
		}
		lm.logBlocked(err)
		return err
	}

	if lm.config.Tokens.Daily.OutputMax > 0 && lm.counter.OutputTokensDaily >= lm.config.Tokens.Daily.OutputMax {
		err := &LimitError{
			Type:        "tokens_output",
			Level:       "daily",
			Current:     lm.counter.OutputTokensDaily,
			Max:         lm.config.Tokens.Daily.OutputMax,
			Enforcement: lm.config.Enforcement,
		}
		lm.logBlocked(err)
		return err
	}

	return nil
}

func (lm *LimitsManager) logBlocked(err *LimitError) {
	if lm.logger != nil {
		lm.logger.Info("limits exceeded, request blocked",
			zap.String("type", err.Type),
			zap.String("level", err.Level),
			zap.Int("current", err.Current),
			zap.Int("max", err.Max),
			zap.String("enforcement", err.Enforcement),
		)
	}
	RecordBlocked(err.Type, err.Enforcement)
}

func (lm *LimitsManager) UpdateUsage(usage *Usage) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.counter.RequestsDaily++
	lm.counter.RequestsWeekly++
	lm.counter.RequestsMonthly++
	lm.counter.InputTokensDaily += usage.InputTokens
	lm.counter.OutputTokensDaily += usage.OutputTokens
	lm.counter.InputTokensWeekly += usage.InputTokens
	lm.counter.OutputTokensWeekly += usage.OutputTokens
	lm.counter.InputTokensMonthly += usage.InputTokens
	lm.counter.OutputTokensMonthly += usage.OutputTokens

	lm.counter.Persist()

	RecordRequest("daily")
	RecordRequest("weekly")
	RecordRequest("monthly")
	if usage.InputTokens > 0 {
		RecordTokens("input", "daily", usage.InputTokens)
		RecordTokens("input", "weekly", usage.InputTokens)
		RecordTokens("input", "monthly", usage.InputTokens)
	}
	if usage.OutputTokens > 0 {
		RecordTokens("output", "daily", usage.OutputTokens)
		RecordTokens("output", "weekly", usage.OutputTokens)
		RecordTokens("output", "monthly", usage.OutputTokens)
	}
	RecordConcurrency(lm.semaphore.Current())
}

func (lm *LimitsManager) isExempt(req *CheckRequest) bool {
	if req == nil || req.Model == "" {
		return false
	}
	for _, pattern := range lm.config.Exempt.Patterns {
		if matchPattern(pattern, req.Model) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, value string) bool {
	if pattern == value {
		return true
	}
	if len(pattern) > 1 && (pattern[0] == '*' && pattern[len(pattern)-1] == '*') {
		substr := pattern[1 : len(pattern)-1]
		return contains(value, substr)
	}
	if len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return hasPrefix(value, prefix)
	}
	if len(pattern) > 1 && pattern[0] == '*' {
		suffix := pattern[1:]
		return hasSuffix(value, suffix)
	}
	return false
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (lm *LimitsManager) GetStatus() LimitsStatus {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	return LimitsStatus{
		Enabled:         lm.config.Enabled,
		SchedulerActive: lm.scheduler != nil,
		Requests: map[string]UsageStat{
			"daily":   {Current: lm.counter.RequestsDaily, Max: lm.config.Requests.Daily.Max},
			"weekly":  {Current: lm.counter.RequestsWeekly, Max: lm.config.Requests.Weekly.Max},
			"monthly": {Current: lm.counter.RequestsMonthly, Max: lm.config.Requests.Monthly.Max},
		},
		Tokens: map[string]UsageStat{
			"daily_input":    {Current: lm.counter.InputTokensDaily, Max: lm.config.Tokens.Daily.InputMax},
			"daily_output":   {Current: lm.counter.OutputTokensDaily, Max: lm.config.Tokens.Daily.OutputMax},
			"weekly_input":   {Current: lm.counter.InputTokensWeekly, Max: lm.config.Tokens.Weekly.InputMax},
			"weekly_output":  {Current: lm.counter.OutputTokensWeekly, Max: lm.config.Tokens.Weekly.OutputMax},
			"monthly_input":  {Current: lm.counter.InputTokensMonthly, Max: lm.config.Tokens.Monthly.InputMax},
			"monthly_output": {Current: lm.counter.OutputTokensMonthly, Max: lm.config.Tokens.Monthly.OutputMax},
		},
		Concurrency: UsageStat{
			Current: lm.semaphore.Current(),
			Max:     lm.config.Concurrency.MaxRunning,
		},
	}
}

type CheckRequest struct {
	Model    string
	Provider string
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type LimitsStatus struct {
	Enabled         bool
	SchedulerActive bool
	Requests        map[string]UsageStat
	Tokens          map[string]UsageStat
	Concurrency     UsageStat
}

type UsageStat struct {
	Current int
	Max     int
}

func (s UsageStat) Percent() float64 {
	if s.Max == 0 {
		return 0
	}
	return float64(s.Current) / float64(s.Max) * 100
}

type LimitError struct {
	Type        string
	Level       string
	Provider    string
	Current     int
	Max         int
	Enforcement string
	NextReset   time.Time
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("llm limit exceeded: %s %s (%d/%d)", e.Type, e.Level, e.Current, e.Max)
}

func IsLimitError(err error) bool {
	_, ok := err.(*LimitError)
	return ok
}
