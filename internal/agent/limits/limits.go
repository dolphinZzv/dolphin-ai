package limits

import (
	"context"
	"fmt"
	"sync"

	"dolphin/internal/config"
)

type LimitsManager struct {
	config    *config.LimitsConfig
	counter   *TokenCounter
	semaphore *ConcurrencyLimiter
	mu        sync.RWMutex
}

func NewLimitsManager(cfg *config.LimitsConfig) *LimitsManager {
	return &LimitsManager{
		config:    cfg,
		counter:   NewTokenCounter(cfg),
		semaphore: NewConcurrencyLimiter(cfg.Concurrency.MaxRunning),
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
		return &LimitError{
			Type:        "concurrency",
			Current:     lm.semaphore.Current(),
			Max:         lm.config.Concurrency.MaxRunning,
			Enforcement: lm.config.Enforcement,
		}
	}
	defer lm.semaphore.Release()

	if lm.config.Requests.Daily.Max > 0 && lm.counter.RequestsDaily >= lm.config.Requests.Daily.Max {
		return &LimitError{
			Type:        "requests",
			Level:       "daily",
			Current:     lm.counter.RequestsDaily,
			Max:         lm.config.Requests.Daily.Max,
			Enforcement: lm.config.Enforcement,
		}
	}

	if lm.config.Requests.Weekly.Max > 0 && lm.counter.RequestsWeekly >= lm.config.Requests.Weekly.Max {
		return &LimitError{
			Type:        "requests",
			Level:       "weekly",
			Current:     lm.counter.RequestsWeekly,
			Max:         lm.config.Requests.Weekly.Max,
			Enforcement: lm.config.Enforcement,
		}
	}

	if lm.config.Requests.Monthly.Max > 0 && lm.counter.RequestsMonthly >= lm.config.Requests.Monthly.Max {
		return &LimitError{
			Type:        "requests",
			Level:       "monthly",
			Current:     lm.counter.RequestsMonthly,
			Max:         lm.config.Requests.Monthly.Max,
			Enforcement: lm.config.Enforcement,
		}
	}

	if lm.config.Tokens.Daily.InputMax > 0 && lm.counter.InputTokensDaily >= lm.config.Tokens.Daily.InputMax {
		return &LimitError{
			Type:        "tokens_input",
			Level:       "daily",
			Current:     lm.counter.InputTokensDaily,
			Max:         lm.config.Tokens.Daily.InputMax,
			Enforcement: lm.config.Enforcement,
		}
	}

	if lm.config.Tokens.Daily.OutputMax > 0 && lm.counter.OutputTokensDaily >= lm.config.Tokens.Daily.OutputMax {
		return &LimitError{
			Type:        "tokens_output",
			Level:       "daily",
			Current:     lm.counter.OutputTokensDaily,
			Max:         lm.config.Tokens.Daily.OutputMax,
			Enforcement: lm.config.Enforcement,
		}
	}

	return nil
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
}

func (lm *LimitsManager) isExempt(req *CheckRequest) bool {
	for _, pattern := range lm.config.Exempt.Patterns {
		if req.Model != "" && pattern == req.Model {
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
		SchedulerActive: false,
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
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("llm limit exceeded: %s %s (%d/%d)", e.Type, e.Level, e.Current, e.Max)
}

func IsLimitError(err error) bool {
	_, ok := err.(*LimitError)
	return ok
}
