package limit

import (
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// ResetScheduler manages periodic counter resets using robfig/cron.
type ResetScheduler struct {
	cron    *cron.Cron
	store   Store
	logger  *zap.Logger
	OnReset func() // called after each reset, can be nil
}

// NewResetScheduler creates a ResetScheduler.
// expr is a standard 5-field cron expression (e.g. "0 0 * * *" for daily at midnight).
// On startup, it checks if a reset was missed and runs one immediately if needed.
func NewResetScheduler(expr string, store Store, lastReset time.Time, logger *zap.Logger) (*ResetScheduler, error) {
	if _, err := cron.ParseStandard(expr); err != nil {
		return nil, err
	}

	rs := &ResetScheduler{
		cron:   cron.New(),
		store:  store,
		logger: logger,
	}

	// Check if we missed a reset.
	if !lastReset.IsZero() {
		next := nextResetAfter(expr, lastReset)
		if !next.IsZero() && time.Now().After(next) {
			logger.Info("limit: missed reset detected, executing now",
				zap.Time("last_reset", lastReset),
				zap.Time("scheduled_next", next),
			)
			rs.resetCounters()
		}
	}

	// Register the cron job.
	_, err := rs.cron.AddFunc(expr, rs.resetCounters)
	if err != nil {
		return nil, err
	}

	rs.cron.Start()
	return rs, nil
}

func (rs *ResetScheduler) resetCounters() {
	rs.logger.Info("limit: resetting counters")
	totalTokens, _ := rs.store.Get("llm.total_tokens")
	if err := rs.store.Reset(""); err != nil {
		rs.logger.Error("limit: reset failed", zap.Error(err))
		return
	}
	if totalTokens > 0 {
		rs.store.Increment("llm.total_tokens", totalTokens)
	}
	if rs.OnReset != nil {
		rs.OnReset()
	}
}

// Stop stops the cron scheduler.
func (rs *ResetScheduler) Stop() {
	ctx := rs.cron.Stop()
	<-ctx.Done()
}

// nextResetAfter computes the next fire time after the given time for a cron expression.
func nextResetAfter(expr string, after time.Time) time.Time {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}
	}
	return sched.Next(after)
}
