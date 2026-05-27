package provider

import (
	"context"
	"strings"
	"time"

	"dolphin/internal/config"

	"go.uber.org/zap"
)

// FailoverProvider manages a list of Providers and handles automatic failover
// when the current provider encounters an unrecoverable error.
type FailoverProvider struct {
	providers  []Provider
	configs    []config.ProviderConfig
	currentIdx int
	hcTimeout  time.Duration
}

// NewFailoverProvider creates a FailoverProvider wrapping each provider config
// in a RetryProvider for automatic retries. All providers are pre-created so
// SwitchToNext/SwitchTo can select among them without additional construction.
func NewFailoverProvider(cfgs []config.ProviderConfig, maxAttempts int, backoffBase, hcTimeout time.Duration) *FailoverProvider {
	providers := make([]Provider, len(cfgs))
	for i, c := range cfgs {
		base := NewProviderFromConfig(&c)
		providers[i] = NewRetryProvider(base, maxAttempts, backoffBase)
	}
	if hcTimeout <= 0 {
		hcTimeout = 10 * time.Second
	}
	return &FailoverProvider{
		providers:  providers,
		configs:    cfgs,
		currentIdx: 0,
		hcTimeout:  hcTimeout,
	}
}

func (f *FailoverProvider) Type() ProviderType { return f.current().Type() }
func (f *FailoverProvider) Name() string       { return f.current().Name() }
func (f *FailoverProvider) HealthCheck(ctx context.Context) error {
	return f.current().HealthCheck(ctx)
}

// current returns the provider at the current index with bounds safety.
func (f *FailoverProvider) current() Provider {
	if f.currentIdx >= len(f.providers) {
		f.currentIdx = 0
	}
	return f.providers[f.currentIdx]
}

// Complete calls the current provider's Complete (which includes retries via
// RetryProvider). If all retries are exhausted and the error is transient,
// failover to the next healthy provider and retry.
func (f *FailoverProvider) Complete(ctx context.Context, req ProviderRequest) (*ProviderResponse, error) {
	startIdx := f.currentIdx
	for {
		resp, err := f.current().Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		// Non-retryable error — no point failing over
		if !isRetryable(err) {
			return nil, err
		}
		// Try failover
		prevName := f.configs[f.currentIdx].Name
		if !f.SwitchToNext() {
			return nil, err
		}
		zap.S().Warnw("failed over to next provider on Complete",
			"from", prevName,
			"to", f.configs[f.currentIdx].Name,
		)
		// All providers tried — give up
		if f.currentIdx == startIdx {
			return nil, err
		}
	}
}

// CompleteStream calls the current provider's CompleteStream with retry/failover.
func (f *FailoverProvider) CompleteStream(ctx context.Context, req ProviderRequest) (<-chan StreamChunk, error) {
	startIdx := f.currentIdx
	for {
		ch, err := f.current().CompleteStream(ctx, req)
		if err == nil {
			return ch, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		prevName := f.configs[f.currentIdx].Name
		if !f.SwitchToNext() {
			return nil, err
		}
		zap.S().Warnw("failed over to next provider on CompleteStream",
			"from", prevName,
			"to", f.configs[f.currentIdx].Name,
		)
		if f.currentIdx == startIdx {
			return nil, err
		}
	}
}

// SwitchToNext advances to the next healthy provider. Returns false if no
// more providers pass the health check.
func (f *FailoverProvider) SwitchToNext() bool {
	for i := f.currentIdx + 1; i < len(f.providers); i++ {
		ctx, cancel := context.WithTimeout(context.Background(), f.hcTimeout)
		err := f.providers[i].HealthCheck(ctx)
		cancel()
		if err == nil {
			f.currentIdx = i
			zap.S().Infow("failed over to provider",
				"name", f.configs[i].Name,
				"model", f.configs[i].Model,
			)
			return true
		}
		zap.S().Warnw("failover health check failed",
			"name", f.configs[i].Name,
			"error", err,
		)
	}
	return false
}

// SwitchTo switches to the provider with the given name (case-insensitive).
// Returns true on success.
func (f *FailoverProvider) SwitchTo(name string) bool {
	for i, c := range f.configs {
		if !strings.EqualFold(c.Name, name) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), f.hcTimeout)
		err := f.providers[i].HealthCheck(ctx)
		cancel()
		if err != nil {
			zap.S().Warnw("switch to provider: health check failed",
				"name", name, "error", err,
			)
			return false
		}
		f.currentIdx = i
		zap.S().Infow("switched to provider",
			"name", c.Name,
			"model", c.Model,
		)
		return true
	}
	zap.S().Warnw("switch to provider: not found", "name", name)
	return false
}

// SelectProvider sets the current provider index (used during initial selection).
func (f *FailoverProvider) SelectProvider(idx int) {
	if idx >= 0 && idx < len(f.providers) {
		f.currentIdx = idx
	}
}

// Current returns the currently active Provider.
func (f *FailoverProvider) Current() Provider {
	return f.current()
}

// CurrentConfig returns the ProviderConfig for the currently active provider.
func (f *FailoverProvider) CurrentConfig() config.ProviderConfig {
	return f.configs[f.currentIdx]
}

// Configs returns all provider configs.
func (f *FailoverProvider) Configs() []config.ProviderConfig {
	return f.configs
}

// CurrentIndex returns the index of the currently active provider.
func (f *FailoverProvider) CurrentIndex() int {
	return f.currentIdx
}
